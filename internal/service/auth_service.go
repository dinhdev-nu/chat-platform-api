package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/dinhdev-nu/chat-platform-api/pkg/jwt"
	"go.uber.org/zap"
)

const (
	maxDevicesPerUser = 5
)

type AuthService struct {
	userRepo   r.UserRepository // 'u' viết thường vì tính đóng gói của go ( chỉ tương tác không can thiệp )
	tokenRepo  r.UserTokenRepository
	jwtManager *jwt.JWTManager

	otpStore              OTPStore
	session               SessionStore
	userCache             UserCache
	jobEnqueuer           JobEnqueuer
	tokenLastUsedThrottle TokenLastUsedThrottle
	tokenTTL              time.Duration
	log                   *zap.Logger
}

type AuthServiceDeps struct {
	OTPStore              OTPStore
	Session               SessionStore
	UserCache             UserCache
	JobEnqueuer           JobEnqueuer
	TokenLastUsedThrottle TokenLastUsedThrottle
	TokenTTL              time.Duration
	Logger                *zap.Logger
}

func NewAuthService(
	ur r.UserRepository,
	tr r.UserTokenRepository,
	jm *jwt.JWTManager,
	deps AuthServiceDeps,
) *AuthService {
	return &AuthService{
		userRepo:              ur,
		tokenRepo:             tr,
		jwtManager:            jm,
		otpStore:              deps.OTPStore,
		session:               deps.Session,
		userCache:             deps.UserCache,
		jobEnqueuer:           deps.JobEnqueuer,
		tokenLastUsedThrottle: deps.TokenLastUsedThrottle,
		tokenTTL:              deps.TokenTTL,
		log:                   serviceLogger(deps.Logger),
	}
}

func (s *AuthService) SendOTP(ctx context.Context, req dto.SendOTPRequest) (*dto.SendOTPResponse, error) {
	locked, err := s.otpStore.IsLocked(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if locked {
		return nil, ar.New(
			ar.ErrTooManyRequests,
			"Account temporarily locked due to too many failed attempts. Try again in 15 minutes",
		)
	}

	canResend, err := s.otpStore.CanResend(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if !canResend {
		return nil, ar.New(
			ar.ErrTooManyRequests,
			"Please wait 1 minute before requesting another OTP",
		)
	}

	otp, err := crypto.GenerateOTP()
	if err != nil {
		return nil, ar.Internal(err)
	}

	if err := s.otpStore.Set(ctx, req.Email, otp); err != nil {
		return nil, ar.Internal(err)
	}

	if err := s.otpStore.SetResendLimit(ctx, req.Email); err != nil {
		return nil, ar.Internal(err)
	}

	payload := SendOTPEmailJob{
		Email:        req.Email,
		OTP:          otp,
		IPAddress:    "", // Có thể thêm IP từ context nếu cần
		ExpiresInMin: 5,
	}
	clearSendState := func(reason string) {
		cleanupCtx, cancel := detachedContext(ctx, cacheTaskTimeout)
		defer cancel()

		if cleanupErr := s.otpStore.ClearSendState(cleanupCtx, req.Email); cleanupErr != nil {
			s.log.Warn("failed to cleanup OTP send state",
				zap.String("email", req.Email),
				zap.String("reason", reason),
				zap.Error(cleanupErr),
			)
		}
	}
	if s.jobEnqueuer == nil {
		clearSendState("stream unavailable")
		return nil, ar.Internal(fmt.Errorf("enqueue OTP email job: stream store unavailable"))
	}

	enqueueCtx, cancel := detachedContext(ctx, streamEnqueueTimeout)
	defer cancel()
	if err := s.jobEnqueuer.EnqueueSendOTPEmail(enqueueCtx, payload); err != nil {
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
	locked, err := s.otpStore.IsLocked(ctx, email)
	if err != nil {
		return ar.Internal(err)
	}
	if locked {
		return ar.New(
			ar.ErrTooManyRequests,
			"Account temporarily locked due to too many failed attempts. Try again in 15 minutes",
		)
	}

	stored, err := s.otpStore.Get(ctx, email)
	if err != nil {
		return ar.Internal(err)
	}
	if stored == "" {
		return ar.New(ar.ErrInvalidRequest, "OTP expired or not found")
	}

	if stored != submittedOTP {
		attempts, err := s.otpStore.IncrAttempts(ctx, email)
		if err != nil {
			return ar.Internal(err)
		}
		if attempts >= 5 {
			if err := s.otpStore.Lock(ctx, email); err != nil {
				return ar.Internal(err)
			}
		}

		return ar.New(ar.ErrInvalidCredentials,
			fmt.Sprintf("Invalid OTP. You have %d attempts left", 5-attempts))
	}

	if err := s.otpStore.Delete(ctx, email); err != nil {
		return ar.Internal(err)
	}
	return nil
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
		if err := s.session.Revoke(ctx, jti); err != nil {
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
		ExpiresAt:  token.ExpiresAT,
		LastUsedAt: time.Now(), // Không có trigger nên set thủ công
	}
	if err := s.tokenRepo.Upsert(ctx, userToken); err != nil {
		return nil, ar.Internal(err)
	}

	if err := s.enforceDeviceLimit(ctx, user.ID); err != nil {
		return nil, ar.Internal(err)
	}

	if err := s.session.Set(ctx, jti, user.ID, s.tokenTTL); err != nil {
		s.log.Warn("Failed to set session in cache", zap.Error(err))
	}

	return &dto.LoginResponse{
		AccessToken: token.Token,
		ExpiresAt:   token.ExpiresAT,
		User:        dto.UserResponseFromModel(user),
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
		if err := s.revokeSessions(jtisToEvict); err != nil {
			return err
		}
	}
	if err := s.tokenRepo.DeleteOldestBeyondLimit(ctx, userID, maxDevicesPerUser); err != nil {
		s.log.Warn("Failed to evict old devices", zap.Error(err))
	}
	return nil
}

func (s *AuthService) Logout(ctx context.Context, jti []byte) error {
	if err := s.session.Revoke(ctx, jti); err != nil {
		return ar.Internal(err)
	}
	if err := s.tokenRepo.DeleteByJTI(ctx, jti); err != nil {
		s.log.Warn("Failed to delete token record after revoke", zap.Error(err))
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

	hexUserID, err := s.session.Get(ctx, jti)
	if err != nil {
		return nil, nil, ar.Internal(err)
	}
	if hexUserID == "" {
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token")
	}

	userID, err := crypto.ParseHexToBytes(hexUserID)
	if err != nil {
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token cache")
	}

	claimUserID, err := claims.UserIDBytes()
	if err != nil {
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token claims")
	}
	if !bytes.Equal(claimUserID, userID) {
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token owner")
	}

	// Kiểm tra user status từ cache để có thể revoke token ngay khi user bị suspend/deactivate
	cached, err := s.userCache.GetUser(ctx, userID)
	if err != nil {
		return nil, nil, ar.Internal(err)
	}
	if cached == "" {
		userDB, err := s.userRepo.FindByID(ctx, userID)
		if err != nil {
			return nil, nil, ar.Internal(err)
		}
		if userDB == nil {
			return nil, nil, ar.New(ar.ErrUserNotFound, "User not found")
		}

		userJson, err := json.Marshal(userDB)
		if err != nil {
			return nil, nil, ar.Internal(err)
		}
		cached = string(userJson)

		// warm up cache
		if err := s.userCache.WarmUser(ctx, userID, cached); err != nil {
			s.log.Warn("Failed to cache user status", zap.Error(err))
		}
	}

	var user model.User
	if err := json.Unmarshal([]byte(cached), &user); err != nil {
		return nil, nil, ar.Internal(err)
	}

	if user.Status != model.UserStatusActive {
		return nil, nil, ar.New(ar.ErrForbidden, "User account is not active")
	}

	shouldUpdate := false
	if s.tokenLastUsedThrottle != nil {
		var err error
		shouldUpdate, err = s.tokenLastUsedThrottle.ShouldUpdateTokenLastUsed(ctx, jti, 10*time.Minute)
		if err != nil {
			s.log.Warn("Failed to set token last_used throttle", zap.Error(err))
		}
	}
	if shouldUpdate {
		s.enqueueTokenLastUsed(jti, time.Now())
	}

	return &user, jti, nil
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

func (s *AuthService) revokeSessions(jtis [][]byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.session == nil {
		return fmt.Errorf("session store unavailable for evicted device revoke: count=%d", len(jtis))
	}

	var firstErr error
	for _, jti := range jtis {
		if len(jti) != 16 {
			s.log.Warn("authService: skip invalid evicted session jti", zap.Int("jti_len", len(jti)))
			continue
		}
		if err := s.session.Revoke(ctx, jti); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.log.Warn("authService: failed to revoke evicted session",
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

func (s *AuthService) enqueueTokenLastUsed(jti []byte, usedAt time.Time) {
	jtiCopy := append([]byte(nil), jti...)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		payload := AuthTokenLastUsedJob{
			JTI:    jtiCopy,
			UsedAt: usedAt,
		}
		if s.jobEnqueuer == nil {
			s.log.Warn("authService: stream unavailable for token last_used update, dropping best-effort job",
				zap.String("jti", hex.EncodeToString(jtiCopy)),
			)
			return
		}
		if err := s.jobEnqueuer.EnqueueAuthTokenLastUsed(ctx, payload); err != nil {
			s.log.Warn("authService: enqueue token last_used update failed, dropping best-effort job",
				zap.String("jti", hex.EncodeToString(jtiCopy)),
				zap.Error(err),
			)
		}
	}()
}
