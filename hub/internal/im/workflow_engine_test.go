package im

import (
	"strings"
	"testing"
)

func TestIsAdvanceTrigger(t *testing.T) {
	positives := []string{"下一步", "确认", "继续", "next", "ok", "好的", "可以", "没问题", "通过"}
	for _, s := range positives {
		if !isAdvanceTrigger(s) {
			t.Errorf("isAdvanceTrigger(%q) = false, want true", s)
		}
	}
	negatives := []string{"改一下", "帮我修改", "跳过", "取消", "这个接口需要调整"}
	for _, s := range negatives {
		if isAdvanceTrigger(s) {
			t.Errorf("isAdvanceTrigger(%q) = true, want false", s)
		}
	}
}

func TestIsSkipTrigger(t *testing.T) {
	positives := []string{"跳过", "skip"}
	for _, s := range positives {
		if !isSkipTrigger(s) {
			t.Errorf("isSkipTrigger(%q) = false, want true", s)
		}
	}
	negatives := []string{"下一步", "取消", "改一下"}
	for _, s := range negatives {
		if isSkipTrigger(s) {
			t.Errorf("isSkipTrigger(%q) = true, want false", s)
		}
	}
}

func TestIsCancelTrigger(t *testing.T) {
	positives := []string{"取消", "cancel", "算了", "不做了"}
	for _, s := range positives {
		if !isCancelTrigger(s) {
			t.Errorf("isCancelTrigger(%q) = false, want true", s)
		}
	}
	negatives := []string{"下一步", "跳过", "改一下", "继续"}
	for _, s := range negatives {
		if isCancelTrigger(s) {
			t.Errorf("isCancelTrigger(%q) = true, want false", s)
		}
	}
}

func TestIsModifyRequest(t *testing.T) {
	positives := []string{"改一下接口设计", "加个登录功能", "这个需要调整"}
	for _, s := range positives {
		if !isModifyRequest(s) {
			t.Errorf("isModifyRequest(%q) = false, want true", s)
		}
	}
	negatives := []string{"下一步", "跳过", "取消", "ok", "好的"}
	for _, s := range negatives {
		if isModifyRequest(s) {
			t.Errorf("isModifyRequest(%q) = true, want false", s)
		}
	}
}

func TestFormatPhaseOutput(t *testing.T) {
	checks := []CheckResult{
		{Item: "功能完整性", Status: "pass", Detail: "符合要求"},
		{Item: "边界情况", Status: "warn", Detail: "缺少错误处理"},
		{Item: "安全性", Status: "fail", Detail: "未考虑SQL注入"},
	}
	phase := PhaseTemplate{
		Name:         "需求分析",
		NeedsConfirm: true,
	}

	output := formatPhaseOutput("这是产出物内容", checks, phase)

	if output == "" {
		t.Fatal("output is empty")
	}
	if !strings.Contains(output, "✅") {
		t.Error("missing pass icon ✅")
	}
	if !strings.Contains(output, "⚠️") {
		t.Error("missing warn icon ⚠️")
	}
	if !strings.Contains(output, "❌") {
		t.Error("missing fail icon ❌")
	}
	if !strings.Contains(output, "下一步") {
		t.Error("missing action hint")
	}
}
