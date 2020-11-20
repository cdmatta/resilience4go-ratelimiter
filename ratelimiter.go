package resilience4go_ratelimiter

import (
	"errors"
	"time"
)

type RateLimiterConfig struct {
	timeoutDuration           time.Duration
	limitRefreshPeriod        time.Duration
	limitForPeriod            int
	writableStackTraceEnabled bool
}

type Metrics interface {
	GetNumberOfWaitingThreads() int

	GetAvailablePermissions() int
}

type RateLimiter interface {
	ChangeTimeoutDuration(timeoutDuration time.Duration)

	ChangeLimitForPeriod(limitForPeriod int)

	AcquirePermission(permits int) bool

	ReservePermission(permits int) bool

	GetName() string

	GetRateLimiterConfig() *RateLimiterConfig

	GetMetrics() Metrics
}

type RateLimiterRegistry interface {
	RateLimiter(name string, rateLimiterConfig *RateLimiterConfig) *RateLimiter
}

func NewRateLimiterConfig(timeoutDuration time.Duration, limitRefreshPeriod time.Duration, limitForPeriod int, writableStackTraceEnabled bool) (*RateLimiterConfig, error) {
	if timeoutDuration.Nanoseconds() == int64(0) {
		return nil, errors.New("TimeoutDuration must not be null")
	}

	if limitRefreshPeriod.Nanoseconds() == int64(0) {
		return nil, errors.New("LimitRefreshPeriod must not be null")
	}

	if limitRefreshPeriod < 1*time.Nanosecond {
		return nil, errors.New("LimitRefreshPeriod is too short")
	}

	if limitForPeriod < 1 {
		return nil, errors.New("LimitForPeriod should be greater than 0")
	}

	return &RateLimiterConfig{
		timeoutDuration,
		limitRefreshPeriod,
		limitForPeriod,
		writableStackTraceEnabled,
	}, nil
}
