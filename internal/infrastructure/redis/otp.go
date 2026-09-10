package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"github.com/redis/go-redis/v9"
)

const (
	otpKey         = "otp:%s"
	otpAttemptsKey = "otp:attempts:%s"
	otpLockKey     = "otp:lock:%s"
	otpResendKey   = "otp:resend:%s"
	otpIssuanceKey = "otp:issuance:%s"
	otpTTL         = 5 * time.Minute
	otpAttemptsTTL = 15 * time.Minute
	otpLockTTL     = 15 * time.Minute
	otpResendTTL   = time.Minute
	otpMaxAttempts = 5
)

type OTPStore struct{ client *redis.Client }

func NewOTPStore(client *redis.Client) *OTPStore { return &OTPStore{client: client} }

// Reserve the OTP, cooldown and cleanup identity in one Redis operation.
var issueOTPScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[2]) == 1 then return 1 end
if redis.call('EXISTS', KEYS[3]) == 1 then return 2 end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
redis.call('SET', KEYS[3], ARGV[4], 'EX', ARGV[3])
redis.call('SET', KEYS[4], ARGV[4], 'EX', ARGV[2])
return 0
`)

func (o *OTPStore) Issue(ctx context.Context, email, code, issuanceID string) (model.OTPStatus, error) {
	result, err := issueOTPScript.Run(ctx, o.client, []string{
		fmt.Sprintf(otpKey, email), fmt.Sprintf(otpLockKey, email), fmt.Sprintf(otpResendKey, email),
		fmt.Sprintf(otpIssuanceKey, email),
	}, code, int(otpTTL.Seconds()), int(otpResendTTL.Seconds()), issuanceID).Int64()
	return model.OTPStatus(result), err
}

// Verification, failed-attempt limits and consuming a valid code are atomic.
var verifyOTPScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[3]) == 1 then return {1, 0} end
local stored = redis.call('GET', KEYS[1])
if not stored then return {3, 0} end
if stored == ARGV[1] then
    redis.call('DEL', KEYS[1], KEYS[2], KEYS[4])
    return {0, 0}
end
local attempts = redis.call('INCR', KEYS[2])
redis.call('EXPIRE', KEYS[2], ARGV[3])
local remaining = math.max(0, tonumber(ARGV[2]) - attempts)
if remaining == 0 then
    redis.call('SET', KEYS[3], 1, 'EX', ARGV[4])
end
return {4, remaining}
`)

func (o *OTPStore) Verify(ctx context.Context, email, code string) (model.OTPVerification, error) {
	result, err := verifyOTPScript.Run(ctx, o.client, []string{
		fmt.Sprintf(otpKey, email), fmt.Sprintf(otpAttemptsKey, email), fmt.Sprintf(otpLockKey, email),
		fmt.Sprintf(otpIssuanceKey, email),
	}, code, otpMaxAttempts, int(otpAttemptsTTL.Seconds()), int(otpLockTTL.Seconds())).Int64Slice()
	if err != nil {
		return model.OTPVerification{}, err
	}
	return model.OTPVerification{Status: model.OTPStatus(result[0]), AttemptsLeft: result[1]}, nil
}

// Match the issuance, not the six-digit code, which can repeat on a later send.
// The ownership key lives as long as the OTP, even after the resend cooldown ends.
var clearOTPScript = redis.NewScript(`
if redis.call('GET', KEYS[3]) == ARGV[1] then
    redis.call('DEL', KEYS[1], KEYS[3])
end
if redis.call('GET', KEYS[2]) == ARGV[1] then
    redis.call('DEL', KEYS[2])
end
return 0
`)

func (o *OTPStore) ClearSendState(ctx context.Context, email, issuanceID string) error {
	return clearOTPScript.Run(ctx, o.client, []string{
		fmt.Sprintf(otpKey, email), fmt.Sprintf(otpResendKey, email),
		fmt.Sprintf(otpIssuanceKey, email),
	}, issuanceID).Err()
}
