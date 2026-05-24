package main

import "strings"

type sshToolAction string

const (
	sshToolActionUnknown        sshToolAction = ""
	sshToolActionConnect        sshToolAction = "connect"
	sshToolActionExec           sshToolAction = "exec"
	sshToolActionExecBackground sshToolAction = "exec_background"
	sshToolActionCheckTask      sshToolAction = "check_task"
	sshToolActionWaitTask       sshToolAction = "wait_task"
	sshToolActionListTasks      sshToolAction = "list_tasks"
	sshToolActionKillTask       sshToolAction = "kill_task"
	sshToolActionSudoPrepare    sshToolAction = "sudo_prepare"
	sshToolActionUpload         sshToolAction = "upload"
	sshToolActionDownload       sshToolAction = "download"
	sshToolActionList           sshToolAction = "list"
	sshToolActionClose          sshToolAction = "close"
	sshToolActionCloseAll       sshToolAction = "close_all"
)

func classifySSHToolAction(action string) sshToolAction {
	switch sshToolAction(strings.TrimSpace(action)) {
	case sshToolActionConnect:
		return sshToolActionConnect
	case sshToolActionExec:
		return sshToolActionExec
	case sshToolActionExecBackground:
		return sshToolActionExecBackground
	case sshToolActionCheckTask:
		return sshToolActionCheckTask
	case sshToolActionWaitTask:
		return sshToolActionWaitTask
	case sshToolActionListTasks:
		return sshToolActionListTasks
	case sshToolActionKillTask:
		return sshToolActionKillTask
	case sshToolActionSudoPrepare:
		return sshToolActionSudoPrepare
	case sshToolActionUpload:
		return sshToolActionUpload
	case sshToolActionDownload:
		return sshToolActionDownload
	case sshToolActionList:
		return sshToolActionList
	case sshToolActionClose:
		return sshToolActionClose
	case sshToolActionCloseAll:
		return sshToolActionCloseAll
	default:
		return sshToolAction(strings.TrimSpace(action))
	}
}
