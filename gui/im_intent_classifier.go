package main

import (
	"fmt"
	"regexp"
	"strings"
)

type taskIntent string

const (
	intentCoding    taskIntent = "coding"
	intentSSH       taskIntent = "ssh"
	intentNonCoding taskIntent = "non_coding"
	intentAmbiguous taskIntent = "ambiguous"
	intentUnknown   taskIntent = "unknown"
)

type taskIntentResult struct {
	Intent   taskIntent
	Matched  string
	Evidence []string
}

var sshKeywords = []string{
	"ssh", "服务器", "服务端", "主机", "远程机器", "远程主机", "云服务器", "线上机器",
	"登录服务器", "连上服务器", "连接服务器", "远程登录", "看日志", "查看日志", "日志", "tail -f",
	"journalctl", "systemctl", "service ", "nginx", "docker", "docker compose", "k8s", "kubectl",
	"pm2", "supervisor", "重启服务", "重启 nginx", "重启进程", "上传到服务器", "下载服务器文件",
	"sftp", "scp", "rsync", "端口", "进程", "服务器文件", "服务器上", "远程执行",
	"host", "user", "label", "initial_command",
}

var ambiguousKeywords = []string{
	"部署", "deploy", "上线", "线上问题", "线上故障", "服务挂了", "服务异常", "环境问题",
	"处理一下线上问题", "看看服务", "看下服务", "排查一下", "处理一下这个项目",
}

var ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

func classifyTaskIntent(text string) taskIntentResult {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return taskIntentResult{Intent: intentUnknown}
	}

	codingHits := collectIntentMatches(msg, codingKeywords)
	sshHits := collectIntentMatches(msg, sshKeywords)
	nonCodingHits := collectIntentMatches(msg, nonCodingKeywords)
	ambiguousHits := collectIntentMatches(msg, ambiguousKeywords)
	if ipv4Pattern.MatchString(msg) {
		sshHits = appendIfMissing(sshHits, "ip")
	}

	hasCoding := len(codingHits) > 0
	hasSSH := len(sshHits) > 0
	hasNonCoding := len(nonCodingHits) > 0
	hasAmbiguous := len(ambiguousHits) > 0

	switch {
	case hasNonCoding && hasCoding && !hasSSH:
		if hasOnlyWeakCodingEvidence(codingHits) {
			return taskIntentResult{Intent: intentNonCoding, Matched: nonCodingHits[0], Evidence: combineEvidence(nonCodingHits, codingHits)}
		}
		return taskIntentResult{Intent: intentAmbiguous, Matched: firstMatch(nonCodingHits, codingHits), Evidence: combineEvidence(nonCodingHits, codingHits)}
	case hasCoding && !hasSSH && !hasAmbiguous:
		return taskIntentResult{Intent: intentCoding, Matched: codingHits[0], Evidence: codingHits}
	case hasSSH && !hasCoding && !hasNonCoding:
		return taskIntentResult{Intent: intentSSH, Matched: sshHits[0], Evidence: sshHits}
	case hasNonCoding && !hasCoding && !hasSSH:
		return taskIntentResult{Intent: intentNonCoding, Matched: nonCodingHits[0], Evidence: nonCodingHits}
	case hasSSH && hasCoding:
		return taskIntentResult{Intent: intentAmbiguous, Matched: firstMatch(ambiguousHits, sshHits, codingHits), Evidence: combineEvidence(sshHits, codingHits, ambiguousHits)}
	case hasAmbiguous:
		return taskIntentResult{Intent: intentAmbiguous, Matched: firstMatch(ambiguousHits, sshHits, codingHits, nonCodingHits), Evidence: combineEvidence(ambiguousHits, sshHits, codingHits, nonCodingHits)}
	case hasSSH:
		return taskIntentResult{Intent: intentSSH, Matched: sshHits[0], Evidence: sshHits}
	case hasNonCoding:
		return taskIntentResult{Intent: intentNonCoding, Matched: nonCodingHits[0], Evidence: nonCodingHits}
	case hasCoding:
		return taskIntentResult{Intent: intentCoding, Matched: codingHits[0], Evidence: codingHits}
	default:
		return taskIntentResult{Intent: intentAmbiguous}
	}
}

func hasOnlyWeakCodingEvidence(hits []string) bool {
	if len(hits) == 0 {
		return false
	}
	weak := map[string]struct{}{
		"编程": {},
		"代码": {},
		"测试": {},
	}
	for _, hit := range hits {
		if _, ok := weak[hit]; !ok {
			return false
		}
	}
	return true
}

func collectIntentMatches(msg string, keywords []string) []string {
	var hits []string
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			hits = appendIfMissing(hits, kw)
		}
	}
	return hits
}

func appendIfMissing(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func combineEvidence(groups ...[]string) []string {
	merged := make([]string, 0)
	for _, group := range groups {
		for _, item := range group {
			merged = appendIfMissing(merged, item)
		}
	}
	return merged
}

func firstMatch(groups ...[]string) string {
	for _, group := range groups {
		if len(group) > 0 {
			return group[0]
		}
	}
	return ""
}

func formatIntentEvidence(result taskIntentResult) string {
	if len(result.Evidence) == 0 {
		if result.Matched != "" {
			return fmt.Sprintf("%q", result.Matched)
		}
		return "未命中特征词"
	}
	if len(result.Evidence) == 1 {
		return fmt.Sprintf("%q", result.Evidence[0])
	}
	return fmt.Sprintf("%q", strings.Join(result.Evidence, `", "`))
}
