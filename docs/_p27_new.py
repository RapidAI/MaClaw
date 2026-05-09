# P27: ag-UI 协议集成 —— 用对话驱动结构化数据管理
# ================================================================
s = new_slide()
section_header(s, "AG-UI PROTOCOL", "ag-UI — 用对话驱动结构化数据管理",
               "Agent 自然语言交互 → 结构化数据 CRUD，替代传统 MIS/ERP/CRM 前端")

# ===== 顶部标语条 =====
rect(s, Inches(0.8), Inches(2.05), Inches(11.7), Inches(0.65), fill=RGBColor(0x0E,0x1A,0x40), line=ACCENT_BLUE)
mp(s, Inches(1.1), Inches(2.1), Inches(11.2), Inches(0.55), [
    ("一句话录入订单、查询库存、审批报销 —— Agent 理解意图，MIS 结构化存储，ag-UI 实时呈现", 14, ACCENT_CYAN, True),
])

# ===== 左上：传统 MIS vs MaClaw+AG-UI 对比 =====
rect(s, Inches(0.8), Inches(2.9), Inches(5.6), Inches(2.1), fill=DARK_CARD, line=RGBColor(0x1E,0x2A,0x50))
tb(s, Inches(1.1), Inches(3.0), Inches(5), Inches(0.3), "📊 传统 MIS 前端  vs  MaClaw + ag-UI", sz=14, c=ACCENT_CYAN, b=True)
line(s, Inches(1.1), Inches(3.3), Inches(1.0), ACCENT_CYAN, Pt(2))

compare_items = [
    ("❌  传统方式", "表单填写 · 多级菜单 · 培训上岗\n固定报表 · 开发周期长 · 改动需发版", RED),
    ("✅  MaClaw + ag-UI", "自然语言指令 → Agent 自动映射 MIS 操作\n生成式 UI 动态渲染表单 · 零培训上手", GREEN),
]
for i, (title, desc, color) in enumerate(compare_items):
    y = Inches(3.5) + i * Inches(0.72)
    tb(s, Inches(1.1), y, Inches(5.2), Inches(0.25), title, sz=12, c=color, b=True)
    tb(s, Inches(1.3), y + Inches(0.25), Inches(4.8), Inches(0.45), desc, sz=9, c=LIGHT_GRAY)

# ===== 右上：ag-UI 驱动结构化数据的核心交互流程 =====
rect(s, Inches(6.7), Inches(2.9), Inches(5.8), Inches(2.1), fill=DARK_CARD, line=RGBColor(0x1E,0x2A,0x50))
tb(s, Inches(7.0), Inches(3.0), Inches(5), Inches(0.3), "🔄 对话 → 结构化的完整链路", sz=14, c=ORANGE, b=True)
line(s, Inches(7.0), Inches(3.3), Inches(1.0), ORANGE, Pt(2))

flow_steps = [
    ("🗣️ 用户意图", "\"帮我录入一笔Q4采购订单\n供应商华为，金额15.8万\"", ACCENT_CYAN),
    ("🤖 Agent 解析", "NLU 提取实体 → 匹配 business_action\nresolve_intent → sales.order_upsert", ACCENT_BLUE),
    ("📦 MIS 存储", "Schema 校验 → Record 创建\n关联 person_ref / org_ref / file_ref", GREEN),
    ("🖥️ ag-UI 呈现", "生成式 UI 渲染订单卡片\n共享状态实时同步前端", ORANGE),
]
for i, (step, desc, color) in enumerate(flow_steps):
    x = Inches(7.0) + i * Inches(1.35)
    tb(s, x, Inches(3.5), Inches(1.3), Inches(0.25), step, sz=10, c=color, b=True, a=PP_ALIGN.CENTER)
    tb(s, x, Inches(3.8), Inches(1.3), Inches(0.65), desc, sz=8, c=LIGHT_GRAY, a=PP_ALIGN.CENTER)
    if i < 3:
        tb(s, x + Inches(1.25), Inches(3.6), Inches(0.2), Inches(0.2), "→", sz=12, c=MID_GRAY, a=PP_ALIGN.CENTER)

# ===== 左下：ag-UI 六大能力支撑结构化数据 =====
rect(s, Inches(0.8), Inches(5.2), Inches(5.6), Inches(1.95), fill=DARK_CARD, line=RGBColor(0x1E,0x2A,0x50))
tb(s, Inches(1.1), Inches(5.3), Inches(5), Inches(0.3), "⚡ ag-UI 能力 × 结构化数据场景", sz=14, c=ACCENT_BLUE, b=True)
line(s, Inches(1.1), Inches(5.6), Inches(1.0), ACCENT_BLUE, Pt(2))

agui_data_items = [
    ("生成式 UI", "动态渲染表单/卡片/列表/图表\n替代传统 MIS 固定页面", ORANGE),
    ("共享状态", "前端与 MIS 数据双向同步\n实时反映 Record 变更", ACCENT_PURPLE),
    ("前端工具调用", "Agent 调用前端组件完成复杂输入\n日期选择/文件上传/关联选择", ACCENT_CYAN),
]
for i, (title, desc, color) in enumerate(agui_data_items):
    y = Inches(5.75) + i * Inches(0.48)
    tb(s, Inches(1.1), y, Inches(5.2), Inches(0.2), f"● {title}", sz=10, c=color, b=True)
    tb(s, Inches(2.5), y, Inches(3.7), Inches(0.4), desc, sz=9, c=LIGHT_GRAY)

# ===== 右下：MaClaw 结构化数据产品定位 =====
rect(s, Inches(6.7), Inches(5.2), Inches(5.8), Inches(1.95), fill=DARK_CARD, line=ACCENT_BLUE)
tb(s, Inches(7.0), Inches(5.3), Inches(5), Inches(0.3), "🎯 产品核心定位", sz=14, c=WHITE, b=True)
line(s, Inches(7.0), Inches(5.6), Inches(1.0), ACCENT_BLUE, Pt(2))

mp(s, Inches(7.2), Inches(5.75), Inches(5.2), Inches(1.2), [
    ("MIS = 结构化数据存储引擎（后端能力）", 12, YELLOW, True),
    ("ag-UI = 自然语言驱动的前端界面（交互层）", 12, ACCENT_CYAN, True),
    ("", 6, WHITE, False),
    ("MaClaw = Agent 时代的企业信息系统", 16, WHITE, True),
    ("无需 ERP/CRM 前端开发，对话即系统", 11, LIGHT_GRAY, False),
])

print("P27 ag-UI 结构化数据定位 完成")
