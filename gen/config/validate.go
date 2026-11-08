package genconfig

import (
	"errors"
	"fmt"
	"go/types"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var sizes = types.SizesFor("gc", "amd64")

// value is a default or rule argument parsed at generation time, so a bad one
// fails `nano gen config` instead of the service at start-up.
type value struct {
	i int64   // int and duration
	u uint64  // uint
	f float64 // float
	s string  // string, and the URL scheme for URLs
}

// validateSetting parses the validate tag into checks and proves that the
// default parses and passes them.
func validateSetting(s *setting) error {
	rules, order, err := parseRules(s.rawCheck)
	if err != nil {
		return err
	}
	for _, rule := range order {
		if ruleErr := checkRule(s, rule, rules[rule]); ruleErr != nil {
			return ruleErr
		}
	}
	if minArg, hasMin := rules["min"]; hasMin {
		if maxArg, hasMax := rules["max"]; hasMax {
			lo, _ := parseValue(s, minArg)
			hi, _ := parseValue(s, maxArg)
			if less(s.kind, hi, lo) {
				return fmt.Errorf("validate min=%s is greater than max=%s", minArg, maxArg)
			}
		}
	}
	s.checks = buildChecks(s, rules)
	return checkDefault(s, rules)
}

// parseRules splits "min=1,max=5" into rules, keeping their order.
func parseRules(raw string) (map[string]string, []string, error) {
	rules := map[string]string{}
	var order []string
	if strings.TrimSpace(raw) == "" {
		return rules, nil, nil
	}
	for part := range strings.SplitSeq(raw, ",") {
		rule, arg, _ := strings.Cut(strings.TrimSpace(part), "=")
		if _, dup := rules[rule]; dup {
			return nil, nil, fmt.Errorf("validate rule %s is set twice", rule)
		}
		rules[rule] = arg
		order = append(order, rule)
	}
	return rules, order, nil
}

// checkRule proves that one rule applies to the setting's type and that its
// arguments parse as that type.
func checkRule(s *setting, rule, arg string) error {
	switch rule {
	case "min", "max":
		if !slices.Contains([]kind{kindInt, kindUint, kindFloat, kindDuration}, s.kind) {
			return fmt.Errorf("validate rule %s applies to numbers and durations, not %s", rule, typeLabel(s))
		}
		if _, err := parseValue(s, arg); err != nil {
			return fmt.Errorf("validate %s=%s: %w", rule, arg, err)
		}
	case "oneof":
		if !slices.Contains([]kind{kindString, kindInt, kindUint}, s.kind) {
			return fmt.Errorf("validate rule oneof applies to strings and integers, not %s", typeLabel(s))
		}
		if len(strings.Fields(arg)) == 0 {
			return errors.New("validate oneof needs values separated by spaces, for example oneof=live test")
		}
		for o := range strings.FieldsSeq(arg) {
			if _, err := parseValue(s, o); err != nil {
				return fmt.Errorf("validate oneof value %q: %w", o, err)
			}
		}
	case "scheme":
		if s.kind != kindURL {
			return fmt.Errorf("validate rule scheme applies to *url.URL, not %s", typeLabel(s))
		}
		if len(strings.Fields(arg)) == 0 {
			return errors.New("validate scheme needs schemes separated by spaces, for example scheme=https http")
		}
	case "required":
		return fmt.Errorf("remove validate rule required: settings without a default are required already "+
			"(config:\"%s,optional\" makes one optional)", s.name)
	default:
		return fmt.Errorf("unknown validate rule %q; valid rules: min, max, oneof, scheme", rule)
	}
	return nil
}

// checkDefault proves that every default value parses and passes the rules.
func checkDefault(s *setting, rules map[string]string) error {
	if s.def == "" {
		return nil
	}
	defaults := []string{s.def}
	if s.list {
		defaults = splitList(s.def)
		if len(defaults) == 0 {
			return fmt.Errorf("default %q has no elements, so it counts as unset; use config:\"%s,optional\" "+
				"for a list that may stay empty", s.def, s.name)
		}
	}
	for _, d := range defaults {
		v, err := parseValue(s, d)
		if err != nil {
			return fmt.Errorf("default %q: %w", d, err)
		}
		if ruleErr := applyRules(s, rules, v, d); ruleErr != nil {
			return fmt.Errorf("default %q: %w", d, ruleErr)
		}
	}
	return nil
}

// parseValue parses raw with the same rules as the runtime parse helper for
// the setting's kind, or stricter. Text values (types implementing
// encoding.TextUnmarshaler, such as slog.Level) cannot be parsed without
// running their UnmarshalText, so they pass here and are checked when Bind
// runs; the reference doc and the package doc say so.
func parseValue(s *setting, raw string) (value, error) {
	bits := 64
	if s.elemType != nil {
		if basic, ok := s.elemType.Underlying().(*types.Basic); ok {
			bits = int(sizes.Sizeof(basic) * 8)
		}
	}
	switch s.kind {
	case kindInt:
		n, err := strconv.ParseInt(raw, 10, bits)
		if err != nil {
			return value{}, fmt.Errorf("must be an integer that fits %s", typeLabel(s))
		}
		return value{i: n}, nil
	case kindUint:
		n, err := strconv.ParseUint(raw, 10, bits)
		if err != nil {
			return value{}, fmt.Errorf("must be a non-negative integer that fits %s", typeLabel(s))
		}
		return value{u: n}, nil
	case kindFloat:
		// NaN and Inf parse in strconv but not in the runtime ParseFloat, and
		// have no Go literal, so they are rejected here too.
		f, err := strconv.ParseFloat(raw, bits)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return value{}, fmt.Errorf("must be a finite number that fits %s", typeLabel(s))
		}
		return value{f: f}, nil
	case kindDuration:
		d, err := time.ParseDuration(raw)
		if err != nil {
			return value{}, errors.New("must be a duration such as 300ms, 30s or 5m")
		}
		return value{i: int64(d)}, nil
	case kindBool:
		if _, err := strconv.ParseBool(raw); err != nil {
			return value{}, errors.New("must be true or false")
		}
		return value{}, nil
	case kindURL:
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return value{}, errors.New("must be an absolute URL such as https://example.com")
		}
		return value{s: u.Scheme}, nil
	case kindString, kindText, kindSecret:
		return value{s: raw}, nil
	}
	return value{}, fmt.Errorf("unsupported kind %d", s.kind)
}

func applyRules(s *setting, rules map[string]string, v value, raw string) error {
	if arg, ok := rules["min"]; ok {
		if lo, _ := parseValue(s, arg); less(s.kind, v, lo) {
			return fmt.Errorf("is below min=%s", arg)
		}
	}
	if arg, ok := rules["max"]; ok {
		if hi, _ := parseValue(s, arg); less(s.kind, hi, v) {
			return fmt.Errorf("is above max=%s", arg)
		}
	}
	if arg, ok := rules["oneof"]; ok {
		match := false
		for o := range strings.FieldsSeq(arg) {
			ov, _ := parseValue(s, o)
			match = match || ov == v
		}
		if !match {
			return fmt.Errorf("is not one of %s", arg)
		}
	}
	if arg, ok := rules["scheme"]; ok && !slices.Contains(strings.Fields(arg), v.s) {
		return fmt.Errorf("%s does not use scheme %s", raw, arg)
	}
	return nil
}

func less(k kind, a, b value) bool {
	switch k {
	case kindUint:
		return a.u < b.u
	case kindFloat:
		return a.f < b.f
	default:
		return a.i < b.i
	}
}

// buildChecks turns validated rules into checks with Go literal arguments.
// min and max together become one Between check, so the problem reads "must
// be between 1 and 365".
func buildChecks(s *setting, rules map[string]string) []check {
	var out []check
	minArg, hasMin := rules["min"]
	maxArg, hasMax := rules["max"]
	switch {
	case hasMin && hasMax:
		out = append(out, check{rule: "between", args: []string{literal(s, minArg), literal(s, maxArg)}})
	case hasMin:
		out = append(out, check{rule: "min", args: []string{literal(s, minArg)}})
	case hasMax:
		out = append(out, check{rule: "max", args: []string{literal(s, maxArg)}})
	}
	if arg, ok := rules["oneof"]; ok {
		c := check{rule: "oneof"}
		for o := range strings.FieldsSeq(arg) {
			c.args = append(c.args, literal(s, o))
		}
		out = append(out, c)
	}
	if arg, ok := rules["scheme"]; ok {
		c := check{rule: "scheme"}
		for o := range strings.FieldsSeq(arg) {
			c.args = append(c.args, strconv.Quote(o))
		}
		out = append(out, c)
	}
	return out
}

// literal renders an already validated value as Go source.
func literal(s *setting, raw string) string {
	v, _ := parseValue(s, raw)
	switch s.kind {
	case kindInt:
		return strconv.FormatInt(v.i, 10)
	case kindUint:
		return strconv.FormatUint(v.u, 10)
	case kindFloat:
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	case kindDuration:
		return durationLiteral(time.Duration(v.i))
	default:
		return strconv.Quote(raw)
	}
}

// durationLiteral renders a readable constant: 90s -> 90 * time.Second.
// The generated file always imports "time" under that name when it uses one.
func durationLiteral(d time.Duration) string {
	units := []struct {
		d    time.Duration
		name string
	}{
		{time.Hour, "time.Hour"}, {time.Minute, "time.Minute"}, {time.Second, "time.Second"},
		{time.Millisecond, "time.Millisecond"}, {time.Microsecond, "time.Microsecond"},
	}
	if d == 0 {
		return "0"
	}
	for _, u := range units {
		if d%u.d == 0 {
			return strconv.FormatInt(int64(d/u.d), 10) + " * " + u.name
		}
	}
	return "time.Duration(" + strconv.FormatInt(int64(d), 10) + ")"
}

// splitList mirrors the runtime: comma-separated, trimmed, empties dropped.
func splitList(raw string) []string {
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// typeLabel names the setting's type for messages and the reference doc.
func typeLabel(s *setting) string {
	label := types.TypeString(s.elemType, packageNameQualifier)
	switch s.kind {
	case kindSecret:
		label = "secret"
	case kindURL:
		label = "URL"
	case kindDuration:
		label = "duration"
	case kindString, kindBool, kindInt, kindUint, kindFloat:
		if _, named := s.elemType.(*types.Named); named {
			label += " (" + s.elemType.Underlying().String() + ")"
		}
	case kindText:
		// The type name says it all: slog.Level, netip.Addr.
	}
	if s.list {
		return "list of " + label
	}
	return label
}
