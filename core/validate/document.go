package validate

import (
	"bytes"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// document indexes a JSON text as a tree of values: the kind of every value
// and the member names of every object. It is built once, on first use, in
// one pass over the text.
//
// The index is linear in the size of the text, whatever its shape. No JSON
// Pointer string is stored per value: a lookup walks the tree along the
// pointer's reference tokens. A scalar costs one byte in an array and one
// pointer in an object, and every scalar of a kind shares one node. (An index keyed by full pointer
// stores the parent's key again for every array element, so a long key
// over a wide array costs key length × element count.)
type document struct {
	data []byte

	once sync.Once
	root *node // nil when the text does not parse
}

// node is one value of the document.
type node struct {
	kind byte
	// names and values are the members of an object, in document order.
	names  []string
	values []*node
	// byName indexes the members of a large object, so that checking every
	// member of a map-typed object stays linear.
	byName map[string]*node
	// kinds holds the kind of every element of an array, one byte each, so
	// a wide array of scalars costs one byte per element. The objects and
	// arrays among the elements are in nested, at the indexes in nestedAt.
	kinds    []byte
	nested   []*node
	nestedAt []int
}

// indexedMembers is the member count from which an object gets a map; below
// it a lookup scans the names.
const indexedMembers = 16

// kindEmptyObject and kindEmptyArray mark an empty container in the kinds
// of an array, so it needs no entry in nested.
const (
	kindEmptyObject = 'o'
	kindEmptyArray  = 'a'
)

// Scalars carry nothing but their kind, so every scalar of a kind is one
// shared node. They are never mutated.
var (
	nullNode   = &node{kind: 'n'}
	falseNode  = &node{kind: 'f'}
	trueNode   = &node{kind: 't'}
	stringNode = &node{kind: '"'}
	numberNode = &node{kind: '0'}
	// An empty object or array has nothing to index either, so a wide
	// array of {} costs one byte per element, like an array of scalars.
	emptyObject = &node{kind: '{'}
	emptyArray  = &node{kind: '['}
)

func scalarNode(kind byte) *node {
	switch kind {
	case 'n':
		return nullNode
	case 'f':
		return falseNode
	case 't':
		return trueNode
	case '"':
		return stringNode
	}
	return numberNode
}

func newDocument(data []byte) *document {
	return &document{data: data}
}

// ok builds the index if needed and reports whether it is usable. A text
// that does not parse leaves the path without a document, so validators
// fall back to the Go value instead of reporting presence errors.
func (d *document) ok() bool {
	d.once.Do(d.index)
	return d.root != nil
}

func (d *document) index() {
	// A bytes.Buffer is parsed in place, without a copy of the text.
	dec := jsontext.NewDecoder(bytes.NewBuffer(d.data),
		jsontext.AllowDuplicateNames(true), // the strict decode already ran
		jsontext.AllowInvalidUTF8(true))
	root, err := parseValue(dec)
	if err != nil {
		return
	}
	d.root = root
}

// parseValue reads one value and everything nested in it.
func parseValue(dec *jsontext.Decoder) (*node, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	switch kind := byte(tok.Kind()); kind {
	case '{':
		return parseObject(dec)
	case '[':
		return parseArray(dec)
	default:
		return scalarNode(kind), nil
	}
}

// parseObject reads the members of an object whose '{' has been read, and
// its closing '}'.
func parseObject(dec *jsontext.Decoder) (*node, error) {
	if dec.PeekKind() == '}' {
		_, err := dec.ReadToken()
		return emptyObject, err
	}
	n := &node{kind: '{'}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		name := tok.String() // before the next read voids the token
		value, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		n.names = append(n.names, name)
		n.values = append(n.values, value)
	}
	if len(n.names) >= indexedMembers {
		n.byName = make(map[string]*node, len(n.names))
		for i, name := range n.names {
			if _, dup := n.byName[name]; !dup { // the first one, like the scan
				n.byName[name] = n.values[i]
			}
		}
	}
	_, err := dec.ReadToken()
	return n, err
}

// parseArray reads the elements of an array whose '[' has been read, and
// its closing ']'.
func parseArray(dec *jsontext.Decoder) (*node, error) {
	if dec.PeekKind() == ']' {
		_, err := dec.ReadToken()
		return emptyArray, err
	}
	n := &node{kind: '['}
	for i := 0; dec.PeekKind() != ']'; i++ {
		elem, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		switch elem {
		case emptyObject:
			n.kinds = append(n.kinds, kindEmptyObject)
			continue
		case emptyArray:
			n.kinds = append(n.kinds, kindEmptyArray)
			continue
		}
		n.kinds = append(n.kinds, elem.kind)
		if isContainer(elem.kind) {
			n.nested = append(n.nested, elem)
			n.nestedAt = append(n.nestedAt, i)
		}
	}
	_, err := dec.ReadToken()
	return n, err
}

// lookup returns the value at the JSON Pointer ptr, or nil when there is
// none. ptr is built with the same escaping as Path.
func (d *document) lookup(ptr string) *node {
	n := d.root
	for ptr != "" {
		if ptr[0] != '/' {
			return nil
		}
		ptr = ptr[1:]
		tok := ptr
		if i := strings.IndexByte(ptr, '/'); i >= 0 {
			tok, ptr = ptr[:i], ptr[i:]
		} else {
			ptr = ""
		}
		switch n.kind {
		case '{':
			n = n.member(UnescapeToken(tok))
		case '[':
			n = n.elem(tok)
		default:
			return nil
		}
		if n == nil {
			return nil
		}
	}
	return n
}

func (n *node) member(name string) *node {
	if n.byName != nil {
		return n.byName[name]
	}
	for i, have := range n.names {
		if have == name {
			return n.values[i]
		}
	}
	return nil
}

// elem returns the element at the reference token tok, which must be a
// canonical decimal index, as RFC 6901 requires.
func (n *node) elem(tok string) *node {
	i, err := strconv.Atoi(tok)
	if err != nil || i < 0 || i >= len(n.kinds) || strconv.Itoa(i) != tok {
		return nil
	}
	switch kind := n.kinds[i]; {
	case kind == kindEmptyObject:
		return emptyObject
	case kind == kindEmptyArray:
		return emptyArray
	case !isContainer(kind):
		return scalarNode(kind)
	}
	j, _ := slices.BinarySearch(n.nestedAt, i)
	return n.nested[j]
}

func isContainer(kind byte) bool { return kind == '{' || kind == '[' }

// UnescapeToken reverses EscapeToken: "~1" becomes "/", then "~0" becomes
// "~", as RFC 6901 section 4 requires.
func UnescapeToken(s string) string {
	if !strings.Contains(s, "~") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}
