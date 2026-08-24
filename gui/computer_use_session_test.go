package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

func resetComputerUseRuntimeForTest(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.session = nil
		globalComputerUse.sessions = nil
		globalComputerUse.sessionUsed = nil
		globalComputerUse.activeOwner = ""
		globalComputerUse.taskStates = nil
		globalComputerUse.horizonClaimOnly = nil
		globalComputerUse.mu.Unlock()
	})
	globalComputerUse.mu.Lock()
	globalComputerUse.session = nil
	globalComputerUse.sessions = nil
	globalComputerUse.sessionUsed = nil
	globalComputerUse.activeOwner = ""
	globalComputerUse.taskStates = nil
	globalComputerUse.horizonClaimOnly = nil
	globalComputerUse.mu.Unlock()
}

func TestComputerUseSessionPerOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)

	setComputerUseOwner("tab-a")
	a := cuSession()
	a.Pause()

	setComputerUseOwner("tab-b")
	b := cuSession()
	if a == b {
		t.Fatal("owners must not share a Computer Use session")
	}
	if cuSessionForOwner("tab-a") != a {
		t.Fatal("lookup-only must return existing owner session")
	}
	if cuSessionForOwner("missing-owner") != nil {
		t.Fatal("lookup-only must not create sessions")
	}
	if computerUseOwnerKey() != "tab-b" {
		t.Fatalf("lookup mutated active owner: %s", computerUseOwnerKey())
	}
	paused, _ := b.ControlState()
	if paused {
		t.Fatal("tab-b inherited tab-a pause")
	}

	app := &App{}
	if err := app.ComputerUsePause(); err != nil {
		t.Fatal(err)
	}
	setComputerUseOwner("tab-a")
	if p, _ := cuSession().ControlState(); !p {
		t.Fatal("operator pause must affect tab-a")
	}
	setComputerUseOwner("tab-b")
	if p, _ := cuSession().ControlState(); !p {
		t.Fatal("operator pause must affect tab-b")
	}
}

func TestComputerUseDefaultOwnerHonorsDirectSessionAssign(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	globalComputerUse.mu.Lock()
	globalComputerUse.session = sess
	globalComputerUse.mu.Unlock()
	got := cuSession()
	if got != sess {
		t.Fatal("tests that assign globalComputerUse.session must keep working")
	}
}
