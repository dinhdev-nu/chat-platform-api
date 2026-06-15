package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	iredis "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"go.uber.org/zap"
)

const (
	defaultBlock = 2 * time.Second
	defaultCount = int64(10)

	reclaimIdle     = 45 * time.Second
	reclaimInterval = 60 * time.Second

	backoffBase     = 500 * time.Millisecond
	promoteInterval = 500 * time.Millisecond
)

type Worker struct {
	stream   string
	group    string
	consumer string
	handlers map[string]queue.Handler
	store    *iredis.StreamStore
	logger   *zap.Logger
}

func New(stream, group, consumer string, store *iredis.StreamStore, logger *zap.Logger) *Worker {
	return &Worker{
		stream:   stream,
		group:    group,
		consumer: consumer,
		handlers: make(map[string]queue.Handler),
		store:    store,
		logger:   logger,
	}
}

func (w *Worker) Register(h queue.Handler) *Worker {
	w.handlers[h.Type()] = h
	return w
}

func (w *Worker) Run(ctx context.Context) {
	// Tạo consumer group nếu chưa tồn tại
	w.logger.Info("worker running",
		zap.String("stream", w.stream),
		zap.String("group", w.group),
		zap.String("consumer", w.consumer),
	)

	// Goroutine recover pending messages khi worker start
	go w.reclaimer(ctx)
	go w.promoter(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker stopped", zap.String("consumer", w.consumer))
			return
		default:
			w.processBatch(ctx)
		}
	}

}

func (w *Worker) processBatch(ctx context.Context) {
	msgs, err := w.store.ReadJobs(ctx, iredis.ConsumerOptions{
		Stream:   w.stream,
		Group:    w.group,
		Consumer: w.consumer,
		Count:    defaultCount,
		Block:    defaultBlock,
	})
	if err != nil {
		if ctx.Err() != nil {
			return // Shutdown signal, không log lỗi
		}
		w.logger.Error("read jobs failed", zap.String("stream", w.stream), zap.Error(err))

		time.Sleep(backoffBase) // backoff khi READ lỗi
		return
	}

	for _, msg := range msgs {
		w.processOne(ctx, msg)
	}
}

func (w *Worker) processOne(ctx context.Context, msg iredis.StreamMessage) {
	handler, ok := w.handlers[msg.Job.Type]
	if !ok {
		w.logger.Warn("no handler for job type", zap.String("type", msg.Job.Type), zap.String("job_id", msg.ID))
		// Ack để không bị block stream
		if err := w.store.AckJob(ctx, w.stream, w.group, msg.ID); err != nil {
			w.logger.Error("ack job failed for unknown type", zap.String("job_id", msg.ID), zap.Error(err))
		}
		return
	}

	// Timeout cho mỗi job để tránh bị block nếu handler gặp lỗi hoặc chạy quá lâu
	jobCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err := handler.Handle(jobCtx, msg.Job)
	if err == nil {
		// Thành công -> Ack
		if err := w.store.AckJob(ctx, w.stream, w.group, msg.ID); err != nil {
			w.logger.Error("ack succeeded-job failed — potential duplicate delivery",
				zap.String("id", msg.ID),
				zap.Error(err),
			)
		}
		return
	}

	// Xử lý lỗi
	w.logger.Error("job failed",
		zap.String("type", msg.Job.Type),
		zap.String("id", msg.ID),
		zap.Int("attempt", msg.Job.Attempt),
		zap.Error(err),
	)

	if msg.Job.Attempt >= queue.MaxRetries {
		// Hết retry -> dead-letter
		w.logger.Warn("job exceeded max retries, dead lettering", zap.String("type", msg.Job.Type), zap.String("id", msg.ID))
		if err := w.store.AckJob(ctx, w.stream, w.group, msg.ID); err != nil {
			w.logger.Error("ack before dead-letter failed",
				zap.String("id", msg.ID),
				zap.Error(err),
			)
		}
		if err := w.store.DeadLetter(ctx, msg, fmt.Sprintf("max retries exceeded: %v", err)); err != nil {
			w.logger.Error("dead letter write failed — job metadata lost",
				zap.String("id", msg.ID),
				zap.Error(err),
			)
		}
		return
	}

	// Còn retry -> tăng attempt và re-enqueue với exponential backoff
	backoff := backoffBase * time.Duration(1<<msg.Job.Attempt)
	w.logger.Info("job scheduled for retry",
		zap.String("type", msg.Job.Type),
		zap.String("id", msg.ID),
		zap.Int("nextAttempt", msg.Job.Attempt+1),
		zap.Duration("backoff", backoff),
	)

	if err := w.store.AckJob(ctx, w.stream, w.group, msg.ID); err != nil {
		w.logger.Error("ack before retry failed — skipping retry to avoid duplicate",
			zap.String("id", msg.ID),
			zap.Error(err),
		)
		return
	}

	if err := w.store.RetryJob(ctx, w.stream, msg, backoff); err != nil {
		w.logger.Error("retry job enqueue failed — job lost",
			zap.String("id", msg.ID),
			zap.Error(err),
		)
	}
}

func (w *Worker) promoter(ctx context.Context) {
	ticket := time.NewTicker(promoteInterval)
	defer ticket.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticket.C:
			n, err := w.store.PromoteDelayedJobs(ctx, w.stream)
			if err != nil {
				w.logger.Error("promote delayed failed", zap.String("stream", w.stream), zap.Error(err))
				continue
			}
			if n > 0 {
				w.logger.Info("promoted delayed jobs to main stream", zap.String("stream", w.stream), zap.Int64("count", n))
			}
		}
	}
}

// reclaimer: định kỳ reclaim pending messages crash
func (w *Worker) reclaimer(ctx context.Context) {
	ticket := time.NewTicker(reclaimInterval)
	defer ticket.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticket.C:
			msgs, err := w.store.ReclaimPending(ctx, w.stream, w.group, w.consumer, reclaimIdle)
			if err != nil {
				w.logger.Error("reclaim failed pending", zap.Error(err))
				continue
			}
			if len(msgs) > 0 {
				w.logger.Info("reclaimed pending messages", zap.Int("count", len(msgs)))
				for _, msg := range msgs {
					w.processOne(ctx, msg)
				}
			}

		}
	}

}
