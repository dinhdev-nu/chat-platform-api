package config

type LoggerConfig struct {
	Level      string `mapstructure:"level"`
	Dir        string `mapstructure:"dir"`
	MaxSizeMB  int    `mapstructure:"maxSizeMB"`
	MaxBackups int    `mapstructure:"maxBackups"`
	MaxAgeDays int    `mapstructure:"maxAgeDays"`
	Compress   bool   `mapstructure:"compress"`
}
