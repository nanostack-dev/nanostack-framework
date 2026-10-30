// Package lint ships the Nanostack golangci-lint standard: the base
// configuration, the golangci-lint version every service pins, and the merge
// that adds a service's own rules from its .golangci.local.yml.
package lint

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed base.golangci.yml
var base []byte

//go:embed golangci-lint-version
var version string

// Version is the golangci-lint release the standard is written for, such as
// "v2.15.0".
func Version() string {
	return strings.TrimSpace(version)
}

// Config merges overlay into the base configuration and returns the YAML for
// .golangci.yml, starting with header. Mappings merge key by key, lists gain the
// overlay items they do not already hold, and an overlay scalar replaces the
// base one. An empty overlay returns the base unchanged.
func Config(header string, overlay []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, fmt.Errorf("parse base configuration: %w", err)
	}
	var extra yaml.Node
	if err := yaml.Unmarshal(overlay, &extra); err != nil {
		return nil, fmt.Errorf("parse overlay: %w", err)
	}
	if len(extra.Content) > 0 {
		if err := merge(doc.Content[0], extra.Content[0], ""); err != nil {
			return nil, err
		}
	}
	doc.HeadComment = header

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("write configuration: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("write configuration: %w", err)
	}
	return out.Bytes(), nil
}

var errKindMismatch = errors.New("overlay value has a different shape than the base")

func merge(dst, src *yaml.Node, path string) error {
	if dst.Kind != src.Kind {
		return fmt.Errorf("%s (line %d): %w", path, src.Line, errKindMismatch)
	}
	switch dst.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(src.Content); i += 2 {
			key, value := src.Content[i], src.Content[i+1]
			at := lookup(dst, key.Value)
			if at < 0 {
				dst.Content = append(dst.Content, key, value)
				continue
			}
			if err := merge(dst.Content[at], value, path+"."+key.Value); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for _, item := range src.Content {
			if item.Kind != yaml.ScalarNode || !holds(dst, item.Value) {
				dst.Content = append(dst.Content, item)
			}
		}
	default:
		*dst = *src
	}
	return nil
}

// lookup returns the index of the value under key in a mapping, or -1.
func lookup(mapping *yaml.Node, key string) int {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

func holds(sequence *yaml.Node, value string) bool {
	for _, item := range sequence.Content {
		if item.Kind == yaml.ScalarNode && item.Value == value {
			return true
		}
	}
	return false
}
