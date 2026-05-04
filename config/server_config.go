package config

type ServerConfig struct {
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	Mode            string `mapstructure:"mode"`
	ReadTimeout     int    `mapstructure:"readTimeout"`
	WriteTimeout    int    `mapstructure:"writeTimeout"`
	ShutdownTimeout int    `mapstructure:"shutdownTimeout"`
}

func (s *ServerConfig) IsProduction() bool {
	return s.Mode == "production"
}
