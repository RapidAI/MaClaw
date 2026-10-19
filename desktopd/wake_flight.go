package desktopd

import (
	"context"
	"sync"
)

// A wake flight is one in-progress Ensure or OpenSession. Another call for
// the same key waits and takes that result. It does not start a second
// docker start or a second Restore. The key is taken before the user lock,
// which the leader holds for the whole wake.
//
// The flight ends when the leader returns or panics, and the next call for
// that key starts again. Callers are counted under the same lock that
// publishes the flight, so a caller that has already joined cannot be missed.
// The docker context stays alive until every joined caller has cancelled or
// returned. One request disconnecting does not abort a wake that another
// caller still needs. A caller that arrives after the wake has been abandoned
// waits for that attempt to finish, then starts the wake again.

type flightCall struct {
	done      chan struct{}
	err       error
	session   Session
	waiting   int
	missed    int
	recovered any
	live      int
	doomed    bool
	finished  bool

	ctx    context.Context
	cancel context.CancelFunc
}

func ensureFlightKey(spec Spec) string {
	return "ensure\x00" + spec.TenantID + "\x00" + spec.UserID
}

func sessionFlightKey(spec Spec) string {
	return "session\x00" + spec.TenantID + "\x00" + spec.UserID + "\x00" + spec.BotID
}

func (s *Service) flightWaiting(key string) int {
	if s == nil {
		return 0
	}
	s.flightMu.Lock()
	defer s.flightMu.Unlock()
	call := s.flights[key]
	if call == nil {
		return 0
	}
	return call.waiting
}

func (s *Service) beginFlight(key string) (call *flightCall, leader, missed bool) {
	s.flightMu.Lock()
	defer s.flightMu.Unlock()
	if s.flights == nil {
		s.flights = map[string]*flightCall{}
	}
	if existing := s.flights[key]; existing != nil {
		if existing.doomed {
			existing.missed++
			return existing, false, true
		}
		existing.waiting++
		existing.live++
		return existing, false, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	call = &flightCall{done: make(chan struct{}), ctx: ctx, cancel: cancel, live: 1}
	s.flights[key] = call
	return call, true, false
}

func (s *Service) finishFlight(key string, call *flightCall) {
	s.flightMu.Lock()
	delete(s.flights, key)
	s.flightMu.Unlock()
	close(call.done)
	call.cancel()
}

func (s *Service) releaseFlight(call *flightCall) {
	s.flightMu.Lock()
	call.live--
	// Releasing the count after the wake has already returned is the normal
	// exit. That must not mark the flight abandoned, or a caller that arrives
	// in this window would run the wake a second time.
	abort := call.live == 0 && !call.finished
	if abort {
		call.doomed = true
	}
	s.flightMu.Unlock()
	if abort {
		call.cancel()
	}
}

func (s *Service) completeFlight(call *flightCall) {
	s.flightMu.Lock()
	call.finished = true
	s.flightMu.Unlock()
}

func (s *Service) flightMissed(key string) int {
	if s == nil {
		return 0
	}
	s.flightMu.Lock()
	defer s.flightMu.Unlock()
	call := s.flights[key]
	if call == nil {
		return 0
	}
	return call.missed
}

// watchCaller drops this caller's count when its context ends. The count
// itself was taken in beginFlight, before that call returned.
func (s *Service) watchCaller(call *flightCall, ctx context.Context) (leave func()) {
	var once sync.Once
	leave = func() {
		once.Do(func() { s.releaseFlight(call) })
	}
	if ctx.Err() != nil {
		leave()
		return leave
	}
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				leave()
			case <-call.done:
			}
		}()
	}
	return leave
}

func (s *Service) flightSession(key string, ctx context.Context, fn func(context.Context) (Session, error)) (Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		call, leader, missed := s.beginFlight(key)
		if missed {
			select {
			case <-call.done:
			case <-ctx.Done():
				return Session{}, ctx.Err()
			}
			if err := ctx.Err(); err != nil {
				return Session{}, err
			}
			continue
		}
		leave := s.watchCaller(call, ctx)
		if !leader {
			select {
			case <-call.done:
				leave()
				if call.recovered != nil {
					panic(call.recovered)
				}
				return call.session, call.err
			case <-ctx.Done():
				leave()
				return Session{}, ctx.Err()
			}
		}
		return s.leadFlight(key, call, ctx, leave, fn)
	}
}

func (s *Service) leadFlight(key string, call *flightCall, ctx context.Context, leave func(), fn func(context.Context) (Session, error)) (Session, error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			call.recovered = recovered
			s.finishFlight(key, call)
			panic(recovered)
		}
		s.finishFlight(key, call)
	}()
	defer leave()
	call.session, call.err = fn(call.ctx)
	s.completeFlight(call)
	// This caller's request is already done. The shared result stays what the
	// wake returned, for a caller that is still waiting.
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	return call.session, call.err
}

func (s *Service) flightErr(key string, ctx context.Context, fn func(context.Context) error) error {
	_, err := s.flightSession(key, ctx, func(ctx context.Context) (Session, error) {
		return Session{}, fn(ctx)
	})
	return err
}
