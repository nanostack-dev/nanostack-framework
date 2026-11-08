package genapi

import (
	"runtime/debug"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/oapitemplates"
)

// modulePath is oapi-codegen's module.
const modulePath = "github.com/oapi-codegen/oapi-codegen/v2"

// getFix is the fix of every version problem: the service module pins the
// version that nano resolves.
const getFix = "go get " + modulePath + "@" + oapitemplates.OapiCodegenVersion

// readBuildInfo is debug.ReadBuildInfo; a test replaces it.
var readBuildInfo = debug.ReadBuildInfo //nolint:gochecknoglobals // a seam for the version test.

// pinnedVersion returns the oapi-codegen version linked into this binary,
// and an environment error unless it is the one the templates are cut from.
func pinnedVersion() (string, error) {
	info, ok := readBuildInfo()
	return checkVersion(info, ok)
}

// checkVersion checks the oapi-codegen module in the build
// information. A replaced module fails as well: its templates and its
// output are not the pinned release's.
func checkVersion(info *debug.BuildInfo, ok bool) (string, error) {
	want := oapitemplates.OapiCodegenVersion
	if !ok || info == nil {
		return "", gen.EnvErrorf("build nano from its module: go tool nano, or go run .../gen/cmd/nano",
			"the nano binary has no build information, so the oapi-codegen version is unknown; "+
				"nano templates are pinned to %s", want)
	}
	for _, dep := range info.Deps {
		if dep.Path != modulePath {
			continue
		}
		if dep.Replace != nil {
			return "", gen.EnvErrorf("remove the replace of "+modulePath+" from go.mod, then "+getFix,
				"oapi-codegen is replaced by %s %s; nano templates are pinned to %s",
				dep.Replace.Path, dep.Replace.Version, want)
		}
		if dep.Version != want {
			return "", gen.EnvErrorf(getFix,
				"oapi-codegen %s resolved; nano templates are pinned to %s", dep.Version, want)
		}
		return dep.Version, nil
	}
	return "", gen.EnvErrorf(getFix,
		"the nano binary links no %s; nano templates are pinned to %s", modulePath, want)
}
