package httpapi

import (
	"bytes"
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

func readAdminAsset(t *testing.T, ref string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "web", filepath.FromSlash(ref)))
	if err != nil {
		t.Fatalf("read admin asset %s: %v", ref, err)
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
		{`haRuntimeMatches: '\u5f53\u524d\u8fd0\u884c\u4e2d\u7684\u70ed\u5907\u5173\u952e\u53c2\u6570\u4e0e\u672c\u9875\u5df2\u4fdd\u5b58\u914d\u7f6e\u4e00\u81f4\u3002'`, `haRuntimeMatches: '\u5f53\u524d\u8fd0\u884c\u4e2d\u7684\u70ed\u5907\u5173\u952e\u53c2\u6570\u4e0e\u672c\u9875\u5df2\u4fdd\u5b58\u914d\u7f6e\u4e00\u81f4\u3002'`},
		{`haNodePlanTitle: '\u4e09\u8282\u70b9\u90e8\u7f72\u5361\u7247'`, `haNodePlanTitle: '\u4e09\u8282\u70b9\u90e8\u7f72\u5361\u7247'`},
	}
	for _, pair := range altRequired {
		if !strings.Contains(html, pair[0]) && !strings.Contains(html, pair[1]) {
			t.Fatalf("admin page missing HA snippet: %s OR %s", pair[0], pair[1])
		}
	}

	assertHATemplate(t, html, "hc-1", "HubCenter 1", "http://hub.mypapers.top:9388", []haPeerTemplate{
		{nodeID: "hc-2", baseURL: "http://107.172.86.131:9388"},
		{nodeID: "hc-3", baseURL: "http://66.154.113.63:9388"},
	})
	assertHATemplate(t, html, "hc-2", "HubCenter 2", "http://107.172.86.131:9388", []haPeerTemplate{
		{nodeID: "hc-1", baseURL: "http://hub.mypapers.top:9388"},
		{nodeID: "hc-3", baseURL: "http://66.154.113.63:9388"},
	})
	assertHATemplate(t, html, "hc-3", "HubCenter 3", "http://66.154.113.63:9388", []haPeerTemplate{
		{nodeID: "hc-1", baseURL: "http://hub.mypapers.top:9388"},
		{nodeID: "hc-2", baseURL: "http://107.172.86.131:9388"},
	})
	if !strings.Contains(html, `id="haPullBatchSize" type="number" min="1" value="1000"`) {
		t.Fatal("HA config form must default pull_batch_size to 1000")
	}
	if !strings.Contains(html, `id="haSyncIntervalSeconds" type="number" min="1" value="5"`) ||
		!strings.Contains(html, `id="haPushDebounceSeconds" type="number" min="1" value="5"`) {
		t.Fatal("HA config form must default sync and push debounce intervals to 5 seconds")
	}
	if strings.Contains(html, `while(list.length < 3)`) {
		t.Fatal("HA peer config renderer must not pad saved peers with empty rows")
	}
}

func TestRenderedMypapersMaclawHAConfigsUseDirectFastSync(t *testing.T) {
	deployRoot := filepath.Join("..", "..", "..", "deploy")
	for _, fileName := range []string{"hubcenter-hc-1.yaml", "hubcenter-hc-2.yaml", "hubcenter-hc-3.yaml"} {
		path := filepath.Join(deployRoot, "out-mypapers-maclaw", fileName)
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read rendered HA config %s: %v", fileName, err)
		}
		if bytes.HasPrefix(contentBytes, []byte{0xEF, 0xBB, 0xBF}) {
			t.Fatalf("rendered HA config %s must be UTF-8 without BOM", fileName)
		}

		content := string(contentBytes)
		assertContainsAll(t, content, fileName, []string{
			"  sync_interval_seconds: 5",
			"  push_debounce_seconds: 5",
			"  pull_batch_size: 1000",
			"  nodes:",
			"      advertise_url: http://hub.mypapers.top:9388",
			"      advertise_url: http://107.172.86.131:9388",
			"      advertise_url: http://66.154.113.63:9388",
		})
		for _, stale := range []string{
			"  sync_interval_seconds: 180",
			"  push_debounce_seconds: 180",
			"  pull_batch_size: 200",
			"  peers:",
			"      base_url: https://hubs.",
		} {
			if strings.Contains(content, stale) {
				t.Fatalf("rendered HA config %s contains stale snippet %q", fileName, stale)
			}
		}
	}

	for _, fileName := range []string{"hub-mypapers.yaml", "hub-maclaw.yaml", "hub2-maclaw.yaml"} {
		path := filepath.Join(deployRoot, "out-mypapers-maclaw", fileName)
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read rendered hub config %s: %v", fileName, err)
		}
		if bytes.HasPrefix(contentBytes, []byte{0xEF, 0xBB, 0xBF}) {
			t.Fatalf("rendered hub config %s must be UTF-8 without BOM", fileName)
		}
		if !strings.Contains(string(contentBytes), "  accept_public_signup: false") {
			t.Fatalf("rendered hub config %s must keep public signup disabled unless inventory opts in", fileName)
		}
	}
}

func TestDeployAllHAUsesExplicitHubCenterDBPath(t *testing.T) {
	deployScriptPath := filepath.Join("..", "..", "..", "deploy", "deploy_all_ha.ps1")
	contentBytes, err := os.ReadFile(deployScriptPath)
	if err != nil {
		t.Fatalf("read deploy_all_ha.ps1: %v", err)
	}
	content := string(contentBytes)
	if strings.Contains(content, "$target.DatabaseDSN") {
		t.Fatal("deploy_all_ha.ps1 must not export HUBCENTER_DB_PATH from missing target.DatabaseDSN")
	}
	if count := strings.Count(content, "HubCenterDBPath = './data/codeclaw-hubcenter.db'"); count != 3 {
		t.Fatalf("deploy_all_ha.ps1 HubCenterDBPath target count = %d, want 3", count)
	}
	assertContainsAll(t, content, "deploy_all_ha.ps1", []string{
		"'  if [ -z \"$HUBCENTER_DB_PATH\" ]; then',",
		"'    echo \"[ERROR] HUBCENTER_DB_PATH is empty\" >&2',",
		`("export HUBCENTER_DB_PATH={0}" -f (Quote-ShellEnvValue $target.HubCenterDBPath))`,
	})
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
	assertContainsAll(t, html, "admin page stylesheet contract", []string{`href="/pro-ui.css"`, `href="/admin/assets/css/admin-shell.css`, `href="/admin/assets/css/admin-responsive.css"`})
	shared := strings.Index(html, `href="/pro-ui.css"`)
	shell := strings.Index(html, `href="/admin/assets/css/admin-shell.css`)
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
		"assets/js/petstore-admin.js",
		"assets/js/expertmarket-admin.js",
		"assets/js/industry-management-admin.js",
		"assets/js/ha-news-admin.js",
		"assets/js/llm-service-tab.js",
		"assets/js/compute-market-tab.js",
		"assets/js/user-rankings-tab.js",
		"assets/js/notification-admin.js",
		"assets/js/problem-reports-tab.js",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("admin split script order = %v, want %v", got, want)
	}
}

func TestPetStoreAdminUsesDelegatedCardActions(t *testing.T) {
	js := readAdminAsset(t, "admin/assets/js/petstore-admin.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")
	assertContainsAll(t, js, "pet store delegated card actions", []string{
		`data-pet-admin-action="preview"`,
		`data-pet-admin-action="pause"`,
		`data-pet-admin-action="resume"`,
		`data-pet-admin-action="delete"`,
		`data-pet-admin-action="purge"`,
		`function purgePetStorePack(id, button)`,
		`function bindPetStoreAdminActions()`,
		`function setPetStorePreviewVisibility(target, visible)`,
		`target.style.display = visible ? '' : 'none'`,
		`root.addEventListener('click'`,
		`return text.replace(/&/g, '&amp;')`,
	})
	assertContainsAll(t, readAdminPageHTML(t), "pet store cache version", []string{
		`/admin/assets/js/petstore-admin.js?v=pet-store-admin-20260801-2`,
	})
	if strings.Contains(js, `onclick="petAdminSetStatus(`) || strings.Contains(js, `onclick="togglePetStorePreview(`) || strings.Contains(js, `onclick="deletePetStorePack(`) {
		t.Fatal("pet store cards must not interpolate JavaScript into inline onclick handlers")
	}
	assertContainsAll(t, css, "pet store preview visibility", []string{
		`.pet-admin-preview[hidden]{display:none}`,
	})
}

func TestExpertMarketAdminUsesCompactDelegatedReviewCards(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/expertmarket-admin.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")

	assertContainsAll(t, html, "expert market cache version", []string{
		`/admin/assets/js/expertmarket-admin.js?v=expert-market-admin-20260804-3`,
	})
	if strings.Contains(html, `<option value="approved">`) {
		t.Fatal("expert market filter must not expose the retired approved state")
	}
	assertContainsAll(t, js, "expert market compact review actions", []string{
		`class="expert-market-card"`,
		`data-expert-reason`,
		`const reviewNote = status === 'pending_review'`,
		`const footer = reviewNote || actions ?`,
		`function expertMarketEnsureActionReason(card)`,
		`const existing = card.querySelector('[data-expert-reason]')`,
		`operationReasonRequired`,
		`const requiresReason = action === 'unlist' || action === 'delete' || action === 'purge'`,
		`reasonInput?.focus(); expertMarketSetStatus('error', expertMarketText('operationReasonRequired')); return;`,
		`data-expert-action="approve"`,
		`data-expert-action="reject"`,
		`data-expert-action="unlist"`,
		`status === 'unlisted'`,
		`expertMarketAdminFilterLabel`,
		`function bindExpertMarketActions()`,
		`grid.addEventListener('click'`,
		`deleteConfirm`,
		`action === 'delete' && !window.confirm`,
		`reasonInput?.focus(); return;`,
		`const successKey = { approve: 'approvedOk'`,
		`expertMarketAdminRequestKey`,
		`expertMarketAdminInFlight && expertMarketAdminRequestKey === requestKey`,
		`async function loadExpertMarketAdmin(page, force = false)`,
		`if (!force && expertMarketAdminInFlight && expertMarketAdminRequestKey === requestKey)`,
		`expertMarketAdminLoadController.abort()`,
		`signal: controller.signal`,
		`expertMarketIsAbort(err)`,
		`document.getElementById('tab-expertmarket')?.classList.contains('active')`,
		`void loadExpertMarketAdmin()`,
		`await loadExpertMarketAdmin(undefined, true);`,
		`expertMarketSetStatus('success', expertMarketText(successKey), 3200)`,
	})
	assertContainsAll(t, css, "expert market compact card styles", []string{
		`#expertMarketAdminGrid{grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}`,
		`.expert-market-card{display:flex`,
		`.expert-market-note input:focus`,
		`content-visibility:auto`,
		`.sm-status.success`,
	})
	assertContainsAll(t, core, "expert market tab loader", []string{
		`expertmarket:['expertMarketTabTitle','expertMarketTabSubtitle']`,
		`if(name==='expertmarket'&&typeof loadExpertMarketAdmin==='function')loadExpertMarketAdmin()`,
		`if(typeof applyExpertMarketAdminI18n==='function')applyExpertMarketAdminI18n()`,
	})
	if strings.Contains(js, `window.prompt(`) || strings.Contains(js, `onclick="expertMarket`) {
		t.Fatal("expert market moderation must use an inline reason field and delegated card actions")
	}
	if strings.Contains(js, `action === 'approve' || action === 'reject'`) || strings.Contains(js, `reasonRequired`) || strings.Contains(js, `if (!reason)`) {
		t.Fatal("expert market approval and rejection review notes must remain optional")
	}
	if strings.Contains(js, `status === 'approved'`) || strings.Contains(js, `data-expert-action="list"`) {
		t.Fatal("expert market approval must publish directly without a separate list action")
	}
	if strings.Contains(readAdminAsset(t, "admin/assets/js/admin-core.js"), `/experts/{id}/list`) {
		t.Fatal("expert market admin must not expose a retired separate listing action")
	}
	for _, retired := range []string{`approved: '已通过'`, `list: '上架'`, `listedOk: '专家条目已上架。'`} {
		if strings.Contains(js, retired) {
			t.Fatalf("expert market Chinese UI must not expose the retired separate listing state: %s", retired)
		}
	}
}

func TestAdminPageUserRankingsContract(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/user-rankings-tab.js")
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "user rankings script", []string{
		`/admin/assets/js/user-rankings-tab.js?v=user-rankings-20260624-1`,
	})
	assertContainsAll(t, core, "user rankings lazy tab restore", []string{
		`requested==='usermgmt'||requested==='userrankings'`,
		`if(name==='userrankings'&&typeof initUserRankingsTab==='function')initUserRankingsTab()`,
	})
	assertContainsAll(t, js, "user rankings frontend contract", []string{
		`/api/admin/user-rankings?`,
		`/api/admin/hubs`,
		`client-reported actual LLM usage`,
		`localStorage.getItem('maclawHubCenterActiveTab')`,
		`openTab('userrankings')`,
	})
	assertContainsAll(t, css, "user rankings layout", []string{
		`#tab-userrankings`,
		`.user-rankings-filter-grid`,
		`.user-ranking-row`,
	})
}
func TestAdminPageComputeMarketArchivedDeleteContract(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/compute-market-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "compute market cache busting", []string{
		`/admin/assets/css/admin-shell.css?v=pet-store-preview-toggle-20260731-1`,
		`/admin/assets/js/compute-market-tab.js?v=compact-compute-orders-20260622-2`,
	})
	assertContainsAll(t, js, "compute market archived delete contract", []string{
		"computeMarketDeleteArchivedOrder",
		"deleteArchivedComputeOrder",
		"cmOrdersArchived && CONFIRMABLE_STATUSES.indexOf(status) >= 0",
		"computeMarketRestoreOrder",
		"cmRestoringOrders",
		"restoreArchivedComputeOrder",
		"cmOrdersArchived && status === 'activated'",
		"this)",
		"/restore",
		"/api/admin/cardstore/orders/",
		"method: 'DELETE'",
		"\\u00b7 \\u00a5",
	})
	restoreFn := regexp.MustCompile(`async function restoreArchivedComputeOrder[\s\S]*?async function deleteArchivedComputeOrder`).FindString(js)
	if restoreFn == "" {
		t.Fatal("compute market restore contract missing restoreArchivedComputeOrder")
	}
	if strings.Contains(restoreFn, "confirm(") {
		t.Fatal("compute market restore must not ask for confirmation")
	}
	assertContainsAll(t, restoreFn, "compute market restore busy guard", []string{
		"cmRestoringOrders[key]",
		"button.disabled = true",
		"button.disabled = false",
		"delete cmRestoringOrders[key]",
	})
	assertContainsAll(t, css, "compute market delete button style", []string{
		".btn-danger-ghost{background:rgba(214,93,87,.06)",
		".btn-danger-ghost:hover{background:rgba(214,93,87,.1)",
	})
	assertContainsAll(t, html, "compute market order pager", []string{
		`id="cmOrdersPager"`,
		`onclick="changeComputeOrdersPage(-1)"`,
		`onclick="changeComputeOrdersPage(1)"`,
	})
	assertContainsAll(t, js, "compute market order paging contract", []string{
		"const CM_ORDERS_PAGE_SIZE = 20",
		"limit=' + CM_ORDERS_PAGE_SIZE + '&offset=' + offset",
		"limit=1&statuses=' + CONFIRMABLE_STATUS_QUERY",
		"renderComputeOrders(data.orders || [], data.total || 0, pendingData.total)",
		"renderComputeOrdersPager(total)",
		"Math.ceil(total / CM_ORDERS_PAGE_SIZE) - 1",
		"window.changeComputeOrdersPage = changeComputeOrdersPage",
	})
	assertContainsAll(t, css, "compute market compact order grid", []string{
		"#cmOrdersList.list{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px",
		".cm-order-metrics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:6px",
		".cm-orders-pager.is-visible{display:flex}",
	})
	if strings.HasPrefix(js, "\ufeff") {
		t.Fatal("compute-market-tab.js must not start with UTF-8 BOM")
	}
}

func TestAdminPageComputeMarketUsageStatsLayoutContract(t *testing.T) {
	js := readAdminAsset(t, "admin/assets/js/compute-market-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, js, "compute market usage stats layout contract", []string{
		"renderComputeStatsTable",
		"cmNumber(value)",
		"cmRows(data)",
		"Math.round(n * 10000) / 10000",
		"maximumFractionDigits: 4",
		"new URLSearchParams({ period: period })",
		"cm-stats-table-shell",
		"cm-stats-identity legacy",
		"computeMarketStatsLegacyIdentity",
		"<colgroup><col class=\"cm-col-scope\">",
	})
	assertContainsAll(t, css, "compute market usage stats table style", []string{
		".cm-stats-table-shell{width:100%;overflow:auto;overscroll-behavior-x:contain",
		".cm-data-table{width:100%;min-width:860px;table-layout:fixed",
		".cm-col-scope{width:34%}",
		".cm-data-table .num{text-align:right;font-variant-numeric:tabular-nums}",
	})
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

func TestAdminPageLLMServiceDoesNotExposeComputeGrant(t *testing.T) {
	html := readAdminPageBundle(t)

	assertContainsAll(t, html, "node config compute grant contract", []string{
		`id="hubComputeExternal-'+escapeHtml(domID)+'"`,
		`function grantHubComputeAuth(id,tenantID)`,
		`/api/admin/llm/authorizations`,
		`hubComputeAuthTitle:'Compute module authorization'`,
	})
	for _, forbidden := range []string{
		`id="llmSubTabAuth"`,
		`id="llmSubViewAuth"`,
		`showAuthDialog()`,
		`window.showAuthDialog`,
		`window.saveAuthorization`,
		`loadRegisteredHubs()`,
		`updateAuthTenantOptions`,
		`<select id="llmAuthHub"`,
		`<select id="llmAuthTenant"`,
		`id="llmAuthExternal"`,
		`field('llmAuthHub'`,
		`field('llmAuthTenant'`,
		`llmAuthCredits`,
		`llmAuthDays`,
		`llmAuthEmail`,
		`id="llmAuthGroup"`,
		`auth_external_`,
		`service_group_id === '__external_compute_permission__'`,
		`source: 'external_provider_permission'`,
		`hubComputeGroup-`,
		`hubComputeCreditsUsage`,
		`hubComputeActiveCount`,
		`tr('hubComputeAccessRecord')`,
		`fieldServiceGroup') + ' required'`,
		`esc(t('status')) + ': -`,
		`Grant Authorization`,
		`\u6388\u4e88\u6388\u6743`,
		`\u914d\u7f6e\u6388\u6743`,
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("admin LLM compute grant contract must not contain %s", forbidden)
		}
	}
	fn := regexp.MustCompile(`async function loadHubComputeAuthData[\s\S]*?async function grantHubComputeAuth`)
	match := fn.FindString(html)
	if match == "" {
		t.Fatalf("node compute grant contract missing loadHubComputeAuthData")
	}
	if strings.Contains(match, `/api/admin/llm/service-groups`) {
		t.Fatalf("node compute grant must not load service groups")
	}
	for _, forbidden := range []string{
		`admin_email`,
		`service_group_id`,
		`credits_total`,
		`starts_at`,
		`expires_at`,
		`status:'active'`,
	} {
		if strings.Contains(match, forbidden) {
			t.Fatalf("node compute grant payload must not contain %s", forbidden)
		}
	}
}

func TestAdminPageRouteQueryRendererIsSingleSource(t *testing.T) {
	html := readAdminPageBundle(t)
	if count := strings.Count(html, `function renderRouteQueryResult(meta,data)`); count != 1 {
		t.Fatalf("admin page should define renderRouteQueryResult once, got %d", count)
	}
	assertContainsAll(t, html, "admin route query renderer", []string{`tr('routeQueryHubDomain')`, `hub.corporate_email_domain||'-'`})
	assertContainsAll(t, html, "admin route phone query", []string{
		`meta.queryType==='phone'`,
		`body.phone_number=meta.phone`,
		`routeQueryTypePhone`,
	})
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
