package wire

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
)

func exprString(e ast.Expr) string { return types.ExprString(e) }

// funcName is pkg.Name for messages.
func funcName(fn *types.Func) string {
	if fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Name() + "." + fn.Name()
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9' && i > 0:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "wire"
	}
	return strings.ToLower(b.String())
}

// relPos formats a position as file:line relative to Options.Base for files
// in the main module, and as importpath/file:line otherwise, so messages are
// the same on every machine.
// at returns the position of p for a problem: the file relative to
// Options.Base, and the line.
func (m *manifest) at(p token.Pos) pos {
	if !p.IsValid() {
		return pos{}
	}
	at := m.fset.Position(p)
	if at.Filename == "" {
		return pos{}
	}
	return pos{file: m.relFile(at.Filename), line: at.Line}
}

func (m *manifest) relPos(p token.Pos) string {
	if !p.IsValid() {
		return ""
	}
	pos := m.fset.Position(p)
	if pos.Filename == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", m.relFile(pos.Filename), pos.Line)
}

func (m *manifest) relFile(file string) string {
	if inDir(m.modDir, file) {
		if rel, err := filepath.Rel(m.opts.Base, file); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	// A file in the module cache or GOROOT: name it by its package.
	if pkgPath, ok := m.fileToPkg[file]; ok {
		return pkgPath + "/" + filepath.Base(file)
	}
	return filepath.Base(file)
}

// declPos formats the declaration position of obj like relPos.
func (m *manifest) declPos(obj types.Object) string {
	pos := m.fset.Position(obj.Pos())
	if !pos.IsValid() || pos.Filename == "" {
		return ""
	}
	if !inDir(m.modDir, pos.Filename) && obj.Pkg() != nil {
		return fmt.Sprintf("%s/%s:%d", obj.Pkg().Path(), filepath.Base(pos.Filename), pos.Line)
	}
	return m.relPos(obj.Pos())
}

func inDir(dir, file string) bool {
	rel, err := filepath.Rel(dir, file)
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// typeString prints t with package names, for messages and lifecycle names.
func (m *manifest) typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

func (m *manifest) tupleString(t *types.Tuple) string {
	parts := make([]string, t.Len())
	for i := range t.Len() {
		parts[i] = m.typeString(t.At(i).Type())
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (m *manifest) providerLabel(p *provider) string {
	switch p.kind {
	case kindBind:
		return m.bindLabel(p)
	case kindCollect:
		return m.collectLabel(p)
	case kindInvoke:
		return "nano.Invoke(" + funcName(p.fn) + ")"
	case kindFunc, kindMember:
	}
	return funcName(p.fn)
}

func (m *manifest) collectLabel(p *provider) string {
	return fmt.Sprintf("nano.Collect[%s]", m.typeString(p.elem))
}

func (m *manifest) bindLabel(p *provider) string {
	return fmt.Sprintf("nano.Bind[%s, %s]", m.typeString(p.out), m.typeString(p.impl))
}
