package lint_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/lint"
)

type config struct {
	Version string `yaml:"version"`
	Run     struct {
		Timeout string `yaml:"timeout"`
	} `yaml:"run"`
	Formatters struct {
		Settings struct {
			Goimports struct {
				LocalPrefixes []string `yaml:"local-prefixes"`
			} `yaml:"goimports"`
		} `yaml:"settings"`
	} `yaml:"formatters"`
	Linters struct {
		Enable     []string `yaml:"enable"`
		Exclusions struct {
			Rules []struct {
				Path string `yaml:"path"`
			} `yaml:"rules"`
		} `yaml:"exclusions"`
	} `yaml:"linters"`
}

func render(t *testing.T, overlay string) config {
	t.Helper()
	out, err := lint.Config("header", []byte(overlay))
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if !strings.HasPrefix(string(out), "# header\n") {
		t.Fatalf("output does not start with the header:\n%s", out)
	}
	var c config
	if err = yaml.Unmarshal(out, &c); err != nil {
		t.Fatalf("output is not YAML: %v\n%s", err, out)
	}
	return c
}

func TestConfig(t *testing.T) {
	base := render(t, "")

	t.Run("an empty overlay keeps the base", func(t *testing.T) {
		if base.Version != "2" || len(base.Linters.Enable) == 0 {
			t.Fatalf("base = %+v, want version 2 and enabled linters", base)
		}
	})

	t.Run("a new key is added", func(t *testing.T) {
		got := render(t, "formatters:\n  settings:\n    goimports:\n      local-prefixes: [echopoint]\n")
		if p := got.Formatters.Settings.Goimports.LocalPrefixes; len(p) != 1 || p[0] != "echopoint" {
			t.Fatalf("local-prefixes = %v, want [echopoint]", p)
		}
	})

	t.Run("a scalar replaces the base value", func(t *testing.T) {
		if got := render(t, "run:\n  timeout: 20m\n"); got.Run.Timeout != "20m" {
			t.Fatalf("timeout = %q, want 20m", got.Run.Timeout)
		}
	})

	t.Run("a list gains only the items it lacks", func(t *testing.T) {
		got := render(t, "linters:\n  enable: [errcheck, dupl]\n")
		if len(got.Linters.Enable) != len(base.Linters.Enable)+1 ||
			got.Linters.Enable[len(got.Linters.Enable)-1] != "dupl" {
			t.Fatalf("enable = %v, want the base list plus dupl", got.Linters.Enable)
		}
	})

	t.Run("exclusion rules are appended", func(t *testing.T) {
		got := render(t, "linters:\n  exclusions:\n    rules:\n      - path: cmd/it/\n        linters: [noctx]\n")
		rules := got.Linters.Exclusions.Rules
		if len(rules) != len(base.Linters.Exclusions.Rules)+1 || rules[len(rules)-1].Path != "cmd/it/" {
			t.Fatalf("rules = %+v, want the base rules plus cmd/it/", rules)
		}
	})

	t.Run("a value of another shape is refused", func(t *testing.T) {
		_, err := lint.Config("header", []byte("linters:\n  enable: errcheck\n"))
		if err == nil || !strings.Contains(err.Error(), ".linters.enable") {
			t.Fatalf("err = %v, want a shape error naming .linters.enable", err)
		}
	})

	t.Run("invalid YAML is refused", func(t *testing.T) {
		_, err := lint.Config("header", []byte("linters: ["))
		if err == nil || !strings.Contains(err.Error(), "parse overlay") {
			t.Fatalf("err = %v, want a parse error", err)
		}
	})
}

func TestVersion(t *testing.T) {
	if v := lint.Version(); !strings.HasPrefix(v, "v2.") {
		t.Fatalf("Version() = %q, want a v2 release", v)
	}
}
