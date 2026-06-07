package initialize

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/worker"
	"github.com/dinhdev-nu/chat-platform-api/internal/wire/provider"
	"go.uber.org/zap"
)

type WorkerApplication struct {
	supervisor *worker.Supervisor
}

type streamWorkerSpec struct {
	stream   string
	handlers []queue.Handler
}

func NewWorkerApp() *WorkerApplication {
	cfg := LoadConfig()
	g.Config = cfg

	InitLogger()
	InitMySQL()
	InitRedis()
	InitMailer()

	supervisor := worker.NewSupervisor(g.Logger)
	if err := registerWorkers(supervisor); err != nil {
		g.Logger.Fatal("register workers failed", zap.Error(err))
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
	if sqlDB := mysqlDB(); sqlDB != nil {
		if err := sqlDB.Close(); err != nil {
			g.Logger.Warn("failed to close MySQL", zap.Error(err))
		}
	}

	if g.RedisClient == nil {
		return
	}

	if err := g.RedisClient.Close(); err != nil {
		g.Logger.Warn("failed to close Redis", zap.Error(err))
	}
}

func mysqlDB() *sql.DB {
	if g.MySQLDB == nil {
		return nil
	}
	sqlDB, err := g.MySQLDB.DB()
	if err != nil {
		return nil
	}
	return sqlDB
}

func registerWorkers(supervisor *worker.Supervisor) error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
		g.Logger.Warn("failed to read hostname for worker consumer name", zap.Error(err))
	}

	roomRepo := provider.NewRoomRepository()
	messageRepo := provider.NewMessageRepository()
	userRepo := provider.NewUserRepository()
	tokenRepo := provider.NewUserTokenRepository()

	specs := []streamWorkerSpec{
		{
			stream: queue.GetStreamByJobType(queue.JobSendOTPEmail),
			handlers: []queue.Handler{
				handler.NewSendEmailHandler(g.Mailer, g.Logger, g.Config.Mail.SenderName),
			},
		},
		{
			stream: queue.GetStreamByJobType(queue.JobUpdateConversationLastActivity),
			handlers: []queue.Handler{
				handler.NewConversationLastActivityHandler(roomRepo, g.Logger),
				handler.NewConversationSystemMessageHandler(messageRepo, roomRepo, g.Logger),
			},
		},
		{
			stream: queue.GetStreamByJobType(queue.JobUpdateAuthTokenLastUsed),
			handlers: []queue.Handler{
				handler.NewAuthTokenLastUsedHandler(tokenRepo, g.Logger),
			},
		},
		{
			stream: queue.GetStreamByJobType(queue.JobUpdateUserLastSeen),
			handlers: []queue.Handler{
				handler.NewUserLastSeenHandler(userRepo, g.Logger),
			},
		},
	}

	for _, spec := range specs {
		if err := registerStreamWorker(supervisor, hostname, spec); err != nil {
			return err
		}
	}
	return nil
}

func registerStreamWorker(supervisor *worker.Supervisor, hostname string, spec streamWorkerSpec) error {
	group, ok := queue.GroupByStream[spec.stream]
	if !ok || group == "" {
		return fmt.Errorf("missing consumer group for stream %s", spec.stream)
	}
	if len(spec.handlers) == 0 {
		return fmt.Errorf("missing handlers for stream %s", spec.stream)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := g.Stream.EnsureConsumerGroup(ctx, spec.stream, group); err != nil {
		return fmt.Errorf("setup consumer group for %s: %w", spec.stream, err)
	}

	consumer := fmt.Sprintf("worker-%s-%s-%d", spec.stream, hostname, os.Getpid())
	w := worker.New(spec.stream, group, consumer, g.Stream, g.Logger)
	for _, h := range spec.handlers {
		w.Register(h)
	}
	supervisor.Add(w)
	return nil
}
