package service

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

func startRunnerForTest(t *testing.T) (*Runner, net.Conn) {
	t.Helper()
	host, device := net.Pipe()
	runner := NewRunner(func() (io.ReadWriteCloser, error) { return device, nil }, testOptions())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		host.Close()
		<-done
	})
	return runner, host
}

func waitForState(t *testing.T, host net.Conn) *protocol.Message {
	t.Helper()
	frames := readFrames(t, host, time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.TypeState
	})
	for _, m := range frames {
		if m.Type == protocol.TypeState {
			return m
		}
	}
	t.Fatal("no state frame")
	return nil
}

func TestRunnerCelebratesCompletedTask(t *testing.T) {
	runner, host := startRunnerForTest(t)

	runner.SetSnapshot(protocol.Snapshot{
		Counts: protocol.Counts{Total: 2, InProgress: 1, Done: 1},
		Current: []protocol.TaskItem{
			{ID: "task-a", Title: "整理会议纪要", Progress: -1},
		},
		UpdatedAt: time.Now().UnixMilli(),
	})
	waitForState(t, host)

	runner.SetSnapshot(protocol.Snapshot{
		Counts:    protocol.Counts{Total: 2, InProgress: 0, Done: 2},
		UpdatedAt: time.Now().UnixMilli(),
	})
	frames := readFrames(t, host, time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.TypeCelebrate
	})
	var celebrate *protocol.Message
	for _, m := range frames {
		if m.Type == protocol.TypeCelebrate {
			celebrate = m
			break
		}
	}
	if celebrate == nil || celebrate.Celebrate == nil {
		t.Fatalf("no celebrate frame; got %v", frameTypes(frames))
	}
	if celebrate.Celebrate.Title != "整理会议纪要" {
		t.Fatalf("celebrate title=%q", celebrate.Celebrate.Title)
	}
	if celebrate.Celebrate.Seconds != 5 {
		t.Fatalf("celebrate seconds=%d", celebrate.Celebrate.Seconds)
	}
	if err := celebrate.Validate(); err != nil {
		t.Fatalf("celebrate invalid: %v", err)
	}
}

func TestRunnerDoesNotCelebrateDeletion(t *testing.T) {
	runner, host := startRunnerForTest(t)

	runner.SetSnapshot(protocol.Snapshot{
		Counts: protocol.Counts{Total: 2, InProgress: 1, Done: 1},
		Current: []protocol.TaskItem{
			{ID: "task-a", Title: "整理会议纪要", Progress: -1},
		},
		UpdatedAt: time.Now().UnixMilli(),
	})
	waitForState(t, host)

	// The person deleted the in-progress task: total shrinks, done is unchanged.
	runner.SetSnapshot(protocol.Snapshot{
		Counts:    protocol.Counts{Total: 1, InProgress: 0, Done: 1},
		UpdatedAt: time.Now().UnixMilli(),
	})
	frames := readFrames(t, host, 300*time.Millisecond, nil)
	for _, m := range frames {
		if m.Type == protocol.TypeCelebrate {
			t.Fatalf("deletion must not celebrate: %+v", m.Celebrate)
		}
	}
}

func TestRunnerDoesNotCelebrateFirstState(t *testing.T) {
	runner, host := startRunnerForTest(t)

	runner.SetSnapshot(protocol.Snapshot{
		Counts:    protocol.Counts{Total: 1, Done: 1},
		UpdatedAt: time.Now().UnixMilli(),
	})
	frames := readFrames(t, host, 300*time.Millisecond, nil)
	for _, m := range frames {
		if m.Type == protocol.TypeCelebrate {
			t.Fatalf("first state must not celebrate")
		}
	}
}

func TestRunnerSendsSoundConfig(t *testing.T) {
	runner, host := startRunnerForTest(t)

	want := protocol.SoundConfig{
		Master:   true,
		Running:  protocol.SoundEventConfig{Enabled: true, Effect: 0},
		Complete: protocol.SoundEventConfig{Enabled: true, Effect: 2},
		Standby:  protocol.SoundEventConfig{Enabled: false, Effect: 1},
	}
	runner.SetSoundConfig(want)

	frames := readFrames(t, host, time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.TypeSound
	})
	var got *protocol.Message
	for _, m := range frames {
		if m.Type == protocol.TypeSound {
			got = m
			break
		}
	}
	if got == nil || got.Sound == nil {
		t.Fatalf("no sound frame; got %v", frameTypes(frames))
	}
	if got.Sound.Complete.Effect != 2 || got.Sound.Standby.Enabled {
		t.Fatalf("unexpected sound config: %+v", got.Sound)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("sound frame invalid: %v", err)
	}
}
