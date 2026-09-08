package worker

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

// Supervisor khởi động và quản lý vòng đời của nhiều Worker.
type Supervisor struct {
	workers []*Worker
	logger  *zap.Logger
	mu      sync.Mutex
}

func NewSupervisor(logger *zap.Logger) *Supervisor {
	return &Supervisor{logger: logger}
}

// Add thêm worker vào supervisor. Phải gọi trước Run.
func (s *Supervisor) Add(w *Worker) *Supervisor {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers = append(s.workers, w)
	return s
}

// Run khởi động tất cả worker đã đăng ký đồng thời.
// Block cho đến khi ctx bị cancel và tất cả worker đã dừng.
func (s *Supervisor) Run(ctx context.Context) {
	s.mu.Lock()
	workers := make([]*Worker, len(s.workers))
	copy(workers, s.workers)
	s.mu.Unlock()

	if len(workers) == 0 {
		s.logger.Warn("supervisor: no workers registered, nothing to run")
		return
	}

	s.logger.Info("supervisor: starting all workers",
		zap.Int("workerCount", len(workers)),
	)

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		w := w // capture
		go func() {
			defer wg.Done()
			w.Run(ctx)
		}()
	}

	wg.Wait()
	s.logger.Info("supervisor: all workers stopped")
}
