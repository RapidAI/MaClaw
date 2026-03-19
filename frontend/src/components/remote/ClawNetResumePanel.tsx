import { useState, useEffect, useCallback, useRef } from "react";
import {
    ClawNetGetResume,
    ClawNetUpdateResume,
    ClawNetGetProfile,
    ClawNetUpdateProfile,
    ClawNetSetMotto,
    ClawNetSearchKnowledge,
} from "../../../wailsjs/go/main/App";
import { colors, radius } from "./styles";
import { cnCard, cnLabel, cnHeading, cnInput, cnActionBtn, cnTabStyle } from "./clawnetStyles";

type Props = { lang: string; clawNetRunning: boolean };

export function ClawNetResumePanel({ lang, clawNetRunning }: Props) {
    const zh = lang?.startsWith("zh");
    const [tab, setTab] = useState<"resume" | "search">("resume");

    // Resume
    const [skills, setSkills] = useState("");
    const [domains, setDomains] = useState("");
    const [bio, setBio] = useState("");
    const [name, setName] = useState("");
    const [motto, setMotto] = useState("");
    const [saving, setSaving] = useState(false);
    const [msg, setMsg] = useState("");
    const [loaded, setLoaded] = useState(false);

    // Search
    const [query, setQuery] = useState("");
    const [results, setResults] = useState<any[]>([]);
    const [searching, setSearching] = useState(false);

    const mountedRef = useRef(true);
    useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false; }; }, []);

    const loadResume = useCallback(async () => {
        if (!clawNetRunning) return;
        try {
            const [rRes, pRes] = await Promise.all([ClawNetGetResume(), ClawNetGetProfile()]);
            if (!mountedRef.current) return;
            if (rRes.ok && rRes.resume) {
                const r = rRes.resume as any;
                setSkills((r.skills || []).join(", "));
                setDomains((r.domains || []).join(", "));
                setBio(r.bio || "");
            }
            if (pRes.ok && pRes.profile) {
                const p = pRes.profile as any;
                setName(p.name || "");
                setMotto(p.motto || "");
            }
            setLoaded(true);
        } catch {}
    }, [clawNetRunning]);

    useEffect(() => { if (tab === "resume" && !loaded) loadResume(); }, [tab, loaded, loadResume]);

    const handleSave = async () => {
        setSaving(true); setMsg("");
        try {
            const skillList = skills.split(",").map(s => s.trim()).filter(Boolean);
            const domainList = domains.split(",").map(s => s.trim()).filter(Boolean);
            const saveResults = await Promise.all([
                ClawNetUpdateResume(skillList, domainList, bio.trim()),
                name.trim() ? ClawNetUpdateProfile(name.trim(), bio.trim()) : Promise.resolve({ ok: true }),
                motto.trim() ? ClawNetSetMotto(motto.trim()) : Promise.resolve({ ok: true }),
            ]);
            const failed = saveResults.find((r: any) => !r.ok);
            if (failed) setMsg(`❌ ${(failed as any).error}`);
            else setMsg(zh ? "✅ 已保存" : "✅ Saved");
        } catch (e: any) { setMsg(`❌ ${e.message}`); }
        setSaving(false);
    };

    const doSearch = async () => {
        if (!query.trim()) return;
        setSearching(true); setResults([]);
        try {
            const res = await ClawNetSearchKnowledge(query.trim());
            if (mountedRef.current && res.ok) setResults(res.entries as any[] || []);
        } catch {}
        setSearching(false);
    };

    if (!clawNetRunning) return <div style={cnLabel}>{zh ? "虾网未连接" : "ClawNet not connected"}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={cnTabStyle(tab === "resume")} onClick={() => setTab("resume")}>📋 {zh ? "简历" : "Resume"}</button>
                <button style={cnTabStyle(tab === "search")} onClick={() => setTab("search")}>🔍 {zh ? "全文搜索" : "Search"}</button>
            </div>

            {tab === "resume" && (
                <div>
                    <div style={cnCard}>
                        <div style={cnHeading}>👤 {zh ? "个人资料" : "Profile"}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "名称" : "Name"}</div>
                            <input value={name} onChange={e => setName(e.target.value)} placeholder={zh ? "你的名称" : "Your name"} style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "座右铭" : "Motto"}</div>
                            <input value={motto} onChange={e => setMotto(e.target.value)} placeholder={zh ? "一句话介绍" : "One-liner"} style={cnInput} />
                        </div>
                    </div>
                    <div style={cnCard}>
                        <div style={cnHeading}>🛠 {zh ? "技能与领域" : "Skills & Domains"}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "技能（逗号分隔）" : "Skills (comma separated)"}</div>
                            <input value={skills} onChange={e => setSkills(e.target.value)}
                                placeholder="research, coding, translation, analysis" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "领域（逗号分隔）" : "Domains (comma separated)"}</div>
                            <input value={domains} onChange={e => setDomains(e.target.value)}
                                placeholder="AI, web-dev, data-science" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{zh ? "简介" : "Bio"}</div>
                            <textarea value={bio} onChange={e => setBio(e.target.value)}
                                placeholder={zh ? "描述你的能力和特长..." : "Describe your capabilities..."}
                                style={{ ...cnInput, minHeight: "60px", resize: "vertical" }} />
                        </div>
                        <button style={cnActionBtn(saving)} onClick={handleSave} disabled={saving}>
                            {saving ? "..." : (zh ? "保存简历" : "Save Resume")}
                        </button>
                        {msg && <div style={{ fontSize: "0.72rem", marginTop: "6px", color: msg.startsWith("✅") ? colors.success : colors.danger }}>{msg}</div>}
                    </div>
                </div>
            )}

            {tab === "search" && (
                <div>
                    <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                        <input value={query} onChange={e => setQuery(e.target.value)}
                            placeholder={zh ? "搜索知识、文档、Agent..." : "Search knowledge, docs, agents..."}
                            style={{ ...cnInput, flex: 1 }} onKeyDown={e => e.key === "Enter" && doSearch()} />
                        <button style={cnActionBtn(searching || !query.trim())} onClick={doSearch} disabled={searching || !query.trim()}>
                            {searching ? "..." : (zh ? "搜索" : "Search")}
                        </button>
                    </div>
                    {results.map((r: any, i: number) => (
                        <div key={i} style={cnCard}>
                            <div style={{ fontSize: "0.76rem", fontWeight: 600, color: colors.text }}>{r.title}</div>
                            {r.body && <div style={{ fontSize: "0.72rem", color: colors.textSecondary, marginTop: "4px", maxHeight: "80px", overflow: "auto", whiteSpace: "pre-wrap" }}>{r.body}</div>}
                            <div style={{ display: "flex", gap: "8px", marginTop: "4px" }}>
                                {r.author && <span style={{ fontSize: "0.65rem", color: colors.textMuted }}>{(r.author || "").slice(0, 12)}…</span>}
                                {r.domains?.map((d: string) => <span key={d} style={{ fontSize: "0.65rem", padding: "1px 6px", background: colors.accentBg, borderRadius: radius.pill, color: colors.textSecondary }}>{d}</span>)}
                            </div>
                        </div>
                    ))}
                    {!searching && results.length === 0 && query && <div style={cnLabel}>{zh ? "无结果" : "No results"}</div>}
                </div>
            )}
        </div>
    );
}
