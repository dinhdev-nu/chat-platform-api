package initialize

import (
	"fmt"

	"github.com/dinhdev-nu/chat-platform-api/global"
	g "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm"
)

func InitMySQL() {
	cfg := global.Config.MySQL
	isProd := global.Config.Server.IsProduction()

	db, sqlDB, err := g.NewDB(cfg, isProd)
	if err != nil {
		panic(fmt.Errorf("failed to connect to MySQL: %w", err))
	}

	if err := g.RunMigrations(db); err != nil {
		panic(fmt.Errorf("failed to run MySQL migrations: %w", err))
	}

	// Sqlc + Goose dùng chung connection pool của GORM
	// Goose Migrations sử dụng Makefile để chạy không cần Viết AutoMigrate ở đây nữa
	global.MySQLDB = db
	global.SqlDB = sqlDB
	fmt.Println("MySQL initialized successfully")
}
