package genrepo

import (
	"fmt"
	"go/token"
	"io"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// mode is the read-only run that a flag asks for instead of generation.
type mode struct{ explain, verify, inventory bool }

// mainMode runs the read-only mode of m, if any; handled is false when the
// run generates.
func mainMode(opts Options, m mode, names []string, stdout, stderr io.Writer) (code int, handled bool) {
	switch {
	case m.inventory:
		return mainInventory(opts, stdout, stderr), true
	case m.verify:
		return mainVerify(opts, stdout, stderr), true
	case m.explain:
		return mainExplain(opts, names, stdout, stderr), true
	}
	return 0, false
}

// parseScopePairs reads the -context-scopes and -context-stamps pairs,
// column=importpath.Func, the -scope-names pairs, column=Name, and the
// -scope-implies pairs, column=column, into opts.
func parseScopePairs(
	opts *Options,
	contextScopes, contextStamps, contextPresents, scopeNames, scopeImplies string,
) error {
	var err error
	hasValue := func(_, v string) bool { return v != "" }
	if opts.ContextScopes, err = parsePairs(opts.ContextScopes, "-context-scopes", "column=importpath.Func",
		contextScopes, hasValue); err != nil {
		return err
	}
	if opts.ContextStamps, err = parsePairs(opts.ContextStamps, "-context-stamps", "column=importpath.Func",
		contextStamps, hasValue); err != nil {
		return err
	}
	if opts.ContextPresents, err = parsePairs(opts.ContextPresents, "-context-present", "column=importpath.Func",
		contextPresents, hasValue); err != nil {
		return err
	}
	if opts.ScopeNames, err = parsePairs(opts.ScopeNames, "-scope-names", "column=ExportedName",
		scopeNames, func(_, v string) bool { return token.IsExported(v) && token.IsIdentifier(v) }); err != nil {
		return err
	}
	opts.ScopeImplies, err = parsePairs(opts.ScopeImplies, "-scope-implies", "column=column",
		scopeImplies, hasValue)
	return err
}

// parsePairs reads a comma list of key=value pairs into dst, which it
// allocates on the first pair. valid checks the value.
func parsePairs(
	dst map[string]string,
	flagName, shape, list string,
	valid func(k, v string) bool,
) (map[string]string, error) {
	for pair := range strings.SplitSeq(list, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" || !valid(k, v) {
			return dst, fmt.Errorf("%s entry %q is not %s", flagName, pair, shape)
		}
		if dst == nil {
			dst = map[string]string{}
		}
		dst[k] = v
	}
	return dst, nil
}

// mainVerify runs VerifyScopes and prints each violation.
func mainVerify(opts Options, stdout, stderr io.Writer) int {
	v, err := VerifyScopes(opts)
	if err != nil {
		return gen.Report(stderr, "repo", err)
	}
	for _, bad := range v.Violations {
		_, _ = fmt.Fprintln(stderr, "nanostack gen repo: scope not covered: "+bad)
	}
	if len(v.Violations) > 0 {
		_, _ = fmt.Fprintf(stderr, "nanostack gen repo: %d of %d derived methods leave a scope column to the value or "+
			"to nothing\n", len(v.Violations), v.Methods)
		return 1
	}
	_, _ = fmt.Fprintf(
		stdout,
		"nanostack gen repo: %d derived methods read, every one covers every scope column\n",
		v.Methods,
	)
	return 0
}

// mainInventory lists what a review looks at: the derived methods that skip a
// scope (Internal), that read a context scope from a parameter, or that keep a
// fail-open filter on a reviewed choice, and the hand-written methods whose
// names also derive (their code is not verified).
func mainInventory(opts Options, stdout, stderr io.Writer) int {
	res, err := Generate(opts)
	if err != nil {
		return gen.Report(stderr, "repo", err)
	}
	n := 0
	for _, r := range res.resolutions {
		for _, m := range r.derived {
			if notes := reviewNotes(r, m); len(notes) > 0 {
				n++
				_, _ = fmt.Fprintf(stdout, "%s.%s (table %s): %s\n", r.b.iface.Obj().Name(), m.fn.Name(),
					r.b.table.sqlName, strings.Join(notes, "; "))
			}
		}
	}
	for _, r := range res.resolutions {
		for _, name := range r.overrides {
			n++
			_, _ = fmt.Fprintf(stdout, "%s.%s (table %s): HAND-WRITTEN (derivable), the name also derives and the "+
				"hand-written method wins: its code is not verified\n", r.b.iface.Obj().Name(), name, r.b.table.sqlName)
		}
	}
	_, _ = fmt.Fprintf(stdout, "nanostack gen repo: %d derived methods to review\n", n)
	return 0
}

// mainExplain prints the resolution of each bound interface and what each
// search reads, or only those named, and writes nothing.
func mainExplain(opts Options, names []string, stdout, stderr io.Writer) int {
	res, err := Generate(opts)
	if res != nil {
		var shown []Explanation
		for _, e := range res.Explain {
			if len(names) == 0 || slices.Contains(names, e.Iface) {
				shown = append(shown, e)
			}
		}
		var searches []SearchExplanation
		for _, e := range res.Searches {
			if len(names) == 0 || slices.Contains(names, e.Name) {
				searches = append(searches, e)
			}
		}
		if len(shown) == 0 && len(searches) == 0 && err == nil {
			_, _ = fmt.Fprintf(stderr, "nanostack gen repo: no nanorepo.Implement binds %s, and no nanorepo.Search "+
				"has that name\n", strings.Join(names, ", "))
			return 1
		}
		WriteExplain(stdout, shown)
		WriteSearchExplain(stdout, searches)
	}
	if err != nil {
		return gen.Report(stderr, "repo", err)
	}
	return 0
}

// WriteExplain writes the explanations as nanostack gen repo -explain prints
// them: per interface a header line, then one line per method with how it
// resolves, and a note line when there is one.
func WriteExplain(w io.Writer, explain []Explanation) {
	for _, e := range explain {
		scope := ""
		if e.Scope != "" {
			scope = ", scope " + e.Scope
		}
		_, _ = fmt.Fprintf(w, "%s on *%s (table %s%s)\n", e.Iface, e.Impl, e.Table, scope)
		width := 0
		for _, m := range e.Methods {
			width = max(width, len(m.Name))
		}
		for _, m := range e.Methods {
			_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, m.Name, m.How)
			if m.Note != "" {
				_, _ = fmt.Fprintf(w, "  %-*s  note: %s\n", width, "", m.Note)
			}
		}
	}
}

// checkImpliesFlag checks -scope-implies as the project file is checked, and
// prints each problem; it reports whether the map is sound.
func checkImpliesFlag(opts Options, stderr io.Writer) bool {
	var ctxCols []string
	for col := range opts.ContextScopes {
		ctxCols = append(ctxCols, col)
	}
	problems := CheckScopeImplies("-scope-implies", opts.Scopes, ctxCols, opts.ScopeImplies)
	for _, pr := range problems {
		_, _ = fmt.Fprintf(stderr, "nanostack gen repo: %s\n  fix: %s\n", pr.Msg, pr.Fix)
	}
	return len(problems) == 0
}

// reviewNotes are what a review sees of a derived method: a skipped scope, a
// context scope read from a parameter, a reviewed fail-open choice.
func reviewNotes(r *resolution, m *derivedMethod) []string {
	var notes []string
	if m.unscoped {
		cols := make([]string, len(m.skipped))
		for i, f := range m.skipped {
			cols[i] = columnOf(r.b.table, f)
		}
		notes = append(notes, "UNSCOPED, no filter on "+strings.Join(cols, " or "))
	}
	for _, col := range m.cross {
		notes = append(notes, "CROSS-"+strings.ToUpper(strings.TrimSuffix(col, "_id"))+", "+col+" from a parameter")
	}
	if m.op != opDelete && !m.op.isWrite() && len(m.present) == 0 {
		var cols []string
		for _, t := range m.terms {
			if t.ctx != nil {
				cols = append(cols, t.ctx.col)
			}
		}
		if len(cols) > 0 {
			notes = append(notes, "ORG-WIDE (ctx scope fail-open read), a context without "+strings.Join(cols, " or ")+
				" reads the whole organisation")
		}
	}
	if len(m.failOpen) > 0 {
		cols := make([]string, len(m.failOpen))
		for i, c := range m.failOpen {
			cols[i] = c.col
		}
		notes = append(
			notes,
			"FAIL-OPEN (reviewed), a context without "+strings.Join(cols, " or ")+" reaches every value",
		)
	}
	return notes
}

// addFailOpen reads the -context-fail-open list into opts.
func addFailOpen(opts *Options, list string) {
	for col := range strings.SplitSeq(list, ",") {
		if col = strings.TrimSpace(col); col != "" {
			if opts.ContextFailOpen == nil {
				opts.ContextFailOpen = map[string]bool{}
			}
			opts.ContextFailOpen[col] = true
		}
	}
}
