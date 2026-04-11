package recommend

import (
	"testing"
)

var testColleagues = []ColleagueProfile{
	{ID: "xiaodi", Name: "小迪", RoleCode: "office", RoleName: "办公同事",
		Strengths: []string{"通知", "纪要", "周报", "邮件"}, Tasks: []string{"写通知", "会议纪要", "周报总结", "邮件草稿"}},
	{ID: "aning", Name: "阿宁", RoleCode: "data", RoleName: "数据同事",
		Strengths: []string{"表格整理", "数据汇总", "图表分析"}, Tasks: []string{"整理表格", "汇总数据", "生成图表", "写分析摘要"}},
	{ID: "laochen", Name: "老陈", RoleCode: "production", RoleName: "生产同事",
		Strengths: []string{"生产日报", "交接班", "异常汇总"}, Tasks: []string{"生产日报", "交接班记录", "异常说明", "上报摘要"}},
	{ID: "xiaozhou", Name: "小周", RoleCode: "quality", RoleName: "质量同事",
		Strengths: []string{"质量说明", "原因分析", "整改建议"}, Tasks: []string{"质量说明", "问题归类", "整改建议", "原因分析"}},
}

func TestRecommend_OfficeTask(t *testing.T) {
	recs := Recommend("帮我写一份会议纪要", testColleagues, 3)
	if len(recs) == 0 {
		t.Fatal("expected recommendations")
	}
	if recs[0].ColleagueID != "xiaodi" {
		t.Errorf("expected 小迪 first, got %s", recs[0].Name)
	}
}

func TestRecommend_DataTask(t *testing.T) {
	recs := Recommend("整理这个月的销售数据表格", testColleagues, 3)
	if len(recs) == 0 {
		t.Fatal("expected recommendations")
	}
	if recs[0].ColleagueID != "aning" {
		t.Errorf("expected 阿宁 first, got %s", recs[0].Name)
	}
}

func TestRecommend_ProductionTask(t *testing.T) {
	recs := Recommend("今天产线A有异常，需要写生产日报", testColleagues, 3)
	if len(recs) == 0 {
		t.Fatal("expected recommendations")
	}
	if recs[0].ColleagueID != "laochen" {
		t.Errorf("expected 老陈 first, got %s", recs[0].Name)
	}
}

func TestRecommend_QualityTask(t *testing.T) {
	recs := Recommend("质检发现了几个问题需要整改", testColleagues, 3)
	if len(recs) == 0 {
		t.Fatal("expected recommendations")
	}
	if recs[0].ColleagueID != "xiaozhou" {
		t.Errorf("expected 小周 first, got %s", recs[0].Name)
	}
}

func TestRecommend_EmptyInput(t *testing.T) {
	recs := Recommend("", testColleagues, 3)
	if len(recs) != 0 {
		t.Errorf("expected empty for empty input, got %d", len(recs))
	}
}

func TestRecommend_NoColleagues(t *testing.T) {
	recs := Recommend("写通知", nil, 3)
	if len(recs) != 0 {
		t.Errorf("expected empty for no colleagues, got %d", len(recs))
	}
}

func TestRecommend_TopN(t *testing.T) {
	recs := Recommend("帮我处理一些工作", testColleagues, 2)
	if len(recs) > 2 {
		t.Errorf("expected at most 2, got %d", len(recs))
	}
}

func TestRecommend_HasReason(t *testing.T) {
	recs := Recommend("写周报", testColleagues, 1)
	if len(recs) == 0 {
		t.Fatal("expected at least 1 recommendation")
	}
	if recs[0].Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestRecommend_MixedTask(t *testing.T) {
	// A task that mentions both quality and production keywords
	recs := Recommend("产线质量问题需要分析原因并写异常报告", testColleagues, 3)
	if len(recs) < 2 {
		t.Fatalf("expected at least 2 recommendations, got %d", len(recs))
	}
	// Both 小周 and 老陈 should appear in top results
	ids := map[string]bool{}
	for _, r := range recs {
		ids[r.ColleagueID] = true
	}
	if !ids["xiaozhou"] {
		t.Error("expected 小周 in recommendations for quality+production task")
	}
	if !ids["laochen"] {
		t.Error("expected 老陈 in recommendations for quality+production task")
	}
}
