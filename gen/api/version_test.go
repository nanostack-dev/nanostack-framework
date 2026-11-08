package genapi

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
)

// fakeBuildInfo makes the binary report oapi-codegen at version, replaced
// by replace when it is not nil, until the test ends.
func fakeBuildInfo(t *testing.T, version string, replace *debug.Module) {
	t.Helper()
	saved := readBuildInfo
	t.Cleanup(func() { readBuildInfo = saved })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return buildInfo(&debug.Module{Path: modulePath, Version: version, Replace: replace}), true
	}
}

func buildInfo(deps ...*debug.Module) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Path: "example.com/service"}, Deps: deps}
}

func TestVersionCheck(t *testing.T) {
	other := &debug.Module{Path: "golang.org/x/tools", Version: "v0.52.0"}
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string // the message; empty when the check passes
		fix  string
	}{
		{"pinned", buildInfo(other, &debug.Module{Path: modulePath, Version: "v2.8.2"}), true, "", ""},
		{"newer", buildInfo(&debug.Module{Path: modulePath, Version: "v2.9.0"}), true,
			"oapi-codegen v2.9.0 resolved; nano templates are pinned to v2.8.2",
			"go get github.com/oapi-codegen/oapi-codegen/v2@v2.8.2"},
		{"older", buildInfo(&debug.Module{Path: modulePath, Version: "v2.8.0"}), true,
			"oapi-codegen v2.8.0 resolved", "go get github.com/oapi-codegen/oapi-codegen/v2@v2.8.2"},
		{
			"replaced",
			buildInfo(&debug.Module{Path: modulePath, Version: "v2.8.2",
				Replace: &debug.Module{Path: "../oapi-codegen"}}),
			true,
			"oapi-codegen is replaced by ../oapi-codegen",
			"remove the replace of github.com/oapi-codegen/oapi-codegen/v2",
		},
		{"absent", buildInfo(other), true, "links no github.com/oapi-codegen/oapi-codegen/v2",
			"go get github.com/oapi-codegen/oapi-codegen/v2@v2.8.2"},
		{"no build info", nil, false, "no build information", "build nano from its module"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := checkVersion(tt.info, tt.ok)
			if tt.want == "" {
				if err != nil || v != "v2.8.2" {
					t.Fatalf("checkVersion = %q, %v", v, err)
				}
				return
			}
			problems := gentest.Problems(t, err)
			if len(problems) != 1 || !strings.Contains(problems[0].Msg, tt.want) ||
				!strings.HasPrefix(problems[0].Fix, tt.fix) {
				t.Errorf("problems = %v, want %q with fix %q", problems, tt.want, tt.fix)
			}
			if !gentest.Environment(err) {
				t.Error("a version mismatch must exit 3: it is the machine, not the source")
			}
		})
	}
}

// The test binary links the gen module's oapi-codegen, which go.mod pins.
func TestThisBinaryIsPinned(t *testing.T) {
	if _, err := pinnedVersion(); err != nil {
		t.Fatalf("gen/go.mod resolves another oapi-codegen than the templates: %v", err)
	}
}
