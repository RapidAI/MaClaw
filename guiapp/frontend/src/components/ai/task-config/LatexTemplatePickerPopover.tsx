import { useMemo, useState } from "react";
import type { Theme } from "../aiAssistantPanelTheme";
import { TaskConfigIcon } from "./taskConfigIcons";
import { TaskConfigPopoverShell, popoverItemKeyDown, popoverSearchInputStyle } from "./TaskConfigPopoverShell";
import { popoverHeadStyle, popoverItemStyle, popoverListStyle } from "./taskConfigPopoverStyles";
import {
    isBlankLatexTemplate,
    LATEX_BLANK_TEMPLATE_ID,
    latexBlankTemplateName,
    latexTemplateText,
    type LatexTemplate,
} from "../../../utils/latexTemplates";

export interface LatexTemplatePickerPopoverProps {
    anchor: HTMLElement | null;
    theme: Theme;
    lang?: string;
    templates: LatexTemplate[];
    selectedTemplateId: string | null;
    onSelect: (id: string, name: string) => void;
    onClose: () => void;
}

/** LaTeX 模板弹层。空白模板始终在最前，也是未选择时的默认值。 */
export function LatexTemplatePickerPopover({
    anchor,
    theme: t,
    lang,
    templates,
    selectedTemplateId,
    onSelect,
    onClose,
}: LatexTemplatePickerPopoverProps) {
    const isZh = !lang?.startsWith("en");
    const [query, setQuery] = useState("");
    const q = query.trim().toLowerCase();
    const selectedId = String(selectedTemplateId || "").trim() || LATEX_BLANK_TEMPLATE_ID;

    const choices = useMemo(() => {
        const blankFromList = templates.find((template) => isBlankLatexTemplate(template));
        const blank: LatexTemplate = blankFromList || {
            id: LATEX_BLANK_TEMPLATE_ID,
            name: latexBlankTemplateName(lang || "zh-Hans"),
            description: latexTemplateText(lang || "", "Minimal article skeleton", "从最小骨架开始", "從最小骨架開始"),
        };
        const rest = templates.filter((template) => !isBlankLatexTemplate(template));
        const all = [blank, ...rest];
        if (!q) return all;
        return all.filter((template) => `${template.name} ${template.description || ""}`.toLowerCase().includes(q));
    }, [templates, q, lang]);

    return (
        <TaskConfigPopoverShell
            anchor={anchor}
            theme={t}
            onClose={onClose}
            width={420}
            data-testid="task-config-popover-latex-template"
        >
            <div style={popoverHeadStyle()}>
                <input
                    value={query}
                    onChange={(e) => setQuery(e.currentTarget.value)}
                    placeholder={isZh ? "搜索 LaTeX 模板" : "Search LaTeX templates"}
                    aria-label={isZh ? "搜索 LaTeX 模板" : "Search LaTeX templates"}
                    style={popoverSearchInputStyle(t)}
                    autoFocus
                />
            </div>
            <div style={popoverListStyle()}>
                {choices.map((template) => {
                    const selected = template.id === selectedId;
                    const v = popoverItemStyle(t, { selected });
                    const blank = isBlankLatexTemplate(template);
                    return (
                        <div
                            key={template.id}
                            role="button"
                            tabIndex={0}
                            data-testid={`latex-template-item-${template.id}`}
                            className="mc-taskcfg-item"
                            style={v.item}
                            onClick={() => onSelect(template.id, template.name)}
                            onKeyDown={popoverItemKeyDown(() => onSelect(template.id, template.name))}
                            title={template.description || template.name}
                        >
                            <span style={v.icon}><TaskConfigIcon name={blank ? "file" : "grid"} size={15} /></span>
                            <span style={v.meta}>
                                {template.name}
                                {template.description ? (
                                    <span style={{ ...v.desc, display: "block" }}>{template.description}</span>
                                ) : null}
                            </span>
                            {selected && <TaskConfigIcon name="check" size={14} />}
                        </div>
                    );
                })}
                {choices.length === 0 && (
                    <div style={{ padding: "10px", fontSize: 12.5, color: t.textMuted }}>
                        {latexTemplateText(lang || "", "No matching template", "没有匹配的模板", "沒有匹配的模板")}
                    </div>
                )}
            </div>
        </TaskConfigPopoverShell>
    );
}
