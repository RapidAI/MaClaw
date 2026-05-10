package httpapi

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAdminPageHAStaticContract(t *testing.T) {
	content, err := os.ReadFile("../../web/admin/index.html")
	if err != nil {
		t.Fatalf("read admin page: %v", err)
	}
	if !utf8.Valid(content) {
		t.Fatal("admin page is not valid UTF-8")
	}

	html := string(content)
	for _, marker := range []string{"\ufffd", "\u951f", "\u95bf", "\u93b7"} {
		if strings.Contains(html, marker) {
			t.Fatalf("admin page contains mojibake marker %q", marker)
		}
	}

	required := []string{
		`<button data-tab="ha"`,
		`id="haConfigReadinessBadge"`,
		`id="haRuntimeConfigBadge"`,
		`id="haClusterSecret"`,
		`id="haPeerConfigRows" class="ha-peer-config-list"`,
		`id="haNodePlanGrid" class="ha-node-plan-grid"`,
		`function renderHAConfigReadiness(cfg)`,
		`function generateHAClusterSecret()`,
		`function applyHAClusterTemplate(nodeID)`,
		`function detectHARuntimeDifferences(savedCfg, runtimeStatus)`,
		`function renderHANodePlans(cfg)`,
		`function copyHANodeYaml(nodeID)`,
		`function copyHANodeChecklist(nodeID)`,
		`const haNodeTemplates = {`,
		`haReadinessReady: 'Saved HA config includes the key fields needed for a 3-node rollout. Restart after saving on all nodes.'`,
		`haNodePlanTitle: '3-Node Deployment Cards'`,
		"navHA: '\\u591a\\u673a\\u70ed\\u5907'",
		`id="deleteFlaggedGossipBtn" onclick="deleteFlaggedGossipPosts()"`,
		`function deleteFlaggedGossipPosts()`,
		`deleteFlaggedConfirm:'Delete all flagged gossip posts? This cannot be undone.'`,
		`deleteFlagged:'\u5220\u9664\u5df2\u5ba1\u6838'`,
	}
	for _, snippet := range required {
		if !strings.Contains(html, snippet) {
			t.Fatalf("admin page missing HA snippet: %s", snippet)
		}
	}

	altRequired := [][2]string{
		{`haRuntimeMatches: '\u5f53\u524d\u8fd0\u884c\u4e2d\u7684\u70ed\u5907\u5173\u952e\u53c2\u6570\u4e0e\u672c\u9875\u5df2\u4fdd\u5b58\u914d\u7f6e\u4e00\u81f4\u3002'`, "haRuntimeMatches: '当前运行中的热备关键参数与本页已保存配置一致。'"},
		{`haNodePlanTitle: '\u4e09\u8282\u70b9\u90e8\u7f72\u5361\u7247'`, "haNodePlanTitle: '三节点部署卡片'"},
	}
	for _, pair := range altRequired {
		if !strings.Contains(html, pair[0]) && !strings.Contains(html, pair[1]) {
			t.Fatalf("admin page missing HA snippet: %s OR %s", pair[0], pair[1])
		}
	}

	assertHATemplate(t, html, "hc-1", "HubCenter 1", "https://hubs.mypapers.top", []haPeerTemplate{
		{nodeID: "hc-2", baseURL: "https://hubs.maclaw.top"},
		{nodeID: "hc-3", baseURL: "https://hubs2.maclaw.top"},
	})
	assertHATemplate(t, html, "hc-2", "HubCenter 2", "https://hubs.maclaw.top", []haPeerTemplate{
		{nodeID: "hc-1", baseURL: "https://hubs.mypapers.top"},
		{nodeID: "hc-3", baseURL: "https://hubs2.maclaw.top"},
	})
	assertHATemplate(t, html, "hc-3", "HubCenter 3", "https://hubs2.maclaw.top", []haPeerTemplate{
		{nodeID: "hc-1", baseURL: "https://hubs.mypapers.top"},
		{nodeID: "hc-2", baseURL: "https://hubs.maclaw.top"},
	})
}

type haPeerTemplate struct {
	nodeID  string
	baseURL string
}

func assertHATemplate(t *testing.T, html, nodeID, nodeName, advertiseURL string, peers []haPeerTemplate) {
	t.Helper()
	_ = nodeName
	needle := "'" + nodeID + "': { node_id: '" + nodeID + "', advertise_url: '" + advertiseURL + "', peers: ["
	start := strings.Index(html, needle)
	if start < 0 {
		t.Fatalf("missing HA template header for %s", nodeID)
	}
	end := strings.Index(html[start:], "]}")
	if end < 0 {
		t.Fatalf("missing HA template closing bracket for %s", nodeID)
	}
	block := html[start : start+end]
	if strings.Contains(block, "node_id: '"+nodeID+"', name:") {
		t.Fatalf("HA template %s lists itself as a peer", nodeID)
	}
	for _, peer := range peers {
		peerNeedle := "{ enabled: true, node_id: '" + peer.nodeID + "', base_url: '" + peer.baseURL + "' }"
		if !strings.Contains(block, peerNeedle) {
			t.Fatalf("HA template %s missing peer %s", nodeID, peer.nodeID)
		}
	}
	for _, snippet := range []string{
		`function detectHARuntimeDifferences(savedCfg, runtimeStatus)`,
		`function renderHANodePlans(cfg)`,
		`function copyHANodeYaml(nodeID)`,
		`function copyHANodeChecklist(nodeID)`,
	} {
		if strings.Count(html, snippet) != 1 {
			t.Fatalf("admin page should contain exactly one %q, got %d", snippet, strings.Count(html, snippet))
		}
	}
}
