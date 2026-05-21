package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// ServerConfig holds HTTP server and API key settings.
type ServerConfig struct {
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`
	AdminAPIKey string `mapstructure:"admin_api_key"`
	ProxyAPIKey string `mapstructure:"proxy_api_key"`
}

// KiroConfig holds Kiro service region settings.
type KiroConfig struct {
	Region     string `mapstructure:"region"`
	AuthRegion string `mapstructure:"auth_region"`
	APIRegion  string `mapstructure:"api_region"`
}

// StorageConfig holds persistence settings.
type StorageConfig struct {
	SQLitePath          string `mapstructure:"sqlite_path"`
	CredentialsJSONPath string `mapstructure:"credentials_json_path"`
}

// LoadBalancerConfig holds load-balancer behaviour.
type LoadBalancerConfig struct {
	Strategy      string `mapstructure:"strategy"`
	StickySession bool   `mapstructure:"sticky_session"`
}

// QuotaConfig holds quota-cache settings.
type QuotaConfig struct {
	CacheTTLSeconds int `mapstructure:"cache_ttl_seconds"`
}

// FailoverConfig holds circuit-breaker / retry settings.
type FailoverConfig struct {
	BaseCooldownSec          int     `mapstructure:"base_cooldown_sec"`
	MaxBackoffMultiplier     int     `mapstructure:"max_backoff_multiplier"`
	ProbabilisticRetryChance float64 `mapstructure:"probabilistic_retry_chance"`
	MaxAttempts              int     `mapstructure:"max_attempts"`
}

// LoggingConfig holds logging settings.
type LoggingConfig struct {
	Level         string `mapstructure:"level"`
	Format        string `mapstructure:"format"`
	RequestLogFile string `mapstructure:"request_log_file"`
}

// ModelMappingRule maps incoming model names to one or more Kiro model names.
type ModelMappingRule struct {
	ID           string   `mapstructure:"id" json:"id"`
	Name         string   `mapstructure:"name" json:"name"`
	Enabled      bool     `mapstructure:"enabled" json:"enabled"`
	RuleType     string   `mapstructure:"rule_type" json:"rule_type"`
	SourceModel  string   `mapstructure:"source_model" json:"source_model"`
	TargetModels []string `mapstructure:"target_models" json:"target_models"`
	Weights      []int    `mapstructure:"weights" json:"weights"`
}

// Config is the top-level application configuration.
type Config struct {
	Server        ServerConfig       `mapstructure:"server"`
	Kiro          KiroConfig         `mapstructure:"kiro"`
	Storage       StorageConfig      `mapstructure:"storage"`
	LoadBalancer  LoadBalancerConfig `mapstructure:"load_balancer"`
	Quota         QuotaConfig        `mapstructure:"quota"`
	Failover      FailoverConfig     `mapstructure:"failover"`
	Logging       LoggingConfig      `mapstructure:"logging"`
	ModelMappings []ModelMappingRule `mapstructure:"model_mappings" json:"model_mappings"`
}

// setDefaults configures viper with the built-in default values.
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8765)
	v.SetDefault("kiro.region", "us-east-1")
	v.SetDefault("kiro.auth_region", "us-east-1")
	v.SetDefault("kiro.api_region", "us-east-1")
	v.SetDefault("storage.sqlite_path", ".data/kiro.db")
	v.SetDefault("load_balancer.strategy", "round_robin")
	v.SetDefault("load_balancer.sticky_session", true)
	v.SetDefault("quota.cache_ttl_seconds", 43200)
	v.SetDefault("failover.base_cooldown_sec", 60)
	v.SetDefault("failover.max_backoff_multiplier", 1440)
	v.SetDefault("failover.probabilistic_retry_chance", 0.10)
	v.SetDefault("failover.max_attempts", 9)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

// bindEnvs recursively walks the struct type and registers viper env bindings
// for every leaf field so that AutomaticEnv works with nested keys during Unmarshal.
func bindEnvs(v *viper.Viper, prefix string, t reflect.Type) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("mapstructure")
		if tag == "" {
			continue
		}
		name := tag
		if idx := strings.Index(tag, ","); idx != -1 {
			name = tag[:idx]
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		if field.Type.Kind() == reflect.Struct {
			bindEnvs(v, key, field.Type)
		} else {
			envName := v.GetEnvPrefix() + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
			_ = v.BindEnv(key, envName)
		}
	}
}

// Load reads configuration from defaults, an optional JSON file, environment
// variables (prefix KIRO_), and bound CLI flags.
func Load(path string) (*Config, error) {
	return LoadWithFlags(path, nil)
}

// LoadWithFlags is the testable variant of Load that accepts an optional
// *pflag.FlagSet so that CLI flag precedence can be verified without
// touching the global flag set.
func LoadWithFlags(path string, flags *pflag.FlagSet) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	if path != "" {
		v.SetConfigFile(path)
		// ReadInConfig returns an error only when the file is missing or unreadable.
		// We intentionally ignore "file not found" so that Load("") works with
		// defaults + env + flags only.
		_ = v.ReadInConfig()
	}

	v.SetEnvPrefix("KIRO")
	v.AutomaticEnv()
	bindEnvs(v, "", reflect.TypeOf(Config{}))

	if flags != nil {
		if err := v.BindPFlags(flags); err != nil {
			return nil, fmt.Errorf("bind flags: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

// Validate returns an error if required fields are missing.
func (c *Config) Validate() error {
	if c.Server.AdminAPIKey == "" {
		return fmt.Errorf("Server.AdminAPIKey is required")
	}
	if c.Server.ProxyAPIKey == "" {
		return fmt.Errorf("Server.ProxyAPIKey is required")
	}
	return nil
}
