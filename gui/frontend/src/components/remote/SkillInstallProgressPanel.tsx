import { useEffect, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import { colors, remoteInfoPanelStyle } from "./styles";

type SkillInstallProgress = {
    skill?: string;
    status?: string;
    level?: string;
};

export const SkillInstallProgressPanel = ({ active }: { active: boolean }) => {
    const [progress, setProgress] = useState<SkillInstallProgress | null>(null);

    useEffect(() => {
        const cleanup = EventsOn("skill-install-progress", (payload: any) => {
            if (!payload || typeof payload !== "object") return;
            setProgress({
                skill: typeof payload.skill === "string" ? payload.skill : undefined,
                status: typeof payload.status === "string" ? payload.status : undefined,
                level: typeof payload.level === "string" ? payload.level : undefined,
            });
        });
        return cleanup;
    }, []);

    if (!active || !progress) return null;
    const label = (progress.skill || "Skill install") + (progress.level ? ` - risk ${progress.level}` : "");
    return (
        <div style={{ ...remoteInfoPanelStyle, fontSize: "0.78rem", display: "flex", gap: "8px", alignItems: "center" }}>
            <span style={{ width: "12px", height: "12px", border: `2px solid ${colors.primary}`, borderTopColor: "transparent", borderRadius: "50%", animation: "spin 1s linear infinite", flex: "0 0 auto" }} />
            <span style={{ color: colors.textSecondary }}>{label}: {progress.status || "Scanning before install..."}</span>
        </div>
    );
};
