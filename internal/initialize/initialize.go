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
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/worker"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
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
	InitMailer()
	r := InitRouter()

	return &Application{
		Config: cfg,
		Router: r,
	}
}

func (app *Application) Run() {
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

// Triển khai sau khi hệ thốn đã sẵn sàng, đảm bảo
func InitWorker() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Tùy chọn: chạy worker inline trong cùng process với API server
	if os.Getenv("WORKER_MODE") == "inline" {
		startInlineWorkers(ctx)
		g.Logger.Info("inline workers started (WORKER_MODE=inline)")
	}
}

func startInlineWorkers(ctx context.Context) {
	hostname, _ := os.Hostname()

	emailHandler := handler.NewSendEmailHandler(
		g.Mailer,
		g.Logger,
		g.Config.Mail.SenderName,
	)

	sup := worker.NewSupervisor(g.Logger)

	emailStream := queue.GetStreamByJobType(queue.JobSendOTPEmail)
	emailGroup := queue.GroupByStream[emailStream]
	// Prefix "api-inline" để phân biệt với standalone worker consumer trong Redis dashboard
	emailConsumer := fmt.Sprintf("api-inline-%s-%s", emailStream, hostname)

	if err := g.Stream.EnsureConsumerGroup(ctx, emailStream, emailGroup); err != nil {
		g.Logger.Fatal("inline worker: setup consumer group failed", zap.Error(err))
	}

	emailWorker := worker.New(emailStream, emailGroup, emailConsumer, g.Stream, g.Logger)
	emailWorker.Register(emailHandler)
	sup.Add(emailWorker)

	go sup.Run(ctx)
}
