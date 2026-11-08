package config

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// Key describes one setting to the Binder. Generated code writes one literal
// Key per setting, so the path, environment variable and default of every
// setting are visible in the generated file.
type Key struct {
	// Path is the dotted setting path, also used as the YAML key.
	Path string
	// Env is the environment variable that sets the value.
	Env string
	// Default is the raw default value. Empty means no default.
	Default string
	// DefaultEnv names an environment variable that supplies the default:
	// when the setting finds no value in its own sources (Env, the secret
	// store, the config files), it takes the value of DefaultEnv, then
	// Default. So one variable can feed two settings: one reads it as Env,
	// the other as DefaultEnv, and a config file still overrides the second.
	// A secret cannot have one; Bind reports it as an issue.
	DefaultEnv string
	// Optional settings may stay unset; they keep the Go zero value. A setting
	// with neither a default nor Optional is required.
	Optional bool
	// Secret settings read env and the secret store, never YAML files, and
	// their values never appear in issues.
	Secret bool

	elem int // 1-based list element index for issues; 0 for the whole value
}

// Elem returns the key of list element i, so a check on one element reports
// a path such as billing.hosts[2].
func (k Key) Elem(i int) Key {
	k.elem = i + 1
	return k
}

func (k Key) path() string {
	if k.elem == 0 {
		return k.Path
	}
	return k.Path + "[" + strconv.Itoa(k.elem-1) + "]"
}

// Binder resolves settings against Sources and accumulates issues. Generated
// Bind functions create one, bind every setting, then return Err.
type Binder struct {
	ctx     context.Context //nolint:containedctx // lives for one Bind call only
	src     *Sources
	used    map[string]bool
	origins map[string]string
	errs    Error
}

// NewBinder starts one Bind run.
func NewBinder(ctx context.Context, src *Sources) *Binder {
	if src == nil {
		src = &Sources{env: func(string) (string, bool) { return "", false }}
	}
	return &Binder{ctx: ctx, src: src, used: map[string]bool{}, origins: map[string]string{}}
}

// Err reports every issue found, including YAML keys that no setting reads,
// or nil when the configuration is valid.
//
// Err does not change the Binder, so calling it again reports the same
// issues.
func (b *Binder) Err() error {
	issues := slices.Clone(b.errs.Issues)
	for _, layer := range b.src.files {
		for _, key := range layer.order {
			if !b.used[key] {
				issues = append(issues, Issue{
					Path:    key,
					Origin:  "file " + layer.name,
					Message: "unknown setting; fix the spelling or remove it",
				})
			}
		}
	}
	if len(issues) == 0 {
		return nil
	}
	return &Error{Issues: issues}
}

// AddIssue records an issue for k. Custom checks use it; message is used
// verbatim, for example "must be a valid email address".
func (b *Binder) AddIssue(k Key, message string) {
	b.errs.Issues = append(b.errs.Issues, Issue{
		Path:    k.path(),
		Env:     k.Env,
		Origin:  b.origins[k.Path],
		Message: message,
	})
}

// invalid records a value that failed a check. The raw value is shown unless
// the setting is secret, and what looks like a credential in it is replaced
// first (see secret.RedactText). message must not repeat the raw value: it
// is shown as is.
func (b *Binder) invalid(k Key, raw, message string) {
	p := Issue{Path: k.path(), Env: k.Env, Origin: b.origins[k.Path], Message: message}
	if !k.Secret {
		p.Value = secret.RedactText(raw)
	}
	b.errs.Issues = append(b.errs.Issues, p)
}

// unparsed records a value that parse rejected with err. It is invalid,
// except that a hiddenValueError shows no value at all.
func (b *Binder) unparsed(k Key, raw string, err error) {
	if _, hidden := errors.AsType[*hiddenValueError](err); hidden {
		raw = ""
	}
	b.invalid(k, raw, err.Error())
}

// resolved is a raw value and where it came from.
type resolved struct {
	raw    string
	list   []string
	isList bool
}

// lookup applies the precedence rules. It reports a missing required setting
// and returns false for any setting without a usable value. DefaultEnv is
// read after the config files, as the first half of the default, and never
// for a secret.
//
// An empty value counts as unset and the next source is tried. For a list,
// empty means no elements: env "," and YAML [] are unset, so a required list
// can never bind with zero elements.
func (b *Binder) lookup(k Key, list bool) (resolved, bool) {
	b.used[k.Path] = true
	if k.Secret {
		b.rejectSecretInFiles(k)
		if k.DefaultEnv != "" {
			b.AddIssue(k, "is a secret and cannot default to env "+k.DefaultEnv+
				"; remove DefaultEnv and set env "+k.Env+" or the secret store")
		}
	}
	if v, ok := b.src.env(k.Env); ok && !isEmpty(v, list) {
		b.origins[k.Path] = "env " + k.Env
		return resolved{raw: v}, true
	}
	if k.Secret {
		return b.lookupSecret(k, list)
	}
	for _, layer := range slices.Backward(b.src.files) {
		v, ok := layer.values[k.Path]
		if !ok || v.isList && len(v.list) == 0 || !v.isList && isEmpty(v.scalar, list) {
			continue
		}
		b.origins[k.Path] = "file " + layer.name
		return resolved{raw: v.scalar, list: v.list, isList: v.isList}, true
	}
	if k.DefaultEnv != "" {
		if v, ok := b.src.env(k.DefaultEnv); ok && !isEmpty(v, list) {
			b.origins[k.Path] = "env " + k.DefaultEnv + " (the default)"
			return resolved{raw: v}, true
		}
		return b.fallback(k, list, "set env "+k.Env+", "+k.Path+" in a config file, or env "+k.DefaultEnv)
	}
	return b.fallback(k, list, "set env "+k.Env+" or "+k.Path+" in a config file")
}

// isEmpty reports whether raw counts as unset: empty, or for a list, no
// elements once split.
func isEmpty(raw string, list bool) bool {
	if list {
		return len(splitList(raw)) == 0
	}
	return raw == ""
}

// rejectSecretInFiles reports a secret found in a config file, whatever its
// final source: the leak is in the committed file, not in the value used.
func (b *Binder) rejectSecretInFiles(k Key) {
	for _, layer := range b.src.files {
		if _, ok := layer.values[k.Path]; ok {
			b.errs.Issues = append(b.errs.Issues, Issue{
				Path:    k.Path,
				Env:     k.Env,
				Origin:  "file " + layer.name,
				Message: "is a secret and must not be in a config file; remove it and set env " + k.Env + " or the secret store",
			})
		}
	}
}

func (b *Binder) lookupSecret(k Key, list bool) (resolved, bool) {
	if b.src.secrets != nil {
		v, err := b.src.secrets.LookupSecret(b.ctx, k.Env)
		if err != nil {
			b.AddIssue(k, "secret store lookup failed: "+err.Error())
			return resolved{}, false
		}
		if raw := v.Reveal(); !isEmpty(raw, list) {
			b.origins[k.Path] = "secret store"
			return resolved{raw: raw}, true
		}
	}
	return b.fallback(k, list, "set env "+k.Env+" or the secret store entry "+k.Env)
}

func (b *Binder) fallback(k Key, list bool, hint string) (resolved, bool) {
	if !isEmpty(k.Default, list) {
		b.origins[k.Path] = "default"
		return resolved{raw: k.Default}, true
	}
	if !k.Optional {
		b.AddIssue(k, "is required; "+hint)
	}
	return resolved{}, false
}

// Scalar binds a single value. It returns false when the setting is unset or
// invalid; the issue is already recorded.
func Scalar[T any](b *Binder, k Key, parse func(string) (T, error)) (T, bool) {
	var zero T
	v, ok := b.lookup(k, false)
	if !ok {
		return zero, false
	}
	if v.isList {
		b.invalid(k, strings.Join(v.list, ","), "must be a single value, not a list")
		return zero, false
	}
	t, err := parse(v.raw)
	if err != nil {
		b.unparsed(k, v.raw, err)
		return zero, false
	}
	return t, true
}

// List binds a list. Environment variables and defaults separate elements with
// commas; YAML files use a YAML list (a comma-separated string also works).
// Every bad element is reported with its index. A value with no elements, such
// as env "," or YAML [], counts as unset like any empty value.
func List[T any](b *Binder, k Key, parse func(string) (T, error)) ([]T, bool) {
	v, ok := b.lookup(k, true)
	if !ok {
		return nil, false
	}
	items := v.list
	if !v.isList {
		items = splitList(v.raw)
	}
	out := make([]T, 0, len(items))
	valid := true
	for i, item := range items {
		t, err := parse(item)
		if err != nil {
			b.unparsed(k.Elem(i), item, err)
			valid = false
			continue
		}
		out = append(out, t)
	}
	if !valid {
		return nil, false
	}
	return out, true
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
