package logger

import (
	"os"

	c "github.com/dinhdev-nu/chat-platform-api/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func New(mode string, cfg c.LoggerConfig) *zap.Logger {
	level := parseLogLevel(cfg.Level)
	encoder := buildEncoder(mode)

	cores := []zapcore.Core{
		consoleCore(encoder, level),   // write to console
		fileCore(encoder, level, cfg), // write to file with rotation
	}

	core := zapcore.NewTee(cores...)
	return zap.New(
		core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
}

func fileCore(enc zapcore.Encoder, level zapcore.Level, cfg c.LoggerConfig) zapcore.Core {
	lj := &lumberjack.Logger{
		Filename:   cfg.Dir + "/" + "app.log",
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
		LocalTime:  true, // Use local time for log rotation
	}

	return zapcore.NewCore(enc, zapcore.AddSync(lj), level)
}

func consoleCore(enc zapcore.Encoder, level zapcore.Level) zapcore.Core {
	return zapcore.NewCore(enc, zapcore.AddSync(zapcore.AddSync(os.Stdout)), level)
}

func buildEncoder(mode string) zapcore.Encoder {
	cfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	if mode == "production" {
		cfg.EncodeLevel = zapcore.CapitalLevelEncoder
		return zapcore.NewJSONEncoder(cfg)
	}
	return zapcore.NewConsoleEncoder(cfg)
}

func parseLogLevel(level string) zapcore.Level {
	l, err := zapcore.ParseLevel(level)
	if err != nil {
		return zapcore.InfoLevel
	}
	return l
}
