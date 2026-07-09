// MaClawDataSrv MIS Admin Console - Internationalization
"use strict";

const I18N = {
  _lang: localStorage.getItem("mis_lang") || "zh",
  _dict: {},

  get lang() { return this._lang; },
  set lang(v) { this._lang = v; localStorage.setItem("mis_lang", v); },

  t(key) {
    if (this._lang === "en") return key;
    return this._dict[key] || key;
  },

  init() {
    this._dict = {
      // === App Chrome ===
      "MaClawDataSrv MIS Admin Console": "MaClawDataSrv MIS 管理控制台",
      "Enterprise structured data operations": "企业结构化数据运营工作台",
      "Sign out": "退出登录",
      "Language": "语言",

      // === Login ===
      "Admin Login": "管理员登录",
      "Username": "用户名",
      "Password": "密码",
      "Sign in": "登录",
      "First-time setup": "首次初始化",
      "Initialize": "初始化",

      // === Navigation Groups ===
      "Overview": "总览",
      "Quick Start": "快速操作",
      "Schema": "数据建模",
      "Data": "数据操作",
      "Integration": "集成",
      "Security": "安全与治理",
      "Dev": "开发",

      // === Navigation Items ===
      "Domains": "业务域",
      "Datasets": "数据集",
      "Fields": "字段",
      "Relationships": "关联",
      "Records": "记录",
      "Editor": "编辑器",
      "Actions": "业务动作",
      "Rules": "规则",
      "Inbox": "收件箱",
      "Connectors": "连接器",
      "Views": "视图",
      "Dashboards": "仪表盘",
      "Reports": "报表",
      "API Keys": "API 密钥",
      "Admins": "管理员",
      "Quality": "质量检查",
      "Backups": "备份",
      "Events": "事件",
      "Audit": "审计",
      "Ops": "运维",
      "Response": "原始响应",

      // === Overview Page ===
      "Service Status": "服务状态",
      "Total Datasets": "数据集数",
      "Total Records": "记录数",
      "Pending Items": "待处理项",
      "Active Keys": "有效密钥",
      "Setup Checklist": "初始设置清单",
      "Operational Health": "运营健康",
      "Business Domain Readiness": "业务域就绪度",
      "Work Queue": "工作队列",
      "Daily Operations": "日常运营",
      "Analytics": "分析",
      "Governance": "治理",
      "Monitoring Details": "监控详情",
      "Integration Health": "集成健康",
      "Access Risk": "访问风险",
      "Recent Activity": "近期活动",

      // === API Keys Page ===
      "API Key Management": "API 密钥管理",
      "Manage agent and service access credentials. Create scoped keys, review permissions, and rotate expired credentials.": "管理 Agent 和服务的访问凭证。创建权限范围化密钥、复核权限、轮换过期凭证。",
      "Key Overview": "密钥概览",
      "Total keys": "密钥总数",
      "Expiring soon": "即将过期",
      "Expired": "已过期",
      "High risk": "高风险",
      "Create New Key": "创建新密钥",
      "Step 1: Choose Role": "第 1 步：选择角色",
      "Select a preset role that matches the agent's business function.": "选择一个匹配 Agent 业务功能的预设角色。",
      "Step 2: Configure Permissions": "第 2 步：配置权限",
      "Fine-tune which operations, datasets, and fields the key can access.": "精细调整密钥可访问的操作、数据集和字段。",
      "Step 3: Set Expiry & Create": "第 3 步：设置有效期并创建",
      "Set an expiration date and create the managed key.": "设置过期时间并创建托管密钥。",
      "API key ID": "密钥 ID",
      "User / Agent": "用户 / Agent",
      "Role": "角色",
      "Authorization preset": "授权预设",
      "Custom": "自定义",
      "Apply preset": "应用预设",
      "Agent purpose": "Agent 用途",
      "Recommend": "推荐方案",
      "Expires at": "过期时间",
      "Allow views/reports/dashboards": "允许视图/报表/仪表盘",
      "Allow raw dataset API": "允许原始数据集 API",
      "Allow sensitive fields": "允许敏感字段",
      "Allow admin operations": "允许管理操作",
      "Generate policy": "生成策略",
      "Create key": "创建密钥",
      "Key List": "密钥列表",
      "Search keys...": "搜索密钥...",
      "All": "全部",
      "Active": "有效",
      "Disabled": "已停用",
      "Refresh": "刷新",
      "Agent Onboarding": "Agent 接入",
      "Generate handoff document": "生成交接文档",
      "Run readiness check": "运行就绪检查",
      "Generate onboarding packet": "生成接入包",
      "Compliance & Review": "合规与复核",
      "Review access": "复核访问",
      "Export evidence": "导出证据",
      "Refresh evidence": "刷新证据",

      // === Records Page ===
      "Data Records": "数据记录",
      "Search and manage structured business records.": "搜索和管理结构化业务记录。",
      "Search & Browse": "搜索与浏览",
      "Keyword": "关键词",
      "Tag": "标签",
      "Limit": "数量",
      "Query": "查询",
      "Export CSV": "导出 CSV",
      "Export JSONL": "导出 JSONL",
      "Clear": "清除",
      "Record Editor": "记录编辑",
      "New record": "新建记录",
      "Record ID": "记录 ID",
      "Title": "标题",
      "Tags": "标签",
      "Data JSON": "数据 JSON",
      "Validate": "校验",
      "Save": "保存",
      "Delete": "删除",
      "Batch Operations": "批量操作",
      "Import CSV": "导入 CSV",
      "Import JSONL": "导入 JSONL",
      "Bulk update": "批量更新",
      "Bulk delete": "批量删除",

      // === Connectors Page ===
      "External Connectors": "外部连接器",
      "Manage integrations with CRM, ERP, HR, and other external systems.": "管理与 CRM、ERP、HR 等外部系统的集成。",
      "Connector List": "连接器列表",
      "Connector Configuration": "连接器配置",
      "Connector ID": "连接器 ID",
      "Name": "名称",
      "Domain": "业务域",
      "Kind": "类型",
      "Auth type": "认证方式",
      "Token ref": "令牌引用",
      "Base URL": "基础 URL",
      "Subscribed actions": "订阅动作",
      "Config JSON": "配置 JSON",
      "Enabled": "启用",
      "Save connector": "保存",
      "Health & Diagnostics": "健康检查与诊断",
      "Test bindings": "测试绑定",
      "Check readiness": "检查就绪",
      "Check health": "检查健康",
      "Sync Management": "同步管理",
      "Sync state": "同步状态",
      "Run sync batch": "运行同步",
      "Sync history": "同步历史",

      // === Actions Page ===
      "Business Actions": "业务动作",
      "Execute governed business operations with rule checks and audit trails.": "执行带规则检查和审计轨迹的受控业务操作。",
      "Action List": "动作列表",
      "Execute Action": "执行动作",
      "Action ID": "动作 ID",
      "Target Dataset": "目标数据集",
      "Description": "描述",
      "Input JSON": "输入 JSON",
      "Idempotency Key": "幂等键",
      "Dry-run": "试运行",
      "Execute": "执行",
      "Check rules": "检查规则",
      "Event Contracts": "事件契约",

      // === Common ===
      "Loading...": "加载中...",
      "No data": "暂无数据",
      "Confirm": "确认",
      "Cancel": "取消",
      "Yes": "是",
      "No": "否",
      "Error": "错误",
      "Success": "成功",
      "Save": "保存",
      "Delete": "删除",
      "Edit": "编辑",
      "Create": "创建",
      "Update": "更新",
      "Close": "关闭",
    };
  },

  applyToDOM(root) {
    const walker = document.createTreeWalker(root || document.body, NodeFilter.SHOW_TEXT, {
      acceptNode(node) {
        const tag = node.parentElement?.tagName;
        if (["SCRIPT", "STYLE", "TEXTAREA", "INPUT", "SELECT"].includes(tag)) return NodeFilter.FILTER_REJECT;
        if (!node.nodeValue?.trim()) return NodeFilter.FILTER_REJECT;
        return NodeFilter.FILTER_ACCEPT;
      }
    });
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    nodes.forEach(node => {
      const trimmed = node.nodeValue.trim();
      const translated = this.t(trimmed);
      if (translated !== trimmed) {
        node.nodeValue = node.nodeValue.replace(trimmed, translated);
      }
    });
    // Translate placeholders
    (root || document.body).querySelectorAll("[placeholder]").forEach(el => {
      const ph = el.getAttribute("placeholder");
      const t = this.t(ph);
      if (t !== ph) el.setAttribute("placeholder", t);
    });
  }
};

I18N.init();
