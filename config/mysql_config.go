package config

import "fmt"

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

func (c MySQLConfig) BuildDSN() string {
	dsnFormat := "%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local"
	return fmt.Sprintf(dsnFormat, c.Username, c.Password, c.Host, c.Port, c.Database)
}
