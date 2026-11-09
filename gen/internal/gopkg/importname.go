package gopkg

import (
	"go/ast"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var majorVersion = regexp.MustCompile(`^v[0-9]+$`) //nolint:gochecknoglobals // compiled once.

// ImportName returns the name an import binds: its explicit name, or a guess
// from its path (the last element, without a major version suffix), for code
// that reads files without type-checking them.
func ImportName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	p, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	name := path.Base(p)
	if majorVersion.MatchString(name) && path.Dir(p) != "." {
		name = path.Base(path.Dir(p))
	}
	if i := strings.Index(name, ".v"); i > 0 {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, "go-")
	return strings.ReplaceAll(name, "-", "_")
}
