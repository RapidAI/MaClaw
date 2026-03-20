package main

import (
	"fmt"
	"log"
	"strings"
)

// SecurityPolicyMode 安全策略模式。
type SecurityPolicyMode string

const (
	SecurityAllow SecurityPolicyMode = "allow"
	SecurityDeny  SecurityPolicyMode = "deny"
	SecurityAsk   SecurityPolicyMode = "ask"
)

// SkillSecurityPolicy MaClaw 端 Skill 安全策略配置。
type SkillSecurityPolicy struct {
	NetworkAccess    SecurityPolicyMode `json:"network_access"`
	FileSystemAccess SecurityPolicyMode `json:"file_system_access"`
	ShellExec        SecurityPolicyMode `json:"shell_exec"`
	DatabaseAccess   SecurityPolicyMode `json:"database_access"`
}

// DefaultSkillSecurityPolicy 返回默认安全策略。
func DefaultSkillSecurityPolicy() SkillSecurityPolicy {
	return SkillSecurityPolicy{
		NetworkAccess:    SecurityAsk,
		FileSystemAccess: SecurityAllow,
		ShellExec:        SecurityAsk,
		DatabaseAccess:   SecurityAllow,
	}
}

// SecurityPolicyChecker 检查 Skill 安全标签是否符合策略。
type SecurityPolicyChecker struct {
	policy SkillSecurityPolicy
	askFn  func(label, skillName string) bool // 询问用户的回调
}

// NewSecurityPolicyChecker 创建安全策略检查器。
func NewSecurityPolicyChecker(policy SkillSecurityPolicy, askFn func(label, skillName string) bool) *SecurityPolicyChecker {
	return &SecurityPolicyChecker{policy: policy, askFn: askFn}
}

// CheckLabels 检查 Skill 的安全标签是否允许执行。
// 返回 nil 表示允许，返回 error 表示拒绝。
func (c *SecurityPolicyChecker) CheckLabels(skillName string, labels []string) error {
	for _, label := range labels {
		mode := c.getModeForLabel(label)
		switch mode {
		case SecurityDeny:
			log.Printf("[security-policy] denied: skill=%s label=%s", skillName, label)
			return fmt.Errorf("security policy denied: %s requires %s", skillName, label)
		case SecurityAsk:
			if c.askFn != nil && !c.askFn(label, skillName) {
				log.Printf("[security-policy] user denied: skill=%s label=%s", skillName, label)
				return fmt.Errorf("user denied: %s requires %s", skillName, label)
			}
		case SecurityAllow:
			// pass
		}
	}
	return nil
}

func (c *SecurityPolicyChecker) getModeForLabel(label string) SecurityPolicyMode {
	label = strings.ToLower(label)
	switch label {
	case "network_access":
		return c.policy.NetworkAccess
	case "file_system_access":
		return c.policy.FileSystemAccess
	case "shell_exec":
		return c.policy.ShellExec
	case "database_access":
		return c.policy.DatabaseAccess
	default:
		return SecurityAsk // 未知标签默认询问
	}
}
