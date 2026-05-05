package main

import (
	"fmt"
	"log"

	"github.com/dinhdev-nu/chat-platform-api/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	dns := cfg.MySQL.BuildDSN()
	fmt.Print(dns)
}
