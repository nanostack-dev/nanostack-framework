package genvalidate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// payload would end a // comment and add a declaration to the generated
// file if spec text reached a comment unescaped.
const payload = `x\nfunc init() { panic(\"pwned\") }\n//`

func TestSpecTextCannotLeaveAComment(t *testing.T) {
	cases := map[string]struct {
		golden string
		edit   func(string) string
	}{
		"format": {"required", func(s string) string {
			return strings.Replace(s, "note: {type: string, maxLength: 3}",
				`note: {type: string, maxLength: 3, format: "`+payload+`"}`, 1)
		}},
		"type": {"required", func(s string) string {
			return strings.Replace(s, "tags: {type: array, items: {type: string}}",
				`tags: {type: "`+payload+`", items: {type: string}}`, 1)
		}},
		"discriminator mapping": {"unions", func(s string) string {
			return strings.Replace(s, "basic: '#/components/schemas/Basic'",
				"basic: '#/components/schemas/Basic'\n          \""+payload+"\": '#/components/schemas/Cat'", 1)
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(goldenRoot, c.golden)
			spec := writeSpec(t, dir, c.edit)
			res, err := generateWith(Options{Spec: spec, Dir: dir}, loaded(t, dir))
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(token.NewFileSet(), "validate.gen.go", res.File.Data, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "init" {
					t.Fatalf("spec text added a declaration:\n%s", res.File.Data)
				}
			}
			if !strings.Contains(string(res.File.Data), `\nfunc init()`) {
				t.Fatalf("the escaped payload is missing:\n%s", res.File.Data)
			}
			for _, d := range res.Diagnostics {
				if strings.ContainsAny(d.String(), "\n\r") {
					t.Errorf("diagnostic %q carries a line break", d)
				}
			}
		})
	}
}

// A boolean enum value is decoded, never copied: YAML's True is true, and a
// value tagged !!bool that is not a boolean is a spec error, not Go code.
func TestBooleanEnumValuesAreDecoded(t *testing.T) {
	dir := filepath.Join(goldenRoot, "enums")
	spec := writeSpec(t, dir, func(s string) string {
		return strings.Replace(s, "flag: {type: boolean, enum: [true]}", "flag: {type: boolean, enum: [True]}", 1)
	})
	res, err := generateWith(Options{Spec: spec, Dir: dir}, loaded(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.File.Data), "True") || !strings.Contains(string(res.File.Data), "case true:") {
		t.Fatalf("enum [True] must generate case true:\n%s", res.File.Data)
	}

	for _, tagged := range []string{
		`!!bool "true || func() bool { panic(\"pwned\") }()"`,
		`!!bool yes`,
		`!!int "1 + f()"`,
	} {
		spec = writeSpec(t, dir, func(s string) string {
			return strings.Replace(
				s,
				"flag: {type: boolean, enum: [true]}",
				"flag: {type: boolean, enum: ["+tagged+"]}",
				1,
			)
		})
		if _, err = generateWith(Options{Spec: spec, Dir: dir}, loaded(t, dir)); err == nil ||
			!strings.Contains(err.Error(), "properties/flag") {
			t.Errorf("enum [%s]: err = %v, want a spec error at the property", tagged, err)
		}
	}
}
