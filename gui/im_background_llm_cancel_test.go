package main

import (
	"context"
	"testing"
)

func TestBackgroundLLMCancelIsScopedByOwner(t *testing.T) {
	h := &IMMessageHandler{}
	ownerA := "desktop-user:C:/tasks/a"
	ownerB := "desktop-user:C:/tasks/b"

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	h.storeBackgroundLLMCancelForOwner(ownerA, cancelA)
	h.storeBackgroundLLMCancelForOwner(ownerB, cancelB)

	if !h.cancelBackgroundLLMForOwner(ownerA) {
		t.Fatal("expected owner A background LLM canceler to be found")
	}

	select {
	case <-ctxA.Done():
	default:
		t.Fatal("owner A background context was not canceled")
	}
	select {
	case <-ctxB.Done():
		t.Fatal("owner B background context was canceled by owner A")
	default:
	}
}

func TestBackgroundLLMCancelReplacingSameOwnerCancelsPrevious(t *testing.T) {
	h := &IMMessageHandler{}
	owner := "desktop-user:C:/tasks/same"

	ctxOld, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	ctxNew, cancelNew := context.WithCancel(context.Background())
	defer cancelNew()

	h.storeBackgroundLLMCancelForOwner(owner, cancelOld)
	h.storeBackgroundLLMCancelForOwner(owner, cancelNew)

	select {
	case <-ctxOld.Done():
	default:
		t.Fatal("replacing same owner did not cancel previous background context")
	}
	select {
	case <-ctxNew.Done():
		t.Fatal("new background context was canceled while storing replacement")
	default:
	}
}
