package remote

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSSHHostKeyReuse(t *testing.T) {
	if !SSHHostKeyReuse("SHA256:pin", "SHA256:pin", false) {
		t.Fatal("the same pin is the same session")
	}
	if SSHHostKeyReuse("SHA256:pin", "SHA256:other", false) {
		t.Fatal("a different pin is a different trust decision")
	}
	if SSHHostKeyReuse("", "SHA256:pin", false) {
		t.Fatal("a requested pin must not adopt a session that recorded none")
	}
	if !SSHHostKeyReuse("SHA256:pin", "", true) {
		t.Fatal("a capture is already satisfied by a session that recorded a pin")
	}
	if SSHHostKeyReuse("", "", true) {
		t.Fatal("a capture must not adopt a session that recorded no pin")
	}
	if !SSHHostKeyReuse("", "", false) || !SSHHostKeyReuse("SHA256:pin", "", false) {
		t.Fatal("a connect with no pin and no capture keeps the open session")
	}
}

func TestSelectReusableSSHSession(t *testing.T) {
	pinned := &SSHManagedSession{
		ID:      "ssh_pinned",
		OwnerID: "task",
		Spec:    SSHSessionSpec{HostConfig: SSHHostConfig{HostKeyFingerprint: "SHA256:pin"}},
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "busy", UpdatedAt: 2},
	}
	newerOther := &SSHManagedSession{
		ID:      "ssh_other",
		OwnerID: "task",
		Spec:    SSHSessionSpec{HostConfig: SSHHostConfig{HostKeyFingerprint: "SHA256:other"}},
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 9},
	}
	unpinned := &SSHManagedSession{
		ID:      "ssh_open",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 1},
	}
	foreign := &SSHManagedSession{
		ID:      "ssh_foreign",
		OwnerID: "other",
		Spec:    SSHSessionSpec{HostConfig: SSHHostConfig{HostKeyFingerprint: "SHA256:pin"}},
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 20},
	}
	exited := &SSHManagedSession{
		ID:      "ssh_exited",
		OwnerID: "task",
		Spec:    SSHSessionSpec{HostConfig: SSHHostConfig{HostKeyFingerprint: "SHA256:pin"}},
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "exited", UpdatedAt: 30},
	}
	sessions := []*SSHManagedSession{unpinned, newerOther, pinned, foreign, exited, nil}

	if got := SelectReusableSSHSession(sessions, "root@10.0.0.8:8040", "", "task", "SHA256:pin", false); got != pinned {
		t.Fatalf("same pin = %#v", got)
	}
	if got := SelectReusableSSHSession(sessions, "root@10.0.0.8:8040", "", "task", "", true); got != newerOther {
		t.Fatalf("capture keeps the newest already-pinned session, got %#v", got)
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{unpinned}, "root@10.0.0.8:8040", "", "task", "", true); got != nil {
		t.Fatal("capture must not adopt a session with no pin")
	}
	if got := SelectReusableSSHSession(sessions, "root@10.0.0.8:8040", "", "task", "", false); got != newerOther {
		t.Fatalf("unscoped connect keeps the newest open session, got %#v", got)
	}
	if got := SelectReusableSSHSession(sessions, "root@10.0.0.8:8040", "", "mine", "SHA256:pin", false); got != nil {
		t.Fatal("another owner is not this debug machine")
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{exited}, "root@10.0.0.8:8040", "", "task", "", false); got != exited {
		t.Fatal("a terminal session is still this debug machine")
	}
	failed := &SSHManagedSession{
		ID:      "ssh_failed",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "error", UpdatedAt: 4},
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{failed, exited}, "root@10.0.0.8:8040", "", "task", "", false); got != exited {
		t.Fatal("the newest terminal session is the one to reconnect")
	}
	sameSecondB := &SSHManagedSession{
		ID:      "ssh_b",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 5},
	}
	sameSecondA := &SSHManagedSession{
		ID:      "ssh_a",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 5},
	}
	tied := []*SSHManagedSession{sameSecondB, sameSecondA}
	for i := 0; i < 20; i++ {
		if got := SelectReusableSSHSession(tied, "root@10.0.0.8:8040", "", "task", "", false); got != sameSecondA {
			t.Fatalf("same-second tie = %#v", got)
		}
	}
	later := &SSHManagedSession{
		ID:      "ssh_z",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 6},
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{sameSecondA, later}, "root@10.0.0.8:8040", "", "task", "", false); got != later {
		t.Fatalf("a later second wins the tie break, got %#v", got)
	}

	fallback := &SSHManagedSession{
		ID:      "ssh_fallback",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.1:22", HostLabel: "root@10.0.0.1:22", Status: "running", UpdatedAt: 3},
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{fallback}, "root@10.0.0.2:22", "root@10.0.0.1:22", "task", "", false); got != nil {
		t.Fatal("the host id copied into the display label is not a configured label")
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{fallback}, "root@10.0.0.1:22", "", "task", "", false); got != fallback {
		t.Fatal("the same host still matches when the display label is the host id")
	}
	labeled := &SSHManagedSession{
		ID:      "ssh_gpu",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "other-host", HostLabel: " gpu ", Status: "running", UpdatedAt: 4},
	}
	if got := SelectReusableSSHSession([]*SSHManagedSession{labeled}, "root@10.0.0.2:22", "gpu", "task", "", false); got != labeled {
		t.Fatal("a configured label still matches across host ids")
	}
}

func TestSSHReconnectIsOneHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var mu sync.Mutex
	var accepted []time.Time
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			accepted = append(accepted, time.Now())
			mu.Unlock()
			go func(c net.Conn) {
				time.Sleep(250 * time.Millisecond)
				_ = c.Close()
			}(conn)
		}
	}()

	host, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	session := &SSHManagedSession{
		ID:     "ssh_dead",
		Status: SessionExited,
		Spec: SSHSessionSpec{HostConfig: SSHHostConfig{
			Host:           host,
			User:           "root",
			Port:           port,
			AuthMethod:     "password",
			Password:       "x",
			ConnectTimeout: time.Second,
		}},
	}
	mgr := NewSSHSessionManager(nil)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_ = mgr.reconnectSession(session)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(accepted) != 2 {
		t.Fatalf("dials = %d, want one handshake at a time", len(accepted))
	}
	if gap := accepted[1].Sub(accepted[0]); gap < 200*time.Millisecond {
		t.Fatalf("handshakes overlapped by %s", gap)
	}
}

func TestSSHAdmitKeysKeepAnExplicitLabelEqualToTheHostID(t *testing.T) {
	keys := sshAdmitKeys("task", "root@10.0.0.2:22", "root@10.0.0.2:22")
	wantHost := "host\ntask\nroot@10.0.0.2:22"
	wantLabel := "label\ntask\nroot@10.0.0.2:22"
	if len(keys) != 2 || keys[0] != wantHost || keys[1] != wantLabel {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestDoExclusiveConnectIsOneDial(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	var mu sync.Mutex
	in := 0
	maxIn := 0
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.DoExclusiveConnect("task", "root@10.0.0.8:8040", "", "", false, func() {
				mu.Lock()
				in++
				if in > maxIn {
					maxIn = in
				}
				mu.Unlock()
				time.Sleep(15 * time.Millisecond)
				mu.Lock()
				in--
				mu.Unlock()
			})
		}()
	}
	wg.Wait()
	if maxIn != 1 {
		t.Fatalf("overlapping connects = %d", maxIn)
	}
}

func TestDoExclusiveConnectSecondAdmitSeesThePublishedSession(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	started := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", "root@10.0.0.8:8040", "", "", false, func() {
			close(started)
			time.Sleep(40 * time.Millisecond)
			mgr.mu.Lock()
			mgr.sessions["ssh_1"] = &SSHManagedSession{
				ID:      "ssh_1",
				OwnerID: "task",
				Spec:    SSHSessionSpec{HostConfig: SSHHostConfig{HostKeyFingerprint: "SHA256:pin"}},
				Summary: SSHSessionSummary{HostID: "root@10.0.0.8:8040", Status: "running", UpdatedAt: 1},
			}
			mgr.mu.Unlock()
		})
	}()
	<-started
	var got *SSHManagedSession
	go func() {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", "root@10.0.0.8:8040", "", "", false, func() {
			got = SelectReusableSSHSession(mgr.List(), "root@10.0.0.8:8040", "", "task", "", true)
		})
	}()
	wg.Wait()
	if got == nil || got.ID != "ssh_1" {
		t.Fatalf("second connect did not take the session the first one published: %#v", got)
	}
}

func TestDoExclusiveConnectSameLabelIsOneDial(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	var mu sync.Mutex
	in := 0
	maxIn := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		host := "root@10.0.0." + strconv.Itoa(i+1) + ":22"
		go func(host string) {
			defer wg.Done()
			mgr.DoExclusiveConnect("task", host, "gpu", "", false, func() {
				mu.Lock()
				in++
				if in > maxIn {
					maxIn = in
				}
				mu.Unlock()
				time.Sleep(15 * time.Millisecond)
				mu.Lock()
				in--
				mu.Unlock()
			})
		}(host)
	}
	wg.Wait()
	if maxIn != 1 {
		t.Fatalf("same label on different hosts overlapped %d", maxIn)
	}
}

func TestDoExclusiveConnectDifferentEndpointsRunTogether(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	both := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	enter := func(host string) {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", host, "", "", false, func() {
			both <- struct{}{}
			<-release
		})
	}
	go enter("root@10.0.0.1:22")
	go enter("root@10.0.0.2:22")
	timeout := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-both:
		case <-timeout:
			close(release)
			t.Fatal("different endpoints did not run together")
		}
	}
	close(release)
	wg.Wait()
}

func TestDoExclusiveConnectSameLabelAdmitsTheOtherHostsSession(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	started := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", "root@10.0.0.1:22", "gpu", "", false, func() {
			close(started)
			time.Sleep(40 * time.Millisecond)
			mgr.mu.Lock()
			mgr.sessions["ssh_gpu"] = &SSHManagedSession{
				ID:      "ssh_gpu",
				OwnerID: "task",
				Summary: SSHSessionSummary{HostID: "root@10.0.0.1:22", HostLabel: "gpu", Status: "running", UpdatedAt: 1},
			}
			mgr.mu.Unlock()
		})
	}()
	<-started
	var got *SSHManagedSession
	go func() {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", "root@10.0.0.2:22", "gpu", "", false, func() {
			got = SelectReusableSSHSession(mgr.List(), "root@10.0.0.2:22", "gpu", "task", "", false)
		})
	}()
	wg.Wait()
	if got == nil || got.ID != "ssh_gpu" {
		t.Fatalf("label connect dialed beside the labeled session: %#v", got)
	}
}

func TestDoExclusiveConnectMatchedSessionIsOneFlight(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	mgr.mu.Lock()
	mgr.sessions["ssh_gpu"] = &SSHManagedSession{
		ID:      "ssh_gpu",
		OwnerID: "task",
		Summary: SSHSessionSummary{HostID: "root@10.0.0.1:22", HostLabel: "gpu", Status: "running", UpdatedAt: 1},
	}
	mgr.mu.Unlock()

	var mu sync.Mutex
	in := 0
	maxIn := 0
	var wg sync.WaitGroup
	run := func(host, label string) {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", host, label, "", false, func() {
			mu.Lock()
			in++
			if in > maxIn {
				maxIn = in
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			in--
			mu.Unlock()
		})
	}
	wg.Add(2)
	go run("root@10.0.0.1:22", "")
	go run("root@10.0.0.2:22", "gpu")
	wg.Wait()
	if maxIn != 1 {
		t.Fatalf("host connect and label connect overlapped %d on one session", maxIn)
	}
}

func TestDoExclusiveConnectUnrelatedRequestRunsBesideTheMatchedSession(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	mgr.mu.Lock()
	mgr.sessions["ssh_gpu"] = &SSHManagedSession{
		ID:      "ssh_gpu",
		OwnerID: "task",
		Spec:    SSHSessionSpec{HostConfig: SSHHostConfig{HostKeyFingerprint: "SHA256:other"}},
		Summary: SSHSessionSummary{HostID: "root@10.0.0.1:22", HostLabel: "gpu", Status: "running", UpdatedAt: 1},
	}
	mgr.mu.Unlock()

	both := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	enter := func(host, label, pin string) {
		defer wg.Done()
		mgr.DoExclusiveConnect("task", host, label, pin, false, func() {
			both <- struct{}{}
			<-release
		})
	}
	go enter("root@10.0.0.1:22", "", "SHA256:mine")
	go enter("root@10.0.0.2:22", "cpu", "SHA256:mine")
	timeout := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-both:
		case <-timeout:
			close(release)
			t.Fatal("a request that does not attach the open session waited for it")
		}
	}
	close(release)
	wg.Wait()
}

func TestAttachOpenSessionMissIsTheDialSignal(t *testing.T) {
	mgr := NewSSHSessionManager(nil)
	session, reconnected, retired := mgr.AttachOpenSession("root@10.0.0.8:8040", "", "task", "", true)
	if session != nil || reconnected || retired != "" {
		t.Fatalf("miss = session %#v reconnected %v retired %q", session, reconnected, retired)
	}
}
