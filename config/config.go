package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	MySQL  MySQLConfig  `mapstructure:"mysql"`
	Redis  RedisConfig  `mapstructure:"redis"`
	Logger LoggerConfig `mapstructure:"logger"`
	Mail   MailConfig   `mapstructure:"mail"`
	Jwt    JwtConfig    `mapstructure:"jwt"`
}

func Load(env string) (*Config, error) {

	v := viper.New()
	v.SetConfigName(env)
	v.SetConfigType("yaml")
	v.AddConfigPath("./environment")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file %s.yaml : %w", env, err)
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
	return nil
}
