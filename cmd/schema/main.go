package main

import (
	"fmt"
	"log"

	"github.com/dinhdev-nu/chat-platform-api/config"
	mysqlgorm "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, sqlDB, err := mysqlgorm.NewDB(cfg.MySQL, cfg.Server.IsProduction())
	if err != nil {
		log.Fatalf("connect to MySQL: %v", err)
	}
	defer sqlDB.Close()

	if err := mysqlgorm.RunMigrations(db); err != nil {
		log.Fatalf("run GORM migrations: %v", err)
	}

	fmt.Println("GORM migrations applied successfully")
}
