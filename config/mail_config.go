package config

type MailConfig struct {
	Host       string `mapstructure:"host"`
	Port       int    `mapstructure:"port"`
	From       string `mapstructure:"from"`
	Password   string `mapstructure:"password"`
	SenderName string `mapstructure:"senderName"`
}
