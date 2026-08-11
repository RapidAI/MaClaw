package digitalasset

import (
	"sync"
	"time"
)

// SyncLimiter provides per-user RPM and per-tenant concurrent pull slots.
type SyncLimiter struct {
	perUserRPM           int
	perTenantConcurrent  int

	mu       sync.Mutex
	users    map[string]*userBucket
	tenants  map[string]*tenantSlots
}

type userBucket struct {
	tokens   float64
	last     time.Time
	capacity float64
	refillPS float64
}

type tenantSlots struct {
	inFlight int
	max      int
}

// NewSyncLimiter creates a limiter with design defaults if values are non-positive.
func NewSyncLimiter(perUserRPM, perTenantConcurrent int) *SyncLimiter {
	if perUserRPM <= 0 {
		perUserRPM = 30
	}
	if perTenantConcurrent <= 0 {
		perTenantConcurrent = 8
	}
	return &SyncLimiter{
		perUserRPM:          perUserRPM,
		perTenantConcurrent: perTenantConcurrent,
		users:               make(map[string]*userBucket),
		tenants:             make(map[string]*tenantSlots),
	}
}

// AllowPull returns false if user is over RPM.
func (l *SyncLimiter) AllowPull(tenantID, userID string) (ok bool, retryAfterMS int) {
	if l == nil {
		return true, 0
	}
	key := tenantID + "|" + userID
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, okb := l.users[key]
	if !okb {
		b = &userBucket{
			tokens:   float64(l.perUserRPM),
			last:     now,
			capacity: float64(l.perUserRPM),
			refillPS: float64(l.perUserRPM) / 60.0,
		}
		l.users[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.refillPS
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.last = now
	if b.tokens < 1 {
		need := (1 - b.tokens) / b.refillPS
		return false, int(need*1000) + 1
	}
	b.tokens--
	return true, 0
}

// AcquireSlot acquires a tenant concurrent pull slot. Caller must ReleaseSlot.
func (l *SyncLimiter) AcquireSlot(tenantID string) (release func(), err error) {
	if l == nil {
		return func() {}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.tenants[tenantID]
	if !ok {
		s = &tenantSlots{max: l.perTenantConcurrent}
		l.tenants[tenantID] = s
	}
	if s.inFlight >= s.max {
		return nil, errTenantBusy
	}
	s.inFlight++
	released := false
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if released {
			return
		}
		released = true
		if s.inFlight > 0 {
			s.inFlight--
		}
	}, nil
}

type busyError string

func (e busyError) Error() string { return string(e) }

const errTenantBusy busyError = "tenant concurrent pull limit reached"
