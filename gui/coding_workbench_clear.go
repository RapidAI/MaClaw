package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// Host-owned workspace clear.
//
// Permission mode and destructive classification are host policy. The model
// proposes work; it must not be the deny authority. A pure "清空当前目录"
// request therefore never enters the coding SubAgent loop: the workbench
// consults the same high-risk approval path as bash (request → prompt,
// full → auto-allow) and then deletes children of the frozen project path.
//
// The clear target is always the session project path, never a path parsed
// from the user text.

var codingWorkspaceClearCNMarkers = []string{
	"清空当前目录", "清空當前目錄", "清空这个目录", "清空這個目錄",
	"清空此目录", "清空此目錄", "清空本目录", "清空本目錄",
	"清空项目目录", "清空項目目錄", "清空工作区", "清空工作區",
	"清空文件夹", "清空資料夾", "清空整个目录", "清空整個目錄",
	"删光当前目录", "刪光當前目錄", "删除当前目录下", "刪除當前目錄下",
	"把当前目录清空", "把當前目錄清空",
}

var codingWorkspaceClearENPhrases = []string{
	"clear the current directory",
	"clear current directory",
	"clear the directory",
	"clear this directory",
	"empty the current directory",
	"empty the directory",
	"empty the folder",
	"wipe the current directory",
	"wipe the workspace",
	"wipe the directory",
	"delete everything in the current directory",
	"delete everything in the directory",
	"delete all files in the folder",
	"delete all files in the directory",
	"remove all files in the folder",
	"remove all files in the directory",
}

func codingRequestLooksExplicitWorkspaceClear(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" || codingWorkspaceClearLooksLikeQuestionOrNegation(text) {
		return false
	}
	compact := codingWorkspaceClearCompact(text)
	for _, marker := range codingWorkspaceClearCNMarkers {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	for _, phrase := range codingWorkspaceClearENPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func normalizeCodingWorkspaceClearText(userText string) string {
	text := strings.TrimSpace(userText)
	if strings.HasPrefix(text, codingPlanApproveExecuteMarker) {
		return strings.TrimSpace(strings.TrimPrefix(text, codingPlanApproveExecuteMarker))
	}
	return text
}

func forceWorkspaceClearCodingDecision(userText string, decision codingRequestDecision) codingRequestDecision {
	if !codingRequestLooksExplicitWorkspaceClear(normalizeCodingWorkspaceClearText(userText)) {
		return decision
	}
	return codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: false}
}

func codingTaskLooksWorkspaceClear(task *TaskItem) bool {
	if task == nil {
		return false
	}
	return codingRequestLooksExplicitWorkspaceClear(task.Title) ||
		codingRequestLooksExplicitWorkspaceClear(task.Description)
}

func codingRequestIsPureWorkspaceClear(userText string) bool {
	if !codingRequestLooksExplicitWorkspaceClear(userText) {
		return false
	}
	compact := codingWorkspaceClearCompact(strings.ToLower(strings.TrimSpace(userText)))
	for _, extra := range []string{"请帮我", "請幫我", "帮我", "幫我", "麻烦", "麻煩", "请", "請", "一下", "吧", "谢谢", "謝謝", "please", "pls"} {
		compact = strings.ReplaceAll(compact, extra, "")
	}
	for _, marker := range codingWorkspaceClearCNMarkers {
		compact = strings.ReplaceAll(compact, marker, "")
	}
	for _, phrase := range codingWorkspaceClearENPhrases {
		compact = strings.ReplaceAll(compact, strings.ReplaceAll(phrase, " ", ""), "")
	}
	compact = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, compact)
	return compact == ""
}

func codingWorkspaceClearLooksLikeQuestionOrNegation(text string) bool {
	compact := codingWorkspaceClearCompact(text)
	for _, deny := range []string{"怎么", "如何", "怎样", "howdo", "howto", "不要", "别清空", "don't", "do not", "dont"} {
		if strings.Contains(text, deny) || strings.Contains(compact, deny) {
			return true
		}
	}
	return strings.Contains(text, "how ")
}

func codingWorkspaceClearCompact(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

func codingWorkspaceClearRejected(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "工作区路径为空"
	}
	if projectPath == "." || projectPath == ".." {
		return "工作区路径不是绝对路径"
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return "无法解析工作区路径"
	}
	clean := filepath.Clean(abs)
	if !filepath.IsAbs(clean) {
		return "工作区路径不是绝对路径"
	}
	if filepath.Dir(clean) == clean {
		return "拒绝清空磁盘或文件系统根目录"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if filepath.Clean(home) == clean {
			return "拒绝清空用户主目录"
		}
	}
	parent := filepath.Dir(clean)
	if filepath.Dir(parent) == parent {
		switch strings.ToLower(filepath.Base(clean)) {
		case "windows", "program files", "program files (x86)", "programdata",
			"users", "home", "etc", "usr", "bin", "sbin", "var", "boot",
			"sys", "proc", "dev", "root", "tmp", "temp", "system", "libraries":
			return "拒绝清空系统目录 " + clean
		}
	}
	return ""
}

func codingRemoteWorkspaceClearRejected(projectDir string) string {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" || projectDir == "." || projectDir == ".." {
		return "远程工作区路径不是绝对路径"
	}
	if !path.IsAbs(projectDir) && !strings.Contains(projectDir, ":/") && !filepath.IsAbs(projectDir) {
		return "远程工作区路径不是绝对路径"
	}
	clean := path.Clean(strings.ReplaceAll(projectDir, "\\", "/"))
	if clean == "/" || len(clean) == 3 && strings.HasSuffix(clean, ":/") {
		return "拒绝清空远程文件系统根目录"
	}
	if path.Dir(clean) == "/" {
		switch strings.ToLower(path.Base(clean)) {
		case "home", "etc", "usr", "bin", "sbin", "var", "boot", "sys", "proc",
			"dev", "root", "tmp", "users", "windows":
			return "拒绝清空远程系统目录 " + clean
		}
	}
	return ""
}

func clearLocalCodingWorkspaceContents(projectPath string) (removed []string, failed []string, err error) {
	if reason := codingWorkspaceClearRejected(projectPath); reason != "" {
		return nil, nil, fmt.Errorf("%s", reason)
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("工作区不是目录")
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		target := filepath.Join(abs, name)
		rel, relErr := filepath.Rel(abs, target)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			failed = append(failed, name+": escaped workspace")
			continue
		}
		if removeErr := os.RemoveAll(target); removeErr != nil {
			failed = append(failed, name+": "+removeErr.Error())
			continue
		}
		removed = append(removed, name)
	}
	return removed, failed, nil
}

func (h *IMMessageHandler) tryHostClearCodingWorkspace(
	userID, userText, projectPath string,
	loopCtx *LoopContext,
	onProgress func(string),
	onToken func(string),
	remote *remoteCodingTemplateContext,
) *IMAgentResponse {
	if h == nil {
		return nil
	}
	userText = normalizeCodingWorkspaceClearText(userText)
	if !codingRequestIsPureWorkspaceClear(userText) {
		if codingRequestLooksExplicitWorkspaceClear(userText) {
			log.Printf("[coding-env] host workspace clear skipped: mixed request text=%q", truncateRunes(userText, 80))
		}
		return nil
	}
	log.Printf("[coding-env] host workspace clear intercept user=%s remote=%v path=%s", userID, remote != nil, projectPath)
	if loopCtx != nil && loopCtx.IsCancelled() {
		return &IMAgentResponse{Text: "编码任务已取消"}
	}

	isRemote := remote != nil
	if isRemote {
		if reason := codingRemoteWorkspaceClearRejected(projectPath); reason != "" {
			return &IMAgentResponse{Text: reason + "。未清空。"}
		}
	} else if reason := codingWorkspaceClearRejected(projectPath); reason != "" {
		return &IMAgentResponse{Text: reason + "。未清空。"}
	}

	if denied := h.approveCodingWorkspaceClear(userID, projectPath, loopCtx, onProgress); denied != "" {
		text := "未授权，未清空当前目录。"
		if onToken != nil {
			onToken(text)
		}
		return &IMAgentResponse{Text: text}
	}

	if onProgress != nil {
		onProgress("正在清空工作区目录（宿主执行，不经过模型）")
	}

	var text string
	if isRemote {
		text = h.executeRemoteCodingWorkspaceClear(remote, projectPath)
	} else {
		text = executeLocalCodingWorkspaceClear(projectPath)
	}
	if onToken != nil {
		onToken(text)
	}
	h.discardStickyCodingPlansAfterHostClear(userID)
	log.Printf("[coding-env] host workspace clear user=%s remote=%v path=%s", userID, isRemote, projectPath)
	return &IMAgentResponse{Text: text}
}

func (h *IMMessageHandler) discardStickyCodingPlansAfterHostClear(userID string) {
	if h == nil {
		return
	}
	h.takeStickyApprovedCodingPlan(userID)
	h.clearStickyPendingCodingPlan(userID)
}

func (h *IMMessageHandler) approveCodingWorkspaceClear(userID, projectPath string, loopCtx *LoopContext, onProgress func(string)) string {
	globalFull := false
	if h != nil && h.app != nil {
		globalFull = h.app.isSubAgentFullAccessGranted()
	}
	fullAccess := false
	if h != nil {
		fullAccess = h.stickyCodingEffectiveFullAccess(userID, globalFull)
	}
	var callback ScopeApprovalCallback
	if h != nil && h.app != nil {
		callback = buildSubAgentScopeApprovalCallback(h, loopCtx, onProgress)
	}
	state := newScopeApprovalState(callback, fullAccess)
	rejection := fmt.Sprintf("清空工作区目录（删除该目录下全部内容，保留目录本身）：%s", projectPath)
	return state.checkHighRisk("clear_workspace", projectPath, projectPath, projectPath, rejection)
}

func executeLocalCodingWorkspaceClear(projectPath string) string {
	removed, failed, err := clearLocalCodingWorkspaceContents(projectPath)
	if err != nil {
		return "清空失败：" + err.Error()
	}
	return formatCodingWorkspaceClearResult(projectPath, removed, failed)
}

func (h *IMMessageHandler) executeRemoteCodingWorkspaceClear(remote *remoteCodingTemplateContext, projectDir string) string {
	if h == nil || remote == nil || strings.TrimSpace(remote.SessionID) == "" {
		return "远程 SSH 会话不可用，未清空。"
	}
	quoted := remoteShellQuote(projectDir)
	cmd := fmt.Sprintf(
		`set -eu; DIR=%s; cd -- "$DIR"; find . -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +; echo __MACLAW_WS_CLEAR__`,
		quoted,
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   remote.SessionID,
		"command":      cmd,
		"working_dir":  projectDir,
		"wait_seconds": float64(60),
	})
	if !strings.Contains(out, "__MACLAW_WS_CLEAR__") {
		return "远程清空未完成：\n" + strings.TrimSpace(out)
	}
	return "已清空远程工作区目录 " + projectDir + "。"
}

func formatCodingWorkspaceClearResult(projectPath string, removed, failed []string) string {
	var b strings.Builder
	b.WriteString("已清空工作区目录 ")
	b.WriteString(projectPath)
	b.WriteString("（保留目录本身）")
	if len(removed) == 0 && len(failed) == 0 {
		b.WriteString("：本来就是空的。")
		return b.String()
	}
	b.WriteString("。")
	if len(removed) > 0 {
		b.WriteString("\n已删除：")
		b.WriteString(strings.Join(removed, "、"))
		b.WriteString("。")
	}
	if len(failed) > 0 {
		b.WriteString("\n未删除：")
		b.WriteString(strings.Join(failed, "；"))
		b.WriteString("。")
	}
	return b.String()
}
