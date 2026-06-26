package brand

import "testing"

func TestCurrentBrandConfig(t *testing.T) {
	b := Current()
	switch b.ID {
	case "maclaw":
		if b.DisplayName != "MaClaw" || b.DisplayNameCN != "码卡龙" {
			t.Fatalf("default brand = (%q, %q), want MaClaw/码卡龙", b.DisplayName, b.DisplayNameCN)
		}
	case "qianxin":
		if b.DisplayName != "TigerClaw" || b.DisplayNameCN != "虎爪" {
			t.Fatalf("qianxin brand = (%q, %q), want TigerClaw/虎爪", b.DisplayName, b.DisplayNameCN)
		}
	case "metastaff":
		if b.DisplayName != "MetaStaff" || b.DisplayNameCN != "智员" || b.WindowTitle != "智员 MetaStaff" {
			t.Fatalf("metastaff brand = (%q, %q, %q), want MetaStaff/智员/智员 MetaStaff", b.DisplayName, b.DisplayNameCN, b.WindowTitle)
		}
	default:
		t.Fatalf("unexpected brand id %q", b.ID)
	}
}
