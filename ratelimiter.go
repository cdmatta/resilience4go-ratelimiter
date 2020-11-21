package resilience4go_ratelimiter

import (
	"errors"
	"github.com/cdmatta/resilience4go-ratelimiter/atom"
	"sync"
	"time"
)

var nanoTimeStart = time.Now().UnixNano()

type RateLimiterConfig struct {
	timeoutDuration           time.Duration
	limitRefreshPeriod        time.Duration
	limitForPeriod            int
	writableStackTraceEnabled bool
}

const (
	DEFAULT_PREFIX        = "resilience4go.ratelimiter"
	WAITING_THREADS       = "number_of_waiting_threads"
	AVAILABLE_PERMISSIONS = "available_permissions"
)

type RateLimiter interface {
	AcquirePermission(permits int) bool

	ReservePermission(permits int) int64

	GetName() string

	GetRateLimiterConfig() RateLimiterConfig

	GetNumberOfWaitingThreads() int32

	GetAvailablePermissions() int
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

type InMemoryRateLimiterRegistry struct {
	entryMap sync.Map
}

func NewInMemoryRateLimiterRegistry() *InMemoryRateLimiterRegistry {
	return &InMemoryRateLimiterRegistry{}
}

func (i *InMemoryRateLimiterRegistry) RateLimiter(name string, rateLimiterConfig *RateLimiterConfig) *RateLimiter {
	val, _ := i.entryMap.LoadOrStore(name, AtomicRateLimiter{})
	return val.(*RateLimiter)
}

type AtomicRateLimiter struct {
	name           string
	waitingThreads *atom.Int32
	state          *AtomicState
}

func NewAtomicRateLimiter(name string, rateLimiterConfig RateLimiterConfig) RateLimiter {
	state := NewAtomicState(&State{
		config:            rateLimiterConfig,
		activeCycle:       0,
		activePermissions: rateLimiterConfig.limitForPeriod,
		nanosToWait:       0,
	})

	return &AtomicRateLimiter{
		name:           name,
		waitingThreads: atom.NewInt32(0),
		state:          state,
	}
}

func currentNanoTime() int64 {
	return time.Now().UnixNano() - nanoTimeStart
}

func (a *AtomicRateLimiter) AcquirePermission(permits int) bool {
	timeoutInNanos := a.state.get().config.timeoutDuration.Nanoseconds()
	modifiedState := a.updateStateWithBackOff(permits, timeoutInNanos)
	result := a.waitForPermissionIfNecessary(timeoutInNanos, modifiedState.nanosToWait)
	return result
}

func (a *AtomicRateLimiter) ReservePermission(permits int) int64 {
	timeoutInNanos := a.state.get().config.timeoutDuration.Nanoseconds()
	modifiedState := a.updateStateWithBackOff(permits, timeoutInNanos)

	canAcquireImmediately := modifiedState.nanosToWait <= 0
	if canAcquireImmediately {
		return 0
	}

	canAcquireInTime := timeoutInNanos >= modifiedState.nanosToWait
	if canAcquireInTime {
		return modifiedState.nanosToWait
	}

	return -1
}

func (a *AtomicRateLimiter) updateStateWithBackOff(permits int, timeoutInNanos int64) *State {
	var prev, next *State
	for ok := true; ok; ok = !a.compareAndSet(prev, next) {
		prev = a.state.get()
		next = a.calculateNextState(permits, timeoutInNanos, prev)
	}
	return next
}

func (a *AtomicRateLimiter) compareAndSet(current, next *State) bool {
	if a.state.compareAndSet(current, next) {
		return true
	}
	time.Sleep(1 * time.Nanosecond)
	return false
}

func (a *AtomicRateLimiter) calculateNextState(permits int, timeoutInNanos int64, activeState *State) *State {
	cyclePeriodInNanos := activeState.config.limitRefreshPeriod.Nanoseconds()
	permissionsPerCycle := activeState.config.limitForPeriod

	currentNanos := currentNanoTime()
	currentCycle := currentNanos / cyclePeriodInNanos

	nextCycle := activeState.activeCycle
	nextPermissions := activeState.activePermissions
	if nextCycle != currentCycle {
		elapsedCycles := currentCycle - nextCycle
		accumulatedPermissions := elapsedCycles * int64(permissionsPerCycle)
		nextCycle = currentCycle
		nextPermissions = min(nextPermissions+int(accumulatedPermissions), permissionsPerCycle)
	}
	nextNanosToWait := a.nanosToWaitForPermission(permits, cyclePeriodInNanos, permissionsPerCycle, nextPermissions, currentNanos, currentCycle)

	nextState := a.reservePermissions(activeState.config, permits, timeoutInNanos, nextCycle, nextPermissions, nextNanosToWait)
	return nextState
}

func (a *AtomicRateLimiter) nanosToWaitForPermission(permits int, cyclePeriodInNanos int64, permissionsPerCycle int, availablePermissions int, currentNanos int64, currentCycle int64) int64 {
	if availablePermissions >= permits {
		return 0
	}
	nextCycleTimeInNanos := (currentCycle + 1) * cyclePeriodInNanos
	nanosToNextCycle := nextCycleTimeInNanos - currentNanos
	permissionsAtTheStartOfNextCycle := availablePermissions + permissionsPerCycle
	fullCyclesToWait := divCeil(-(permissionsAtTheStartOfNextCycle - permits), permissionsPerCycle)
	return int64(fullCyclesToWait)*cyclePeriodInNanos + nanosToNextCycle
}

func divCeil(x int, y int) int {
	return (x + y - 1) / y
}

func (a *AtomicRateLimiter) reservePermissions(config RateLimiterConfig, permits int, timeoutInNanos int64, cycle int64, permissions int, nanosToWait int64) *State {
	canAcquireInTime := timeoutInNanos >= nanosToWait
	permissionsWithReservation := permissions
	if canAcquireInTime {
		permissionsWithReservation -= permits
	}
	return &State{
		config:            config,
		activeCycle:       cycle,
		activePermissions: permissionsWithReservation,
		nanosToWait:       nanosToWait,
	}
}

func (a *AtomicRateLimiter) waitForPermissionIfNecessary(timeoutInNanos int64, nanosToWait int64) bool {
	canAcquireImmediately := nanosToWait <= 0
	canAcquireInTime := timeoutInNanos >= nanosToWait

	if canAcquireImmediately {
		return true
	}
	if canAcquireInTime {
		return a.waitForPermission(nanosToWait)
	}
	a.waitForPermission(timeoutInNanos)
	return false
}

func (a *AtomicRateLimiter) waitForPermission(nanosToWait int64) bool {
	a.waitingThreads.Add(1)
	deadline := currentNanoTime() + nanosToWait
	wasInterrupted := false
	for ok := true; ok; ok = currentNanoTime() < deadline && !wasInterrupted {
		sleepBlockDuration := deadline - currentNanoTime()

		time.Sleep(time.Duration(sleepBlockDuration) * time.Nanosecond)
		// TODO wasInterrupted = Thread.interrupted()
	}

	a.waitingThreads.Sub(1)
	if wasInterrupted {
		// TODO currentThread().interrupt()
	}
	return !wasInterrupted
}

func (a *AtomicRateLimiter) GetName() string {
	return a.name
}

func (a *AtomicRateLimiter) GetRateLimiterConfig() RateLimiterConfig {
	return a.state.get().config
}

func (a *AtomicRateLimiter) GetNumberOfWaitingThreads() int32 {
	return a.waitingThreads.Load()
}

func (a *AtomicRateLimiter) GetAvailablePermissions() int {
	currentState := a.state.get()
	estimatedState := a.calculateNextState(1, -1, currentState)
	return estimatedState.activePermissions
}

func (a *AtomicRateLimiter) getNanosToWait() int64 {
	currentState := a.state.get()
	estimatedState := a.calculateNextState(1, -1, currentState)
	return estimatedState.nanosToWait
}

func (a *AtomicRateLimiter) getCycle() int64 {
	currentState := a.state.get()
	estimatedState := a.calculateNextState(1, -1, currentState)
	return estimatedState.activeCycle
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}
