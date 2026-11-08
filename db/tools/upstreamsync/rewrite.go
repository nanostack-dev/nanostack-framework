package main

import (
	"bytes"
	"fmt"
	pathpkg "path"
	"regexp"
	"strings"
)

const (
	// UpstreamModule is the go-jet module path that the fork replaces.
	UpstreamModule = "github.com/go-jet/jet/v2"
	// ForkModule is the module path of Nanostack DB.
	ForkModule = "github.com/nanostack-dev/nanostack-framework/db"

	headerFirstLine  = "// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit %s."
	headerSecondLine = "// See db/UPSTREAM.md for the upstream base and the list of changes."
)

// Header returns the modification notice that the fork puts at the top of every Go file
// taken from go-jet (Apache-2.0 §4(b)). shortSHA is the 7-character upstream commit the
// file comes from. The notice ends with a blank line before the original content.
func Header(shortSHA string) string {
	return fmt.Sprintf(headerFirstLine, shortSHA) + "\n" + headerSecondLine + "\n\n"
}

// Rewrite applies the fork's mechanical transformation to one upstream file, exactly as the
// original import did:
//   - in a go.mod that replaces go-jet with a local directory, the go-jet requirement's
//     version becomes v0.0.0 (see goModRequire);
//   - every occurrence of the go-jet module path in a text file becomes the fork module path;
//   - every .go file gets Header(shortSHA) prepended.
//
// Binary files (any file containing a NUL byte) are returned unchanged.
func Rewrite(path string, content []byte, shortSHA string) []byte {
	if bytes.IndexByte(content, 0) >= 0 {
		return content
	}
	out := content
	if pathpkg.Base(path) == "go.mod" && goModLocalReplace.Match(out) {
		out = goModRequire.ReplaceAll(out, []byte("${1}"+UpstreamModule+" v0.0.0${2}"))
	}
	out = bytes.ReplaceAll(out, []byte(UpstreamModule), []byte(ForkModule))
	if strings.HasSuffix(path, ".go") {
		out = append([]byte(Header(shortSHA)), out...)
	}
	return out
}

// Upstream's nested modules (tests, examples) require go-jet at a v2 tag and replace it
// with the parent directory. The fork's module path has no /v2 suffix, so a v2 version on
// it is invalid ("should be v0 or v1, not v2") and the go.mod does not parse. The replace
// decides what is built anyway, so the requirement becomes v0.0.0.
var (
	goModLocalReplace = regexp.MustCompile(
		`(?m)^\s*(?:replace\s+)?` + regexp.QuoteMeta(UpstreamModule) + `(?:\s+v\S+)?\s+=>\s+\.`,
	)
	goModRequire = regexp.MustCompile(
		`(?m)^(\s*(?:require\s+)?)` + regexp.QuoteMeta(UpstreamModule) + `\s+v\S+([ \t]*(?://.*)?)$`,
	)
)

// shortSHA returns the 7-character abbreviation used in file headers. It does not use
// `git rev-parse --short`, whose length depends on the repository.
func shortSHA(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
