package main

import "strings"

type asyncWaitAction string

const (
	asyncWaitActionUnknown asyncWaitAction = ""
	asyncWaitActionCheck   asyncWaitAction = "check"
	asyncWaitActionWait    asyncWaitAction = "wait"
	asyncWaitActionKill    asyncWaitAction = "kill"
	asyncWaitActionList    asyncWaitAction = "list"
)

func normalizeAsyncWaitAction(action string) asyncWaitAction {
	switch asyncWaitAction(strings.ToLower(strings.TrimSpace(action))) {
	case "", asyncWaitActionCheck:
		return asyncWaitActionCheck
	case asyncWaitActionWait:
		return asyncWaitActionWait
	case asyncWaitActionKill:
		return asyncWaitActionKill
	case asyncWaitActionList:
		return asyncWaitActionList
	default:
		return asyncWaitAction(strings.TrimSpace(action))
	}
}
