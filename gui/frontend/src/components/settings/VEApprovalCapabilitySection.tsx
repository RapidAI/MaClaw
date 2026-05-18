import { useEffect, useState, useCallback } from "react";
import {
  GetVEApprovalConfig,
  SaveVEApprovalConfig,
} from "../../../wailsjs/go/main/App";
import { VEApprovalRulesSection, ApprovalRules } from "./VEApprovalRulesSection";

type ACLMode = "whitelist" | "blacklist";

interface AccessControlList {
  mode: ACLMode;
  departments: string[];
  roles: string[];
  skills: string[];
  entities: string[];
}

interface VEApprovalConfig {
  enabled: boolean;
  acl: AccessControlList;
  rules: ApprovalRules;
  max_queue_size: number;
  timeout_hours: number;
  daily_quota: number;
  fallback_approver: string;
}

type Props = {
  lang?: string;
};

const textForLang = (
  lang: string | undefined,
  en: string,
  zhHans: string,
  zhHant = zhHans,
) => (lang === "en" ? en : lang === "zh-Hant" ? zhHant : zhHans);

function defaultConfig(): VEApprovalConfig {
  return {
    enabled: false,
    acl: {
      mode: "whitelist",
      departments: [],
      roles: [],
      skills: [],
      entities: [],
    },
    rules: {
      auto_reject: [],
      auto_approve: [],
      require_human: [],
    },
    max_queue_size: 50,
    timeout_hours: 24,
    daily_quota: 100,
    fallback_approver: "",
  };
}

export function VEApprovalCapabilitySection({ lang }: Props) {
  const [config, setConfig] = useState<VEApprovalConfig>(defaultConfig());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [saveSuccess, setSaveSuccess] = useState(false);

  // ACL text area inputs
  const [deptInput, setDeptInput] = useState("");
  const [rolesInput, setRolesInput] = useState("");
  const [skillsInput, setSkillsInput] = useState("");
  const [entitiesInput, setEntitiesInput] = useState("");

  const loadConfig = useCallback(async () => {
    try {
      const resp = await GetVEApprovalConfig();
      if (resp) {
        const cfg: VEApprovalConfig = {
          enabled: resp.enabled ?? false,
          acl: {
            mode: resp.acl?.mode || "whitelist",
            departments: resp.acl?.departments || [],
            roles: resp.acl?.roles || [],
            skills: resp.acl?.skills || [],
            entities: resp.acl?.entities || [],
          },
          rules: resp.rules || { auto_reject: [], auto_approve: [], require_human: [] },
          max_queue_size: resp.max_queue_size ?? 50,
          timeout_hours: resp.timeout_hours ?? 24,
          daily_quota: resp.daily_quota ?? 100,
          fallback_approver: resp.fallback_approver || "",
        };
        setConfig(cfg);
        setDeptInput((cfg.acl.departments || []).join("\n"));
        setRolesInput((cfg.acl.roles || []).join("\n"));
        setSkillsInput((cfg.acl.skills || []).join("\n"));
        setEntitiesInput((cfg.acl.entities || []).join("\n"));
      }
    } catch {
      // Use defaults on error
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  function parseTextArea(text: string): string[] {
    return text
      .split("\n")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
  }

  async function handleSave() {
    setSaving(true);
    setSaveError("");
    setSaveSuccess(false);

    const toSave: VEApprovalConfig = {
      ...config,
      acl: {
        ...config.acl,
        departments: parseTextArea(deptInput),
        roles: parseTextArea(rolesInput),
        skills: parseTextArea(skillsInput),
        entities: parseTextArea(entitiesInput),
      },
    };

    try {
      await SaveVEApprovalConfig(toSave);
      setConfig(toSave);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (err: any) {
      setSaveError(err?.message || String(err || "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  function handleToggleEnabled(enabled: boolean) {
    setConfig((prev) => ({ ...prev, enabled }));
  }

  function handleACLModeChange(mode: ACLMode) {
    setConfig((prev) => ({ ...prev, acl: { ...prev.acl, mode } }));
  }

  function handleNumberChange(field: "max_queue_size" | "timeout_hours" | "daily_quota", value: string) {
    const num = parseInt(value, 10);
    if (!isNaN(num)) {
      setConfig((prev) => ({ ...prev, [field]: num }));
    }
  }

  function handleFallbackChange(value: string) {
    setConfig((prev) => ({ ...prev, fallback_approver: value }));
  }

  function handleRulesChange(updatedRules: ApprovalRules) {
    setConfig((prev) => ({ ...prev, rules: updatedRules }));
  }

  if (loading) {
    return (
      <div data-testid="ve-approval-section" className="ve-form-group">
        <h4 className="ve-form-group-title">
          {textForLang(lang, "Approval Capability", "审批能力", "審批能力")}
        </h4>
        <p className="ve-form-hint">
          {textForLang(lang, "Loading...", "加载中...", "載入中...")}
        </p>
      </div>
    );
  }

  return (
    <div data-testid="ve-approval-section" className="ve-form-group">
      <h4 className="ve-form-group-title">
        {textForLang(lang, "Approval Capability", "审批能力", "審批能力")}
      </h4>

      {/* Enable/Disable Toggle */}
      <div className="ve-form-row">
        <label className="ve-form-label" htmlFor="ve-approval-enabled">
          {textForLang(lang, "Enable Approval", "启用审批", "啟用審批")}
        </label>
        <div className="ve-form-field">
          <label className="ve-toggle">
            <input
              id="ve-approval-enabled"
              data-testid="ve-approval-enabled-toggle"
              type="checkbox"
              checked={config.enabled}
              onChange={(e) => handleToggleEnabled(e.target.checked)}
            />
            <span className="ve-toggle-label">
              {config.enabled
                ? textForLang(lang, "Enabled", "已启用", "已啟用")
                : textForLang(lang, "Disabled", "已禁用", "已停用")}
            </span>
          </label>
        </div>
      </div>

      {config.enabled && (
        <>
          {/* ACL Configuration */}
          <div className="ve-form-group ve-form-group--nested">
            <h5 className="ve-form-subgroup-title">
              {textForLang(lang, "Access Control List", "访问控制列表", "存取控制列表")}
            </h5>

            <div className="ve-form-row">
              <label className="ve-form-label" htmlFor="ve-approval-acl-mode">
                {textForLang(lang, "Mode", "模式", "模式")}
              </label>
              <div className="ve-form-field">
                <select
                  id="ve-approval-acl-mode"
                  data-testid="ve-approval-acl-mode"
                  className="ve-form-input ve-form-select"
                  value={config.acl.mode}
                  onChange={(e) => handleACLModeChange(e.target.value as ACLMode)}
                >
                  <option value="whitelist">
                    {textForLang(lang, "Whitelist (only listed can submit)", "白名单（仅列出的可提交）", "白名單（僅列出的可提交）")}
                  </option>
                  <option value="blacklist">
                    {textForLang(lang, "Blacklist (all except listed can submit)", "黑名单（除列出的外均可提交）", "黑名單（除列出的外均可提交）")}
                  </option>
                </select>
              </div>
            </div>

            <div className="ve-form-row ve-form-row--top">
              <label className="ve-form-label" htmlFor="ve-approval-acl-departments">
                {textForLang(lang, "Departments", "部门", "部門")}
              </label>
              <div className="ve-form-field">
                <textarea
                  id="ve-approval-acl-departments"
                  data-testid="ve-approval-acl-departments"
                  className="ve-form-input ve-form-textarea"
                  rows={3}
                  value={deptInput}
                  onChange={(e) => setDeptInput(e.target.value)}
                  placeholder={textForLang(lang, "One department per line (max 100)", "每行一个部门（最多100个）", "每行一個部門（最多100個）")}
                />
              </div>
            </div>

            <div className="ve-form-row ve-form-row--top">
              <label className="ve-form-label" htmlFor="ve-approval-acl-roles">
                {textForLang(lang, "Roles", "角色", "角色")}
              </label>
              <div className="ve-form-field">
                <textarea
                  id="ve-approval-acl-roles"
                  data-testid="ve-approval-acl-roles"
                  className="ve-form-input ve-form-textarea"
                  rows={3}
                  value={rolesInput}
                  onChange={(e) => setRolesInput(e.target.value)}
                  placeholder={textForLang(lang, "One role per line (max 100)", "每行一个角色（最多100个）", "每行一個角色（最多100個）")}
                />
              </div>
            </div>

            <div className="ve-form-row ve-form-row--top">
              <label className="ve-form-label" htmlFor="ve-approval-acl-skills">
                {textForLang(lang, "Skills", "技能", "技能")}
              </label>
              <div className="ve-form-field">
                <textarea
                  id="ve-approval-acl-skills"
                  data-testid="ve-approval-acl-skills"
                  className="ve-form-input ve-form-textarea"
                  rows={3}
                  value={skillsInput}
                  onChange={(e) => setSkillsInput(e.target.value)}
                  placeholder={textForLang(lang, "One skill per line (max 100)", "每行一个技能（最多100个）", "每行一個技能（最多100個）")}
                />
              </div>
            </div>

            <div className="ve-form-row ve-form-row--top">
              <label className="ve-form-label" htmlFor="ve-approval-acl-entities">
                {textForLang(lang, "Entities (User/VE IDs)", "实体（用户/VE ID）", "實體（用戶/VE ID）")}
              </label>
              <div className="ve-form-field">
                <textarea
                  id="ve-approval-acl-entities"
                  data-testid="ve-approval-acl-entities"
                  className="ve-form-input ve-form-textarea"
                  rows={3}
                  value={entitiesInput}
                  onChange={(e) => setEntitiesInput(e.target.value)}
                  placeholder={textForLang(lang, "One user/VE ID per line (max 100)", "每行一个用户/VE ID（最多100个）", "每行一個用戶/VE ID（最多100個）")}
                />
              </div>
            </div>
          </div>

          {/* Operational Limits */}
          <div className="ve-form-group ve-form-group--nested">
            <h5 className="ve-form-subgroup-title">
              {textForLang(lang, "Operational Limits", "运行限制", "運行限制")}
            </h5>

            <div className="ve-form-row">
              <label className="ve-form-label" htmlFor="ve-approval-max-queue">
                {textForLang(lang, "Max Queue Size", "最大队列大小", "最大佇列大小")}
              </label>
              <div className="ve-form-field">
                <input
                  id="ve-approval-max-queue"
                  data-testid="ve-approval-max-queue"
                  className="ve-form-input"
                  type="number"
                  min={1}
                  max={1000}
                  value={config.max_queue_size}
                  onChange={(e) => handleNumberChange("max_queue_size", e.target.value)}
                />
                <span className="ve-form-hint-inline">
                  {textForLang(lang, "(1 - 1000)", "（1 - 1000）", "（1 - 1000）")}
                </span>
              </div>
            </div>

            <div className="ve-form-row">
              <label className="ve-form-label" htmlFor="ve-approval-timeout">
                {textForLang(lang, "Timeout (hours)", "超时时间（小时）", "逾時時間（小時）")}
              </label>
              <div className="ve-form-field">
                <input
                  id="ve-approval-timeout"
                  data-testid="ve-approval-timeout"
                  className="ve-form-input"
                  type="number"
                  min={1}
                  max={720}
                  value={config.timeout_hours}
                  onChange={(e) => handleNumberChange("timeout_hours", e.target.value)}
                />
                <span className="ve-form-hint-inline">
                  {textForLang(lang, "(1 - 720 hours)", "（1 - 720 小时）", "（1 - 720 小時）")}
                </span>
              </div>
            </div>

            <div className="ve-form-row">
              <label className="ve-form-label" htmlFor="ve-approval-daily-quota">
                {textForLang(lang, "Daily Quota", "每日配额", "每日配額")}
              </label>
              <div className="ve-form-field">
                <input
                  id="ve-approval-daily-quota"
                  data-testid="ve-approval-daily-quota"
                  className="ve-form-input"
                  type="number"
                  min={1}
                  max={10000}
                  value={config.daily_quota}
                  onChange={(e) => handleNumberChange("daily_quota", e.target.value)}
                />
                <span className="ve-form-hint-inline">
                  {textForLang(lang, "(1 - 10000)", "（1 - 10000）", "（1 - 10000）")}
                </span>
              </div>
            </div>
          </div>

          {/* Fallback Approver */}
          <div className="ve-form-group ve-form-group--nested">
            <h5 className="ve-form-subgroup-title">
              {textForLang(lang, "Fallback Approver", "备用审批人", "備用審批人")}
            </h5>

            <div className="ve-form-row">
              <label className="ve-form-label" htmlFor="ve-approval-fallback">
                {textForLang(lang, "Fallback Approver ID", "备用审批人 ID", "備用審批人 ID")}
              </label>
              <div className="ve-form-field">
                <input
                  id="ve-approval-fallback"
                  data-testid="ve-approval-fallback"
                  className="ve-form-input"
                  type="text"
                  value={config.fallback_approver}
                  onChange={(e) => handleFallbackChange(e.target.value)}
                  placeholder={textForLang(lang, "VE or user ID (optional)", "VE 或用户 ID（可选）", "VE 或用戶 ID（可選）")}
                />
                <p className="ve-form-hint">
                  {textForLang(
                    lang,
                    "When the queue is full or timeout is reached, requests will be routed to this approver.",
                    "当队列已满或超时时，请求将路由到此审批人。",
                    "當佇列已滿或逾時時，請求將路由到此審批人。",
                  )}
                </p>
              </div>
            </div>
          </div>

          {/* Three-Way Routing Rules */}
          <VEApprovalRulesSection
            rules={config.rules}
            onChange={handleRulesChange}
            lang={lang}
          />

          {/* Save Button */}
          <div className="ve-form-actions">
            {saveError && (
              <span data-testid="ve-approval-save-error" className="ve-form-error" role="alert">
                {saveError}
              </span>
            )}
            {saveSuccess && (
              <span data-testid="ve-approval-save-success" className="ve-notice ve-notice--success" role="status">
                {textForLang(lang, "Saved successfully", "保存成功", "儲存成功")}
              </span>
            )}
            <button
              className="ve-btn ve-btn--primary"
              onClick={handleSave}
              disabled={saving}
              data-testid="ve-approval-save-btn"
            >
              {saving
                ? "..."
                : textForLang(lang, "Save Approval Settings", "保存审批设置", "儲存審批設定")}
            </button>
          </div>
        </>
      )}

      {!config.enabled && (
        <div className="ve-form-actions">
          <button
            className="ve-btn ve-btn--primary"
            onClick={handleSave}
            disabled={saving}
            data-testid="ve-approval-save-btn"
          >
            {saving
              ? "..."
              : textForLang(lang, "Save Approval Settings", "保存审批设置", "儲存審批設定")}
          </button>
        </div>
      )}
    </div>
  );
}
