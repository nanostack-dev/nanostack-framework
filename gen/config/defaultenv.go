package genconfig

import (
	"go/types"
	"reflect"
)

// defaultEnv checks the defaultenv tag of s on its own; checkDefaultEnvs
// checks it against the other settings once all are known.
func (l *loader) defaultEnv(f *types.Var, tag reflect.StructTag, s *setting) bool {
	_, set := tag.Lookup(tagDefaultEnv)
	switch {
	case !set:
		return true
	case !envPattern.MatchString(s.defEnv):
		l.problemf(s.at, "write the name in upper snake case, for example defaultenv:\"ENVIRONMENT\"",
			"%s: defaultenv %q must be upper snake case", f.Name(), s.defEnv)
	case s.defEnv == s.env:
		l.problemf(s.at, "remove defaultenv, or name another variable",
			"%s: defaultenv %s is the setting's own env", f.Name(), s.defEnv)
	case s.secret():
		l.problemf(s.at, "remove defaultenv, and set the secret under its own env or in the secret store",
			"%s: secrets cannot have a default, not even from env %s", f.Name(), s.defEnv)
	default:
		return true
	}
	return false
}

// checkDefaultEnvs rejects a defaultenv that is the env of a secret setting:
// the setting is not secret (secrets take no defaultenv), so Bind issues and
// LogValue would print the secret.
func (l *loader) checkDefaultEnvs() {
	for _, s := range l.model.settings {
		if owner, ok := l.envs[s.defEnv]; ok && s.defEnv != "" && owner.secret() {
			l.problemf(s.at, "remove defaultenv; a setting that is not secret would print the secret",
				"%s: defaultenv %s is the env of secret setting %s", lastSegment(s.goExpr), s.defEnv, owner.path)
		}
	}
}
