// Package config is the small runtime behind generated configuration binders.
//
// A service declares its configuration as plain Go structs with struct tags.
// `nanostack gen config` reads those structs and writes a Bind function that has no
// reflection: one block per setting that looks the value up, parses it,
// validates it and records every issue. This package supplies what that
// generated code calls:
//
//   - Sources: where values come from (environment, YAML file layers, a
//     NANO_PROFILE profile of layered files and a secret store), with a fixed
//     precedence.
//   - Binder and Key: lookup, required/default handling and the issue list.
//   - Parse helpers (ParseInt, ParseDuration, ParseURL, ...) and checks
//     (Between, OneOf, Scheme) that the generated code composes.
//   - ParseSecret: binds a setting of type secret.Secret (package
//     core/secret), which never prints its value.
//   - Error and Issue: every issue found during one Bind, reported together.
//
// Precedence, highest first: environment variable, then the secret store
// (secret settings only), then YAML file layers (later files win), then the
// declared default. An empty value counts as unset, and the next source is
// tried; for a list, a value with no elements (env "," or YAML []) is empty.
//
// The default may come from another environment variable first
// (Key.DefaultEnv, the defaultenv struct tag): the setting reads it only
// when its own variable and the config files leave it unset, then falls back
// to the literal default. That is how one variable feeds two settings: one
// reads it as its env, the other as its default, and a config file can still
// set the second apart. A secret has no default, so it takes no DefaultEnv.
//
// Issues show the rejected raw value so an operator can fix it, with what
// looks like a credential replaced by secret.Redacted. They show no value for
// a secret setting, nor for a URL setting whose value does not parse, because
// a password in a malformed URL cannot be found reliably.
//
// Secret settings never read from YAML files: config files are committed, so a
// secret found in one is reported as an issue instead of being used.
package config
