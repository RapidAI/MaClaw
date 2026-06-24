import { colors } from "./styles";
import { inputStyle } from "./LLMConfigPanelShared";

type ProviderModelOption = { id: string; name: string };

type Props = {
    selectedIdx: number | null;
    value: string;
    models: ProviderModelOption[];
    fetching: boolean;
    error: string | null;
    open: boolean;
    canFetch: boolean;
    onOpenChange: (open: boolean) => void;
    onChange: (value: string) => void;
    onFetch: () => void;
    t: (en: string, zhHans: string, zhHant?: string) => string;
};

export function ProviderModelCombobox({
    selectedIdx,
    value,
    models,
    fetching,
    error,
    open,
    canFetch,
    onOpenChange,
    onChange,
    onFetch,
    t,
}: Props) {
    const optionsId = `llm-provider-model-options-${selectedIdx ?? "none"}`;
    const query = String(value || "").trim().toLowerCase();
    const filteredModels = query
        ? models.filter(m => {
            const id = String(m.id || "").toLowerCase();
            const name = String(m.name || "").toLowerCase();
            return id.includes(query) || name.includes(query);
        })
        : models;
    const visibleModels = filteredModels.length > 0 ? filteredModels : models;
    const hasModels = models.length > 0;

    return (
        <div
            style={{ position: "relative" }}
            onBlur={e => {
                const nextFocus = e.relatedTarget as Node | null;
                if (!nextFocus || !e.currentTarget.contains(nextFocus)) onOpenChange(false);
            }}
        >
            <div style={{ display: "flex", gap: 4, alignItems: "center" }}>
                <input
                    type="text"
                    style={{ ...inputStyle, flex: 1 }}
                    role="combobox"
                    aria-autocomplete="list"
                    aria-haspopup="listbox"
                    aria-expanded={open && hasModels}
                    aria-controls={optionsId}
                    value={value}
                    onChange={e => {
                        onChange(e.target.value);
                        if (hasModels) onOpenChange(true);
                    }}
                    onFocus={() => {
                        if (hasModels) onOpenChange(true);
                    }}
                    onKeyDown={e => {
                        if (e.key === "Escape") onOpenChange(false);
                        if (e.key === "ArrowDown" && hasModels) {
                            onOpenChange(true);
                            window.setTimeout(() => {
                                document
                                    .getElementById(optionsId)
                                    ?.querySelector<HTMLButtonElement>(".llm-provider-model-option")
                                    ?.focus();
                            }, 0);
                        }
                        if (e.key === "Enter" && hasModels) onOpenChange(true);
                    }}
                    placeholder={fetching
                        ? t("Loading...", "加载中...")
                        : hasModels
                            ? t("Select or type model name", "选择或输入模型名称", "選擇或輸入模型名稱")
                            : t("Type model name or click Fetch", "输入模型名称或点击《获取》", "輸入模型名稱或點擊《獲取》")}
                    disabled={fetching}
                    autoCapitalize="off"
                    autoCorrect="off"
                    spellCheck={false}
                    autoComplete="off"
                />
                <button
                    type="button"
                    aria-label={t("Show model list", "显示模型列表", "顯示模型列表")}
                    aria-haspopup="listbox"
                    aria-expanded={open && hasModels}
                    disabled={fetching || !hasModels}
                    onClick={() => onOpenChange(hasModels ? !open : false)}
                    style={{
                        width: 32, alignSelf: "stretch", cursor: (fetching || !hasModels) ? "not-allowed" : "pointer",
                        background: colors.surface, color: colors.text,
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                        opacity: (fetching || !hasModels) ? 0.5 : 1,
                    }}
                >
                    v
                </button>
                <button
                    type="button"
                    onClick={onFetch}
                    disabled={fetching || !canFetch}
                    style={{
                        fontSize: "0.72rem", padding: "6px 10px", cursor: (fetching || !canFetch) ? "not-allowed" : "pointer",
                        background: colors.surface, color: colors.text,
                        border: `1px solid ${colors.border}`, borderRadius: 4,
                        whiteSpace: "nowrap", flexShrink: 0,
                        opacity: (fetching || !canFetch) ? 0.5 : 1,
                    }}
                    title={t("Fetch available models from provider", "从服务商获取可用模型列表")}
                >
                    {fetching ? t("Loading...", "加载中...") : t("Fetch", "获取", "獲取")}
                </button>
            </div>
            {open && hasModels && (
                <div
                    id={optionsId}
                    role="listbox"
                    style={{
                        position: "absolute",
                        top: "calc(100% + 4px)",
                        left: 0,
                        right: 0,
                        zIndex: 20,
                        maxHeight: 220,
                        overflow: "auto",
                        padding: 4,
                        border: `1px solid ${colors.border}`,
                        borderRadius: 4,
                        background: colors.surface,
                        boxShadow: "0 8px 20px rgba(15, 23, 42, 0.14)",
                    }}
                >
                    {visibleModels.map(m => (
                        <button
                            key={m.id}
                            type="button"
                            className="llm-provider-model-option"
                            role="option"
                            aria-selected={m.id === value}
                            onMouseDown={e => e.preventDefault()}
                            onClick={() => {
                                onChange(m.id);
                                onOpenChange(false);
                            }}
                            style={{
                                width: "100%",
                                display: "flex",
                                flexDirection: "column",
                                alignItems: "flex-start",
                                gap: 2,
                                padding: "8px 10px",
                                border: 0,
                                borderRadius: 4,
                                background: m.id === value ? colors.primaryLight : "transparent",
                                color: colors.text,
                                cursor: "pointer",
                                textAlign: "left",
                            }}
                        >
                            <span style={{ fontSize: "0.82rem", fontWeight: 700 }}>{m.id}</span>
                            {m.name && m.name !== m.id && (
                                <span style={{ color: colors.textMuted, fontSize: "0.72rem" }}>{m.name}</span>
                            )}
                        </button>
                    ))}
                </div>
            )}
            {error && (
                <div style={{ fontSize: "0.68rem", color: colors.danger, marginTop: 4 }}>
                    {error}
                </div>
            )}
        </div>
    );
}
