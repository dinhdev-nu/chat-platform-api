package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	otpKey         string = "otp:%s"          // s = phone number
	otpAttemptsKey string = "otp:attempts:%s" // s = phone number
	otpLockKey     string = "otp:lock:%s"     // s = phone number
	otpResendKey   string = "otp:resend:%s"   // s = phone number

	otpTTL         time.Duration = 5 * time.Minute
	otpMaxAttempts time.Duration = 15 * time.Minute
	otpLockTTL     time.Duration = 15 * time.Minute
	otpResendTTL   time.Duration = 1 * time.Minute

	maxOTPAttempts = 5
)

type OTPStore struct {
	client *redis.Client
}

func NewOTPStore(client *redis.Client) *OTPStore {
	return &OTPStore{client: client}
}

func (o *OTPStore) Set(ctx context.Context, email, code string) error {
	key := fmt.Sprintf(otpKey, email)
	return o.client.Set(ctx, key, code, otpTTL).Err()
}

func (o *OTPStore) Get(ctx context.Context, email string) (string, error) {
	key := fmt.Sprintf(otpKey, email)
	val, err := o.client.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}
	return val, nil
}

func (o *OTPStore) Delete(ctx context.Context, email string) error {
	otpKey := fmt.Sprintf(otpKey, email)
	attemptsKey := fmt.Sprintf(otpAttemptsKey, email)

	return o.client.Del(ctx, otpKey, attemptsKey).Err()
}

func (o *OTPStore) IncrAttempts(ctx context.Context, email string) (int64, error) {
	key := fmt.Sprintf(otpAttemptsKey, email)

	pipe := o.client.TxPipeline()
	inr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, otpMaxAttempts)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return inr.Val(), nil
}

func (o *OTPStore) Lock(ctx context.Context, email string) error {
	key := fmt.Sprintf(otpLockKey, email)
	return o.client.Set(ctx, key, 1, otpLockTTL).Err()
}

func (o *OTPStore) IsLocked(ctx context.Context, email string) (bool, error) {
	key := fmt.Sprintf(otpLockKey, email)
	exists, err := o.client.Exists(ctx, key).Result()
	return exists > 0, err
}

func (o *OTPStore) SetResendLimit(ctx context.Context, email string) error {
	key := fmt.Sprintf(otpResendKey, email)
	return o.client.Set(ctx, key, 1, otpResendTTL).Err()
}

func (o *OTPStore) CanResend(ctx context.Context, email string) (bool, error) {
	key := fmt.Sprintf(otpResendKey, email)
	exists, err := o.client.Exists(ctx, key).Result()
	return exists == 0, err
}
