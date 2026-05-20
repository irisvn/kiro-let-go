package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 8765, cfg.Server.Port)
	assert.Equal(t, "us-east-1", cfg.Kiro.Region)
	assert.Equal(t, "us-east-1", cfg.Kiro.AuthRegion)
	assert.Equal(t, "us-east-1", cfg.Kiro.APIRegion)
	assert.Equal(t, ".data/kiro.db", cfg.Storage.SQLitePath)
	assert.Equal(t, "round_robin", cfg.LoadBalancer.Strategy)
	assert.True(t, cfg.LoadBalancer.StickySession)
	assert.Equal(t, 43200, cfg.Quota.CacheTTLSeconds)
	assert.Equal(t, 60, cfg.Failover.BaseCooldownSec)
	assert.Equal(t, 1440, cfg.Failover.MaxBackoffMultiplier)
	assert.InDelta(t, 0.10, cfg.Failover.ProbabilisticRetryChance, 0.001)
	assert.Equal(t, 9, cfg.Failover.MaxAttempts)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestValidate_MissingAdminKey(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Host:        "0.0.0.0",
			Port:        8765,
			AdminAPIKey: "",
			ProxyAPIKey: "proxy-key",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AdminAPIKey")
}

func TestValidate_MissingProxyKey(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Host:        "0.0.0.0",
			Port:        8765,
			AdminAPIKey: "admin-key",
			ProxyAPIKey: "",
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ProxyAPIKey")
}

func TestValidate_OK(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Host:        "0.0.0.0",
			Port:        8765,
			AdminAPIKey: "admin-key",
			ProxyAPIKey: "proxy-key",
		},
	}
	require.NoError(t, cfg.Validate())
}

func TestLoad_FileOverridesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	content := `{
		"server": {
			"host": "127.0.0.1",
			"port": 9999,
			"admin_api_key": "file-admin",
			"proxy_api_key": "file-proxy"
		},
		"kiro": {
			"region": "eu-west-1"
		},
		"logging": {
			"level": "debug"
		}
	}`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 9999, cfg.Server.Port)
	assert.Equal(t, "file-admin", cfg.Server.AdminAPIKey)
	assert.Equal(t, "file-proxy", cfg.Server.ProxyAPIKey)
	assert.Equal(t, "eu-west-1", cfg.Kiro.Region)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "us-east-1", cfg.Kiro.AuthRegion)
	assert.Equal(t, 9999, cfg.Server.Port)
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	content := `{
		"server": {
			"port": 1111,
			"admin_api_key": "file-admin",
			"proxy_api_key": "file-proxy"
		}
	}`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	t.Setenv("KIRO_SERVER_PORT", "9999")

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, 9999, cfg.Server.Port)
	assert.Equal(t, "file-admin", cfg.Server.AdminAPIKey)
}

func TestLoad_FlagsOverrideEnv(t *testing.T) {
	t.Setenv("KIRO_SERVER_PORT", "8888")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("server.port", 7777, "server port")
	require.NoError(t, flags.Set("server.port", "7777"))

	cfg, err := LoadWithFlags("", flags)
	require.NoError(t, err)

	assert.Equal(t, 7777, cfg.Server.Port)
}

func TestLoad_EnvVarKeys(t *testing.T) {
	t.Setenv("KIRO_SERVER_ADMIN_API_KEY", "env-admin")
	t.Setenv("KIRO_SERVER_PROXY_API_KEY", "env-proxy")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, "env-admin", cfg.Server.AdminAPIKey)
	assert.Equal(t, "env-proxy", cfg.Server.ProxyAPIKey)
}

func TestLoad_AllNestedDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)

	assert.Empty(t, cfg.Server.AdminAPIKey)
	assert.Empty(t, cfg.Server.ProxyAPIKey)
	assert.Empty(t, cfg.Storage.CredentialsJSONPath)
	assert.Equal(t, "us-east-1", cfg.Kiro.AuthRegion)
	assert.Equal(t, "us-east-1", cfg.Kiro.APIRegion)
}
