package desktopd

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Section 9. Concurrent wakes for one user share one start or one Restore.
// The other call waits. A second screen still restores itself.
func TestDesktopBotWakeCases(t *testing.T) {
	t.Run("one_restore_while_waking", func(t *testing.T) {
		spec := Spec{TenantID: "tenant", UserID: "alice"}
		release := make(chan struct{})
		started := make(chan struct{})
		var once sync.Once
		var mu sync.Mutex
		ensures, restores := 0, 0
		tape := &wakeTape{
			ensure: func(_ context.Context, spec Spec) error {
				mu.Lock()
				ensures++
				n := ensures
				mu.Unlock()
				if spec.UserID == "alice" && n == 1 {
					once.Do(func() { close(started) })
					<-release
				}
				return nil
			},
			restore: func(spec Spec) (RestoreResult, error) {
				mu.Lock()
				restores++
				mu.Unlock()
				display := ":20"
				if spec.BotID != "" {
					display = ":21"
				}
				name := containerName(spec.TenantID, spec.UserID)
				out := `{"display":"` + display + `","cdp_port":19020,"token":"` + strings.Repeat("ab", 16) + `"}`
				return RestoreResult{Container: name, EnsureOut: out}, nil
			},
		}
		svc := &Service{
			AdvertiseHost: "docker.example",
			Machines:      tape,
			Run: func(_ context.Context, args ...string) (string, error) {
				if len(args) > 0 && args[0] == "port" {
					if strings.Contains(strings.Join(args, " "), "6080") {
						return "127.0.0.1:6081", nil
					}
					return "127.0.0.1:32768", nil
				}
				t.Fatalf("docker %v", args)
				return "", nil
			},
		}

		first := make(chan error, 1)
		go func() {
			first <- svc.Ensure(context.Background(), spec)
		}()
		<-started
		second := make(chan error, 1)
		go func() {
			second <- svc.Ensure(context.Background(), spec)
		}()
		waitFlight(t, svc, ensureFlightKey(spec))
		close(release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if ensures != 1 {
			t.Fatalf("ensure calls = %d", ensures)
		}
		mu.Unlock()

		release = make(chan struct{})
		started = make(chan struct{})
		once = sync.Once{}
		tape.blockRestore = started
		tape.releaseRestore = release
		owner := make(chan Session, 1)
		ownerErr := make(chan error, 1)
		go func() {
			session, err := svc.OpenSession(context.Background(), spec)
			owner <- session
			ownerErr <- err
		}()
		<-started
		again := make(chan Session, 1)
		againErr := make(chan error, 1)
		go func() {
			session, err := svc.OpenSession(context.Background(), spec)
			again <- session
			againErr <- err
		}()
		waitFlight(t, svc, sessionFlightKey(spec))
		botSpec := Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"}
		botSession := make(chan Session, 1)
		botErr := make(chan error, 1)
		go func() {
			session, err := svc.OpenSession(context.Background(), botSpec)
			botSession <- session
			botErr <- err
		}()
		// The other screen has its own flight. It waits on the user lock and
		// does not join this Restore.
		for i := 0; i < 50; i++ {
			runtime.Gosched()
		}
		mu.Lock()
		n := restores
		mu.Unlock()
		if n != 1 || svc.flightWaiting(sessionFlightKey(spec)) != 1 || svc.flightWaiting(sessionFlightKey(botSpec)) != 0 {
			t.Fatalf("second screen joined the first restore: restores=%d ownerWait=%d botWait=%d", n, svc.flightWaiting(sessionFlightKey(spec)), svc.flightWaiting(sessionFlightKey(botSpec)))
		}
		close(release)
		if err := <-ownerErr; err != nil {
			t.Fatal(err)
		}
		if err := <-againErr; err != nil {
			t.Fatal(err)
		}
		if err := <-botErr; err != nil {
			t.Fatal(err)
		}
		gotOwner, gotAgain, gotBot := <-owner, <-again, <-botSession
		mu.Lock()
		n = restores
		mu.Unlock()
		if n != 2 || gotOwner.Display != ":20" || gotAgain != gotOwner || gotBot.Display != ":21" {
			t.Fatalf("restores=%d owner=%+v again=%+v bot=%+v", n, gotOwner, gotAgain, gotBot)
		}
		if strings.Contains(gotOwner.CDP, "token") || !strings.Contains(gotOwner.CDP, "/v1/desktops/tunnel/") {
			t.Fatalf("session url = %s", gotOwner.CDP)
		}
	})

	t.Run("panic_releases_the_flight", func(t *testing.T) {
		spec := Spec{TenantID: "tenant", UserID: "alice"}
		release := make(chan struct{})
		started := make(chan struct{})
		var once sync.Once
		var mu sync.Mutex
		ensures := 0
		tape := &wakeTape{
			ensure: func(context.Context, Spec) error {
				mu.Lock()
				ensures++
				n := ensures
				mu.Unlock()
				if n == 1 {
					once.Do(func() { close(started) })
					<-release
					panic("wake failed")
				}
				return nil
			},
		}
		svc := &Service{AdvertiseHost: "docker.example", Machines: tape}
		leaderPanic := make(chan any, 1)
		go func() {
			defer func() { leaderPanic <- recover() }()
			_ = svc.Ensure(context.Background(), spec)
		}()
		<-started
		waiterPanic := make(chan any, 1)
		go func() {
			defer func() { waiterPanic <- recover() }()
			_ = svc.Ensure(context.Background(), spec)
		}()
		waitFlight(t, svc, ensureFlightKey(spec))
		close(release)
		if got := waitValue(t, leaderPanic); got != "wake failed" {
			t.Fatalf("leader panic = %v", got)
		}
		if got := waitValue(t, waiterPanic); got != "wake failed" {
			t.Fatalf("waiter panic = %v", got)
		}
		if svc.wakeWanted(spec.TenantID, spec.UserID) {
			t.Fatal("wake stayed announced")
		}
		if svc.flightWaiting(ensureFlightKey(spec)) != 0 {
			t.Fatal("flight stayed")
		}
		if err := svc.Ensure(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		n := ensures
		mu.Unlock()
		if n != 2 {
			t.Fatalf("ensures = %d", n)
		}
	})

	t.Run("cancelled_leader_keeps_the_waiter", func(t *testing.T) {
		spec := Spec{TenantID: "tenant", UserID: "alice"}
		release := make(chan struct{})
		started := make(chan struct{})
		var once sync.Once
		var mu sync.Mutex
		var held context.Context
		ensures := 0
		tape := &wakeTape{
			ensure: func(ctx context.Context, _ Spec) error {
				mu.Lock()
				ensures++
				if held == nil {
					held = ctx
				}
				mu.Unlock()
				once.Do(func() { close(started) })
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}
		svc := &Service{AdvertiseHost: "docker.example", Machines: tape}
		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		defer cancelLeader()
		leaderErr := make(chan error, 1)
		go func() {
			leaderErr <- svc.Ensure(leaderCtx, spec)
		}()
		<-started
		waiterErr := make(chan error, 1)
		go func() {
			waiterErr <- svc.Ensure(context.Background(), spec)
		}()
		waitFlight(t, svc, ensureFlightKey(spec))
		cancelLeader()
		mu.Lock()
		flightCtx := held
		mu.Unlock()
		select {
		case <-flightCtx.Done():
			t.Fatal("leader cancel aborted the shared wake")
		default:
		}
		close(release)
		if err := waitErr(t, leaderErr); err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("leader err = %v", err)
		}
		if err := waitErr(t, waiterErr); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		n := ensures
		mu.Unlock()
		if n != 1 {
			t.Fatalf("ensures = %d", n)
		}

		// The wake stops when the last caller cancels.
		release = make(chan struct{})
		started = make(chan struct{})
		once = sync.Once{}
		ensures = 0
		held = nil
		leaderCtx, cancelLeader = context.WithCancel(context.Background())
		defer cancelLeader()
		waiterCtx, cancelWaiter := context.WithCancel(context.Background())
		defer cancelWaiter()
		leaderErr = make(chan error, 1)
		go func() {
			leaderErr <- svc.Ensure(leaderCtx, spec)
		}()
		<-started
		waiterErr = make(chan error, 1)
		go func() {
			waiterErr <- svc.Ensure(waiterCtx, spec)
		}()
		waitFlight(t, svc, ensureFlightKey(spec))
		cancelLeader()
		mu.Lock()
		flightCtx = held
		mu.Unlock()
		select {
		case <-flightCtx.Done():
			t.Fatal("leader cancel aborted a wake that still had a waiter")
		default:
		}
		cancelWaiter()
		if err := waitErr(t, leaderErr); err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("leader err = %v", err)
		}
		if err := waitErr(t, waiterErr); err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter err = %v", err)
		}
		mu.Lock()
		n = ensures
		mu.Unlock()
		if n != 1 {
			t.Fatalf("ensures = %d", n)
		}
	})

	t.Run("abandoned_wake_runs_again", func(t *testing.T) {
		spec := Spec{TenantID: "tenant", UserID: "alice"}
		started := make(chan struct{})
		sawCancel := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		var mu sync.Mutex
		ensures := 0
		tape := &wakeTape{
			ensure: func(ctx context.Context, _ Spec) error {
				mu.Lock()
				ensures++
				n := ensures
				mu.Unlock()
				if n == 1 {
					once.Do(func() { close(started) })
					<-ctx.Done()
					close(sawCancel)
					<-release
					return ctx.Err()
				}
				return nil
			},
		}
		svc := &Service{AdvertiseHost: "docker.example", Machines: tape}
		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		defer cancelLeader()
		leaderErr := make(chan error, 1)
		go func() {
			leaderErr <- svc.Ensure(leaderCtx, spec)
		}()
		<-started
		cancelLeader()
		<-sawCancel
		nextErr := make(chan error, 1)
		go func() {
			nextErr <- svc.Ensure(context.Background(), spec)
		}()
		waitMissed(t, svc, ensureFlightKey(spec))
		select {
		case err := <-nextErr:
			t.Fatalf("later ensure took the abandoned wake: %v", err)
		default:
		}
		close(release)
		if err := waitErr(t, leaderErr); err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("leader err = %v", err)
		}
		if err := waitErr(t, nextErr); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		n := ensures
		mu.Unlock()
		if n != 2 {
			t.Fatalf("ensures = %d", n)
		}
		if svc.wakeWanted(spec.TenantID, spec.UserID) {
			t.Fatal("wake stayed announced")
		}
	})
}

func waitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("wake did not finish")
		return nil
	}
}

func waitValue(t *testing.T, ch <-chan any) any {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("wake did not finish")
		return nil
	}
}

func waitMissed(t *testing.T, svc *Service, key string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for svc.flightMissed(key) < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("later ensure joined the abandoned wake %s", key)
		}
		runtime.Gosched()
	}
}

func waitFlight(t *testing.T, svc *Service, key string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for svc.flightWaiting(key) < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("waiter did not join %s", key)
		}
		runtime.Gosched()
	}
}

// wakeTape counts wakes. Ensure and the first Restore block until released.
type wakeTape struct {
	ensure         func(context.Context, Spec) error
	restore        func(Spec) (RestoreResult, error)
	blockRestore   chan struct{}
	releaseRestore chan struct{}
	restoreOnce    sync.Once
}

func (t *wakeTape) Ensure(ctx context.Context, spec Spec) error { return t.ensure(ctx, spec) }

func (t *wakeTape) Exec(context.Context, Spec, ExecRequest) (ExecResult, error) {
	return ExecResult{}, nil
}

func (t *wakeTape) Snapshot(context.Context, Spec) error { return nil }

func (t *wakeTape) Restore(_ context.Context, spec Spec) (RestoreResult, error) {
	result, err := t.restore(spec)
	if spec.BotID == "" && t.blockRestore != nil {
		t.restoreOnce.Do(func() { close(t.blockRestore) })
		<-t.releaseRestore
	}
	return result, err
}

func (t *wakeTape) Stop(context.Context, Spec) (Desktop, error) {
	return Desktop{}, nil
}

func (t *wakeTape) Status(context.Context, Spec) (RuntimeStatus, error) {
	return RuntimeStatus{}, nil
}
