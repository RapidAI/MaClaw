export type ProjectSearchIconName = "info" | "arrowRight" | "externalLink";

export function ProjectSearchIcon({ name, size = 13 }: { name: ProjectSearchIconName; size?: number }) {
    const common = { fill: "none", stroke: "currentColor", strokeWidth: 2, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    return <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
        {name === "info" && <><circle {...common} cx="12" cy="12" r="10" /><path {...common} d="M12 16v-4" /><path {...common} d="M12 8h.01" /></>}
        {name === "arrowRight" && <><path {...common} d="M5 12h14" /><path {...common} d="m13 6 6 6-6 6" /></>}
        {name === "externalLink" && <><path {...common} d="M14 3h7v7" /><path {...common} d="M10 14 21 3" /><path {...common} d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5" /></>}
    </svg>;
}
