package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// SecretProvider looks up secret settings in a secret store (a mounted secrets
// directory, Vault, a cloud secret manager). The name is the setting's
// environment variable name, for example STRIPE_KEY, so one name identifies a
// secret everywhere.
//
// The value is a secret.Secret from the point the store returns it, so no
// provider or test handles it as a plain string; the Binder reveals it only
// to parse it. An unset Secret means the store has no value for name, as an
// empty value counts as unset for every source. err is reserved for a store
// that could not answer; Bind reports it as an issue for that setting.
type SecretProvider interface {
	LookupSecret(ctx context.Context, name string) (secret.Secret, error)
}

// Sources holds where setting values come from. Build it once at start-up with
// NewSources and pass it to the generated Bind.
type Sources struct {
	env     func(string) (string, bool)
	files   []fileLayer // increasing precedence: later layers win
	secrets SecretProvider
}

// fileLayer is one YAML file flattened to dotted setting paths.
type fileLayer struct {
	name   string
	values map[string]fileValue
	order  []string // keys in file order, so unknown-key issues are stable
}

type fileValue struct {
	scalar string
	list   []string
	isList bool
}

// Option adds one source to Sources.
type Option func(*Sources) error

// NewSources applies every option and reports all failures together.
func NewSources(opts ...Option) (*Sources, error) {
	s := &Sources{env: func(string) (string, bool) { return "", false }}
	var errs []error
	for _, opt := range opts {
		if err := opt(s); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("config sources: %w", errors.Join(errs...))
	}
	return s, nil
}

// Env reads environment variables of the process.
func Env() Option {
	return func(s *Sources) error {
		s.env = os.LookupEnv
		return nil
	}
}

// EnvMap reads environment variables from m instead of the process. Tests use
// it to bind without touching the real environment.
func EnvMap(m map[string]string) Option {
	return func(s *Sources) error {
		s.env = func(name string) (string, bool) {
			v, ok := m[name]
			return v, ok
		}
		return nil
	}
}

// File adds a YAML file layer that must exist. Later layers override earlier
// ones, so pass the base file first and a profile file after it.
func File(path string) Option {
	return func(s *Sources) error {
		//nolint:gosec // the path comes from start-up code, or from Profile, which accepts only a plain name
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		return s.addYAML(path, data)
	}
}

// OptionalFile adds a YAML file layer when the file exists and skips it when it
// does not. Any other read error is reported.
func OptionalFile(path string) Option {
	return func(s *Sources) error {
		data, err := os.ReadFile(filepath.Clean(path))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		return s.addYAML(path, data)
	}
}

// ProfileEnv is the environment variable that names the configuration profile,
// for example NANO_PROFILE=dev.
const ProfileEnv = "NANO_PROFILE"

// Profile adds the layered files of a profile: dir/base.yaml when it exists,
// then dir/<profile>.yaml, which must exist so that a misspelled profile fails
// start-up instead of running on base values. An empty profile adds only
// base.yaml. The profile is a plain name such as dev or prod.
//
//	config.NewSources(config.Profile("config", "dev"), config.Env())
func Profile(dir, profile string) Option {
	return func(s *Sources) error {
		if err := OptionalFile(filepath.Join(dir, "base.yaml"))(s); err != nil {
			return err
		}
		if profile == "" {
			return nil
		}
		if strings.ContainsAny(profile, `/\`) || profile == "." || profile == ".." || profile == "base" {
			return fmt.Errorf("profile %q must be a plain name such as dev or prod, other than base", profile)
		}
		return File(filepath.Join(dir, profile+".yaml"))(s)
	}
}

// ProfileFromEnv is Profile with the profile read from the NANO_PROFILE
// environment variable of the process.
//
//	config.NewSources(config.ProfileFromEnv("config"), config.Env())
func ProfileFromEnv(dir string) Option {
	return Profile(dir, os.Getenv(ProfileEnv))
}

// YAML adds a YAML layer from memory. name identifies the layer in issues.
func YAML(name string, data []byte) Option {
	return func(s *Sources) error { return s.addYAML(name, data) }
}

// Secrets sets the secret store consulted for secret settings.
func Secrets(p SecretProvider) Option {
	return func(s *Sources) error {
		s.secrets = p
		return nil
	}
}

func (s *Sources) addYAML(name string, data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	layer := fileLayer{name: name, values: map[string]fileValue{}}
	if len(doc.Content) > 0 {
		if err := layer.flatten("", doc.Content[0]); err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
	}
	s.files = append(s.files, layer)
	return nil
}

// flatten turns nested mappings into dotted paths. Scalars keep their source
// text, so "1e3" or "yes" reach the typed parser unchanged.
func (l *fileLayer) flatten(prefix string, n *yaml.Node) error {
	switch n.Kind {
	case yaml.MappingNode:
		return l.flattenMapping(prefix, n)
	case yaml.SequenceNode:
		return l.flattenList(prefix, n)
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			l.set(prefix, fileValue{})
		} else {
			l.set(prefix, fileValue{scalar: n.Value})
		}
	case yaml.AliasNode:
		return l.flatten(prefix, n.Alias)
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := l.flatten(prefix, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *fileLayer) flattenMapping(prefix string, n *yaml.Node) error {
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if prefix != "" {
			key = prefix + "." + key
		}
		if err := l.flatten(key, n.Content[i+1]); err != nil {
			return err
		}
	}
	return nil
}

func (l *fileLayer) flattenList(prefix string, n *yaml.Node) error {
	items := make([]string, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.ScalarNode {
			return fmt.Errorf("line %d: %s: lists may hold only plain values", item.Line, prefix)
		}
		items = append(items, item.Value)
	}
	l.set(prefix, fileValue{list: items, isList: true})
	return nil
}

func (l *fileLayer) set(key string, v fileValue) {
	if _, seen := l.values[key]; !seen {
		l.order = append(l.order, key)
	}
	l.values[key] = v
}

// SecretDir returns a SecretProvider that reads one file per secret from dir,
// named after the setting's environment variable (dir/STRIPE_KEY). This is the
// layout of Docker and Kubernetes secret mounts. One trailing newline is
// removed.
func SecretDir(dir string) SecretProvider { return secretDir(dir) }

type secretDir string

func (d secretDir) LookupSecret(_ context.Context, name string) (secret.Secret, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return secret.Secret{}, fmt.Errorf("secret name %q is not a file name", name)
	}
	data, err := os.ReadFile(filepath.Join(string(d), name))
	if errors.Is(err, fs.ErrNotExist) {
		return secret.Secret{}, nil
	}
	if err != nil {
		return secret.Secret{}, fmt.Errorf("read secret %s: %w", name, err)
	}
	v := strings.TrimSuffix(string(data), "\n")
	return secret.New(strings.TrimSuffix(v, "\r")), nil
}
