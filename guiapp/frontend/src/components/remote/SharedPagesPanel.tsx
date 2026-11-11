import { useCallback, useEffect, useState } from "react";
import { localizeText } from "../../i18n";
import {
    CreateSharedPage,
    ListSharedPages,
    AppendSharedPageSection,
    EditSharedPageSection,
    GetSharedPage,
    DeleteSharedPage,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";

type Section = { id: string; title: string; body: string; updated_by?: string; updated_at?: string };
type Page = {
    id: string;
    title: string;
    group_id?: string;
    sections: Section[];
    version: number;
    updated_at?: string;
};

/** Versioned shared output pages co-edited by bots and the user. */
export function SharedPagesPanel({ lang }: { lang: string }) {
    const t = useCallback((en: string, zh: string) => localizeText(lang, en, zh, zh), [lang]);
    const [pages, setPages] = useState<Page[]>([]);
    const [selected, setSelected] = useState<Page | null>(null);
    const [title, setTitle] = useState("");
    const [secTitle, setSecTitle] = useState("");
    const [secBody, setSecBody] = useState("");
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    const refresh = useCallback(async () => {
        try {
            const list = await ListSharedPages("");
            setPages(Array.isArray(list) ? (list as Page[]) : []);
        } catch (e: any) {
            setError(String(e?.message || e || "list failed"));
        }
    }, []);

    useEffect(() => { void refresh(); }, [refresh]);

    const open = async (id: string) => {
        try {
            const p = await GetSharedPage(id);
            setSelected(p as Page);
        } catch (e: any) {
            setError(String(e?.message || e));
        }
    };

    const create = async () => {
        const name = title.trim();
        if (!name) return;
        setBusy(true); setError("");
        try {
            const p = await CreateSharedPage(name, "", "user");
            setTitle("");
            setSelected(p as Page);
            await refresh();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally { setBusy(false); }
    };

    const append = async () => {
        if (!selected || !secTitle.trim()) return;
        setBusy(true); setError("");
        try {
            const p = await AppendSharedPageSection(selected.id, secTitle.trim(), secBody, "user");
            setSecTitle(""); setSecBody("");
            setSelected(p as Page);
            await refresh();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally { setBusy(false); }
    };

    const saveSection = async (sec: Section) => {
        if (!selected) return;
        setBusy(true); setError("");
        try {
            const p = await EditSharedPageSection(selected.id, sec.id, sec.title, sec.body, "user");
            setSelected(p as Page);
            await refresh();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally { setBusy(false); }
    };

    const remove = async (id: string) => {
        if (!window.confirm(t("Delete this page? This cannot be undone.", "删除该页面？此操作不可撤销。"))) return;
        setBusy(true); setError("");
        try {
            await DeleteSharedPage(id);
            if (selected?.id === id) setSelected(null);
            await refresh();
        } catch (e: any) {
            setError(String(e?.message || e));
        } finally { setBusy(false); }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: 12, padding: 16 }}>
            <div style={{ fontWeight: 600 }}>{t("Shared Pages", "共享产出页")}</div>
            <div style={{ color: colors.textSecondary, fontSize: 13 }}>
                {t(
                    "Versioned documents that bots in a group and you can co-edit by section. Bots get read/append/edit tools.",
                    "版本化文档，编组内机器人与用户可按章节共写。机器人可调用读取/追加/编辑工具。",
                )}
            </div>
            <div style={{ display: "flex", gap: 8 }}>
                <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("New page title", "新页面标题")}
                    style={{ flex: 1, padding: 8, borderRadius: radius.md, border: `1px solid ${colors.border}` }} />
                <button disabled={busy || !title.trim()} onClick={() => void create()}>{t("Create", "创建")}</button>
            </div>
            {error ? <div style={{ color: colors.danger }}>{error}</div> : null}
            <div style={{ display: "grid", gridTemplateColumns: "240px 1fr", gap: 12, minHeight: 320 }}>
                <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 8 }}>
                    {pages.map((p) => (
                        <div key={p.id} style={{ padding: 8, cursor: "pointer", background: selected?.id === p.id ? colors.dangerBg : "transparent", borderRadius: radius.md }}
                            onClick={() => void open(p.id)}>
                            <div style={{ fontWeight: 600 }}>{p.title}</div>
                            <div style={{ fontSize: 12, color: colors.textSecondary }}>v{p.version}</div>
                            <button style={{ fontSize: 12, marginTop: 4 }} onClick={(e) => { e.stopPropagation(); void remove(p.id); }}>{t("Delete", "删除")}</button>
                        </div>
                    ))}
                    {pages.length === 0 ? <div style={{ color: colors.textSecondary }}>{t("No pages", "暂无页面")}</div> : null}
                </div>
                <div style={{ border: `1px solid ${colors.border}`, borderRadius: radius.md, padding: 12 }}>
                    {!selected ? <div style={{ color: colors.textSecondary }}>{t("Select a page", "选择一个页面")}</div> : (
                        <>
                            <div style={{ fontWeight: 600 }}>{selected.title} <span style={{ color: colors.textSecondary }}>v{selected.version}</span></div>
                            {(selected.sections || []).map((sec) => (
                                <div key={sec.id} style={{ marginTop: 10, padding: 8, border: `1px solid ${colors.border}`, borderRadius: radius.md }}>
                                    <input value={sec.title} onChange={(e) => setSelected({
                                        ...selected,
                                        sections: selected.sections.map((s) => s.id === sec.id ? { ...s, title: e.target.value } : s),
                                    })} style={{ width: "100%", marginBottom: 6 }} />
                                    <textarea value={sec.body} rows={4} onChange={(e) => setSelected({
                                        ...selected,
                                        sections: selected.sections.map((s) => s.id === sec.id ? { ...s, body: e.target.value } : s),
                                    })} style={{ width: "100%" }} />
                                    <div style={{ fontSize: 12, color: colors.textSecondary }}>{sec.updated_by} · {sec.updated_at}</div>
                                    <button disabled={busy} onClick={() => void saveSection(sec)}>{t("Save section", "保存章节")}</button>
                                </div>
                            ))}
                            <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 6 }}>
                                <div style={{ fontWeight: 600 }}>{t("Append section", "追加章节")}</div>
                                <input value={secTitle} onChange={(e) => setSecTitle(e.target.value)} placeholder={t("Section title", "章节标题")} />
                                <textarea value={secBody} onChange={(e) => setSecBody(e.target.value)} rows={3} placeholder={t("Body", "正文")} />
                                <button disabled={busy || !secTitle.trim()} onClick={() => void append()}>{t("Append", "追加")}</button>
                            </div>
                        </>
                    )}
                </div>
            </div>
        </div>
    );
}
