package initialize

import (
	"fmt"

	c "github.com/dinhdev-nu/chat-platform-api/config"
)

func LoadConfig() *c.Config {
	cfg, err := c.Load("local")
	if err != nil {
		panic(fmt.Errorf("failed to load config: %w", err))
	}
	return cfg
}
