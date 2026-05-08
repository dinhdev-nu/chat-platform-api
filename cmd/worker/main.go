package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dinhdev-nu/chat-platform-api/global"
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/worker"
	i "github.com/dinhdev-nu/chat-platform-api/internal/initialize"
	"go.uber.org/zap"
)

func main() {
	cfg := i.LoadConfig()
	g.Config = cfg

	i.InitLogger()
	i.InitRedis()
	i.InitMailer()

	hostname, _ := os.Hostname()
	pid := os.Getpid()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emailHandler := handler.NewSendEmailHandler(
		g.Mailer,
		g.Logger,
		g.Config.Mail.SenderName,
	)
	supervisor := worker.NewSupervisor(g.Logger)

	// --- Thêm stream
	// Email Stream
	emailStream := queue.GetStreamByJobType(queue.JobSendOTPEmail)
	emailGroup := queue.GroupByStream[emailStream]
	emailConsumer := fmt.Sprintf("worker-%s-%s-%d", emailStream, hostname, pid)

	if err := global.Stream.EnsureConsumerGroup(ctx, emailStream, emailGroup); err != nil {
		global.Logger.Fatal("setup email consumer group failed", zap.Error(err))
	}

	emailWorker := worker.New(emailStream, emailGroup, emailConsumer, global.Stream, global.Logger)
	emailWorker.Register(emailHandler)
	supervisor.Add(emailWorker)

	// ... Stream : có thể thêm nhiều stream và worker khác nếu cần

	global.Logger.Info("starting standalone worker supervisor",
		zap.String("hostname", hostname),
	)
	go supervisor.Run(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	global.Logger.Info("worker shutting down...")
	cancel()
}
