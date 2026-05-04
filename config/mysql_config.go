package config

type MySQLConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Username     string `mapstructure:"username"`
	Password     string `mapstructure:"password"`
	Database     string `mapstructure:"database"`
	MaxIdleConns int    `mapstructure:"maxIdleConns"`
	MaxOpenConns int    `mapstructure:"maxOpenConns"`
	MaxLifetime  int    `mapstructure:"maxLifetime"`
}
