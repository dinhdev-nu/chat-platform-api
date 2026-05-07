package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/global"
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/dinhdev-nu/chat-platform-api/pkg/jwt"
	gored "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	cacheUserStatus    = "user:%s" // s = userID
	cacheUserStatusTTL = 10 * time.Minute

	maxDevicesPerUser = 5
)

type AuthService struct {
	userRepo   r.UserRepository // 'u' viết thường vì tính đóng gói của go ( chỉ tương tác không can thiệp )
	tokenRepo  r.UserTokenRepository
	jwtManager *jwt.JWTManager
}

func NewAuthService(
	ur r.UserRepository,
	tr r.UserTokenRepository,
	jm *jwt.JWTManager,
) *AuthService {
	return &AuthService{
		userRepo:   ur,
		tokenRepo:  tr,
		jwtManager: jm,
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

	// THiếu: queue gửi email
	fmt.Printf("OTP for %s: %s\n", req.Email, otp) // TODO: Send OTP via email/SMS

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
		DiviceID: deviceID,
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
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token")
	}
	jti, err := claims.JTIBytes()
	if err != nil {
		return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token")
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
			return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token")
		}
	} else {
		// MISS
		tokenDB, err := s.tokenRepo.FindByJTI(ctx, jti)
		if err != nil {
			return nil, nil, ar.Internal(err)
		}
		if tokenDB == nil {
			return nil, nil, ar.New(ar.ErrTokenInvalid, "Invalid token")
		}
		userID = tokenDB.UserID

		// warm up cache
		ttl := time.Until(tokenDB.ExpiresAt)
		g.Session.Set(ctx, jti, userID, ttl)
	}

	cKey := fmt.Sprintf(cacheUserStatus, string(userID))
	cached, err := g.RedisClient.Get(ctx, cKey).Result()
	if err != nil && err != gored.Nil {
		return nil, nil, ar.Internal(err)
	}
	if err == gored.Nil || cached == "" {
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
		if err := g.RedisClient.Set(ctx, cKey, userDB.Status, cacheUserStatusTTL).Err(); err != nil {
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

	go func() {
		bgCtx := context.Background()
		s.tokenRepo.UpdateLastUsed(bgCtx, jti)
	}()

	return &user, nil, nil
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
