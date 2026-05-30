package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestAppConfig struct {
	Database struct {
		Password string `yaml:"password"`
	} `yaml:"database"`
	SecretKey string `yaml:"secret_key"`
}

func TestConfigLoaderFileFallback(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Test case: Fallback to _FILE path and trim whitespace/newlines
	secretFilePath := filepath.Join(tempDir, "db_password")
	err := os.WriteFile(secretFilePath, []byte("  my-super-secret-password \n\n"), 0600)
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "application.yaml")
	configYAML := `
app:
  database:
    password: ${DB_PASSWORD}
  secret_key: ${SECRET_KEY:default-key}
`
	err = os.WriteFile(configPath, []byte(configYAML), 0600)
	require.NoError(t, err)

	t.Setenv("DB_PASSWORD_FILE", secretFilePath)

	loader := NewConfigLoader()
	err = loader.Init(configPath, tempDir)
	require.NoError(t, err)

	var appConfig TestAppConfig
	err = loader.LoadConfig("app", &appConfig)
	require.NoError(t, err)
	assert.Equal(t, "my-super-secret-password", appConfig.Database.Password)
	assert.Equal(t, "default-key", appConfig.SecretKey)

	// 2. Test case: Direct environment variable has higher priority than _FILE
	t.Setenv("DB_PASSWORD", "direct-env-password")
	loader2 := NewConfigLoader()
	err = loader2.Init(configPath, tempDir)
	require.NoError(t, err)

	var appConfig2 TestAppConfig
	err = loader2.LoadConfig("app", &appConfig2)
	require.NoError(t, err)
	assert.Equal(t, "direct-env-password", appConfig2.Database.Password)

	// Clean direct env variable
	os.Unsetenv("DB_PASSWORD")
}

func TestConfigLoaderFileFallbackErrors(t *testing.T) {
	tempDir := t.TempDir()

	configPath := filepath.Join(tempDir, "application.yaml")
	configYAML := `
app:
  database:
    password: ${DB_PASSWORD}
  secret_key: ${SECRET_KEY}
`
	err := os.WriteFile(configPath, []byte(configYAML), 0600)
	require.NoError(t, err)

	// Test case: Missing required environment variable entirely (and no default value)
	loader := NewConfigLoader()
	err = loader.Init(configPath, tempDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required environment variables")

	// Test case: File path does not exist
	t.Setenv("DB_PASSWORD_FILE", "/nonexistent/path/to/secret")
	t.Setenv("SECRET_KEY", "some-key")
	loader2 := NewConfigLoader()
	err = loader2.Init(configPath, tempDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read secret file for DB_PASSWORD")
}
