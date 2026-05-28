package app

import (
	"context"
	"testing"
	"time"
)

func TestAppCloseCancelsBackgroundWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{ctx: ctx, cancel: cancel}
	stopped := make(chan struct{})

	a.goBackground(func(ctx context.Context) {
		<-ctx.Done()
		close(stopped)
	})

	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("background worker was not canceled")
	}
}

func TestAppCloseIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{ctx: ctx, cancel: cancel}
	stopped := make(chan struct{})

	a.goBackground(func(ctx context.Context) {
		<-ctx.Done()
		close(stopped)
	})

	if err := a.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("background worker was not canceled")
	}
}
