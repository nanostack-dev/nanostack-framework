package genconfig

import (
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
)

// TestDefaultEnvErrorsNameTheFix covers the defaultenv tag errors, and the
// env clash, whose fix names defaultenv only where it would work: on a
// setting whose env tag can go.
func TestDefaultEnvErrorsNameTheFix(t *testing.T) {
	cases := map[string]struct{ fields, msg, fix string }{
		"bad defaultenv": {
			"Name string `config:\"name\" defaultenv:\"name\"`",
			`defaultenv "name" must be upper snake case`, "upper snake case",
		},
		"defaultenv is its own env": {
			"Name string `config:\"name\" defaultenv:\"NAME\"`",
			"defaultenv NAME is the setting's own env", "remove defaultenv",
		},
		"defaultenv on a nested struct": {
			"A struct{ X string `config:\"x\"` } `config:\"a\" defaultenv:\"X\"`",
			"A: defaultenv does not apply to a nested struct", "set it on the fields",
		},
		"defaultenv on a secret": {
			"Key secret.Secret `config:\"key\" defaultenv:\"SHARED\"`",
			"secrets cannot have a default, not even from env SHARED", "remove defaultenv",
		},
		"defaultenv reads a secret": {
			"Key secret.Secret `config:\"key\" env:\"STRIPE_KEY\"`\n\tName string `config:\"name\" defaultenv:\"STRIPE_KEY\"`",
			"Name: defaultenv STRIPE_KEY is the env of secret setting key",
			"would print the secret",
		},
		"defaultenv reads a secret declared later": {
			"Name string `config:\"name\" defaultenv:\"STRIPE_KEY\"`\n\tKey secret.Secret `config:\"key\" env:\"STRIPE_KEY\"`",
			"Name: defaultenv STRIPE_KEY is the env of secret setting key",
			"would print the secret",
		},
		"env clash": {
			"A string `config:\"a\" env:\"X\"`\n\tB string `config:\"b\" env:\"X\"`",
			"B: env X is already used by a",
			`tag b defaultenv:"X" instead of env:"X" to read X after its own env (B)`,
		},
		"env clash with a derived env": {
			"B string `config:\"b\" env:\"A\"`\n\tA string `config:\"a\"`",
			"A: env A is already used by b",
			`tag b defaultenv:"A" instead of env:"A" to read A after its own env (B)`,
		},
		"env clash of two derived envs": {
			"AB struct{ C string `config:\"c\"` } `config:\"a_b\"`\n\tA struct{ BC string `config:\"b_c\"` } `config:\"a\"`",
			"BC: env A_B_C is already used by a_b.c",
			`another env with an env tag; tag that one defaultenv:"A_B_C" as well`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "package bad\n\nimport \"github.com/nanostack-dev/nanostack-framework/core/secret\"\n\n" +
				"var _ secret.Secret\n\ntype Config struct {\n\t" + tc.fields + "\n}\n"
			_, err := Generate(Options{Dir: badPackage(t, src)})
			problems := gentest.Problems(t, err)
			if !slices.ContainsFunc(problems, func(p *gen.Error) bool {
				return strings.Contains(p.Msg, tc.msg) && strings.Contains(p.Fix, tc.fix) &&
					p.File == "config.go" && p.Line > 0
			}) {
				t.Fatalf(
					"got error %v\nwant a problem at config.go:<line> with message %q and fix %q",
					err,
					tc.msg,
					tc.fix,
				)
			}
		})
	}
}
