package database

import "testing"

func TestHostReadGrantRequiresHostTokens(t *testing.T) {
	if HostReadGrant("查看 rapidbi库", nil) {
		t.Fatal("no configured source must not grant")
	}
}

func TestHostReadGrantNamedCatalog(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242", "192.168.1.242", "rapidbi", "mysql"}
	if !HostReadGrant("查看 rapidbi库", tokens) {
		t.Fatal("host-owned catalog name must grant")
	}
	if !HostReadGrant("查看 192.168.1.242 上的库", tokens) {
		t.Fatal("host-owned address must grant")
	}
}

func TestHostReadGrantInspectShapeWithProfiles(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242", "192.168.1.242"}
	if !HostReadGrant("查看库", tokens) {
		t.Fatal("catalog inspect with configured sources must grant")
	}
	if HostReadGrant("optimize the database queries", tokens) {
		t.Fatal("code-maintenance wording must not grant")
	}
}

func TestHostReadGrantRejectsGitAndKnowledge(t *testing.T) {
	tokens := []string{"rapidbi", "192.168.1.242"}
	for _, msg := range []string{
		"查看当前 Git 仓库状态",
		"帮我看下现在的 git diff",
		"查看知识库",
	} {
		if HostReadGrant(msg, tokens) {
			t.Fatalf("%q must not grant SQL read", msg)
		}
	}
}

func TestHostReadGrantDoesNotTripOnEnglishSubstrings(t *testing.T) {
	tokens := []string{"rapidbi", "192.168.1.242"}
	if HostReadGrant("the digital design looks different", tokens) {
		t.Fatal("digital/difference must not count as git inspect or catalog inspect")
	}
	if !HostReadGrant("pull rows from rapidbi", tokens) {
		t.Fatal("host catalog name should still grant when git substring pull is present")
	}
}

func TestHostReadGrantRejectsAssetLibraries(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242", "192.168.1.242"}
	for _, msg := range []string{
		"查看素材库", "查看组件库", "查看图库", "查看库存", "查看库房", "查看库位", "列出表格",
	} {
		if HostReadGrant(msg, tokens) {
			t.Fatalf("%q must not grant SQL read", msg)
		}
	}
}

func TestHostReadGrantBareKuAllowsParticles(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242", "192.168.1.242"}
	if !HostReadGrant("查看库的表", tokens) {
		t.Fatal("查看库的表 is catalog inspect")
	}
	if !HostReadGrant("列出 库", tokens) {
		t.Fatal("列出 库 is catalog inspect")
	}
}

func TestHostReadGrantEnglishPhrasesAreBounded(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242"}
	if HostReadGrant("inspect schematic drawings", tokens) {
		t.Fatal("schematic must not count as inspect schema")
	}
	if HostReadGrant("inspect the schema of this protobuf", tokens) {
		t.Fatal("protobuf schema must not grant SQL read")
	}
	if !HostReadGrant("inspect schema for the app", tokens) {
		t.Fatal("inspect schema must grant")
	}
}

func TestHostReadGrantIPIsBounded(t *testing.T) {
	tokens := []string{"192.168.1.242"}
	if HostReadGrant("ping 192.168.1.2420", tokens) {
		t.Fatal("IP must not match as a prefix of a longer address")
	}
	if HostReadGrant("ping 192.168.1.242", tokens) {
		t.Fatal("pinging a database host must not grant SQL read")
	}
	if HostReadGrant("ssh 192.168.1.242", tokens) {
		t.Fatal("ssh to a database host must not grant SQL read")
	}
	if !HostReadGrant("查看 192.168.1.242", tokens) {
		t.Fatal("exact host address with inspect wording must grant")
	}
	if !HostReadGrant("inspect 192.168.1.242", tokens) {
		t.Fatal("inspect + host address must grant")
	}
}

func TestHostReadGrantDataWarehouse(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242", "192.168.1.242"}
	if !HostReadGrant("查看数据仓库", tokens) {
		t.Fatal("数据仓库 is SQL catalog inspect, not a git repo")
	}
	if HostReadGrant("查看代码仓库", tokens) {
		t.Fatal("代码仓库 must still fail closed")
	}
	if HostReadGrant("这个不是数据仓库，是代码仓库", tokens) {
		t.Fatal("代码仓库 must veto even when 数据仓库 is mentioned")
	}
}

func TestHostReadGrantTableSchemaNotReport(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242"}
	if HostReadGrant("这个报表结构不对", tokens) {
		t.Fatal("报表结构 must not grant SQL read")
	}
	if !HostReadGrant("查看表结构", tokens) {
		t.Fatal("查看表结构 is catalog inspect")
	}
}

func TestHostReadGrantTwoCharHanCatalog(t *testing.T) {
	tokens := []string{"订单"}
	if !HostReadGrant("订单", tokens) {
		t.Fatal("naming the Han catalog alone must grant")
	}
	if !HostReadGrant("查一下订单", tokens) {
		t.Fatal("two-character Han catalog names must grant with 查")
	}
	if HostReadGrant("订单位置在哪", tokens) {
		t.Fatal("订单 as a prefix of 订单位置 must not grant")
	}
	if HostReadGrant("生产订单", tokens) {
		t.Fatal("订单 as a suffix of 生产订单 must not grant")
	}
}

func TestHostReadGrantUnknownLatinKuDoesNotGrant(t *testing.T) {
	tokens := []string{"mysql-192-168-1-242", "192.168.1.242"}
	if HostReadGrant("查看 lodash库", tokens) {
		t.Fatal("latin+库 must not grant unless the identifier is a host-owned token")
	}
}

func TestHostReadGrantRequiresBoundedTokenHit(t *testing.T) {
	tokens := []string{"app"}
	if HostReadGrant("this application is broken", tokens) {
		t.Fatal("short token app must not match application")
	}
	if HostReadGrant("open the app", tokens) {
		t.Fatal("short token app must not grant on mention alone")
	}
	if HostReadGrant("inspect the app", tokens) {
		t.Fatal("inspect the app is not SQL catalog inspect")
	}
	if !HostReadGrant("查看 app库", tokens) {
		t.Fatal("app followed by 库 should match the host token")
	}
}

func TestHostReadGrantShortEnglishCatalogNeedsContext(t *testing.T) {
	tokens := []string{"test", "user"}
	if HostReadGrant("the test failed", tokens) {
		t.Fatal("common word test must not grant on mention")
	}
	if HostReadGrant("the user is offline", tokens) {
		t.Fatal("common word user must not grant on mention")
	}
	if !HostReadGrant("查看 test库", tokens) {
		t.Fatal("short catalog name with inspect wording must grant")
	}
}

func TestManagerHostReadGrantHonorsKillSwitch(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "mysql-192-168-1-242", Type: SourceMySQL, Host: "192.168.1.242", Database: "mysql",
	}}, nil)
	if !m.HostReadGrant("查看库") {
		t.Fatal("enabled manager with profiles should grant catalog inspect")
	}
	if !m.HostReadGrant("查看 192.168.1.242") {
		t.Fatal("enabled manager should grant on a configured host address")
	}
	m.SetEnabled(false)
	if m.HostReadGrant("查看库") {
		t.Fatal("kill switch must fail closed")
	}
}
