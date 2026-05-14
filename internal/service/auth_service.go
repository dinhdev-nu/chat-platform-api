package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/global"
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
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
	userRepo     r.UserRepository // 'u' viết thường vì tính đóng gói của go ( chỉ tương tác không can thiệp )
	tokenRepo    r.UserTokenRepository
	jwtManager   *jwt.JWTManager
	emailHandler queue.Handler
}

func NewAuthService(
	ur r.UserRepository,
	tr r.UserTokenRepository,
	jm *jwt.JWTManager,
	eh queue.Handler,
) *AuthService {
	return &AuthService{
		userRepo:     ur,
		tokenRepo:    tr,
		jwtManager:   jm,
		emailHandler: eh,
	}
}

func (s *AuthService) SendOTP(ctx context.Context, req dto.SendOTPRequest) (*dto.SendOTPResponse, error) {
	locked, err := g.OTPStore.IsLocked(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if locked {
		return nil, ar.New(
			ar.ErrTooManyRequests,
			"Account temporarily locked due to too many failed attempts. Try again in 15 minutes",
		)
	}

	canResend, err := g.OTPStore.CanResend(ctx, req.Email)
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

	if err := g.OTPStore.Set(ctx, req.Email, otp); err != nil {
		return nil, ar.Internal(err)
	}

	if err := g.OTPStore.SetResendLimit(ctx, req.Email); err != nil {
		return nil, ar.Internal(err)
	}

	// Queue job gửi email OTP
	payload := queue.SendOTPEmailPayload{
		Email:        req.Email,
		OTP:          otp,
		IPAddress:    "", // Có thể thêm IP từ context nếu cần
		ExpiresInMin: 5,
	}
	go s.sendDirectFallback(req.Email, payload)
	// queue strea, sẽ triển khai sau
	// if err := g.Stream.EnqueueJob(ctx, queue.JobSendOTPEmail, payload); err != nil {
	// 	g.Logger.Warn("enqueue job failed, sending directly",
	// 		zap.String("email", payload.Email),
	// 		zap.Error(err))
	// 	go s.sendDirectFallback(req.Email, payload)
	// }

	return &dto.SendOTPResponse{
		Message:   "OTP sent successfully",
		Email:     req.Email,
		ExpiredIn: 300,
	}, nil
}

func (s *AuthService) VerifyOTP(ctx context.Context, req dto.VerifyOTPRequest, ip string) (*dto.LoginResponse, error) {
	locked, err := g.OTPStore.IsLocked(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if locked {
		return nil, ar.New(
			ar.ErrTooManyRequests,
			"Account temporarily locked due to too many failed attempts. Try again in 15 minutes",
		)
	}

	stored, err := g.OTPStore.Get(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if stored == "" {
		return nil, ar.New(ar.ErrInvalidRequest, "OTP expired or not found")
	}

	if stored != req.OTP {
		attempts, err := g.OTPStore.IncrAttempts(ctx, req.Email)
		if err != nil {
			return nil, ar.Internal(err)
		}
		if attempts >= 5 {
			if err := g.OTPStore.Lock(ctx, req.Email); err != nil {
				return nil, ar.Internal(err)
			}
		}

		return nil, ar.New(ar.ErrInvalidCredentials,
			fmt.Sprintf("Invalid OTP. You have %d attempts left", 5-attempts))
	}

	if err := g.OTPStore.Delete(ctx, req.Email); err != nil {
		return nil, ar.Internal(err)
	}

	user, err := s.findByEmailOrCreateUser(ctx, req.Email)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if !user.IsActive() {
		return nil, ar.New(ar.ErrForbidden, "User account has been suspended")
	}

	deviceID, err := crypto.ParseUUIDToBytes(req.DeviceID)
	if err != nil {
		return nil, ar.New(ar.ErrInvalidRequest, "Invalid device id")
	}

	jti, err := s.tokenRepo.GetJTIByUserAndDevice(ctx, user.ID, deviceID)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if jti != nil {
		g.Session.Revoke(ctx, jti)
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

	count, err := s.tokenRepo.CountByUserID(ctx, user.ID)
	if err != nil {
		return nil, ar.Internal(err)
	}
	if count > maxDevicesPerUser {
		jtisToEvict, err := s.tokenRepo.GetOldestJTIByUserIDBeyondLimit(ctx, user.ID, maxDevicesPerUser)
		if err != nil {
			return nil, ar.Internal(err)
		}
		if err := s.tokenRepo.DeleteOldestBeyondLimit(ctx, user.ID, maxDevicesPerUser); err != nil {
			g.Logger.Warn("Failed to evict old devices", zap.Error(err))
		}

		if len(jtisToEvict) > 0 {
			go func(ids [][]byte) {
				bgCtx := context.Background() // 1 context mới để tránh bị hủy khi request kết thúc
				for _, jti := range ids {
					if err := g.Session.Revoke(bgCtx, jti); err != nil {
						g.Logger.Warn("Failed to revoke session for evicted device", zap.Error(err))
					}
				}
			}(jtisToEvict)
		}
	}

	expireDuration := time.Duration(g.Config.Jwt.ExpireTime) * time.Second
	if err := g.Session.Set(ctx, jti, user.ID, expireDuration); err != nil {
		g.Logger.Warn("Failed to set session in cache", zap.Error(err))
	}

	return &dto.LoginResponse{
		AccessToken: token.Token,
		ExpiresAt:   token.ExpiresAT,
		User:        user.ToUserResponse(),
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, jti []byte) error {
	if err := g.Session.Revoke(ctx, jti); err != nil {
		global.Logger.Warn("Failed to revoke session", zap.Error(err))
	}
	if err := s.tokenRepo.DeleteByJTI(ctx, jti); err != nil {
		return ar.Internal(err)
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

	hexUserID, err := g.Session.Get(ctx, jti)
	if err != nil {
		return nil, nil, ar.Internal(err)
	}

	var userID []byte
	if hexUserID != "" {
		// HIT
		userID, err = crypto.ParseHexToBytes(hexUserID)
		if err != nil {
			return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token 3")
		}
	} else {
		// MISS
		tokenDB, err := s.tokenRepo.FindByJTI(ctx, jti)
		if err != nil {
			return nil, nil, ar.Internal(err)
		}
		if tokenDB == nil {
			return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token 4")
		}
		userID = tokenDB.UserID

		// warm up cache
		ttl := time.Until(tokenDB.ExpiresAt)
		g.Session.Set(ctx, jti, userID, ttl)
	}

	// Kiểm tra user status từ cache để có thể revoke token ngay khi user bị suspend/deactivate
	cached, err := g.Session.GetUser(ctx, userID)
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
		if err := g.Session.WarmUser(ctx, userID, cached); err != nil {
			g.Logger.Warn("Failed to cache user status", zap.Error(err))
		}
	}

	var user model.User
	if err := json.Unmarshal([]byte(cached), &user); err != nil {
		return nil, nil, ar.Internal(err)
	}

	if user.Status != model.UserStatusActive {
		return nil, nil, ar.New(ar.ErrForbidden, "User account is not active")
	}

	if user.LastSeenAt == nil || user.LastSeenAt.Before(time.Now().Add(-10*time.Minute)) {
		go func() {
			bgCtx := context.Background()
			s.tokenRepo.UpdateLastUsed(bgCtx, jti)
		}()
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

// sendDirectFallback là phương án dự phòng khi enqueue job thất bại, đảm bảo OTP vẫn được gửi đến người dùng
// Gửi
func (s *AuthService) sendDirectFallback(email string, payload queue.SendOTPEmailPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	data, err := json.Marshal(payload)
	if err != nil {
		global.Logger.Error("failed to marshal payload for direct email fallback",
			zap.String("email", email),
			zap.Error(err),
		)
		return
	}

	job := queue.Job{
		Type:      queue.JobSendOTPEmail,
		Payload:   data,
		Attempt:   0,
		CreatedAt: time.Now(),
	}

	if err := s.emailHandler.Handle(ctx, job); err != nil {
		global.Logger.Error("direct email fallback failed",
			zap.String("email", email),
			zap.Error(err),
		)
	}
}
