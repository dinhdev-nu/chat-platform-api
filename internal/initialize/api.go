package initialize

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	c "github.com/dinhdev-nu/chat-platform-api/config"
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/gin-gonic/gin"
)

type APIApplication struct {
	Config *c.Config
	Router *gin.Engine
}

func NewAPIApp() *APIApplication {
	cfg := LoadConfig()
	g.Config = cfg

	InitLogger()
	InitMySQL()
	InitRedis()
	r := InitRouter()

	return &APIApplication{
		Config: cfg,
		Router: r,
	}
}

func (app *APIApplication) Run() {
	addr := fmt.Sprintf("%s:%d", app.Config.Server.Host, app.Config.Server.Port)
	readTimeout := time.Duration(app.Config.Server.ReadTimeout) * time.Second
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}
	writeTimeout := time.Duration(app.Config.Server.WriteTimeout) * time.Second
	if writeTimeout <= 0 {
		writeTimeout = 30 * time.Second
	}
	shutdownTimeout := time.Duration(app.Config.Server.ShutdownTimeout) * time.Second
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Router,
		ReadHeaderTimeout: readTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	go func() {
		fmt.Printf("Starting server on %s in %s mode...\n", addr, app.Config.Server.Mode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(fmt.Errorf("failed to start server: %w", err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	app.closeConnections()

	fmt.Println("Server exited.")
}

func (app *APIApplication) closeConnections() {
	if sqlDB, err := g.MySQLDB.DB(); err == nil {
		if err := sqlDB.Close(); err != nil {
			fmt.Printf("Failed to close MySQL: %v\n", err)
		} else {
			fmt.Println("MySQL closed.")
		}
	}

	if err := g.RedisClient.Close(); err != nil {
		fmt.Printf("Failed to close Redis: %v\n", err)
	} else {
		fmt.Println("Redis closed.")
	}
}
