package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// SecurityReadOnlyFn is set by the main package. When it returns true,
// security settings modifications are blocked (centralized security mode).
var SecurityReadOnlyFn func() bool

// RunPolicy 执行 policy 子命令。
func RunPolicy(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui policy <list>")
	}
	switch args[0] {
	case "list":
		return policyList(dataDir, args[1:])
	default:
		return NewUsageError("unknown policy action: %s", args[0])
	}
}

func policyList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("policy list", flag.ExitOnError)
	mode := fs.String("mode", "standard", "安全策略模式 (standard/relaxed/strict/developer)")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	rules := security.PolicyRulesForMode(*mode)
	if *jsonOut {
		return PrintJSON(rules)
	}
	if len(rules) == 0 {
		fmt.Println("No policy rules.")
		return nil
	}
	fmt.Printf("Security policy mode: %s\n\n", *mode)
	fmt.Printf("%-30s %-8s %-15s %-12s %s\n", "NAME", "PRI", "TOOL", "ACTION", "RISK LEVELS")
	fmt.Println(strings.Repeat("-", 85))
	for _, r := range rules {
		riskLevels := make([]string, len(r.RiskLevels))
		for i, rl := range r.RiskLevels {
			riskLevels[i] = string(rl)
		}
		fmt.Printf("%-30s %-8d %-15s %-12s %s\n",
			TruncateDisplay(r.Name, 30), r.Priority,
			TruncateDisplay(r.ToolPattern, 15), string(r.Action),
			strings.Join(riskLevels, ","))
	}
	return nil
}
