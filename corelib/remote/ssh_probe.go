package remote

import (
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// sshProbeWait is how long one caller waits for the shared keepalive.
	// SendRequest itself is not cancelable and can sit on a half-open TCP
	// connection for minutes, so the wait is local to the caller.
	sshProbeWait = 5 * time.Second
	// sshProbeStuckAfter is how long that one unanswered request may run
	// before the connection is treated as dead. A shorter timeout is not
	// another failure: the request is still the same one.
	sshProbeStuckAfter = 30 * time.Second
	// sshProbeResultKeep is how long a finished keepalive stays readable.
	// Callers that already timed out still need the transport error; deleting
	// it on completion leaves the catalog bound to a session that has failed.
	sshProbeResultKeep = 3 * time.Second
)

// sharedSSHProbe is the single in-flight keepalive for one ssh.Client.
// crypto/ssh holds the connection-wide globalSentMu until the reply arrives.
// A caller that times out and leaves that call running makes every later
// probe block on the mutex, time out, and look like a dead connection.
type sharedSSHProbe struct {
	started time.Time
	done    chan struct{}
	ok      bool
}

var sshProbes sync.Map // *ssh.Client -> *sharedSSHProbe

func loadSharedSSHProbe(client *ssh.Client) *sharedSSHProbe {
	p := &sharedSSHProbe{started: time.Now(), done: make(chan struct{})}
	actual, loaded := sshProbes.LoadOrStore(client, p)
	if loaded {
		return actual.(*sharedSSHProbe)
	}
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		p.ok = err == nil
		close(p.done)
		// A success is dropped at once so the next read checks the connection
		// again. A failure stays readable for sshProbeResultKeep: callers that
		// already timed out still observe it, and the handle records it.
		if p.ok {
			sshProbes.CompareAndDelete(client, p)
			return
		}
		time.AfterFunc(sshProbeResultKeep, func() {
			sshProbes.CompareAndDelete(client, p)
		})
	}()
	return p
}

// peekSSHProbe reports the keepalive already stored for client.
// It does not start one. present is false when no probe is stored.
// finished is false while that request is still in flight.
func peekSSHProbe(client *ssh.Client) (ok bool, finished bool, present bool) {
	if client == nil {
		return false, false, false
	}
	value, found := sshProbes.Load(client)
	if !found {
		return false, false, false
	}
	p := value.(*sharedSSHProbe)
	select {
	case <-p.done:
		return p.ok, true, true
	default:
		return false, false, true
	}
}

// awaitSSHProbe waits for the one keepalive already running on client.
// finished is false when wait elapses first. ok is only meaningful when
// finished is true. A rejected keepalive reply still has a nil error and
// counts as alive; only a transport error does not.
// probe is that same request. A caller that still needs its result waits
// on probe.done instead of starting another keepalive.
//
// A request that has already been unanswered for sshProbeStuckAfter is
// reported immediately. Waiting another sshProbeWait would only postpone
// the same failure.
func awaitSSHProbe(client *ssh.Client, wait time.Duration) (ok bool, finished bool, running time.Duration, probe *sharedSSHProbe) {
	if client == nil {
		return false, true, 0, nil
	}
	if wait < 0 {
		wait = 0
	}
	p := loadSharedSSHProbe(client)
	running = time.Since(p.started)
	if running >= sshProbeStuckAfter {
		select {
		case <-p.done:
			return p.ok, true, time.Since(p.started), p
		default:
			return false, false, running, p
		}
	}
	if remaining := sshProbeStuckAfter - running; remaining < wait {
		wait = remaining
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-p.done:
		return p.ok, true, time.Since(p.started), p
	case <-timer.C:
		return false, false, time.Since(p.started), p
	}
}

// sshProbeCountsAsFailure is true when the probe finished with a transport
// error, or the single request has been unanswered for sshProbeStuckAfter.
// An unfinished shorter wait is the same request and is not a new failure.
func sshProbeCountsAsFailure(finished, ok bool, running time.Duration) bool {
	if finished {
		return !ok
	}
	return running >= sshProbeStuckAfter
}
