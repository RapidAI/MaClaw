package structureddata

import "net/http"

func (s *HTTPServer) handleWebConsole(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(webConsoleHTML))
}

const webConsoleHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MaClawDataSrv MIS Admin Console</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #eef2f6;
      --panel: #ffffff;
      --panel-2: #f8fafc;
      --line: #d4dce7;
      --line-2: #e8edf3;
      --text: #17212f;
      --muted: #657386;
      --brand: #155e75;
      --brand-2: #0f4c5f;
      --brand-soft: #e7f5f8;
      --accent: #7c3aed;
      --amber: #b7791f;
      --danger: #b42318;
      --ok: #0f7a45;
      --focus: #2d7ff9;
      --nav: #102331;
      --nav-2: #162f42;
      --nav-muted: #a8b8c5;
      --shadow-sm: 0 1px 2px rgba(16, 35, 49, .06);
      --shadow-md: 0 14px 34px rgba(16, 35, 49, .10);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: var(--bg);
      color: var(--text);
      font-size: 14px;
      line-height: 1.45;
    }
    header {
      display: grid;
      grid-template-columns: minmax(240px, 360px) minmax(0, 1fr);
      gap: 18px;
      padding: 16px 22px;
      background: rgba(255, 255, 255, .96);
      border-bottom: 1px solid var(--line);
      position: sticky;
      top: 0;
      z-index: 10;
      box-shadow: var(--shadow-sm);
      backdrop-filter: blur(12px);
    }
    h1 { margin: 0; font-size: 19px; font-weight: 760; letter-spacing: 0; }
    h2 { margin: 0 0 12px; font-size: 16px; letter-spacing: 0; }
    h3 { margin: 0 0 8px; font-size: 14px; letter-spacing: 0; }
    button, input, textarea, select {
      font: inherit;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--text);
    }
    input, textarea, select { width: 100%; padding: 9px 10px; }
    textarea { min-height: 140px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace; font-size: 13px; }
    button { min-height: 36px; padding: 8px 12px; cursor: pointer; white-space: nowrap; font-weight: 650; box-shadow: 0 1px 0 rgba(16, 35, 49, .03); }
    button:hover { border-color: #b8c4d2; background: #f8fafc; }
    button.primary { background: var(--brand); border-color: var(--brand); color: #fff; }
    button.primary:hover { background: var(--brand-2); }
    button.danger { border-color: #e5b7b2; color: var(--danger); }
    button.small { min-height: 30px; padding: 5px 8px; font-size: 12px; }
    button:disabled { cursor: not-allowed; opacity: .6; }
    input:focus, textarea:focus, select:focus, button:focus { outline: 2px solid var(--focus); outline-offset: 1px; }
    main { padding: 18px; max-width: 1680px; margin: 0 auto; }
    .brand { display: flex; gap: 12px; align-items: center; min-width: 0; }
    .brand-mark { width: 42px; height: 42px; border-radius: 10px; display: grid; place-items: center; background: linear-gradient(135deg, var(--brand), var(--accent)); color: #fff; font-weight: 760; box-shadow: 0 10px 22px rgba(21, 94, 117, .22); }
    .brand-copy { min-width: 0; }
    .product-subtitle { margin-top: 2px; color: var(--muted); font-size: 12px; }
    .topbar { display: grid; grid-template-columns: minmax(200px, 1.4fr) minmax(160px, 1fr) 120px 130px 120px 112px 112px; gap: 10px; align-items: end; min-width: 780px; }
    .layout { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 18px; align-items: start; }
    .panel { background: var(--panel); border: 1px solid var(--line); border-radius: 10px; padding: 14px; box-shadow: var(--shadow-sm); }
    .setup-panel { max-width: 1180px; margin: 16px auto 0; display: grid; grid-template-columns: minmax(220px, 1fr) minmax(260px, 1.2fr) minmax(260px, 1.2fr); gap: 14px; align-items: start; }
    .setup-panel.ready { border-color: #b7dfc4; background: #f7fcf9; }
    .setup-panel.locked { display: none; }
    .setup-copy p { margin: 4px 0 0; color: var(--muted); }
    .resource-sidebar { position: sticky; top: 92px; max-height: calc(100vh - 112px); overflow: auto; }
    .workspace { padding: 0; overflow: hidden; box-shadow: var(--shadow-md); }
    .workspace-shell { display: grid; grid-template-columns: 220px minmax(0, 1fr); min-height: calc(100vh - 132px); }
    .workspace-body { min-width: 0; padding: 18px; background: #fff; }
    .module-header { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; padding-bottom: 14px; margin-bottom: 14px; border-bottom: 1px solid var(--line-2); }
    .module-kicker { color: var(--brand); font-size: 12px; font-weight: 760; text-transform: uppercase; letter-spacing: .05em; }
    .module-title { margin: 2px 0 4px; font-size: 20px; font-weight: 760; }
    .module-desc { color: var(--muted); max-width: 760px; }
    .context-chip { display: inline-flex; align-items: center; min-height: 30px; padding: 4px 10px; border: 1px solid var(--line); border-radius: 999px; background: var(--panel-2); color: var(--muted); font-size: 12px; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .summary-bar { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-bottom: 14px; }
    .summary-item { border: 1px solid var(--line-2); border-radius: 8px; background: var(--panel-2); padding: 10px 12px; min-width: 0; }
    .summary-label { color: var(--muted); font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; }
    .summary-value { margin-top: 4px; font-weight: 760; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .status-history { display: grid; gap: 6px; margin-bottom: 14px; }
    .status-history-row { display: grid; grid-template-columns: 76px 72px minmax(0, 1fr); gap: 8px; align-items: center; border: 1px solid var(--line-2); border-radius: 6px; padding: 7px 9px; background: #fff; font-size: 12px; }
    .status-history-kind { font-weight: 700; color: var(--muted); }
    .status-history-kind.ok { color: var(--ok); }
    .status-history-kind.err { color: var(--danger); }
    .status-history-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .overview-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
    .overview-card { border: 1px solid var(--line); border-radius: 10px; padding: 15px; background: #fff; min-height: 150px; display: grid; gap: 10px; align-content: start; box-shadow: var(--shadow-sm); }
    .overview-card h3 { margin: 0; font-size: 15px; }
    .overview-card p { margin: 0; color: var(--muted); }
    .overview-card .row { margin-top: 2px; }
    .health-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
    .health-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .health-label { color: var(--muted); font-size: 12px; }
    .health-value { margin-top: 4px; font-size: 18px; font-weight: 760; }
    .risk-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
    .risk-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .risk-value { margin-top: 4px; font-size: 18px; font-weight: 760; }
    .risk-value.high { color: var(--danger); }
    .access-summary-grid { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; }
    .access-summary-item { border: 1px solid var(--line-2); border-radius: 8px; background: var(--panel-2); padding: 10px 12px; min-width: 0; }
    .access-summary-value { margin-top: 4px; font-size: 18px; font-weight: 760; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .access-summary-value.warn { color: var(--danger); }
    .evidence-summary-grid { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-bottom: 10px; }
    .evidence-summary-item { border: 1px solid var(--line-2); border-radius: 8px; background: var(--panel-2); padding: 10px 12px; min-width: 0; }
    .evidence-summary-value { margin-top: 4px; font-size: 18px; font-weight: 760; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .evidence-summary-value.ok { color: var(--ok); }
    .evidence-summary-value.warn { color: var(--danger); }
    .evidence-summary-value.fail { color: var(--danger); }
    .summary-preview { margin: 0 0 10px; padding: 12px; border: 1px solid var(--line-2); border-radius: 8px; background: #f7f9fb; color: var(--text); max-height: 220px; overflow: auto; }
    .access-playbook { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
    .access-playbook-card { display: grid; gap: 8px; align-content: start; border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 12px; min-height: 132px; }
    .access-playbook-card strong { font-size: 14px; }
    .access-playbook-card span { color: var(--muted); font-size: 12px; }
    .queue-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
    .queue-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .queue-value { margin-top: 4px; font-size: 18px; font-weight: 760; }
    .queue-value.high { color: var(--danger); }
    .integration-grid { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; }
    .integration-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .integration-value { margin-top: 4px; font-size: 18px; font-weight: 760; }
    .integration-value.high { color: var(--danger); }
    .coverage-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
    .coverage-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .coverage-value { margin-top: 4px; font-size: 18px; font-weight: 760; }
    .coverage-value.high { color: var(--danger); }
    .domain-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
    .domain-card { display: grid; gap: 5px; text-align: left; border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; min-height: 86px; }
    .domain-card.warn { border-color: #efc7bd; background: #fff8f6; }
    .domain-title { font-weight: 760; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .domain-status { color: var(--muted); font-size: 12px; }
    .capability-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
    .capability-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .capability-value { margin-top: 4px; font-size: 18px; font-weight: 760; }
    .intent-panel { display: grid; gap: 10px; }
    .intent-row { display: grid; grid-template-columns: minmax(180px, 1fr) 150px 92px auto; gap: 8px; align-items: end; }
    .intent-results { display: grid; gap: 8px; }
    .intent-card { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: center; border: 1px solid var(--line-2); border-radius: 8px; padding: 10px 12px; background: #fff; text-align: left; }
    .intent-card strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .intent-card span { color: var(--muted); font-size: 12px; }
    .readiness-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
    .readiness-item { border: 1px solid var(--line-2); border-radius: 8px; background: #fff; padding: 10px 12px; }
    .readiness-value { margin-top: 4px; font-size: 15px; font-weight: 760; }
    .readiness-value.ok { color: var(--ok); }
    .readiness-value.warn { color: var(--danger); }
    .recommendation-list { display: grid; gap: 6px; }
    .recommendation-item { display: flex; align-items: center; justify-content: space-between; gap: 10px; border: 1px solid var(--line-2); border-radius: 8px; padding: 8px 10px; background: var(--panel-2); font-size: 12px; }
    .recommendation-item strong { color: var(--text); }
    .recommendation-item span { color: var(--muted); }
    .activity-list { display: grid; border: 1px solid var(--line-2); border-radius: 8px; overflow: hidden; background: #fff; }
    .activity-row { display: grid; grid-template-columns: 150px 150px minmax(0, 1fr); gap: 10px; padding: 9px 12px; border-bottom: 1px solid var(--line-2); font-size: 12px; align-items: center; }
    .activity-row:last-child { border-bottom: 0; }
    .activity-action { font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .activity-summary { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .checklist { border: 1px solid var(--line); border-radius: 8px; overflow: hidden; background: #fff; }
    .checklist-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; background: var(--panel-2); border-bottom: 1px solid var(--line); }
    .checklist-title { font-weight: 760; }
    .checklist-body { display: grid; }
    .checklist-item { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 10px 14px; border-bottom: 1px solid var(--line-2); }
    .checklist-item:last-child { border-bottom: 0; }
    .check-icon { width: 22px; height: 22px; border-radius: 999px; display: grid; place-items: center; border: 1px solid var(--line); color: var(--muted); font-size: 12px; font-weight: 760; }
    .checklist-item.done .check-icon { background: #e8f5ec; border-color: #b7dfc4; color: var(--ok); }
    .checklist-main { min-width: 0; }
    .checklist-label { font-weight: 700; }
    .checklist-desc { color: var(--muted); font-size: 12px; margin-top: 2px; }
    .stack { display: grid; gap: 12px; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .grid-2 { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
    .grid-3 { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
    .label { display: grid; gap: 5px; color: var(--muted); font-size: 12px; font-weight: 650; }
    .check { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); font-size: 12px; font-weight: 650; }
    .readonly { background: #f5f7f8; color: var(--muted); }
    .status { font-size: 12px; color: var(--muted); }
    #serviceStatus { display: inline-flex; align-items: center; gap: 6px; width: fit-content; margin-top: 6px; padding: 4px 9px; border: 1px solid var(--line); border-radius: 999px; background: var(--panel-2); }
    #serviceStatus::before { content: ""; width: 7px; height: 7px; border-radius: 999px; background: var(--muted); }
    #serviceStatus.ok::before { background: var(--ok); }
    #serviceStatus.err::before { background: var(--danger); }
    .status.ok { color: var(--ok); }
    .status.err { color: var(--danger); }
    .tabs { display: grid; align-content: start; gap: 3px; padding: 12px; background: linear-gradient(180deg, var(--nav), var(--nav-2)); border-right: 1px solid #0b1c24; overflow: auto; }
    .nav-group { margin: 10px 8px 4px; color: #6f8791; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; }
    .nav-group:first-child { margin-top: 2px; }
    .tab { width: 100%; justify-content: flex-start; text-align: left; border: 1px solid transparent; border-radius: 6px; background: transparent; color: var(--nav-muted); }
    .tab:hover { color: #fff; background: rgba(255,255,255,.07); }
    .tab.active { color: #fff; background: rgba(255,255,255,.14); border-color: rgba(255,255,255,.16); box-shadow: inset 3px 0 0 #67e8f9; }
    .dataset-list { display: grid; gap: 6px; max-height: calc(100vh - 210px); overflow: auto; }
    .action-list { display: grid; gap: 8px; max-height: 320px; overflow: auto; }
    .action-item { text-align: left; background: #fff; border: 1px solid var(--line); border-radius: 6px; padding: 10px; min-height: 62px; }
    .action-item.active { border-color: var(--brand); background: var(--brand-soft); }
    .dataset-item { text-align: left; background: #fff; border: 1px solid var(--line); border-radius: 8px; padding: 10px; min-height: 58px; }
    .dataset-item.active { border-color: var(--brand); background: var(--brand-soft); }
    .dataset-id { font-weight: 700; color: var(--text); word-break: break-all; }
    .dataset-meta { color: var(--muted); font-size: 12px; margin-top: 2px; }
    .table-wrap { overflow: auto; border: 1px solid var(--line); border-radius: 8px; }
    table { width: 100%; border-collapse: collapse; background: #fff; min-width: 760px; }
    th, td { border-bottom: 1px solid var(--line); padding: 9px 10px; text-align: left; vertical-align: top; }
    th { background: var(--panel-2); font-size: 12px; color: var(--muted); position: sticky; top: 0; }
    tr:last-child td { border-bottom: 0; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace; }
    pre { margin: 0; white-space: pre-wrap; word-break: break-word; font-size: 12px; }
    .pill { display: inline-flex; align-items: center; min-height: 22px; padding: 2px 7px; border-radius: 999px; background: #eef2f6; color: #46515a; font-size: 12px; margin: 0 4px 4px 0; }
    .empty { padding: 28px; text-align: center; color: var(--muted); border: 1px dashed var(--line); border-radius: 8px; background: #fff; }
    .result { background: #101418; color: #e8edf2; border-radius: 8px; padding: 12px; min-height: 120px; max-height: 360px; overflow: auto; }
    .hide { display: none !important; }
    @media (max-width: 960px) {
      header { grid-template-columns: 1fr; position: static; }
      .topbar, .layout, .workspace-shell, .grid-2, .grid-3 { grid-template-columns: 1fr; min-width: 0; }
      .setup-panel { grid-template-columns: 1fr; margin: 12px; }
      main { padding: 12px; }
      .resource-sidebar { position: static; max-height: none; }
      .tabs { display: flex; border-right: 0; overflow-x: auto; }
      .tab { width: auto; }
      .module-header { display: grid; }
      .summary-bar { grid-template-columns: 1fr 1fr; }
      .health-grid { grid-template-columns: 1fr 1fr; }
      .risk-grid { grid-template-columns: 1fr 1fr; }
      .queue-grid { grid-template-columns: 1fr 1fr; }
      .integration-grid { grid-template-columns: 1fr 1fr; }
      .coverage-grid { grid-template-columns: 1fr 1fr; }
      .domain-strip { grid-template-columns: 1fr 1fr; }
      .capability-grid { grid-template-columns: 1fr 1fr; }
      .intent-row, .intent-card { grid-template-columns: 1fr; }
      .readiness-grid { grid-template-columns: 1fr 1fr; }
      .activity-row { grid-template-columns: 1fr; gap: 3px; }
      .overview-grid { grid-template-columns: 1fr; }
      .dataset-list { max-height: 280px; }
    }
  </style>
</head>
<body>
  <header>
    <div class="brand">
      <div class="brand-mark">MIS</div>
      <div class="brand-copy">
        <h1 data-i18n="app.title">MaClawDataSrv MIS Admin Console</h1>
        <div class="product-subtitle" data-i18n="app.subtitle">Enterprise structured data operations</div>
        <div class="status" id="serviceStatus">Not connected</div>
      </div>
    </div>
    <div class="topbar">
      <label class="label">Endpoint<input id="endpoint" data-testid="endpoint" value="" placeholder="http://127.0.0.1:18180"></label>
      <label class="label">Token<input id="token" data-testid="token" type="password" autocomplete="off"></label>
      <label class="label">Tenant<input id="tenant" data-testid="tenant" value="default"></label>
      <label class="label">User<input id="user" data-testid="user" value="web-console"></label>
      <label class="label">Role<input id="role" data-testid="role" value="data_user" placeholder="data_user"></label>
      <label class="label">Language<select id="language" data-testid="language-switch"><option value="en">English</option><option value="zh">中文</option></select></label>
      <button class="primary" id="saveAuth" data-testid="connect">Connect</button>
    </div>
  </header>

  <section class="panel setup-panel" id="adminSetupPanel" data-testid="admin-setup-panel">
    <div class="setup-copy">
      <h2>Administrator access</h2>
      <p>On first launch, create the local administrator account. After initialization, sign in to receive a temporary bearer token.</p>
      <div class="status" id="setupStatusText" data-testid="setup-status-text">Checking setup status</div>
      <div class="status" id="adminPasswordPolicy" data-testid="admin-password-policy">Password policy loading</div>
    </div>
    <div class="stack" id="adminInitBox" data-testid="admin-init-box">
      <h3>First-time setup</h3>
      <label class="label">Admin username<input id="initUsername" data-testid="init-username" autocomplete="username" placeholder="admin"></label>
      <label class="label">Admin display name<input id="initDisplayName" data-testid="init-display-name" placeholder="Data Administrator"></label>
      <label class="label">Admin password<input id="initPassword" data-testid="init-password" type="password" autocomplete="new-password" placeholder="At least 8 characters"></label>
      <button class="primary" id="initializeAdmin" data-testid="initialize-admin">Initialize administrator</button>
    </div>
    <div class="stack" id="adminLoginBox" data-testid="admin-login-box">
      <h3>Admin sign in</h3>
      <label class="label">Admin username<input id="loginUsername" data-testid="login-username" autocomplete="username" placeholder="admin"></label>
      <label class="label">Admin password<input id="loginPassword" data-testid="login-password" type="password" autocomplete="current-password"></label>
      <button class="primary" id="loginAdmin" data-testid="login-admin">Sign in</button>
    </div>
  </section>

  <main class="layout">
    <aside class="panel stack resource-sidebar">
      <div class="row" style="justify-content: space-between;">
        <h2>Datasets</h2>
        <button id="refreshDatasets" data-testid="refresh-datasets">Refresh</button>
      </div>
      <div class="dataset-list" id="datasetList" data-testid="dataset-list"></div>
      <section class="stack">
        <h3>Create from Template</h3>
        <label class="label">Template<select id="templateSelect" data-testid="template-select"></select></label>
        <label class="label">Dataset ID override<input id="templateDatasetId" data-testid="template-dataset-id" placeholder="optional, e.g. sales.orders_2026"></label>
        <label class="label">Bootstrap domains<input id="bootstrapDomains" data-testid="bootstrap-domains" placeholder="optional, e.g. sales,finance"></label>
        <div class="row">
          <button class="primary" id="createFromTemplate" data-testid="create-from-template">Create template</button>
          <button id="loadMoreTemplates" data-testid="load-more-templates">Load more templates</button>
          <button id="previewBootstrapTemplates" data-testid="preview-bootstrap-templates">Preview Bootstrap</button>
          <button id="bootstrapTemplates" data-testid="bootstrap-templates">Bootstrap MIS</button>
        </div>
      </section>
      <section class="stack">
        <h3>Custom Dataset</h3>
        <label class="label">Domain<input id="newDomain" data-testid="new-domain" placeholder="sales"></label>
        <label class="label">Name<input id="newName" data-testid="new-name" placeholder="orders"></label>
        <label class="label">Title<input id="newTitle" data-testid="new-title" placeholder="Sales orders"></label>
        <button id="createDataset" data-testid="create-dataset">Create custom</button>
      </section>
    </aside>

    <section class="panel workspace">
      <div class="workspace-shell">
      <div class="tabs" id="tabs">
        <div class="nav-group">Operations</div>
        <button class="tab active" data-tab="overview" data-testid="tab-overview">Overview</button>
        <button class="tab" data-tab="records" data-testid="tab-records">Records</button>
        <button class="tab" data-tab="inbox" data-testid="tab-inbox">Inbox</button>
        <button class="tab" data-tab="domains" data-testid="tab-domains">Domains</button>
        <button class="tab" data-tab="relationships" data-testid="tab-relationships">Relationships</button>
        <button class="tab" data-tab="actions" data-testid="tab-actions">Actions</button>
        <button class="tab" data-tab="rules" data-testid="tab-rules">Rules</button>
        <div class="nav-group">Integration</div>
        <button class="tab" data-tab="connectors" data-testid="tab-connectors">Connectors</button>
        <button class="tab" data-tab="views" data-testid="tab-views">Views</button>
        <button class="tab" data-tab="dashboards" data-testid="tab-dashboards">Dashboards</button>
        <button class="tab" data-tab="reports" data-testid="tab-reports">Reports</button>
        <div class="nav-group">Governance</div>
        <button class="tab" data-tab="quality" data-testid="tab-quality">Quality</button>
        <button class="tab" data-tab="dataset" data-testid="tab-dataset">Dataset</button>
        <button class="tab" data-tab="fields" data-testid="tab-fields">Fields</button>
        <button class="tab" data-tab="write" data-testid="tab-write">Editor</button>
        <button class="tab" data-tab="backups" data-testid="tab-backups">Backups</button>
        <button class="tab" data-tab="events" data-testid="tab-events">Events</button>
        <button class="tab" data-tab="audit" data-testid="tab-audit">Audit</button>
        <div class="nav-group">System</div>
        <button class="tab" data-tab="ops" data-testid="tab-ops">Ops</button>
        <button class="tab" data-tab="access" data-testid="tab-access">Access</button>
        <button class="tab" data-tab="raw" data-testid="tab-raw">Response</button>
      </div>

      <div class="workspace-body">
      <div class="module-header">
        <div>
          <div class="module-kicker" id="moduleKicker">Operations</div>
          <div class="module-title" id="moduleTitle">Overview</div>
          <div class="module-desc" id="moduleDesc">Start from common MIS workflows and service health.</div>
        </div>
        <div class="context-chip" id="moduleContext">Dataset: none selected</div>
      </div>
      <div class="summary-bar" aria-label="Service summary">
        <div class="summary-item"><div class="summary-label">Engine</div><div class="summary-value" id="summaryEngine">-</div></div>
        <div class="summary-item"><div class="summary-label">Schema</div><div class="summary-value" id="summarySchema">-</div></div>
        <div class="summary-item"><div class="summary-label">Datasets</div><div class="summary-value" id="summaryDatasets">0</div></div>
        <div class="summary-item"><div class="summary-label">Records</div><div class="summary-value" id="summaryRecords">0</div></div>
        <div class="summary-item"><div class="summary-label">Backups</div><div class="summary-value" id="summaryBackups">0</div></div>
        <div class="summary-item"><div class="summary-label">Selected Dataset</div><div class="summary-value" id="summarySelectedDataset">-</div></div>
      </div>
      <div class="status-history hide" id="statusHistory" aria-label="Recent status"></div>
      <div id="overview" class="tab-panel stack">
        <section class="checklist" aria-label="Setup checklist">
          <div class="checklist-head">
            <div>
              <div class="checklist-title">Setup Checklist</div>
              <div class="dataset-meta">Prepare the service for reliable company MIS usage.</div>
            </div>
            <button id="refreshOverview" data-testid="refresh-overview">Refresh overview</button>
          </div>
          <div class="checklist-body" id="setupChecklist" data-testid="setup-checklist"></div>
        </section>
        <section class="overview-card">
          <h3>Operational Health</h3>
          <p>Current service counters from the controlled stats API.</p>
          <div class="health-grid" id="overviewHealth" data-testid="overview-health"></div>
        </section>
        <section class="overview-card">
          <h3>MIS Coverage</h3>
          <p>Template and dataset coverage across common enterprise business domains.</p>
          <div class="coverage-grid" id="overviewCoverage" data-testid="overview-coverage"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="domains">Open domains</button>
            <button id="previewOverviewBootstrap" data-testid="preview-overview-bootstrap">Preview bootstrap</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Business Domain Readiness</h3>
          <p>Readiness of sales, finance, HR, legal, procurement, inventory, and asset domains.</p>
          <div class="domain-strip" id="overviewDomainReadiness" data-testid="overview-domain-readiness"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="domains">Manage domains</button>
            <button id="refreshOverviewDomains" data-testid="refresh-overview-domains">Refresh domains</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Business Capabilities</h3>
          <p>Business-first operations exposed to MaClaw agents and human operators.</p>
          <div class="capability-grid" id="overviewCapabilities" data-testid="overview-capabilities"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="actions">Run actions</button>
            <button class="quick-action" data-target-tab="views">Query views</button>
            <button class="quick-action" data-target-tab="reports">Run reports</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Intent Launcher</h3>
          <p>Resolve a business request into actions, views, reports, dashboards, and safe next steps.</p>
          <div class="intent-panel">
            <div class="intent-row">
              <label class="label">Intent<input id="overviewIntentQuery" data-testid="overview-intent-query" placeholder="expense, low stock, sales order status"></label>
              <label class="label">Domain<input id="overviewIntentDomain" data-testid="overview-intent-domain" placeholder="optional"></label>
              <label class="label">Limit<input id="overviewIntentLimit" data-testid="overview-intent-limit" type="number" min="1" max="10" value="3"></label>
              <button class="primary" id="resolveOverviewIntent" data-testid="resolve-overview-intent">Resolve</button>
            </div>
            <div class="intent-results" id="overviewIntentResults" data-testid="overview-intent-results"></div>
          </div>
        </section>
        <section class="overview-card">
          <h3>Work Queue</h3>
          <p>Pending operational work from the MIS inbox summary API.</p>
          <div class="queue-grid" id="overviewWorkQueue" data-testid="overview-work-queue"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="inbox">Open inbox</button>
            <button id="refreshOverviewWorkQueue" data-testid="refresh-overview-work-queue">Refresh queue</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Integration Health</h3>
          <p>Connector status, recent failures, and open dead letters.</p>
          <div class="integration-grid" id="overviewIntegrationHealth" data-testid="overview-integration-health"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="connectors">Open connectors</button>
            <button id="refreshOverviewIntegration" data-testid="refresh-overview-integration">Refresh integration</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Access Risk</h3>
          <p>Managed API key risk summary from the authorization review API.</p>
          <div class="risk-grid" id="overviewAccessRisk" data-testid="overview-access-risk"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="access">Review access</button>
            <button id="refreshOverviewAccessRisk" data-testid="refresh-overview-access-risk">Refresh risk</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Governance Readiness</h3>
          <p>Minimum controls for using DataSrv with real company operations.</p>
          <div class="readiness-grid" id="overviewReadiness" data-testid="overview-readiness"></div>
          <div class="recommendation-list" id="overviewRecommendations" data-testid="overview-recommendations"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="backups">Create backup</button>
            <button class="quick-action" data-target-tab="quality">Check quality</button>
            <button class="quick-action" data-target-tab="audit">Open audit</button>
          </div>
        </section>
        <section class="overview-card">
          <h3>Recent Activity</h3>
          <p>Latest audit trail entries from the controlled audit API.</p>
          <div class="activity-list" id="overviewActivity" data-testid="overview-activity"></div>
          <div class="row">
            <button class="primary quick-action" data-target-tab="audit">Open audit</button>
            <button id="refreshOverviewActivity" data-testid="refresh-overview-activity">Refresh activity</button>
          </div>
        </section>
        <div class="overview-grid">
          <section class="overview-card">
            <h3>Daily Operations</h3>
            <p>Search records, run business actions, and handle pending MIS work from one place.</p>
            <div class="row">
              <button class="primary quick-action" data-target-tab="records">Query records</button>
              <button class="quick-action" data-target-tab="actions">Run actions</button>
              <button class="quick-action" data-target-tab="inbox">Open inbox</button>
            </div>
          </section>
          <section class="overview-card">
            <h3>Analytics</h3>
            <p>Use reports, dashboards, and curated views before asking for raw dataset access.</p>
            <div class="row">
              <button class="primary quick-action" data-target-tab="reports">Run reports</button>
              <button class="quick-action" data-target-tab="dashboards">Open dashboards</button>
              <button class="quick-action" data-target-tab="views">Query views</button>
            </div>
          </section>
          <section class="overview-card">
            <h3>Governance</h3>
            <p>Review access, inspect audit trails, run quality checks, and create backups before risky changes.</p>
            <div class="row">
              <button class="primary quick-action" data-target-tab="access">Review access</button>
              <button class="quick-action" data-target-tab="quality">Check quality</button>
              <button class="quick-action" data-target-tab="backups">Create backup</button>
            </div>
          </section>
        </div>
      </div>

      <div id="records" class="tab-panel stack hide">
        <div class="grid-3">
          <label class="label">Keyword<input id="queryText" data-testid="query-text" placeholder="customer, amount, name"></label>
          <label class="label">Tag<input id="queryTag" data-testid="query-tag" placeholder="q1"></label>
          <label class="label">Limit<input id="queryLimit" data-testid="query-limit" type="number" min="1" max="500" value="50"></label>
        </div>
        <label class="label">Filter JSON<textarea id="queryFilter" data-testid="query-filter" spellcheck="false" placeholder='{"field":"amount","op":"gte","value":1000}'></textarea></label>
        <div class="row">
          <button class="primary" id="queryRecords" data-testid="query-records">Query</button>
          <button id="exportRecordsCsv" data-testid="export-records-csv">Export CSV</button>
          <button id="exportRecordsJsonl" data-testid="export-records-jsonl">Export JSONL</button>
          <button id="startCsvExportJob" data-testid="start-csv-export-job">Start CSV export job</button>
          <button id="startJsonlExportJob" data-testid="start-jsonl-export-job">Start JSONL export job</button>
          <button id="refreshExportJobs" data-testid="refresh-export-jobs">Refresh export jobs</button>
          <button id="clearQuery" data-testid="clear-query">Clear</button>
        </div>
        <div id="exportJobTable" class="table-wrap" data-testid="export-job-table"></div>
        <div id="recordTable" class="table-wrap" data-testid="record-table"></div>
      </div>

      <div id="inbox" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>MIS Inbox</h2>
          <button id="refreshInbox" data-testid="refresh-inbox">Refresh inbox</button>
        </div>
        <div class="grid-3">
          <label class="label">Type<input id="inboxType" data-testid="inbox-type" placeholder="approval, operation_plan, import_job, export_job, quality"></label>
          <label class="label">Status<input id="inboxStatus" data-testid="inbox-status" placeholder="pending, failed, issue"></label>
          <label class="label">Limit<input id="inboxLimit" data-testid="inbox-limit" type="number" min="1" max="500" value="100"></label>
        </div>
        <label class="check"><input id="inboxIncludeOK" data-testid="inbox-include-ok" type="checkbox"> Include completed or OK items</label>
        <div id="inboxSummary" class="table-wrap" data-testid="inbox-summary"></div>
        <div id="inboxTable" class="table-wrap" data-testid="inbox-table"></div>
      </div>

      <div id="domains" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Business Domains</h2>
          <button id="refreshDomains" data-testid="refresh-domains">Refresh domains</button>
        </div>
        <div class="grid-3">
          <label class="label">Intent<input id="intentQuery" data-testid="intent-query" placeholder="e.g. low stock, expense reimbursement, procurement status"></label>
          <label class="label">Domain<input id="intentDomain" data-testid="intent-domain" placeholder="optional, e.g. inventory"></label>
          <label class="label">Limit<input id="intentLimit" data-testid="intent-limit" type="number" min="1" max="20" value="5"></label>
        </div>
        <div class="row">
          <button class="primary" id="resolveIntent" data-testid="resolve-intent">Resolve intent</button>
        </div>
        <div id="intentResultTable" class="table-wrap" data-testid="intent-result-table"></div>
        <div id="domainTable" class="table-wrap" data-testid="domain-table"></div>
      </div>

      <div id="relationships" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Relationships</h2>
          <button id="refreshRelationships" data-testid="refresh-relationships">Refresh relationships</button>
        </div>
        <div class="grid-2">
          <label class="label">Dataset filter<input id="relationshipDataset" data-testid="relationship-dataset" placeholder="optional, e.g. sales.orders"></label>
          <label class="label">Current selected<input id="relationshipSelectedDataset" class="readonly" readonly></label>
        </div>
        <div class="row">
          <button id="useSelectedDatasetForRelationships" data-testid="use-selected-dataset-relationships">Use selected dataset</button>
          <button id="clearRelationshipFilter" data-testid="clear-relationship-filter">Clear filter</button>
        </div>
        <div id="relationshipTable" class="table-wrap" data-testid="relationship-table"></div>
      </div>

      <div id="actions" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Business Actions</h2>
          <button id="refreshActions" data-testid="refresh-actions">Refresh</button>
        </div>
        <div class="action-list" id="actionList" data-testid="action-list"></div>
        <div class="grid-2">
          <label class="label">Action ID<input id="businessActionId" class="readonly" readonly></label>
          <label class="label">Target Dataset<input id="businessActionDataset" class="readonly" readonly></label>
        </div>
        <label class="label">Description<textarea id="businessActionDescription" class="readonly" readonly></textarea></label>
        <label class="label">Input JSON<textarea id="businessActionData" data-testid="business-action-data" spellcheck="false">{}</textarea></label>
        <div class="grid-2">
          <label class="label">Record ID<input id="businessActionRecordId" data-testid="business-action-record-id" placeholder="external business id"></label>
          <label class="label">Idempotency Key<input id="businessActionIdempotencyKey" data-testid="business-action-idempotency-key" placeholder="source:object:id:version"></label>
        </div>
        <div class="row">
          <button id="dryRunBusinessAction" data-testid="dry-run-business-action">Dry-run action</button>
          <button class="primary" id="executeBusinessAction" data-testid="execute-business-action">Execute action</button>
          <button id="checkBusinessRules" data-testid="check-business-rules">Check rules</button>
          <button id="loadEventContract" data-testid="load-event-contract">Event contract</button>
          <button id="formatBusinessActionData" data-testid="format-business-action-data">Format JSON</button>
        </div>
        <div id="businessActionResult" class="table-wrap" data-testid="business-action-result"></div>
        <h3>Event Contracts</h3>
        <div class="grid-2">
          <label class="label">Domain filter<input id="eventContractDomain" data-testid="event-contract-domain" placeholder="optional, e.g. sales"></label>
          <button id="refreshEventContracts" data-testid="refresh-event-contracts">Refresh contracts</button>
        </div>
        <div id="eventContractTable" class="table-wrap" data-testid="event-contract-table"></div>
      </div>

      <div id="rules" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Business Rules</h2>
          <button id="refreshRules" data-testid="refresh-rules">Refresh</button>
        </div>
        <div class="grid-3">
          <label class="label">Domain<input id="ruleDomain" data-testid="rule-domain" placeholder="finance"></label>
          <label class="label">Business Action<input id="ruleBusinessAction" data-testid="rule-business-action" placeholder="finance.expense_submit"></label>
          <label class="label">Severity<input id="ruleSeverity" data-testid="rule-severity" placeholder="high, critical"></label>
        </div>
        <div class="row">
          <button id="useSelectedActionForRules" data-testid="use-selected-action-rules">Use selected action</button>
          <button class="primary" id="evaluateRules" data-testid="evaluate-rules">Evaluate rules</button>
        </div>
        <div id="ruleTable" class="table-wrap" data-testid="rule-table"></div>
        <div id="ruleEvaluation" class="table-wrap" data-testid="rule-evaluation"></div>
      </div>

      <div id="connectors" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>External Connectors</h2>
          <div class="row">
            <button id="refreshConnectors" data-testid="refresh-connectors">Refresh</button>
            <button id="refreshConnectorHealth" data-testid="refresh-connector-health">Health overview</button>
          </div>
        </div>
        <div id="connectorTable" class="table-wrap" data-testid="connector-table"></div>
        <div class="grid-3">
          <label class="label">Connector ID<input id="connectorId" data-testid="connector-id" placeholder="sales.crm"></label>
          <label class="label">Name<input id="connectorName" data-testid="connector-name" placeholder="Sales CRM"></label>
          <label class="label">Domain<input id="connectorDomain" data-testid="connector-domain" placeholder="sales"></label>
        </div>
        <div class="grid-3">
          <label class="label">Kind<input id="connectorKind" data-testid="connector-kind" placeholder="crm, erp, hris"></label>
          <label class="label">Auth type<input id="connectorAuthType" data-testid="connector-auth-type" placeholder="bearer, api_key"></label>
          <label class="label">Token ref<input id="connectorTokenRef" data-testid="connector-token-ref" placeholder="MIS_CRM_TOKEN"></label>
        </div>
        <label class="label">Base URL<input id="connectorBaseUrl" data-testid="connector-base-url" placeholder="https://crm.example.local"></label>
        <label class="label">Subscribed actions<textarea id="connectorActions" data-testid="connector-actions" spellcheck="false">["sales.order_upsert"]</textarea></label>
        <label class="label">Config JSON<textarea id="connectorConfig" data-testid="connector-config" spellcheck="false">{}</textarea></label>
        <label class="check"><input id="connectorEnabled" data-testid="connector-enabled" type="checkbox" checked> Enabled</label>
        <div class="row">
          <button class="primary" id="saveConnector" data-testid="save-connector">Save connector</button>
          <button id="testConnector" data-testid="test-connector">Test contract bindings</button>
          <button id="validateConnectorConfig" data-testid="validate-connector-config">Validate config</button>
          <button id="checkConnectorReadiness" data-testid="check-connector-readiness">Readiness</button>
          <button id="checkConnectorHealth" data-testid="check-connector-health">Check health</button>
          <button id="getConnectorSyncState" data-testid="get-connector-sync-state">Sync state</button>
          <button id="listConnectorSyncRuns" data-testid="list-connector-sync-runs">Sync runs</button>
          <button id="markConnectorSyncSuccess" data-testid="mark-connector-sync-success">Mark sync success</button>
          <button id="planConnectorSync" data-testid="plan-connector-sync">Plan sync</button>
          <button id="runConnectorSyncBatch" data-testid="run-connector-sync-batch">Run sync batch</button>
          <button id="suggestConnectorMapping" data-testid="suggest-connector-mapping">Suggest mapping</button>
          <button id="applySuggestedConnectorMapping" data-testid="apply-suggested-connector-mapping">Use suggestion</button>
          <button id="saveSuggestedConnectorMapping" data-testid="save-suggested-connector-mapping">Save suggestion</button>
          <button id="loadConnectorEventTemplate" data-testid="load-connector-event-template">Load event template</button>
          <button id="previewConnectorEvent" data-testid="preview-connector-event">Preview mapped event</button>
          <button id="formatConnectorConfig" data-testid="format-connector-config">Format JSON</button>
        </div>
        <div id="connectorSyncRuns" class="table-wrap" data-testid="connector-sync-runs"></div>
      </div>

      <div id="views" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Business Views</h2>
          <button id="refreshViews" data-testid="refresh-views">Refresh</button>
        </div>
        <div class="action-list" id="viewList" data-testid="view-list"></div>
        <div class="grid-2">
          <label class="label">View ID<input id="businessViewId" class="readonly" readonly></label>
          <label class="label">Dataset<input id="businessViewDataset" class="readonly" readonly></label>
        </div>
        <label class="label">Description<textarea id="businessViewDescription" class="readonly" readonly></textarea></label>
        <div class="grid-3">
          <label class="label">Keyword<input id="viewQueryText" data-testid="view-query-text" placeholder="customer, employee"></label>
          <label class="label">Tag<input id="viewQueryTag" data-testid="view-query-tag" placeholder="optional"></label>
          <label class="label">Limit<input id="viewQueryLimit" data-testid="view-query-limit" type="number" min="1" max="500" value="100"></label>
        </div>
        <label class="label">Filter JSON<textarea id="viewQueryFilter" data-testid="view-query-filter" spellcheck="false">{}</textarea></label>
        <div class="row">
          <button class="primary" id="queryBusinessView" data-testid="query-business-view">Query view</button>
          <button id="formatViewQuery" data-testid="format-view-query">Format filter</button>
        </div>
        <div id="viewRecordTable" class="table-wrap" data-testid="view-record-table"></div>
      </div>

      <div id="reports" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Reports</h2>
          <button id="refreshReports" data-testid="refresh-reports">Refresh</button>
        </div>
        <div class="action-list" id="reportList" data-testid="report-list"></div>
        <div class="grid-2">
          <label class="label">Report ID<input id="reportId" class="readonly" readonly></label>
          <label class="label">Dataset<input id="reportDataset" class="readonly" readonly></label>
        </div>
        <label class="label">Report filter JSON<textarea id="reportFilter" data-testid="report-filter" spellcheck="false">{}</textarea></label>
        <div class="row">
          <button class="primary" id="runReport" data-testid="run-report">Run report</button>
          <button id="formatReportFilter" data-testid="format-report-filter">Format filter</button>
        </div>
        <label class="label">Ad-hoc aggregate JSON<textarea id="aggregateJson" data-testid="aggregate-json" spellcheck="false">{
  "group_by": ["stage"],
  "metrics": [
    {"name":"count","op":"count"},
    {"name":"distinct_customers","op":"count_distinct","field":"customer"},
    {"name":"amount","op":"sum","field":"amount"}
  ],
  "limit": 500
}</textarea></label>
        <div class="row">
          <button class="primary" id="runAggregate" data-testid="run-aggregate">Run aggregate on selected dataset</button>
          <button id="formatAggregate" data-testid="format-aggregate">Format aggregate</button>
        </div>
        <div id="reportTable" class="table-wrap" data-testid="report-table"></div>
      </div>

      <div id="dashboards" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Dashboards</h2>
          <button id="refreshDashboards" data-testid="refresh-dashboards">Refresh</button>
        </div>
        <div class="action-list" id="dashboardList" data-testid="dashboard-list"></div>
        <div class="grid-2">
          <label class="label">Dashboard ID<input id="dashboardId" class="readonly" readonly></label>
          <label class="label">Domain<input id="dashboardDomain" class="readonly" readonly></label>
        </div>
        <label class="label">Description<textarea id="dashboardDescription" class="readonly" readonly></textarea></label>
        <div class="row">
          <button class="primary" id="runDashboard" data-testid="run-dashboard">Run dashboard</button>
        </div>
        <div id="dashboardSummary" class="table-wrap" data-testid="dashboard-summary"></div>
        <div id="dashboardReportTable" class="table-wrap" data-testid="dashboard-report-table"></div>
      </div>

      <div id="quality" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Data Quality</h2>
          <button id="refreshQualityChecks" data-testid="refresh-quality-checks">Refresh checks</button>
        </div>
        <div class="action-list" id="qualityCheckList" data-testid="quality-check-list"></div>
        <div class="grid-3">
          <label class="label">Limit<input id="qualityLimit" data-testid="quality-limit" type="number" min="1" max="5000" value="1000"></label>
          <label class="label">Include warnings<select id="qualityIncludeWarnings" data-testid="quality-include-warnings"><option value="true">true</option><option value="false">false</option></select></label>
          <label class="label">Checks<input id="qualityChecks" data-testid="quality-checks" placeholder="blank = all"></label>
        </div>
        <div class="row">
          <button class="primary" id="runQualityCheck" data-testid="run-quality-check">Run on selected dataset</button>
          <button id="refreshQualityRuns" data-testid="refresh-quality-runs">Refresh runs</button>
        </div>
        <div id="qualityRunTable" class="table-wrap" data-testid="quality-run-table"></div>
        <div id="qualityTable" class="table-wrap" data-testid="quality-table"></div>
      </div>

      <div id="dataset" class="tab-panel stack hide">
        <div class="grid-3">
          <label class="label">ID<input id="datasetId" class="readonly" readonly></label>
          <label class="label">Domain<input id="datasetDomain" class="readonly" readonly></label>
          <label class="label">Name<input id="datasetName" class="readonly" readonly></label>
        </div>
        <label class="label">Title<input id="datasetTitle" data-testid="dataset-title"></label>
        <label class="label">Description<textarea id="datasetDescription" data-testid="dataset-description" spellcheck="false"></textarea></label>
        <div class="row">
          <button class="primary" id="reloadDataset" data-testid="reload-dataset">Reload</button>
          <button class="primary" id="updateDataset" data-testid="update-dataset">Update</button>
          <button class="danger" id="deleteDataset" data-testid="delete-dataset">Delete dataset</button>
        </div>
      </div>

      <div id="fields" class="tab-panel stack hide">
        <div class="row">
          <button class="primary" id="loadFields" data-testid="load-fields">Load fields</button>
          <button id="formatFields" data-testid="format-fields">Format JSON</button>
        </div>
        <label class="label">Field definitions JSON<textarea id="fieldsJson" data-testid="fields-json" spellcheck="false">{
  "fields": [
    {"key":"amount","type":"number","title":"Amount","indexed":true},
    {"key":"customer","type":"string","title":"Customer","indexed":true}
  ]
}</textarea></label>
        <button class="primary" id="saveFields" data-testid="save-fields">Save fields</button>
        <h3>Schema Proposal</h3>
        <div class="row">
          <button id="refreshSchemaProposals" data-testid="refresh-schema-proposals">Refresh proposals</button>
        </div>
        <div id="schemaProposalList" class="table-wrap" data-testid="schema-proposal-list"></div>
        <label class="label">Sample business data JSON<textarea id="schemaSampleJson" data-testid="schema-sample-json" spellcheck="false">{
  "new_status": "pending",
  "new_amount": 1000
}</textarea></label>
        <div class="row">
          <button id="proposeSchema" data-testid="propose-schema">Propose schema</button>
          <button class="primary" id="applySchemaProposal" data-testid="apply-schema-proposal">Apply proposal with confirmation</button>
        </div>
        <label class="label">Proposal JSON<textarea id="schemaProposalJson" data-testid="schema-proposal-json" spellcheck="false">{"fields":[]}</textarea></label>
      </div>

      <div id="write" class="tab-panel stack hide">
        <div class="grid-2">
          <label class="label">Record ID<input id="recordId" data-testid="record-id" placeholder="Leave blank to create"></label>
          <label class="label">Title<input id="recordTitle" data-testid="record-title"></label>
        </div>
        <label class="label">Tags, comma separated<input id="recordTags" data-testid="record-tags" placeholder="q1, imported"></label>
        <label class="label">Data JSON<textarea id="recordData" data-testid="record-data" spellcheck="false">{
  "customer": "Acme",
  "amount": 8800
}</textarea></label>
        <div class="row">
          <button id="validateRecord" data-testid="validate-record">Validate</button>
          <button class="primary" id="saveRecord" data-testid="save-record">Save record</button>
          <button id="newRecord" data-testid="new-record">New record</button>
          <button id="loadRecordRevisions" data-testid="load-record-revisions">Load revisions</button>
          <button id="loadRelatedRecords" data-testid="load-related-records">Load related</button>
          <button id="loadRecordTimeline" data-testid="load-record-timeline">Load timeline</button>
          <button id="restoreRecord" data-testid="restore-record">Restore deleted</button>
          <button class="danger" id="deleteRecord" data-testid="delete-record">Delete record</button>
        </div>
        <div id="revisionTable" class="table-wrap" data-testid="revision-table"></div>
        <h3>Approvals</h3>
        <label class="label">Approval JSON<textarea id="approvalJson" data-testid="approval-json" spellcheck="false">{
  "kind": "general",
  "priority": "medium",
  "summary": "Approve this business record",
  "assigned_to": "",
  "due_at": "",
  "request": {}
}</textarea></label>
        <div class="row">
          <button id="createApproval" data-testid="create-approval">Create approval</button>
          <button id="refreshApprovals" data-testid="refresh-approvals">Refresh approvals</button>
          <button id="formatApproval" data-testid="format-approval">Format approval</button>
        </div>
        <div id="approvalTable" class="table-wrap" data-testid="approval-table"></div>
        <h3>Bulk Update</h3>
        <label class="label">Bulk update JSON<textarea id="bulkUpdateJson" data-testid="bulk-update-json" spellcheck="false">{
  "query": {"filter":{"field":"stage","op":"eq","value":"draft"},"limit":100},
  "set": {"stage":"confirmed"},
  "unset": [],
  "dry_run": true,
  "reason": "controlled cleanup"
}</textarea></label>
        <div class="row">
          <button id="dryRunBulkUpdate" data-testid="dry-run-bulk-update">Dry-run bulk update</button>
          <button class="primary" id="runBulkUpdate" data-testid="run-bulk-update">Apply bulk update</button>
          <button id="formatBulkUpdate" data-testid="format-bulk-update">Format bulk update</button>
        </div>
        <div id="bulkUpdateTable" class="table-wrap" data-testid="bulk-update-table"></div>
        <h3>Bulk Delete</h3>
        <label class="label">Bulk delete JSON<textarea id="bulkDeleteJson" data-testid="bulk-delete-json" spellcheck="false">{
  "query": {"filter":{"field":"stage","op":"eq","value":"cancelled"},"limit":100},
  "dry_run": true,
  "reason": "controlled cleanup"
}</textarea></label>
        <div class="row">
          <button id="dryRunBulkDelete" data-testid="dry-run-bulk-delete">Dry-run bulk delete</button>
          <button class="danger" id="runBulkDelete" data-testid="run-bulk-delete">Apply bulk delete</button>
          <button id="formatBulkDelete" data-testid="format-bulk-delete">Format bulk delete</button>
        </div>
        <div id="bulkDeleteTable" class="table-wrap" data-testid="bulk-delete-table"></div>
        <h3>Batch Import</h3>
        <label class="label">Records JSON<textarea id="batchRecordsJson" data-testid="batch-records-json" spellcheck="false">[
  {
    "id": "SO-BATCH-1",
    "title": "Batch order",
    "tags": ["batch"],
    "data": {"customer":"Acme", "amount": 8800}
  }
]</textarea></label>
        <div class="row">
          <button id="dryRunBatchImport" data-testid="dry-run-batch-import">Dry-run batch</button>
          <button class="primary" id="runBatchImport" data-testid="run-batch-import">Import batch</button>
          <button id="startBatchImportJob" data-testid="start-batch-import-job">Start batch job</button>
          <button id="formatBatchRecords" data-testid="format-batch-records">Format batch</button>
        </div>
        <h3>CSV Import</h3>
        <label class="label">CSV text<textarea id="csvImportText" data-testid="csv-import-text" spellcheck="false">id,order_no,customer,amount,stage
SO-CSV-1,SO-CSV-1,Acme,8800,confirmed</textarea></label>
        <div class="row">
          <button id="loadCsvTemplate" data-testid="load-csv-template">Load CSV template</button>
          <button id="dryRunCsvImport" data-testid="dry-run-csv-import">Dry-run CSV</button>
          <button class="primary" id="runCsvImport" data-testid="run-csv-import">Import CSV</button>
          <button id="startCsvImportJob" data-testid="start-csv-import-job">Start CSV job</button>
        </div>
        <h3>JSONL Import</h3>
        <label class="label">JSONL text<textarea id="jsonlImportText" data-testid="jsonl-import-text" spellcheck="false">{"id":"SO-JSONL-1","data":{"order_no":"SO-JSONL-1","customer":"Acme","amount":8800,"stage":"confirmed"}}</textarea></label>
        <div class="row">
          <button id="dryRunJsonlImport" data-testid="dry-run-jsonl-import">Dry-run JSONL</button>
          <button class="primary" id="runJsonlImport" data-testid="run-jsonl-import">Import JSONL</button>
          <button id="startJsonlImportJob" data-testid="start-jsonl-import-job">Start JSONL job</button>
          <button id="refreshImportJobs" data-testid="refresh-import-jobs">Refresh jobs</button>
        </div>
        <div id="importJobTable" class="table-wrap" data-testid="import-job-table"></div>
      </div>

      <div id="backups" class="tab-panel stack hide">
        <div class="grid-2">
          <label class="label">Backup name<input id="backupName" data-testid="backup-name" placeholder="before_import"></label>
          <label class="label">Note<input id="backupNote" data-testid="backup-note" placeholder="Before batch operation"></label>
        </div>
        <div class="row">
          <button class="primary" id="createBackup" data-testid="create-backup">Create backup</button>
          <button id="listBackups" data-testid="list-backups">Refresh backups</button>
        </div>
        <div id="backupTable" class="table-wrap" data-testid="backup-table"></div>
      </div>

      <div id="audit" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Audit</h2>
          <div class="row">
            <button id="refreshAudit" data-testid="refresh-audit">Refresh audit</button>
            <button id="exportAuditCsv" data-testid="export-audit-csv">Export CSV</button>
          </div>
        </div>
        <div class="grid-3">
          <label class="label">Dataset filter<input id="auditDataset" data-testid="audit-dataset" placeholder="optional"></label>
          <label class="label">Action filter<input id="auditAction" data-testid="audit-action" placeholder="record.create"></label>
          <label class="label">Limit<input id="auditLimit" data-testid="audit-limit" type="number" min="1" max="500" value="100"></label>
        </div>
        <div class="grid-3">
          <label class="label">User filter<input id="auditUser" data-testid="audit-user" placeholder="optional"></label>
          <label class="label">Target ID<input id="auditTargetId" data-testid="audit-target-id" placeholder="record id, plan id"></label>
          <label class="label">Keyword<input id="auditKeyword" data-testid="audit-keyword" placeholder="summary, action, target"></label>
        </div>
        <label class="label">Target type<input id="auditTargetType" data-testid="audit-target-type" placeholder="record, dataset, operation_plan"></label>
        <div id="auditTable" class="table-wrap" data-testid="audit-table"></div>
      </div>

      <div id="events" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Data Events</h2>
          <button id="refreshEvents" data-testid="refresh-events">Refresh events</button>
        </div>
        <div class="grid-3">
          <label class="label">Dataset filter<input id="eventDataset" data-testid="event-dataset" placeholder="optional"></label>
          <label class="label">Source filter<input id="eventSource" data-testid="event-source" placeholder="crm"></label>
          <label class="label">Limit<input id="eventLimit" data-testid="event-limit" type="number" min="1" max="500" value="100"></label>
        </div>
        <div class="grid-2">
          <label class="label">Event type<input id="eventType" data-testid="event-type" placeholder="sales.order.updated"></label>
          <label class="label">Idempotency key<input id="eventIdempotencyKey" data-testid="event-idempotency-key" placeholder="optional"></label>
        </div>
        <label class="label">Business action filter<input id="eventBusinessAction" data-testid="event-business-action" placeholder="sales.order_upsert"></label>
        <label class="label">Ingest event JSON<textarea id="eventIngestJson" data-testid="event-ingest-json" spellcheck="false">{
  "source": "crm",
  "business_action_id": "sales.order_upsert",
  "record_id": "SO-0001",
  "idempotency_key": "crm:sales.order:SO-0001:v1",
  "data": {
    "order_no": "SO-0001",
    "customer": "Acme",
    "amount": 1000
  }
}</textarea></label>
        <div class="row">
          <button id="dryRunEvent" data-testid="dry-run-event">Dry-run event</button>
          <button id="ingestEvent" data-testid="ingest-event">Ingest event</button>
          <button id="formatEventIngest" data-testid="format-event-ingest">Format event</button>
        </div>
        <div id="eventTable" class="table-wrap" data-testid="event-table"></div>
        <div class="row" style="justify-content: space-between;">
          <h3>Dead Letters</h3>
          <button id="refreshDeadLetters" data-testid="refresh-dead-letters">Refresh dead letters</button>
        </div>
        <div id="deadLetterTable" class="table-wrap" data-testid="dead-letter-table"></div>
      </div>

      <div id="ops" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Operations</h2>
          <button id="refreshStats" data-testid="refresh-stats">Refresh stats</button>
        </div>
        <div class="row">
          <button id="runIntegrityCheck" data-testid="run-integrity-check">Integrity check</button>
          <button id="runOptimize" data-testid="run-optimize">Optimize</button>
          <button class="danger" id="runVacuum" data-testid="run-vacuum">Vacuum</button>
        </div>
        <div id="maintenanceResult" class="table-wrap" data-testid="maintenance-result"></div>
        <h3>Operation Plans</h3>
        <label class="label">Operation plan JSON<textarea id="operationPlanJson" data-testid="operation-plan-json" spellcheck="false">{
  "dataset_id": "sales.orders",
  "operation": "bulk_update_records",
  "summary": "Confirm draft sales orders",
  "risk_level": "high",
  "request": {
    "query": {"filter":{"field":"stage","op":"eq","value":"draft"},"limit":100},
    "set": {"stage":"confirmed"},
    "reason": "operation plan"
  }
}</textarea></label>
        <div class="row">
          <button id="createOperationPlan" data-testid="create-operation-plan">Create plan</button>
          <button id="refreshOperationPlans" data-testid="refresh-operation-plans">Refresh plans</button>
          <button id="formatOperationPlan" data-testid="format-operation-plan">Format plan</button>
        </div>
        <div id="operationPlanTable" class="table-wrap" data-testid="operation-plan-table"></div>
        <div id="statsSummary" class="table-wrap" data-testid="stats-summary"></div>
        <div id="statsDatasetTable" class="table-wrap" data-testid="stats-dataset-table"></div>
      </div>

      <div id="raw" class="tab-panel stack hide">
        <pre class="result" id="rawOutput" data-testid="raw-output">{}</pre>
      </div>

      <div id="access" class="tab-panel stack hide">
        <div class="row" style="justify-content: space-between;">
          <h2>Business Access</h2>
          <button id="refreshAccessCatalog" data-testid="refresh-access-catalog">Refresh catalog</button>
        </div>
        <section class="overview-card">
          <h3>Agent Authorization Workspace</h3>
          <p>Grant agents by business capability first; raw datasets and sensitive fields stay explicit exceptions.</p>
          <div id="accessWorkspaceSummary" class="access-summary-grid" data-testid="access-workspace-summary"></div>
          <div class="access-playbook">
            <div class="access-playbook-card">
              <strong>1. Pick a business role</strong>
              <span>Start from a preset for sales, finance, HR, legal, audit, or read-only reporting.</span>
              <button class="small" id="accessGuideLoadPresets" data-testid="access-guide-load-presets">Load presets</button>
            </div>
            <div class="access-playbook-card">
              <strong>2. Grant business operations</strong>
              <span>Select actions, views, reports, and dashboards. Leave raw data disabled unless the agent truly administers data.</span>
              <button class="small" id="accessGuideGrantAnalytics" data-testid="access-guide-grant-analytics">Select analytics</button>
            </div>
            <div class="access-playbook-card">
              <strong>3. Verify and rotate</strong>
              <span>Preview effective capabilities, run access checks, then rotate or disable stale keys from the managed list.</span>
              <button class="small" id="accessGuideReview" data-testid="access-guide-review">Review now</button>
            </div>
          </div>
        </section>
        <div class="grid-3">
          <label class="label">API key ID<input id="accessKeyId" data-testid="access-key-id" placeholder="sales-agent"></label>
          <label class="label">User / agent<input id="accessUserId" data-testid="access-user-id" placeholder="agent_sales"></label>
          <label class="label">Role<select id="accessRole" data-testid="access-role"><option value="data_user">data_user</option><option value="data_auditor">data_auditor</option><option value="data_admin">data_admin</option></select></label>
        </div>
        <div class="grid-3">
          <label class="label">Authorization preset<select id="accessPreset" data-testid="access-preset"><option value="">Custom</option></select></label>
          <button id="applyAccessPreset" data-testid="apply-access-preset">Apply preset</button>
          <button id="loadMoreAccessPresets" data-testid="load-more-access-presets">Load more presets</button>
        </div>
        <div class="grid-3">
          <label class="label">Agent purpose<input id="accessAgentPurpose" data-testid="access-agent-purpose" placeholder="finance reporting agent, sales order processor"></label>
          <button id="recommendAccessPolicy" data-testid="recommend-access-policy">Recommend authorization</button>
          <button id="clearAccessRecommendation" data-testid="clear-access-recommendation">Clear recommendation</button>
        </div>
        <div id="accessRecommendation" class="table-wrap" data-testid="access-recommendation"></div>
        <div class="grid-3">
          <label class="label">Expires at<input id="accessExpiresAt" data-testid="access-expires-at" placeholder="2026-12-31 or RFC3339"></label>
        </div>
        <div class="grid-3">
          <label class="check"><input id="accessAllowReports" data-testid="access-allow-reports" type="checkbox" checked> Allow selected views/reports/dashboards</label>
          <label class="check"><input id="accessAllowRawData" data-testid="access-allow-raw-data" type="checkbox"> Allow raw dataset API</label>
          <label class="check"><input id="accessAllowSensitive" data-testid="access-allow-sensitive" type="checkbox"> Allow sensitive fields</label>
        </div>
        <div class="grid-3">
          <label class="check"><input id="accessAllowAdmin" data-testid="access-allow-admin" type="checkbox"> Allow admin operations</label>
        </div>
        <div class="row">
          <button class="primary" id="generateAccessPolicy" data-testid="generate-access-policy">Generate policy</button>
          <button id="createAccessKey" data-testid="create-access-key">Create managed key</button>
          <button id="updateAccessKey" data-testid="update-access-key">Update managed key</button>
          <button id="previewAccessKey" data-testid="preview-access-key">Preview key access</button>
          <button id="checkAccessKey" data-testid="check-access-key">Check access</button>
          <button id="reviewAccessKeys" data-testid="review-access-keys">Review access</button>
          <button id="exportAccessReview" data-testid="export-access-review">Export review</button>
          <button id="refreshEvidenceSummary" data-testid="refresh-evidence-summary">Refresh evidence summary</button>
          <button id="downloadEvidenceSummary" data-testid="download-evidence-summary">Download summary</button>
          <button id="exportEvidencePack" data-testid="export-evidence-pack">Export evidence pack</button>
          <button id="planAccessRemediation" data-testid="plan-access-remediation">Plan remediation</button>
          <button id="refreshAccessKeys" data-testid="refresh-access-keys">Refresh keys</button>
          <button id="generateAgentHandoff" data-testid="generate-agent-handoff">Generate agent handoff</button>
          <button id="runAgentReadiness" data-testid="run-agent-readiness">Run agent readiness</button>
          <button id="compareAccessPolicy" data-testid="compare-access-policy">Compare policy changes</button>
          <button id="generateAgentOnboarding" data-testid="generate-agent-onboarding">Generate onboarding checklist</button>
          <button id="generateAgentPacket" data-testid="generate-agent-packet">Generate onboarding packet</button>
        </div>
        <div class="grid-3">
          <label class="label">Check type<select id="accessCheckType" data-testid="access-check-type"><option value="business_action">Business action</option><option value="report">Report</option><option value="business_view">Business view</option><option value="dashboard">Dashboard</option><option value="dataset">Raw dataset</option><option value="admin">Admin</option><option value="sensitive">Sensitive fields</option><option value="domain">Domain</option></select></label>
          <label class="label">Check resource<input id="accessCheckResource" data-testid="access-check-resource" placeholder="sales.order_upsert"></label>
          <label class="label">Review severity<select id="accessReviewSeverity" data-testid="access-review-severity"><option value="">All findings</option><option value="critical">Critical+</option><option value="high">High+</option><option value="medium">Medium+</option><option value="info">Info+</option></select></label>
        </div>
        <div class="grid-3">
          <label class="label">Key status<select id="accessKeyStatus" data-testid="access-key-status"><option value="">All</option><option value="active">Active</option><option value="expiring_soon">Expiring soon</option><option value="expired">Expired</option><option value="disabled">Disabled</option></select></label>
          <label class="label">Search keys<input id="accessKeySearch" data-testid="access-key-search" placeholder="id, user, note"></label>
          <label class="label">Limit<input id="accessKeyLimit" data-testid="access-key-limit" placeholder="200"></label>
        </div>
        <div id="accessCatalog" class="table-wrap" data-testid="access-catalog"></div>
        <label class="label">MACLAW_DATA_API_KEYS entry<textarea id="accessPolicyJson" data-testid="access-policy-json" spellcheck="false">{}</textarea></label>
        <div id="accessPolicyDiff" class="table-wrap" data-testid="access-policy-diff"></div>
        <div id="accessPolicyRisk" class="table-wrap" data-testid="access-policy-risk"></div>
        <div id="governanceEvidenceSummary" class="table-wrap" data-testid="governance-evidence-summary"><button class="small hide" data-testid="copy-evidence-summary">Copy summary</button><pre class="hide" data-testid="governance-evidence-summary-text"></pre></div>
        <div id="accessKeySecret" class="notice hide" data-testid="access-key-secret"></div>
        <section class="overview-card">
          <div class="row" style="justify-content: space-between;">
            <h3>Administrator accounts</h3>
            <button class="small" id="refreshAdminAccounts" data-testid="refresh-admin-accounts">Refresh admins</button>
          </div>
          <p>Create and review local administrator accounts. Disabling an administrator revokes that account's active sessions.</p>
          <div class="grid-3">
            <label class="label">Admin username<input id="adminAccountUsername" data-testid="admin-account-username" placeholder="ops-admin"></label>
            <label class="label">Display name<input id="adminAccountDisplayName" data-testid="admin-account-display-name" placeholder="Operations Admin"></label>
            <label class="label">Role<select id="adminAccountRole" data-testid="admin-account-role"><option value="data_admin">data_admin</option><option value="data_auditor">data_auditor</option><option value="data_user">data_user</option></select></label>
          </div>
          <div class="grid-3">
            <label class="label">Temporary password<input id="adminAccountPassword" data-testid="admin-account-password" type="password" autocomplete="new-password" placeholder="At least 8 characters"></label>
            <button id="createAdminAccount" data-testid="create-admin-account">Create admin</button>
            <button id="updateAdminAccount" data-testid="update-admin-account">Update admin</button>
          </div>
          <div id="adminAccounts" class="table-wrap" data-testid="admin-accounts"></div>
          <div class="row" style="justify-content: space-between;">
            <h3>Administrator sessions</h3>
            <button class="small" id="refreshAdminSessions" data-testid="refresh-admin-sessions">Refresh sessions</button>
          </div>
          <div id="adminSessions" class="table-wrap" data-testid="admin-sessions"></div>
        </section>
        <section class="overview-card">
          <div class="row" style="justify-content: space-between;">
            <h3>Agent handoff</h3>
            <button class="small" id="copyAgentHandoff" data-testid="copy-agent-handoff">Copy handoff</button>
          </div>
          <p>Share this with MaClaw or a scoped agent after the managed key is created. Secrets are only included immediately after create or rotate.</p>
          <textarea id="accessAgentHandoff" data-testid="access-agent-handoff" spellcheck="false" placeholder="Create or load a managed API key, then generate the handoff."></textarea>
          <div id="agentOnboardingChecklist" class="table-wrap" data-testid="agent-onboarding-checklist"></div>
          <div id="agentReadinessResult" class="table-wrap" data-testid="agent-readiness-result"></div>
          <div class="row" style="justify-content: space-between;">
            <h3>Onboarding packet</h3>
            <div class="row">
              <button class="small" id="copyAgentPacket" data-testid="copy-agent-packet">Copy packet</button>
              <button class="small" id="downloadAgentPacket" data-testid="download-agent-packet">Download packet</button>
            </div>
          </div>
          <textarea id="agentOnboardingPacket" data-testid="agent-onboarding-packet" spellcheck="false" placeholder="Generate a full onboarding packet after preparing the key."></textarea>
        </section>
        <div id="accessKeys" class="table-wrap" data-testid="access-keys"></div>
      </div>
      </div>
    </section>
  </main>

  <script>
    const $ = (id) => document.getElementById(id);
	const state = { datasets: [], selectedDataset: "", records: [], templates: [], businessActions: [], selectedBusinessAction: "", businessRules: [], eventContracts: [], connectors: [], selectedConnector: "", connectorSyncRuns: [], lastConnectorMappingSuggestion: null, businessViews: [], selectedBusinessView: "", dashboards: [], selectedDashboard: "", reports: [], selectedReport: "", qualityChecks: [], schemaProposals: [], relationships: [], accessCapabilities: null, accessPresets: [], accessKeys: [], adminAccounts: [], adminSessions: [], lastAccessKeySecret: "", loadedAccessPolicy: null };
    state.ready = null;
    state.stats = null;
    state.domains = [];
    state.domainsUnavailable = "";
    state.accessReview = null;
    state.accessReviewUnavailable = "";
    state.inboxSummary = null;
    state.inboxUnavailable = "";
    state.inboxItems = [];
    state.inboxNextBefore = "";
    state.inboxNextBeforeID = "";
    state.inboxHasMore = false;
    state.auditLogs = [];
    state.auditUnavailable = "";
    state.auditNextBefore = "";
    state.auditNextBeforeID = "";
    state.auditHasMore = false;
    state.dataEvents = [];
    state.dataEventNextBefore = "";
    state.dataEventNextBeforeID = "";
    state.dataEventHasMore = false;
    state.eventDeadLetters = [];
    state.eventDeadLetterNextBefore = "";
    state.eventDeadLetterNextBeforeID = "";
    state.eventDeadLetterHasMore = false;
    state.qualityRuns = [];
    state.qualityRunNextBefore = "";
    state.qualityRunNextBeforeID = "";
    state.qualityRunHasMore = false;
    state.importJobs = [];
    state.importJobNextBefore = "";
    state.importJobNextBeforeID = "";
    state.importJobHasMore = false;
    state.exportJobs = [];
    state.exportJobNextBefore = "";
    state.exportJobNextBeforeID = "";
    state.exportJobHasMore = false;
    state.approvals = [];
    state.approvalNextBefore = "";
    state.approvalNextBeforeID = "";
    state.approvalHasMore = false;
    state.operationPlans = [];
    state.operationPlanNextBefore = "";
    state.operationPlanNextBeforeID = "";
    state.operationPlanHasMore = false;
    state.schemaProposalNextBefore = "";
    state.schemaProposalNextBeforeID = "";
    state.schemaProposalHasMore = false;
    state.backups = [];
    state.backupNextBefore = "";
    state.backupNextBeforeID = "";
    state.backupHasMore = false;
    state.connectorSyncRunNextBefore = "";
    state.connectorSyncRunNextBeforeID = "";
    state.connectorSyncRunHasMore = false;
    state.accessKeyNextBefore = "";
    state.accessKeyNextBeforeID = "";
    state.accessKeyHasMore = false;
    state.accessPresetNextBeforeID = "";
    state.accessPresetHasMore = false;
    state.connectorHealth = [];
    state.connectorHealthUnavailable = "";
    state.statusHistory = [];
    state.governanceEvidence = null;
    state.relatedRecords = [];
    state.relatedNextBeforeID = "";
    state.relatedHasMore = false;
    state.templateNextBeforeID = "";
    state.templateHasMore = false;
    state.recordRevisions = [];
    state.recordRevisionNextBefore = "";
    state.recordRevisionNextBeforeID = "";
    state.recordRevisionHasMore = false;
    state.recordTimeline = [];
    state.recordTimelineNextBefore = "";
    state.recordTimelineNextBeforeID = "";
    state.recordTimelineHasMore = false;
    state.recordNextBefore = "";
    state.recordNextBeforeID = "";
    state.recordHasMore = false;
    state.businessViewRecords = [];
    state.businessViewNextBefore = "";
    state.businessViewNextBeforeID = "";
    state.businessViewHasMore = false;
    state.businessActionNextBeforeID = "";
    state.businessActionHasMore = false;
    state.businessRuleNextBeforeID = "";
    state.businessRuleHasMore = false;
    state.eventContractNextBeforeID = "";
    state.eventContractHasMore = false;
    state.businessListNextBeforeID = "";
    state.businessListHasMore = false;
    state.dashboardNextBeforeID = "";
    state.dashboardHasMore = false;
    state.reportNextBeforeID = "";
    state.reportHasMore = false;
    state.qualityCheckNextBeforeID = "";
    state.qualityCheckHasMore = false;
    state.domainNextBeforeID = "";
    state.domainHasMore = false;
    state.relationshipNextBeforeID = "";
    state.relationshipHasMore = false;
    state.datasetNextBefore = "";
    state.datasetNextBeforeID = "";
    state.datasetHasMore = false;
    state.connectorNextBefore = "";
    state.connectorNextBeforeID = "";
    state.connectorHasMore = false;
    const storageKey = "maclaw-data-console";
    const i18nSource = new WeakMap();
    let i18nApplying = false;
    let i18nMutationSuppressed = false;
    const moduleMeta = {
      overview: ["Operations", "Overview", "Start from common MIS workflows and service health."],
      records: ["Operations", "Records", "Search, export, and inspect structured business records."],
      inbox: ["Operations", "Inbox", "Review pending approvals, failed jobs, quality issues, and operational work."],
      domains: ["Operations", "Business Domains", "Discover business capabilities by domain or natural-language intent."],
      relationships: ["Operations", "Relationships", "Inspect controlled links between business datasets and records."],
      actions: ["Operations", "Business Actions", "Run business-level operations without editing raw tables."],
      rules: ["Operations", "Rules", "Evaluate business rules and preflight checks before operational writes."],
      connectors: ["Integration", "Connectors", "Manage external CRM, ERP, HR, finance, and inventory integrations."],
      views: ["Integration", "Views", "Query curated business views without exposing raw dataset internals."],
      dashboards: ["Integration", "Dashboards", "Run operational dashboard summaries for company and domain views."],
      reports: ["Integration", "Reports", "Run built-in reports and controlled aggregate analysis."],
      quality: ["Governance", "Quality", "Run data quality checks and inspect historical quality scans."],
      dataset: ["Governance", "Dataset", "Manage dataset metadata and administrative lifecycle controls."],
      fields: ["Governance", "Fields", "Maintain schema fields and controlled schema improvement proposals."],
      write: ["Governance", "Editor", "Validate, edit, import, approve, and recover individual business records."],
      backups: ["Governance", "Backups", "Create, download, and restore database recovery points."],
      events: ["Governance", "Events", "Inspect event ingestion, dead letters, and retry workflows."],
      audit: ["Governance", "Audit", "Search and export audit trails for compliance review."],
      ops: ["System", "Ops", "Check service statistics and run controlled database maintenance."],
      access: ["System", "Access", "Grant API keys by business capability, review risk, and plan remediation."],
      raw: ["System", "Response", "Inspect the latest raw API response for debugging and verification."]
    };
    const i18n = { zh: {
      "MaClawDataSrv MIS Admin Console": "MaClawDataSrv MIS 管理控制台",
      "Enterprise structured data operations": "企业结构化数据运营工作台",
      "Not connected": "未连接",
      "Endpoint": "服务地址",
      "Token": "令牌",
      "Tenant": "租户",
      "User": "用户",
      "Role": "角色",
      "Language": "语言",
      "Connect": "连接",
      "Datasets": "数据集",
      "Refresh": "刷新",
      "Create from Template": "从模板创建",
      "Template": "模板",
      "Dataset ID override": "数据集 ID 覆盖",
      "Bootstrap domains": "初始化领域",
      "Create template": "创建模板",
      "Load more templates": "加载更多模板",
      "Preview Bootstrap": "预览初始化",
      "Bootstrap MIS": "初始化 MIS",
      "Custom Dataset": "自定义数据集",
      "Domain": "领域",
      "Name": "名称",
      "Title": "标题",
      "Create custom": "创建自定义",
      "Operations": "运营",
      "Overview": "总览",
      "Records": "记录",
      "Inbox": "待办",
      "Business Domains": "业务领域",
      "Domains": "领域",
      "Relationships": "关系",
      "Business Actions": "业务动作",
      "Actions": "动作",
      "Rules": "规则",
      "Integration": "集成",
      "Connectors": "连接器",
      "Views": "视图",
      "Dashboards": "仪表盘",
      "Reports": "报表",
      "Governance": "治理",
      "Quality": "质量",
      "Dataset": "数据集",
      "Fields": "字段",
      "Editor": "编辑器",
      "Backups": "备份",
      "Events": "事件",
      "Audit": "审计",
      "System": "系统",
      "Ops": "运维",
      "Access": "访问控制",
      "Response": "响应",
      "Start from common MIS workflows and service health.": "从常用 MIS 流程和服务健康开始。",
      "Search, export, and inspect structured business records.": "搜索、导出并检查结构化业务记录。",
      "Review pending approvals, failed jobs, quality issues, and operational work.": "复核待审批、失败任务、质量问题和运营待办。",
      "Discover business capabilities by domain or natural-language intent.": "按业务领域或自然语言意图发现能力。",
      "Inspect controlled links between business datasets and records.": "检查业务数据集和记录之间的受控关联。",
      "Run business-level operations without editing raw tables.": "不直接编辑原始表，执行业务级操作。",
      "Evaluate business rules and preflight checks before operational writes.": "在写入前评估业务规则和预检项。",
      "Manage external CRM, ERP, HR, finance, and inventory integrations.": "管理外部 CRM、ERP、人事、财务和库存集成。",
      "Query curated business views without exposing raw dataset internals.": "查询业务视图，不暴露原始数据集内部结构。",
      "Run operational dashboard summaries for company and domain views.": "运行公司和领域视角的运营仪表盘摘要。",
      "Run built-in reports and controlled aggregate analysis.": "运行内置报表和受控聚合分析。",
      "Run data quality checks and inspect historical quality scans.": "运行数据质量检查并查看历史扫描。",
      "Manage dataset metadata and administrative lifecycle controls.": "管理数据集元数据和生命周期控制。",
      "Maintain schema fields and controlled schema improvement proposals.": "维护 schema 字段和受控 schema 改进提案。",
      "Validate, edit, import, approve, and recover individual business records.": "校验、编辑、导入、审批和恢复单条业务记录。",
      "Create, download, and restore database recovery points.": "创建、下载和恢复数据库恢复点。",
      "Inspect event ingestion, dead letters, and retry workflows.": "检查事件接入、死信和重试流程。",
      "Search and export audit trails for compliance review.": "搜索并导出审计轨迹用于合规复核。",
      "Check service statistics and run controlled database maintenance.": "检查服务统计并执行受控数据库维护。",
      "Grant API keys by business capability, review risk, and plan remediation.": "按业务能力授权 API key，复核风险并制定整改计划。",
      "Inspect the latest raw API response for debugging and verification.": "查看最新原始 API 响应用于调试和验证。",
      "Dataset: none selected": "数据集：未选择",
      "Service summary": "服务摘要",
      "Engine": "引擎",
      "Schema": "Schema",
      "Selected Dataset": "已选数据集",
      "Recent status": "最近状态",
      "Setup Checklist": "上线检查清单",
      "Prepare the service for reliable company MIS usage.": "为可靠承载企业 MIS 使用做好准备。",
      "Administrator accounts": "管理员账号",
      "Refresh admins": "刷新管理员",
      "Create and review local administrator accounts. Disabling an administrator revokes that account's active sessions.": "创建并复核本地管理员账号。禁用管理员会撤销该账号的活动会话。",
      "Admin username": "管理员用户名",
      "Display name": "显示名称",
      "Temporary password": "临时密码",
      "Create admin": "创建管理员",
      "Update admin": "更新管理员",
      "Administrator sessions": "管理员会话",
      "Refresh sessions": "刷新会话",
      "Username": "用户名",
      "Last login": "上次登录",
      "Session": "会话",
      "Current": "当前",
      "Created": "创建时间",
      "Expires": "过期时间",
      "Revoke": "撤销",
      "Load": "载入",
      "Disable": "禁用",
      "Enable": "启用",
      "Refresh overview": "刷新总览",
      "Operational Health": "运营健康",
      "Current service counters from the controlled stats API.": "来自受控统计 API 的当前服务指标。",
      "MIS Coverage": "MIS 覆盖",
      "Template and dataset coverage across common enterprise business domains.": "常见企业业务领域中的模板和数据集覆盖。",
      "Open domains": "打开领域",
      "Preview bootstrap": "预览初始化",
      "Business Domain Readiness": "业务领域就绪度",
      "Readiness of sales, finance, HR, legal, procurement, inventory, and asset domains.": "销售、财务、人事、法务、采购、库存和资产领域的就绪状态。",
      "Manage domains": "管理领域",
      "Refresh domains": "刷新领域",
      "Business Capabilities": "业务能力",
      "Business-first operations exposed to MaClaw agents and human operators.": "面向 MaClaw agent 和人工操作员的业务优先能力。",
      "Run actions": "执行业务动作",
      "Query views": "查询视图",
      "Run reports": "运行报表",
      "Intent Launcher": "意图启动器",
      "Resolve a business request into actions, views, reports, dashboards, and safe next steps.": "将业务请求解析为动作、视图、报表、仪表盘和安全下一步。",
      "Intent": "意图",
      "Limit": "上限",
      "Resolve": "解析",
      "Work Queue": "工作队列",
      "Pending operational work from the MIS inbox summary API.": "来自 MIS 待办摘要 API 的运营待办。",
      "Open inbox": "打开待办",
      "Refresh queue": "刷新队列",
      "Integration Health": "集成健康",
      "Connector status, recent failures, and open dead letters.": "连接器状态、近期失败和未处理死信。",
      "Open connectors": "打开连接器",
      "Refresh integration": "刷新集成",
      "Access Risk": "访问风险",
      "Managed API key risk summary from the authorization review API.": "授权复核 API 返回的托管 API key 风险摘要。",
      "Review access": "复核访问",
      "Refresh risk": "刷新风险",
      "Governance Readiness": "治理就绪度",
      "Minimum controls for using DataSrv with real company operations.": "DataSrv 承载真实公司运营所需的最低控制项。",
      "Create backup": "创建备份",
      "Check quality": "检查质量",
      "Open audit": "打开审计",
      "Recent Activity": "最近活动",
      "Latest audit trail entries from the controlled audit API.": "来自受控审计 API 的最新审计轨迹。",
      "Refresh activity": "刷新活动",
      "Daily Operations": "日常运营",
      "Search records, run business actions, and handle pending MIS work from one place.": "在一个工作台搜索记录、执行业务动作并处理 MIS 待办。",
      "Query records": "查询记录",
      "Analytics": "分析",
      "Use reports, dashboards, and curated views before asking for raw dataset access.": "优先使用报表、仪表盘和业务视图，再申请原始数据访问。",
      "Run dashboards": "运行仪表盘",
      "Keyword": "关键词",
      "Tag": "标签",
      "Filter JSON": "过滤 JSON",
      "Query": "查询",
      "Export CSV": "导出 CSV",
      "Export JSONL": "导出 JSONL",
      "Start CSV export job": "启动 CSV 导出任务",
      "Start JSONL export job": "启动 JSONL 导出任务",
      "Refresh export jobs": "刷新导出任务",
      "Clear": "清空",
      "MIS Inbox": "MIS 待办",
      "Refresh inbox": "刷新待办",
      "Type": "类型",
      "Status": "状态",
      "Include completed or OK items": "包含已完成或正常项",
      "Business Domains": "业务领域",
      "Resolve intent": "解析意图",
      "Dataset filter": "数据集过滤",
      "Current selected": "当前选择",
      "Use selected dataset": "使用已选数据集",
      "Clear filter": "清除过滤",
      "Action ID": "动作 ID",
      "Target Dataset": "目标数据集",
      "Description": "说明",
      "Input JSON": "输入 JSON",
      "Record ID": "记录 ID",
      "Idempotency Key": "幂等键",
      "Dry-run action": "试运行",
      "Execute action": "执行动作",
      "Check rules": "检查规则",
      "Event contract": "事件契约",
      "Format JSON": "格式化 JSON",
      "Event Contracts": "事件契约",
      "Domain filter": "领域过滤",
      "Refresh contracts": "刷新契约",
      "Business Rules": "业务规则",
      "Business Action": "业务动作",
      "Severity": "严重级别",
      "Use selected action": "使用已选动作",
      "Evaluate rules": "评估规则",
      "External Connectors": "外部连接器",
      "Health overview": "健康总览",
      "Connector ID": "连接器 ID",
      "Kind": "类型",
      "Auth type": "认证类型",
      "Token ref": "令牌引用",
      "Base URL": "基础 URL",
      "Subscribed actions": "订阅动作",
      "Config JSON": "配置 JSON",
      "Enabled": "启用",
      "Save connector": "保存连接器",
      "Test contract bindings": "测试契约绑定",
      "Validate config": "校验配置",
      "Readiness": "就绪度",
      "Check health": "检查健康",
      "Sync state": "同步状态",
      "Sync runs": "同步运行",
      "Suggested actions:": "建议动作：",
      "Service online:": "服务在线：",
      "Service online: ": "服务在线：",
      "Service is not ready": "服务未就绪",
      "Governance evidence summary downloaded": "治理证据摘要已下载",
      "Governance evidence summary refreshed": "治理证据摘要已刷新",
      "Dataset: ": "数据集：",
      "No datasets": "暂无数据集",
      "No backups": "暂无备份",
      "No maintenance result": "暂无维护结果",
      "No managed API keys.": "暂无托管 API key。",
      "No records": "暂无记录",
      "Load more": "加载更多",
      "Download": "下载",
      "Restore": "恢复",
      "Approve": "批准",
      "Reject": "拒绝",
      "Apply": "应用",
      "Cancel": "取消",
      "Disable": "禁用",
      "Rotate": "轮换",
      "Preview": "预览",
      "Edit": "编辑",
      "Delete": "删除",
      "Copy": "复制",
      "Run": "运行",
      "Create": "创建",
      "Update": "更新",
      "Save": "保存",
      "ID": "ID",
      "Task": "任务",
      "Message": "消息",
      "Duration ms": "耗时 ms",
      "Metric": "指标",
      "Value": "值",
      "Size": "大小",
      "Created": "创建时间",
      "Updated": "更新时间",
      "Actions": "操作",
      "Fields": "字段",
      "SHA256": "SHA256",
      "Record": "记录",
      "Records": "记录",
      "Source": "来源",
      "Target": "目标",
      "Severity": "严重级别",
      "Summary": "摘要",
      "Priority": "优先级",
      "Owner": "负责人",
      "Created by": "创建人",
      "Backup created": "备份已创建",
      "Backup downloaded": "备份已下载",
      "Backup restored": "备份已恢复",
      "Maintenance completed": "维护完成",
      "Maintenance found issues": "维护发现问题",
      "Operation plan applied": "操作计划已应用",
      "Operation plan canceled": "操作计划已取消",
      "Authorization recommendation cleared": "授权建议已清除",
      "Agent handoff generated": "Agent 交接信息已生成",
      "Select a dataset first": "请先选择数据集",
      "Dataset:": "数据集："
    } };
    const placeholderI18n = { zh: {
      "http://127.0.0.1:18180": "http://127.0.0.1:18180",
      "data_user": "data_user",
      "optional, e.g. sales.orders_2026": "可选，例如 sales.orders_2026",
      "optional, e.g. sales,finance": "可选，例如 sales,finance",
      "sales": "sales",
      "orders": "orders",
      "Sales orders": "销售订单",
      "customer, amount, name": "客户、金额、姓名",
      "q1": "q1",
      "{\"field\":\"amount\",\"op\":\"gte\",\"value\":1000}": "{\"field\":\"amount\",\"op\":\"gte\",\"value\":1000}",
      "approval, operation_plan, import_job, export_job, quality": "approval、operation_plan、import_job、export_job、quality",
      "pending, failed, issue": "pending、failed、issue",
      "e.g. low stock, expense reimbursement, procurement status": "例如：低库存、费用报销、采购状态",
      "optional, e.g. inventory": "可选，例如 inventory",
      "optional": "可选",
      "finance": "finance",
      "finance.expense_submit": "finance.expense_submit",
      "high, critical": "high、critical",
      "sales.crm": "sales.crm",
      "Sales CRM": "销售 CRM",
      "crm, erp, hris": "crm、erp、hris",
      "bearer, api_key": "bearer、api_key",
      "MIS_CRM_TOKEN": "MIS_CRM_TOKEN",
      "https://crm.example.local": "https://crm.example.local",
      "external business id": "外部业务 ID",
      "source:object:id:version": "source:object:id:version",
      "expense, low stock, sales order status": "报销、低库存、销售订单状态"
    } };
    function currentLanguage() {
      return ($("language") && $("language").value) || "en";
    }

    function translateText(text) {
      const lang = currentLanguage();
      if (lang === "en") return text;
      const prefixed = [
        "Suggested actions: ",
        "Service online: ",
        "Dataset: ",
        "HTTP "
      ];
      for (const prefix of prefixed) {
        if (text.startsWith(prefix)) return translateText(prefix.trimEnd()) + text.slice(prefix.length);
      }
      if (text.startsWith("Operation plan ")) return text.replace("Operation plan ", "操作计划 ").replace("approved", "已批准").replace("rejected", "已拒绝");
      const setupI18n = {
        "Administrator access": "管理员访问",
        "On first launch, create the local administrator account. After initialization, sign in to receive a temporary bearer token.": "首次启动时创建本地管理员账号。初始化后登录即可获得临时 Bearer 令牌。",
        "Checking setup status": "正在检查初始化状态",
        "First-time setup": "首次初始化",
        "Admin username": "管理员用户名",
        "Admin display name": "管理员显示名",
        "Admin password": "管理员密码",
        "Initialize administrator": "初始化管理员",
        "Admin sign in": "管理员登录",
        "Sign in": "登录",
        "Password policy loading": "正在读取密码策略",
        "Setup required: create the first administrator account": "需要初始化：请创建第一个管理员账号",
        "Administrator initialized. Token saved for this console.": "管理员已初始化，令牌已保存到当前控制台。",
        "Administrator login succeeded. Token saved.": "管理员登录成功，令牌已保存。",
        "Administrator initialized": "管理员已初始化"
      };
      if (setupI18n[text]) return setupI18n[text];
      return (i18n[lang] && i18n[lang][text]) || text;
    }

    function setText(id, text) {
      const el = $(id);
      if (!el) return;
      el.textContent = translateText(text);
    }

    function activeModuleName() {
      const active = document.querySelector(".tab.active");
      return (active && active.dataset.tab) || "records";
    }

    function translateStatus(text) {
      return translateText(text);
    }
    function applyI18n(root) {
      if (i18nApplying) return;
      i18nApplying = true;
      i18nMutationSuppressed = true;
      try {
        document.documentElement.lang = currentLanguage() === "zh" ? "zh-CN" : "en";
        document.title = translateText("MaClawDataSrv MIS Admin Console");
        const walker = document.createTreeWalker(root || document.body, NodeFilter.SHOW_TEXT, {
          acceptNode(node) {
            const parent = node.parentElement;
            if (!parent) return NodeFilter.FILTER_REJECT;
            if (parent.id === "serviceStatus") return NodeFilter.FILTER_REJECT;
            const tag = parent.tagName;
            if (["SCRIPT", "STYLE", "TEXTAREA", "PRE", "CODE", "INPUT", "SELECT"].includes(tag)) return NodeFilter.FILTER_REJECT;
            if (!node.nodeValue || !node.nodeValue.trim()) return NodeFilter.FILTER_REJECT;
            return NodeFilter.FILTER_ACCEPT;
          }
        });
        const nodes = [];
        while (walker.nextNode()) nodes.push(walker.currentNode);
        nodes.forEach(node => {
          if (!i18nSource.has(node)) i18nSource.set(node, node.nodeValue);
          const source = i18nSource.get(node);
          const leading = source.match(/^\s*/)[0];
          const trailing = source.match(/\s*$/)[0];
          const core = source.trim();
          node.nodeValue = leading + translateText(core) + trailing;
        });
        document.querySelectorAll("[placeholder]").forEach(el => {
          if (!el.dataset.i18nPlaceholderSource) el.dataset.i18nPlaceholderSource = el.getAttribute("placeholder") || "";
          const source = el.dataset.i18nPlaceholderSource;
          const lang = currentLanguage();
          el.setAttribute("placeholder", (placeholderI18n[lang] && placeholderI18n[lang][source]) || source);
        });
          renderStatus();
          updateModuleHeader(activeModuleName());
          renderStatusHistory();
        } finally {
          i18nApplying = false;
          setTimeout(() => { i18nMutationSuppressed = false; }, 0);
        }
    }

    function loadSettings() {
      const saved = JSON.parse(localStorage.getItem(storageKey) || "{}");
      $("endpoint").value = saved.endpoint || location.origin;
      $("token").value = saved.token || "";
      $("tenant").value = saved.tenant || "default";
      $("user").value = saved.user || "web-console";
      $("role").value = saved.role || "data_user";
      $("language").value = saved.language || "en";
      return saved;
    }

    function saveSettings() {
      localStorage.setItem(storageKey, JSON.stringify({
        endpoint: $("endpoint").value.trim() || location.origin,
        token: $("token").value.trim(),
        tenant: $("tenant").value.trim() || "default",
        user: $("user").value.trim() || "web-console",
        role: $("role").value.trim() || "data_user",
        language: $("language").value || "en",
        active_tab: activeModuleName()
      }));
    }

    function endpoint(path) {
      const base = ($("endpoint").value.trim() || location.origin).replace(/\/$/, "");
      return base + path;
    }

    async function api(path, options) {
      const init = options || {};
      init.headers = Object.assign({}, init.headers || {}, {
        "Authorization": "Bearer " + $("token").value.trim(),
        "X-MaClaw-Tenant-ID": $("tenant").value.trim() || "default",
        "X-MaClaw-User-ID": $("user").value.trim() || "web-console",
        "X-MaClaw-Role": $("role").value.trim() || "data_user"
      });
      if (init.body && !init.headers["Content-Type"]) init.headers["Content-Type"] = "application/json";
      const resp = await fetch(endpoint(path), init);
      const text = await resp.text();
      let data = text;
      try { data = text ? JSON.parse(text) : {}; } catch (e) {}
      setRaw(data);
      if (!resp.ok) throw new Error("HTTP " + resp.status + ": " + (typeof data === "string" ? data : JSON.stringify(data)));
      return data;
    }

    async function apiText(path, options) {
      const init = options || {};
      init.headers = Object.assign({}, init.headers || {}, {
        "Authorization": "Bearer " + $("token").value.trim(),
        "X-MaClaw-Tenant-ID": $("tenant").value.trim() || "default",
        "X-MaClaw-User-ID": $("user").value.trim() || "web-console",
        "X-MaClaw-Role": $("role").value.trim() || "data_user"
      });
      if (init.body && !init.headers["Content-Type"]) init.headers["Content-Type"] = "application/json";
      const resp = await fetch(endpoint(path), init);
      const text = await resp.text();
      $("rawOutput").textContent = text;
      if (!resp.ok) throw new Error("HTTP " + resp.status + ": " + text);
      return text;
    }

    async function apiBlob(path, options) {
      const init = options || {};
      init.headers = Object.assign({}, init.headers || {}, {
        "Authorization": "Bearer " + $("token").value.trim(),
        "X-MaClaw-Tenant-ID": $("tenant").value.trim() || "default",
        "X-MaClaw-User-ID": $("user").value.trim() || "web-console",
        "X-MaClaw-Role": $("role").value.trim() || "data_user"
      });
      if (init.body && !init.headers["Content-Type"]) init.headers["Content-Type"] = "application/json";
      const resp = await fetch(endpoint(path), init);
      if (!resp.ok) {
        const text = await resp.text();
        throw new Error("HTTP " + resp.status + ": " + text);
      }
      const blob = await resp.blob();
      setRaw({
        content_type: resp.headers.get("Content-Type") || "",
        content_disposition: resp.headers.get("Content-Disposition") || "",
        sha256: resp.headers.get("X-MaClaw-Backup-SHA256") || "",
        size_bytes: blob.size
      });
      return { blob, headers: resp.headers };
    }

    async function publicApi(path) {
      const resp = await fetch(endpoint(path));
      const data = await resp.json();
      setRaw(data);
      if (!resp.ok) throw new Error("HTTP " + resp.status);
      return data;
    }

    async function publicApiJSON(path, body) {
      const resp = await fetch(endpoint(path), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body || {})
      });
      const data = await resp.json();
      setRaw(data);
      if (!resp.ok) throw new Error("HTTP " + resp.status + ": " + JSON.stringify(data));
      return data;
    }

    function updateSetupPanel(status) {
      const panel = $("adminSetupPanel");
      if (!panel) return;
      const initialized = !!(status && status.initialized);
      panel.classList.toggle("ready", initialized);
      $("adminInitBox").classList.toggle("hide", initialized);
      $("adminLoginBox").classList.toggle("hide", !initialized);
      setText("setupStatusText", initialized ? "Administrator initialized" : "Setup required: create the first administrator account");
      renderAdminPasswordPolicy(status && status.password_policy);
      if ($("tenant").value.trim() === "" && status && status.tenant_id) $("tenant").value = status.tenant_id;
    }

    function renderAdminPasswordPolicy(policy) {
      const root = $("adminPasswordPolicy");
      if (!root) return;
      if (!policy) {
        root.textContent = translateText("Password policy loading");
        return;
      }
      const minLength = policy.min_length || 8;
      const lockout = policy.lockout_enabled && policy.login_max_failures
        ? " Lockout after " + policy.login_max_failures + " failures for " + (policy.login_lockout_minutes || 0) + " minutes."
        : " Login lockout is disabled.";
      const reset = policy.offline_reset_available ? " Offline reset-password command is available." : "";
      root.textContent = translateText("Password policy: minimum " + minLength + " characters.") + translateText(lockout) + translateText(reset);
    }

    async function refreshSetupStatus() {
      try {
        const status = await publicApi("/api/v1/setup/status");
        updateSetupPanel(status);
        return status;
      } catch (err) {
        notifyError(err);
        return null;
      }
    }

    async function initializeAdmin() {
      try {
        const result = await publicApiJSON("/api/v1/setup/admin", {
          tenant_id: $("tenant").value.trim() || "default",
          username: $("initUsername").value.trim(),
          display_name: $("initDisplayName").value.trim(),
          password: $("initPassword").value
        });
        $("token").value = result.token || "";
        $("user").value = result.username || $("initUsername").value.trim() || "admin";
        $("role").value = result.role || "data_admin";
        $("loginUsername").value = result.username || "";
        $("initPassword").value = "";
        saveSettings();
        updateSetupPanel({ initialized: true, tenant_id: result.tenant_id || "default" });
        setStatus("Administrator initialized. Token saved for this console.", "ok");
        await checkConnection();
      } catch (err) { notifyError(err); }
    }

    async function loginAdmin() {
      try {
        const result = await publicApiJSON("/api/v1/login", {
          tenant_id: $("tenant").value.trim() || "default",
          username: $("loginUsername").value.trim(),
          password: $("loginPassword").value
        });
        $("token").value = result.token || "";
        $("user").value = result.username || $("loginUsername").value.trim() || "admin";
        $("role").value = result.role || "data_admin";
        $("loginPassword").value = "";
        saveSettings();
        setStatus("Administrator login succeeded. Token saved.", "ok");
        await checkConnection();
      } catch (err) { notifyError(err); }
    }

    function renderStatus() {
      const el = $("serviceStatus");
      const source = el.dataset.statusSource || "Not connected";
      const text = translateStatus(source);
      if (el.firstChild && el.firstChild.nodeType === Node.TEXT_NODE) {
        if (el.firstChild.nodeValue !== text) el.firstChild.nodeValue = text;
      } else {
        el.textContent = text;
      }
    }

    function renderStatusHistory() {
      const root = $("statusHistory");
      if (!root) return;
      if (!state.statusHistory.length) {
        root.classList.add("hide");
        root.innerHTML = "";
        return;
      }
      root.classList.remove("hide");
      root.innerHTML = "";
      state.statusHistory.slice(0, 3).forEach(item => {
        const row = document.createElement("div");
        row.className = "status-history-row";
        row.innerHTML = "<div></div><div></div><div></div>";
        row.children[0].textContent = item.time;
        row.children[1].className = "status-history-kind" + (item.kind ? " " + item.kind : "");
        row.children[1].textContent = translateText(item.kind || "info");
        row.children[2].className = "status-history-text";
        row.children[2].textContent = translateStatus(item.text);
        row.children[2].title = item.text;
        root.appendChild(row);
      });
    }

    function updateModuleHeader(name) {
      const meta = moduleMeta[name] || moduleMeta.records;
      setText("moduleKicker", meta[0]);
      setText("moduleTitle", meta[1]);
      setText("moduleDesc", meta[2]);
      const context = state.selectedDataset ? "Dataset: " + state.selectedDataset : "Dataset: none selected";
      const contextEl = $("moduleContext");
      if (contextEl) {
        contextEl.dataset.contextSource = context;
        contextEl.textContent = currentLanguage() === "zh" && state.selectedDataset ? translateText("Dataset: ") + state.selectedDataset : translateText(context);
        contextEl.title = context;
      }
      updateAdminSummary();
    }

    function updateAdminSummary() {
      const ready = state.ready || {};
      const stats = state.stats || {};
      setText("summaryEngine", ready.engine || "-");
      setText("summarySchema", ready.schema_version === undefined ? "-" : String(ready.schema_version));
      setText("summaryDatasets", String(stats.dataset_count === undefined ? (state.datasets || []).length : stats.dataset_count));
      setText("summaryRecords", String(stats.record_count || 0));
      setText("summaryBackups", String(stats.backup_count || 0));
      setText("summarySelectedDataset", state.selectedDataset || "-");
      renderSetupChecklist();
      renderOverviewHealth();
      renderOverviewCoverage();
      renderOverviewDomainReadiness();
      renderOverviewCapabilities();
      renderOverviewAccessRisk();
      renderOverviewWorkQueue();
      renderOverviewIntegrationHealth();
      renderOverviewReadiness();
      renderOverviewActivity();
      renderAccessWorkspaceSummary();
    }

    function renderOverviewHealth() {
      const root = $("overviewHealth");
      if (!root) return;
      const stats = state.stats || {};
      const rows = [
        ["Records", stats.record_count || 0],
        ["Fields", stats.field_count || 0],
        ["Quality runs", stats.quality_run_count || 0],
        ["Audit logs", stats.audit_log_count || 0],
        ["Database bytes", stats.database_bytes || 0]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "health-item";
        item.innerHTML = "<div class='health-label'></div><div class='health-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        item.querySelector(".health-value").textContent = String(row[1]);
        root.appendChild(item);
      });
    }

    function renderOverviewCoverage() {
      const root = $("overviewCoverage");
      if (!root) return;
      const domains = new Set();
      (state.templates || []).forEach(item => { if (item.domain) domains.add(item.domain); });
      (state.datasets || []).forEach(item => { if (item.domain) domains.add(item.domain); });
      const initialized = new Set();
      (state.datasets || []).forEach(item => { if (item.domain) initialized.add(item.domain); });
      const rows = [
        ["Domains", domains.size],
        ["Initialized", initialized.size],
        ["Missing", Math.max(domains.size - initialized.size, 0)],
        ["Templates", (state.templates || []).length],
        ["Datasets", (state.datasets || []).length]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "coverage-item";
        item.innerHTML = "<div class='health-label'></div><div class='coverage-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        const value = item.querySelector(".coverage-value");
        value.textContent = String(row[1]);
        if (row[0] === "Missing" && Number(row[1]) > 0) value.classList.add("high");
        root.appendChild(item);
      });
    }

    function renderOverviewDomainReadiness() {
      const root = $("overviewDomainReadiness");
      if (!root) return;
      const domains = state.domains || [];
      root.innerHTML = "";
      if (state.domainsUnavailable) {
        const item = document.createElement("div");
        item.className = "domain-card warn";
        item.innerHTML = "<div class='domain-title'></div><div class='domain-status'></div>";
        item.querySelector(".domain-title").textContent = translateText("Unavailable");
        item.querySelector(".domain-status").textContent = translateText(state.domainsUnavailable);
        root.appendChild(item);
        return;
      }
      if (!domains.length) {
        const item = document.createElement("div");
        item.className = "empty";
        item.textContent = translateText("No business domains");
        root.appendChild(item);
        return;
      }
      domains.slice(0, 8).forEach(domain => {
        const missing = (domain.missing_templates || []).length;
        const capabilities = (domain.business_actions || []).length + (domain.business_views || []).length + (domain.reports || []).length + (domain.dashboards || []).length;
        const item = document.createElement("button");
        item.className = "domain-card" + (missing > 0 || !domain.initialized ? " warn" : "");
        item.innerHTML = "<div class='domain-title'></div><div class='domain-status'></div><div class='dataset-meta'></div>";
        item.querySelector(".domain-title").textContent = (domain.title || domain.domain || "") + (domain.initialized ? "" : " *");
        item.querySelector(".domain-status").textContent = translateText("Datasets") + ": " + ((domain.datasets || []).length) + " / " + translateText("Missing templates") + ": " + missing;
        item.querySelector(".dataset-meta").textContent = translateText("Use cases") + ": " + ((domain.use_cases || []).length) + " / " + translateText("Capabilities") + ": " + capabilities;
        item.onclick = () => {
          $("intentDomain").value = domain.domain || "";
          switchTab("domains");
        };
        root.appendChild(item);
      });
    }

    function renderOverviewCapabilities() {
      const root = $("overviewCapabilities");
      if (!root) return;
      const access = (state.accessCapabilities && state.accessCapabilities.access) || {};
      const rows = [
        ["Business actions", (state.businessActions || []).length, "actions"],
        ["Business views", (state.businessViews || []).length, "views"],
        ["Reports", (state.reports || []).length, "reports"],
        ["Dashboards", (state.dashboards || []).length, "dashboards"],
        ["Access mode", access.scope_mode || "-", "access"],
        ["Raw data", access.raw_dataset_allowed ? "Allowed" : "Blocked", "access"],
        ["Admin scope", access.admin_allowed ? "Allowed" : "Blocked", "access"]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("button");
        item.className = "capability-item";
        item.innerHTML = "<div class='health-label'></div><div class='capability-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        item.querySelector(".capability-value").textContent = String(row[1]);
        item.onclick = () => switchTab(row[2]);
        root.appendChild(item);
      });
    }

    async function resolveOverviewIntent() {
      try {
        const query = $("overviewIntentQuery").value.trim();
        if (!query) throw new Error("Intent is required");
        const body = {
          query,
          domain: $("overviewIntentDomain").value.trim(),
          limit: Number($("overviewIntentLimit").value || 3)
        };
        const result = await api("/api/v1/data/intent/resolve", { method: "POST", body: JSON.stringify(body) });
        renderOverviewIntentMatches((result && result.matches) || []);
        setRaw(result || {});
        setStatus("Intent resolved: " + (((result && result.matches) || []).length) + " matches", "ok");
      } catch (err) {
        notifyError(err);
      }
    }

    function renderOverviewIntentMatches(items) {
      const root = $("overviewIntentResults");
      if (!root) return;
      if (!items.length) {
        root.innerHTML = '<div class="empty">No intent matches</div>';
        applyI18n(root);
        return;
      }
      root.innerHTML = "";
      items.slice(0, 3).forEach(item => {
        const useCase = item.use_case || {};
        const writeStep = (item.next_steps || []).find(step => step.action === "execute_business_action");
        const dashboardStep = (item.next_steps || []).find(step => step.action === "run_dashboard");
        const viewStep = (item.next_steps || []).find(step => step.action === "query_business_view");
        const reportStep = (item.next_steps || []).find(step => step.action === "run_report");
        const bootstrapStep = (item.next_steps || []).find(step => step.action === "bootstrap_templates");
        const card = document.createElement("div");
        card.className = "intent-card";
        card.innerHTML = "<div><strong></strong><span></span></div><div class='row'></div>";
        card.querySelector("strong").textContent = (useCase.title || useCase.id || item.domain || "") + " / " + (item.domain || "");
        card.querySelector("span").textContent = [
          useCase.preferred_action || "",
          useCase.preferred_view || "",
          useCase.preferred_report || "",
          useCase.preferred_dashboard || ""
        ].filter(Boolean).join(" | ");
        const actions = card.querySelector(".row");
        if (writeStep) {
          const load = document.createElement("button");
          load.className = "small primary";
          load.textContent = translateText("Load action");
          load.onclick = () => useIntentWriteStep(writeStep);
          actions.appendChild(load);
        }
        if (dashboardStep) actions.appendChild(intentStepButton("Run dashboard", "small", () => useIntentReadStep(dashboardStep)));
        if (viewStep) actions.appendChild(intentStepButton("Query view", "small", () => useIntentReadStep(viewStep)));
        if (reportStep) actions.appendChild(intentStepButton("Run report", "small", () => useIntentReadStep(reportStep)));
        if (bootstrapStep) actions.appendChild(intentStepButton("Preview bootstrap", "small", () => useIntentReadStep(bootstrapStep)));
        const open = document.createElement("button");
        open.className = "small";
        open.textContent = translateText("Open domain");
        open.onclick = () => {
          $("intentDomain").value = item.domain || "";
          $("intentQuery").value = $("overviewIntentQuery").value.trim();
          switchTab("domains");
        };
        actions.appendChild(open);
        root.appendChild(card);
      });
    }

    function intentStepButton(label, className, onClick) {
      const button = document.createElement("button");
      button.className = className;
      button.textContent = translateText(label);
      button.onclick = onClick;
      return button;
    }

    function renderOverviewAccessRisk() {
      const root = $("overviewAccessRisk");
      if (!root) return;
      const review = state.accessReview || {};
      const severity = review.by_severity || {};
      const rows = state.accessReviewUnavailable ? [
        ["Unavailable", state.accessReviewUnavailable]
      ] : [
        ["Total keys", review.total || 0],
        ["Findings", review.filtered || 0],
        ["Critical", severity.critical || 0],
        ["High", severity.high || 0],
        ["Medium", severity.medium || 0]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "risk-item";
        item.innerHTML = "<div class='health-label'></div><div class='risk-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        const value = item.querySelector(".risk-value");
        value.textContent = String(row[1]);
        if ((row[0] === "Critical" || row[0] === "High") && Number(row[1]) > 0) value.classList.add("high");
        root.appendChild(item);
      });
    }

    function renderOverviewWorkQueue() {
      const root = $("overviewWorkQueue");
      if (!root) return;
      const summary = state.inboxSummary || {};
      const rows = state.inboxUnavailable ? [
        ["Unavailable", state.inboxUnavailable]
      ] : [
        ["Total", summary.total || 0],
        ["Critical", summary.critical || 0],
        ["High", summary.high || 0],
        ["Overdue", summary.overdue || 0],
        ["Updated", summary.generated_at ? new Date(summary.generated_at).toLocaleTimeString() : "-"]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "queue-item";
        item.innerHTML = "<div class='health-label'></div><div class='queue-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        const value = item.querySelector(".queue-value");
        value.textContent = String(row[1]);
        if ((row[0] === "Critical" || row[0] === "High" || row[0] === "Overdue") && Number(row[1]) > 0) value.classList.add("high");
        root.appendChild(item);
      });
    }

    function renderOverviewIntegrationHealth() {
      const root = $("overviewIntegrationHealth");
      if (!root) return;
      const items = state.connectorHealth || [];
      const degraded = items.filter(item => {
        const status = String(item.status || "").toLowerCase();
        return status === "degraded" || status === "needs_attention";
      }).length;
      const disabled = items.filter(item => !item.enabled).length;
      const failures = items.reduce((total, item) => total + Number(item.recent_failures || 0), 0);
      const deadLetters = items.reduce((total, item) => total + Number(item.open_dead_letters || 0), 0);
      const recentEvents = items.reduce((total, item) => total + Number(item.recent_events || 0), 0);
      const rows = state.connectorHealthUnavailable ? [
        ["Unavailable", state.connectorHealthUnavailable]
      ] : [
        ["Connectors", items.length],
        ["Degraded", degraded],
        ["Disabled", disabled],
        ["Failures", failures],
        ["Dead letters", deadLetters],
        ["Recent events", recentEvents]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "integration-item";
        item.innerHTML = "<div class='health-label'></div><div class='integration-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        const value = item.querySelector(".integration-value");
        value.textContent = String(row[1]);
        if ((row[0] === "Degraded" || row[0] === "Failures" || row[0] === "Dead letters") && Number(row[1]) > 0) value.classList.add("high");
        root.appendChild(item);
      });
    }

    function renderOverviewReadiness() {
      const root = $("overviewReadiness");
      if (!root) return;
      const recommendationRoot = $("overviewRecommendations");
      const stats = state.stats || {};
      const review = state.accessReview || {};
      const inbox = state.inboxSummary || {};
      const rows = [
        ["Recovery", (stats.backup_count || 0) > 0, (stats.backup_count || 0) + " backups"],
        ["Scoped keys", !state.accessReviewUnavailable && (review.total || 0) > 0, state.accessReviewUnavailable || ((review.total || 0) + " keys")],
        ["Audit trail", (stats.audit_log_count || 0) > 0, (stats.audit_log_count || 0) + " logs"],
        ["Quality", (stats.quality_run_count || 0) > 0, (stats.quality_run_count || 0) + " runs"],
        ["Open work", !state.inboxUnavailable && (inbox.critical || 0) === 0 && (inbox.high || 0) === 0 && (inbox.overdue || 0) === 0, state.inboxUnavailable || ((inbox.total || 0) + " items")]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "readiness-item";
        item.innerHTML = "<div class='health-label'></div><div class='readiness-value'></div><div class='dataset-meta'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        const value = item.querySelector(".readiness-value");
        value.textContent = translateText(row[1] ? "Ready" : "Needed");
        value.classList.add(row[1] ? "ok" : "warn");
        item.querySelector(".dataset-meta").textContent = translateText(String(row[2]));
        root.appendChild(item);
      });
      if (!recommendationRoot) return;
      const recommendations = [];
      if ((stats.backup_count || 0) === 0) recommendations.push(["Recovery", "Create a recovery backup before imports or schema changes.", "backups"]);
      if (state.accessReviewUnavailable || (review.total || 0) === 0) recommendations.push(["Scoped keys", "Create scoped API keys for agents and employees.", "access"]);
      if ((stats.quality_run_count || 0) === 0) recommendations.push(["Quality", "Run a quality check on the active business datasets.", "quality"]);
      if (!state.inboxUnavailable && ((inbox.critical || 0) > 0 || (inbox.high || 0) > 0 || (inbox.overdue || 0) > 0)) recommendations.push(["Open work", "Review critical or high-priority MIS inbox items.", "inbox"]);
      if (!recommendations.length) recommendations.push(["Next actions", "Governance controls look ready for normal operations.", "ops"]);
      recommendationRoot.innerHTML = "";
      recommendations.slice(0, 3).forEach(row => {
        const item = document.createElement("button");
        item.className = "recommendation-item";
        item.innerHTML = "<strong></strong><span></span>";
        item.querySelector("strong").textContent = translateText(row[0]);
        item.querySelector("span").textContent = translateText(row[1]);
        item.onclick = () => switchTab(row[2]);
        recommendationRoot.appendChild(item);
      });
    }

    function renderOverviewActivity() {
      const root = $("overviewActivity");
      if (!root) return;
      if (state.auditUnavailable) {
        root.innerHTML = "";
        const row = document.createElement("div");
        row.className = "activity-row";
        row.innerHTML = "<div></div><div></div><div></div>";
        row.children[0].textContent = translateText("Unavailable");
        row.children[2].textContent = translateText(state.auditUnavailable);
        root.appendChild(row);
        return;
      }
      const items = state.auditLogs || [];
      if (!items.length) {
        root.innerHTML = '<div class="empty">No recent audit activity</div>';
        applyI18n(root);
        return;
      }
      root.innerHTML = "";
      items.slice(0, 5).forEach(item => {
        const row = document.createElement("div");
        row.className = "activity-row";
        row.innerHTML = "<div></div><div></div><div></div>";
        row.children[0].textContent = item.created_at ? new Date(item.created_at).toLocaleString() : "";
        row.children[1].className = "activity-action";
        row.children[1].textContent = item.action || "";
        row.children[2].className = "activity-summary";
        row.children[2].textContent = [item.user_id || "", item.dataset_id || "", item.summary || ""].filter(Boolean).join(" / ");
        row.children[2].title = row.children[2].textContent;
        root.appendChild(row);
      });
    }

    function renderSetupChecklist() {
      const root = $("setupChecklist");
      if (!root) return;
      const items = [
        {
          done: !!state.ready,
          label: "Service connected",
          desc: "Ready endpoint returns current engine and schema version.",
          target: "overview"
        },
        {
          done: (state.templates || []).length > 0,
          label: "Templates loaded",
          desc: "Business templates are available for sales, finance, HR, and operations.",
          target: "overview"
        },
        {
          done: (state.datasets || []).length > 0,
          label: "Datasets created",
          desc: "Create datasets from templates or bootstrap common MIS domains.",
          target: "dataset"
        },
        {
          done: (state.accessKeys || []).length > 0,
          label: "Admin access configured",
          desc: "Create scoped API keys before giving agents or employees access.",
          target: "access"
        },
        {
          done: ((state.stats || {}).backup_count || 0) > 0,
          label: "Recovery path available",
          desc: "Create a backup before imports, schema changes, or data cleanup.",
          target: "backups"
        }
      ];
      root.innerHTML = "";
      items.forEach(item => {
        const row = document.createElement("div");
        row.className = "checklist-item" + (item.done ? " done" : "");
        row.innerHTML = "<div class='check-icon'></div><div class='checklist-main'><div class='checklist-label'></div><div class='checklist-desc'></div></div><button class='small'></button>";
        row.querySelector(".check-icon").textContent = item.done ? "OK" : "!";
        row.querySelector(".checklist-label").textContent = translateText(item.label);
        row.querySelector(".checklist-desc").textContent = translateText(item.desc);
        const button = row.querySelector("button");
        button.textContent = translateText("Open");
        button.onclick = () => switchTab(item.target);
        root.appendChild(row);
      });
    }

    function setStatus(text, kind) {
      const el = $("serviceStatus");
      el.dataset.statusSource = text;
      el.dataset.statusKind = kind || "";
      renderStatus();
      el.className = "status" + (kind ? " " + kind : "");
      state.statusHistory.unshift({ text, kind: kind || "info", time: new Date().toLocaleTimeString() });
      state.statusHistory = state.statusHistory.slice(0, 12);
      renderStatusHistory();
    }

    function setRaw(value) {
      $("rawOutput").textContent = JSON.stringify(value, null, 2);
    }

    function requireDataset() {
      if (!state.selectedDataset) throw new Error("Select a dataset first");
      return state.selectedDataset;
    }

    function parseJSONField(id, fallback) {
      const raw = $(id).value.trim();
      if (!raw) return fallback;
      return JSON.parse(raw);
    }

    function mergePlainObject(target, patch) {
      const base = target && typeof target === "object" && !Array.isArray(target) ? target : {};
      const update = patch && typeof patch === "object" && !Array.isArray(patch) ? patch : {};
      Object.keys(update).forEach((key) => {
        if (update[key] && typeof update[key] === "object" && !Array.isArray(update[key])) {
          base[key] = mergePlainObject(base[key], update[key]);
        } else {
          base[key] = update[key];
        }
      });
      return base;
    }

    function notifyError(err) {
      setStatus(err.message || String(err), "err");
    }

    async function checkConnection() {
      saveSettings();
      try {
        const ready = await publicApi("/readyz");
        state.ready = ready || {};
        updateAdminSummary();
        setStatus("Connected: " + ready.engine + " schema " + ready.schema_version, "ok");
        await loadOverviewStats(false);
        await loadOverviewCapabilitiesData(false);
        await loadOverviewDomains(false);
        await loadOverviewAccessRisk(false);
        await loadOverviewWorkQueue(false);
        await loadOverviewIntegrationHealth(false);
        await loadOverviewActivity(false);
        await loadTemplates();
        await loadBusinessActions();
        await loadConnectors();
        await loadBusinessViews();
        await loadQualityChecks();
        await loadDatasets();
      } catch (err) { notifyError(err); }
    }

    async function loadTemplates(loadMore = false) {
      try {
        const params = new URLSearchParams({ limit: "50" });
        loadMore = preparePageParams("templatePageKey", params, loadMore === true);
        if (loadMore && state.templateNextBeforeID) params.set("before_id", state.templateNextBeforeID);
        const data = await api("/api/v1/data/templates?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.templates = loadMore ? (state.templates || []).concat(items) : items;
        state.templateNextBeforeID = (data && data.next_before_id) || "";
        state.templateHasMore = !!(data && data.has_more && data.next_before_id);
        renderTemplates();
        updateAdminSummary();
      } catch (err) { notifyError(err); }
    }

    async function loadBusinessActions(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("businessActionPageKey", params, loadMore === true);
        if (loadMore && state.businessActionNextBeforeID) params.set("before_id", state.businessActionNextBeforeID);
        const data = await api("/api/v1/data/business-actions" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.businessActions = loadMore ? (state.businessActions || []).concat(items) : items;
        state.businessActionNextBeforeID = (data && data.next_before_id) || "";
        state.businessActionHasMore = !!(data && data.has_more && data.next_before_id);
        renderBusinessActions();
        updateAdminSummary();
      } catch (err) { notifyError(err); }
    }

    function renderBusinessActions() {
      const root = $("actionList");
      if (!root) return;
      root.innerHTML = "";
      if (!state.businessActions.length) {
        root.innerHTML = '<div class="empty">No business actions</div>';
        return;
      }
      state.businessActions.forEach(action => {
        const btn = document.createElement("button");
        btn.className = "action-item" + (action.id === state.selectedBusinessAction ? " active" : "");
        btn.innerHTML = '<div class="dataset-id"></div><div class="dataset-meta"></div>';
        btn.querySelector(".dataset-id").textContent = action.title;
        btn.querySelector(".dataset-meta").textContent = action.id + " / " + action.dataset_id;
        btn.onclick = () => selectBusinessAction(action);
        root.appendChild(btn);
      });
      appendLoadMoreButton(root, state.businessActionHasMore && state.businessActionNextBeforeID, () => loadBusinessActions(true));
    }

    function selectBusinessAction(action) {
      state.selectedBusinessAction = action.id;
      renderBusinessActions();
      $("businessActionId").value = action.id || "";
      $("businessActionDataset").value = action.dataset_id || "";
      $("businessActionDescription").value = (action.description || "") + "\nRequired: " + (action.required_fields || []).join(", ");
      const sample = {};
      (action.input_fields || []).forEach(field => {
        if ((action.required_fields || []).includes(field.key)) sample[field.key] = field.type === "number" ? 0 : "";
      });
      $("businessActionData").value = JSON.stringify(sample, null, 2);
    }

    async function executeBusinessAction(dryRun) {
      try {
        const actionID = state.selectedBusinessAction || $("businessActionId").value.trim();
        if (!actionID) throw new Error("Select a business action first");
        const body = {
          record_id: $("businessActionRecordId").value.trim(),
          idempotency_key: $("businessActionIdempotencyKey").value.trim(),
          data: parseJSONField("businessActionData", {}),
          dry_run: !!dryRun
        };
        const result = await api("/api/v1/data/business-actions/" + encodeURIComponent(actionID) + "/execute", { method: "POST", body: JSON.stringify(body) });
        renderBusinessActionResult(result || {});
        setRaw(result || {});
        if (dryRun) {
          setStatus(result.valid ? "Business action dry-run passed" : "Business action dry-run failed", result.valid ? "ok" : "err");
          return;
        }
        if (result.event && result.event.dataset_id) state.selectedDataset = result.event.dataset_id;
        await loadDatasets();
        if (state.selectedDataset) await queryRecords();
        setStatus("Business action executed", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderBusinessActionResult(result) {
      const root = $("businessActionResult");
      const validation = result.validation || {};
      const rows = [
        ["Dry run", result.dry_run ? "yes" : "no"],
        ["Valid", result.valid === undefined ? "" : String(result.valid)],
        ["Dataset", (result.action && result.action.dataset_id) || (result.event && result.event.dataset_id) || ""],
        ["Record", (result.event && result.event.record_id) || ""],
        ["Event status", (result.event && result.event.status) || ""],
        ["Governance", result.rules ? (result.rules.governance_status || "") : ""],
        ["Can execute now", result.rules ? String(!!result.rules.can_execute_now) : ""],
        ["Recommended action", result.rules ? (result.rules.recommended_action || "") : ""],
        ["Rules", result.rules ? [
          result.rules.requires_dry_run ? "dry-run" : "",
          result.rules.requires_approval ? "approval" : "",
          result.rules.requires_backup ? "backup" : "",
          result.rules.requires_quality ? "quality" : "",
          result.rules.requires_admin ? "data_admin" : ""
        ].filter(Boolean).join(", ") : ""],
        ["Rule next steps", result.rules ? JSON.stringify(result.rules.next_steps || [], null, 2) : ""],
        ["Unknown fields", (validation.unknown_fields || []).join(", ")],
        ["Errors", (validation.errors || []).join("; ")],
      ];
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Item</th><th>Value</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(row => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td><pre></pre></td>";
        tr.children[0].textContent = row[0];
        tr.children[1].querySelector("pre").textContent = row[1];
        body.appendChild(tr);
      });
      if (result.preview) {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td>Preview</td><td><pre></pre></td>";
        tr.children[1].querySelector("pre").textContent = JSON.stringify(result.preview, null, 2);
        body.appendChild(tr);
      }
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function loadBusinessRules(loadMore = false) {
      try {
        const params = new URLSearchParams();
        const domain = $("ruleDomain").value.trim();
        const actionID = $("ruleBusinessAction").value.trim();
        const severity = $("ruleSeverity").value.trim();
        if (domain) params.set("domain", domain);
        if (actionID) params.set("business_action_id", actionID);
        if (severity) params.set("severity", severity);
        loadMore = preparePageParams("businessRulePageKey", params, loadMore === true);
        if (loadMore && state.businessRuleNextBeforeID) params.set("before_id", state.businessRuleNextBeforeID);
        const path = "/api/v1/data/business-rules" + (params.toString() ? "?" + params.toString() : "");
        const data = await api(path, { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.businessRules = loadMore ? (state.businessRules || []).concat(items) : items;
        state.businessRuleNextBeforeID = (data && data.next_before_id) || "";
        state.businessRuleHasMore = !!(data && data.has_more && data.next_before_id);
        renderBusinessRules();
        setStatus("Business rules loaded: " + state.businessRules.length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderBusinessRules() {
      const root = $("ruleTable");
      if (!root) return;
      if (!state.businessRules.length) {
        root.innerHTML = '<div class="empty">No matching business rules</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>Scope</th><th>Severity</th><th>Conditions</th><th>Requires</th><th>Checks</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      state.businessRules.forEach(rule => {
        const requires = [];
        if (rule.requires_dry_run) requires.push("dry-run");
        if (rule.requires_approval) requires.push("approval");
        if (rule.requires_backup) requires.push("backup");
        if (rule.requires_quality) requires.push("quality");
        if (rule.requires_admin) requires.push("data_admin");
        const tr = document.createElement("tr");
        tr.innerHTML = "<td><strong></strong><div class=\"muted\"></div></td><td></td><td></td><td><pre></pre></td><td></td><td><pre></pre></td>";
        tr.querySelector("strong").textContent = rule.title || rule.id;
        tr.querySelector(".muted").textContent = rule.id || "";
        tr.children[1].textContent = [rule.domain, rule.dataset_id, rule.business_action_id].filter(Boolean).join(" / ") || "global";
        tr.children[2].textContent = rule.severity || "";
        const conditionMode = rule.conditions_mode || ((rule.conditions || []).length ? "all" : "");
        const conditionLines = (rule.conditions || []).map(condition => condition.field + " " + condition.op + " " + JSON.stringify(condition.value));
        tr.children[3].querySelector("pre").textContent = conditionMode ? conditionMode + ":\n" + conditionLines.join("\n") : "";
        tr.children[4].textContent = requires.join(", ");
        tr.children[5].querySelector("pre").textContent = (rule.recommended_checks || []).join("\n");
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.businessRuleHasMore && state.businessRuleNextBeforeID, () => loadBusinessRules(true));
    }

    async function evaluateBusinessRules() {
      try {
        const actionID = $("ruleBusinessAction").value.trim() || state.selectedBusinessAction || $("businessActionId").value.trim();
        const body = {
          domain: $("ruleDomain").value.trim(),
          business_action_id: actionID,
          dataset_id: $("businessActionDataset").value.trim() || state.selectedDataset,
          record_id: $("businessActionRecordId").value.trim(),
          dry_run: true,
          data: parseJSONField("businessActionData", {})
        };
        const result = await api("/api/v1/data/business-rules/evaluate", { method: "POST", body: JSON.stringify(body) });
        renderRuleEvaluation(result || {});
        setRaw(result || {});
        setStatus("Business rule evaluation ready", result.requires_admin ? "err" : "ok");
      } catch (err) { notifyError(err); }
    }

    function renderRuleEvaluation(result) {
      const root = $("ruleEvaluation");
      const rows = [
        ["Business action", result.business_action_id || ""],
        ["Dataset", result.dataset_id || ""],
        ["Domain", result.domain || ""],
        ["Dry-run context", result.dry_run ? "yes" : "no"],
        ["Governance", result.governance_status || ""],
        ["Reasons", (result.status_reasons || []).join("\n")],
        ["Can execute now", result.can_execute_now === undefined ? "" : String(result.can_execute_now)],
        ["Recommended action", result.recommended_action || ""],
        ["Gate statuses", JSON.stringify(result.gate_statuses || [], null, 2)],
        ["Matched rules", (result.matched_rules || []).map(rule => rule.id).join("\n")],
        ["Condition results", JSON.stringify(result.rule_evaluations || [], null, 2)],
        ["Dry-run", result.requires_dry_run ? "required" : ""],
        ["Approval", result.requires_approval ? "required" : ""],
        ["Backup", result.requires_backup ? "required" : ""],
        ["Quality", result.requires_quality ? "required" : ""],
        ["Admin", result.requires_admin ? "required" : ""],
        ["Checks", (result.recommended_checks || []).join("\n")],
        ["Next steps", JSON.stringify(result.next_steps || [], null, 2)],
      ];
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Item</th><th>Value</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(row => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td><pre></pre></td>";
        tr.children[0].textContent = row[0];
        tr.children[1].querySelector("pre").textContent = row[1];
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function useSelectedActionForRules() {
      const actionID = state.selectedBusinessAction || $("businessActionId").value.trim();
      if (actionID) $("ruleBusinessAction").value = actionID;
      const datasetID = $("businessActionDataset").value.trim();
      if (datasetID && datasetID.includes(".")) $("ruleDomain").value = datasetID.split(".")[0];
      switchTab("rules");
      loadBusinessRules();
    }

    async function checkSelectedBusinessRules() {
      const actionID = state.selectedBusinessAction || $("businessActionId").value.trim();
      if (actionID) $("ruleBusinessAction").value = actionID;
      const datasetID = $("businessActionDataset").value.trim();
      if (datasetID && datasetID.includes(".")) $("ruleDomain").value = datasetID.split(".")[0];
      switchTab("rules");
      await loadBusinessRules();
      await evaluateBusinessRules();
    }

    async function loadEventContract() {
      try {
        const actionID = state.selectedBusinessAction || $("businessActionId").value.trim();
        if (!actionID) throw new Error("Select a business action first");
        const result = await api("/api/v1/data/event-contracts/" + encodeURIComponent(actionID), { method: "GET" });
        applyEventContract(result);
        setStatus("Loaded event contract: " + actionID, "ok");
      } catch (err) { notifyError(err); }
    }

    async function loadEventContracts(loadMore = false) {
      try {
        const params = new URLSearchParams({ limit: "50" });
        const domain = $("eventContractDomain").value.trim();
        if (domain) params.set("domain", domain);
        loadMore = preparePageParams("eventContractPageKey", params, loadMore === true);
        if (loadMore && state.eventContractNextBeforeID) params.set("before_id", state.eventContractNextBeforeID);
        const data = await api("/api/v1/data/event-contracts?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.eventContracts = loadMore ? (state.eventContracts || []).concat(items) : items;
        state.eventContractNextBeforeID = (data && data.next_before_id) || "";
        state.eventContractHasMore = !!(data && data.has_more && data.next_before_id);
        renderEventContracts();
        setStatus("Event contracts loaded: " + (state.eventContracts || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderEventContracts() {
      const root = $("eventContractTable");
      if (!root) return;
      if (!(state.eventContracts || []).length) {
        root.innerHTML = '<div class="empty">No event contracts</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>Domain</th><th>Dataset</th><th>Event type</th><th>Endpoint</th><th>Fields</th><th></th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      state.eventContracts.forEach(contract => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td><strong></strong><div class='muted'></div></td><td></td><td></td><td></td><td></td><td><pre></pre></td><td><button>Select</button></td>";
        tr.querySelector("strong").textContent = contract.title || contract.id || "";
        tr.querySelector(".muted").textContent = contract.business_action_id || "";
        tr.children[1].textContent = contract.domain || "";
        tr.children[2].textContent = contract.dataset_id || "";
        tr.children[3].textContent = contract.event_type || "";
        tr.children[4].textContent = contract.connector_endpoint_template || contract.endpoint || "";
        tr.children[5].querySelector("pre").textContent = (contract.required_fields || []).join("\n");
        tr.querySelector("button").onclick = () => applyEventContract(contract);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.eventContractHasMore && state.eventContractNextBeforeID, () => loadEventContracts(true));
    }

    function applyEventContract(contract) {
      setRaw(contract || {});
      const actionID = (contract && contract.business_action_id) || state.selectedBusinessAction || $("businessActionId").value.trim();
      if (actionID) $("eventBusinessAction").value = actionID;
      if (contract && contract.dry_run_body_template) {
        $("eventIngestJson").value = JSON.stringify(contract.dry_run_body_template, null, 2);
      }
    }

    async function loadConnectors(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("connectorPageKey", params, loadMore === true);
        if (loadMore && state.connectorNextBefore) params.set("before", state.connectorNextBefore);
        if (loadMore && state.connectorNextBeforeID) params.set("before_id", state.connectorNextBeforeID);
        const data = await api("/api/v1/data/connectors" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.connectors = loadMore ? (state.connectors || []).concat(items) : items;
        state.connectorNextBefore = (data && data.next_before) || "";
        state.connectorNextBeforeID = (data && data.next_before_id) || "";
        state.connectorHasMore = !!(data && data.has_more && data.next_before_id);
        renderConnectors();
      } catch (err) { notifyError(err); }
    }

    async function loadConnectorHealthOverview() {
      try {
        const data = await loadAllConnectorHealth();
        state.connectorHealth = data.items;
        state.connectorHealthUnavailable = "";
        updateAdminSummary();
        setRaw(data);
        setStatus("Connector health overview loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function loadAllConnectorHealth() {
      const items = [];
      let before = "";
      let beforeID = "";
      let page = 0;
      let last = null;
      do {
        const params = new URLSearchParams({ limit: "100" });
        if (before) params.set("before", before);
        if (beforeID) params.set("before_id", beforeID);
        last = await api("/api/v1/data/connectors/health?" + params.toString(), { method: "GET" });
        items.push(...(Array.isArray(last) ? last : ((last && last.items) || [])));
        before = (last && last.next_before) || "";
        beforeID = (last && last.next_before_id) || "";
        page += 1;
      } while (last && last.has_more && beforeID && page < 20);
      return { items, limit: items.length, has_more: !!(last && last.has_more && beforeID), next_before: before, next_before_id: beforeID };
    }

    function renderConnectors() {
      const root = $("connectorTable");
      if (!root) return;
      if (!state.connectors.length) {
        root.innerHTML = '<div class="empty">No connectors registered</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>Name</th><th>Domain</th><th>Kind</th><th>Enabled</th><th>Actions</th><th></th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      state.connectors.forEach(connector => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td><pre></pre></td><td><button>Select</button></td>";
        tr.children[0].textContent = connector.id || "";
        tr.children[1].textContent = connector.name || "";
        tr.children[2].textContent = connector.domain || "";
        tr.children[3].textContent = connector.kind || "";
        tr.children[4].textContent = connector.enabled ? "yes" : "no";
        tr.children[5].querySelector("pre").textContent = (connector.subscribed_actions || []).join(", ");
        tr.children[6].querySelector("button").onclick = () => selectConnector(connector);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.connectorHasMore && state.connectorNextBeforeID, () => loadConnectors(true));
    }

    function selectConnector(connector) {
      state.selectedConnector = connector.id || "";
      $("connectorId").value = connector.id || "";
      $("connectorName").value = connector.name || "";
      $("connectorDomain").value = connector.domain || "";
      $("connectorKind").value = connector.kind || "";
      $("connectorAuthType").value = connector.auth_type || "";
      $("connectorTokenRef").value = connector.token_ref || "";
      $("connectorBaseUrl").value = connector.base_url || "";
      $("connectorEnabled").checked = connector.enabled !== false;
      $("connectorActions").value = JSON.stringify(connector.subscribed_actions || [], null, 2);
      $("connectorConfig").value = JSON.stringify(connector.config || {}, null, 2);
      setRaw(connector);
    }

    async function saveConnector() {
      try {
        const connectorID = $("connectorId").value.trim();
        const body = {
          id: connectorID,
          name: $("connectorName").value.trim(),
          domain: $("connectorDomain").value.trim(),
          kind: $("connectorKind").value.trim(),
          auth_type: $("connectorAuthType").value.trim(),
          token_ref: $("connectorTokenRef").value.trim(),
          base_url: $("connectorBaseUrl").value.trim(),
          enabled: $("connectorEnabled").checked,
          subscribed_actions: parseJSONField("connectorActions", []),
          config: parseJSONField("connectorConfig", {})
        };
        const path = connectorID ? "/api/v1/data/connectors/" + encodeURIComponent(connectorID) : "/api/v1/data/connectors";
        const method = connectorID ? "PUT" : "POST";
        const result = await api(path, { method, body: JSON.stringify(body) });
        setRaw(result);
        setStatus("Connector saved: " + result.id, "ok");
        await loadConnectors();
      } catch (err) { notifyError(err); }
    }

    async function testConnector() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/test", { method: "POST" });
        setRaw(result);
        setStatus(result.valid ? "Connector contracts valid" : "Connector contracts need attention", result.valid ? "ok" : "err");
      } catch (err) { notifyError(err); }
    }

    async function validateConnectorConfig() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/config/validate", { method: "POST" });
        setRaw(result);
        setStatus(result.valid ? "Connector config valid" : "Connector config has issues: " + ((result.issues || []).length), result.valid ? "ok" : "err");
      } catch (err) { notifyError(err); }
    }

    async function checkConnectorReadiness() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        let sampleEvent = null;
        if ($("eventIngestJson").value.trim()) {
          sampleEvent = parseJSONField("eventIngestJson", {});
        }
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/readiness", { method: "POST", body: JSON.stringify({ sample_event: sampleEvent }) });
        setRaw(result);
        setStatus(result.ready ? "Connector ready for dry-run sync" : "Connector readiness failed", result.ready ? "ok" : "err");
      } catch (err) { notifyError(err); }
    }

    async function checkConnectorHealth() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/health", { method: "GET" });
        setRaw(result);
        setStatus("Connector health: " + (result.status || "unknown"), result.status === "degraded" || result.status === "needs_attention" ? "err" : "ok");
      } catch (err) { notifyError(err); }
    }

    async function getConnectorSyncState() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/sync-state", { method: "GET" });
        setRaw(result);
        setStatus("Connector sync state loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function listConnectorSyncRuns(loadMore = false) {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const params = new URLSearchParams({ limit: "50" });
        params.set("connector_id", connectorID);
        loadMore = preparePageParams("connectorSyncRunPageKey", params, loadMore === true);
        params.delete("connector_id");
        if (loadMore && state.connectorSyncRunNextBefore) params.set("before", state.connectorSyncRunNextBefore);
        if (loadMore && state.connectorSyncRunNextBeforeID) params.set("before_id", state.connectorSyncRunNextBeforeID);
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/sync-runs?" + params.toString(), { method: "GET" });
        const items = Array.isArray(result) ? result : ((result && result.items) || []);
        state.connectorSyncRuns = loadMore ? (state.connectorSyncRuns || []).concat(items) : items;
        state.connectorSyncRunNextBefore = (result && result.next_before) || "";
        state.connectorSyncRunNextBeforeID = (result && result.next_before_id) || "";
        state.connectorSyncRunHasMore = !!(result && result.has_more && result.next_before_id);
        setRaw(result);
        renderConnectorSyncRuns(state.connectorSyncRuns);
        setStatus("Connector sync runs loaded: " + (state.connectorSyncRuns || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderConnectorSyncRuns(items) {
      const root = $("connectorSyncRuns");
      if (!root) return;
      if (!items.length) {
        root.innerHTML = '<div class="empty">No connector sync runs</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Finished</th><th>Status</th><th>Total</th><th>Succeeded</th><th>Failed</th><th>Dry run</th><th>Error</th><th>ID</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.finished_at || item.started_at || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = String(item.total || 0);
        tr.children[3].textContent = String(item.succeeded || 0);
        tr.children[4].textContent = String(item.failed || 0);
        tr.children[5].textContent = item.dry_run ? "yes" : "";
        tr.children[6].textContent = item.error_summary || "";
        tr.children[7].textContent = item.id || "";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.connectorSyncRunHasMore && state.connectorSyncRunNextBeforeID, () => listConnectorSyncRuns(true));
    }

    async function markConnectorSyncSuccess() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const body = { status: "success", message: "manual checkpoint from web console", finished_at: new Date().toISOString() };
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/sync-state", { method: "POST", body: JSON.stringify(body) });
        setRaw(result);
        setStatus("Connector sync state updated", "ok");
      } catch (err) { notifyError(err); }
    }

    async function planConnectorSync() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        let sampleEvent = null;
        if ($("eventIngestJson").value.trim()) {
          sampleEvent = parseJSONField("eventIngestJson", {});
        }
        const body = { sample_event: sampleEvent, first_page_events: sampleEvent ? [sampleEvent] : [], page_size: 100 };
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/sync-plan", { method: "POST", body: JSON.stringify(body) });
        setRaw(result);
        setStatus(result.ready ? "Connector sync plan ready" : "Connector sync plan has blockers", result.ready ? "ok" : "err");
      } catch (err) { notifyError(err); }
    }

    async function runConnectorSyncBatch() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        let event = {};
        if ($("eventIngestJson").value.trim()) {
          event = parseJSONField("eventIngestJson", {});
        } else {
          const actions = parseJSONField("connectorActions", []);
          const actionID = Array.isArray(actions) && actions.length ? String(actions[0]) : "";
          if (!actionID) throw new Error("Load or enter an event payload first");
          event = { business_action_id: actionID, data: {} };
        }
        event.dry_run = false;
        const body = { events: [event], sync_state: { status: "success", message: "manual batch from web console", finished_at: new Date().toISOString() } };
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/sync-batch", { method: "POST", body: JSON.stringify(body) });
        setRaw(result);
        setStatus("Connector sync batch: " + result.succeeded + "/" + result.total + " succeeded", result.failed ? "err" : "ok");
      } catch (err) { notifyError(err); }
    }

    async function suggestConnectorMapping() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const actions = parseJSONField("connectorActions", []);
        const actionID = Array.isArray(actions) && actions.length ? String(actions[0]) : "";
        if (!actionID) throw new Error("Connector has no subscribed actions");
        let sample = {};
        if ($("eventIngestJson").value.trim()) {
          const event = parseJSONField("eventIngestJson", {});
          sample = event.data || event.sample_data || event;
        }
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/mappings/suggest", { method: "POST", body: JSON.stringify({ business_action_id: actionID, sample_data: sample }) });
        state.lastConnectorMappingSuggestion = result;
        setRaw(result);
        setStatus("Connector mapping suggestion ready", "ok");
      } catch (err) { notifyError(err); }
    }

    function applySuggestedConnectorMapping() {
      try {
        const suggestion = state.lastConnectorMappingSuggestion;
        if (!suggestion || !suggestion.config_patch) throw new Error("Generate a mapping suggestion first");
        const config = parseJSONField("connectorConfig", {});
        $("connectorConfig").value = JSON.stringify(mergePlainObject(config, suggestion.config_patch), null, 2);
        setRaw({ applied_config_patch: suggestion.config_patch, config: parseJSONField("connectorConfig", {}) });
        setStatus("Connector mapping suggestion merged into config draft", "ok");
      } catch (err) { notifyError(err); }
    }

    async function saveSuggestedConnectorMapping() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const suggestion = state.lastConnectorMappingSuggestion;
        if (!suggestion || !suggestion.config_patch) throw new Error("Generate a mapping suggestion first");
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/config/patch", { method: "POST", body: JSON.stringify({ patch: suggestion.config_patch }) });
        $("connectorConfig").value = JSON.stringify(result.patched_config || {}, null, 2);
        setRaw(result);
        setStatus("Connector mapping suggestion saved", "ok");
        await loadConnectors();
      } catch (err) { notifyError(err); }
    }

    async function loadConnectorEventTemplate() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        const actions = parseJSONField("connectorActions", []);
        const actionID = Array.isArray(actions) && actions.length ? String(actions[0]) : "";
        if (!actionID) throw new Error("Connector has no subscribed actions");
        const result = await api("/api/v1/data/event-contracts/" + encodeURIComponent(actionID), { method: "GET" });
        const body = clonePlain(result.dry_run_body_template || {});
        const source = $("connectorKind").value.trim() || connectorID;
        body.source = source;
        $("eventBusinessAction").value = actionID;
        $("eventSource").value = source;
        $("eventIngestJson").value = JSON.stringify(body, null, 2);
        setRaw({ connector_id: connectorID, endpoint: "/api/v1/data/connectors/" + connectorID + "/events", body });
        switchTab("events");
        setStatus("Loaded connector event template: " + connectorID + " / " + actionID, "ok");
      } catch (err) { notifyError(err); }
    }

    async function previewConnectorEvent() {
      try {
        const connectorID = $("connectorId").value.trim() || state.selectedConnector;
        if (!connectorID) throw new Error("Select or save a connector first");
        let body = {};
        if ($("eventIngestJson").value.trim()) {
          body = parseJSONField("eventIngestJson", {});
        } else {
          const actions = parseJSONField("connectorActions", []);
          const actionID = Array.isArray(actions) && actions.length ? String(actions[0]) : "";
          if (!actionID) throw new Error("Connector has no subscribed actions");
          body = {
            business_action_id: actionID,
            event_type: actionID.replaceAll(".", "_") + ".preview",
            data: {}
          };
        }
        body.dry_run = true;
        const result = await api("/api/v1/data/connectors/" + encodeURIComponent(connectorID) + "/events/preview", { method: "POST", body: JSON.stringify(body) });
        setRaw(result);
        setStatus(result.mapping_applied ? "Connector mapping preview ready" : "Connector preview ready without mapping", "ok");
      } catch (err) { notifyError(err); }
    }

    function clonePlain(value) {
      return JSON.parse(JSON.stringify(value || {}));
    }

    async function loadBusinessViews(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("businessListPageKey", params, loadMore === true);
        if (loadMore && state.businessListNextBeforeID) params.set("before_id", state.businessListNextBeforeID);
        const data = await api("/api/v1/data/views" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.businessViews = loadMore ? (state.businessViews || []).concat(items) : items;
        state.businessListNextBeforeID = (data && data.next_before_id) || "";
        state.businessListHasMore = !!(data && data.has_more && data.next_before_id);
        renderBusinessViews();
        updateAdminSummary();
      } catch (err) { notifyError(err); }
    }

    function renderBusinessViews() {
      const root = $("viewList");
      if (!root) return;
      root.innerHTML = "";
      if (!state.businessViews.length) {
        root.innerHTML = '<div class="empty">No business views</div>';
        return;
      }
      state.businessViews.forEach(view => {
        const btn = document.createElement("button");
        btn.className = "action-item" + (view.id === state.selectedBusinessView ? " active" : "");
        btn.innerHTML = '<div class="dataset-id"></div><div class="dataset-meta"></div>';
        btn.querySelector(".dataset-id").textContent = view.title;
        btn.querySelector(".dataset-meta").textContent = view.id + " / " + view.dataset_id;
        btn.onclick = () => selectBusinessView(view);
        root.appendChild(btn);
      });
      appendLoadMoreButton(root, state.businessListHasMore && state.businessListNextBeforeID, () => loadBusinessViews(true));
    }

    function selectBusinessView(view) {
      state.selectedBusinessView = view.id;
      renderBusinessViews();
      $("businessViewId").value = view.id || "";
      $("businessViewDataset").value = view.dataset_id || "";
      $("businessViewDescription").value = (view.description || "") + "\nFields: " + (view.fields || []).join(", ");
      $("viewQueryLimit").value = view.default_limit || 100;
      $("viewQueryFilter").value = JSON.stringify(view.default_filter || {}, null, 2);
    }

    async function queryBusinessView(loadMore = false) {
      try {
        const viewID = state.selectedBusinessView || $("businessViewId").value.trim();
        if (!viewID) throw new Error("Select a business view first");
        const body = {
          q: $("viewQueryText").value.trim(),
          tag: $("viewQueryTag").value.trim(),
          filter: parseJSONField("viewQueryFilter", {}),
          limit: Number($("viewQueryLimit").value || 100)
        };
        const pageKey = JSON.stringify({ viewID, q: body.q, tag: body.tag, filter: body.filter, limit: body.limit });
        loadMore = loadMore === true && state.businessViewPageKey === pageKey;
        if (loadMore && state.businessViewNextBefore) body.before = state.businessViewNextBefore;
        if (loadMore && state.businessViewNextBeforeID) body.before_id = state.businessViewNextBeforeID;
        const result = await api("/api/v1/data/views/" + encodeURIComponent(viewID) + "/query", { method: "POST", body: JSON.stringify(body) });
        const records = (result && result.records) || [];
        state.businessViewPageKey = pageKey;
        state.businessViewRecords = loadMore ? (state.businessViewRecords || []).concat(records) : records;
        state.businessViewNextBefore = (result && result.next_before) || "";
        state.businessViewNextBeforeID = (result && result.next_before_id) || "";
        state.businessViewHasMore = !!(result && result.has_more && result.next_before && result.next_before_id);
        renderViewRecords(state.businessViewRecords || []);
        if (result.view && result.view.dataset_id) state.selectedDataset = result.view.dataset_id;
        await loadDatasets();
        setStatus("Business view loaded: " + (state.businessViewRecords || []).length + " records", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderViewRecords(items) {
      const root = $("viewRecordTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No records</div>';
        return;
      }
      const keys = Array.from(new Set(items.flatMap(item => Object.keys(item.data || {}))));
      const table = document.createElement("table");
      const head = document.createElement("thead");
      head.innerHTML = "<tr><th>ID</th>" + keys.map(k => "<th></th>").join("") + "<th>Updated</th><th>Actions</th></tr>";
      keys.forEach((key, index) => head.querySelectorAll("th")[index + 1].textContent = key);
      const body = document.createElement("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td>" + keys.map(() => "<td><pre></pre></td>").join("") + "<td></td><td><button class='small'>Edit</button></td>";
        tr.children[0].textContent = item.id;
        keys.forEach((key, index) => { tr.children[index + 1].querySelector("pre").textContent = formatCell((item.data || {})[key]); });
        tr.children[tr.children.length - 2].textContent = item.updated_at || "";
        tr.querySelector("button").onclick = () => loadRecordToEditor(item);
        tr.ondblclick = () => loadRecordToEditor(item);
        body.appendChild(tr);
      });
      table.appendChild(head);
      table.appendChild(body);
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.businessViewHasMore && state.businessViewNextBefore && state.businessViewNextBeforeID, () => queryBusinessView(true));
    }

    async function loadDashboards(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("dashboardPageKey", params, loadMore === true);
        if (loadMore && state.dashboardNextBeforeID) params.set("before_id", state.dashboardNextBeforeID);
        const data = await api("/api/v1/data/dashboards" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.dashboards = loadMore ? (state.dashboards || []).concat(items) : items;
        state.dashboardNextBeforeID = (data && data.next_before_id) || "";
        state.dashboardHasMore = !!(data && data.has_more && data.next_before_id);
        renderDashboards();
        updateAdminSummary();
      } catch (err) { notifyError(err); }
    }

    function renderDashboards() {
      const root = $("dashboardList");
      if (!root) return;
      root.innerHTML = "";
      if (!state.dashboards.length) {
        root.innerHTML = '<div class="empty">No dashboards</div>';
        return;
      }
      state.dashboards.forEach(dashboard => {
        const btn = document.createElement("button");
        btn.className = "action-item" + (dashboard.id === state.selectedDashboard ? " active" : "");
        btn.innerHTML = '<div class="dataset-id"></div><div class="dataset-meta"></div>';
        btn.querySelector(".dataset-id").textContent = dashboard.title || dashboard.id;
        btn.querySelector(".dataset-meta").textContent = dashboard.id + " / " + dashboard.domain;
        btn.onclick = () => selectDashboard(dashboard);
        root.appendChild(btn);
      });
      appendLoadMoreButton(root, state.dashboardHasMore && state.dashboardNextBeforeID, () => loadDashboards(true));
    }

    function selectDashboard(dashboard) {
      state.selectedDashboard = dashboard.id;
      renderDashboards();
      $("dashboardId").value = dashboard.id || "";
      $("dashboardDomain").value = dashboard.domain || "";
      $("dashboardDescription").value = dashboard.description || "";
    }

    async function runDashboard() {
      try {
        const dashboardID = state.selectedDashboard || $("dashboardId").value.trim();
        if (!dashboardID) throw new Error("Select a dashboard first");
        const result = await api("/api/v1/data/dashboards/" + encodeURIComponent(dashboardID) + "/run", { method: "POST" });
        renderDashboardResult(result);
        setStatus("Dashboard complete", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderDashboardResult(result) {
      renderDashboardSummary(result || {});
      renderDashboardReports((result && result.reports) || []);
      $("rawOutput").textContent = JSON.stringify(result || {}, null, 2);
    }

    function renderDashboardSummary(result) {
      const root = $("dashboardSummary");
      const stats = result.stats || {};
      const inbox = result.inbox_summary || {};
      const rows = [
        ["Datasets", stats.dataset_count || 0],
        ["Records", stats.record_count || 0],
        ["Fields", stats.field_count || 0],
        ["Inbox total", inbox.total || 0],
        ["Critical", inbox.critical || 0],
        ["High", inbox.high || 0],
        ["Overdue", inbox.overdue || 0],
      ];
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Metric</th><th>Count</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(row => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td>";
        tr.children[0].textContent = row[0];
        tr.children[1].textContent = row[1];
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function renderDashboardReports(items) {
      const root = $("dashboardReportTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No dashboard reports</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Report</th><th>Status</th><th>Rows</th><th>Preview</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const rows = item.result && item.result.result ? (item.result.result.rows || []) : [];
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td><pre></pre></td>";
        tr.children[0].textContent = item.title || item.report_id || "";
        tr.children[1].textContent = item.error ? item.error : "ok";
        tr.children[2].textContent = rows.length;
        tr.children[3].querySelector("pre").textContent = rows.slice(0, 5).map(row => JSON.stringify(row)).join("\n");
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function loadReports(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("reportPageKey", params, loadMore === true);
        if (loadMore && state.reportNextBeforeID) params.set("before_id", state.reportNextBeforeID);
        const data = await api("/api/v1/data/reports" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.reports = loadMore ? (state.reports || []).concat(items) : items;
        state.reportNextBeforeID = (data && data.next_before_id) || "";
        state.reportHasMore = !!(data && data.has_more && data.next_before_id);
        renderReports();
        updateAdminSummary();
      } catch (err) { notifyError(err); }
    }

    function renderReports() {
      const root = $("reportList");
      if (!root) return;
      root.innerHTML = "";
      if (!state.reports.length) {
        root.innerHTML = '<div class="empty">No reports</div>';
        return;
      }
      state.reports.forEach(report => {
        const btn = document.createElement("button");
        btn.className = "action-item" + (report.id === state.selectedReport ? " active" : "");
        btn.innerHTML = '<div class="dataset-id"></div><div class="dataset-meta"></div>';
        btn.querySelector(".dataset-id").textContent = report.title;
        btn.querySelector(".dataset-meta").textContent = report.id + " / " + report.dataset_id;
        btn.onclick = () => selectReport(report);
        root.appendChild(btn);
      });
      appendLoadMoreButton(root, state.reportHasMore && state.reportNextBeforeID, () => loadReports(true));
    }

    function selectReport(report) {
      state.selectedReport = report.id;
      renderReports();
      $("reportId").value = report.id || "";
      $("reportDataset").value = report.dataset_id || "";
      $("reportFilter").value = JSON.stringify({}, null, 2);
      $("aggregateJson").value = JSON.stringify(report.aggregate || {}, null, 2);
    }

    async function runReport() {
      try {
        const reportID = state.selectedReport || $("reportId").value.trim();
        if (!reportID) throw new Error("Select a report first");
        const filter = parseJSONField("reportFilter", {});
        const result = await api("/api/v1/data/reports/" + encodeURIComponent(reportID) + "/run", { method: "POST", body: JSON.stringify({ filter }) });
        renderReportRows((result.result && result.result.rows) || []);
        if (result.report && result.report.dataset_id) state.selectedDataset = result.report.dataset_id;
        await loadDatasets();
        setStatus("Report complete", "ok");
      } catch (err) { notifyError(err); }
    }

    async function runAggregate() {
      try {
        const dataset = requireDataset();
        const body = parseJSONField("aggregateJson", {});
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/aggregate", { method: "POST", body: JSON.stringify(body) });
        renderReportRows(result.rows || []);
        setStatus("Aggregate complete", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderReportRows(rows) {
      const root = $("reportTable");
      if (!root) return;
      if (!rows.length) {
        root.innerHTML = '<div class="empty">No report rows</div>';
        return;
      }
      const keys = Array.from(new Set(rows.flatMap(row => Object.keys(row))));
      const table = document.createElement("table");
      const head = document.createElement("thead");
      head.innerHTML = "<tr>" + keys.map(() => "<th></th>").join("") + "</tr>";
      keys.forEach((key, index) => head.querySelectorAll("th")[index].textContent = key);
      const body = document.createElement("tbody");
      rows.forEach(row => {
        const tr = document.createElement("tr");
        tr.innerHTML = keys.map(() => "<td><pre></pre></td>").join("");
        keys.forEach((key, index) => { tr.children[index].querySelector("pre").textContent = formatCell(row[key]); });
        body.appendChild(tr);
      });
      table.appendChild(head);
      table.appendChild(body);
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function loadQualityChecks(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("qualityCheckPageKey", params, loadMore === true);
        if (loadMore && state.qualityCheckNextBeforeID) params.set("before_id", state.qualityCheckNextBeforeID);
        const data = await api("/api/v1/data/quality-checks" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.qualityChecks = loadMore ? (state.qualityChecks || []).concat(items) : items;
        state.qualityCheckNextBeforeID = (data && data.next_before_id) || "";
        state.qualityCheckHasMore = !!(data && data.has_more && data.next_before_id);
        renderQualityChecks();
      } catch (err) { notifyError(err); }
    }

    function renderQualityChecks() {
      const root = $("qualityCheckList");
      if (!root) return;
      if (!state.qualityChecks.length) {
        root.innerHTML = '<div class="empty">No quality checks</div>';
        return;
      }
      root.innerHTML = "";
      state.qualityChecks.forEach(check => {
        const item = document.createElement("div");
        item.className = "action-item";
        item.innerHTML = '<div class="dataset-id"></div><div class="dataset-meta"></div>';
        item.querySelector(".dataset-id").textContent = check.title || check.id;
        item.querySelector(".dataset-meta").textContent = check.id + " / " + (check.severity || "info");
        root.appendChild(item);
      });
      appendLoadMoreButton(root, state.qualityCheckHasMore && state.qualityCheckNextBeforeID, () => loadQualityChecks(true));
    }

    async function runQualityCheck() {
      try {
        const dataset = requireDataset();
        const checks = $("qualityChecks").value.split(",").map(x => x.trim()).filter(Boolean);
        const body = {
          checks,
          limit: Number($("qualityLimit").value || 1000),
          include_warnings: $("qualityIncludeWarnings").value === "true"
        };
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/quality/run", { method: "POST", body: JSON.stringify(body) });
        renderQualityIssues(result);
        await listQualityRuns(false);
        setStatus("Quality check scanned " + (result.scanned || 0) + " records", result.valid ? "ok" : "err");
      } catch (err) { notifyError(err); }
    }

    async function listQualityRuns(showStatus, loadMore = false) {
      try {
        const dataset = requireDataset();
        const params = new URLSearchParams({ limit: "20" });
        params.set("dataset_id", dataset);
        loadMore = preparePageParams("qualityRunPageKey", params, loadMore === true);
        params.delete("dataset_id");
        if (loadMore && state.qualityRunNextBefore) params.set("before", state.qualityRunNextBefore);
        if (loadMore && state.qualityRunNextBeforeID) params.set("before_id", state.qualityRunNextBeforeID);
        const data = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/quality/runs?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : ((data && data.items) || []);
        state.qualityRuns = loadMore ? (state.qualityRuns || []).concat(items) : items;
        state.qualityRunNextBefore = (data && data.next_before) || "";
        state.qualityRunNextBeforeID = (data && data.next_before_id) || "";
        state.qualityRunHasMore = !!(data && data.has_more && data.next_before_id);
        renderQualityRuns(state.qualityRuns);
        if (showStatus !== false) setStatus("Quality runs loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderQualityRuns(items) {
      const root = $("qualityRunTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No quality runs</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Time</th><th>ID</th><th>Valid</th><th>Scanned</th><th>Issues</th><th>Checks</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td><button class='small'>Load</button></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.id || "";
        tr.children[2].textContent = String(!!item.valid);
        tr.children[3].textContent = String(item.scanned || 0);
        tr.children[4].textContent = String(item.issue_count || 0);
        tr.children[5].textContent = (item.checks || []).join(", ");
        tr.querySelector("button").onclick = () => loadQualityRun(item.id);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.qualityRunHasMore && state.qualityRunNextBeforeID, () => listQualityRuns(false, true));
    }

    async function loadQualityRun(id) {
      try {
        const dataset = requireDataset();
        const run = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/quality/runs/" + encodeURIComponent(id), { method: "GET" });
        renderQualityIssues(run);
        setStatus("Quality run loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderQualityIssues(result) {
      const root = $("qualityTable");
      const issues = (result && result.issues) || [];
      if (!issues.length) {
        root.innerHTML = '<div class="empty">No quality issues</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Severity</th><th>Check</th><th>Record</th><th>Field</th><th>Message</th><th>Value</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      issues.forEach(issue => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td><pre></pre></td>";
        tr.children[0].textContent = issue.severity || "";
        tr.children[1].textContent = issue.check || "";
        tr.children[2].textContent = issue.record_id || "";
        tr.children[3].textContent = issue.field || "";
        tr.children[4].textContent = issue.message || "";
        tr.children[5].querySelector("pre").textContent = formatCell(issue.value);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }
    function renderTemplates() {
      const select = $("templateSelect");
      select.innerHTML = "";
      state.templates.forEach(tmpl => {
        const option = document.createElement("option");
        option.value = tmpl.id;
        option.textContent = tmpl.title + " (" + tmpl.id + ")";
        select.appendChild(option);
      });
      const loadMore = $("loadMoreTemplates");
      if (loadMore) loadMore.disabled = !(state.templateHasMore && state.templateNextBeforeID);
    }

    async function loadInbox(loadMore = false) {
      try {
        const params = new URLSearchParams();
        const type = $("inboxType").value.trim();
        const status = $("inboxStatus").value.trim();
        const limit = $("inboxLimit").value.trim() || "100";
        if (state.selectedDataset) params.set("dataset_id", state.selectedDataset);
        if (type) params.set("type", type);
        if (status) params.set("status", status);
        if (limit) params.set("limit", limit);
        if ($("inboxIncludeOK").checked) params.set("include_ok", "true");
        loadMore = preparePageParams("inboxPageKey", params, loadMore === true);
        if (loadMore && state.inboxNextBefore) params.set("before", state.inboxNextBefore);
        if (loadMore && state.inboxNextBeforeID) params.set("before_id", state.inboxNextBeforeID);
        let summary = state.inboxSummary || {};
        if (!loadMore) {
          summary = await api("/api/v1/data/inbox/summary?" + params.toString(), { method: "GET" });
          state.inboxSummary = summary || {};
          state.inboxUnavailable = "";
          updateAdminSummary();
        }
        const data = await api("/api/v1/data/inbox?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : ((data && data.items) || []);
        state.inboxItems = loadMore ? (state.inboxItems || []).concat(items) : items;
        state.inboxNextBefore = (data && data.next_before) || "";
        state.inboxNextBeforeID = (data && data.next_before_id) || "";
        state.inboxHasMore = !!(data && data.has_more && data.next_before_id);
        renderInboxSummary(summary || {});
        renderInbox(state.inboxItems);
        setStatus("Inbox loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderInboxSummary(summary) {
      const root = $("inboxSummary");
      const parts = [
        ["Total", summary.total || 0],
        ["Critical", summary.critical || 0],
        ["High", summary.high || 0],
        ["Overdue", summary.overdue || 0]
      ];
      const typeCounts = summary.by_type || {};
      Object.keys(typeCounts).sort().forEach(key => parts.push([key, typeCounts[key]]));
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Metric</th><th>Count</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      parts.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td>";
        tr.children[0].textContent = item[0];
        tr.children[1].textContent = String(item[1]);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.inboxHasMore && state.inboxNextBeforeID, () => loadInbox(true));
    }

    function renderInbox(items) {
      const root = $("inboxTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No inbox items</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Type</th><th>Severity</th><th>Status</th><th>Dataset</th><th>Record</th><th>Title</th><th>Recommended</th><th>Updated</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.type || "";
        tr.children[1].textContent = item.severity || "";
        tr.children[2].textContent = item.status || "";
        tr.children[3].textContent = item.dataset_id || "";
        tr.children[4].textContent = item.record_id || "";
        tr.children[5].textContent = item.title || item.summary || item.id || "";
        tr.children[6].textContent = item.recommended_action || item.action || "";
        tr.children[7].textContent = item.updated_at || item.created_at || "";
        tr.ondblclick = () => loadInboxItem(item);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function loadInboxItem(item) {
      if (item.dataset_id) {
        state.selectedDataset = item.dataset_id;
        renderDatasets();
      }
      if (item.record_id) {
        switchTab("write");
        $("recordId").value = item.record_id;
        if (item.type === "approval") listApprovals(true);
        return;
      }
      if (item.type === "operation_plan") switchTab("ops");
      if (item.type === "quality") switchTab("quality");
    }

    async function loadDomains(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("domainPageKey", params, loadMore === true);
        if (loadMore && state.domainNextBeforeID) params.set("before_id", state.domainNextBeforeID);
        const domains = await api("/api/v1/data/domains" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(domains) ? domains : (Array.isArray(domains?.items) ? domains.items : []);
        state.domains = loadMore ? (state.domains || []).concat(items) : items;
        state.domainNextBeforeID = (domains && domains.next_before_id) || "";
        state.domainHasMore = !!(domains && domains.has_more && domains.next_before_id);
        state.domainsUnavailable = "";
        updateAdminSummary();
        renderDomains(state.domains);
        setStatus("Business domains loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function resolveIntent() {
      try {
        const body = {
          query: $("intentQuery").value.trim(),
          domain: $("intentDomain").value.trim(),
          limit: Number($("intentLimit").value || 5)
        };
        const result = await api("/api/v1/data/intent/resolve", { method: "POST", body: JSON.stringify(body) });
        renderIntentMatches((result && result.matches) || []);
        setRaw(result || {});
        setStatus("Intent resolved: " + (((result && result.matches) || []).length) + " matches", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderIntentMatches(items) {
      const root = $("intentResultTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No intent matches</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Score</th><th>Domain</th><th>Use Case</th><th>Action</th><th>Use</th><th>Required</th><th>Data Template</th><th>Body Template</th><th>Tool Template</th><th>View</th><th>Report</th><th>Dashboard</th><th>Next Steps</th><th>Matched</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const useCase = item.use_case || {};
        const tr = document.createElement("tr");
        const writeStep = (item.next_steps || []).find(step => step.action === "execute_business_action");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td><pre></pre></td><td><pre></pre></td><td><pre></pre></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = String(item.score || 0);
        tr.children[1].textContent = item.domain || "";
        tr.children[2].textContent = (useCase.title || useCase.id || "") + " (" + (useCase.id || "") + ")";
        tr.children[3].textContent = useCase.preferred_action || "";
        const useCell = tr.children[4];
        if (writeStep) {
          const useButton = document.createElement("button");
          useButton.className = "small primary";
          useButton.textContent = "Load action";
          useButton.onclick = () => useIntentWriteStep(writeStep);
          useCell.appendChild(useButton);
        }
        tr.children[5].textContent = ((writeStep && writeStep.required_fields) || []).join(", ");
        tr.children[6].querySelector("pre").textContent = writeStep && writeStep.data_template ? JSON.stringify(writeStep.data_template) : "";
        tr.children[7].querySelector("pre").textContent = writeStep && writeStep.body_template ? JSON.stringify(writeStep.body_template) : "";
        tr.children[8].querySelector("pre").textContent = writeStep && writeStep.tool_call_template ? JSON.stringify(writeStep.tool_call_template) : "";
        tr.children[9].textContent = useCase.preferred_view || "";
        tr.children[10].textContent = useCase.preferred_report || "";
        tr.children[11].textContent = useCase.preferred_dashboard || "";
        tr.children[12].textContent = (item.next_steps || []).map(step => step.order + ". " + step.action + (step.dry_run ? " dry-run" : "")).join(" | ");
        tr.children[13].textContent = (item.matched || []).join(", ");
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function useIntentWriteStep(step) {
      const params = step.params || {};
      const actionID = String(params.business_action_id || "");
      if (!actionID) {
        setStatus("Intent step has no business action id", "err");
        return;
      }
      if (!state.businessActions.length) await loadBusinessActions();
      const action = state.businessActions.find(item => item.id === actionID);
      if (action) {
        selectBusinessAction(action);
      } else {
        state.selectedBusinessAction = actionID;
        $("businessActionId").value = actionID;
        $("businessActionDataset").value = "";
        $("businessActionDescription").value = step.description || "";
      }
      if (step.data_template) $("businessActionData").value = JSON.stringify(step.data_template, null, 2);
      $("businessActionRecordId").value = "";
      $("businessActionIdempotencyKey").value = "";
      switchTab("actions");
      setRaw(step.tool_call_template || step.body_template || step);
      setStatus("Loaded intent action template: " + actionID + " (dry-run first)", "ok");
    }

    async function useIntentReadStep(step) {
      const params = step.params || {};
      const action = step.action || "";
      if (action === "run_dashboard") {
        const dashboardID = String(params.dashboard_id || "");
        if (!dashboardID) return setStatus("Intent step has no dashboard id", "err");
        if (!state.dashboards.length) await loadDashboards();
        const dashboard = state.dashboards.find(item => item.id === dashboardID);
        if (dashboard) selectDashboard(dashboard);
        else {
          state.selectedDashboard = dashboardID;
          $("dashboardId").value = dashboardID;
          $("dashboardDomain").value = "";
          $("dashboardDescription").value = step.description || "";
        }
        switchTab("dashboards");
        await runDashboard();
        return;
      }
      if (action === "query_business_view") {
        const viewID = String(params.view_id || "");
        if (!viewID) return setStatus("Intent step has no view id", "err");
        if (!state.businessViews.length) await loadBusinessViews();
        const view = state.businessViews.find(item => item.id === viewID);
        if (view) selectBusinessView(view);
        else {
          state.selectedBusinessView = viewID;
          $("businessViewId").value = viewID;
          $("businessViewDataset").value = "";
          $("businessViewDescription").value = step.description || "";
          $("viewQueryFilter").value = "{}";
        }
        switchTab("views");
        await queryBusinessView();
        return;
      }
      if (action === "run_report") {
        const reportID = String(params.report_id || "");
        if (!reportID) return setStatus("Intent step has no report id", "err");
        if (!state.reports.length) await loadReports();
        const report = state.reports.find(item => item.id === reportID);
        if (report) selectReport(report);
        else {
          state.selectedReport = reportID;
          $("reportId").value = reportID;
          $("reportDataset").value = "";
          $("reportFilter").value = "{}";
        }
        switchTab("reports");
        await runReport();
        return;
      }
      if (action === "bootstrap_templates") {
        const domains = (params.domains || []).join(",");
        $("bootstrapDomains").value = domains;
        await bootstrapTemplates(true);
        switchTab("raw");
        return;
      }
      setRaw(step.tool_call_template || step.body_template || step);
    }

    function renderDomains(items) {
      const root = $("domainTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No business domains</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Domain</th><th>Ready</th><th>Use Cases</th><th>Datasets</th><th>Missing Templates</th><th>Actions</th><th>Views</th><th>Dashboards</th><th>Reports</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = (item.title || item.domain || "") + " (" + (item.domain || "") + ")";
        tr.children[1].textContent = item.initialized ? "yes" : "no";
        tr.children[2].textContent = String((item.use_cases || []).length);
        tr.children[3].textContent = String((item.datasets || []).length);
        tr.children[4].textContent = (item.missing_templates || []).join(", ");
        tr.children[5].textContent = String((item.business_actions || []).length);
        tr.children[6].textContent = String((item.business_views || []).length);
        tr.children[7].textContent = String((item.dashboards || []).length);
        tr.children[8].textContent = String((item.reports || []).length);
        const actionCell = tr.children[9];
        const inspect = document.createElement("button");
        inspect.className = "small";
        inspect.textContent = "Inspect";
        inspect.onclick = () => setRaw(item);
        const bootstrap = document.createElement("button");
        bootstrap.className = "small primary";
        bootstrap.textContent = "Bootstrap";
        bootstrap.onclick = async () => {
          $("bootstrapDomains").value = item.domain || "";
          await bootstrapTemplates(true);
          switchTab("raw");
        };
        actionCell.appendChild(inspect);
        actionCell.appendChild(document.createTextNode(" "));
        actionCell.appendChild(bootstrap);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.domainHasMore && state.domainNextBeforeID, () => loadDomains(true));
    }

    async function loadRelationships(loadMore = false) {
      try {
        if ($("relationshipSelectedDataset")) $("relationshipSelectedDataset").value = state.selectedDataset || "";
        const datasetID = $("relationshipDataset").value.trim();
        const params = new URLSearchParams();
        if (datasetID) params.set("dataset_id", datasetID);
        loadMore = preparePageParams("relationshipPageKey", params, loadMore === true);
        if (loadMore && state.relationshipNextBeforeID) params.set("before_id", state.relationshipNextBeforeID);
        const path = "/api/v1/data/relationships" + (params.toString() ? "?" + params.toString() : "");
        const relationships = await api(path, { method: "GET" });
        const items = Array.isArray(relationships) ? relationships : (Array.isArray(relationships?.items) ? relationships.items : []);
        state.relationships = loadMore ? (state.relationships || []).concat(items) : items;
        state.relationshipNextBeforeID = (relationships && relationships.next_before_id) || "";
        state.relationshipHasMore = !!(relationships && relationships.has_more && relationships.next_before_id);
        renderRelationships(state.relationships);
        setStatus("Relationships loaded: " + state.relationships.length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderRelationships(items) {
      const root = $("relationshipTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No relationships</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Source Dataset</th><th>Field</th><th>Title</th><th>Type</th><th>Target Dataset</th><th>Source</th><th>Ready</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.source_dataset_id || "";
        tr.children[1].textContent = item.source_field || "";
        tr.children[2].textContent = item.source_title || "";
        tr.children[3].textContent = item.field_type || "";
        tr.children[4].textContent = item.target_dataset_id || "";
        tr.children[5].textContent = item.from_template ? "template" : "dataset";
        tr.children[6].textContent = item.initialized ? "yes" : "no";
        const actionCell = tr.children[7];
        const inspect = document.createElement("button");
        inspect.className = "small";
        inspect.textContent = "Inspect";
        inspect.onclick = () => setRaw(item);
        const openSource = document.createElement("button");
        openSource.className = "small";
        openSource.textContent = "Open source";
        openSource.onclick = () => openRelationshipDataset(item.source_dataset_id);
        actionCell.appendChild(inspect);
        actionCell.appendChild(document.createTextNode(" "));
        actionCell.appendChild(openSource);
        if (item.target_dataset_id) {
          const openTarget = document.createElement("button");
          openTarget.className = "small";
          openTarget.textContent = "Open target";
          openTarget.onclick = () => openRelationshipDataset(item.target_dataset_id);
          actionCell.appendChild(document.createTextNode(" "));
          actionCell.appendChild(openTarget);
        }
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.relationshipHasMore && state.relationshipNextBeforeID, () => loadRelationships(true));
    }

    function openRelationshipDataset(datasetID) {
      datasetID = String(datasetID || "").trim();
      if (!datasetID) return;
      state.selectedDataset = datasetID;
      renderDatasets();
      switchTab("records");
      queryRecords();
    }

    async function loadDatasets(loadMore = false) {
      try {
        const params = new URLSearchParams();
        loadMore = preparePageParams("datasetPageKey", params, loadMore === true);
        if (loadMore && state.datasetNextBefore) params.set("before", state.datasetNextBefore);
        if (loadMore && state.datasetNextBeforeID) params.set("before_id", state.datasetNextBeforeID);
        const data = await api("/api/v1/data/datasets" + (params.toString() ? "?" + params.toString() : ""), { method: "GET" });
        const items = Array.isArray(data) ? data : (Array.isArray(data?.items) ? data.items : []);
        state.datasets = loadMore ? (state.datasets || []).concat(items) : items;
        state.datasetNextBefore = (data && data.next_before) || "";
        state.datasetNextBeforeID = (data && data.next_before_id) || "";
        state.datasetHasMore = !!(data && data.has_more && data.next_before_id);
        if (state.selectedDataset && !state.datasets.some(ds => ds.id === state.selectedDataset)) state.selectedDataset = "";
        renderDatasets();
        updateAdminSummary();
        if (state.selectedDataset) await loadDatasetDetail(false);
      } catch (err) { notifyError(err); }
    }

    function renderDatasets() {
      const root = $("datasetList");
      root.innerHTML = "";
      if (!state.datasets.length) {
        root.innerHTML = '<div class="empty">No datasets</div>';
        clearDatasetForm();
        updateModuleHeader(activeModuleName());
        return;
      }
      state.datasets.forEach(ds => {
        const btn = document.createElement("button");
        btn.className = "dataset-item" + (ds.id === state.selectedDataset ? " active" : "");
        btn.dataset.testid = "dataset-item";
        btn.innerHTML = '<div class="dataset-id"></div><div class="dataset-meta"></div>';
        btn.querySelector(".dataset-id").textContent = ds.id;
        btn.querySelector(".dataset-meta").textContent = (ds.title || "Untitled") + " / " + ds.domain;
        btn.onclick = async () => {
          state.selectedDataset = ds.id;
          renderDatasets();
          fillDatasetForm(ds);
          await queryRecords();
        };
          root.appendChild(btn);
        });
        appendLoadMoreButton(root, state.datasetHasMore && state.datasetNextBeforeID, () => loadDatasets(true));
        updateModuleHeader(activeModuleName());
      }

    async function createFromTemplate() {
      try {
        const templateID = $("templateSelect").value;
        if (!templateID) throw new Error("Select a template first");
        const body = {};
        const customID = $("templateDatasetId").value.trim();
        if (customID) body.id = customID;
        const result = await api("/api/v1/data/templates/" + encodeURIComponent(templateID) + "/create", { method: "POST", body: JSON.stringify(body) });
        state.selectedDataset = result.dataset.id;
        fillDatasetForm(result.dataset);
        $("fieldsJson").value = JSON.stringify({ fields: result.fields || [] }, null, 2);
        await loadDatasets();
        setStatus("Created from template " + templateID, "ok");
      } catch (err) { notifyError(err); }
    }

    async function bootstrapTemplates(dryRun) {
      try {
        const domains = $("bootstrapDomains").value.split(",").map((item) => item.trim()).filter(Boolean);
        const payload = { dry_run: !!dryRun };
        if (domains.length) payload.domains = domains;
        const result = await api("/api/v1/data/templates/bootstrap", { method: "POST", body: JSON.stringify(payload) });
        if (!dryRun) await loadDatasets();
        setRaw(result || {});
        setStatus((dryRun ? "Bootstrap preview: would create " + ((result.would_create || []).length) : "Bootstrap complete: created " + ((result.created || []).length)) + ", skipped " + ((result.skipped || []).length), "ok");
      } catch (err) { notifyError(err); }
    }

    async function createDataset() {
      try {
        const body = {
          domain: $("newDomain").value.trim(),
          name: $("newName").value.trim(),
          title: $("newTitle").value.trim()
        };
        const ds = await api("/api/v1/data/datasets", { method: "POST", body: JSON.stringify(body) });
        state.selectedDataset = ds.id;
        fillDatasetForm(ds);
        await loadDatasets();
        setStatus("Created " + ds.id, "ok");
      } catch (err) { notifyError(err); }
    }

    function fillDatasetForm(ds) {
      if (!ds) return clearDatasetForm();
      $("datasetId").value = ds.id || "";
      $("datasetDomain").value = ds.domain || "";
      $("datasetName").value = ds.name || "";
      $("datasetTitle").value = ds.title || "";
      $("datasetDescription").value = ds.description || "";
    }

    function clearDatasetForm() {
      ["datasetId", "datasetDomain", "datasetName", "datasetTitle", "datasetDescription"].forEach(id => { if ($(id)) $(id).value = ""; });
    }

    async function loadDatasetDetail(showStatus) {
      try {
        const dataset = requireDataset();
        const ds = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset), { method: "GET" });
        fillDatasetForm(ds);
        if (showStatus !== false) setStatus("Dataset loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function updateDataset() {
      try {
        const dataset = requireDataset();
        const body = { title: $("datasetTitle").value.trim(), description: $("datasetDescription").value.trim() };
        const ds = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset), { method: "PATCH", body: JSON.stringify(body) });
        fillDatasetForm(ds);
        await loadDatasets();
        setStatus("Dataset updated", "ok");
      } catch (err) { notifyError(err); }
    }

    async function deleteDataset() {
      try {
        const dataset = requireDataset();
        if (!confirm("Delete dataset " + dataset + " and all its records?")) return;
        await api("/api/v1/data/datasets/" + encodeURIComponent(dataset), { method: "DELETE" });
        state.selectedDataset = "";
        state.records = [];
        state.recordNextBefore = "";
        state.recordNextBeforeID = "";
        state.recordHasMore = false;
        clearDatasetForm();
        renderRecords([]);
        await loadDatasets();
        setStatus("Dataset deleted", "ok");
      } catch (err) { notifyError(err); }
    }

    async function queryRecords(loadMore = false) {
      try {
        const dataset = requireDataset();
        const body = queryBody();
        const pageKey = JSON.stringify({ dataset, q: body.q, tag: body.tag, filter: body.filter || null, limit: body.limit });
        loadMore = loadMore === true && state.recordPageKey === pageKey;
        if (loadMore && state.recordNextBefore) body.before = state.recordNextBefore;
        if (loadMore && state.recordNextBeforeID) body.before_id = state.recordNextBeforeID;
        const data = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/query", { method: "POST", body: JSON.stringify(body) });
        const items = data.items || [];
        state.recordPageKey = pageKey;
        state.records = loadMore ? (state.records || []).concat(items) : items;
        state.recordNextBefore = (data && data.next_before) || "";
        state.recordNextBeforeID = (data && data.next_before_id) || "";
        state.recordHasMore = !!(data && data.has_more && data.next_before_id);
        renderRecords(state.records);
        setStatus("Query complete: " + state.records.length + " records", "ok");
      } catch (err) { notifyError(err); }
    }

    function queryBody() {
      const filter = parseJSONField("queryFilter", null);
      const body = {
        q: $("queryText").value.trim(),
        tag: $("queryTag").value.trim(),
        limit: Number($("queryLimit").value || 50)
      };
      if (filter) body.filter = filter;
      return body;
    }

    async function exportRecordsCsv() {
      try {
        const dataset = requireDataset();
        const csv = await apiText("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/export.csv", { method: "POST", body: JSON.stringify(queryBody()) });
        const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = dataset.replace(/[\\/:*?"<>|]+/g, "_") + ".csv";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setStatus("CSV exported: " + dataset, "ok");
      } catch (err) { notifyError(err); }
    }

    async function exportRecordsJsonl() {
      try {
        const dataset = requireDataset();
        const jsonl = await apiText("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/export.jsonl", { method: "POST", body: JSON.stringify(queryBody()) });
        const blob = new Blob([jsonl], { type: "application/x-ndjson;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = dataset.replace(/[\\/:*?"<>|]+/g, "_") + ".jsonl";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setStatus("JSONL exported", "ok");
      } catch (err) { notifyError(err); }
    }

    async function startExportJob(format) {
      try {
        const dataset = requireDataset();
        const job = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/export." + format + "/jobs", { method: "POST", body: JSON.stringify(queryBody()) });
        setStatus(format.toUpperCase() + " export job queued: " + job.id, "ok");
        await listExportJobs(false);
      } catch (err) { notifyError(err); }
    }

    async function listExportJobs(showStatus, loadMore = false) {
      try {
        const dataset = requireDataset();
        const params = new URLSearchParams({ dataset_id: dataset, limit: "20" });
        loadMore = preparePageParams("exportJobPageKey", params, loadMore === true);
        if (loadMore && state.exportJobNextBefore) params.set("before", state.exportJobNextBefore);
        if (loadMore && state.exportJobNextBeforeID) params.set("before_id", state.exportJobNextBeforeID);
        const data = await api("/api/v1/data/export-jobs?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : ((data && data.items) || []);
        state.exportJobs = loadMore ? (state.exportJobs || []).concat(items) : items;
        state.exportJobNextBefore = (data && data.next_before) || "";
        state.exportJobNextBeforeID = (data && data.next_before_id) || "";
        state.exportJobHasMore = !!(data && data.has_more && data.next_before_id);
        renderExportJobs(state.exportJobs);
        if (showStatus) setStatus("Export jobs loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderExportJobs(items) {
      const root = $("exportJobTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No export jobs</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Created</th><th>Status</th><th>Format</th><th>Total</th><th>Bytes</th><th>Error</th><th>Download</th><th>ID</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.format || "";
        tr.children[3].textContent = String(item.total || 0);
        tr.children[4].textContent = String(item.bytes || 0);
        tr.children[5].textContent = item.error || "";
        if (item.status === "completed" && item.download_path) {
          const link = document.createElement("button");
          link.className = "small";
          link.textContent = "Download";
          link.onclick = () => downloadExportJob(item);
          tr.children[6].appendChild(link);
        }
        tr.children[7].textContent = item.id || "";
        body.appendChild(tr);
      });
      table.appendChild(body);
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.exportJobHasMore && state.exportJobNextBeforeID, () => listExportJobs(false, true));
    }

    async function downloadExportJob(item) {
      try {
        const text = await apiText(item.download_path, { method: "GET" });
        const ext = item.format === "csv" ? "csv" : "jsonl";
        const type = item.format === "csv" ? "text/csv;charset=utf-8" : "application/x-ndjson;charset=utf-8";
        const blob = new Blob([text], { type });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = (item.dataset_id || "records").replace(/[\\/:*?"<>|]+/g, "_") + "." + ext;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setStatus("Export job downloaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderRecords(items) {
      const root = $("recordTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No records</div>';
        return;
      }
      const keys = Array.from(new Set(items.flatMap(item => Object.keys(item.data || {})))).slice(0, 8);
      const table = document.createElement("table");
      const head = document.createElement("thead");
      head.innerHTML = "<tr><th>ID</th><th>Title</th><th>Tags</th>" + keys.map(k => "<th></th>").join("") + "<th>Updated</th><th>Actions</th></tr>";
      keys.forEach((key, index) => head.querySelectorAll("th")[index + 3].textContent = key);
      const body = document.createElement("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        const tagHtml = (item.tags || []).map(tag => '<span class="pill">' + escapeHTML(tag) + '</span>').join("");
        tr.innerHTML = "<td></td><td></td><td>" + tagHtml + "</td>" + keys.map(() => "<td><pre></pre></td>").join("") + "<td></td><td><div class='row'></div></td>";
        tr.children[0].textContent = item.id;
        tr.children[1].textContent = item.title || "";
        keys.forEach((key, index) => { tr.children[index + 3].querySelector("pre").textContent = formatCell((item.data || {})[key]); });
        tr.children[tr.children.length - 2].textContent = item.updated_at || "";
        const actions = tr.children[tr.children.length - 1].querySelector(".row");
        const edit = document.createElement("button");
        edit.className = "small";
        edit.textContent = "Edit";
        edit.onclick = () => loadRecordToEditor(item);
        const del = document.createElement("button");
        del.className = "small danger";
        del.textContent = "Delete";
        del.onclick = () => deleteRecordByID(item.id);
        actions.appendChild(edit);
        actions.appendChild(del);
        tr.ondblclick = () => loadRecordToEditor(item);
        body.appendChild(tr);
      });
      table.appendChild(head);
      table.appendChild(body);
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.recordHasMore && state.recordNextBeforeID, () => queryRecords(true));
    }

    function formatCell(value) {
      if (value === null || value === undefined) return "";
      if (typeof value === "object") return JSON.stringify(value);
      return String(value);
    }

    function escapeHTML(value) {
      return String(value).replace(/[&<>"']/g, ch => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[ch]));
    }

    async function loadAccessCatalog(loadMorePresets = false) {
      try {
        const caps = await api("/api/v1/data/capabilities");
        const presetParams = new URLSearchParams({ limit: "50" });
        if (loadMorePresets && state.accessPresetNextBeforeID) presetParams.set("before_id", state.accessPresetNextBeforeID);
        const presets = await api("/api/v1/data/access/presets?" + presetParams.toString());
        const presetItems = Array.isArray(presets) ? presets : (Array.isArray(presets?.items) ? presets.items : []);
        state.accessCapabilities = caps;
        state.accessPresets = loadMorePresets ? (state.accessPresets || []).concat(presetItems) : presetItems;
        state.accessPresetNextBeforeID = (presets && presets.next_before_id) || "";
        state.accessPresetHasMore = !!(presets && presets.has_more && presets.next_before_id);
        renderAccessPresets();
        renderAccessCatalog(caps);
        await loadManagedAccessKeys(false);
        setStatus(loadMorePresets ? "More authorization presets loaded" : "Business access catalog loaded", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function loadAccessWorkspace(showStatus = false) {
      await loadAccessCatalog();
      await loadAdminAccounts(false);
      await loadAdminSessions(false);
      try {
        await loadGovernanceEvidencePack();
        if (showStatus) setStatus("Governance evidence summary refreshed", "ok");
      } catch (err) {
        $("governanceEvidenceSummary").innerHTML = "<p class='muted'>Governance evidence summary requires data_admin with allow_admin.</p>";
        if (showStatus) setStatus(err.message, "err");
      }
    }

    async function loadOverviewStats(showStatus) {
      try {
        const stats = await api("/api/v1/data/stats", { method: "GET" });
        state.stats = stats || {};
        updateAdminSummary();
        if (showStatus) setStatus("Overview stats loaded", "ok");
      } catch (err) {
        state.stats = null;
        updateAdminSummary();
        if (showStatus) notifyError(err);
      }
    }

    async function loadOverviewCapabilitiesData(showStatus) {
      try {
        const caps = await api("/api/v1/data/capabilities", { method: "GET" });
        state.accessCapabilities = caps || {};
        updateAdminSummary();
        if (showStatus) setStatus("Access capabilities loaded", "ok");
      } catch (err) {
        state.accessCapabilities = state.accessCapabilities || {};
        if (showStatus) notifyError(err);
      } finally {
        renderOverviewCapabilities();
      }
    }

    async function loadOverviewAccessRisk(showStatus) {
      try {
        const review = await api("/api/v1/data/access/review", { method: "GET" });
        state.accessReview = review || {};
        state.accessReviewUnavailable = "";
        updateAdminSummary();
        if (showStatus) setStatus("Access risk loaded", "ok");
      } catch (err) {
        state.accessReview = null;
        state.accessReviewUnavailable = "Admin permission required";
        updateAdminSummary();
        if (showStatus) setStatus(err.message, "err");
      }
    }

    async function loadOverviewWorkQueue(showStatus) {
      try {
        const summary = await api("/api/v1/data/inbox/summary?limit=100", { method: "GET" });
        state.inboxSummary = summary || {};
        state.inboxUnavailable = "";
        updateAdminSummary();
        if (showStatus) setStatus("Work queue loaded", "ok");
      } catch (err) {
        state.inboxSummary = null;
        state.inboxUnavailable = "Unavailable";
        updateAdminSummary();
        if (showStatus) setStatus(err.message, "err");
      }
    }

    async function loadOverviewDomains(showStatus) {
      try {
        const domains = await api("/api/v1/data/domains", { method: "GET" });
        state.domains = Array.isArray(domains) ? domains : (Array.isArray(domains?.items) ? domains.items : []);
        state.domainsUnavailable = "";
        updateAdminSummary();
        if (showStatus) setStatus("Business domains loaded", "ok");
      } catch (err) {
        state.domains = [];
        state.domainsUnavailable = "Unavailable";
        updateAdminSummary();
        if (showStatus) setStatus(err.message, "err");
      }
    }

    async function loadOverviewIntegrationHealth(showStatus) {
      try {
        const health = await loadAllConnectorHealth();
        state.connectorHealth = health.items;
        state.connectorHealthUnavailable = "";
        updateAdminSummary();
        if (showStatus) setStatus("Integration health loaded", "ok");
      } catch (err) {
        state.connectorHealth = [];
        state.connectorHealthUnavailable = "Unavailable";
        updateAdminSummary();
        if (showStatus) setStatus(err.message, "err");
      }
    }

    async function loadOverviewActivity(showStatus) {
      try {
        const data = await api("/api/v1/data/audit?limit=5", { method: "GET" });
        state.auditLogs = (data && data.items) || [];
        state.auditUnavailable = "";
        updateAdminSummary();
        if (showStatus) setStatus("Recent activity loaded", "ok");
      } catch (err) {
        state.auditLogs = [];
        state.auditUnavailable = "Unavailable";
        updateAdminSummary();
        if (showStatus) setStatus(err.message, "err");
      }
    }

    function renderAccessPresets() {
      const select = $("accessPreset");
      select.innerHTML = "<option value=''>Custom</option>";
      (state.accessPresets || []).forEach(item => {
        const option = document.createElement("option");
        option.value = item.id || "";
        option.textContent = (item.title || item.id || "") + (item.description ? " - " + item.description : "");
        select.appendChild(option);
      });
      const loadMore = $("loadMoreAccessPresets");
      if (loadMore) loadMore.disabled = !(state.accessPresetHasMore && state.accessPresetNextBeforeID);
    }

    function renderAccessWorkspaceSummary() {
      const root = $("accessWorkspaceSummary");
      if (!root) return;
      const caps = state.accessCapabilities || {};
      const review = state.accessReview || {};
      const keys = state.accessKeys || [];
      const risky = Number(review.filtered || 0);
      const rawKeys = keys.filter(item => item.allow_raw_data).length;
      const adminKeys = keys.filter(item => item.allow_admin).length;
      const rows = [
        ["Domains", (caps.domains || []).length],
        ["Business actions", (caps.business_actions || []).length],
        ["Reports", (caps.reports || []).length],
        ["Managed keys", keys.length],
        ["Access findings", state.accessReviewUnavailable ? state.accessReviewUnavailable : risky],
        ["Raw/admin exceptions", rawKeys + "/" + adminKeys]
      ];
      root.innerHTML = "";
      rows.forEach(row => {
        const item = document.createElement("div");
        item.className = "access-summary-item";
        item.innerHTML = "<div class='health-label'></div><div class='access-summary-value'></div>";
        item.querySelector(".health-label").textContent = translateText(row[0]);
        const value = item.querySelector(".access-summary-value");
        value.textContent = String(row[1]);
        if ((row[0] === "Access findings" && risky > 0) || (row[0] === "Raw/admin exceptions" && (rawKeys > 0 || adminKeys > 0))) {
          value.classList.add("warn");
        }
        root.appendChild(item);
      });
    }

    function renderAccessCatalog(caps) {
      const root = $("accessCatalog");
      const domains = caps.domains || [];
      const actions = caps.business_actions || [];
      const views = caps.business_views || [];
      const reports = caps.reports || [];
      const dashboards = caps.dashboards || [];
      if (!domains.length) {
        root.innerHTML = "<p>No business capabilities available for the current key.</p>";
        return;
      }
      root.innerHTML = "";
      domains.forEach(domain => {
        const domainID = String(domain);
        const box = document.createElement("section");
        box.className = "stack";
        box.innerHTML = "<h3><label class='check'><input class='access-domain' type='checkbox' value='" + escapeHTML(domainID) + "'> " + escapeHTML(domainID) + "</label></h3>";
        const table = document.createElement("table");
        table.innerHTML = "<thead><tr><th>Grant</th><th>Business capability</th><th>Type</th><th>Description</th></tr></thead><tbody></tbody>";
        const body = table.querySelector("tbody");
        const addRow = (item, type, value, checked) => {
          const tr = document.createElement("tr");
          tr.innerHTML = "<td><input class='access-capability' data-kind='" + type + "' data-domain='" + escapeHTML(domainID) + "' type='checkbox'></td><td></td><td></td><td></td>";
          const input = tr.querySelector("input");
          input.value = value;
          input.checked = !!checked;
          tr.children[1].textContent = item.title || item.id || value;
          tr.children[2].textContent = type;
          tr.children[3].textContent = item.description || item.id || "";
          body.appendChild(tr);
        };
        actions.filter(item => item.domain === domainID).forEach(item => addRow(item, "action", item.id, false));
        views.filter(item => item.domain === domainID).forEach(item => addRow(item, "view", item.id, true));
        reports.filter(item => item.domain === domainID).forEach(item => addRow(item, "report", item.id, true));
        dashboards.filter(item => item.domain === domainID).forEach(item => addRow(item, "dashboard", item.id, true));
        box.appendChild(table);
        root.appendChild(box);
      });
      document.querySelectorAll(".access-domain").forEach(input => {
        input.onchange = () => {
          document.querySelectorAll(".access-capability").forEach(item => {
            if (item.dataset.domain === input.value) item.checked = input.checked;
          });
        };
      });
    }

    function applyAccessPreset() {
      const presetID = $("accessPreset").value;
      const preset = (state.accessPresets || []).find(item => item.id === presetID);
      if (!preset) {
        setStatus("Choose an authorization preset first", "err");
        return;
      }
      $("accessRole").value = preset.role || "data_user";
      $("accessAllowRawData").checked = !!preset.allow_raw_data;
      $("accessAllowSensitive").checked = !!preset.allow_sensitive;
      $("accessAllowAdmin").checked = !!preset.allow_admin;
      $("accessAllowReports").checked = true;
      const domains = new Set(preset.allowed_domains || []);
      const grants = new Set([]
        .concat(preset.allowed_actions || [])
        .concat(preset.allowed_views || [])
        .concat(preset.allowed_reports || [])
        .concat(preset.allowed_dashboards || []));
      document.querySelectorAll(".access-domain").forEach(input => input.checked = domains.has(input.value));
      document.querySelectorAll(".access-capability").forEach(input => input.checked = grants.has(input.value));
      generateAccessPolicy();
      setStatus("Authorization preset applied", "ok");
    }

    function recommendAccessAuthorization() {
      const purpose = $("accessAgentPurpose").value.trim();
      if (!purpose || !(state.accessPresets || []).length) {
        $("accessRecommendation").innerHTML = "<p class='muted'>No authorization recommendation. Load presets and describe the agent purpose first.</p>";
        applyI18n($("accessRecommendation"));
        setStatus("No authorization recommendation. Load presets and describe the agent purpose first.", "err");
        return;
      }
      const tokens = accessPurposeTokens(purpose);
      const rows = (state.accessPresets || []).map(preset => {
        const haystack = [
          preset.id,
          preset.title,
          preset.description,
          ...(preset.allowed_domains || []),
          ...(preset.allowed_actions || []),
          ...(preset.allowed_views || []),
          ...(preset.allowed_reports || []),
          ...(preset.allowed_dashboards || [])
        ].join(" ").toLowerCase();
        const matched = tokens.filter(token => haystack.includes(token));
        const domainBoost = (preset.allowed_domains || []).filter(domain => tokens.includes(String(domain).toLowerCase())).length * 3;
        const actionBoost = (preset.allowed_actions || []).filter(id => tokens.some(token => String(id).toLowerCase().includes(token))).length;
        const score = matched.length + domainBoost + actionBoost;
        return { preset, score, matched };
      }).filter(item => item.score > 0).sort((a, b) => b.score - a.score).slice(0, 5);
      renderAccessRecommendation(rows, tokens);
      setRaw({ purpose, recommendation: rows.map(item => ({ preset_id: item.preset.id, score: item.score, matched: item.matched })) });
      setStatus("Authorization recommendation generated", "ok");
    }

    function accessPurposeTokens(text) {
      const synonyms = {
        finance: ["finance", "financial", "invoice", "expense", "payment", "budget", "voucher", "cashflow", "ar", "ap"],
        sales: ["sales", "crm", "customer", "order", "opportunity", "quote", "pipeline", "account"],
        hr: ["hr", "human", "employee", "payroll", "leave", "people", "recruiting"],
        legal: ["legal", "contract", "compliance", "policy", "case", "matter"],
        inventory: ["inventory", "stock", "warehouse", "sku", "fulfillment"],
        procurement: ["procurement", "purchase", "supplier", "vendor", "sourcing"],
        assets: ["asset", "fixed", "equipment", "depreciation"],
        audit: ["audit", "auditor", "readonly", "read-only", "review", "governance", "risk"]
      };
      const normalized = String(text || "").toLowerCase();
      const raw = normalized.split(/[^a-z0-9_\u4e00-\u9fa5-]+/).filter(Boolean);
      const out = new Set(raw);
      Object.keys(synonyms).forEach(key => {
        if (synonyms[key].some(word => normalized.includes(word))) out.add(key);
      });
      return Array.from(out).filter(token => token.length > 1);
    }
    function renderAccessRecommendation(rows, tokens) {
      const root = $("accessRecommendation");
      if (!rows.length) {
        root.innerHTML = "<p class='muted'>No authorization recommendation. Load presets and describe the agent purpose first.</p>";
        applyI18n(root);
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Preset</th><th>Score</th><th>Matched</th><th>Setup</th><th>Scope</th><th>Action</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(item => {
        const preset = item.preset || {};
        const setup = recommendedAccessSetup(preset, $("accessAgentPurpose").value.trim());
        const scope = []
          .concat((preset.allowed_domains || []).map(v => "domain:" + v))
          .concat((preset.allowed_actions || []).map(v => "action:" + v))
          .concat((preset.allowed_reports || []).map(v => "report:" + v))
          .concat((preset.allowed_views || []).map(v => "view:" + v))
          .concat((preset.allowed_dashboards || []).map(v => "dashboard:" + v));
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td><button class='small'>Use preset</button></td>";
        tr.children[0].textContent = (preset.title || preset.id || "") + " (" + (preset.id || "") + ")";
        tr.children[1].textContent = String(item.score || 0);
        tr.children[2].textContent = (item.matched || tokens || []).slice(0, 8).join(", ");
        tr.children[3].textContent = setup.key_id + " / " + setup.user_id + " / " + setup.expires_at;
        tr.children[4].textContent = scope.slice(0, 8).join(", ") + (scope.length > 8 ? " ..." : "");
        tr.querySelector("button").onclick = () => {
          $("accessPreset").value = preset.id || "";
          $("accessKeyId").value = setup.key_id;
          $("accessUserId").value = setup.user_id;
          $("accessExpiresAt").value = setup.expires_at;
          applyAccessPreset();
          compareAccessPolicyChanges();
        };
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function recommendedAccessSetup(preset, purpose) {
      const base = slugifyAccessID(preset.id || purpose || "agent");
      const suffix = slugifyAccessID(purpose || "agent").split("-").slice(0, 3).join("-");
      const keyID = (base + (suffix && !base.includes(suffix) ? "-" + suffix : "")).replace(/-+/g, "-").replace(/^-|-$/g, "").slice(0, 64) || "agent-key";
      const expires = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000);
      return {
        key_id: keyID,
        user_id: ("agent_" + keyID.replace(/-/g, "_")).slice(0, 72),
        expires_at: expires.toISOString().slice(0, 10)
      };
    }

    function slugifyAccessID(value) {
      return String(value || "")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "")
        .replace(/-{2,}/g, "-");
    }

    function generateAccessPolicy() {
      const domains = Array.from(document.querySelectorAll(".access-domain:checked")).map(item => item.value);
      const selected = Array.from(document.querySelectorAll(".access-capability:checked"));
      const actions = selected.filter(item => item.dataset.kind === "action").map(item => item.value);
      const views = selected.filter(item => item.dataset.kind === "view").map(item => item.value);
      const reports = selected.filter(item => item.dataset.kind === "report").map(item => item.value);
      const dashboards = selected.filter(item => item.dataset.kind === "dashboard").map(item => item.value);
      const policy = {
        id: $("accessKeyId").value.trim() || "agent-key",
        key: "REPLACE_WITH_GENERATED_SECRET",
        tenant_id: $("tenant").value.trim() || "default",
        user_id: $("accessUserId").value.trim() || "agent",
        role: $("accessRole").value || "data_user",
        expires_at: $("accessExpiresAt").value.trim(),
        allowed_datasets: [],
        allowed_actions: Array.from(new Set(actions)).sort(),
        allowed_views: $("accessAllowReports").checked ? Array.from(new Set(views)).sort() : [],
        allowed_reports: $("accessAllowReports").checked ? Array.from(new Set(reports)).sort() : [],
        allowed_dashboards: $("accessAllowReports").checked ? Array.from(new Set(dashboards)).sort() : [],
        allowed_domains: Array.from(new Set(domains)).sort(),
        allow_raw_data: $("accessAllowRawData").checked,
        allow_sensitive: $("accessAllowSensitive").checked,
        allow_admin: $("accessAllowAdmin").checked
      };
      if (!$("accessAllowReports").checked && policy.allowed_actions.length > 0) {
        policy.allowed_domains = [];
      }
      $("accessPolicyJson").value = JSON.stringify(policy, null, 2);
      setRaw({ MACLAW_DATA_API_KEYS_entry: policy, note: "Append this object to the MACLAW_DATA_API_KEYS JSON array and restart the service." });
      setStatus("Generated scoped API key policy", "ok");
      return policy;
    }

    async function createManagedAccessKey() {
      try {
        const policy = generateAccessPolicy();
        if (!policy.id || policy.id === "agent-key") throw new Error("API key ID is required for managed keys");
        delete policy.key;
        const out = await api("/api/v1/data/access/api-keys", { method: "POST", body: JSON.stringify(policy) });
        $("accessKeySecret").classList.remove("hide");
        $("accessKeySecret").textContent = "Created key " + out.policy.id + ". Secret is shown once: " + out.key;
        state.lastAccessKeySecret = out.key || "";
        renderAgentHandoff(out.policy, state.lastAccessKeySecret);
        setRaw(out);
        await loadManagedAccessKeys(false);
        setStatus("Managed API key created", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function updateManagedAccessKey() {
      try {
        const policy = generateAccessPolicy();
        if (!policy.id || policy.id === "agent-key") throw new Error("API key ID is required for managed keys");
        delete policy.key;
        const out = await api("/api/v1/data/access/api-keys/" + encodeURIComponent(policy.id), { method: "PATCH", body: JSON.stringify(policy) });
        state.lastAccessKeySecret = "";
        renderAgentHandoff(out, "");
        setRaw(out);
        await loadManagedAccessKeys(false);
        setStatus("Managed API key updated", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    function currentAccessPolicyFromForm() {
      try {
        return JSON.parse($("accessPolicyJson").value || "{}");
      } catch (err) {
        return generateAccessPolicy();
      }
    }

    function normalizePolicyList(policy, key) {
      return Array.isArray(policy && policy[key]) ? policy[key] : [];
    }

    function comparableAccessPolicy(policy) {
      policy = policy || {};
      const out = {
        user_id: policy.user_id || "",
        role: policy.role || "data_user",
        expires_at: policy.expires_at || "",
        allowed_domains: normalizePolicyList(policy, "allowed_domains").slice().sort(),
        allowed_actions: normalizePolicyList(policy, "allowed_actions").slice().sort(),
        allowed_views: normalizePolicyList(policy, "allowed_views").slice().sort(),
        allowed_reports: normalizePolicyList(policy, "allowed_reports").slice().sort(),
        allowed_dashboards: normalizePolicyList(policy, "allowed_dashboards").slice().sort(),
        allowed_datasets: normalizePolicyList(policy, "allowed_datasets").slice().sort(),
        allow_raw_data: !!policy.allow_raw_data,
        allow_sensitive: !!policy.allow_sensitive,
        allow_admin: !!policy.allow_admin,
        enabled: policy.enabled !== false
      };
      return out;
    }

    function compareArrays(before, after) {
      before = Array.isArray(before) ? before : [];
      after = Array.isArray(after) ? after : [];
      return {
        added: after.filter(item => !before.includes(item)),
        removed: before.filter(item => !after.includes(item))
      };
    }

    function compareAccessPolicyChanges() {
      if (!state.loadedAccessPolicy) {
        $("accessPolicyDiff").innerHTML = "<p class='muted'>Load an existing managed key before comparing policy changes.</p>";
        applyI18n($("accessPolicyDiff"));
        setStatus("Load an existing managed key before comparing policy changes.", "err");
        return;
      }
      const before = comparableAccessPolicy(state.loadedAccessPolicy);
      const after = comparableAccessPolicy(generateAccessPolicy());
      const rows = [];
      const scalarKeys = ["user_id", "role", "expires_at", "allow_raw_data", "allow_sensitive", "allow_admin", "enabled"];
      scalarKeys.forEach(key => {
        if (String(before[key]) !== String(after[key])) {
          rows.push({ field: key, change: "changed", before: String(before[key]), after: String(after[key]) });
        }
      });
      ["allowed_domains", "allowed_actions", "allowed_views", "allowed_reports", "allowed_dashboards", "allowed_datasets"].forEach(key => {
        const diff = compareArrays(before[key], after[key]);
        if (diff.added.length) rows.push({ field: key, change: "added", before: "", after: diff.added.join(", ") });
        if (diff.removed.length) rows.push({ field: key, change: "removed", before: diff.removed.join(", "), after: "" });
      });
      renderAccessPolicyDiff(rows);
      const risks = accessPolicyLocalRisks(after);
      renderAccessPolicyRisk(risks);
      setRaw({ key_id: $("accessKeyId").value.trim(), policy_diff: rows, local_risks: risks });
      setStatus(rows.length ? "Access policy changes compared" : "No policy changes.", "ok");
    }

    function renderAccessPolicyDiff(rows) {
      const root = $("accessPolicyDiff");
      if (!rows.length) {
        root.innerHTML = "<p class='muted'>No policy changes.</p>";
        applyI18n(root);
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Field</th><th>Change</th><th>Before</th><th>After</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.field || "";
        tr.children[1].textContent = item.change || "";
        tr.children[2].textContent = item.before || "";
        tr.children[3].textContent = item.after || "";
        if (item.change === "removed") tr.children[1].style.color = "var(--danger)";
        if (item.change === "added") tr.children[1].style.color = "var(--ok)";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function accessPolicyLocalRisks(policy) {
      const risks = [];
      const add = (severity, code, reason, recommendation) => risks.push({ severity, code, reason, recommendation });
      if (!policy.expires_at) add("medium", "no_expiration", "Key has no expiration time.", "Set expires_at for agent and employee access.");
      if (policy.allow_admin) add("high", "allow_admin", "Key can perform administrative operations.", "Keep only for break-glass or schema administration agents.");
      if (policy.allow_sensitive) add("high", "allow_sensitive", "Key can access sensitive fields.", "Limit to trusted HR, finance, legal, or audit agents.");
      if (policy.allow_raw_data) add("medium", "allow_raw_data", "Key can use raw dataset APIs.", "Prefer business actions, views, reports, and dashboards.");
      if ((policy.allowed_domains || []).length > 0) add("medium", "domain_scope", "Key grants whole business domains.", "Prefer explicit business capabilities for narrow agents.");
      if ((policy.allowed_datasets || []).length > 0) add("medium", "raw_dataset_scope", "Key grants raw datasets.", "Use curated business views unless raw access is required.");
      if ((policy.allowed_actions || []).length === 0 && (policy.allowed_views || []).length === 0 && (policy.allowed_reports || []).length === 0 && (policy.allowed_dashboards || []).length === 0 && (policy.allowed_domains || []).length === 0 && (policy.allowed_datasets || []).length === 0) {
        add("info", "empty_scope", "Key has no scoped business resources.", "Add at least one action, view, report, dashboard, domain, or dataset.");
      }
      return risks;
    }

    function renderAccessPolicyRisk(risks) {
      const root = $("accessPolicyRisk");
      if (!risks.length) {
        root.innerHTML = "<p class='muted'>No local policy risks detected.</p>";
        applyI18n(root);
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Severity</th><th>Code</th><th>Reason</th><th>Recommended</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      risks.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.severity || "";
        tr.children[1].textContent = item.code || "";
        tr.children[2].textContent = item.reason || "";
        tr.children[3].textContent = item.recommendation || "";
        if (item.severity === "high") tr.children[0].style.color = "var(--danger)";
        if (item.severity === "medium") tr.children[0].style.color = "#9a6700";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function renderAgentHandoff(policy, secret) {
      policy = policy || currentAccessPolicyFromForm();
      const endpoint = $("endpoint").value.trim() || location.origin;
      const tenant = policy.tenant_id || $("tenant").value.trim() || "default";
      const user = policy.user_id || $("accessUserId").value.trim() || "agent";
      const keyID = policy.id || $("accessKeyId").value.trim() || "agent-key";
      const role = policy.role || $("accessRole").value || "data_user";
      const actions = normalizePolicyList(policy, "allowed_actions");
      const views = normalizePolicyList(policy, "allowed_views");
      const reports = normalizePolicyList(policy, "allowed_reports");
      const dashboards = normalizePolicyList(policy, "allowed_dashboards");
      const domains = normalizePolicyList(policy, "allowed_domains");
      const datasets = normalizePolicyList(policy, "allowed_datasets");
      const lines = [
        "# MaClawDataSrv Agent Handoff",
        "",
        "Endpoint: " + endpoint,
        "Tenant: " + tenant,
        "API key ID: " + keyID,
        "User / agent: " + user,
        "Role: " + role,
        "Secret: " + (secret || "[not shown; create or rotate the key to show it once]"),
        "Expires at: " + (policy.expires_at || "not set"),
        "",
        "Recommended agent rule:",
        "- Prefer business actions for writes.",
        "- Prefer dashboards, business views, reports, and aggregate APIs for analysis.",
        "- Do not use raw dataset APIs unless allow_raw_data is explicitly true.",
        "- Do not change schema unless the user/admin asks for schema governance.",
        "",
        "Allowed domains: " + (domains.length ? domains.join(", ") : "none"),
        "Allowed actions: " + (actions.length ? actions.join(", ") : "none"),
        "Allowed views: " + (views.length ? views.join(", ") : "none"),
        "Allowed reports: " + (reports.length ? reports.join(", ") : "none"),
        "Allowed dashboards: " + (dashboards.length ? dashboards.join(", ") : "none"),
        "Allowed raw datasets: " + (datasets.length ? datasets.join(", ") : "none"),
        "Raw dataset API: " + (policy.allow_raw_data ? "allowed" : "disabled"),
        "Sensitive fields: " + (policy.allow_sensitive ? "allowed" : "masked/denied"),
        "Admin operations: " + (policy.allow_admin ? "allowed" : "disabled"),
        "",
        "REST header example:",
        "Authorization: Bearer " + (secret || "[API_KEY_SECRET]"),
        "X-MaClaw-Tenant-ID: " + tenant,
        "",
        "Quick verification:",
        "GET " + endpoint.replace(/\/+$/, "") + "/api/v1/data/capabilities",
        "POST " + endpoint.replace(/\/+$/, "") + "/api/v1/data/intent/resolve"
      ];
      $("accessAgentHandoff").value = lines.join("\n");
      return $("accessAgentHandoff").value;
    }

    async function copyAgentHandoff() {
      const text = $("accessAgentHandoff").value || renderAgentHandoff(currentAccessPolicyFromForm(), state.lastAccessKeySecret);
      try {
        await navigator.clipboard.writeText(text);
        setStatus("Agent handoff copied", "ok");
      } catch (err) {
        $("accessAgentHandoff").focus();
        $("accessAgentHandoff").select();
        setStatus("Agent handoff selected for copying", "ok");
      }
    }

    function generateAgentOnboardingChecklist() {
      const policy = currentAccessPolicyFromForm();
      const scopedCount = normalizePolicyList(policy, "allowed_actions").length +
        normalizePolicyList(policy, "allowed_views").length +
        normalizePolicyList(policy, "allowed_reports").length +
        normalizePolicyList(policy, "allowed_dashboards").length +
        normalizePolicyList(policy, "allowed_domains").length +
        normalizePolicyList(policy, "allowed_datasets").length;
      const rows = [
        {
          step: "Purpose captured",
          done: !!$("accessAgentPurpose").value.trim(),
          detail: $("accessAgentPurpose").value.trim() || "Describe why this agent needs MIS access."
        },
        {
          step: "Scoped policy prepared",
          done: scopedCount > 0,
          detail: scopedCount + " scoped business resources selected."
        },
        {
          step: "Expiration set",
          done: !!policy.expires_at,
          detail: policy.expires_at || "Set an expiration for agent and employee access."
        },
        {
          step: "High-risk exceptions reviewed",
          done: !policy.allow_admin && !policy.allow_sensitive && !policy.allow_raw_data,
          detail: ["admin=" + !!policy.allow_admin, "sensitive=" + !!policy.allow_sensitive, "raw=" + !!policy.allow_raw_data].join(", ")
        },
        {
          step: "Policy diff/risk reviewed",
          done: !!$("accessPolicyDiff").textContent.trim() || !!$("accessPolicyRisk").textContent.trim(),
          detail: "Run Compare policy changes before updating an existing key."
        },
        {
          step: "Managed key created or loaded",
          done: !!$("accessKeyId").value.trim() && $("accessKeyId").value.trim() !== "agent-key",
          detail: $("accessKeyId").value.trim() || "Create or load a managed key."
        },
        {
          step: "Readiness check run",
          done: !!$("agentReadinessResult").textContent.trim(),
          detail: "Run agent readiness to verify effective scopes."
        },
        {
          step: "Handoff generated",
          done: !!$("accessAgentHandoff").value.trim(),
          detail: "Generate and share the handoff after key creation or rotation."
        }
      ];
      renderAgentOnboardingChecklist(rows);
      setRaw({ agent_onboarding_checklist: rows });
      setStatus("Agent onboarding checklist generated", "ok");
    }

    function renderAgentOnboardingChecklist(rows) {
      const root = $("agentOnboardingChecklist");
      const table = document.createElement("table");
      table.innerHTML = "<caption>Agent onboarding checklist</caption><thead><tr><th>Done</th><th>Step</th><th>Detail</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td>";
        tr.children[0].textContent = item.done ? "yes" : "no";
        tr.children[0].style.color = item.done ? "var(--ok)" : "var(--danger)";
        tr.children[1].textContent = item.step || "";
        tr.children[2].textContent = item.detail || "";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function textRowsFromTable(id) {
      const root = $(id);
      if (!root) return [];
      return Array.from(root.querySelectorAll("tbody tr")).map(tr => Array.from(tr.children).map(cell => cell.textContent.trim()));
    }

    function generateAgentOnboardingPacket() {
      const policy = currentAccessPolicyFromForm();
      const handoff = $("accessAgentHandoff").value.trim() || renderAgentHandoff(policy, state.lastAccessKeySecret);
      if (!$("agentOnboardingChecklist").textContent.trim()) generateAgentOnboardingChecklist();
      const checklist = textRowsFromTable("agentOnboardingChecklist").map(row => ({ done: row[0], step: row[1], detail: row[2] }));
      const readiness = textRowsFromTable("agentReadinessResult").map(row => ({ type: row[0], resource: row[1], allowed: row[2], reasons: row[3] }));
      const diff = textRowsFromTable("accessPolicyDiff").map(row => ({ field: row[0], change: row[1], before: row[2], after: row[3] }));
      const risks = textRowsFromTable("accessPolicyRisk").map(row => ({ severity: row[0], code: row[1], reason: row[2], recommended: row[3] }));
      const packet = {
        generated_at: new Date().toISOString(),
        purpose: $("accessAgentPurpose").value.trim(),
        key_id: policy.id || $("accessKeyId").value.trim(),
        tenant_id: policy.tenant_id || $("tenant").value.trim() || "default",
        user_id: policy.user_id || $("accessUserId").value.trim(),
        role: policy.role || $("accessRole").value,
        operation_summary: agentOnboardingOperationSummary(policy, checklist, readiness, risks),
        recommended_next_steps: agentOnboardingNextSteps(policy, checklist, readiness, risks),
        policy,
        local_risks: risks,
        readiness_checks: readiness,
        onboarding_checklist: checklist,
        handoff
      };
      $("agentOnboardingPacket").value = JSON.stringify(packet, null, 2);
      setRaw({ agent_onboarding_packet: packet });
      setStatus("Agent onboarding packet generated", "ok");
      return packet;
    }

    function agentOnboardingOperationSummary(policy, checklist, readiness, risks) {
      return {
        scoped_resources:
          normalizePolicyList(policy, "allowed_actions").length +
          normalizePolicyList(policy, "allowed_views").length +
          normalizePolicyList(policy, "allowed_reports").length +
          normalizePolicyList(policy, "allowed_dashboards").length +
          normalizePolicyList(policy, "allowed_domains").length +
          normalizePolicyList(policy, "allowed_datasets").length,
        high_risk_exceptions: ["allow_admin", "allow_sensitive", "allow_raw_data"].filter(key => !!policy[key]),
        checklist_done: checklist.filter(item => item.done === "yes").length,
        checklist_total: checklist.length,
        readiness_allowed: readiness.filter(item => item.allowed === "yes").length,
        readiness_total: readiness.length,
        local_risk_count: risks.length
      };
    }

    function agentOnboardingNextSteps(policy, checklist, readiness, risks) {
      const steps = [];
      if (!policy.expires_at) steps.push("Set an expiration date before production use.");
      if (risks.some(item => item.severity === "high")) steps.push("Review high-risk admin or sensitive scopes with a data admin.");
      if (!readiness.length) steps.push("Run agent readiness before handing the key to MaClaw or an external agent.");
      if (readiness.some(item => item.allowed === "no")) steps.push("Fix denied readiness checks before production use.");
      if (!$("accessAgentHandoff").value.trim()) steps.push("Generate the handoff after creating or rotating the managed key.");
      if (!steps.length) steps.push("Store this packet with the change record and rotate the key on schedule.");
      return steps;
    }

    async function copyAgentPacket() {
      const text = $("agentOnboardingPacket").value || JSON.stringify(generateAgentOnboardingPacket(), null, 2);
      try {
        await navigator.clipboard.writeText(text);
        setStatus("Agent onboarding packet copied", "ok");
      } catch (err) {
        $("agentOnboardingPacket").focus();
        $("agentOnboardingPacket").select();
        setStatus("Agent onboarding packet selected for copying", "ok");
      }
    }

    function downloadAgentPacket() {
      const text = $("agentOnboardingPacket").value || JSON.stringify(generateAgentOnboardingPacket(), null, 2);
      const keyID = $("accessKeyId").value.trim() || "agent-key";
      const blob = new Blob([text], { type: "application/json;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "maclaw-datasrv-agent-onboarding-" + keyID.replace(/[\\/:*?"<>|]+/g, "_") + ".json";
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      setStatus("Agent onboarding packet downloaded", "ok");
    }

    async function runAgentReadinessCheck() {
      const policy = currentAccessPolicyFromForm();
      const keyID = policy.id || $("accessKeyId").value.trim();
      if (!keyID || keyID === "agent-key") {
        setStatus("API key ID is required for access check", "err");
        return;
      }
      const checks = []
        .concat(normalizePolicyList(policy, "allowed_actions").slice(0, 3).map(id => ({ resource_type: "business_action", resource_id: id })))
        .concat(normalizePolicyList(policy, "allowed_views").slice(0, 2).map(id => ({ resource_type: "business_view", resource_id: id })))
        .concat(normalizePolicyList(policy, "allowed_reports").slice(0, 2).map(id => ({ resource_type: "report", resource_id: id })))
        .concat(normalizePolicyList(policy, "allowed_dashboards").slice(0, 2).map(id => ({ resource_type: "dashboard", resource_id: id })))
        .concat(normalizePolicyList(policy, "allowed_datasets").slice(0, 1).map(id => ({ resource_type: "dataset", resource_id: id })));
      if (policy.allow_admin) checks.push({ resource_type: "admin", resource_id: "access.review" });
      if (policy.allow_sensitive) checks.push({ resource_type: "sensitive", resource_id: "sensitive_fields" });
      if (!checks.length) {
        $("agentReadinessResult").innerHTML = "<p class='muted'>No scoped resources selected for readiness check.</p>";
        applyI18n($("agentReadinessResult"));
        setStatus("No scoped resources selected for readiness check.", "err");
        return;
      }
      try {
        const rows = [];
        for (const check of checks) {
          const out = await api("/api/v1/data/access/check", {
            method: "POST",
            body: JSON.stringify({ key_id: keyID, resource_type: check.resource_type, resource_id: check.resource_id })
          });
          rows.push(out);
        }
        renderAgentReadinessResult(rows);
        setRaw({ key_id: keyID, readiness_checks: rows });
        setStatus("Agent readiness checked", rows.every(item => item.allowed) ? "ok" : "err");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    function renderAgentReadinessResult(rows) {
      const root = $("agentReadinessResult");
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Type</th><th>Resource</th><th>Allowed</th><th>Reasons</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.resource_type || "";
        tr.children[1].textContent = item.resource_id || "";
        tr.children[2].textContent = item.allowed ? "yes" : "no";
        tr.children[2].style.color = item.allowed ? "var(--ok)" : "var(--danger)";
        tr.children[3].textContent = (item.reasons || []).join("; ");
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function loadAdminAccounts(showStatus = true) {
      try {
        const out = await api("/api/v1/data/admin/accounts");
        state.adminAccounts = Array.isArray(out?.items) ? out.items : [];
        renderAdminAccounts();
        if (showStatus) setStatus("Administrator accounts loaded", "ok");
      } catch (err) {
        $("adminAccounts").innerHTML = "<p class='muted'>Administrator account management requires data_admin with allow_admin.</p>";
        if (showStatus) setStatus(err.message, "err");
      }
    }

    function renderAdminAccounts() {
      const root = $("adminAccounts");
      if (!state.adminAccounts.length) {
        root.innerHTML = "<p class='muted'>No administrator accounts loaded.</p>";
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Username</th><th>Display name</th><th>Role</th><th>Enabled</th><th>Last login</th><th>Updated</th><th></th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      state.adminAccounts.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td><button data-action='load'>Load</button> <button data-action='toggle'></button></td>";
        tr.children[0].textContent = item.username || "";
        tr.children[1].textContent = item.display_name || "";
        tr.children[2].textContent = item.role || "";
        tr.children[3].textContent = item.enabled ? "yes" : "no";
        tr.children[4].textContent = item.last_login_at ? (new Date(item.last_login_at)).toLocaleString() : "never";
        tr.children[5].textContent = item.updated_at ? (new Date(item.updated_at)).toLocaleString() : "";
        tr.querySelector("button[data-action='load']").onclick = () => loadAdminAccountIntoForm(item);
        const toggle = tr.querySelector("button[data-action='toggle']");
        toggle.textContent = item.enabled ? "Disable" : "Enable";
        toggle.onclick = () => setAdminAccountEnabled(item.username, !item.enabled);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function loadAdminAccountIntoForm(item) {
      $("adminAccountUsername").value = item.username || "";
      $("adminAccountDisplayName").value = item.display_name || "";
      $("adminAccountRole").value = item.role || "data_admin";
      $("adminAccountPassword").value = "";
      setStatus("Administrator account loaded into form", "ok");
    }

    async function createAdminAccount() {
      const username = $("adminAccountUsername").value.trim();
      const password = $("adminAccountPassword").value;
      if (!username || !password) {
        setStatus("Admin username and temporary password are required", "err");
        return;
      }
      try {
        const out = await api("/api/v1/data/admin/accounts", {
          method: "POST",
          body: JSON.stringify({
            username,
            password,
            display_name: $("adminAccountDisplayName").value.trim(),
            role: $("adminAccountRole").value
          })
        });
        $("adminAccountPassword").value = "";
        setRaw(out);
        await loadAdminAccounts(false);
        setStatus("Administrator account created", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function updateAdminAccount() {
      const username = $("adminAccountUsername").value.trim();
      if (!username) {
        setStatus("Admin username is required", "err");
        return;
      }
      try {
        const out = await api("/api/v1/data/admin/accounts/" + encodeURIComponent(username), {
          method: "PATCH",
          body: JSON.stringify({
            display_name: $("adminAccountDisplayName").value.trim(),
            role: $("adminAccountRole").value
          })
        });
        setRaw(out);
        await loadAdminAccounts(false);
        setStatus("Administrator account updated", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function setAdminAccountEnabled(username, enabled) {
      if (!username) return;
      if (!enabled && !confirm("Disable administrator " + username + "? Active sessions for this account will be revoked.")) return;
      try {
        const out = await api("/api/v1/data/admin/accounts/" + encodeURIComponent(username), {
          method: "PATCH",
          body: JSON.stringify({ enabled })
        });
        setRaw(out);
        await loadAdminAccounts(false);
        setStatus(enabled ? "Administrator account enabled" : "Administrator account disabled", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function loadAdminSessions(showStatus = true) {
      try {
        const out = await api("/api/v1/data/admin/sessions");
        state.adminSessions = Array.isArray(out?.items) ? out.items : [];
        renderAdminSessions();
        if (showStatus) setStatus("Administrator sessions loaded", "ok");
      } catch (err) {
        $("adminSessions").innerHTML = "<p class='muted'>Administrator session management requires data_admin with allow_admin.</p>";
        if (showStatus) setStatus(err.message, "err");
      }
    }

    function renderAdminSessions() {
      const root = $("adminSessions");
      if (!state.adminSessions.length) {
        root.innerHTML = "<p class='muted'>No active administrator sessions.</p>";
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Session</th><th>User</th><th>Role</th><th>Current</th><th>Created</th><th>Expires</th><th></th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      state.adminSessions.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td class='row-actions'><button data-action='shorten'>1h</button><button data-action='extend'>12h</button><button data-action='revoke'>Revoke</button></td>";
        tr.children[0].textContent = item.id || "";
        tr.children[1].textContent = item.username || item.user_id || "";
        tr.children[2].textContent = item.role || "";
        tr.children[3].textContent = item.current ? "yes" : "no";
        tr.children[4].textContent = item.created_at ? (new Date(item.created_at)).toLocaleString() : "";
        tr.children[5].textContent = item.expires_at ? (new Date(item.expires_at)).toLocaleString() : "";
        tr.querySelector("button[data-action='shorten']").onclick = () => updateAdminSessionTTL(item.id, 1);
        tr.querySelector("button[data-action='extend']").onclick = () => updateAdminSessionTTL(item.id, 12);
        tr.querySelector("button[data-action='revoke']").onclick = () => revokeAdminSession(item.id, item.current);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function updateAdminSessionTTL(sessionID, expiresHours) {
      if (!sessionID) return;
      try {
        const out = await api("/api/v1/data/admin/sessions/" + encodeURIComponent(sessionID), {
          method: "PATCH",
          body: JSON.stringify({ expires_hours: expiresHours })
        });
        setRaw(out);
        await loadAdminSessions(false);
        setStatus("Administrator session expiry updated", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function revokeAdminSession(sessionID, current) {
      if (!sessionID) return;
      const prompt = current ? "Revoke your current administrator session? You may need to sign in again." : "Revoke administrator session " + sessionID + "?";
      if (!confirm(prompt)) return;
      try {
        const out = await api("/api/v1/data/admin/sessions/" + encodeURIComponent(sessionID), { method: "DELETE" });
        setRaw(out);
        await loadAdminSessions(false);
        setStatus("Administrator session revoked", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function loadManagedAccessKeys(showStatus = true, loadMore = false) {
      try {
        const params = new URLSearchParams();
        const status = $("accessKeyStatus").value.trim();
        const q = $("accessKeySearch").value.trim();
        const limit = $("accessKeyLimit").value.trim();
        if (status) params.set("status", status);
        if (q) params.set("q", q);
        if (limit) params.set("limit", limit);
        loadMore = preparePageParams("accessKeyPageKey", params, loadMore === true);
        if (loadMore && state.accessKeyNextBefore) params.set("before", state.accessKeyNextBefore);
        if (loadMore && state.accessKeyNextBeforeID) params.set("before_id", state.accessKeyNextBeforeID);
        const path = "/api/v1/data/access/api-keys" + (params.toString() ? "?" + params.toString() : "");
        const keys = await api(path);
        const items = Array.isArray(keys) ? keys : (Array.isArray(keys?.items) ? keys.items : []);
        state.accessKeys = loadMore ? (state.accessKeys || []).concat(items) : items;
        state.accessKeyNextBefore = (keys && keys.next_before) || "";
        state.accessKeyNextBeforeID = (keys && keys.next_before_id) || "";
        state.accessKeyHasMore = !!(keys && keys.has_more && keys.next_before_id);
        renderManagedAccessKeys();
        updateAdminSummary();
        if (showStatus) setStatus("Managed API keys loaded", "ok");
      } catch (err) {
        $("accessKeys").innerHTML = "<p class='muted'>Managed key list requires data_admin with allow_admin.</p>";
        if (showStatus) setStatus(err.message, "err");
      }
    }

    function renderManagedAccessKeys() {
      const root = $("accessKeys");
      if (!state.accessKeys.length) {
        root.innerHTML = "<p class='muted'>No managed API keys.</p>";
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>User</th><th>Role</th><th>Status</th><th>Scopes</th><th>Prefix</th><th>Expires</th><th>Last used</th><th></th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      state.accessKeys.forEach(item => {
        const scopes = []
          .concat((item.allowed_actions || []).map(v => "action:" + v))
          .concat((item.allowed_views || []).map(v => "view:" + v))
          .concat((item.allowed_reports || []).map(v => "report:" + v))
          .concat((item.allowed_dashboards || []).map(v => "dashboard:" + v))
          .concat((item.allowed_datasets || []).map(v => "dataset:" + v))
          .concat(item.allow_raw_data ? ["raw_data"] : []);
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td><button data-action='load'>Load</button> <button data-action='preview'>Preview</button> <button data-action='rotate'>Rotate</button> <button data-action='disable'>Disable</button></td>";
        tr.children[0].textContent = item.id || "";
        tr.children[1].textContent = item.user_id || "";
        tr.children[2].textContent = item.role || "";
        tr.children[3].textContent = accessKeyStatusLabel(item);
        tr.children[4].textContent = scopes.slice(0, 8).join(", ") + (scopes.length > 8 ? " ..." : "");
        tr.children[5].textContent = item.key_prefix || "";
        tr.children[6].textContent = item.expires_at ? (new Date(item.expires_at)).toLocaleString() : "never";
        tr.children[7].textContent = item.last_used_at ? (new Date(item.last_used_at)).toLocaleString() + (item.last_used_ip ? " / " + item.last_used_ip : "") : "never";
        tr.querySelector("button[data-action='load']").onclick = () => loadManagedAccessKeyIntoForm(item.id);
        tr.querySelector("button[data-action='preview']").onclick = () => previewManagedAccessKey(item.id);
        tr.querySelector("button[data-action='rotate']").onclick = () => rotateManagedAccessKey(item.id);
        const disableButton = tr.querySelector("button[data-action='disable']");
        disableButton.disabled = !item.enabled;
        disableButton.onclick = () => disableManagedAccessKey(item.id);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.accessKeyHasMore && state.accessKeyNextBeforeID, () => loadManagedAccessKeys(false, true));
    }

    function accessKeyStatusLabel(item) {
      const status = item.status || (item.enabled ? "active" : "disabled");
      if (status === "expiring_soon") {
        return "expiring soon" + (item.expires_in_days ? " (" + item.expires_in_days + "d)" : "");
      }
      return status.replace("_", " ");
    }

    async function loadManagedAccessKeyIntoForm(keyID) {
      if (!keyID) return;
      try {
        const item = await api("/api/v1/data/access/api-keys/" + encodeURIComponent(keyID));
        $("accessKeyId").value = item.id || "";
        $("accessUserId").value = item.user_id || "";
        $("accessRole").value = item.role || "data_user";
        $("accessExpiresAt").value = item.expires_at || "";
        $("accessAllowRawData").checked = !!item.allow_raw_data;
        $("accessAllowSensitive").checked = !!item.allow_sensitive;
        $("accessAllowAdmin").checked = !!item.allow_admin;
        $("accessAllowReports").checked = true;
        const grant = new Set([]
          .concat(item.allowed_actions || [])
          .concat(item.allowed_views || [])
          .concat(item.allowed_reports || [])
          .concat(item.allowed_dashboards || []));
        document.querySelectorAll(".access-domain").forEach(input => input.checked = (item.allowed_domains || []).includes(input.value));
        document.querySelectorAll(".access-capability").forEach(input => input.checked = grant.has(input.value));
        $("accessPolicyJson").value = JSON.stringify({
          id: item.id,
          tenant_id: item.tenant_id,
          user_id: item.user_id,
          role: item.role,
          expires_at: item.expires_at || "",
          allowed_domains: item.allowed_domains || [],
          allowed_actions: item.allowed_actions || [],
          allowed_views: item.allowed_views || [],
          allowed_reports: item.allowed_reports || [],
          allowed_dashboards: item.allowed_dashboards || [],
          allowed_datasets: item.allowed_datasets || [],
          allow_raw_data: !!item.allow_raw_data,
          allow_sensitive: !!item.allow_sensitive,
          allow_admin: !!item.allow_admin,
          enabled: !!item.enabled,
          note: item.note || ""
        }, null, 2);
        state.loadedAccessPolicy = currentAccessPolicyFromForm();
        $("accessPolicyDiff").innerHTML = "";
        state.lastAccessKeySecret = "";
        renderAgentHandoff(item, "");
        setRaw(item);
        setStatus("Managed API key loaded into form", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function rotateManagedAccessKey(keyID) {
      if (!keyID || !confirm("Rotate API key " + keyID + "? The old secret will stop working.")) return;
      try {
        const out = await api("/api/v1/data/access/api-keys/" + encodeURIComponent(keyID) + "/rotate", { method: "POST", body: "{}" });
        $("accessKeySecret").classList.remove("hide");
        $("accessKeySecret").textContent = "Rotated key " + out.policy.id + ". New secret is shown once: " + out.key;
        state.lastAccessKeySecret = out.key || "";
        renderAgentHandoff(out.policy, state.lastAccessKeySecret);
        setRaw(out);
        await loadManagedAccessKeys(false);
        setStatus("Managed API key rotated", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function previewManagedAccessKey(keyID) {
      keyID = keyID || $("accessKeyId").value.trim();
      if (!keyID) {
        setStatus("API key ID is required for access preview", "err");
        return;
      }
      try {
        const out = await api("/api/v1/data/access/api-keys/" + encodeURIComponent(keyID) + "/capabilities");
        const summary = {
          api_key_id: out.api_key_id,
          domains: out.domains || [],
          business_actions: (out.business_actions || []).map(item => item.id),
          business_views: (out.business_views || []).map(item => item.id),
          reports: (out.reports || []).map(item => item.id),
          dashboards: (out.dashboards || []).map(item => item.id),
          datasets: (out.datasets || []).map(item => item.dataset && item.dataset.id).filter(Boolean)
        };
        setRaw({ summary, capabilities: out });
        renderAgentHandoff({ id: out.api_key_id, tenant_id: out.tenant_id, user_id: out.user_id, role: out.role }, "");
        setStatus("Managed API key access preview loaded", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function checkManagedAccessKey() {
      const keyID = $("accessKeyId").value.trim();
      const resourceType = $("accessCheckType").value.trim();
      const resourceID = $("accessCheckResource").value.trim();
      if (!keyID) {
        setStatus("API key ID is required for access check", "err");
        return;
      }
      try {
        const out = await api("/api/v1/data/access/check", {
          method: "POST",
          body: JSON.stringify({ key_id: keyID, resource_type: resourceType, resource_id: resourceID })
        });
        setRaw(out);
        setStatus(out.allowed ? "Access check allowed" : "Access check denied", out.allowed ? "ok" : "err");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function reviewManagedAccessKeys() {
      try {
        const params = new URLSearchParams();
        const minSeverity = $("accessReviewSeverity").value.trim();
        if (minSeverity) params.set("min_severity", minSeverity);
        const out = await api("/api/v1/data/access/review" + (params.toString() ? "?" + params.toString() : ""));
        state.accessReview = out || {};
        state.accessReviewUnavailable = "";
        updateAdminSummary();
        setRaw(out);
        renderAccessReview(out);
        setStatus("Access review loaded", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    function renderAccessReview(review) {
      const findings = review.findings || [];
      if (!findings.length) {
        $("accessKeys").innerHTML = "<p class='muted'>No access review findings for the current filter.</p>";
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<caption>Total keys: " + (review.total || 0) + " / findings: " + (review.filtered || findings.length) + "</caption><thead><tr><th>Severity</th><th>Key</th><th>User</th><th>Status</th><th>Findings</th><th>Recommended</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      findings.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.severity || "";
        tr.children[1].textContent = item.key_id || "";
        tr.children[2].textContent = item.user_id || "";
        tr.children[3].textContent = item.status || "";
        tr.children[4].textContent = (item.codes || []).join(", ");
        tr.children[5].textContent = item.recommended || "";
        body.appendChild(tr);
      });
      $("accessKeys").innerHTML = "";
      $("accessKeys").appendChild(table);
    }

    async function exportAccessReview() {
      try {
        if (!state.accessReview) {
          await reviewManagedAccessKeys();
        }
        const review = state.accessReview || {};
        const payload = {
          exported_at: new Date().toISOString(),
          min_severity: $("accessReviewSeverity").value.trim() || "",
          review
        };
        const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = "maclaw-datasrv-access-review-" + payload.exported_at.replace(/[\\/:*?"<>|.]+/g, "_") + ".json";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setRaw({ access_review_export: payload });
        setStatus("Access review exported", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function captureEvidenceSection(name, loader) {
      try {
        return { name, ok: true, data: await loader() };
      } catch (err) {
        return { name, ok: false, error: err.message || String(err) };
      }
    }

    async function exportGovernanceEvidencePack() {
      try {
        const payload = await loadGovernanceEvidencePack();
        const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        const exportedAt = payload.exported_at || new Date().toISOString();
        a.download = "maclaw-datasrv-governance-evidence-" + exportedAt.replace(/[\\/:*?"<>|.]+/g, "_") + ".json";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setRaw({ governance_evidence_pack: payload });
        renderGovernanceEvidenceSummary(payload);
        setStatus("Governance evidence pack exported", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function downloadGovernanceEvidenceSummary() {
      try {
        const pack = state.governanceEvidence || await loadGovernanceEvidencePack();
        const text = governanceEvidenceSummaryText(pack);
        const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = "maclaw-datasrv-governance-evidence-summary-" + (pack.evidence_id || currentLanguage()) + ".txt";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setStatus("Governance evidence summary downloaded", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    async function loadGovernanceEvidencePack() {
      const minSeverity = $("accessReviewSeverity").value.trim();
      const reviewParams = new URLSearchParams();
      if (minSeverity) reviewParams.set("min_severity", minSeverity);
      reviewParams.set("lang", currentLanguage());
      const payload = await api("/api/v1/data/governance/evidence-pack" + (reviewParams.toString() ? "?" + reviewParams.toString() : ""), { method: "GET" });
      state.governanceEvidence = payload;
      renderGovernanceEvidenceSummary(payload);
      return payload;
    }

    async function refreshGovernanceEvidenceSummary() {
      try {
        const payload = await loadGovernanceEvidencePack();
        setRaw({ governance_evidence_pack: payload });
        setStatus("Governance evidence summary refreshed", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    function handleLanguageChange() {
      saveSettings();
      applyI18n(document.body);
      if (activeModuleName() === "access" && state.governanceEvidence) {
        loadGovernanceEvidencePack().catch(err => setStatus(err.message, "err"));
      }
    }

    function renderGovernanceEvidenceSummary(pack) {
      const root = $("governanceEvidenceSummary");
      const summary = (pack && pack.summary) || {};
      const controls = summary.controls || [];
      const recommendations = summary.recommendations || [];
      if (!pack || !summary.section_count) {
        root.innerHTML = "";
        return;
      }
      root.innerHTML = "";
      const header = document.createElement("div");
      header.className = "row";
      header.style.justifyContent = "space-between";
      const title = document.createElement("h3");
      title.textContent = "Evidence summary";
      const copy = document.createElement("button");
      copy.className = "small";
      copy.textContent = "Copy summary";
      copy.dataset.testid = "copy-evidence-summary";
      copy.onclick = () => copyGovernanceEvidenceSummary(pack);
      header.appendChild(title);
      header.appendChild(copy);
      root.appendChild(header);
      const grid = document.createElement("div");
      grid.className = "evidence-summary-grid";
      [
        ["Status", summary.status || ""],
        ["Risk", summary.risk_level || ""],
        ["Evidence ID", pack.evidence_id || ""],
        ["Sections", (summary.ok_sections || 0) + "/" + (summary.section_count || 0)],
        ["Controls", (summary.control_failures || 0) + " fail / " + (summary.control_warnings || 0) + " warn"],
        ["Recommendations", recommendations.length]
      ].forEach(row => {
        const item = document.createElement("div");
        item.className = "evidence-summary-item";
        item.innerHTML = "<div class='summary-label'></div><div class='evidence-summary-value'></div>";
        item.children[0].textContent = row[0];
        item.children[1].textContent = String(row[1]);
        const value = item.children[1];
        if (row[0] === "Status") value.classList.add(summary.status === "ready" ? "ok" : "warn");
        if (row[0] === "Risk") value.classList.add(summary.risk_level === "low" ? "ok" : "warn");
        if (row[0] === "Controls" && ((summary.control_failures || 0) > 0 || (summary.control_warnings || 0) > 0)) value.classList.add((summary.control_failures || 0) > 0 ? "fail" : "warn");
        grid.appendChild(item);
      });
      root.appendChild(grid);
      if (pack.summary_text) {
        const preview = document.createElement("pre");
        preview.className = "summary-preview";
        preview.dataset.testid = "governance-evidence-summary-text";
        preview.textContent = pack.summary_text;
        root.appendChild(preview);
      }
      const table = document.createElement("table");
      table.innerHTML = "<caption>Governance controls</caption><thead><tr><th>Control</th><th>Status</th><th>Detail</th><th>Action</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      controls.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.title || item.id || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.detail || "";
        if (item.action_target) {
          const btn = document.createElement("button");
          btn.className = "small";
          btn.textContent = item.recommended_action || "Open";
          btn.onclick = () => switchTab(item.action_target);
          tr.children[3].appendChild(btn);
        } else {
          tr.children[3].textContent = item.recommended_action || "";
        }
        body.appendChild(tr);
      });
      root.appendChild(table);
      if (recommendations.length) {
        const rec = document.createElement("div");
        rec.className = "recommendation-list";
        const heading = document.createElement("h3");
        heading.textContent = "Recommendations";
        rec.appendChild(heading);
        recommendations.forEach(item => {
          const row = document.createElement("div");
          row.className = "recommendation-item";
          row.innerHTML = "<span></span>";
          row.children[0].textContent = item;
          rec.appendChild(row);
        });
        root.appendChild(rec);
      }
      applyI18n(root);
    }

    function governanceEvidenceSummaryText(pack) {
      if (pack && pack.summary_text) {
        return pack.summary_text;
      }
      const summary = (pack && pack.summary) || {};
      const controls = summary.controls || [];
      const recommendations = summary.recommendations || [];
      const lines = [
        "MaClawDataSrv governance evidence summary",
        "Tenant: " + ((pack && pack.tenant_id) || ""),
        "Exported at: " + ((pack && pack.exported_at) || ""),
        "Status: " + (summary.status || ""),
        "Risk level: " + (summary.risk_level || ""),
        "Sections: " + (summary.ok_sections || 0) + "/" + (summary.section_count || 0) + " ok",
        "Controls: " + (summary.control_failures || 0) + " fail / " + (summary.control_warnings || 0) + " warn"
      ];
      if (controls.length) {
        lines.push("", "Controls:");
        controls.forEach(item => lines.push("- " + (item.id || item.title || "") + ": " + (item.status || "") + (item.detail ? " (" + item.detail + ")" : "") + (item.recommended_action ? " -> " + item.recommended_action : "")));
      }
      if (recommendations.length) {
        lines.push("", "Recommendations:");
        recommendations.forEach(item => lines.push("- " + item));
      }
      return lines.join("\n");
    }

    async function copyGovernanceEvidenceSummary(pack) {
      const text = governanceEvidenceSummaryText(pack);
      try {
        await navigator.clipboard.writeText(text);
        setStatus("Governance evidence summary copied", "ok");
      } catch (err) {
        const root = $("governanceEvidenceSummary");
        const area = document.createElement("textarea");
        area.value = text;
        root.appendChild(area);
        area.focus();
        area.select();
        setStatus("Governance evidence summary selected for copying", "ok");
      }
    }

    async function planAccessRemediation() {
      try {
        const params = new URLSearchParams();
        const minSeverity = $("accessReviewSeverity").value.trim();
        if (minSeverity) params.set("min_severity", minSeverity);
        const out = await api("/api/v1/data/access/remediation-plan" + (params.toString() ? "?" + params.toString() : ""));
        setRaw(out);
        renderAccessRemediationPlan(out);
        setStatus("Access remediation plan loaded", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    function renderAccessRemediationPlan(plan) {
      const items = plan.items || [];
      if (!items.length) {
        $("accessKeys").innerHTML = "<p class='muted'>No remediation actions for the current filter.</p>";
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<caption>Suggested actions: " + (plan.total || items.length) + "</caption><thead><tr><th>Severity</th><th>Key</th><th>Action</th><th>Method</th><th>Endpoint</th><th>Reason</th><th>Destructive</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.severity || "";
        tr.children[1].textContent = item.key_id || "";
        tr.children[2].textContent = item.action || "";
        tr.children[3].textContent = item.method || "";
        tr.children[4].textContent = item.endpoint || "";
        tr.children[5].textContent = item.reason || "";
        tr.children[6].textContent = item.destructive ? "yes" : "no";
        body.appendChild(tr);
      });
      $("accessKeys").innerHTML = "";
      $("accessKeys").appendChild(table);
    }

    async function disableManagedAccessKey(keyID) {
      if (!keyID || !confirm("Disable API key " + keyID + "?")) return;
      try {
        const out = await api("/api/v1/data/access/api-keys/" + encodeURIComponent(keyID), { method: "DELETE" });
        setRaw(out);
        await loadManagedAccessKeys(false);
        setStatus("Managed API key disabled", "ok");
      } catch (err) {
        setStatus(err.message, "err");
      }
    }

    function loadRecordToEditor(item) {
      switchTab("write");
      $("recordId").value = item.id || "";
      $("recordTitle").value = item.title || "";
      $("recordTags").value = (item.tags || []).join(", ");
      $("recordData").value = JSON.stringify(item.data || {}, null, 2);
    }

    function clearRecordEditor() {
      $("recordId").value = "";
      $("recordTitle").value = "";
      $("recordTags").value = "";
      $("recordData").value = JSON.stringify({ customer: "Acme", amount: 8800 }, null, 2);
    }

    async function loadFields() {
      try {
        const dataset = requireDataset();
        const fields = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/fields", { method: "GET" });
        $("fieldsJson").value = JSON.stringify({ fields: Array.isArray(fields) ? fields : (Array.isArray(fields?.items) ? fields.items : []) }, null, 2);
        setStatus("Fields loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function loadSchemaProposals(showStatus, loadMore = false) {
      try {
        const dataset = requireDataset();
        const params = new URLSearchParams({ limit: "20" });
        params.set("dataset_id", dataset);
        loadMore = preparePageParams("schemaProposalPageKey", params, loadMore === true);
        params.delete("dataset_id");
        if (loadMore && state.schemaProposalNextBefore) params.set("before", state.schemaProposalNextBefore);
        if (loadMore && state.schemaProposalNextBeforeID) params.set("before_id", state.schemaProposalNextBeforeID);
        const proposals = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/schema-proposals?" + params.toString(), { method: "GET" });
        const items = Array.isArray(proposals) ? proposals : (Array.isArray(proposals?.items) ? proposals.items : []);
        state.schemaProposals = loadMore ? (state.schemaProposals || []).concat(items) : items;
        state.schemaProposalNextBefore = (proposals && proposals.next_before) || "";
        state.schemaProposalNextBeforeID = (proposals && proposals.next_before_id) || "";
        state.schemaProposalHasMore = !!(proposals && proposals.has_more && proposals.next_before_id);
        renderSchemaProposals();
        if (showStatus !== false) setStatus("Schema proposals loaded: " + state.schemaProposals.length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderSchemaProposals() {
      const root = $("schemaProposalList");
      if (!root) return;
      const items = state.schemaProposals || [];
      if (!items.length) {
        root.innerHTML = '<div class="empty">No schema proposals</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Status</th><th>ID</th><th>Suggested</th><th>Reason</th><th>Updated</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td><button class='small'>Load</button></td>";
        tr.children[0].textContent = item.status || "pending";
        tr.children[1].textContent = item.id || "";
        tr.children[2].textContent = String((item.suggested || []).length);
        tr.children[3].textContent = item.reason || "";
        tr.children[4].textContent = item.updated_at || item.created_at || "";
        tr.querySelector("button").onclick = () => loadSchemaProposalToEditor(item);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.schemaProposalHasMore && state.schemaProposalNextBeforeID, () => loadSchemaProposals(false, true));
    }

    function loadSchemaProposalToEditor(item) {
      $("schemaProposalJson").value = JSON.stringify({ proposal_id: item.id || "", fields: item.suggested || [], confirm: false, reason: item.reason || "web console schema proposal" }, null, 2);
      setStatus("Loaded schema proposal " + (item.id || ""), "ok");
    }

    async function proposeSchema() {
      try {
        const dataset = requireDataset();
        const sample = parseJSONField("schemaSampleJson", {});
        const proposal = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/schema-proposals", { method: "POST", body: JSON.stringify({ sample_data: sample, reason: "web console schema proposal" }) });
        $("schemaProposalJson").value = JSON.stringify({ proposal_id: proposal.id || "", fields: proposal.suggested || [], confirm: false, reason: proposal.reason || "web console schema proposal" }, null, 2);
        await loadSchemaProposals(false);
        setStatus("Schema proposal generated", "ok");
      } catch (err) { notifyError(err); }
    }

    async function applySchemaProposal() {
      try {
        const dataset = requireDataset();
        const body = parseJSONField("schemaProposalJson", { fields: [] });
        body.confirm = true;
        if (!confirm("Apply schema proposal to " + dataset + "?")) return;
        await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/schema-proposals/apply", { method: "POST", body: JSON.stringify(body) });
        await loadFields();
        await loadSchemaProposals(false);
        setStatus("Schema proposal applied", "ok");
      } catch (err) { notifyError(err); }
    }
    async function saveFields() {
      try {
        const dataset = requireDataset();
        const body = parseJSONField("fieldsJson", { fields: [] });
        await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/fields", { method: "PUT", body: JSON.stringify(body) });
        setStatus("Fields saved", "ok");
      } catch (err) { notifyError(err); }
    }

    async function batchImportRecords(dryRun) {
      try {
        const dataset = requireDataset();
        const records = parseJSONField("batchRecordsJson", []);
        if (!Array.isArray(records)) throw new Error("Batch records must be a JSON array");
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/batch", { method: "POST", body: JSON.stringify({ records, dry_run: !!dryRun }) });
        setStatus(dryRun ? (result.valid ? "Batch dry-run passed" : "Batch dry-run failed") : "Batch import complete: " + (result.imported || 0), result.valid ? "ok" : "err");
        if (!dryRun && result.valid) await queryRecords();
      } catch (err) { notifyError(err); }
    }

    async function bulkUpdateRecords(dryRun) {
      try {
        const dataset = requireDataset();
        const body = parseJSONField("bulkUpdateJson", {});
        body.dry_run = !!dryRun;
        body.confirm = !dryRun;
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/bulk-update", { method: "POST", body: JSON.stringify(body) });
        renderBulkUpdateResult(result || {});
        setStatus(dryRun ? (result.valid ? "Bulk update dry-run passed" : "Bulk update dry-run failed") : "Bulk update applied: " + (result.updated || 0), result.valid ? "ok" : "err");
        if (!dryRun && result.valid) await queryRecords();
      } catch (err) { notifyError(err); }
    }

    function renderBulkUpdateResult(result) {
      const root = $("bulkUpdateTable");
      const items = result.validations || [];
      if (!items.length) {
        root.innerHTML = '<div class="empty">No matched records</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>#</th><th>ID</th><th>Valid</th><th>Errors</th><th>Unknown fields</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = String(item.index || 0);
        tr.children[1].textContent = item.id || "";
        tr.children[2].textContent = String(!!item.valid);
        tr.children[3].textContent = (item.errors || []).join("; ");
        tr.children[4].textContent = (item.unknown_fields || []).join(", ");
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function bulkDeleteRecords(dryRun) {
      try {
        const dataset = requireDataset();
        const body = parseJSONField("bulkDeleteJson", {});
        body.dry_run = !!dryRun;
        body.confirm = !dryRun;
        if (!dryRun && !confirm("Apply bulk delete to matched records?")) return;
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/bulk-delete", { method: "POST", body: JSON.stringify(body) });
        renderBulkDeleteResult(result || {});
        setStatus(dryRun ? "Bulk delete dry-run matched: " + (result.total || 0) : "Bulk delete applied: " + (result.deleted || 0), "ok");
        if (!dryRun) await queryRecords();
      } catch (err) { notifyError(err); }
    }

    function renderBulkDeleteResult(result) {
      const root = $("bulkDeleteTable");
      const items = result.records || [];
      if (!items.length) {
        root.innerHTML = '<div class="empty">No matched records</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>Title</th><th>Data</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td>";
        tr.children[0].textContent = item.id || "";
        tr.children[1].textContent = item.title || "";
        tr.children[2].textContent = JSON.stringify(item.data || {});
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function startBatchImportJob() {
      try {
        const dataset = requireDataset();
        const records = parseJSONField("batchRecordsJson", []);
        if (!Array.isArray(records)) throw new Error("Batch records must be a JSON array");
        const job = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/batch/jobs", { method: "POST", body: JSON.stringify({ records, dry_run: false }) });
        setStatus("Batch import job queued: " + job.id, "ok");
        await listImportJobs(false);
      } catch (err) { notifyError(err); }
    }

    async function importRecordsCSV(dryRun) {
      try {
        const dataset = requireDataset();
        const csv = $("csvImportText").value;
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/import.csv", { method: "POST", body: JSON.stringify({ csv, dry_run: !!dryRun }) });
        setStatus(dryRun ? (result.valid ? "CSV dry-run passed" : "CSV dry-run failed") : "CSV import complete: " + (result.imported || 0), result.valid ? "ok" : "err");
        if (!dryRun && result.valid) await queryRecords();
      } catch (err) { notifyError(err); }
    }

    async function loadCsvTemplate() {
      try {
        const dataset = requireDataset();
        const csv = await apiText("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/import-template.csv", { method: "GET" });
        $("csvImportText").value = csv.trim();
        setStatus("CSV template loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function startCSVImportJob() {
      try {
        const dataset = requireDataset();
        const csv = $("csvImportText").value;
        const job = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/import.csv/jobs", { method: "POST", body: JSON.stringify({ csv, dry_run: false }) });
        setStatus("CSV import job queued: " + job.id, "ok");
        await listImportJobs(false);
      } catch (err) { notifyError(err); }
    }

    async function importRecordsJSONL(dryRun) {
      try {
        const dataset = requireDataset();
        const jsonl = $("jsonlImportText").value;
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/import.jsonl", { method: "POST", body: JSON.stringify({ jsonl, dry_run: !!dryRun }) });
        setStatus(dryRun ? (result.valid ? "JSONL dry-run passed" : "JSONL dry-run failed") : "JSONL import complete: " + (result.imported || 0), result.valid ? "ok" : "err");
        if (!dryRun && result.valid) await queryRecords();
      } catch (err) { notifyError(err); }
    }

    async function startJSONLImportJob() {
      try {
        const dataset = requireDataset();
        const jsonl = $("jsonlImportText").value;
        const job = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/import.jsonl/jobs", { method: "POST", body: JSON.stringify({ jsonl, dry_run: false }) });
        setStatus("JSONL import job queued: " + job.id, "ok");
        await listImportJobs(false);
      } catch (err) { notifyError(err); }
    }

    async function listImportJobs(showStatus, loadMore = false) {
      try {
        const dataset = requireDataset();
        const params = new URLSearchParams({ dataset_id: dataset, limit: "20" });
        loadMore = preparePageParams("importJobPageKey", params, loadMore === true);
        if (loadMore && state.importJobNextBefore) params.set("before", state.importJobNextBefore);
        if (loadMore && state.importJobNextBeforeID) params.set("before_id", state.importJobNextBeforeID);
        const data = await api("/api/v1/data/import-jobs?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : ((data && data.items) || []);
        state.importJobs = loadMore ? (state.importJobs || []).concat(items) : items;
        state.importJobNextBefore = (data && data.next_before) || "";
        state.importJobNextBeforeID = (data && data.next_before_id) || "";
        state.importJobHasMore = !!(data && data.has_more && data.next_before_id);
        renderImportJobs(state.importJobs);
        if (showStatus) setStatus("Import jobs loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderImportJobs(items) {
      const root = $("importJobTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No import jobs</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Created</th><th>Status</th><th>Kind</th><th>Total</th><th>Imported</th><th>Valid</th><th>Error</th><th>ID</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.kind || "";
        tr.children[3].textContent = String(item.total || 0);
        tr.children[4].textContent = String(item.imported || 0);
        tr.children[5].textContent = String(!!item.valid);
        tr.children[6].textContent = item.error || "";
        tr.children[7].textContent = item.id || "";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.importJobHasMore && state.importJobNextBeforeID, () => listImportJobs(false, true));
    }

    async function validateRecordEditor() {
      try {
        const dataset = requireDataset();
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/validate", { method: "POST", body: JSON.stringify({ data: parseJSONField("recordData", {}) }) });
        setStatus(result.valid ? "Record validation passed" : "Record validation failed", result.valid ? "ok" : "err");
      } catch (err) { notifyError(err); }
    }

    async function loadRecordRevisions(loadMore = false) {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        if (!id) throw new Error("Record ID is required");
        const params = new URLSearchParams();
        params.set("limit", "100");
        params.set("dataset_id", dataset);
        params.set("record_id", id);
        loadMore = preparePageParams("recordRevisionPageKey", params, loadMore === true);
        params.delete("dataset_id");
        params.delete("record_id");
        if (loadMore && state.recordRevisionNextBefore) params.set("before", state.recordRevisionNextBefore);
        if (loadMore && state.recordRevisionNextBeforeID) params.set("before_id", state.recordRevisionNextBeforeID);
        const data = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/" + encodeURIComponent(id) + "/revisions?" + params.toString(), { method: "GET" });
        const items = (data && data.items) || [];
        state.recordRevisions = loadMore ? (state.recordRevisions || []).concat(items) : items;
        state.recordRevisionNextBefore = (data && data.next_before) || "";
        state.recordRevisionNextBeforeID = (data && data.next_before_id) || "";
        state.recordRevisionHasMore = !!(data && data.has_more && data.next_before_id);
        renderRecordRevisions(state.recordRevisions);
        setStatus("Revisions loaded: " + (state.recordRevisions || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderRecordRevisions(items) {
      const root = $("revisionTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No revisions</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Time</th><th>Action</th><th>User</th><th>Title</th><th>Data</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.action || "";
        tr.children[2].textContent = item.created_by || "";
        tr.children[3].textContent = item.title || "";
        tr.children[4].textContent = JSON.stringify(item.data || {});
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      if (state.recordRevisionHasMore && state.recordRevisionNextBeforeID) {
        const more = document.createElement("button");
        more.className = "small";
        more.textContent = "Load more";
        more.onclick = () => loadRecordRevisions(true);
        root.appendChild(more);
      }
    }

    async function loadRelatedRecords(loadMore = false) {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        if (!id) throw new Error("Record ID is required");
        const params = new URLSearchParams();
        params.set("limit", "100");
        params.set("dataset_id", dataset);
        params.set("record_id", id);
        loadMore = preparePageParams("relatedRecordPageKey", params, loadMore === true);
        params.delete("dataset_id");
        params.delete("record_id");
        if (loadMore && state.relatedNextBeforeID) params.set("before_id", state.relatedNextBeforeID);
        const data = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/" + encodeURIComponent(id) + "/related?" + params.toString(), { method: "GET" });
        const links = (data && data.links) || [];
        state.relatedRecords = loadMore ? (state.relatedRecords || []).concat(links) : links;
        state.relatedNextBeforeID = (data && data.next_before_id) || "";
        state.relatedHasMore = !!(data && data.has_more && data.next_before_id);
        renderRelatedRecords(state.relatedRecords);
        setRaw(data || {});
        setStatus("Related records loaded: " + (state.relatedRecords || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderRelatedRecords(items) {
      const root = $("revisionTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No related records</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Direction</th><th>Relationship</th><th>Record</th><th>Title</th><th>Missing</th><th>Data</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const rel = item.relationship || {};
        const record = item.record || {};
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td><pre></pre></td><td></td>";
        tr.children[0].textContent = item.direction || "";
        tr.children[1].textContent = (rel.source_dataset_id || "") + "." + (rel.source_field || "") + " -> " + (rel.target_dataset_id || "");
        tr.children[2].textContent = record.dataset_id && record.id ? record.dataset_id + "/" + record.id : "";
        tr.children[3].textContent = record.title || "";
        tr.children[4].textContent = item.missing ? (item.message || "missing") : "";
        tr.children[5].querySelector("pre").textContent = record.data ? JSON.stringify(record.data) : "";
        const actionCell = tr.children[6];
        const inspect = document.createElement("button");
        inspect.className = "small";
        inspect.textContent = "Inspect";
        inspect.onclick = () => setRaw(item);
        actionCell.appendChild(inspect);
        if (record.dataset_id && record.id) {
          const open = document.createElement("button");
          open.className = "small primary";
          open.textContent = "Open";
          open.onclick = () => {
            state.selectedDataset = record.dataset_id;
            renderDatasets();
            loadRecordToEditor(record);
          };
          actionCell.appendChild(document.createTextNode(" "));
          actionCell.appendChild(open);
        }
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      if (state.relatedHasMore && state.relatedNextBeforeID) {
        const more = document.createElement("button");
        more.className = "small";
        more.textContent = "Load more";
        more.onclick = () => loadRelatedRecords(true);
        root.appendChild(more);
      }
    }

    async function loadRecordTimeline(loadMore = false) {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        if (!id) throw new Error("Record ID is required");
        const params = new URLSearchParams();
        params.set("limit", "100");
        params.set("dataset_id", dataset);
        params.set("record_id", id);
        loadMore = preparePageParams("recordTimelinePageKey", params, loadMore === true);
        params.delete("dataset_id");
        params.delete("record_id");
        if (loadMore && state.recordTimelineNextBefore) params.set("before", state.recordTimelineNextBefore);
        if (loadMore && state.recordTimelineNextBeforeID) params.set("before_id", state.recordTimelineNextBeforeID);
        const data = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/" + encodeURIComponent(id) + "/timeline?" + params.toString(), { method: "GET" });
        const items = (data && data.items) || [];
        state.recordTimeline = loadMore ? (state.recordTimeline || []).concat(items) : items;
        state.recordTimelineNextBefore = (data && data.next_before) || "";
        state.recordTimelineNextBeforeID = (data && data.next_before_id) || "";
        state.recordTimelineHasMore = !!(data && data.has_more && data.next_before_id);
        renderRecordTimeline(state.recordTimeline);
        setStatus("Timeline loaded: " + (state.recordTimeline || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderRecordTimeline(items) {
      const root = $("revisionTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No timeline items</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Time</th><th>Type</th><th>Action</th><th>User</th><th>Source</th><th>Summary</th><th>Details</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.type || "";
        tr.children[2].textContent = item.action || "";
        tr.children[3].textContent = item.user_id || "";
        tr.children[4].textContent = item.source || "";
        tr.children[5].textContent = item.summary || "";
        tr.children[6].textContent = JSON.stringify(item.data || item.metadata || {});
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      if (state.recordTimelineHasMore && state.recordTimelineNextBeforeID) {
        const more = document.createElement("button");
        more.className = "small";
        more.textContent = "Load more";
        more.onclick = () => loadRecordTimeline(true);
        root.appendChild(more);
      }
    }

    async function createApproval() {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        if (!id) throw new Error("Record ID is required");
        const body = parseJSONField("approvalJson", {});
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/" + encodeURIComponent(id) + "/approvals", { method: "POST", body: JSON.stringify(body) });
        setStatus("Approval created: " + result.id, "ok");
        await listApprovals(false);
      } catch (err) { notifyError(err); }
    }

    async function listApprovals(showStatus, loadMore = false) {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        if (!id) throw new Error("Record ID is required");
        const params = new URLSearchParams({ dataset_id: dataset, record_id: id, limit: "50" });
        loadMore = preparePageParams("approvalPageKey", params, loadMore === true);
        if (loadMore && state.approvalNextBefore) params.set("before", state.approvalNextBefore);
        if (loadMore && state.approvalNextBeforeID) params.set("before_id", state.approvalNextBeforeID);
        const data = await api("/api/v1/data/approvals?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : ((data && data.items) || []);
        state.approvals = loadMore ? (state.approvals || []).concat(items) : items;
        state.approvalNextBefore = (data && data.next_before) || "";
        state.approvalNextBeforeID = (data && data.next_before_id) || "";
        state.approvalHasMore = !!(data && data.has_more && data.next_before_id);
        renderApprovals(state.approvals);
        if (showStatus) setStatus("Approvals loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderApprovals(items) {
      const root = $("approvalTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No approvals</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>Status</th><th>Kind</th><th>Priority</th><th>Assignee</th><th>Due</th><th>Summary</th><th>Reused</th><th>Created By</th><th>Reviewed By</th><th>Updated</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.id || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.kind || "";
        tr.children[3].textContent = item.priority || "";
        tr.children[4].textContent = item.assigned_to || "";
        tr.children[5].textContent = item.due_at || "";
        tr.children[6].textContent = item.summary || "";
        tr.children[7].textContent = item.reused ? "yes" : "";
        tr.children[8].textContent = item.created_by || "";
        tr.children[9].textContent = item.reviewed_by || "";
        tr.children[10].textContent = item.updated_at || item.created_at || "";
        const actions = tr.children[11];
        if (item.status === "pending") {
          const approve = document.createElement("button");
          approve.className = "small";
          approve.textContent = "Approve";
          approve.onclick = () => reviewApproval(item.id, "approve");
          const reject = document.createElement("button");
          reject.className = "small danger";
          reject.textContent = "Reject";
          reject.onclick = () => reviewApproval(item.id, "reject");
          actions.appendChild(approve);
          actions.appendChild(document.createTextNode(" "));
          actions.appendChild(reject);
        }
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.approvalHasMore && state.approvalNextBeforeID, () => listApprovals(false, true));
    }

    async function reviewApproval(id, decision) {
      try {
        await api("/api/v1/data/approvals/" + encodeURIComponent(id) + "/review", { method: "POST", body: JSON.stringify({ decision, reason: "web console " + decision }) });
        setStatus("Approval " + (decision === "approve" ? "approved" : "rejected"), "ok");
        await listApprovals(false);
        await loadRecordTimeline();
      } catch (err) { notifyError(err); }
    }

    async function saveRecord() {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        const body = {
          title: $("recordTitle").value.trim(),
          tags: $("recordTags").value.split(",").map(x => x.trim()).filter(Boolean),
          data: parseJSONField("recordData", {})
        };
        if (id) body.id = id;
        const path = "/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records" + (id ? "/" + encodeURIComponent(id) : "");
        const method = id ? "PATCH" : "POST";
        const saved = await api(path, { method, body: JSON.stringify(body) });
        $("recordId").value = saved.id || id;
        setStatus("Record saved", "ok");
        await queryRecords();
      } catch (err) { notifyError(err); }
    }

    async function deleteRecordByID(id) {
      try {
        const dataset = requireDataset();
        if (!id) throw new Error("Record ID is required");
        if (!confirm("Delete record " + id + "?")) return;
        await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/" + encodeURIComponent(id), { method: "DELETE" });
        if ($("recordId").value.trim() === id) clearRecordEditor();
        setStatus("Record deleted", "ok");
        await queryRecords();
      } catch (err) { notifyError(err); }
    }

    async function deleteCurrentRecord() {
      await deleteRecordByID($("recordId").value.trim());
    }

    async function restoreCurrentRecord() {
      try {
        const dataset = requireDataset();
        const id = $("recordId").value.trim();
        if (!id) throw new Error("Record ID is required");
        if (!confirm("Restore deleted record " + id + "?")) return;
        const result = await api("/api/v1/data/datasets/" + encodeURIComponent(dataset) + "/records/" + encodeURIComponent(id) + "/restore", { method: "POST", body: JSON.stringify({ confirm: true, reason: "web console restore record" }) });
        loadRecordToEditor(result);
        setStatus("Record restored", "ok");
        await loadRecordRevisions();
        await queryRecords();
      } catch (err) { notifyError(err); }
    }

    function appendLoadMoreButton(root, shouldShow, onClick) {
      if (!shouldShow) return;
      const more = document.createElement("button");
      more.className = "small";
      more.textContent = "Load more";
      more.onclick = onClick;
      root.appendChild(more);
    }

    function preparePageParams(stateKey, params, loadMore) {
      const key = params.toString();
      if (loadMore && state[stateKey] !== key) loadMore = false;
      if (!loadMore) state[stateKey] = key;
      return loadMore === true;
    }

    async function listAuditLogs(loadMore = false) {
      try {
        const params = auditQueryParams();
        loadMore = preparePageParams("auditPageKey", params, loadMore === true);
        if (loadMore && state.auditNextBefore) params.set("before", state.auditNextBefore);
        if (loadMore && state.auditNextBeforeID) params.set("before_id", state.auditNextBeforeID);
        const data = await api("/api/v1/data/audit?" + params.toString(), { method: "GET" });
        const items = (data && data.items) || [];
        state.auditLogs = loadMore ? (state.auditLogs || []).concat(items) : items;
        state.auditNextBefore = (data && data.next_before) || "";
        state.auditNextBeforeID = (data && data.next_before_id) || "";
        state.auditHasMore = !!(data && data.has_more && data.next_before_id);
        state.auditUnavailable = "";
        updateAdminSummary();
        renderAuditLogs(state.auditLogs);
        setStatus("Audit loaded: " + (state.auditLogs || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function auditQueryParams() {
      const params = new URLSearchParams();
      const dataset = $("auditDataset").value.trim() || state.selectedDataset;
      if (dataset) params.set("dataset_id", dataset);
      const action = $("auditAction").value.trim();
      if (action) params.set("action", action);
      const userID = $("auditUser").value.trim();
      if (userID) params.set("user_id", userID);
      const targetType = $("auditTargetType").value.trim();
      if (targetType) params.set("target_type", targetType);
      const targetID = $("auditTargetId").value.trim();
      if (targetID) params.set("target_id", targetID);
      const q = $("auditKeyword").value.trim();
      if (q) params.set("q", q);
      params.set("limit", $("auditLimit").value || "100");
      return params;
    }

    async function exportAuditCsv() {
      try {
        const params = auditQueryParams();
        const csv = await apiText("/api/v1/data/audit/export.csv?" + params.toString(), { method: "GET" });
        const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = "audit.csv";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setStatus("Audit CSV exported", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderAuditLogs(items) {
      const root = $("auditTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No audit logs</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Time</th><th>Action</th><th>User</th><th>Dataset</th><th>Target</th><th>Summary</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.action || "";
        tr.children[2].textContent = item.user_id || "";
        tr.children[3].textContent = item.dataset_id || "";
        tr.children[4].textContent = ((item.target_type || "") + ":" + (item.target_id || "")).replace(/^:/, "");
        tr.children[5].textContent = item.summary || "";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.auditHasMore && state.auditNextBeforeID, () => listAuditLogs(true));
    }

    async function listDataEvents(loadMore = false) {
      try {
        const params = eventQueryParams();
        loadMore = preparePageParams("dataEventPageKey", params, loadMore === true);
        if (loadMore && state.dataEventNextBefore) params.set("before", state.dataEventNextBefore);
        if (loadMore && state.dataEventNextBeforeID) params.set("before_id", state.dataEventNextBeforeID);
        const data = await api("/api/v1/data/events?" + params.toString(), { method: "GET" });
        const items = (data && data.items) || [];
        state.dataEvents = loadMore ? (state.dataEvents || []).concat(items) : items;
        state.dataEventNextBefore = (data && data.next_before) || "";
        state.dataEventNextBeforeID = (data && data.next_before_id) || "";
        state.dataEventHasMore = !!(data && data.has_more && data.next_before_id);
        renderDataEvents(state.dataEvents);
        if (!loadMore) await listEventDeadLetters(false);
        setStatus("Events loaded: " + (state.dataEvents || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function eventQueryParams() {
      const params = new URLSearchParams();
      const dataset = $("eventDataset").value.trim() || state.selectedDataset;
      if (dataset) params.set("dataset_id", dataset);
      const source = $("eventSource").value.trim();
      if (source) params.set("source", source);
      const eventType = $("eventType").value.trim();
      if (eventType) params.set("event_type", eventType);
      const businessAction = $("eventBusinessAction").value.trim();
      if (businessAction) params.set("business_action_id", businessAction);
      const key = $("eventIdempotencyKey").value.trim();
      if (key) params.set("idempotency_key", key);
      params.set("limit", $("eventLimit").value || "100");
      return params;
    }

    async function ingestEvent(dryRun) {
      try {
        const body = parseJSONField("eventIngestJson", {});
        body.dry_run = !!dryRun;
        const result = await api("/api/v1/data/events", { method: "POST", body: JSON.stringify(body) });
        setRaw(result || {});
        if (dryRun) {
          setStatus(result.valid ? "Event dry-run passed" : "Event dry-run failed", result.valid ? "ok" : "err");
          return;
        }
        setStatus("Event ingested: " + ((result && result.status) || "ok"), "ok");
        await listDataEvents();
        if (result && result.dataset_id) {
          state.selectedDataset = result.dataset_id;
          renderDatasets();
        }
      } catch (err) { notifyError(err); }
    }

    function renderDataEvents(items) {
      const root = $("eventTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No events</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Time</th><th>Status</th><th>Source</th><th>Event</th><th>Business Action</th><th>Dataset</th><th>Record</th><th>Idempotency Key</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.applied_at || "";
        tr.children[1].textContent = item.result_status || "";
        tr.children[2].textContent = item.source || "";
        tr.children[3].textContent = item.event_type || "";
        tr.children[4].textContent = item.business_action_id || "";
        tr.children[5].textContent = item.dataset_id || "";
        tr.children[6].textContent = item.record_id || "";
        tr.children[7].textContent = item.idempotency_key || "";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.dataEventHasMore && state.dataEventNextBeforeID, () => listDataEvents(true));
    }

    async function listEventDeadLetters(showStatus, loadMore = false) {
      try {
        const params = eventQueryParams();
        params.set("status", "open");
        loadMore = preparePageParams("eventDeadLetterPageKey", params, loadMore === true);
        if (loadMore && state.eventDeadLetterNextBefore) params.set("before", state.eventDeadLetterNextBefore);
        if (loadMore && state.eventDeadLetterNextBeforeID) params.set("before_id", state.eventDeadLetterNextBeforeID);
        const data = await api("/api/v1/data/events/dead-letter?" + params.toString(), { method: "GET" });
        const items = (data && data.items) || [];
        state.eventDeadLetters = loadMore ? (state.eventDeadLetters || []).concat(items) : items;
        state.eventDeadLetterNextBefore = (data && data.next_before) || "";
        state.eventDeadLetterNextBeforeID = (data && data.next_before_id) || "";
        state.eventDeadLetterHasMore = !!(data && data.has_more && data.next_before_id);
        renderEventDeadLetters(state.eventDeadLetters);
        if (showStatus) setStatus("Dead letters loaded: " + (state.eventDeadLetters || []).length, "ok");
      } catch (err) { notifyError(err); }
    }

    function renderEventDeadLetters(items) {
      const root = $("deadLetterTable");
      if (!root) return;
      if (!items.length) {
        root.innerHTML = '<div class="empty">No open dead letters</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Time</th><th>Status</th><th>Source</th><th>Business Action</th><th>Dataset</th><th>Record</th><th>Error</th><th></th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td><pre></pre></td><td><button>Retry</button><button>Resolve</button></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.source || "";
        tr.children[3].textContent = item.business_action_id || "";
        tr.children[4].textContent = item.dataset_id || "";
        tr.children[5].textContent = item.record_id || "";
        tr.children[6].querySelector("pre").textContent = item.error || "";
        const buttons = tr.children[7].querySelectorAll("button");
        buttons[0].onclick = () => retryDeadLetter(item.id);
        buttons[1].onclick = () => resolveDeadLetter(item.id);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.eventDeadLetterHasMore && state.eventDeadLetterNextBeforeID, () => listEventDeadLetters(true, true));
    }

    async function retryDeadLetter(id) {
      try {
        const result = await api("/api/v1/data/events/dead-letter/" + encodeURIComponent(id) + "/retry", { method: "POST" });
        setRaw(result || {});
        setStatus("Dead letter retried", "ok");
        await listDataEvents();
      } catch (err) { notifyError(err); }
    }

    async function resolveDeadLetter(id) {
      try {
        const result = await api("/api/v1/data/events/dead-letter/" + encodeURIComponent(id) + "/resolve", { method: "POST", body: JSON.stringify({ resolution: "resolved in web console" }) });
        setRaw(result || {});
        setStatus("Dead letter resolved", "ok");
        await listEventDeadLetters(false);
      } catch (err) { notifyError(err); }
    }

    async function loadStats() {
      try {
        const stats = await api("/api/v1/data/stats", { method: "GET" });
        state.stats = stats || {};
        updateAdminSummary();
        renderStats(stats || {});
        await listOperationPlans(false);
        setStatus("Stats loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function createOperationPlan() {
      try {
        const body = parseJSONField("operationPlanJson", {});
        const plan = await api("/api/v1/data/operation-plans", { method: "POST", body: JSON.stringify(body) });
        setStatus("Operation plan created: " + plan.id, "ok");
        await listOperationPlans(false);
      } catch (err) { notifyError(err); }
    }

    async function listOperationPlans(showStatus, loadMore = false) {
      try {
        const params = new URLSearchParams({ limit: "50" });
        loadMore = preparePageParams("operationPlanPageKey", params, loadMore === true);
        if (loadMore && state.operationPlanNextBefore) params.set("before", state.operationPlanNextBefore);
        if (loadMore && state.operationPlanNextBeforeID) params.set("before_id", state.operationPlanNextBeforeID);
        const data = await api("/api/v1/data/operation-plans?" + params.toString(), { method: "GET" });
        const items = Array.isArray(data) ? data : ((data && data.items) || []);
        state.operationPlans = loadMore ? (state.operationPlans || []).concat(items) : items;
        state.operationPlanNextBefore = (data && data.next_before) || "";
        state.operationPlanNextBeforeID = (data && data.next_before_id) || "";
        state.operationPlanHasMore = !!(data && data.has_more && data.next_before_id);
        renderOperationPlans(state.operationPlans);
        if (showStatus) setStatus("Operation plans loaded", "ok");
      } catch (err) { notifyError(err); }
    }

    function renderOperationPlans(items) {
      const root = $("operationPlanTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No operation plans</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Created</th><th>Status</th><th>Operation</th><th>Dataset</th><th>Risk</th><th>Matched</th><th>Summary</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.created_at || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.operation || "";
        tr.children[3].textContent = item.dataset_id || "";
        tr.children[4].textContent = item.risk_level || "";
        tr.children[5].textContent = String((item.preview && item.preview.matched) || 0);
        tr.children[6].textContent = item.summary || "";
        const actions = tr.children[7];
        if (item.status === "pending") {
          const approve = document.createElement("button");
          approve.className = "small primary";
          approve.textContent = "Approve";
          approve.onclick = () => reviewOperationPlan(item.id, "approve");
          const reject = document.createElement("button");
          reject.className = "small danger";
          reject.textContent = "Reject";
          reject.onclick = () => reviewOperationPlan(item.id, "reject");
          const cancel = document.createElement("button");
          cancel.className = "small";
          cancel.textContent = "Cancel";
          cancel.onclick = () => cancelOperationPlan(item.id);
          actions.appendChild(approve);
          actions.appendChild(document.createTextNode(" "));
          actions.appendChild(reject);
          actions.appendChild(document.createTextNode(" "));
          actions.appendChild(cancel);
        } else if (item.status === "approved") {
          const apply = document.createElement("button");
          apply.className = "small primary";
          apply.textContent = "Apply";
          apply.onclick = () => applyOperationPlan(item.id);
          const cancel = document.createElement("button");
          cancel.className = "small";
          cancel.textContent = "Cancel";
          cancel.onclick = () => cancelOperationPlan(item.id);
          actions.appendChild(apply);
          actions.appendChild(document.createTextNode(" "));
          actions.appendChild(cancel);
        }
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.operationPlanHasMore && state.operationPlanNextBeforeID, () => listOperationPlans(false, true));
    }

    async function reviewOperationPlan(id, decision) {
      try {
        await api("/api/v1/data/operation-plans/" + encodeURIComponent(id) + "/review", { method: "POST", body: JSON.stringify({ decision, reason: "web console " + decision }) });
        setStatus("Operation plan " + (decision === "approve" ? "approved" : "rejected"), "ok");
        await listOperationPlans(false);
      } catch (err) { notifyError(err); }
    }

    async function applyOperationPlan(id) {
      if (!confirm("Apply operation plan " + id + "?")) return;
      try {
        await api("/api/v1/data/operation-plans/" + encodeURIComponent(id) + "/apply", { method: "POST", body: JSON.stringify({ confirm: true, reason: "web console apply" }) });
        setStatus("Operation plan applied", "ok");
        await listOperationPlans(false);
      } catch (err) { notifyError(err); }
    }

    async function cancelOperationPlan(id) {
      try {
        await api("/api/v1/data/operation-plans/" + encodeURIComponent(id) + "/cancel", { method: "POST", body: "{}" });
        setStatus("Operation plan canceled", "ok");
        await listOperationPlans(false);
      } catch (err) { notifyError(err); }
    }

    async function runMaintenance(tasks) {
      try {
        const result = await api("/api/v1/data/maintenance/run", { method: "POST", body: JSON.stringify({ tasks }) });
        renderMaintenance(result || {});
        setStatus(result.valid ? "Maintenance completed" : "Maintenance found issues", result.valid ? "ok" : "err");
        await loadStats();
      } catch (err) { notifyError(err); }
    }

    function renderMaintenance(result) {
      const root = $("maintenanceResult");
      const items = result.tasks || [];
      if (!items.length) {
        root.innerHTML = '<div class="empty">No maintenance result</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Task</th><th>Status</th><th>Message</th><th>Duration ms</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.task || "";
        tr.children[1].textContent = item.status || "";
        tr.children[2].textContent = item.message || "";
        tr.children[3].textContent = String(item.duration_ms || 0);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    function renderStats(stats) {
      const summary = $("statsSummary");
      const jobs = stats.import_jobs || {};
      const exportJobs = stats.export_jobs || {};
      const rows = [
        ["Engine", stats.engine || ""],
        ["Schema", String(stats.schema_version || 0)],
        ["Datasets", String(stats.dataset_count || 0)],
        ["Records", String(stats.record_count || 0)],
        ["Fields", String(stats.field_count || 0)],
        ["Import jobs", JSON.stringify(jobs)],
        ["Export jobs", JSON.stringify(exportJobs)],
        ["Quality runs", String(stats.quality_run_count || 0)],
        ["Audit logs", String(stats.audit_log_count || 0)],
        ["Backups", String(stats.backup_count || 0)],
        ["Database bytes", String(stats.database_bytes || 0)]
      ];
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Metric</th><th>Value</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      rows.forEach(row => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td>";
        tr.children[0].textContent = row[0];
        tr.children[1].textContent = row[1];
        body.appendChild(tr);
      });
      summary.innerHTML = "";
      summary.appendChild(table);
      renderStatsDatasets(stats.datasets || []);
    }

    function renderStatsDatasets(items) {
      const root = $("statsDatasetTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No datasets</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>Dataset</th><th>Title</th><th>Schema</th><th>Fields</th><th>Records</th><th>Updated</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.dataset_id || "";
        tr.children[1].textContent = item.title || "";
        tr.children[2].textContent = String(item.schema_version || 0);
        tr.children[3].textContent = String(item.field_count || 0);
        tr.children[4].textContent = String(item.record_count || 0);
        tr.children[5].textContent = item.updated_at || "";
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
    }

    async function createBackup() {
      try {
        const body = { name: $("backupName").value.trim(), note: $("backupNote").value.trim() };
        await api("/api/v1/data/backups", { method: "POST", body: JSON.stringify(body) });
        setStatus("Backup created", "ok");
        await listBackups();
      } catch (err) { notifyError(err); }
    }

    async function listBackups(loadMore = false) {
      try {
        const params = new URLSearchParams({ limit: "50" });
        loadMore = preparePageParams("backupPageKey", params, loadMore === true);
        if (loadMore && state.backupNextBefore) params.set("before", state.backupNextBefore);
        if (loadMore && state.backupNextBeforeID) params.set("before_id", state.backupNextBeforeID);
        const backups = await api("/api/v1/data/backups?" + params.toString(), { method: "GET" });
        const items = Array.isArray(backups) ? backups : (Array.isArray(backups?.items) ? backups.items : []);
        state.backups = loadMore ? (state.backups || []).concat(items) : items;
        state.backupNextBefore = (backups && backups.next_before) || "";
        state.backupNextBeforeID = (backups && backups.next_before_id) || "";
        state.backupHasMore = !!(backups && backups.has_more && backups.next_before_id);
        renderBackups(state.backups);
      } catch (err) { notifyError(err); }
    }

    function renderBackups(items) {
      const root = $("backupTable");
      if (!items.length) {
        root.innerHTML = '<div class="empty">No backups</div>';
        return;
      }
      const table = document.createElement("table");
      table.innerHTML = "<thead><tr><th>ID</th><th>Name</th><th>Size</th><th>SHA256</th><th>Created</th><th>Actions</th></tr></thead><tbody></tbody>";
      const body = table.querySelector("tbody");
      items.forEach(item => {
        const tr = document.createElement("tr");
        tr.innerHTML = "<td></td><td></td><td></td><td></td><td></td><td></td>";
        tr.children[0].textContent = item.id;
        tr.children[1].textContent = item.name || item.note || "";
        tr.children[2].textContent = String(item.size_bytes || 0);
        const checksum = item.sha256 || "";
        tr.children[3].textContent = checksum ? checksum.slice(0, 16) + (checksum.length > 16 ? "..." : "") : "";
        tr.children[3].title = checksum;
        tr.children[4].textContent = item.created_at || "";
        const actions = tr.children[5];
        const download = document.createElement("button");
        download.className = "small";
        download.textContent = "Download";
        download.onclick = () => downloadBackup(item.id);
        const restore = document.createElement("button");
        restore.className = "small danger";
        restore.textContent = "Restore";
        restore.onclick = () => restoreBackup(item.id);
        actions.appendChild(download);
        actions.appendChild(document.createTextNode(" "));
        actions.appendChild(restore);
        body.appendChild(tr);
      });
      root.innerHTML = "";
      root.appendChild(table);
      appendLoadMoreButton(root, state.backupHasMore && state.backupNextBeforeID, () => listBackups(true));
    }

    async function downloadBackup(id) {
      try {
        const result = await apiBlob("/api/v1/data/backups/" + encodeURIComponent(id) + "/download", { method: "GET" });
        const url = URL.createObjectURL(result.blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = id.replace(/[\\/:*?"<>|]+/g, "_") + ".db";
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        setStatus("Backup downloaded", "ok");
      } catch (err) { notifyError(err); }
    }

    async function restoreBackup(id) {
      if (!confirm("Restore backup " + id + "? Current data will be replaced.")) return;
      try {
        await api("/api/v1/data/backups/" + encodeURIComponent(id) + "/restore", { method: "POST", body: JSON.stringify({ confirm: true, reason: "web console restore" }) });
        setStatus("Backup restored", "ok");
        await loadDatasets();
      } catch (err) { notifyError(err); }
    }

    function switchTab(name) {
      document.querySelectorAll(".tab").forEach(btn => btn.classList.toggle("active", btn.dataset.tab === name));
      document.querySelectorAll(".tab-panel").forEach(panel => panel.classList.toggle("hide", panel.id !== name));
      updateModuleHeader(name);
      saveSettings();
      if (name === "inbox") loadInbox();
      if (name === "domains") loadDomains();
      if (name === "relationships") loadRelationships();
      if (name === "actions") loadBusinessActions();
      if (name === "actions") loadEventContracts();
      if (name === "rules") loadBusinessRules();
      if (name === "connectors") loadConnectors();
      if (name === "views") loadBusinessViews();
      if (name === "dashboards") loadDashboards();
      if (name === "reports") loadReports();
      if (name === "quality") { loadQualityChecks(); if (state.selectedDataset) listQualityRuns(false); }
      if (name === "dataset" && state.selectedDataset) loadDatasetDetail(false);
      if (name === "fields" && state.selectedDataset) { loadFields(); loadSchemaProposals(false); }
      if (name === "backups") listBackups();
      if (name === "events") listDataEvents();
      if (name === "audit") listAuditLogs();
      if (name === "ops") loadStats();
      if (name === "access") loadAccessWorkspace(false);
    }

    $("saveAuth").onclick = checkConnection;
    $("refreshDatasets").onclick = loadDatasets;
    $("refreshInbox").onclick = loadInbox;
    $("refreshDomains").onclick = loadDomains;
    $("refreshRelationships").onclick = loadRelationships;
    $("useSelectedDatasetForRelationships").onclick = () => { $("relationshipDataset").value = state.selectedDataset || ""; loadRelationships(); };
    $("clearRelationshipFilter").onclick = () => { $("relationshipDataset").value = ""; loadRelationships(); };
    $("resolveIntent").onclick = resolveIntent;
    $("refreshActions").onclick = loadBusinessActions;
    $("refreshRules").onclick = loadBusinessRules;
    $("evaluateRules").onclick = evaluateBusinessRules;
    $("useSelectedActionForRules").onclick = useSelectedActionForRules;
    $("checkBusinessRules").onclick = checkSelectedBusinessRules;
    $("refreshConnectors").onclick = loadConnectors;
    $("refreshConnectorHealth").onclick = loadConnectorHealthOverview;
    $("saveConnector").onclick = saveConnector;
    $("testConnector").onclick = testConnector;
    $("validateConnectorConfig").onclick = validateConnectorConfig;
    $("checkConnectorReadiness").onclick = checkConnectorReadiness;
    $("checkConnectorHealth").onclick = checkConnectorHealth;
    $("getConnectorSyncState").onclick = getConnectorSyncState;
    $("listConnectorSyncRuns").onclick = listConnectorSyncRuns;
    $("markConnectorSyncSuccess").onclick = markConnectorSyncSuccess;
    $("planConnectorSync").onclick = planConnectorSync;
    $("runConnectorSyncBatch").onclick = runConnectorSyncBatch;
    $("suggestConnectorMapping").onclick = suggestConnectorMapping;
    $("applySuggestedConnectorMapping").onclick = applySuggestedConnectorMapping;
    $("saveSuggestedConnectorMapping").onclick = saveSuggestedConnectorMapping;
    $("loadConnectorEventTemplate").onclick = loadConnectorEventTemplate;
    $("previewConnectorEvent").onclick = previewConnectorEvent;
    $("formatConnectorConfig").onclick = () => { $("connectorActions").value = JSON.stringify(parseJSONField("connectorActions", []), null, 2); $("connectorConfig").value = JSON.stringify(parseJSONField("connectorConfig", {}), null, 2); };
    $("refreshViews").onclick = loadBusinessViews;
    $("queryBusinessView").onclick = queryBusinessView;
    $("formatViewQuery").onclick = () => { $("viewQueryFilter").value = JSON.stringify(parseJSONField("viewQueryFilter", {}), null, 2); };
    $("refreshDashboards").onclick = loadDashboards;
    $("runDashboard").onclick = runDashboard;
    $("refreshReports").onclick = loadReports;
    $("runReport").onclick = runReport;
    $("runAggregate").onclick = runAggregate;
    $("refreshQualityChecks").onclick = loadQualityChecks;
    $("runQualityCheck").onclick = runQualityCheck;
    $("refreshQualityRuns").onclick = () => listQualityRuns(true);
    $("formatReportFilter").onclick = () => { $("reportFilter").value = JSON.stringify(parseJSONField("reportFilter", {}), null, 2); };
    $("formatAggregate").onclick = () => { $("aggregateJson").value = JSON.stringify(parseJSONField("aggregateJson", {}), null, 2); };
    $("dryRunBusinessAction").onclick = () => executeBusinessAction(true);
    $("executeBusinessAction").onclick = () => executeBusinessAction(false);
    $("loadEventContract").onclick = loadEventContract;
    $("refreshEventContracts").onclick = () => loadEventContracts(false);
    $("formatBusinessActionData").onclick = () => { $("businessActionData").value = JSON.stringify(parseJSONField("businessActionData", {}), null, 2); };
    $("createFromTemplate").onclick = createFromTemplate;
    $("loadMoreTemplates").onclick = () => loadTemplates(true);
    $("previewBootstrapTemplates").onclick = () => bootstrapTemplates(true);
    $("bootstrapTemplates").onclick = () => bootstrapTemplates(false);
    $("createDataset").onclick = createDataset;
    $("reloadDataset").onclick = () => loadDatasetDetail(true);
    $("updateDataset").onclick = updateDataset;
    $("deleteDataset").onclick = deleteDataset;
    $("queryRecords").onclick = queryRecords;
    $("exportRecordsCsv").onclick = exportRecordsCsv;
    $("exportRecordsJsonl").onclick = exportRecordsJsonl;
    $("startCsvExportJob").onclick = () => startExportJob("csv");
    $("startJsonlExportJob").onclick = () => startExportJob("jsonl");
    $("refreshExportJobs").onclick = () => listExportJobs(true);
    $("clearQuery").onclick = () => { $("queryText").value = ""; $("queryTag").value = ""; $("queryFilter").value = ""; };
    $("loadFields").onclick = loadFields;
    $("formatFields").onclick = () => { $("fieldsJson").value = JSON.stringify(parseJSONField("fieldsJson", {}), null, 2); };
    $("saveFields").onclick = saveFields;
    $("refreshSchemaProposals").onclick = () => loadSchemaProposals(true);
    $("proposeSchema").onclick = proposeSchema;
    $("applySchemaProposal").onclick = applySchemaProposal;
    $("validateRecord").onclick = validateRecordEditor;
    $("dryRunBatchImport").onclick = () => batchImportRecords(true);
    $("runBatchImport").onclick = () => batchImportRecords(false);
    $("dryRunBulkUpdate").onclick = () => bulkUpdateRecords(true);
    $("runBulkUpdate").onclick = () => bulkUpdateRecords(false);
    $("formatBulkUpdate").onclick = () => { $("bulkUpdateJson").value = JSON.stringify(parseJSONField("bulkUpdateJson", {}), null, 2); };
    $("dryRunBulkDelete").onclick = () => bulkDeleteRecords(true);
    $("runBulkDelete").onclick = () => bulkDeleteRecords(false);
    $("formatBulkDelete").onclick = () => { $("bulkDeleteJson").value = JSON.stringify(parseJSONField("bulkDeleteJson", {}), null, 2); };
    $("startBatchImportJob").onclick = startBatchImportJob;
    $("formatBatchRecords").onclick = () => { $("batchRecordsJson").value = JSON.stringify(parseJSONField("batchRecordsJson", []), null, 2); };
    $("loadCsvTemplate").onclick = loadCsvTemplate;
    $("dryRunCsvImport").onclick = () => importRecordsCSV(true);
    $("runCsvImport").onclick = () => importRecordsCSV(false);
    $("startCsvImportJob").onclick = startCSVImportJob;
    $("dryRunJsonlImport").onclick = () => importRecordsJSONL(true);
    $("runJsonlImport").onclick = () => importRecordsJSONL(false);
    $("startJsonlImportJob").onclick = startJSONLImportJob;
    $("refreshImportJobs").onclick = () => listImportJobs(true);
    $("saveRecord").onclick = saveRecord;
    $("newRecord").onclick = clearRecordEditor;
    $("loadRecordRevisions").onclick = loadRecordRevisions;
    $("loadRelatedRecords").onclick = loadRelatedRecords;
    $("loadRecordTimeline").onclick = loadRecordTimeline;
    $("createApproval").onclick = createApproval;
    $("refreshApprovals").onclick = () => listApprovals(true);
    $("formatApproval").onclick = () => { $("approvalJson").value = JSON.stringify(parseJSONField("approvalJson", {}), null, 2); };
    $("restoreRecord").onclick = restoreCurrentRecord;
    $("deleteRecord").onclick = deleteCurrentRecord;
    $("createBackup").onclick = createBackup;
    $("listBackups").onclick = listBackups;
    $("refreshEvents").onclick = listDataEvents;
    $("refreshDeadLetters").onclick = () => listEventDeadLetters(true);
    $("dryRunEvent").onclick = () => ingestEvent(true);
    $("ingestEvent").onclick = () => ingestEvent(false);
    $("formatEventIngest").onclick = () => { $("eventIngestJson").value = JSON.stringify(parseJSONField("eventIngestJson", {}), null, 2); };
    $("refreshAudit").onclick = listAuditLogs;
    $("exportAuditCsv").onclick = exportAuditCsv;
    $("refreshStats").onclick = loadStats;
    $("createOperationPlan").onclick = createOperationPlan;
    $("refreshOperationPlans").onclick = () => listOperationPlans(true);
    $("formatOperationPlan").onclick = () => { $("operationPlanJson").value = JSON.stringify(parseJSONField("operationPlanJson", {}), null, 2); };
	$("refreshAccessCatalog").onclick = () => loadAccessWorkspace(true);
	$("applyAccessPreset").onclick = applyAccessPreset;
	$("loadMoreAccessPresets").onclick = () => loadAccessCatalog(true);
	$("recommendAccessPolicy").onclick = recommendAccessAuthorization;
	$("clearAccessRecommendation").onclick = () => { $("accessRecommendation").innerHTML = ""; setStatus("Authorization recommendation cleared", "ok"); };
	$("generateAccessPolicy").onclick = generateAccessPolicy;
	$("createAccessKey").onclick = createManagedAccessKey;
	$("updateAccessKey").onclick = updateManagedAccessKey;
	$("previewAccessKey").onclick = () => previewManagedAccessKey("");
	$("checkAccessKey").onclick = checkManagedAccessKey;
	$("reviewAccessKeys").onclick = reviewManagedAccessKeys;
	$("exportAccessReview").onclick = exportAccessReview;
	$("refreshEvidenceSummary").onclick = refreshGovernanceEvidenceSummary;
	$("downloadEvidenceSummary").onclick = downloadGovernanceEvidenceSummary;
	$("exportEvidencePack").onclick = exportGovernanceEvidencePack;
	$("planAccessRemediation").onclick = planAccessRemediation;
	$("refreshAccessKeys").onclick = () => loadManagedAccessKeys(true);
	$("refreshAdminAccounts").onclick = () => loadAdminAccounts(true);
	$("refreshAdminSessions").onclick = () => loadAdminSessions(true);
	$("createAdminAccount").onclick = createAdminAccount;
	$("updateAdminAccount").onclick = updateAdminAccount;
	$("generateAgentHandoff").onclick = () => { renderAgentHandoff(currentAccessPolicyFromForm(), state.lastAccessKeySecret); setStatus("Agent handoff generated", "ok"); };
	$("runAgentReadiness").onclick = runAgentReadinessCheck;
	$("compareAccessPolicy").onclick = compareAccessPolicyChanges;
	$("generateAgentOnboarding").onclick = generateAgentOnboardingChecklist;
	$("generateAgentPacket").onclick = generateAgentOnboardingPacket;
	$("copyAgentPacket").onclick = copyAgentPacket;
	$("downloadAgentPacket").onclick = downloadAgentPacket;
	$("copyAgentHandoff").onclick = copyAgentHandoff;
	$("accessGuideLoadPresets").onclick = async () => { await loadAccessWorkspace(false); $("accessPreset").focus(); };
	$("accessGuideGrantAnalytics").onclick = () => {
	  $("accessAllowReports").checked = true;
	  document.querySelectorAll(".access-capability").forEach(input => {
	    input.checked = input.dataset.kind === "view" || input.dataset.kind === "report" || input.dataset.kind === "dashboard";
	  });
	  generateAccessPolicy();
	};
    $("accessGuideReview").onclick = reviewManagedAccessKeys;
    $("initializeAdmin").onclick = initializeAdmin;
    $("loginAdmin").onclick = loginAdmin;
    $("accessKeyStatus").onchange = () => loadManagedAccessKeys(true);
    $("accessKeySearch").onkeydown = (event) => { if (event.key === "Enter") loadManagedAccessKeys(true); };
    $("accessKeyLimit").onkeydown = (event) => { if (event.key === "Enter") loadManagedAccessKeys(true); };
    $("runIntegrityCheck").onclick = () => runMaintenance(["integrity_check"]);
    $("runOptimize").onclick = () => runMaintenance(["integrity_check", "optimize"]);
    $("runVacuum").onclick = () => { if (confirm("Run VACUUM now?")) runMaintenance(["integrity_check", "vacuum", "optimize"]); };
    $("tabs").onclick = (event) => { if (event.target.dataset.tab) switchTab(event.target.dataset.tab); };
    $("refreshOverview").onclick = async () => { await checkConnection(); await loadOverviewStats(false); await loadOverviewCapabilitiesData(false); await loadOverviewDomains(false); await loadOverviewAccessRisk(false); await loadOverviewWorkQueue(false); await loadOverviewIntegrationHealth(false); await loadOverviewActivity(false); switchTab("overview"); };
    $("refreshOverviewDomains").onclick = async () => { await loadOverviewDomains(true); };
    $("refreshOverviewAccessRisk").onclick = async () => { await loadOverviewAccessRisk(true); };
    $("refreshOverviewWorkQueue").onclick = async () => { await loadOverviewWorkQueue(true); };
    $("refreshOverviewIntegration").onclick = async () => { await loadOverviewIntegrationHealth(true); };
    $("refreshOverviewActivity").onclick = async () => { await loadOverviewActivity(true); };
    $("previewOverviewBootstrap").onclick = async () => { switchTab("overview"); await bootstrapTemplates(true); updateAdminSummary(); };
    $("resolveOverviewIntent").onclick = resolveOverviewIntent;
    $("overviewIntentQuery").onkeydown = (event) => { if (event.key === "Enter") resolveOverviewIntent(); };
    $("overviewIntentDomain").onkeydown = (event) => { if (event.key === "Enter") resolveOverviewIntent(); };
    document.querySelectorAll(".quick-action").forEach(btn => { btn.onclick = () => switchTab(btn.dataset.targetTab); });
    $("language").onchange = handleLanguageChange;

    const savedSettings = loadSettings();
    const i18nObserver = new MutationObserver((items) => {
      if (i18nApplying || i18nMutationSuppressed) return;
      if (items.some(item => item.addedNodes && item.addedNodes.length)) applyI18n(document.body);
    });
    i18nObserver.observe(document.body, { childList: true, subtree: true });
    applyI18n(document.body);
    if ($("token").value.trim() && savedSettings.active_tab && moduleMeta[savedSettings.active_tab]) {
      switchTab(savedSettings.active_tab);
    } else {
      updateModuleHeader("overview");
    }
    if ($("token").value.trim()) { loadOverviewStats(false); loadOverviewCapabilitiesData(false); loadOverviewDomains(false); loadOverviewAccessRisk(false); loadOverviewWorkQueue(false); loadOverviewIntegrationHealth(false); loadOverviewActivity(false); loadTemplates(); loadBusinessActions(); loadBusinessRules(); loadConnectors(); loadBusinessViews(); loadDashboards(); loadReports(); loadQualityChecks(); loadDatasets(); }
    refreshSetupStatus();
    publicApi("/readyz").then(data => { state.ready = data || {}; updateAdminSummary(); setStatus("Service online: " + data.engine + " schema " + data.schema_version, "ok"); }).catch(() => setStatus("Service is not ready", "err"));
  </script>
</body>
</html>`
