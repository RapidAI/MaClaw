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

var cssBackgroundShorthand = regexp.MustCompile(`(^|[^-\w])background\s*:`)

func assertNoBackgroundShorthand(t *testing.T, css, context string, selectorNeedles []string) {
	t.Helper()
	rule := regexp.MustCompile(`([^{}]+)\{([^{}]+)\}`)
	for _, match := range rule.FindAllStringSubmatch(css, -1) {
		sel, body := match[1], match[2]
		hit := false
		for _, needle := range selectorNeedles {
			if strings.Contains(sel, needle) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if cssBackgroundShorthand.MatchString(body) {
			t.Fatalf("%s selector %q uses the background shorthand (resets background-image); body=%q", context, strings.TrimSpace(sel), strings.TrimSpace(body))
		}
	}
}

func TestAdminFeatureIcons(t *testing.T) {
	html := readAdminPageHTML(t)
	css := readAdminAsset(t, "admin/assets/css/admin-responsive.css")
	proUI := readAdminAsset(t, "pro-ui.css")
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")
	assertContainsAll(t, html, "hubcenter feature icons html", []string{
		`data-icon="globe"`,
		`item-title" data-icon="mail" id="mailCardTitle"`,
		`item-title" data-icon="activity" id="routingDiagnosticsTitle"`,
		`id="llmSubTabProviders" data-icon="spark"`,
		`admin-responsive.css?v=feature-icons-20260828-9`,
		`pro-ui.css?v=feature-icons-20260828-9`,
	})
	assertContainsAll(t, css, "hubcenter feature icons css", []string{
		`--icon-cloud`,
		`.item-title[data-icon]`,
		`[data-icon="mail"]`,
		`[data-icon="book"]`,
		`.llm-subtabs button[data-icon]::before`,
		`-webkit-mask:var(--feature-icon)`,
		`mask-mode:alpha`,
		`.head h3{--feature-icon:var(--head-icon)}`,
		`.item-title[data-icon]::after{content:""`,
		`background-color:currentColor!important`,
		`#tab-system{--head-icon:var(--icon-sliders)}`,
		`#tab-ha{--head-icon:var(--icon-sync)}`,
	})
	assertNoBackgroundShorthand(t, proUI, "pro-ui.css", []string{
		".item-title",
		"h3::after",
		"h3::before",
		".head h3",
	})
	assertNoBackgroundShorthand(t, css, "admin-responsive.css", []string{
		".item-title[data-icon]",
		".head h3::after",
		"button[data-icon]::before",
	})
	assertContainsAll(t, proUI, "hubcenter feature icons pro-ui", []string{
		`.item-title:not([data-icon])::before`,
		`background-color: #2563eb !important`,
	})
	assertContainsAll(t, core, "hubcenter page tab icons", []string{
		`TAB_ICONS.petstore=`,
		`TAB_ICONS.userrankings=`,
		`TAB_ICONS.problemreports=`,
		`TAB_ICONS.usermgmt=`,
		`window.TAB_ICONS=TAB_ICONS`,
	})
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
	assertContainsAll(t, html, "admin page stylesheet contract", []string{`href="/pro-ui.css`, `href="/admin/assets/css/admin-shell.css`, `href="/admin/assets/css/admin-responsive.css`})
	shared := strings.Index(html, `href="/pro-ui.css`)
	shell := strings.Index(html, `href="/admin/assets/css/admin-shell.css`)
	responsive := strings.Index(html, `href="/admin/assets/css/admin-responsive.css`)
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
		`/admin/assets/js/expertmarket-admin.js`,
	})
	if strings.Contains(html, `<option value="approved">`) {
		t.Fatal("expert market filter must not expose the retired approved state")
	}
	assertContainsAll(t, html, "expert market terminal status filter", []string{`<option value="purged">Purged</option>`, `/admin/assets/css/admin-shell.css`})
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
		`renderExpertMarketOwnerSearchRequired()`,
		`if ([...keyword].length < 2)`,
		`reasonInput?.reportValidity();`,
		`body: JSON.stringify({ target_user_id: state.selected.id, expected_owner_id: state.ownerID, reason })`,
		`expertMarketOwnerDialogOpener`,
		`event.key !== 'Tab'`,
		`ownerSearchAction`,
	})
	assertContainsAll(t, css, "expert market compact card styles", []string{
		`#expertMarketAdminGrid{grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}`,
		`.expert-market-card{display:flex`,
		`.expert-market-note input:focus`,
		`content-visibility:auto`,
		`.sm-status.success`,
		`.expert-market-status-purged`,
	})
	assertContainsAll(t, core, "expert market tab loader", []string{
		`expertmarket:['expertMarketTabTitle','expertMarketTabSubtitle']`,
		`if(name==='expertmarket'&&typeof loadExpertMarketAdmin==='function')loadExpertMarketAdmin()`,
		`if(typeof applyExpertMarketAdminI18n==='function')applyExpertMarketAdminI18n()`,
	})
	if strings.Contains(js, `window.prompt(`) || strings.Contains(js, `onclick="expertMarket`) {
		t.Fatal("expert market moderation must use an inline reason field and delegated card actions")
	}
	if strings.Contains(js, `action === 'approve' || action === 'reject'`) || strings.Contains(js, `reasonRequired`) || strings.Contains(js, `if (!reasonInput?.value.trim())`) {
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

func TestIndustryManagementAdminI18n(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/industry-management-admin.js")
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")

	assertContainsAll(t, html, "industry management nav and panel i18n", []string{
		`data-tab="industrymanagement"`,
		`data-i18n="navIndustryManagement"`,
		`data-i18n="navIndustryManagementDesc"`,
		`data-i18n="industryManagementTitle"`,
		`data-i18n="industryCreateTitle"`,
		`data-i18n="industryAssetsTitle"`,
		`/admin/assets/js/industry-management-admin.js`,
	})
	assertContainsAll(t, core, "industry management i18n keys", []string{
		`navIndustryManagement:'行业管理'`,
		`industryManagementTitle:'行业管理'`,
		`industryCreateTitle:'创建行业'`,
		`industryAssetsTitle:'已获取的不可变资产'`,
		`if(typeof applyIndustryManagementI18n==='function')applyIndustryManagementI18n()`,
	})
	assertContainsAll(t, js, "industry management i18n apply path", []string{
		`(function () {`,
		`function applyIndustryManagementStaticI18n()`,
		`function applyIndustryManagementI18n()`,
		`window.applyIndustryManagementI18n = applyIndustryManagementI18n`,
		`data-i18n="navIndustryManagement"`,
		`imCaptureBindingDrafts()`,
		`imRestoreOpenIndustries(openIDs)`,
		`if (industryManagementLoading) return industryManagementLoading`,
		`Date.now() - industryManagementLoadedAt < IM_CACHE_MS`,
		`renderIndustryManagement({ resetBindings: true })`,
		`function scheduleTenantIndustrySettings()`,
		`imIsTenantSettingsMutation`,
		`if (industryManagementLoaded) renderIndustryManagement()`,
		`overlay.contains(mutation.target)`,
		`if (seq !== imTenantRenderSeq || !document.contains(root)) return`,
	})
	if strings.Contains(js, `window.applyIndustryManagementI18n = () => { applyIndustryManagementI18n();`) {
		t.Fatal("industry management must not wrap applyIndustryManagementI18n in a recursive global assignment")
	}
	if strings.Contains(js, `window.applyI18n`) {
		t.Fatal("industry management must not re-run the full admin applyI18n pass")
	}
}

func TestLLMServiceAdminTitleI18n(t *testing.T) {
	html := readAdminPageHTML(t)
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")

	assertContainsAll(t, html, "llm service title cache", []string{
		`data-tab="llmservice"`,
		`data-i18n="navLLMService"`,
		`data-i18n="llmServiceTitle"`,
		`/admin/assets/js/admin-core.js`,
		`/admin/assets/js/llm-service-tab.js`,
	})
	assertContainsAll(t, core, "llm service title keys", []string{
		`llmservice:['llmServiceTitle','llmServiceDesc']`,
		`computemarket:['computeMarketTitle','computeMarketDesc']`,
		`ha:['haTabTitle','haTabSubtitle']`,
		`TAB_ICONS.llmservice=`,
		`TAB_ICONS.computemarket=`,
		`TAB_ICONS.ha=`,
		`navLLMService: '\u6a21\u578b\u63a5\u5165'`,
		`llmServiceTitle: '\u6a21\u578b\u63a5\u5165'`,
		`llmServiceClassHead: '\u5206\u7c7b\u5934'`,
		`function applyPageChrome(){`,
	})
	if strings.Contains(js, `llmServiceTitle:`) || strings.Contains(js, `llmServiceGroupsDesc:`) || strings.Contains(js, `llmServiceAgents:`) {
		t.Fatal("llm service tab must not re-assign admin-core chrome keys")
	}
	if strings.Contains(js, `typeof applyI18n === 'function') applyI18n()`) {
		t.Fatal("llm service init must not re-run the full admin applyI18n pass")
	}
	for _, body := range []string{core, js} {
		if strings.Contains(body, `llmServiceTitle:'\u6a21\u578b\u63a5\u5165\u70b9'`) ||
			strings.Contains(body, `llmServiceTitle: '\u6a21\u578b\u63a5\u5165\u70b9'`) {
			t.Fatal("llm service titles must not keep the old 模型接入点 wording")
		}
	}
	if !strings.Contains(core, "applyPageChrome();}") {
		t.Fatal("applyI18n must refresh pageTitle after late llmServiceTitle keys")
	}
	idxTitle := strings.LastIndex(core, "llmServiceTitle:")
	idxApply := strings.LastIndex(core, "applyI18n();")
	if idxTitle < 0 || idxApply < 0 || idxApply < idxTitle {
		t.Fatalf("late llmServiceTitle keys must be followed by applyI18n(); title=%d apply=%d", idxTitle, idxApply)
	}
}

func TestAdminPageEmbeddingModelRuntimeCard(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")

	assertContainsAll(t, html, "embedding model card cache", []string{
		`id="llmSubViewClassHead"`,
		`id="llmEmbeddingModelCard"`,
		`id="sgClassHead"`,
		`/admin/assets/js/llm-service-tab.js`,
	})
	classHead := strings.Index(html, `id="llmSubViewClassHead"`)
	embedCard := strings.Index(html, `id="llmEmbeddingModelCard"`)
	sgHead := strings.Index(html, `id="sgClassHead"`)
	providers := strings.Index(html, `id="llmSubViewProviders"`)
	if classHead < 0 || embedCard < 0 || sgHead < 0 || providers < 0 {
		t.Fatal("embedding card and class-head markers must exist")
	}
	if !(providers < classHead && classHead < embedCard && embedCard < sgHead) {
		t.Fatal("embedding card must sit on 分类头, above the head dashboard")
	}
	assertContainsAll(t, js, "embedding model runtime", []string{
		`/api/admin/model_download/status`,
		`/api/admin/model_download/trigger`,
		`loadLLMEmbeddingModelRuntime`,
		`triggerLLMEmbeddingModelDownload`,
		`maybeAutoSyncEmbeddingModel`,
		`if (data.ready)`,
		`HubCenter syncs the GGUF on start`,
		`runtimeTitle: 'Embedding model'`,
		`runtimeTitle: 'Embedding \u6a21\u578b'`,
		`embedder_ready`,
		`runtimePartial`,
		`data.ready && data.embedder_ready`,
		`runtimeAlreadyRunning`,
		`llm-embed-meta`,
		`llm-embed-title`,
		`if (tab === 'classHead' && typeof window.sgReloadClassHeadPage === 'function')`,
		`sgReloadClassHeadPage`,
		`llmClassHeadViewVisible`,
		`llmEmbeddingLoadSeq`,
		`embeddingRuntimeNeedsPoll`,
		`st.downloading || st.warming`,
		`waitEmbedder`,
		`llmClassHeadViewVisible() && !!(data.ready) && !data.embedder_ready`,
		`loadLLMEmbeddingModelRuntime({ silent: true })`,
		`Sync it above before scoring`,
	})
	if strings.Contains(js, `if(!id||!out)return;`) {
		t.Fatal("class-head score must not require a leftover group id")
	}
}

func TestAdminPageStaticNavHasPageChrome(t *testing.T) {
	html := readAdminPageHTML(t)
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")
	re := regexp.MustCompile(`data-tab="([a-z]+)"`)
	seen := map[string]struct{}{}
	for _, match := range re.FindAllStringSubmatch(html, -1) {
		tab := match[1]
		if _, ok := seen[tab]; ok {
			continue
		}
		seen[tab] = struct{}{}
		if !strings.Contains(core, tab+":['") && !strings.Contains(core, "tabMeta."+tab+"=") {
			t.Errorf("static nav tab %q missing tabMeta in admin-core; page title falls back to overview", tab)
		}
		if !strings.Contains(core, tab+":'<svg") && !strings.Contains(core, "TAB_ICONS."+tab+"=") {
			t.Errorf("static nav tab %q missing TAB_ICONS in admin-core; page icon falls back to overview", tab)
		}
	}
	if len(seen) == 0 {
		t.Fatal("admin page has no data-tab nav buttons")
	}
}

func TestAdminPageUserRankingsContract(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/user-rankings-tab.js")
	core := readAdminAsset(t, "admin/assets/js/admin-core.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "user rankings script", []string{
		`/admin/assets/js/user-rankings-tab.js`,
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
		`/admin/assets/css/admin-shell.css`,
		`/admin/assets/js/compute-market-tab.js?v=sold-card-compact-20260821-5`,
	})
	assertContainsAll(t, js, "compute market archived delete contract", []string{
		"computeMarketDeleteArchivedOrder",
		"deleteArchivedComputeOrder",
		"isArchived && CONFIRMABLE_STATUSES.indexOf(status) >= 0",
		"computeMarketRestoreOrder",
		"cmRestoringOrders",
		"restoreArchivedComputeOrder",
		"isArchived && status === 'activated'",
		"this)",
		"/restore",
		"/api/admin/cardstore/orders/",
		"method: 'DELETE'",
		"'\\u00a5' + esc(order.amount)",
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
		"#cmOrdersList.list{grid-template-columns:repeat(2,minmax(0,1fr));gap:8px",
		".cm-order-metrics{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:4px",
		".cm-order-main .cm-order-metric strong{display:block;margin:0",
		".cm-order-head-actions{display:flex;flex-wrap:nowrap;gap:4px",
		".cm-orders-pager.is-visible{display:flex}",
	})
	if strings.HasPrefix(js, "\ufeff") {
		t.Fatal("compute-market-tab.js must not start with UTF-8 BOM")
	}
}

func TestAdminPageComputeMarketRebindSoldCardContract(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/compute-market-tab.js")

	assertContainsAll(t, html, "compute market active-card filter", []string{
		`id="cmActiveCardsBtn"`,
		`onclick="toggleComputeActiveCards()"`,
		`data-i18n="computeMarketActiveCardsFilter"`,
	})
	assertContainsAll(t, js, "compute market rebind sold card contract", []string{
		"computeMarketActiveCardsFilter",
		"toggleComputeActiveCards",
		"cmActiveCardsOnly",
		"active_cards=1",
		"can_rebind_service_group",
		"showComputeOrderGroupEditor",
		"saveComputeOrderGroup",
		"/service-group",
		"use_default",
		"__default__",
		"computeMarketDefaultGroup",
		"esc(tr('computeMarketDefaultGroup')) + '</option>'",
		"window.toggleComputeActiveCards = toggleComputeActiveCards",
		"window.saveComputeOrderGroup = saveComputeOrderGroup",
		"cmRebindBusy",
		"defaultInList",
		"computeMarketCardGroupRequired",
		"if (cmActiveCardsOnly) cmOrdersArchived = false",
		"if (cmOrdersArchived) cmActiveCardsOnly = false",
		"var isArchived = !!String(o.archived_at || '').trim()",
		"__external_compute_permission__",
		"selected.toLowerCase() === '__default__'",
		"access_policy",
		"policy === 'grant_required'",
		"renderComputeOrderIdentity",
		"computeMarketOrderHub",
		"computeMarketOrderTenant",
		"hub_name",
		"tenant_name",
		"cm-order-head-actions",
		"renderComputeOrderMeta",
	})
	if strings.Contains(js, "esc(defaultID) + ')</option>'") || strings.Contains(js, "' (' + esc(defaultID)") {
		t.Fatal("default service group option must not append the current default group name")
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

func TestAdminPageLLMProviderSequenceCards(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "llm provider sequence cache", []string{
		`/admin/assets/css/admin-shell.css`,
		`/admin/assets/js/llm-service-tab.js`,
	})
	assertContainsAll(t, js, "llm provider sequence cards", []string{
		`function sortedProviders()`,
		`function applyProviderSequenceTargets()`,
		`var providersLoadSeq = 0`,
		`provider-seq' + (seq > 0 ? '' : ' is-unset')`,
		`Object.keys(providerSequenceInFlight).length`,
		`providerSequenceInFlight[p.id] = seq`,
		`/api/admin/llm/providers/sequences`,
		`id="llmPrvSequence"`,
		`p.lb_group && Number(p.lb_group_size||0) >= 2 ? '<span class="badge info">'`,
		`t('lbGroup')`,
		`p.lb_group`,
	})
	if strings.Contains(js, `style="order:`) || strings.Contains(js, "style='order:") {
		t.Fatal("provider cards must sort in the DOM, not with CSS order")
	}
	moveFn := regexp.MustCompile(`window\.moveLLMProvider = async function[\s\S]*?window\.toggleLLMProviderPaused`)
	move := moveFn.FindString(js)
	if move == "" {
		t.Fatal("moveLLMProvider is missing")
	}
	if strings.Count(move, "loadProviders({ traffic: false })") != 1 {
		t.Fatalf("moveLLMProvider should reload providers only on error, got %d loadProviders calls", strings.Count(move, "loadProviders({ traffic: false })"))
	}
	if strings.Count(move, "providersLoadSeq += 1") < 2 {
		t.Fatal("moveLLMProvider should drop in-flight provider lists when a move starts and when it succeeds")
	}
	toggleFn := regexp.MustCompile(`window\.toggleLLMProviderPaused = async function[\s\S]*?async function loadAgents`)
	toggle := toggleFn.FindString(js)
	if toggle == "" {
		t.Fatal("toggleLLMProviderPaused is missing")
	}
	if strings.Contains(toggle, "loadProviders()") {
		t.Fatal("toggleLLMProviderPaused should not reload providers or traffic")
	}
	if !strings.Contains(toggle, "providersLoadSeq += 1") {
		t.Fatal("toggleLLMProviderPaused should drop in-flight provider lists")
	}
	assertContainsAll(t, css, "llm provider sequence badge", []string{
		`.data-row>.provider-seq`,
		`font-variant-numeric:tabular-nums`,
		`#llmProvidersList.llm-card-grid{grid-template-columns:1fr}`,
		`#llmProvidersList .data-row{min-height:0;align-items:center;display:grid`,
		`#llmProvidersList .data-row-actions{grid-area:actions;max-width:420px;align-items:center;flex-wrap:wrap;justify-content:flex-end}`,
		`#llmProvidersList .data-row-meta{display:block;white-space:nowrap`,
		`.provider-seq.is-unset`,
	})
}

func TestAdminPageLLMProviderTrafficCards(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "llm provider traffic switch", []string{
		`id="llmProviderTrafficSwitch" class="provider-traffic-switch" hidden`,
	})

	assertContainsAll(t, js, "llm provider traffic cards", []string{
		`/api/admin/llm/providers/traffic`,
		`function renderProviderTraffic(id)`,
		`function formatTrafficTokens(value)`,
		`function formatTrafficExact(value)`,
		`function patchProviderTraffic()`,
		`Array.isArray(data.traffic)`,
		`providerTrafficReady`,
		`providerTrafficLoadSeq`,
		`providerTrafficInFlight`,
		`loadProviders({ traffic: false })`,
		`setProviderTrafficPeriod`,
		`syncProviderTrafficSwitch`,
		`applyProviderTrafficNode`,
		`onProviderTrafficSwitchKeydown`,
		`llmProvidersList .provider-traffic[data-provider-id]`,
		`llmProviderTrafficSwitch`,
		`is-pending`,
		`data-provider-id=`,
		`trafficDay`,
		`trafficWeek`,
		`trafficMonth`,
		`trafficIn`,
		`trafficOut`,
		`trafficTotal`,
		`function providerTrafficWindow(row)`,
		`function providerTrafficRow(id)`,
		`providerTrafficPeriodLabel(providerTrafficPeriod) + ' \u00b7 '`,
		`var hasData = !!(row && (row.day || row.week || row.month))`,
		`if (!providerTrafficReady) patchProviderTraffic()`,
		`aria-pressed`,
		`class="provider-traffic'`,
	})
	assertContainsAll(t, css, "llm provider traffic layout", []string{
		`#llmProvidersList .provider-traffic`,
		`.provider-traffic-switch{display:inline-flex`,
		`height:36px`,
		`.provider-traffic-col`,
		`.provider-traffic-line`,
		`grid-template-areas:"seq main traffic actions"`,
		`.provider-traffic.is-pending`,
		`minmax(228px,280px)`,
		`grid-template-columns:repeat(3,minmax(0,1fr))`,
		`font-variant-numeric:tabular-nums`,
	})
}

func TestAdminPageLLMServiceGroupTrafficCards(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")
	responsiveCSS := readAdminAsset(t, "admin/assets/css/admin-responsive.css")

	assertContainsAll(t, html, "llm service group traffic switch", []string{
		`id="llmServiceGroupTrafficSwitch" class="provider-traffic-switch" hidden`,
		`/admin/assets/css/admin-shell.css`,
		`/admin/assets/css/admin-responsive.css`,
		`/admin/assets/js/llm-service-tab.js`,
	})
	assertContainsAll(t, html, "llm service subtabs accessibility", []string{
		`class="filter-group llm-subtabs" role="tablist"`,
		`id="llmSubTabProviders"`,
		`role="tab"`,
		`aria-selected="true"`,
		`aria-controls="llmSubViewGroups"`,
		`id="llmSubViewClassHead" class="hidden-view" role="tabpanel" aria-labelledby="llmSubTabClassHead"`,
	})
	assertContainsAll(t, js, "llm service group traffic cards", []string{
		`function loadServiceGroupTraffic()`,
		`var serviceGroupsLoadSeq = 0;`,
		`if (seq !== serviceGroupsLoadSeq) return;`,
		`var seq = ++serviceGroupsLoadSeq;`,
		`function setServiceGroupTrafficPeriod(period)`,
		`function syncServiceGroupTrafficSwitch()`,
		`function applyServiceGroupTrafficNode(node, id)`,
		`function serviceGroupTrafficTotals(data)`,
		`if (data && !Array.isArray(data.rows))`,
		`Array.isArray(traffic)`,
		`item.service_group_id || item.group_id || item.id`,
		`String(keys[i]).trim().toLowerCase() === lower`,
		`Object.keys(traffic).reduce(function(rows, id)`,
		`if (tab === 'groups' && serviceGroups.length && !serviceGroupTrafficInFlight) loadServiceGroupTraffic();`,
		`/api/admin/llm/service-groups/traffic`,
		`data-service-group-id=`,
		`serviceGroupTrafficPeriod`,
		`window.setServiceGroupTrafficPeriod = setServiceGroupTrafficPeriod;`,
		`window.onLLMSubTabKeydown = function(event)`,
		`btn.setAttribute('aria-selected', String(active));`,
		`btn.tabIndex = active ? 0 : -1;`,
		`['ArrowLeft', 'ArrowRight', 'Home', 'End']`,
	})
	assertContainsAll(t, css, "llm service group traffic layout", []string{
		`#llmServiceGroupsList .service-group-traffic`,
		`#llmServiceGroupsList .service-group-traffic-col`,
		`#llmServiceGroupsList .llm-service-group-row`,
	})
	assertContainsAll(t, responsiveCSS, "llm service subtab final selection", []string{
		`#tab-llmservice .llm-subtabs [role="tab"][aria-selected="true"]`,
		`background:#2563eb!important`,
		`#tab-llmservice .llm-subtabs [role="tab"][aria-selected="false"]`,
	})
}

func TestAdminPageLLMProviderDialogI18n(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")

	assertContainsAll(t, html, "llm provider dialog i18n cache", []string{
		`/admin/assets/js/llm-service-tab.js`,
	})
	assertContainsAll(t, js, "llm provider dialog local i18n", []string{
		`t('providerProbeModels')`,
		`t('providerProbing')`,
		`t('providerProbeEmpty')`,
		`t('providerProbeFailed')`,
		`t('providerCapabilityPreset')`,
		`providerProbeModels: 'Probe'`,
		`providerProbeModels: '\u63a2\u6d4b'`,
		`providerProbing: 'Probing models...'`,
		`providerProbing: '\u6b63\u5728\u63a2\u6d4b\u6a21\u578b...'`,
		`providerCapabilityPreset: 'Preset capabilities'`,
		`providerCapabilityPreset: '\u9884\u7f6e\u80fd\u529b'`,
		`trafficLoading: 'Loading'`,
		`trafficLoading: '\u52a0\u8f7d\u4e2d'`,
		`t('trafficLoading')`,
		`typeof I18N_ZH !== 'undefined'`,
		`typeof I18N_EN !== 'undefined'`,
	})
}

func TestAdminPageLLMProviderAvailabilityTestUsesConfiguredModel(t *testing.T) {
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")

	assertContainsAll(t, js, "llm provider availability test", []string{
		`/api/admin/llm/providers/test-chat`,
		`var model = (provider.models && provider.models[0]) || '';`,
		`providerTestStates[id] = { status: 'error', message: 'No model configured' }`,
		`if (!data.success) throw new Error(data.error || 'unknown');`,
		`routeModel = pconfigs[0].model || '';`,
		`model: routeModel || ((provider.models && provider.models.length === 1) ? provider.models[0] : firstModel.name) || ''`,
		`wire_api: provider.wire_api || 'chat'`,
	})
	providerTest := regexp.MustCompile(`window\.testLLMProvider = async function[\s\S]*?function uniqueProviderBillingDays`).FindString(js)
	if providerTest == "" {
		t.Fatal("testLLMProvider is missing")
	}
	if strings.Contains(providerTest, "/probe-models") {
		t.Fatal("provider availability test must send a chat request rather than only probe /models")
	}
}

func TestAdminPageLLMProviderBillingEditor(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "llm provider billing cache", []string{
		`/admin/assets/js/llm-service-tab.js`,
	})
	assertContainsAll(t, js, "llm provider billing editor", []string{
		`id="llmPrvTimezone"`,
		`id="llmPrvMultiplier"`,
		`id="llmPrvBillingWindows"`,
		`id="llmPrvBillStart' + index + '"`,
		`credit_multiplier_schedule`,
		`function providerBillingSection(`,
		`function readProviderBilling()`,
		`function normalizeProviderBillingWindow(`,
		`function copyProviderExtraFields(`,
		`function providerBillingBadge(`,
		`function resolveProviderBillingMultiplier(`,
		`hour12: false`,
		`if (hadDays && !days.length) return null`,
		`startProviderBillingNowClock()`,
		`stopProviderBillingNowClock()`,
		`item.days = normalizeProviderBillingDays(days) || []`,
		`window.refreshProviderBillingNow`,
		`window.addProviderBillingWindow`,
		`window.toggleProviderBillingDay`,
		`keepBilling:true`,
		`t('billingAddWindow')`,
		`t('billingSchedule')`,
		`t('billingEmpty')`,
		`t('billingOvernight')`,
		`t('billingDroppedWindows')`,
		`billingAddWindow: 'Add window'`,
		`billingAddWindow: '\u6dfb\u52a0\u65f6\u6bb5'`,
		`billingEveryday: '\u6bcf\u5929'`,
		`billingWeekdays: '\u5de5\u4f5c\u65e5'`,
		`billingEmpty: '\u6682\u65e0\u5206\u65f6\u65f6\u6bb5`,
		`.map(normalizeProviderBillingWindow).filter(function(w)`,
		`payload.credit_multiplier_schedule = billing.credit_multiplier_schedule`,
		`copyProviderExtraFields(existing)`,
		`providerBillingBadge(p)`,
	})
	saveFn := regexp.MustCompile(`window\.saveProvider = async function[\s\S]*?window\.deleteLLMProvider`)
	save := saveFn.FindString(js)
	if save == "" {
		t.Fatal("saveProvider is missing")
	}
	if !strings.Contains(save, `payload.timezone = billing.timezone`) || !strings.Contains(save, `payload.credit_multiplier = billing.credit_multiplier`) {
		t.Fatal("saveProvider must persist vendor timezone and multiplier")
	}
	if !strings.Contains(save, `toast(t('billingDroppedWindows'), 'error')`) || !strings.Contains(save, `return;`) {
		t.Fatal("saveProvider must block when a time window is empty or has identical start/end")
	}
	openFn := regexp.MustCompile(`function openDialog\([\s\S]*?function closeDialog`)
	open := openFn.FindString(js)
	if open == "" {
		t.Fatal("openDialog is missing")
	}
	if !strings.Contains(open, `stopProviderBillingNowClock()`) {
		t.Fatal("openDialog must stop the billing clock so agent/group dialogs do not keep a stale interval")
	}
	assertContainsAll(t, css, "llm provider billing layout", []string{
		`.provider-billing{`,
		`.provider-billing-window{`,
		`.provider-billing-fields{`,
		`.provider-billing-empty{`,
		`.provider-billing-window.is-invalid{`,
		`.provider-billing-title{`,
		`.provider-day-chip`,
		`.provider-preset-chip`,
		`.provider-billing-times{`,
		`.provider-billing-fields,.provider-billing-times{grid-template-columns:1fr}`,
	})
}

func TestAdminPageLLMServiceGroupOfficialBandCopy(t *testing.T) {
	html := readAdminPageHTML(t)
	js := readAdminAsset(t, "admin/assets/js/llm-service-tab.js")
	css := readAdminAsset(t, "admin/assets/css/admin-shell.css")

	assertContainsAll(t, html, "official band cache", []string{
		`/admin/assets/js/llm-service-tab.js`,
		`/admin/assets/css/admin-shell.css`,
		`id="llmSubTabClassHead"`,
		`id="llmSubViewClassHead"`,
		`switchLLMSubTab('classHead')`,
	})
	assertContainsAll(t, js, "official band helpers", []string{
		`function sgOfficialBandName(n)`,
		`function sgCanonicalModelName(n)`,
		`function sgOfficialBandQuality(n)`,
		`function sgCapLabel(tag)`,
		`function sgEnsureModel(d,name)`,
		`function sgEnsureModelsForRoutes(d)`,
		`function sgPrepareDynamicDraft(d,opts)`,
		`if(opts&&opts.fillEmptyOfficial)sgFillEmptyOfficialBandsFromAuto(d)`,
		`sgPrepareDynamicDraft(sgDraft,{fillEmptyOfficial:true})`,
		`function sgFillEmptyOfficialBandsFromAuto(d)`,
		`function sgDedupeModels(d)`,
		`function sgIsLockedModelName(name)`,
		`function sgModelsNeedingProvider(d)`,
		`function sgWorkloadModelChoices(d,selected,cls)`,
		`sgEnsureModel(sgDraft,v)`,
		`sgPrepareDynamicDraft(sgDraft)`,
		`sgPlanDesignNoLow`,
		`sgProtectedModel`,
		`sgTierHigh: 'Official high (official-high)'`,
		`sgTierHigh: '\u5b98\u65b9\u9ad8\u6863\uff08official-high\uff09'`,
		`sgCapabilityHint: 'Capabilities of this upstream model`,
		`sgCapLabel(f)`,
		`sgFormatCaps(cfg&&cfg.capability_tags)`,
		`(locked?' disabled':'')`,
		`function sgRenderTrafficDialog(opts)`,
		`function sgDialogAlive(kind,id)`,
		`sgOpenKind='traffic'`,
		`window.editLLMClassTraffic=function(id)`,
		`switchLLMSubTab('classHead')`,
		`sgClassHeadQS()`,
		`function sgHeadPageAlive()`,
		`sgClassTraffic: 'Downstream traffic'`,
		`sg-form-dialog sg-traffic-dialog`,
		`function sgFmtHead(data)`,
		`function sgFmtHeadVersions(data)`,
		`sgHeadNeedShadow`,
		`sgHeadNeedServing`,
		`sgHeadAdoptReady`,
		`sgHeadNeedDistribute`,
		`sgHeadDistributing`,
		`function sgFmtHeadTest(data)`,
		`/api/admin/llm/class-head/score`,
		`sgScoreClassHead(true)`,
		`window.sgReloadClassHeadPage=function()`,
		`if(!out)return;`,
		`sgHeadTestCompare`,
		`sgHeadEmbedderOff`,
		`sgHeadScoreGroup`,
		`sgHeadScoreGroupAuto`,
		`sgHeadTestGroup`,
		`function sgSyncHeadScoreGroupSelect()`,
		`if (!serviceGroups.length) {`,
		`if (!providers.length) renderProviders()`,
		`if (!agents.length) renderAgents()`,
		`var snap=el.querySelector('.sg-head-dash')?sgSnapHead():null`,
		`if(hasDash){ toast(e.message||t('sgFailed'),'error'); return; }`,
		`button.sg-traffic-win[data-win]`,
		`if(!hasBoard) el.textContent=t('trafficLoading')`,
		`group_id:groupId`,
		`data.group_id`,
		`sgHeadVersions: 'Head versions'`,
		`sg-head-dash`,
		`sg-pipe-steps`,
		`window.sgReviewClassHead=async function(sampleId,goldPrefill)`,
		`window.sgDeleteClassHeadSample=async function(sampleId)`,
		`/api/admin/llm/class-head/sample/delete`,
		`function sgParseGoldClass(raw)`,
		`function sgGoldSelect(sample)`,
		`function sgSampleActions(sample)`,
		`function sgFmtSamplePager(data)`,
		`window.setDefaultLLMServiceGroup = async function(id)`,
		`/api/admin/llm/service-groups/`,
		`sgSetDefault`,
		`sgDefaultBadge`,
		`sgOfficialNoDelete`,
		`sgSystemBadge`,
		`(isOfficial?'':'<button class="btn-danger-ghost"`,
		`default_service_group_id`,
		`sg-traffic-sample-body`,
		`sg-traffic-sample-time`,
		`window.sgHeadSamplePageTo=function(delta)`,
		`?page=`,
		`sample_page`,
		`sample_total`,
		`function sgHeadSamplePages(data)`,
		`function sgHeadIsOfficial(data)`,
		`function sgSnapHead()`,
		`sgSampleDeleteConfirm`,
		`sgGoldClear`,
		`sgSampleDelete`,
		`function sgFmtTryResult(data)`,
		`function sgSourceLabel(src)`,
		`sgGoldInvalid`,
		`sgGoldPick`,
		`sgTrainBusy`,
		`sgSrc_hint`,
		`sgAck_acked`,
		`function sgWinButtons(id)`,
		`_sgTrafficDataWin`,
		`sgWin_24h`,
		`is-locked`,
		`function sgShowPromoteForm(mode)`,
		`event.ctrlKey||event.metaKey`,
		`function sgHeadIsUnused(data)`,
		`sgTrainNeedDataOfficial`,
		`sgHeadHasSamples`,
		`sgHeadUnusedOfficial`,
		`sgPromoteForm`,
		`onsubmit="sgConfirmPromote(`,
		`sgPipe_off: 'Rules'`,
		`sgGate_review_coverage: 'Review coverage'`,
		`function sgIsOfficialGroup(id)`,
		`function sgHeadCallout(data)`,
		`function sgTrainerNodes(data)`,
		`function sgHeadStatusLabel(status, data)`,
		`function sgHeadLiveLabel(data)`,
		`function sgHeadJobLabel(status)`,
		`function sgHeadStatusHint(data)`,
		`function sgHeadNeedsPoll(data)`,
		`function sgHeadPollKey(data)`,
		`function sgHeadSamplesKey(data)`,
		`promoteMode:promote?String(promote.getAttribute('data-mode')||''):''`,
		`box.setAttribute('data-mode', mode)`,
		`if (choices && snap.choices) choices.innerHTML = snap.choices`,
		`function sgHeadPollBlocked()`,
		`function scheduleClassHeadPoll()`,
		`window.sgLoadClassHead({quiet:true})`,
		`var training=status==='training'`,
		`String(data&&data.pipeline||'off')==='off'`,
		`data.artifact_ready&&String(data.status||'')!=='training'`,
		`sgHeadSt_unused: 'Live: rules only'`,
		`sgHeadSt_unused: '\u7ebf\u4e0a\uff1a\u4ec5\u89c4\u5219'`,
		`sgHeadSt_unused_trained`,
		`sgHeadStHint_unused`,
		`sgHeadPipeline: 'Live path'`,
		`sgHeadPipeline: '\u7ebf\u4e0a\u5206\u6d41'`,
		`sgTrainerLocalTag`,
		`sgTrainerHint`,
		`sg-sample-action`,
		`sgConfirmLive`,
		`canPromote`,
		`tryOpen:!!(tryBox&&tryBox.open)`,
		`var showEval=`,
		`editLLMClassTraffic('+jsArg(g.id)+')`,
		`editLLMServiceGroup('+jsArg(g.id)+')`,
		`function sgHeadIsOfficial(data)`,
		`window.sgRerenderOpenDialog = sgRerenderOpenDialog`,
		`if (typeof renderAgents === 'function') renderAgents()`,
		`window.sgLoadClassHead({quiet:true, relabel:true})`,
		`function sgRelabelProviderDialog()`,
		`function sgRelabelAgentDialog()`,
		`function sgSnapFocus(root)`,
		`board:board?board.innerHTML:''`,
		`if(sgHeadActing()){ scheduleClassHeadPoll(); return; }`,
		`add(t('sgHeadTestSlot'), data.slot||'')`,
		`id="sgFieldName"`,
		`window._sgHeadPollKey`,
	})
	if strings.Contains(js, `>shadow</button>`) || strings.Contains(js, `>canary</button>`) {
		t.Fatal("class-head pipeline buttons must use labels, not raw mode names")
	}
	if strings.Contains(js, `sgPrompt(t('sgGoldPrompt')`) {
		t.Fatal("gold review must use a class select, not a free-text prompt")
	}
	if strings.Contains(js, `sgPrompt(t('sgPromptReason')`) || strings.Contains(js, `sgPrompt(t('sgPromptOverride')`) {
		t.Fatal("pipeline override must use an inline form, not stacked prompts")
	}
	if strings.Contains(js, `function sgEnsureOfficialBandModels`) {
		t.Fatal("dead sgEnsureOfficialBandModels should not remain")
	}
	if strings.Contains(js, `function sgRenderClassTrainSection`) || strings.Contains(js, `{focus:'traffic'}`) {
		t.Fatal("group traffic must open its own dialog, not the group editor")
	}
	if strings.Contains(js, `t('sgCap_'+f)||f`) {
		t.Fatal("capability checkboxes must use sgCapLabel, not t()||raw fallback")
	}
	if !utf8.ValidString(js) {
		t.Fatal("llm-service-tab.js must stay UTF-8")
	}
	for _, stale := range []string{
		"未启用", "头未接入", "Head idle", `sgHeadSt_unused: 'Unused'`,
		`sgHeadPipeline: 'Pipeline'`, `Wait for serving ACK`,
		`\u6d41\u6c34\u7ebf`, `function sgHeadStatusHint(status)`,
		`setTimeout(function(){sgTrainBusy=false;if(sgHeadPageAlive())window.sgLoadClassHead();},800)`,
	} {
		if strings.Contains(js, stale) {
			t.Fatalf("class-head unused badge must name the live path, not %q", stale)
		}
	}
	assertContainsAll(t, css, "class-head dashboard", []string{
		`.sg-head-dash{`,
		`grid-template-columns:minmax(0,1fr)`,
		`.sg-pipe-steps{`,
		`.sg-head-metrics{`,
		`.sg-callout.is-ok{`,
		`.sg-gold-sel{`,
		`.sg-sample-action{display:grid`,
		`.sg-sample-preview{`,
		`overflow-wrap:break-word`,
		`grid-template-columns:minmax(0,1fr) auto`,
		`.sg-traffic-sample{display:grid;grid-template-columns:auto minmax(0,1fr)`,
		`.sg-traffic-sample-body{`,
		`.sg-traffic-dialog .sg-train-block{`,
		`.sg-sample-pager{`,
		`.sg-head-versions,.sg-head-test{`,
		`.sg-version-table{`,
		`.sg-try-result{`,
		`.sg-pipe-btn.is-locked{`,
		`.sg-promote-form{`,
		`.sg-field{`,
	})
	if strings.Contains(css, `.sg-sample,.sg-traffic-sample{display:grid;grid-template-columns:minmax(0,1fr) auto`) {
		t.Fatal("traffic samples must not reuse the sample/action grid (squeezes CJK class labels vertical)")
	}
	if strings.Contains(css, `.sg-sample-preview{color:#3b414d;font-size:12px;line-height:1.45;overflow-wrap:anywhere}`) {
		t.Fatal("sample preview must not use overflow-wrap:anywhere (collapses CJK to one glyph per line)")
	}
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
