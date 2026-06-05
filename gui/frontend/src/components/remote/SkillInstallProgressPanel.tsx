import { useEffect, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import { colors, remoteInfoPanelStyle } from "./styles";

type SkillInstallProgress = {
    skill?: string;
    phase?: string;
    status?: string;
    level?: string;
    percent?: number;
    lang?: string;
};

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

type SkillInstallProgressPanelProps = {
    active: boolean;
    localizeText: LocalizeText;
};

const isTerminalSkillInstallPhase = (phase?: string) =>
    phase === "done" || phase === "scan-complete" || phase === "blocked" || phase === "rejected";

const skillInstallProgressTone = (phase?: string) => {
    if (phase === "blocked" || phase === "rejected") return colors.danger;
    if (phase === "done" || phase === "scan-complete") return colors.success;
    return colors.primary;
};

const localizerForLang = (lang: string | undefined, fallback: LocalizeText): LocalizeText => {
    const normalized = (lang || "").trim().toLowerCase();
    if (normalized === "en" || normalized.startsWith("en-")) return (en) => en;
    if (normalized.startsWith("zh-hant") || normalized.startsWith("zh-tw") || normalized.startsWith("zh-hk")) return (_en, _zhHans, zhHant) => zhHant;
    if (normalized.startsWith("zh")) return (_en, zhHans) => zhHans;
    return fallback;
};

const localizeRiskLevel = (level: string | undefined, text: LocalizeText) => {
    const normalized = (level || "").trim().toLowerCase();
    switch (normalized) {
        case "critical": return text("critical", "严重", "嚴重");
        case "high": return text("high", "高", "高");
        case "medium": return text("medium", "中", "中");
        case "low": return text("low", "低", "低");
        default: return level || "";
    }
};

const localizeStatus = (status: string | undefined, phase: string | undefined, text: LocalizeText) => {
    const trimmed = (status || "").trim();
    const normalized = trimmed.toLowerCase();
    const exact: Record<string, string> = {
        "installing approved skill package.": text("Installing approved skill package.", "正在安装已批准的 Skill 包。", "正在安裝已核准的 Skill 套件。"),
        "skill installed successfully.": text("Skill installed successfully.", "Skill 安装成功。", "Skill 安裝成功。"),
        "security scan did not produce a report; current policy allows installation.": text("Security scan did not produce a report; current policy allows installation.", "安全扫描未生成报告；当前策略允许安装。", "安全掃描未產生報告；目前策略允許安裝。"),
        "security scan did not produce a report. installation blocked by policy.": text("Security scan did not produce a report. Installation blocked by policy.", "安全扫描未生成报告。安装已被策略阻止。", "安全掃描未產生報告。安裝已被策略封鎖。"),
        "security scan passed.": text("Security scan passed.", "安全扫描已通过。", "安全掃描已通過。"),
        "installation rejected.": text("Installation rejected.", "安装已拒绝。", "安裝已拒絕。"),
        "user approved high-risk installation.": text("User approved high-risk installation.", "用户已批准高风险安装。", "使用者已核准高風險安裝。"),
        "security scan recorded risk and allowed installation by current policy.": text("Security scan recorded risk and allowed installation by current policy.", "安全扫描已记录风险，当前策略允许安装。", "安全掃描已記錄風險，目前策略允許安裝。"),
        "security scan recorded risk and allowed installation by trusted marketplace policy.": text("Security scan recorded risk and allowed installation by trusted marketplace policy.", "安全扫描已记录风险，受信能力市场策略允许安装。", "安全掃描已記錄風險，受信能力市場策略允許安裝。"),
        "risk guardrails are off; installation allowed.": text("Risk guardrails are off; installation allowed.", "风险护栏已关闭，允许安装。", "風險護欄已關閉，允許安裝。"),
        "developer mode enabled; security scan will not block installation.": text("Developer mode enabled; security scan will not block installation.", "开发者模式已启用；安全扫描不会阻止安装。", "開發者模式已啟用；安全掃描不會封鎖安裝。"),
        "developer mode enabled; high-risk scan result allowed.": text("Developer mode enabled; high-risk scan result allowed.", "开发者模式已启用；高风险扫描结果已允许。", "開發者模式已啟用；高風險掃描結果已允許。"),
    };
    if (exact[normalized]) return exact[normalized];
    if (normalized.startsWith("security review required:")) {
        const rest = trimmed.slice("Security review required:".length).trim();
        return text("Security review required: " + rest, "需要安全审查：" + rest, "需要安全審查：" + rest);
    }
    if (trimmed) return trimmed;
    switch (phase) {
        case "queued": return text("Queued...", "已加入队列...", "已加入佇列...");
        case "scan-start": return text("Starting security scan...", "正在启动安全扫描...", "正在啟動安全掃描...");
        case "extract": return text("Extracting package...", "正在解压包...", "正在解壓縮套件...");
        case "scanning": return text("Scanning before install...", "安装前扫描中...", "安裝前掃描中...");
        case "awaiting-confirmation": return text("Awaiting confirmation...", "正在等待确认...", "正在等待確認...");
        case "approved": return text("Approved.", "已批准。", "已核准。");
        case "installing": return text("Installing...", "正在安装...", "正在安裝...");
        case "done": return text("Done.", "已完成。", "已完成。");
        case "blocked": return text("Blocked.", "已阻止。", "已封鎖。");
        case "rejected": return text("Rejected.", "已拒绝。", "已拒絕。");
        default: return text("Scanning before install...", "安装前扫描中...", "安裝前掃描中...");
    }
};

export const SkillInstallProgressPanel = ({ active, localizeText }: SkillInstallProgressPanelProps) => {
    const [progress, setProgress] = useState<SkillInstallProgress | null>(null);

    useEffect(() => {
        const cleanup = EventsOn("skill-install-progress", (payload: any) => {
            if (!payload || typeof payload !== "object") return;
            setProgress({
                skill: typeof payload.skill === "string" ? payload.skill : undefined,
                phase: typeof payload.phase === "string" ? payload.phase : undefined,
                status: typeof payload.status === "string" ? payload.status : undefined,
                level: typeof payload.level === "string" ? payload.level : undefined,
                percent: typeof payload.percent === "number" ? Math.max(0, Math.min(100, payload.percent)) : undefined,
                lang: typeof payload.lang === "string" ? payload.lang : undefined,
            });
        });
        return cleanup;
    }, []);

    useEffect(() => {
        if (!progress || !isTerminalSkillInstallPhase(progress.phase)) return;
        const timer = window.setTimeout(() => setProgress(null), 5000);
        return () => window.clearTimeout(timer);
    }, [progress]);

    if (!active || !progress) return null;
    const eventText = localizerForLang(progress.lang, localizeText);
    const terminal = isTerminalSkillInstallPhase(progress.phase);
    const tone = skillInstallProgressTone(progress.phase);
    const baseLabel = progress.skill || eventText("Skill install", "Skill 安装", "Skill 安裝");
    const level = localizeRiskLevel(progress.level, eventText);
    const label = baseLabel + (level ? " - " + eventText("risk", "风险", "風險") + " " + level : "");
    const status = localizeStatus(progress.status, progress.phase, eventText);
    return (
        <div role="status" aria-live="polite" style={{ ...remoteInfoPanelStyle, fontSize: "0.78rem", display: "grid", gap: "6px", textAlign: "left" }}>
            <div style={{ display: "flex", gap: "8px", alignItems: "flex-start", minWidth: 0, textAlign: "left" }}>
                {!terminal && <span style={{ width: "12px", height: "12px", border: "2px solid " + tone, borderTopColor: "transparent", borderRadius: "50%", animation: "spin 1s linear infinite", flex: "0 0 auto" }} />}
                <span style={{ minWidth: 0, color: colors.textSecondary, lineHeight: 1.45, overflowWrap: "anywhere", textAlign: "left" }}>{label}: {status}</span>
            </div>
            <div style={{ height: "4px", borderRadius: "999px", background: colors.border, overflow: "hidden" }} aria-hidden="true">
                <div style={{ width: (progress.percent ?? 25) + "%", height: "100%", background: tone, transition: "width 0.25s ease" }} />
            </div>
        </div>
    );
};
