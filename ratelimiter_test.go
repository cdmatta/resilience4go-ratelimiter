package resilience4go_ratelimiter

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func Test_AcquireBigNumberOfPermitsAtStartOfCycleTest(t *testing.T) {
	config, _ := NewRateLimiterConfig(time.Duration(0), time.Duration(250_000_000)*time.Nanosecond, 10, false)
	limiter := NewAtomicRateLimiter("atomic", config)

	waitForRefresh(limiter, config, ".")

	firstPermission := limiter.AcquirePermission(5)
	require.True(t, firstPermission)

	secondPermission := limiter.AcquirePermission(5)
	require.True(t, secondPermission)

	firstNoPermission := limiter.AcquirePermission(1)
	require.False(t, firstNoPermission)

	waitForRefresh(limiter, config, "*")

	retryInNewCyclePermission := limiter.AcquirePermission(1)
	require.True(t, retryInNewCyclePermission)
}

func Test_TryToAcquireBigNumberOfPermitsAtEndOfCycleTest(t *testing.T) {
	config, _ := NewRateLimiterConfig(time.Duration(0), time.Duration(250_000_000)*time.Nanosecond, 10, false)
	limiter := NewAtomicRateLimiter("atomic", config)

	waitForRefresh(limiter, config, ".")

	firstPermission := limiter.AcquirePermission(1)
	require.True(t, firstPermission)

	secondPermission := limiter.AcquirePermission(5)
	require.True(t, secondPermission)

	firstNoPermission := limiter.AcquirePermission(5)
	require.False(t, firstNoPermission)

	waitForRefresh(limiter, config, "*")

	retryInSecondCyclePermission := limiter.AcquirePermission(5)
	require.True(t, retryInSecondCyclePermission)
}

func waitForRefresh(limiter RateLimiter, config *RateLimiterConfig, printedWhileWaiting string) {
	start := time.Now()
	for ok := true; ok; ok = time.Now().Before(start.Add(config.limitRefreshPeriod)) {
		if limiter.GetAvailablePermissions() == config.limitForPeriod {
			break
		}
		fmt.Print(printedWhileWaiting)
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Println()
}
