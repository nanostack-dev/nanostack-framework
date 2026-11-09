package gopkg

import (
	"bytes"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Names of the tag split that nanostack gen api writes into the server package
// (ADR 0024). A tag interface is declared in HandlersFile, next to
// NewHandlers.
const (
	HandlersFile = "handlers.gen.go"
	handlersFunc = "NewHandlers"
)

// implementPattern matches go/types' report of a type that lacks a method of
// an interface, or has it with another signature.
//
//nolint:gochecknoglobals // compiled once.
var implementPattern = regexp.MustCompile(
	`(\S+) does not implement (\S+) \((missing method|wrong type for method) (\w+)\)`)

// listErrorLine matches one error of the go command's compile report:
// "file:line:col: msg".
//
//nolint:gochecknoglobals // compiled once.
var listErrorLine = regexp.MustCompile(`^(.+?\.go):(\d+)(?::\d+)?: (.*)$`)

// IsTagServer reports whether obj is an interface that nanostack gen api wrote
// for an OpenAPI tag: it sits in handlers.gen.go, next to NewHandlers.
func IsTagServer(fset *token.FileSet, obj types.Object) bool {
	if obj == nil || obj.Pkg() == nil || !types.IsInterface(obj.Type()) {
		return false
	}
	if filepath.Base(fset.Position(obj.Pos()).Filename) != HandlersFile {
		return false
	}
	fn, ok := obj.Pkg().Scope().Lookup(handlersFunc).(*types.Func)
	return ok && filepath.Base(fset.Position(fn.Pos()).Filename) == HandlersFile
}

// MethodDecl returns the declaration a type needs to have method m:
// "func (h *emailHandler) SendEmail(ctx context.Context, …) (…, error)", with
// the types qualified as code in package from writes them: a type of from
// unqualified, any other by its package name, as in apigen.SendEmailRequestObject.
// The receiver name is the one the type's other methods use, or its first
// letter.
func MethodDecl(recv types.Type, m *types.Func, from *types.Package) string {
	qual := func(p *types.Package) string {
		if p == from {
			return ""
		}
		return p.Name()
	}
	sig, ok := m.Type().(*types.Signature)
	if !ok {
		return ""
	}
	var b bytes.Buffer
	b.WriteString("func (" + receiverName(recv) + " " + types.TypeString(recv, qual) + ") " + m.Name())
	types.WriteSignature(&b, sig, qual)
	return b.String()
}

// receiverName returns the receiver name of recv's methods, or the first
// letter of its type name, lowered.
func receiverName(recv types.Type) string {
	t := recv
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return "x"
	}
	for method := range named.Methods() {
		if sig, isSig := method.Type().(*types.Signature); isSig && sig.Recv() != nil {
			if n := sig.Recv().Name(); n != "" && n != "_" {
				return n
			}
		}
	}
	r, _ := utf8.DecodeRuneInString(named.Obj().Name())
	return string(unicode.ToLower(r))
}

// tagMethodFix returns the fix of msg, a type error of pkg, when msg says a
// type lacks a method of a tag interface, or has it with another signature:
// the method to add, or the signature to give it, in full.
func tagMethodFix(pkg *packages.Package, msg string) (string, bool) {
	if pkg.Types == nil || pkg.Fset == nil {
		return "", false
	}
	m := implementPattern.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	recvText, ifaceText, wrongType, method := m[1], m[2], m[3] != "missing method", m[4]
	obj := lookup(pkg.Types, ifaceText)
	if !IsTagServer(pkg.Fset, obj) {
		return "", false
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return "", false
	}
	var fn *types.Func
	for f := range iface.Methods() {
		if f.Name() == method {
			fn = f
		}
	}
	recv := receiverType(pkg.Types, recvText)
	if fn == nil || recv == nil {
		return "", false
	}
	decl := MethodDecl(recv, fn, pkg.Types)
	if wrongType {
		return "give " + method + " the signature of " + ifaceText + ": " + decl, true
	}
	return "add the method of " + ifaceText + ": " + decl, true
}

// lookup resolves a type name as go/types prints it in an error of pkg:
// Name for pkg's own, pkgname.Name for an import's.
func lookup(pkg *types.Package, name string) types.Object {
	qual, local, found := strings.Cut(name, ".")
	if !found {
		return pkg.Scope().Lookup(name)
	}
	for _, imp := range pkg.Imports() {
		if imp.Name() == qual {
			return imp.Scope().Lookup(local)
		}
	}
	return nil
}

// receiverType resolves "T" or "*T" as go/types prints it in an error of
// pkg.
func receiverType(pkg *types.Package, text string) types.Type {
	name, ptr := strings.CutPrefix(text, "*")
	obj, ok := lookup(pkg, name).(*types.TypeName)
	if !ok {
		return nil
	}
	if ptr {
		return types.NewPointer(obj.Type())
	}
	return obj.Type()
}

// TagMethodErrors returns, for every package that roots import, each error
// that says a type lacks a method of a tag interface, with the method to add
// as the fix. The roots' own errors are left to CompileErrors. A load of an
// application reaches its handler packages only as dependencies, whose other
// compile errors the generators do not report. dir is the directory of the
// load, which the go command's report is relative to.
func TagMethodErrors(roots []*packages.Package, dir string, rel func(string) string) []error {
	isRoot := map[*packages.Package]bool{}
	for _, p := range roots {
		isRoot[p] = true
	}
	var out []error
	packages.Visit(roots, nil, func(p *packages.Package) {
		if isRoot[p] {
			return
		}
		for _, e := range p.Errors {
			for _, line := range errorLines(e) {
				out = append(out, tagMethodError(p, line, dir, rel)...)
			}
		}
	})
	return out
}

// tagMethodError returns the problem of one error of pkg when it says a
// type lacks a method of a tag interface, and nothing otherwise.
func tagMethodError(pkg *packages.Package, line errorLine, dir string, rel func(string) string) []error {
	fix, ok := tagMethodFix(pkg, line.msg)
	if !ok {
		return nil
	}
	file := line.file
	if file != "" && !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	if file != "" {
		file = rel(file)
	}
	return []error{gen.Errorf(file, line.line, fix, "%s", implementPattern.FindString(line.msg))}
}

type errorLine struct {
	file string
	line int
	msg  string
}

// errorLines splits a go/packages error into its errors: a type error is
// one, at its position; the go command's compile report holds one per line
// after its "# importpath" line.
func errorLines(e packages.Error) []errorLine {
	if e.Kind != packages.ListError || !strings.HasPrefix(e.Msg, "# ") {
		file, line := SplitPos(e.Pos)
		return []errorLine{{file, line, e.Msg}}
	}
	var out []errorLine
	for _, l := range strings.Split(e.Msg, "\n")[1:] {
		m := listErrorLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		file, line := SplitPos(m[1] + ":" + m[2])
		out = append(out, errorLine{file, line, m[3]})
	}
	return out
}
