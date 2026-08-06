package config

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// placeholderPattern mirrors the expression Init compiles, so these tests
// exercise the same syntax the loader accepts in production.
var placeholderPattern = regexp.MustCompile(`\$\{([^}\s]+)\}`)

func TestParsePlaceholder(t *testing.T) {
	tests := []struct {
		name  string
		inner string
		want  placeholder
	}{
		{
			name:  "bare variable",
			inner: "TOKEN",
			want:  placeholder{varName: "TOKEN"},
		},
		{
			name:  "variable with default",
			inner: "TOKEN:fallback",
			want:  placeholder{varName: "TOKEN", defaultValue: "fallback"},
		},
		{
			name:  "default keeps every colon after the first",
			inner: "URL:https://example.test:8080",
			want:  placeholder{varName: "URL", defaultValue: "https://example.test:8080"},
		},
		{
			name:  "file variable",
			inner: "file:SECRET_FILE",
			want:  placeholder{varName: "SECRET_FILE", fromFile: true},
		},
		{
			name:  "file variable with default",
			inner: "file:SECRET_FILE:fallback",
			want:  placeholder{varName: "SECRET_FILE", defaultValue: "fallback", fromFile: true},
		},
		{
			name:  "trailing colon yields an empty default",
			inner: "TOKEN:",
			want:  placeholder{varName: "TOKEN"},
		},
		{
			name:  "file prefix is only stripped once",
			inner: "file:file:SECRET",
			want:  placeholder{varName: "file", defaultValue: "SECRET", fromFile: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parsePlaceholder(tt.inner))
		})
	}
}

func TestReplacePlaceholders(t *testing.T) {
	secretDir := t.TempDir()
	secretPath := filepath.Join(secretDir, "secret")
	require.NoError(t, os.WriteFile(secretPath, []byte("  file-value \n\n"), 0600))

	tests := []struct {
		name    string
		env     map[string]string
		data    string
		want    string
		wantErr string
	}{
		{
			name: "environment value replaces the placeholder",
			env:  map[string]string{"TOKEN": "abc"},
			data: "token: ${TOKEN}",
			want: "token: abc",
		},
		{
			name:    "unset variable without a default is reported missing",
			data:    "token: ${TOKEN}",
			wantErr: "missing required environment variables: [TOKEN]",
		},
		{
			name: "default applies when the variable is unset",
			data: "token: ${TOKEN:fallback}",
			want: "token: fallback",
		},
		{
			name: "environment value wins over the default",
			env:  map[string]string{"TOKEN": "abc"},
			data: "token: ${TOKEN:fallback}",
			want: "token: abc",
		},
		{
			name: "only the first colon separates name from default",
			data: "url: ${URL:https://example.test}",
			want: "url: https://example.test",
		},
		{
			name:    "an empty default is not a default",
			data:    "token: ${TOKEN:}",
			wantErr: "missing required environment variables: [TOKEN]",
		},
		{
			name: "file placeholder reads and trims the referenced file",
			env:  map[string]string{"SECRET_FILE": secretPath},
			data: "secret: ${file:SECRET_FILE}",
			want: "secret: file-value",
		},
		{
			name: "file placeholder default is used verbatim, not as a path",
			data: "secret: ${file:SECRET_FILE:inline-default}",
			want: "secret: inline-default",
		},
		{
			name:    "file placeholder without a default is reported missing",
			data:    "secret: ${file:SECRET_FILE}",
			wantErr: "missing required environment variables: [SECRET_FILE]",
		},
		{
			name:    "unreadable secret file is an error",
			env:     map[string]string{"SECRET_FILE": filepath.Join(secretDir, "absent")},
			data:    "secret: ${file:SECRET_FILE}",
			wantErr: "failed to read secret file for SECRET_FILE from path",
		},
		{
			name: "file placeholder honours a colon-bearing default",
			data: "secret: ${file:SECRET_FILE:a:b}",
			want: "secret: a:b",
		},
		{
			name: "every occurrence is replaced",
			env:  map[string]string{"TOKEN": "abc"},
			data: "a: ${TOKEN}\nb: ${TOKEN}",
			want: "a: abc\nb: abc",
		},
		{
			name:    "all missing variables are reported together",
			data:    "a: ${ONE}\nb: ${TWO}",
			wantErr: "missing required environment variables: [ONE TWO]",
		},
		{
			name: "text without placeholders is returned unchanged",
			data: "plain: value",
			want: "plain: value",
		},
		{
			name: "placeholders containing whitespace are left alone",
			data: "raw: ${NOT A VAR}",
			want: "raw: ${NOT A VAR}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			got, err := NewConfigLoader().replacePlaceholders(tt.data, placeholderPattern)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// A read failure and a missing variable can occur in the same document; the
// read failure is the more specific diagnosis and is the one reported.
func TestReplacePlaceholdersReportsReadFailureBeforeMissingVariable(t *testing.T) {
	t.Setenv("SECRET_FILE", filepath.Join(t.TempDir(), "absent"))

	_, err := NewConfigLoader().replacePlaceholders(
		"secret: ${file:SECRET_FILE}\ntoken: ${TOKEN}",
		placeholderPattern,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read secret file for SECRET_FILE")
	assert.NotContains(t, err.Error(), "missing required environment variables")
}
