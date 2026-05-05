package gorm

import (
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/config"
	m "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewDB(cfg config.MySQLConfig, isProd bool) (*gorm.DB, error) {
	dsn := cfg.BuildDSN()

	logLevel := logger.Info
	if isProd {
		logLevel = logger.Error
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logLevel),
		PrepareStmt:                              true,  // Sử dụng prepared statements để tăng hiệu suất
		DisableForeignKeyConstraintWhenMigrating: false, // Giữ nguyên ràng buộc khóa ngoại khi tự động tạo bảng
		SkipDefaultTransaction:                   true,  // Tắt transaction mặc định để tăng hiệu suất
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open error: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gorm get sql db error: %w", err)
	}

	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.MaxLifetime) * time.Second)

	return db, nil
}

func RunMigrations(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&m.User{},
		&m.OAuthAccount{},
		&m.UserToken{},
	); err != nil {
		return fmt.Errorf("gorm auto migrate error: %w", err)
	}

	return nil
}
