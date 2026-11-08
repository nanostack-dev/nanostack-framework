// Package importset assigns import names for a generated Go file, so two
// imported packages, or an import and a name the package already declares,
// never collide. Every nano generator (config, errors, validate and wire)
// uses it, so all generated files name their imports by the same rules.
package importset

import (
	"fmt"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"
)

// blocked marks a name the package declares itself; no import may take it.
const blocked = "\x00"

// Set assigns stable names to the packages a generated file uses. The zero
// value is not usable; call New.
type Set struct {
	self   string
	byPath map[string]string
	byName map[string]string
	used   map[string]bool
}

// New returns an empty Set for a file in package self; self is never imported.
func New(self string) *Set {
	return &Set{self: self, byPath: map[string]string{}, byName: map[string]string{}, used: map[string]bool{}}
}

// Block keeps names away from imports, typically the package-level names the
// package declares in its other files. Block before any Add or Reserve.
func (s *Set) Block(names ...string) {
	for _, n := range names {
		if _, taken := s.byName[n]; !taken {
			s.byName[n] = blocked
		}
	}
}

// Reserve claims a name for p without importing it, so code written before
// the first Add, such as a hard-coded "time.Second", stays correct. It returns
// the claimed name, which differs from preferred when that is taken.
func (s *Set) Reserve(p, preferred string) string {
	if name, ok := s.byPath[p]; ok {
		return name
	}
	name := preferred
	for i := 2; s.byName[name] != "" || predeclared(name); i++ {
		name = preferred + strconv.Itoa(i)
	}
	s.byPath[p], s.byName[name] = name, p
	return name
}

// Add returns the name to use for p and imports it, choosing preferred unless
// another package or a blocked name already took it.
func (s *Set) Add(p, preferred string) string {
	name := s.Reserve(p, preferred)
	s.used[p] = true
	return name
}

// Qualifier is a types.Qualifier that imports every package it names.
func (s *Set) Qualifier(p *types.Package) string {
	if p.Path() == s.self {
		return ""
	}
	return s.Add(p.Path(), p.Name())
}

// Name returns the name assigned to p, or "" when p has none.
func (s *Set) Name(p string) string { return s.byPath[p] }

// predeclared reports whether name is a Go keyword or a predeclared
// identifier (error, len, any, ...), which an import must not shadow.
func predeclared(name string) bool {
	return token.IsKeyword(name) || types.Universe.Lookup(name) != nil
}

// Write writes the import block of every added package: standard library
// first, then the rest, each sorted by path. A name is written whenever it
// differs from the last path element, so a package whose name does not match
// its path (gopkg.in/yaml.v3, a /v2 module root) is always named explicitly.
// It writes nothing when no package was added.
func (s *Set) Write(b *strings.Builder) {
	if len(s.used) == 0 {
		return
	}
	var std, other []string
	for p := range s.used {
		if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
			other = append(other, p)
		} else {
			std = append(std, p)
		}
	}
	slices.Sort(std)
	slices.Sort(other)
	b.WriteString("import (\n")
	for i, group := range [][]string{std, other} {
		if i == 1 && len(std) > 0 && len(other) > 0 {
			b.WriteString("\n")
		}
		for _, p := range group {
			if name := s.byPath[p]; name != path.Base(p) {
				fmt.Fprintf(b, "\t%s %q\n", name, p)
			} else {
				fmt.Fprintf(b, "\t%q\n", p)
			}
		}
	}
	b.WriteString(")\n\n")
}
