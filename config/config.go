package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

const (
	EnvLocal      = "local"
	EnvProduction = "production"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	MySQL  MySQLConfig  `mapstructure:"mysql"`
	Redis  RedisConfig  `mapstructure:"redis"`
	Logger LoggerConfig `mapstructure:"logger"`
	Mail   MailConfig   `mapstructure:"mail"`
	Jwt    JwtConfig    `mapstructure:"jwt"`
	Cors   CorsConfig   `mapstructure:"cors"`
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		fmt.Printf("No .env file found: %v\n", err)
	}

	env := strings.TrimSpace(os.Getenv("APP_ENV"))
	if env == "" {
		env = EnvLocal
	}

	if err := validateEnv(env); err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName(env)
	v.SetConfigType("yaml")
	v.AddConfigPath("./environment")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s.yaml : %w", env, err)
	}

	if err := expandEnvVars(v); err != nil {
		return nil, fmt.Errorf("failed to expand env vars: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation error: %w", err)
	}

	return &cfg, nil
}

func expandEnvVars(v *viper.Viper) error {
	for _, key := range v.AllKeys() {
		value := v.GetString(key)
		if strings.Contains(value, "${") {
			expanded := os.ExpandEnv(value)
			v.Set(key, expanded)
		}
	}
	return nil
}

func (c *Config) validate() error {
	if c.Server.Port == 0 {
		return fmt.Errorf("server port must be specified")
	}
	if c.MySQL.Host == "" {
		return fmt.Errorf("mysql host must be specified")
	}
	if c.MySQL.Database == "" {
		return fmt.Errorf("mysql database must be specified")
	}
	if c.Redis.Host == "" {
		return fmt.Errorf("redis host must be specified")
	}
	if c.Jwt.Secret == "" {
		return fmt.Errorf("jwt secret must be specified")
	}
	if c.Cors.AllowedOrigins == nil {
		return fmt.Errorf("cors allowed origins must be specified")
	}
	return nil
}

func validateEnv(env string) error {
	allowed := []string{EnvLocal, EnvProduction}
	if ok := slices.Contains(allowed, env); ok {
		return nil
	}
	return fmt.Errorf("invalid APP_ENV: %s, allowed values are: %v", env, allowed)
}
