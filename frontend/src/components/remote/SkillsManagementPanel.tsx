import { useState, useEffect, useCallback } from "react";
import {
    ListNLSkills,
    CreateNLSkill,
    UpdateNLSkill,
    DeleteNLSkill,
} from "../../../wailsjs/go/main/App";

interface NLSkillStep {
    action: string;
    params: Record<string, any>;
    on_error: string;
}

interface NLSkillDefinition {
    name: string;
    description: string;
    triggers: string[];
    steps: NLSkillStep[];
    status: string;
    created_at: string;
}

type Props = {
    translate: (key: string) => string;
};

const emptySkill: NLSkillDefinition = {
    name: "",
    description: "",
    triggers: [],
    steps: [],
    status: "active",
    created_at: "",
};

export function SkillsManagementPanel({ translate }: Props) {
    const [skills, setSkills] = useState<NLSkillDefinition[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    // Form state
    const [showForm, setShowForm] = useState(false);
    const [editingSkill, setEditingSkill] = useState<NLSkillDefinition | null>(null);
    const [formData, setFormData] = useState<NLSkillDefinition>({ ...emptySkill });
    const [triggerInput, setTriggerInput] = useState("");
    const [stepsYaml, setStepsYaml] = useState("");
    const [formError, setFormError] = useState("");

    // Delete confirmation
    const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const skillList = await ListNLSkills();
            setSkills(Array.isArray(skillList) ? skillList : []);
        } catch (err) {
            setError(String(err));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        loadData();
    }, [loadData]);

    const stepsToYaml = (steps: NLSkillStep[]): string => {
        if (!steps || steps.length === 0) return "";
        return steps
            .map((s) => {
                const lines = [`- action: "${s.action}"`];
                if (s.params && Object.keys(s.params).length > 0) {
                    lines.push("  params:");
                    for (const [k, v] of Object.entries(s.params)) {
                        lines.push(`    ${k}: ${JSON.stringify(v)}`);
                    }
                }
                if (s.on_error) lines.push(`  on_error: "${s.on_error}"`);
                return lines.join("\n");
            })
            .join("\n");
    };

    const yamlToSteps = (yaml: string): NLSkillStep[] => {
        if (!yaml.trim()) return [];
        const steps: NLSkillStep[] = [];
        let current: Partial<NLSkillStep> | null = null;
        let inParams = false;
        const params: Record<string, any> = {};

        for (const line of yaml.split("\n")) {
            const trimmed = line.trim();
            if (trimmed.startsWith("- action:")) {
                if (current) {
                    steps.push({ action: current.action || "", params: { ...params }, on_error: current.on_error || "stop" });
                }
                current = { action: trimmed.replace("- action:", "").trim().replace(/^"|"$/g, "") };
                inParams = false;
                Object.keys(params).forEach((k) => delete params[k]);
            } else if (trimmed === "params:") {
                inParams = true;
            } else if (trimmed.startsWith("on_error:")) {
                if (current) current.on_error = trimmed.replace("on_error:", "").trim().replace(/^"|"$/g, "");
                inParams = false;
            } else if (inParams && trimmed.includes(":")) {
                const idx = trimmed.indexOf(":");
                const key = trimmed.slice(0, idx).trim();
                let val: any = trimmed.slice(idx + 1).trim();
                try { val = JSON.parse(val); } catch { /* keep as string */ }
                params[key] = val;
            }
        }
        if (current) {
            steps.push({ action: current.action || "", params: { ...params }, on_error: current.on_error || "stop" });
        }
        return steps;
    };

    const openCreateForm = () => {
        setEditingSkill(null);
        setFormData({ ...emptySkill });
        setTriggerInput("");
        setStepsYaml("");
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = (skill: NLSkillDefinition) => {
        setEditingSkill(skill);
        setFormData({ ...skill });
        setTriggerInput("");
        setStepsYaml(stepsToYaml(skill.steps));
        setFormError("");
        setShowForm(true);
    };

    const closeForm = () => {
        setShowForm(false);
        setEditingSkill(null);
        setFormError("");
    };

    const addTrigger = () => {
        const t = triggerInput.trim();
        if (t && !formData.triggers.includes(t)) {
            setFormData({ ...formData, triggers: [...formData.triggers, t] });
        }
        setTriggerInput("");
    };

    const removeTrigger = (idx: number) => {
        setFormData({ ...formData, triggers: formData.triggers.filter((_, i) => i !== idx) });
    };

    const handleTriggerKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter") {
            e.preventDefault();
            addTrigger();
        }
    };

    const handleSubmit = async () => {
        if (!formData.name.trim()) {
            setFormError("名称不能为空");
            return;
        }
        setBusy(true);
        setFormError("");
        try {
            const def = { ...formData, steps: yamlToSteps(stepsYaml) };
            if (editingSkill) {
                await UpdateNLSkill(def);
            } else {
                await CreateNLSkill(def);
            }
            closeForm();
            await loadData();
        } catch (err) {
            setFormError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleDelete = async (name: string) => {
        setBusy(true);
        try {
            await DeleteNLSkill(name);
            setDeleteTarget(null);
            await loadData();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            {/* Header with create and upload buttons */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ fontSize: "0.78rem", color: "#5a6577" }}>
                    {skills.length} {translate("skillsRegistered") || "个已注册 Skill"}
                </span>
                <div style={{ display: "flex", gap: "6px" }}>
                    <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                        + 新建 Skill
                    </button>
                </div>
            </div>

            {/* Loading */}
            {loading && (
                <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: "#8b95a5" }}>
                    加载中...
                </div>
            )}

            {/* Error */}
            {error && (
                <div style={{ fontSize: "0.78rem", color: "#c53030", background: "#fff5f5", padding: "6px 10px", borderRadius: "4px", border: "1px solid #fecdd3" }}>
                    {error}
                </div>
            )}

            {/* Skills table */}
            {!loading && skills.length > 0 && (
                <div style={{ border: "1px solid #e1e4e8", borderRadius: "6px", overflow: "hidden" }}>
                    <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                        <thead>
                            <tr style={{ background: "#f4f5f7" }}>
                                <th style={thStyle}>名称</th>
                                <th style={thStyle}>描述</th>
                                <th style={thStyle}>触发短语</th>
                                <th style={thStyle}>状态</th>
                                <th style={{ ...thStyle, width: "100px" }}>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {skills.map((s) => (
                                <tr key={s.name} style={{ borderTop: "1px solid #e1e4e8" }}>
                                    <td style={tdStyle}>{s.name}</td>
                                    <td style={tdStyle}>{s.description || "—"}</td>
                                    <td style={tdStyle}>
                                        <div style={{ display: "flex", flexWrap: "wrap", gap: "3px" }}>
                                            {(s.triggers || []).map((t, i) => (
                                                <span key={i} style={tagStyle}>{t}</span>
                                            ))}
                                        </div>
                                    </td>
                                    <td style={tdStyle}>
                                        <span style={{ ...statusBadgeStyle, ...(s.status === "active" ? activeBadge : disabledBadge) }}>
                                            {s.status === "active" ? "启用" : s.status}
                                        </span>
                                    </td>
                                    <td style={tdStyle}>
                                        <div style={{ display: "flex", gap: "4px" }}>
                                            <button className="btn-secondary" style={smallBtnStyle} onClick={() => openEditForm(s)} disabled={busy}>编辑</button>
                                            <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={() => setDeleteTarget(s.name)} disabled={busy}>删除</button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {!loading && skills.length === 0 && !error && (
                <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: "#8b95a5" }}>
                    暂无已注册的 Skill
                </div>
            )}

            {/* Delete confirmation dialog */}
            {deleteTarget && (
                <div className="modal-backdrop" onClick={() => setDeleteTarget(null)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "280px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>确认删除</h3>
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: "#5a6577", margin: 0 }}>
                                确定要删除 Skill「{deleteTarget}」吗？此操作不可撤销。
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDeleteTarget(null)} disabled={busy}>取消</button>
                            <button className="btn-secondary btn-danger" onClick={() => handleDelete(deleteTarget)} disabled={busy}>
                                {busy ? "删除中..." : "删除"}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create/Edit form dialog */}
            {showForm && (
                <div className="modal-backdrop" onClick={closeForm}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "420px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{editingSkill ? "编辑 Skill" : "新建 Skill"}</h3>
                            <button className="btn-close" onClick={closeForm}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">名称</label>
                                <input
                                    className="form-input"
                                    value={formData.name}
                                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                    placeholder="skill-name"
                                    disabled={!!editingSkill}
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">描述</label>
                                <input
                                    className="form-input"
                                    value={formData.description}
                                    onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                                    placeholder="Skill 功能描述"
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">触发短语</label>
                                <div style={{ display: "flex", flexWrap: "wrap", gap: "4px", marginBottom: "4px" }}>
                                    {formData.triggers.map((t, i) => (
                                        <span key={i} style={{ ...tagStyle, cursor: "pointer" }} onClick={() => removeTrigger(i)}>
                                            {t} ×
                                        </span>
                                    ))}
                                </div>
                                <div style={{ display: "flex", gap: "4px" }}>
                                    <input
                                        className="form-input"
                                        value={triggerInput}
                                        onChange={(e) => setTriggerInput(e.target.value)}
                                        onKeyDown={handleTriggerKeyDown}
                                        placeholder="输入后按 Enter 添加"
                                        spellCheck={false}
                                        style={{ flex: 1 }}
                                    />
                                    <button className="btn-secondary" style={{ fontSize: "0.76rem", padding: "3px 8px", flexShrink: 0 }} onClick={addTrigger} type="button">
                                        添加
                                    </button>
                                </div>
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">操作步骤 (YAML)</label>
                                <textarea
                                    className="form-input"
                                    value={stepsYaml}
                                    onChange={(e) => setStepsYaml(e.target.value)}
                                    placeholder={'- action: "send_input"\n  params:\n    text: "hello"\n  on_error: "stop"'}
                                    spellCheck={false}
                                    style={{ minHeight: "120px", fontFamily: "monospace", fontSize: "0.76rem", resize: "vertical" }}
                                />
                            </div>
                            {formError && (
                                <div style={{ fontSize: "0.76rem", color: "#c53030", background: "#fff5f5", padding: "4px 8px", borderRadius: "4px" }}>
                                    {formError}
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeForm} disabled={busy}>取消</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleSubmit} disabled={busy}>
                                {busy ? "提交中..." : editingSkill ? "保存" : "创建"}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

/* Inline style constants */
const thStyle: React.CSSProperties = {
    padding: "6px 8px",
    textAlign: "left",
    fontWeight: 600,
    fontSize: "0.74rem",
    color: "#5a6577",
    borderBottom: "1px solid #e1e4e8",
};

const tdStyle: React.CSSProperties = {
    padding: "6px 8px",
    fontSize: "0.76rem",
    color: "#1a202c",
    verticalAlign: "top",
};

const tagStyle: React.CSSProperties = {
    display: "inline-block",
    background: "#f4f5f7",
    border: "1px solid #e1e4e8",
    borderRadius: "999px",
    padding: "1px 8px",
    fontSize: "0.7rem",
    color: "#4f5d75",
};

const statusBadgeStyle: React.CSSProperties = {
    display: "inline-block",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
};

const activeBadge: React.CSSProperties = {
    background: "#f0fdf4",
    color: "#2f855a",
    border: "1px solid #86efac",
};

const disabledBadge: React.CSSProperties = {
    background: "#f4f5f7",
    color: "#8b95a5",
    border: "1px solid #e1e4e8",
};

const smallBtnStyle: React.CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};
