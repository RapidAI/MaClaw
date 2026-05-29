package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func readAdminPageBundle(t *testing.T) string {
	t.Helper()
	webRoot := filepath.Join("..", "..", "web")
	indexHTML := readAdminPageHTML(t)

	var b strings.Builder
	b.WriteString(indexHTML)
	b.WriteByte('\n')
	for _, ref := range adminPageBundleAssetRefs(t, indexHTML) {
		content, err := os.ReadFile(filepath.Join(webRoot, filepath.FromSlash(ref)))
		if err != nil {
			t.Fatalf("read admin asset %s: %v", ref, err)
		}
		b.Write(content)
		b.WriteByte('\n')
	}
	return b.String()
}

func readAdminPageHTML(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "index.html"))
	if err != nil {
		t.Fatalf("read admin page: %v", err)
	}
	return string(content)
}

func adminPageBundleAssetRefs(t *testing.T, html string) []string {
	t.Helper()
	assetRef := regexp.MustCompile(`(?:href|src)="/([^"]+)"`)
	matches := assetRef.FindAllStringSubmatch(html, -1)
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		ref := strings.SplitN(match[1], "?", 2)[0]
		if strings.HasPrefix(ref, "admin/assets/") || ref == "pro-ui.css" {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		t.Fatal("admin page should reference local stylesheet or script assets")
	}
	return refs
}

func adminPageAssetRefs(t *testing.T, html string) []string {
	t.Helper()
	assetRef := regexp.MustCompile(`(?:href|src)="/admin/([^"]+)"`)
	matches := assetRef.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("admin page should reference split /admin assets")
	}
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		refs = append(refs, strings.SplitN(match[1], "?", 2)[0])
	}
	return refs
}

func assertContainsAll(t *testing.T, haystack string, context string, snippets []string) {
	t.Helper()
	for _, snippet := range snippets {
		if !strings.Contains(haystack, snippet) {
			t.Fatalf("%s missing snippet: %s", context, snippet)
		}
	}
}

func TestAdminPageHAStaticContract(t *testing.T) {
	content := readAdminPageHTML(t)
	if !utf8.ValidString(content) {
		t.Fatal("admin page is not valid UTF-8")
	}

	html := readAdminPageBundle(t)
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
		`id="haPushDebounceSeconds"`,
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
		`haSummaryPushDebounceSeconds: 'push_debounce_seconds={value}'`,
		`push_debounce_seconds: Number(document.getElementById('haPushDebounceSeconds').value || '0')`,
		"navHA: '\\u591a\\u673a\\u70ed\\u5907'",
		`id="deleteFlaggedGossipBtn" onclick="deleteFlaggedGossipPosts()"`,
		`function deleteFlaggedGossipPosts()`,
		`deleteFlaggedConfirm:'Delete all flagged gossip posts? This cannot be undone.'`,
		`deleteFlagged:'\u5220\u9664\u5df2\u5ba1\u6838'`,
	}
	assertContainsAll(t, html, "admin page HA contract", required)

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
	if strings.Contains(html, `while(list.length < 3)`) {
		t.Fatal("HA peer config renderer must not pad saved peers with empty rows")
	}
}

func TestAdminPageReferencesExistingSplitAssets(t *testing.T) {
	adminRoot := filepath.Join("..", "..", "web", "admin")
	html := readAdminPageHTML(t)

	for _, ref := range adminPageAssetRefs(t, html) {
		assetPath := filepath.Join(adminRoot, filepath.FromSlash(ref))
		info, err := os.Stat(assetPath)
		if err != nil {
			t.Fatalf("admin page references missing asset %s: %v", ref, err)
		}
		if info.IsDir() || info.Size() == 0 {
			t.Fatalf("admin page asset %s must be a non-empty file", ref)
		}
	}
}

func TestAdminPageBundleIncludesSharedAndSplitAssets(t *testing.T) {
	refs := adminPageBundleAssetRefs(t, readAdminPageHTML(t))
	for _, want := range []string{
		"pro-ui.css",
		"admin/assets/css/admin-shell.css",
		"admin/assets/css/admin-responsive.css",
		"admin/assets/js/admin-core.js",
	} {
		found := false
		for _, ref := range refs {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("admin page bundle refs missing %s in %v", want, refs)
		}
	}
}

func TestAdminPageStylesheetOrder(t *testing.T) {
	html := readAdminPageHTML(t)
	assertContainsAll(t, html, "admin page stylesheet contract", []string{`href="/pro-ui.css"`, `href="/admin/assets/css/admin-shell.css"`, `href="/admin/assets/css/admin-responsive.css"`})
	shared := strings.Index(html, `href="/pro-ui.css"`)
	shell := strings.Index(html, `href="/admin/assets/css/admin-shell.css"`)
	responsive := strings.Index(html, `href="/admin/assets/css/admin-responsive.css"`)
	if !(shared < shell && shell < responsive) {
		t.Fatalf("admin stylesheet order must be shared baseline, shell, responsive; got indexes %d, %d, %d", shared, shell, responsive)
	}
}

func TestAdminPageKeepsStyleAndScriptSplit(t *testing.T) {
	html := readAdminPageHTML(t)
	bundle := readAdminPageBundle(t)
	if strings.Contains(html, "<style") {
		t.Fatal("admin page should keep CSS in split stylesheet assets")
	}
	if strings.Contains(bundle, `style=`) || strings.Contains(bundle, `style.cssText`) {
		t.Fatal("admin bundle should keep layout styling in reusable stylesheet classes")
	}
	scriptTag := regexp.MustCompile(`<script([^>]*)>`)
	for _, match := range scriptTag.FindAllStringSubmatch(html, -1) {
		if !strings.Contains(match[1], " src=") {
			t.Fatalf("admin page should keep JavaScript in split script assets, found %s", match[0])
		}
	}
}

func TestAdminPageSplitScriptsAreDeferred(t *testing.T) {
	matches := adminPageSplitScriptRefs(t, readAdminPageHTML(t))
	if len(matches) == 0 {
		t.Fatal("admin page should load split JavaScript assets")
	}
	for _, match := range matches {
		if !strings.Contains(match.attrs, " defer") {
			t.Fatalf("admin split script should use defer: %s", match.tag)
		}
	}
}

func TestAdminPageSplitScriptOrder(t *testing.T) {
	matches := adminPageSplitScriptRefs(t, readAdminPageHTML(t))
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match.src)
	}
	want := []string{
		"assets/js/admin-core.js",
		"assets/js/user-management.js",
		"assets/js/profile-settings.js",
		"assets/js/gossip-admin.js",
		"assets/js/skillmarket-admin.js",
		"assets/js/ha-news-admin.js",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("admin split script order = %v, want %v", got, want)
	}
}

type adminPageScriptRef struct {
	tag   string
	src   string
	attrs string
}

func adminPageSplitScriptRefs(t *testing.T, html string) []adminPageScriptRef {
	t.Helper()
	scriptRef := regexp.MustCompile(`<script\s+src="/admin/([^"]+)"([^>]*)></script>`)
	matches := scriptRef.FindAllStringSubmatch(html, -1)
	refs := make([]adminPageScriptRef, 0, len(matches))
	for _, match := range matches {
		src := strings.SplitN(match[1], "?", 2)[0]
		if !strings.HasPrefix(src, "assets/js/") {
			continue
		}
		refs = append(refs, adminPageScriptRef{tag: match[0], src: src, attrs: match[2]})
	}
	return refs
}

func TestAdminPageRoutePreviewStaticContract(t *testing.T) {
	html := readAdminPageBundle(t)

	assertContainsAll(t, html, "admin page route preview contract", []string{
		`id="hubRoutePreview" class="item hub-route-preview"`,
		`id="hubRoutePreviewList" class="hub-route-list"`,
		`const hubRoutePreviewPageSize=20`,
		`.hub-route-list{display:grid;grid-template-columns:repeat(4,minmax(0,1fr))`,
		`function changeHubRoutePage(delta)`,
		`id="hubRoutePreviewPageInfo"`,
	})
}

func TestAdminPageRouteQueryRendererIsSingleSource(t *testing.T) {
	html := readAdminPageBundle(t)
	if count := strings.Count(html, `function renderRouteQueryResult(meta,data)`); count != 1 {
		t.Fatalf("admin page should define renderRouteQueryResult once, got %d", count)
	}
	assertContainsAll(t, html, "admin route query renderer", []string{`tr('routeQueryHubDomain')`, `hub.corporate_email_domain||'-'`})
}

func TestAdminPageEscapesBlockedPolicyLists(t *testing.T) {
	html := readAdminPageBundle(t)
	assertContainsAll(t, html, "admin blocked policy escaping", []string{
		`escapeHtml(v.email||'')`,
		`escapeHtml(v.ip||'')`,
		`escapeHtml(v.reason||tr('noReason'))`,
	})
}

func TestAdminPageUsesSafeGossipCommentIDs(t *testing.T) {
	html := readAdminPageBundle(t)
	assertContainsAll(t, html, "admin gossip comment IDs", []string{
		`const gossipDomToken=value=>`,
		`const gossipCommentRootID=postId=>`,
		`document.getElementById(gossipCommentRootID(postId))`,
		`escapeHtml(gossipCommentRootID(p.id))`,
	})
	if strings.Contains(html, `id="gossip-comments-${p.id}"`) {
		t.Fatal("admin gossip comment containers must not embed raw post ids")
	}
}

func TestAdminPageDoesNotSerializeNewsRecordsIntoHandlers(t *testing.T) {
	html := readAdminPageBundle(t)
	assertContainsAll(t, html, "admin news editor handlers", []string{
		`let _newsArticles=[]`,
		`_newsArticles=articles`,
		`onclick="editNewsIndex(${idx})"`,
		`function editNewsIndex(idx)`,
	})
	if strings.Contains(html, `JSON.stringify(JSON.stringify(a))`) || strings.Contains(html, `function editNews(jsonStr)`) {
		t.Fatal("admin news editor must not serialize article payloads into inline handlers")
	}
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
