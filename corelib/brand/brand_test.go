package brand

import "testing"

func TestCurrentBrandConfig(t *testing.T) {
	b := Current()
	switch b.ID {
	case "maclaw":
		if b.DisplayName != "MaClaw" || b.DisplayNameCN != "码卡龙" {
			t.Fatalf("default brand = (%q, %q), want MaClaw/码卡龙", b.DisplayName, b.DisplayNameCN)
		}
		if b.WindowTitle != "码卡龙 8 MaClaw（企缘）" {
			t.Fatalf("default window title = %q, want 码卡龙 8 MaClaw（企缘）", b.WindowTitle)
		}
	case "qianxin":
		if b.DisplayName != "QAgent" || b.DisplayNameCN != "虎爪" {
			t.Fatalf("qianxin brand = (%q, %q), want QAgent/虎爪", b.DisplayName, b.DisplayNameCN)
		}
		if len(b.ExtraTools) != 1 || b.ExtraTools[0].Name != "QAgent" || b.ExtraTools[0].ConfigKey != "QAgent" {
			t.Fatalf("qianxin extra tool = %+v, want Name/ConfigKey QAgent", b.ExtraTools)
		}
	case "metastaff":
		if b.DisplayName != "MetaStaff" || b.DisplayNameCN != "智员" || b.WindowTitle != "智员 MetaStaff" {
			t.Fatalf("metastaff brand = (%q, %q, %q), want MetaStaff/智员/智员 MetaStaff", b.DisplayName, b.DisplayNameCN, b.WindowTitle)
		}
	default:
		t.Fatalf("unexpected brand id %q", b.ID)
	}
	if b.Slogan != "AI Native 组织操作系统" {
		t.Fatalf("brand %s slogan = %q, want AI Native 组织操作系统", b.ID, b.Slogan)
	}
}
