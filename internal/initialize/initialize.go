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
	m "github.com/dinhdev-nu/chat-platform-api/internal/midleware"
	"github.com/gin-gonic/gin"
)

type Application struct {
	Config *c.Config
	Router *gin.Engine
}

func NewApp() *Application {
	cfg := LoadConfig()
	g.Config = cfg

	InitLogger()
	InitMySQL()
	InitRedis()
	r := InitRouter()

	r.Use(
		m.Logger(),
		gin.Recovery(),
		m.ErrorHandler(),
	)

	return &Application{
		Config: cfg,
		Router: r,
	}
}

func (app *Application) Run() {
	addr := fmt.Sprintf("%s:%d", app.Config.Server.Host, app.Config.Server.Port)

	srv := &http.Server{
		Addr:    addr,
		Handler: app.Router,
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	app.closeConnections()

	fmt.Println("Server exited.")
}

func (app *Application) closeConnections() {
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
