package config

type JwtConfig struct {
	Secret     string `mapstructure:"secret"`
	ExpireTime string `mapstructure:"expireTime"`
}
