package resilience4go_ratelimiter

import "sync"

type State struct {
	config            RateLimiterConfig
	activeCycle       int64
	activePermissions int
	nanosToWait       int64
}

type AtomicState struct {
	mtx   sync.RWMutex
	state *State
}

func NewAtomicState(state *State) *AtomicState {
	return &AtomicState{
		state: state,
	}
}

func (a *AtomicState) get() *State {
	a.mtx.RUnlock()
	defer a.mtx.RUnlock()

	return a.state
}

func (a *AtomicState) compareAndSet(expectedValue, newValue *State) bool {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	if a.state == expectedValue {
		a.state = newValue
		return true
	}
	return false
}
