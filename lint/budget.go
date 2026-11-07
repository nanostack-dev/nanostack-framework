package lint

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// section is the overlay key that belongs to lintconfig. Config removes it,
// because golangci-lint refuses keys it does not know.
const section = "nanostack"

var errBudget = errors.New("exclusion budget")

// checkBudget removes the nanostack section from overlay and checks that the
// overlay's exclusion rules silence exactly nanostack.exclusion-budget
// linter-file pairs. The budget only goes down: a fix lowers it, and a new
// exclusion raises it in a reviewed diff.
func checkBudget(overlay *yaml.Node) error {
	if overlay.Kind != yaml.MappingNode {
		return nil // merge reports the shape
	}
	budget, set, err := takeSection(overlay)
	if err != nil {
		return err
	}
	count, err := countExclusions(overlay)
	if err != nil {
		return err
	}
	switch {
	case !set && count == 0:
		return nil
	case !set:
		return fmt.Errorf("%w: the overlay excludes %d linter-file pairs; add `%s: {exclusion-budget: %d}`",
			errBudget, count, section, count)
	case count > budget:
		return fmt.Errorf("%w: the overlay excludes %d linter-file pairs, over its budget of %d; "+
			"fix the new findings instead of excluding them, or raise %s.exclusion-budget in a reviewed change",
			errBudget, count, budget, section)
	case count < budget:
		return fmt.Errorf("%w: the overlay excludes %d linter-file pairs, under its budget of %d; "+
			"lower %s.exclusion-budget to %d so the fixed findings stay fixed",
			errBudget, count, budget, section, count)
	}
	return nil
}

// takeSection deletes the nanostack section from overlay and returns its budget.
func takeSection(overlay *yaml.Node) (budget int, set bool, err error) {
	at := lookup(overlay, section)
	if at < 0 {
		return 0, false, nil
	}
	node := overlay.Content[at]
	overlay.Content = append(overlay.Content[:at-1], overlay.Content[at+1:]...)
	if node.Kind != yaml.MappingNode {
		return 0, false, fmt.Errorf("%w: .%s (line %d) must be a mapping", errBudget, section, node.Line)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Value != "exclusion-budget" {
			return 0, false, fmt.Errorf(
				"%w: .%s.%s (line %d) is not a known key",
				errBudget,
				section,
				key.Value,
				key.Line,
			)
		}
		if value.ShortTag() != "!!int" || value.Decode(&budget) != nil || budget < 0 {
			return 0, false, fmt.Errorf("%w: .%s.exclusion-budget (line %d) must be a whole number, 0 or more",
				errBudget, section, value.Line)
		}
		set = true
	}
	return budget, set, nil
}

// countExclusions counts the linters named by the overlay's exclusion rules.
// Each rule must name its linters and one path, or one rule could hide
// any number of files and the count would mean nothing.
func countExclusions(overlay *yaml.Node) (int, error) {
	rules := find(overlay, "linters", "exclusions", "rules")
	if rules == nil || rules.Kind != yaml.SequenceNode {
		return 0, nil
	}
	count := 0
	for _, node := range rules.Content {
		var rule struct {
			Path    string   `yaml:"path"`
			Linters []string `yaml:"linters"`
		}
		if err := node.Decode(&rule); err != nil {
			return 0, fmt.Errorf("%w: exclusion rule (line %d): %w", errBudget, node.Line, err)
		}
		if len(rule.Linters) == 0 {
			return 0, fmt.Errorf("%w: exclusion rule (line %d) must name the linters it silences", errBudget, node.Line)
		}
		if rule.Path == "" {
			return 0, fmt.Errorf("%w: exclusion rule (line %d) must match one path, such as path: ^cmd/app/main\\.go$",
				errBudget, node.Line)
		}
		if strings.Contains(rule.Path, "|") {
			return 0, fmt.Errorf(
				"%w: exclusion rule (line %d) must match one path; write one rule per file instead of %q",
				errBudget,
				node.Line,
				rule.Path,
			)
		}
		count += len(rule.Linters)
	}
	return count, nil
}

// find follows keys through nested mappings and returns the node, or nil.
func find(node *yaml.Node, keys ...string) *yaml.Node {
	for _, key := range keys {
		if node.Kind != yaml.MappingNode {
			return nil
		}
		at := lookup(node, key)
		if at < 0 {
			return nil
		}
		node = node.Content[at]
	}
	return node
}
