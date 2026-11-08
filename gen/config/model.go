package genconfig

import (
	"go/types"
)

// kind is how one value is parsed. Each kind maps to one parse helper in
// core/config.
type kind int

const (
	kindString kind = iota + 1
	kindBool
	kindInt
	kindUint
	kindFloat
	kindDuration
	kindURL
	kindSecret
	kindText
)

// model is the parsed root configuration type.
type model struct {
	pkgPath  string
	pkgName  string
	typeName string
	source   string // file that declares the root type, relative to Dir
	root     *group
	settings []*setting // depth-first, declaration order
}

// group is one struct level; it becomes a nested slog group.
type group struct {
	name     string // YAML key segment; empty for the root
	children []node
}

// node is either a nested group or a setting.
type node struct {
	group   *group
	setting *setting
}

type setting struct {
	path     string // billing.due_days
	env      string // BILLING_DUE_DAYS
	name     string // due_days, the last path segment
	goExpr   string // Billing.DueDays, appended to "c."
	typ      types.Type
	elemType types.Type // the scalar type; equals typ unless list
	kind     kind
	list     bool
	def      string
	optional bool
	checks   []check
	rawCheck string // validate tag as written, for the reference doc
	doc      string
	at       string // file:line for generator errors
}

func (s *setting) secret() bool { return s.kind == kindSecret }

func (s *setting) required() bool { return s.def == "" && !s.optional }

// check is one validate rule, already validated against the field type.
type check struct {
	rule string   // min, max, between, oneof, scheme
	args []string // Go literal expressions, rendered in generated code
}
