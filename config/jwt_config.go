package config

type JwtConfig struct {
	Secret     string `mapstructure:"secret"`
	ExpireTime int    `mapstructure:"expireTime"`
}
