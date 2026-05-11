package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	goredis "github.com/redis/go-redis/v9"
)

const (
	delayedSuffix    = ":delayed"
	deadLetterSuffix = ":dead"
)

type StreamStore struct {
	client *goredis.Client
}

func NewStreamStore(client *goredis.Client) *StreamStore {
	return &StreamStore{client: client}
}

// Producer: Gửi job vào stream
func (s *StreamStore) EnqueueJob(ctx context.Context, jobType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("enqueue marshal payload: %w", err)
	}

	job := queue.Job{
		Type:      jobType,
		Payload:   raw,
		Attempt:   0,
		CreatedAt: time.Now(),
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("enqueue marshal job: %w", err)
	}

	// Sử dụng XADD để thêm job vào stream, với MAXLEN để giới hạn độ dài stream
	stream := queue.GetStreamByJobType(jobType)
	return s.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		MaxLen: queue.GetMaxLenByStream(stream),
		Approx: true,
		Values: map[string]any{"job": string(data)},
	}).Err()
}

// Consumer Group setup
func (s *StreamStore) EnsureConsumerGroup(ctx context.Context, stream, group string) error {
	err := s.client.XGroupCreateMkStream(ctx, stream, group, "0").Err() // "O" để tạo consumer group từ đầu stream , "$" để tạo từ cuối stream
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("ensure consumer group [%s/%s]: %w", stream, group, err)
	}
	return nil
}

// Consumer: Đọc job từ stream, xử lý và ack
type ConsumerOptions struct {
	Stream   string
	Group    string
	Consumer string
	Count    int64
	Block    time.Duration
}
type StreamMessage struct {
	ID  string
	Job queue.Job
}

func (s *StreamStore) ReadJobs(ctx context.Context, opts ConsumerOptions) ([]StreamMessage, error) {
	result, err := s.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    opts.Group,
		Consumer: opts.Consumer,
		Streams:  []string{opts.Stream, ">"}, // ">" để đọc các message mới chưa được claim
		Count:    opts.Count,
		Block:    opts.Block,
	}).Result()
	if err == goredis.Nil {
		return nil, nil // Không có message nào
	}
	if err != nil {
		return nil, fmt.Errorf("read job: %w", err)
	}

	msgs := make([]StreamMessage, 0)
	for _, stream := range result {
		for _, msg := range stream.Messages {
			raw, ok := msg.Values["job"].(string)
			if !ok {
				continue // Bỏ qua nếu không có field "job"
			}

			var job queue.Job
			if err := json.Unmarshal([]byte(raw), &job); err != nil {
				continue // Bỏ qua nếu không parse được job
			}

			job.ID = msg.ID
			msgs = append(msgs, StreamMessage{ID: msg.ID, Job: job})
		}
	}

	return msgs, nil
}

// Xóa job
func (s *StreamStore) AckJob(ctx context.Context, stream, group, id string) error {
	if err := s.client.XAck(ctx, stream, group, id).Err(); err != nil {
		return fmt.Errorf("ack job [%s]: %w", id, err)
	}
	return nil
}

// Retry Job: tăng attempt và re-enqueue - dùng khi sử lý job thất bại
func (s *StreamStore) RetryJob(ctx context.Context, stream string, msg StreamMessage, backoff time.Duration) error {
	msg.Job.Attempt++
	msg.Job.RetryAt = time.Now().Add(backoff)

	data, err := json.Marshal(msg.Job)
	if err != nil {
		return fmt.Errorf("retry job marshal: %w", err)
	}

	delayedKey := stream + delayedSuffix
	score := float64(msg.Job.RetryAt.UnixMilli())

	// Sử dụng ZADD để thêm job vào sorted set delayed, với score là thời điểm retry
	if err := s.client.ZAdd(ctx, delayedKey, goredis.Z{
		Score:  score,
		Member: string(data),
	}).Err(); err != nil {
		return fmt.Errorf("retry job zadd: %w", err)
	}

	return nil
}

// Promote: Đưa job từ delayed stream về main stream khi đến thời điểm retry
var promoteScript = goredis.NewScript(`
local delayed = KEYS[1]
local stream  = KEYS[2]
local now     = ARGV[1]
local maxLen  = ARGV[2]

local jobs = redis.call('ZRANGEBYSCORE', delayed, 0, now, 'LIMIT', 0, 100)
if #jobs == 0 then return 0 end

for _, job in ipairs(jobs) do
    redis.call('XADD', stream, 'MAXLEN', '~', maxLen, '*', 'job', job)
    redis.call('ZREM', delayed, job)
end
return #jobs
`)

func (s *StreamStore) PromoteDelayedJobs(ctx context.Context, stream string) (int64, error) {
	delayedKey := stream + delayedSuffix
	nowMs := fmt.Sprintf("%d", time.Now().UnixMilli())
	maxLen := fmt.Sprintf("%d", queue.GetMaxLenByStream(stream))

	n, err := promoteScript.Run(
		ctx, s.client,
		[]string{delayedKey, stream},
		nowMs, maxLen,
	).Int64()
	if err != nil && err != goredis.Nil {
		return 0, fmt.Errorf("promote delayed jobs [%s] : %w", stream, err)
	}
	return n, nil
}

// Dead-letter queue: Nếu job đã retry quá số lần cho phép, sẽ được chuyển vào stream dead-letter để xử lý sau
func (s *StreamStore) DeadLetter(ctx context.Context, msg StreamMessage, reason string) error {
	type deadEntry struct {
		Job    queue.Job `json:"job"`
		Reason string    `json:"reason"`
		DeadAt time.Time `json:"deadAt"`
	}

	data, err := json.Marshal(deadEntry{
		Job:    msg.Job,
		Reason: reason,
		DeadAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("dead letter marshal: %w", err)
	}

	deadKey := queue.GetStreamByJobType(msg.Job.Type) + deadLetterSuffix
	return s.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: deadKey,
		MaxLen: 10_000,
		Approx: true,
		MinID:  fmt.Sprintf("%d-0", time.Now().Add(-7*24*time.Hour).UnixMilli()),
		Values: map[string]any{"entry": string(data)},
	}).Err()
}

// ReclaimPending lấy lại các msg đã được claim > idleTimeout nhưng chưa được ack
func (s *StreamStore) ReclaimPending(
	ctx context.Context,
	stream, group, consumer string,
	idleTimeout time.Duration,
) ([]StreamMessage, error) {
	result, _, err := s.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
		Stream:   stream,
		Group:    group,
		Consumer: consumer,
		MinIdle:  idleTimeout,
		Start:    "0",
		Count:    100,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("reclaim pending: %w", err)
	}

	msgs := make([]StreamMessage, 0)
	for _, msg := range result {
		raw, ok := msg.Values["job"].(string)
		if !ok {
			continue
		}

		var job queue.Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			continue
		}

		job.ID = msg.ID
		msgs = append(msgs, StreamMessage{ID: msg.ID, Job: job})
	}

	return msgs, nil
}
