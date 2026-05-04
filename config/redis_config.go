package config

type RedisConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Password     string `mapstructure:"password"`
	Database     int    `mapstructure:"db"`
	PoolSize     int    `mapstructure:"poolSize"`
	MinIdleConns int    `mapstructure:"minIdleConns"`
}
