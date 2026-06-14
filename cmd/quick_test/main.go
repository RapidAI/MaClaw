package main

import (
	"fmt"
	"sort"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func main() {
	reg := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(reg)
	tests := []string{"杰青评审", "杰青申请", "优青评审", "优青申请", "面上评审", "面上项目申请", "青基评审", "重点项目评审"}
	for _, t := range tests {
		ranked := reg.RankedByText(t)
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
		m := reg.MatchByText(t)
		name := "NO MATCH"
		if m != nil {
			name = m.Type
		}
		fmt.Printf("%-20s → %-35s", t, name)
		if len(ranked) >= 2 {
			fmt.Printf(" (%.1f vs %.1f ratio=%.2f)", ranked[0].Score, ranked[1].Score, ranked[1].Score/ranked[0].Score)
		}
		fmt.Println()
	}
}
