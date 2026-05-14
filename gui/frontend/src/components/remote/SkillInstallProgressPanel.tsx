import { useEffect, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import { colors, remoteInfoPanelStyle } from "./styles";

type SkillInstallProgress = {
    skill?: string;
    phase?: string;
    status?: string;
    level?: string;
    percent?: number;
};

const isTerminalSkillInstallPhase = (phase?: string) =>
    phase === "done" || phase === "scan-complete" || phase === "blocked" || phase === "rejected";

const skillInstallProgressTone = (phase?: string) => {
    if (phase === "blocked" || phase === "rejected") return colors.danger;
    if (phase === "done" || phase === "scan-complete") return colors.success;
    return colors.primary;
};

export const SkillInstallProgressPanel = ({ active }: { active: boolean }) => {
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
    const terminal = isTerminalSkillInstallPhase(progress.phase);
    const tone = skillInstallProgressTone(progress.phase);
    const label = (progress.skill || "Skill install") + (progress.level ? ` - risk ${progress.level}` : "");
    return (
        <div style={{ ...remoteInfoPanelStyle, fontSize: "0.78rem", display: "grid", gap: "6px" }}>
            <div style={{ display: "flex", gap: "8px", alignItems: "center", minWidth: 0 }}>
                {!terminal && <span style={{ width: "12px", height: "12px", border: `2px solid ${tone}`, borderTopColor: "transparent", borderRadius: "50%", animation: "spin 1s linear infinite", flex: "0 0 auto" }} />}
                <span style={{ color: colors.textSecondary, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{label}: {progress.status || "Scanning before install..."}</span>
            </div>
            <div style={{ height: "4px", borderRadius: "999px", background: colors.border, overflow: "hidden" }} aria-hidden="true">
                <div style={{ width: `${progress.percent ?? 25}%`, height: "100%", background: tone, transition: "width 0.25s ease" }} />
            </div>
        </div>
    );
};
