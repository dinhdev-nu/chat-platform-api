package initialize

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/worker"
	"go.uber.org/zap"
)

type WorkerApplication struct {
	supervisor *worker.Supervisor
}

func NewWorkerApp() *WorkerApplication {
	cfg := LoadConfig()
	g.Config = cfg

	InitLogger()
	InitRedis()
	InitMailer()

	supervisor := worker.NewSupervisor(g.Logger)
	if err := registerEmailWorker(supervisor); err != nil {
		g.Logger.Fatal("register email worker failed", zap.Error(err))
	}

	return &WorkerApplication{
		supervisor: supervisor,
	}
}

func (app *WorkerApplication) Run() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		app.supervisor.Run(ctx)
	}()

	g.Logger.Info("standalone worker supervisor started")

	select {
	case <-ctx.Done():
		g.Logger.Info("worker shutting down...")
		stop()
		<-done
	case <-done:
	}

	app.closeConnections()
	g.Logger.Info("worker exited")
}

func (app *WorkerApplication) closeConnections() {
	if g.RedisClient == nil {
		return
	}

	if err := g.RedisClient.Close(); err != nil {
		g.Logger.Warn("failed to close Redis", zap.Error(err))
	}
}

func registerEmailWorker(supervisor *worker.Supervisor) error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
		g.Logger.Warn("failed to read hostname for worker consumer name", zap.Error(err))
	}

	emailStream := queue.GetStreamByJobType(queue.JobSendOTPEmail)
	emailGroup, ok := queue.GroupByStream[emailStream]
	if !ok || emailGroup == "" {
		return fmt.Errorf("missing consumer group for stream %s", emailStream)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := g.Stream.EnsureConsumerGroup(ctx, emailStream, emailGroup); err != nil {
		return fmt.Errorf("setup email consumer group: %w", err)
	}

	emailHandler := handler.NewSendEmailHandler(
		g.Mailer,
		g.Logger,
		g.Config.Mail.SenderName,
	)
	emailConsumer := fmt.Sprintf("worker-%s-%s-%d", emailStream, hostname, os.Getpid())
	emailWorker := worker.New(emailStream, emailGroup, emailConsumer, g.Stream, g.Logger)

	supervisor.Add(emailWorker.Register(emailHandler))
	return nil
}
