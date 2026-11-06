package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithoutDeadlineCancelIgnoresTheRouteTimer(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	signal := WithoutDeadlineCancel(parent)
	select {
	case <-signal.Done():
		t.Fatalf("route deadline canceled the stop signal: %v", signal.Err())
	case <-time.After(120 * time.Millisecond):
	}
	if parent.Err() == nil {
		t.Fatal("parent deadline did not fire")
	}
}

func TestWithoutDeadlineCancelPropagatesAStop(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	signal := WithoutDeadlineCancel(parent)
	cancel()
	select {
	case <-signal.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel the signal")
	}
	if errors.Is(signal.Err(), context.DeadlineExceeded) {
		t.Fatal("stop was reported as a deadline")
	}
}

func TestContinueAfterOwnedRouteDeadlineKeepsTheScriptAlive(t *testing.T) {
	stopParent, stop := context.WithCancel(context.Background())
	defer stop()
	route, cancel := context.WithTimeout(stopParent, 30*time.Millisecond)
	defer cancel()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, stopParent)
	select {
	case <-route.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}

	scriptCtx, release := ContinueAfterOwnedRouteDeadline(route)
	defer release()
	time.Sleep(20 * time.Millisecond)
	if err := scriptCtx.Err(); err != nil {
		t.Fatalf("script context ended with the route deadline: %v", err)
	}
	scriptTimeout, cancelTimeout := context.WithTimeout(scriptCtx, 40*time.Millisecond)
	defer cancelTimeout()
	select {
	case <-scriptTimeout.Done():
	case <-time.After(time.Second):
		t.Fatal("script timeout did not fire")
	}
	if !errors.Is(scriptTimeout.Err(), context.DeadlineExceeded) {
		t.Fatalf("script timeout err = %v", scriptTimeout.Err())
	}
	if scriptCtx.Err() != nil {
		t.Fatal("script timeout canceled the parent context")
	}

	stop()
	select {
	case <-scriptCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("user stop did not cancel the script context")
	}
}

func TestContinueAfterOwnedRouteDeadlineLeavesALiveParent(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	deadline, _ := parent.Deadline()
	parent = BindRouteStreamDeadline(parent, deadline, context.Background())
	got, release := ContinueAfterOwnedRouteDeadline(parent)
	defer release()
	if got != parent {
		t.Fatal("a live route context was replaced")
	}
	if _, ok := got.Deadline(); !ok {
		t.Fatal("remaining host deadline was removed")
	}
}

func TestContinueAfterOwnedRouteDeadlineDoesNotLiftAHostDeadline(t *testing.T) {
	host, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	select {
	case <-host.Done():
	case <-time.After(time.Second):
		t.Fatal("host deadline did not fire")
	}
	got, release := ContinueAfterOwnedRouteDeadline(host)
	defer release()
	if got.Err() == nil {
		t.Fatal("unowned deadline was lifted")
	}
	child, cancelChild := context.WithTimeout(got, time.Hour)
	defer cancelChild()
	if child.Err() == nil {
		t.Fatal("script context outlived an unowned host deadline")
	}
}

func TestContinueAfterOwnedRouteDeadlineLeavesAFinishedRouteParent(t *testing.T) {
	route, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, route)
	select {
	case <-route.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}

	scriptCtx, release := ContinueAfterOwnedRouteDeadline(route)
	select {
	case <-scriptCtx.Done():
		t.Fatalf("finished route parent canceled the script context: %v", scriptCtx.Err())
	case <-time.After(200 * time.Millisecond):
	}
	release()
	select {
	case <-scriptCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("release did not cancel the script context")
	}
}

func TestContinueAfterOwnedRouteDeadlineHonorsAStopThatAlreadyLanded(t *testing.T) {
	stopParent, stop := context.WithCancel(context.Background())
	route, cancel := context.WithTimeout(stopParent, 30*time.Millisecond)
	defer cancel()
	deadline, ok := route.Deadline()
	if !ok {
		t.Fatal("route missing deadline")
	}
	route = BindRouteStreamDeadline(route, deadline, stopParent)
	select {
	case <-route.Done():
	case <-time.After(time.Second):
		t.Fatal("route deadline did not fire")
	}
	stop()

	scriptCtx, release := ContinueAfterOwnedRouteDeadline(route)
	defer release()
	select {
	case <-scriptCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("a stop that already landed was dropped")
	}
}

func TestContinueAfterOwnedRouteDeadlineHonorsACallerCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	route, cancelRoute := context.WithTimeout(parent, time.Hour)
	defer cancelRoute()
	deadline, _ := route.Deadline()
	route = BindRouteStreamDeadline(route, deadline, parent)
	cancel()
	got, release := ContinueAfterOwnedRouteDeadline(route)
	defer release()
	if got.Err() == nil {
		t.Fatal("caller cancel was lifted with the route deadline")
	}
}
