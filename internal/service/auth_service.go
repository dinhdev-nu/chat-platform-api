package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/presenter"
	"github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/dinhdev-nu/chat-platform-api/pkg/jwt"
	"go.uber.org/zap"
)

type JobEnqueuer interface {
	EnqueueJob(context.Context, string, any) error
}

type AuthUsers interface {
	FindByID(ctx context.Context, id []byte) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
}
type TokenManager interface {
	GenerateToken(jwt.GenerateTokenParams) (*jwt.GenerateTokenResult, error)
	ParseToken(string) (*jwt.Claims, error)
}
type AuthSessions interface {
	Set(context.Context, []byte, []byte, time.Duration) error
	Get(context.Context, []byte) (string, error)
	Revoke(context.Context, []byte) error
}
type AuthUserCache interface {
	ProfileCacheWriter
	GetUser(context.Context, []byte) (string, error)
}
type OTPStore interface {
	Issue(context.Context, string, string) (model.OTPStatus, error)
	Verify(context.Context, string, string) (model.OTPVerification, error)
	ClearSendState(context.Context, string, string) error
}
type TokenUsageThrottle interface {
	ShouldRecord(context.Context, []byte) (bool, error)
}

// SessionTTL comes from configuration. Jobs may be nil (OTP returns an error);
// a nil UsageThrottle disables best-effort usage recording.
type AuthDependencies struct {
	Users         AuthUsers
	Tokens        repository.UserTokenRepository
	JWT           TokenManager
	OTP           OTPStore
	Sessions      AuthSessions
	UserCache     AuthUserCache
	Jobs          JobEnqueuer
	UsageThrottle TokenUsageThrottle
	SessionTTL    time.Duration
	Logger        *zap.Logger
	Now           func() time.Time
}

const (
	maxDevicesPerUser = 5
)

type AuthService struct {
	userRepo      AuthUsers
	tokenRepo     repository.UserTokenRepository
	jwtManager    TokenManager
	otp           OTPStore
	sessions      AuthSessions
	userCache     AuthUserCache
	jobs          JobEnqueuer
	usageThrottle TokenUsageThrottle
	sessionTTL    time.Duration
	logger        *zap.Logger
	now           func() time.Time
}

func NewAuthService(d AuthDependencies) *AuthService {
	return &AuthService{
		userRepo:      d.Users,
		tokenRepo:     d.Tokens,
		jwtManager:    d.JWT,
		otp:           d.OTP,
		sessions:      d.Sessions,
		userCache:     d.UserCache,
		jobs:          d.Jobs,
		usageThrottle: d.UsageThrottle,
		sessionTTL:    d.SessionTTL,
		logger:        loggerOrNop(d.Logger),
		now:           clockOrNow(d.Now),
	}
}

func (s *AuthService) SendOTP(ctx context.Context, req dto.SendOTPRequest) (*dto.SendOTPResponse, error) {
	otp, err := crypto.GenerateOTP()
	if err != nil {
		return nil, ar.Internal(err)
	}
	status, err := s.otp.Issue(ctx, req.Email, otp)
	if err != nil {
		return nil, ar.Internal(err)
	}
	switch status {
	case model.OTPLocked:
		return nil, ar.New(
			ar.ErrTooManyRequests,
			"Account temporarily locked due to too many failed attempts. Try again in 15 minutes",
		)
	case model.OTPCooldown:
		return nil, ar.New(
			ar.ErrTooManyRequests,
			"Please wait 1 minute before requesting another OTP",
		)
	case model.OTPOK:
	default:
		return nil, ar.Internal(fmt.Errorf("unexpected OTP issue status: %d", status))
	}

	payload := queue.SendOTPEmailPayload{
		Email:        req.Email,
		OTP:          otp,
		IPAddress:    "", // Có thể thêm IP từ context nếu cần
		ExpiresInMin: 5,
	}
	clearSendState := func(reason string) {
		cleanupCtx, cancel := detachedContext(ctx, cacheTaskTimeout)
		defer cancel()

		if cleanupErr := s.otp.ClearSendState(cleanupCtx, req.Email, otp); cleanupErr != nil {
			s.logger.Warn("failed to cleanup OTP send state",
				zap.String("email", req.Email),
				zap.String("reason", reason),
				zap.Error(cleanupErr),
			)
		}
	}
	if s.jobs == nil {
		clearSendState("stream unavailable")
		return nil, ar.Internal(fmt.Errorf("enqueue OTP email job: stream store unavailable"))
	}

	enqueueCtx, cancel := detachedContext(ctx, streamEnqueueTimeout)
	defer cancel()
	if err := s.jobs.EnqueueJob(enqueueCtx, queue.JobSendOTPEmail, payload); err != nil {
		clearSendState("enqueue failed")
		return nil, ar.Internal(fmt.Errorf("enqueue OTP email job: %w", err))
	}

	return &dto.SendOTPResponse{
		Message:   "OTP sent successfully",
		Email:     req.Email,
		ExpiredIn: 300,
	}, nil
}

func (s *AuthService) VerifyOTP(ctx context.Context, req dto.VerifyOTPRequest, ip string) (*dto.LoginResponse, error) {
	if err := s.verifyOTPCode(ctx, req.Email, req.OTP); err != nil {
		return nil, err
	}

	user, err := s.findByEmailOrCreateUser(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if !user.IsActive() {
		return nil, ar.New(ar.ErrForbidden, "User account has been suspended")
	}

	return s.createLoginSession(ctx, user, req, ip)
}

func (s *AuthService) verifyOTPCode(ctx context.Context, email, submittedOTP string) error {
	result, err := s.otp.Verify(ctx, email, submittedOTP)
	if err != nil {
		return ar.Internal(err)
	}
	switch result.Status {
	case model.OTPOK:
		return nil
	case model.OTPLocked:
		return ar.New(
			ar.ErrTooManyRequests,
			"Account temporarily locked due to too many failed attempts. Try again in 15 minutes",
		)
	case model.OTPExpired:
		return ar.New(ar.ErrInvalidRequest, "OTP expired or not found")
	case model.OTPInvalid:
		return ar.New(ar.ErrInvalidCredentials,
			fmt.Sprintf("Invalid OTP. You have %d attempts left", result.AttemptsLeft))
	default:
		return ar.Internal(fmt.Errorf("unexpected OTP verification status: %d", result.Status))
	}
}

func (s *AuthService) createLoginSession(
	ctx context.Context,
	user *model.User,
	req dto.VerifyOTPRequest,
	ip string,
) (*dto.LoginResponse, error) {
	deviceID, err := crypto.ParseUUIDToBytes(req.DeviceID)
	if err != nil {
		return nil, ar.New(ar.ErrInvalidRequest, "Invalid device id")
	}

	jti, err := s.tokenRepo.GetJTIByUserAndDevice(ctx, user.ID, deviceID)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if jti != nil {
		if err := s.sessions.Revoke(ctx, jti); err != nil {
			return nil, ar.Internal(err)
		}
	}
	jti, err = crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ar.Internal(err)
	}

	token, err := s.jwtManager.GenerateToken(jwt.GenerateTokenParams{
		UserID:   user.ID,
		DeviceID: deviceID,
		JTI:      jti,
	})
	if err != nil {
		return nil, ar.Internal(err)
	}

	userToken := &model.UserToken{
		UserID:     user.ID,
		JTI:        jti,
		DeviceID:   deviceID,
		DeviceName: &req.DeviceName,
		IPAddress:  &ip,
		ExpiresAt:  token.ExpiresAt,
		LastUsedAt: s.now(), // Không có trigger nên set thủ công
	}
	if err := s.tokenRepo.Upsert(ctx, userToken); err != nil {
		return nil, ar.Internal(err)
	}

	if err := s.enforceDeviceLimit(ctx, user.ID); err != nil {
		return nil, ar.Internal(err)
	}

	if err := s.sessions.Set(ctx, jti, user.ID, s.sessionTTL); err != nil {
		cleanupCtx, cancel := detachedContext(ctx, sideEffectTimeout)
		defer cancel()
		if cleanupErr := s.sessions.Revoke(cleanupCtx, jti); cleanupErr != nil {
			s.logger.Warn("failed to revoke incomplete login session", zap.Error(cleanupErr))
		}
		if cleanupErr := s.tokenRepo.DeleteByJTI(cleanupCtx, jti); cleanupErr != nil {
			s.logger.Warn("failed to delete incomplete login token", zap.Error(cleanupErr))
		}
		return nil, ar.Internal(fmt.Errorf("create login session: %w", err))
	}

	return &dto.LoginResponse{
		AccessToken: token.Token,
		ExpiresAt:   token.ExpiresAt,
		User:        presenter.User(user),
	}, nil
}

func (s *AuthService) enforceDeviceLimit(ctx context.Context, userID []byte) error {
	count, err := s.tokenRepo.CountByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if count <= maxDevicesPerUser {
		return nil
	}

	jtisToEvict, err := s.tokenRepo.GetOldestJTIByUserIDBeyondLimit(ctx, userID, maxDevicesPerUser)
	if err != nil {
		return err
	}
	if len(jtisToEvict) > 0 {
		if err := s.revokeSessions(ctx, jtisToEvict); err != nil {
			return err
		}
	}
	if err := s.tokenRepo.DeleteOldestBeyondLimit(ctx, userID, maxDevicesPerUser); err != nil {
		s.logger.Warn("Failed to evict old devices", zap.Error(err))
	}
	return nil
}

func (s *AuthService) Logout(ctx context.Context, jti []byte) error {
	if err := s.sessions.Revoke(ctx, jti); err != nil {
		return ar.Internal(err)
	}
	if err := s.tokenRepo.DeleteByJTI(ctx, jti); err != nil {
		s.logger.Warn("Failed to delete token record after revoke", zap.Error(err))
	}

	return nil
}

func (s *AuthService) ValidateToken(ctx context.Context, tokenStr string) (*model.User, []byte, error) {
	claims, err := s.jwtManager.ParseToken(tokenStr)
	if err != nil {
		return nil, nil, ar.New(ar.ErrTokenInvalid, err.Error())
	}
	jti, err := claims.JTIBytes()
	if err != nil {
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token 2")
	}

	userID, err := s.validateSessionOwner(ctx, claims, jti)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.loadActiveTokenUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	s.recordTokenUsage(ctx, jti)
	return user, jti, nil
}

func (s *AuthService) validateSessionOwner(ctx context.Context, claims *jwt.Claims, jti []byte) ([]byte, error) {
	hexUserID, err := s.sessions.Get(ctx, jti)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if hexUserID == "" {
		return nil, ar.New(ar.ErrTokenInvalid, "Invalid token")
	}

	userID, err := crypto.ParseHexToBytes(hexUserID)
	if err != nil {
		return nil, ar.New(ar.ErrTokenInvalid, "Invalid token cache")
	}

	claimUserID, err := claims.UserIDBytes()
	if err != nil {
		return nil, ar.New(ar.ErrTokenInvalid, "Invalid token claims")
	}
	if !bytes.Equal(claimUserID, userID) {
		return nil, ar.New(ar.ErrTokenInvalid, "Invalid token owner")
	}
	return userID, nil
}

func (s *AuthService) loadActiveTokenUser(ctx context.Context, userID []byte) (*model.User, error) {
	// Kiểm tra user status từ cache để có thể revoke token ngay khi user bị suspend/deactivate
	cached, err := s.userCache.GetUser(ctx, userID)
	if err != nil {
		return nil, ar.Internal(err)
	}
	var user model.User
	if cached == "" {
		userDB, err := s.userRepo.FindByID(ctx, userID)
		if err != nil {
			return nil, ar.Internal(err)
		}
		if userDB == nil {
			return nil, ar.New(ar.ErrUserNotFound, "User not found")
		}
		user = *userDB

		userJSON, err := json.Marshal(userDB)
		if err != nil {
			return nil, ar.Internal(err)
		}
		cached = string(userJSON)

		// warm up cache
		if err := s.userCache.WarmUser(ctx, userID, cached); err != nil {
			s.logger.Warn("Failed to cache user status", zap.Error(err))
		}
	} else if err := json.Unmarshal([]byte(cached), &user); err != nil {
		return nil, ar.Internal(err)
	}

	if user.Status != model.UserStatusActive {
		return nil, ar.New(ar.ErrForbidden, "User account is not active")
	}
	return &user, nil
}

func (s *AuthService) recordTokenUsage(ctx context.Context, jti []byte) {
	if s.usageThrottle == nil {
		return
	}
	shouldUpdate, err := s.usageThrottle.ShouldRecord(ctx, jti)
	if err != nil {
		s.logger.Warn("Failed to set token last_used throttle", zap.Error(err))
	}
	if shouldUpdate {
		s.enqueueTokenLastUsed(ctx, jti, s.now())
	}
}

func (s *AuthService) findByEmailOrCreateUser(ctx context.Context, email string) (*model.User, error) {
	user, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if user != nil {
		return user, nil
	}

	id, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ar.Internal(err)
	}

	newUser := &model.User{
		ID:       id,
		Username: email,
		Email:    email,
		Status:   model.UserStatusActive,
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, ar.Internal(err)
	}

	return newUser, nil
}

func (s *AuthService) revokeSessions(parent context.Context, jtis [][]byte) error {
	ctx, cancel := detachedContext(parent, sideEffectTimeout)
	defer cancel()

	if s.sessions == nil {
		return fmt.Errorf("session store unavailable for evicted device revoke: count=%d", len(jtis))
	}

	var firstErr error
	for _, jti := range jtis {
		if len(jti) != 16 {
			s.logger.Warn("authService: skip invalid evicted session jti", zap.Int("jti_len", len(jti)))
			continue
		}
		if err := s.sessions.Revoke(ctx, jti); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logger.Warn("authService: failed to revoke evicted session",
				zap.String("jti", hex.EncodeToString(jti)),
				zap.Error(err),
			)
		}
	}
	if firstErr != nil {
		return fmt.Errorf("revoke evicted sessions: %w", firstErr)
	}
	return nil
}

func (s *AuthService) enqueueTokenLastUsed(parent context.Context, jti []byte, usedAt time.Time) {
	jtiCopy := append([]byte(nil), jti...)

	go func() {
		ctx, cancel := detachedContext(parent, 2*time.Second)
		defer cancel()

		payload := queue.AuthTokenLastUsedPayload{
			JTI:    jtiCopy,
			UsedAt: usedAt,
		}
		if s.jobs == nil {
			s.logger.Warn("authService: stream unavailable for token last_used update, dropping best-effort job",
				zap.String("jti", hex.EncodeToString(jtiCopy)),
			)
			return
		}
		if err := s.jobs.EnqueueJob(ctx, queue.JobUpdateAuthTokenLastUsed, payload); err != nil {
			s.logger.Warn("authService: enqueue token last_used update failed, dropping best-effort job",
				zap.String("jti", hex.EncodeToString(jtiCopy)),
				zap.Error(err),
			)
		}
	}()
}

func loggerOrNop(logger *zap.Logger) *zap.Logger {
	if logger == nil {
		return zap.NewNop()
	}
	return logger
}

func clockOrNow(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}
