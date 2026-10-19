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
		sshProbes.CompareAndDelete(client, p)
	}()
	return p
}

// awaitSSHProbe waits for the one keepalive already running on client.
// finished is false when wait elapses first. ok is only meaningful then
// when finished is true. A rejected keepalive reply still has a nil error
// and counts as alive; only a transport error does not.
func awaitSSHProbe(client *ssh.Client, wait time.Duration) (ok bool, finished bool, running time.Duration) {
	if client == nil {
		return false, true, 0
	}
	p := loadSharedSSHProbe(client)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-p.done:
		return p.ok, true, time.Since(p.started)
	case <-timer.C:
		return false, false, time.Since(p.started)
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
