package main

import (
	"context"
	"testing"
)

func TestContextDoneNilSafeAndCanceled(t *testing.T) {
	if contextDone(nil) != nil {
		t.Fatal("nil context unexpectedly returned a done channel")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case <-contextDone(ctx):
	default:
		t.Fatal("canceled context did not expose a closed done channel")
	}
}
