package model

type OTPStatus int64

const (
	OTPOK OTPStatus = iota
	OTPLocked
	OTPCooldown
	OTPExpired
	OTPInvalid
)

type OTPVerification struct {
	Status       OTPStatus
	AttemptsLeft int64
}
