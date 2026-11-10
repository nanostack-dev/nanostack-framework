package genrepo

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// uniqueKey is one key of a table, as nanostack gen db lists it in the UniqueKeys
// method of the table type: the primary key, or a unique index that matches
// at most one row (design D24).
type uniqueKey struct {
	name    string   // constraint or index name
	sqlCols []string // column names, in index order
	fields  []string // Go fields of the columns, same order; "" for a column the table no longer has
	pred    string   // "WHERE (external_id IS NOT NULL)" for a partial index, else ""
}

func (k uniqueKey) String() string {
	s := k.name + " (" + strings.Join(k.sqlCols, ", ") + ")"
	if k.pred != "" {
		s += " " + k.pred
	}
	return s
}

// readUniqueKeys reads each func (XTable) UniqueKeys() map[string][]string
// of f: its keys, and the predicate a trailing "// WHERE (...)" comment
// gives a partial key. nanostack gen repo reads the keys from source and needs no
// database.
func readUniqueKeys(fset *token.FileSet, f *ast.File, src *tableFiles) {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "UniqueKeys" || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Body == nil {
			continue
		}
		recv := identOf(fd.Recv.List[0].Type)
		if recv == nil {
			continue
		}
		keys := []uniqueKey{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			kv, isKV := n.(*ast.KeyValueExpr)
			if !isKV {
				return true
			}
			if k, read := keyOf(fset, f, kv); read {
				keys = append(keys, k)
			}
			return false
		})
		src.keys[recv.Name] = keys
		src.notKeys[recv.Name] = notKeysOf(fd)
	}
}

// notKeysOf reads the "Not a key: <index>: <reason>" lines of the doc
// comment of UniqueKeys: the unique indexes that nanostack gen db leaves out
// because they do not prove one row, such as a partial index or one on an
// expression.
func notKeysOf(fd *ast.FuncDecl) []string {
	if fd.Doc == nil {
		return nil
	}
	var out []string
	for _, c := range fd.Doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if rest, ok := strings.CutPrefix(text, "Not a key: "); ok {
			out = append(out, strings.TrimSuffix(rest, "."))
		}
	}
	return out
}

// keyOf reads one "name": {"col", ...} entry, with its trailing comment.
func keyOf(fset *token.FileSet, f *ast.File, kv *ast.KeyValueExpr) (uniqueKey, bool) {
	name, ok := stringLit(kv.Key)
	lit, isLit := kv.Value.(*ast.CompositeLit)
	if !ok || !isLit || len(lit.Elts) == 0 {
		return uniqueKey{}, false
	}
	k := uniqueKey{name: name}
	for _, e := range lit.Elts {
		col, isCol := stringLit(e)
		if !isCol {
			return uniqueKey{}, false
		}
		k.sqlCols = append(k.sqlCols, col)
	}
	line := fset.Position(kv.End()).Line
	for _, cg := range f.Comments {
		if cg.Pos() > kv.End() && fset.Position(cg.Pos()).Line == line {
			if pred := strings.TrimSpace(cg.Text()); strings.HasPrefix(pred, "WHERE") {
				k.pred = pred
			}
			break
		}
	}
	return k, true
}

// resolveKeys maps the key columns of t to its Go fields. A column the
// table no longer has maps to "", so the key never proves a method.
func (t *tableBase) resolveKeys(keys []uniqueKey, found bool) {
	t.keysFound = found
	for _, k := range keys {
		k.fields = make([]string, len(k.sqlCols))
		for i, col := range k.sqlCols {
			for _, c := range t.columns {
				if c.sqlName == col {
					k.fields[i] = c.field
				}
			}
		}
		t.keys = append(t.keys, k)
	}
}

// provingKey returns the first key whose columns are all among fields: a
// condition on them matches at most one row.
func (t *tableBase) provingKey(fields []string) (uniqueKey, bool) {
	for _, k := range t.keys {
		if slices.Contains(k.fields, "") {
			continue
		}
		if !slices.ContainsFunc(k.fields, func(f string) bool { return !slices.Contains(fields, f) }) {
			return k, true
		}
	}
	return uniqueKey{}, false
}

// keyList renders the keys of t for a message.
func (t *tableBase) keyList() string {
	var out string
	if len(t.keys) == 0 {
		out = t.sqlName + " has no unique key"
	} else {
		s := make([]string, len(t.keys))
		for i, k := range t.keys {
			s[i] = k.String()
		}
		out = "the keys of " + t.sqlName + " are " + strings.Join(s, ", ")
	}
	if len(t.notKeys) > 0 {
		out += "; its unique indexes that are not keys, because they do not prove one row, are " +
			strings.Join(t.notKeys, ", ")
	}
	return out
}
