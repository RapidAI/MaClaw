import { colors } from "./styles";
import { SuggestCombobox } from "../ui/SuggestCombobox";

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
    const hasModels = models.length > 0;
    return (
        <div>
            <div style={{ display: "flex", gap: 4, alignItems: "center" }}>
                <SuggestCombobox
                    listboxId={`llm-provider-model-options-${selectedIdx ?? "none"}`}
                    value={value}
                    options={models.map((model) => ({
                        value: model.id,
                        description: model.name && model.name !== model.id ? model.name : undefined,
                    }))}
                    open={open}
                    onOpenChange={onOpenChange}
                    onChange={onChange}
                    disabled={fetching}
                    openOnEnter
                    toggleLabel={t("Show model list", "显示模型列表", "顯示模型列表")}
                    emptyText={t("No matching models", "没有匹配的模型", "沒有匹配的模型")}
                    placeholder={fetching
                        ? t("Loading...", "加载中...")
                        : hasModels
                            ? t("Select or type model name", "选择或输入模型名称", "選擇或輸入模型名稱")
                            : t("Type model name or click Fetch", "输入模型名称或点击《获取》", "輸入模型名稱或點擊《獲取》")}
                    shellStyle={{ flex: 1, minWidth: 0, width: "auto" }}
                    inputStyle={{
                        flex: 1,
                        minWidth: 0,
                        padding: "7px 10px",
                        fontSize: "0.8rem",
                        border: `1px solid ${colors.border}`,
                        borderRadius: 4,
                        background: colors.surface,
                        color: colors.text,
                        boxSizing: "border-box",
                    }}
                />
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
            {error && (
                <div style={{ fontSize: "0.68rem", color: colors.danger, marginTop: 4 }}>
                    {error}
                </div>
            )}
        </div>
    );
}
