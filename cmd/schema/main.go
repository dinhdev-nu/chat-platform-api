package main

import (
	"fmt"
	"log"

	"github.com/dinhdev-nu/chat-platform-api/config"
	mysqlgorm "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, sqlDB, err := mysqlgorm.NewDB(cfg.MySQL, cfg.Server.IsProduction())
	if err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close MySQL connection: %v", err)
		}
	}()

	if err := mysqlgorm.RunMigrations(db); err != nil {
		return fmt.Errorf("run GORM migrations: %w", err)
	}

	fmt.Println("GORM migrations applied successfully")
	return nil
}
