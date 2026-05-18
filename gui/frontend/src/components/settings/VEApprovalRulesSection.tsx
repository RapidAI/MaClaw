import { useState, useCallback } from "react";

// --- Types ---

type Operator =
  | "equals"
  | "not_equals"
  | "greater_than"
  | "less_than"
  | "contains"
  | "in_list"
  | "not_in_list"
  | "is_empty"
  | "is_not_empty";

interface RuleCondition {
  field: string;
  operator: Operator;
  value: any;
}

interface ApprovalRule {
  id: string;
  name: string;
  position: number;
  conditions: RuleCondition[];
  reason?: string;
}

type RuleCategory = "auto_reject" | "auto_approve" | "require_human";

export interface ApprovalRules {
  auto_reject: ApprovalRule[];
  auto_approve: ApprovalRule[];
  require_human: ApprovalRule[];
}

type Props = {
  rules: ApprovalRules;
  onChange: (rules: ApprovalRules) => void;
  lang?: string;
};

// --- Constants ---

const MAX_RULES_PER_CATEGORY = 50;

const OPERATORS: { value: Operator; labelEn: string; labelZh: string }[] = [
  { value: "equals", labelEn: "Equals", labelZh: "等于" },
  { value: "not_equals", labelEn: "Not Equals", labelZh: "不等于" },
  { value: "greater_than", labelEn: "Greater Than", labelZh: "大于" },
  { value: "less_than", labelEn: "Less Than", labelZh: "小于" },
  { value: "contains", labelEn: "Contains", labelZh: "包含" },
  { value: "in_list", labelEn: "In List", labelZh: "在列表中" },
  { value: "not_in_list", labelEn: "Not In List", labelZh: "不在列表中" },
  { value: "is_empty", labelEn: "Is Empty", labelZh: "为空" },
  { value: "is_not_empty", labelEn: "Is Not Empty", labelZh: "不为空" },
];

const CATEGORIES: { key: RuleCategory; labelEn: string; labelZh: string; labelZhHant: string }[] = [
  { key: "auto_reject", labelEn: "Auto-Reject Rules", labelZh: "自动拒绝规则", labelZhHant: "自動拒絕規則" },
  { key: "auto_approve", labelEn: "Auto-Approve Rules", labelZh: "自动通过规则", labelZhHant: "自動通過規則" },
  { key: "require_human", labelEn: "Require-Human Rules", labelZh: "需人工审批规则", labelZhHant: "需人工審批規則" },
];

// --- Helpers ---

const textForLang = (
  lang: string | undefined,
  en: string,
  zhHans: string,
  zhHant = zhHans,
) => (lang === "en" ? en : lang === "zh-Hant" ? zhHant : zhHans);

function generateId(): string {
  return "rule_" + Date.now().toString(36) + "_" + Math.random().toString(36).slice(2, 8);
}

function operatorNeedsValue(op: Operator): boolean {
  return op !== "is_empty" && op !== "is_not_empty";
}

// --- Sub-components ---

function ConditionEditor({
  condition,
  index,
  onChange,
  onRemove,
  lang,
}: {
  condition: RuleCondition;
  index: number;
  onChange: (index: number, cond: RuleCondition) => void;
  onRemove: (index: number) => void;
  lang?: string;
}) {
  return (
    <div className="ve-rule-condition" data-testid={`rule-condition-${index}`}>
      <input
        className="ve-form-input ve-rule-condition-field"
        type="text"
        value={condition.field}
        onChange={(e) => onChange(index, { ...condition, field: e.target.value })}
        placeholder={textForLang(lang, "Field path (e.g. request.amount)", "字段路径（如 request.amount）", "欄位路徑（如 request.amount）")}
        data-testid={`condition-field-${index}`}
        aria-label={textForLang(lang, "Field path", "字段路径", "欄位路徑")}
      />
      <select
        className="ve-form-input ve-form-select ve-rule-condition-operator"
        value={condition.operator}
        onChange={(e) => onChange(index, { ...condition, operator: e.target.value as Operator })}
        data-testid={`condition-operator-${index}`}
        aria-label={textForLang(lang, "Operator", "运算符", "運算符")}
      >
        {OPERATORS.map((op) => (
          <option key={op.value} value={op.value}>
            {lang === "en" ? op.labelEn : op.labelZh}
          </option>
        ))}
      </select>
      {operatorNeedsValue(condition.operator) && (
        <input
          className="ve-form-input ve-rule-condition-value"
          type="text"
          value={condition.value ?? ""}
          onChange={(e) => onChange(index, { ...condition, value: e.target.value })}
          placeholder={textForLang(lang, "Value", "值", "值")}
          data-testid={`condition-value-${index}`}
          aria-label={textForLang(lang, "Value", "值", "值")}
        />
      )}
      <button
        className="ve-btn ve-btn--ghost ve-btn--sm"
        onClick={() => onRemove(index)}
        data-testid={`condition-remove-${index}`}
        aria-label={textForLang(lang, "Remove condition", "移除条件", "移除條件")}
        title={textForLang(lang, "Remove condition", "移除条件", "移除條件")}
      >
        ✕
      </button>
    </div>
  );
}

function RuleEditor({
  rule,
  category,
  onUpdate,
  onRemove,
  onMoveUp,
  onMoveDown,
  isFirst,
  isLast,
  lang,
}: {
  rule: ApprovalRule;
  category: RuleCategory;
  onUpdate: (rule: ApprovalRule) => void;
  onRemove: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  isFirst: boolean;
  isLast: boolean;
  lang?: string;
}) {
  const [expanded, setExpanded] = useState(true);

  function handleConditionChange(index: number, cond: RuleCondition) {
    const updated = [...rule.conditions];
    updated[index] = cond;
    onUpdate({ ...rule, conditions: updated });
  }

  function handleConditionRemove(index: number) {
    const updated = rule.conditions.filter((_, i) => i !== index);
    onUpdate({ ...rule, conditions: updated });
  }

  function handleAddCondition() {
    const newCond: RuleCondition = { field: "", operator: "equals", value: "" };
    onUpdate({ ...rule, conditions: [...rule.conditions, newCond] });
  }

  return (
    <div className="ve-rule-item" data-testid={`rule-item-${rule.id}`}>
      <div className="ve-rule-header">
        <div className="ve-rule-header-left">
          <div className="ve-rule-position-controls">
            <button
              className="ve-btn ve-btn--ghost ve-btn--xs"
              onClick={onMoveUp}
              disabled={isFirst}
              data-testid={`rule-move-up-${rule.id}`}
              aria-label={textForLang(lang, "Move up", "上移", "上移")}
              title={textForLang(lang, "Move up", "上移", "上移")}
            >
              ▲
            </button>
            <button
              className="ve-btn ve-btn--ghost ve-btn--xs"
              onClick={onMoveDown}
              disabled={isLast}
              data-testid={`rule-move-down-${rule.id}`}
              aria-label={textForLang(lang, "Move down", "下移", "下移")}
              title={textForLang(lang, "Move down", "下移", "下移")}
            >
              ▼
            </button>
          </div>
          <button
            className="ve-btn ve-btn--ghost ve-rule-toggle"
            onClick={() => setExpanded(!expanded)}
            aria-expanded={expanded}
            data-testid={`rule-toggle-${rule.id}`}
          >
            {expanded ? "▾" : "▸"}
          </button>
          <input
            className="ve-form-input ve-rule-name-input"
            type="text"
            value={rule.name}
            onChange={(e) => onUpdate({ ...rule, name: e.target.value })}
            placeholder={textForLang(lang, "Rule name", "规则名称", "規則名稱")}
            data-testid={`rule-name-${rule.id}`}
            aria-label={textForLang(lang, "Rule name", "规则名称", "規則名稱")}
          />
        </div>
        <button
          className="ve-btn ve-btn--ghost ve-btn--danger ve-btn--sm"
          onClick={onRemove}
          data-testid={`rule-remove-${rule.id}`}
          aria-label={textForLang(lang, "Delete rule", "删除规则", "刪除規則")}
          title={textForLang(lang, "Delete rule", "删除规则", "刪除規則")}
        >
          {textForLang(lang, "Delete", "删除", "刪除")}
        </button>
      </div>

      {expanded && (
        <div className="ve-rule-body">
          {/* Conditions */}
          <div className="ve-rule-conditions">
            <span className="ve-rule-conditions-label">
              {textForLang(lang, "Conditions:", "条件:", "條件:")}
            </span>
            {rule.conditions.map((cond, idx) => (
              <ConditionEditor
                key={idx}
                condition={cond}
                index={idx}
                onChange={handleConditionChange}
                onRemove={handleConditionRemove}
                lang={lang}
              />
            ))}
            <button
              className="ve-btn ve-btn--secondary ve-btn--sm"
              onClick={handleAddCondition}
              data-testid={`rule-add-condition-${rule.id}`}
            >
              {textForLang(lang, "+ Add Condition", "+ 添加条件", "+ 新增條件")}
            </button>
          </div>

          {/* Reason (only for auto_reject) */}
          {category === "auto_reject" && (
            <div className="ve-rule-reason">
              <label className="ve-form-label ve-rule-reason-label">
                {textForLang(lang, "Rejection Reason:", "拒绝原因:", "拒絕原因:")}
              </label>
              <input
                className="ve-form-input"
                type="text"
                value={rule.reason || ""}
                onChange={(e) => onUpdate({ ...rule, reason: e.target.value })}
                placeholder={textForLang(lang, "Reason for rejection (max 500 chars)", "拒绝原因（最多500字）", "拒絕原因（最多500字）")}
                maxLength={500}
                data-testid={`rule-reason-${rule.id}`}
                aria-label={textForLang(lang, "Rejection reason", "拒绝原因", "拒絕原因")}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// --- Main Component ---

export function VEApprovalRulesSection({ rules, onChange, lang }: Props) {
  const [collapsedCategories, setCollapsedCategories] = useState<Record<RuleCategory, boolean>>({
    auto_reject: false,
    auto_approve: false,
    require_human: false,
  });

  const toggleCategory = useCallback((cat: RuleCategory) => {
    setCollapsedCategories((prev) => ({ ...prev, [cat]: !prev[cat] }));
  }, []);

  function handleAddRule(category: RuleCategory) {
    const categoryRules = rules[category];
    if (categoryRules.length >= MAX_RULES_PER_CATEGORY) return;

    const newRule: ApprovalRule = {
      id: generateId(),
      name: "",
      position: categoryRules.length,
      conditions: [{ field: "", operator: "equals", value: "" }],
      reason: category === "auto_reject" ? "" : undefined,
    };

    const updated = { ...rules, [category]: [...categoryRules, newRule] };
    onChange(updated);
  }

  function handleUpdateRule(category: RuleCategory, ruleId: string, updatedRule: ApprovalRule) {
    const updated = {
      ...rules,
      [category]: rules[category].map((r) => (r.id === ruleId ? updatedRule : r)),
    };
    onChange(updated);
  }

  function handleRemoveRule(category: RuleCategory, ruleId: string) {
    const filtered = rules[category].filter((r) => r.id !== ruleId);
    // Recompute positions
    const reindexed = filtered.map((r, idx) => ({ ...r, position: idx }));
    onChange({ ...rules, [category]: reindexed });
  }

  function handleMoveRule(category: RuleCategory, ruleId: string, direction: "up" | "down") {
    const list = [...rules[category]];
    const idx = list.findIndex((r) => r.id === ruleId);
    if (idx < 0) return;
    const targetIdx = direction === "up" ? idx - 1 : idx + 1;
    if (targetIdx < 0 || targetIdx >= list.length) return;

    // Swap
    [list[idx], list[targetIdx]] = [list[targetIdx], list[idx]];
    // Recompute positions
    const reindexed = list.map((r, i) => ({ ...r, position: i }));
    onChange({ ...rules, [category]: reindexed });
  }

  return (
    <div data-testid="ve-approval-rules-section" className="ve-form-group ve-form-group--nested">
      <h5 className="ve-form-subgroup-title">
        {textForLang(lang, "Three-Way Routing Rules", "三路路由规则", "三路路由規則")}
      </h5>
      <p className="ve-form-hint">
        {textForLang(
          lang,
          "Configure rules for automatic approval decisions. Priority: auto-reject → auto-approve → require-human. Max 50 rules per category.",
          "配置自动审批决策规则。优先级：自动拒绝 → 自动通过 → 需人工审批。每类最多50条规则。",
          "設定自動審批決策規則。優先級：自動拒絕 → 自動通過 → 需人工審批。每類最多50條規則。",
        )}
      </p>

      {CATEGORIES.map(({ key, labelEn, labelZh, labelZhHant }) => {
        const categoryRules = rules[key];
        const isCollapsed = collapsedCategories[key];
        const atLimit = categoryRules.length >= MAX_RULES_PER_CATEGORY;

        return (
          <div
            key={key}
            className="ve-rule-category"
            data-testid={`rule-category-${key}`}
          >
            <div className="ve-rule-category-header">
              <button
                className="ve-btn ve-btn--ghost ve-rule-category-toggle"
                onClick={() => toggleCategory(key)}
                aria-expanded={!isCollapsed}
                data-testid={`rule-category-toggle-${key}`}
              >
                <span className="ve-rule-category-arrow">
                  {isCollapsed ? "▸" : "▾"}
                </span>
                <span className="ve-rule-category-title">
                  {textForLang(lang, labelEn, labelZh, labelZhHant)}
                </span>
                <span className="ve-rule-category-count">
                  ({categoryRules.length}/{MAX_RULES_PER_CATEGORY})
                </span>
              </button>
              <button
                className="ve-btn ve-btn--secondary ve-btn--sm"
                onClick={() => handleAddRule(key)}
                disabled={atLimit}
                data-testid={`rule-add-${key}`}
                aria-label={textForLang(lang, "Add rule", "添加规则", "新增規則")}
              >
                {textForLang(lang, "+ Add Rule", "+ 添加规则", "+ 新增規則")}
              </button>
            </div>

            {!isCollapsed && (
              <div className="ve-rule-category-body">
                {categoryRules.length === 0 && (
                  <p className="ve-form-hint ve-rule-empty-hint" data-testid={`rule-empty-${key}`}>
                    {textForLang(
                      lang,
                      "No rules configured. Requests will fall through to the next category.",
                      "未配置规则。请求将继续匹配下一类别。",
                      "未設定規則。請求將繼續匹配下一類別。",
                    )}
                  </p>
                )}
                {categoryRules.map((rule, idx) => (
                  <RuleEditor
                    key={rule.id}
                    rule={rule}
                    category={key}
                    onUpdate={(updated) => handleUpdateRule(key, rule.id, updated)}
                    onRemove={() => handleRemoveRule(key, rule.id)}
                    onMoveUp={() => handleMoveRule(key, rule.id, "up")}
                    onMoveDown={() => handleMoveRule(key, rule.id, "down")}
                    isFirst={idx === 0}
                    isLast={idx === categoryRules.length - 1}
                    lang={lang}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
