import type { CSSProperties, ReactNode } from "react";

const s = { fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };

export type TaskConfigIconName =
    | "gear"
    | "chat"
    | "code"
    | "robot"
    | "spark"
    | "folder"
    | "cloud"
    | "server"
    | "check"
    | "plus"
    | "chevronDown"
    | "search"
    | "back"
    | "grid"
    | "home"
    | "clock"
    | "lock"
    | "ban";

const PATHS: Record<TaskConfigIconName, ReactNode> = {
    gear: (<><circle {...s} cx="12" cy="12" r="3" /><path {...s} d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8" /></>),
    chat: (<path {...s} d="M21 12a8 8 0 0 1-8 8H5l-2 2V12a8 8 0 0 1 8-8h2a8 8 0 0 1 8 8Z" />),
    code: (<><path {...s} d="m8 8-3.5 4L8 16" /><path {...s} d="m16 8 3.5 4L16 16" /><path {...s} d="m13.2 6-2.4 12" /></>),
    robot: (<><rect {...s} x="5" y="8" width="14" height="10" rx="2.5" /><path {...s} d="M12 8V4.5" /><circle {...s} cx="12" cy="3.5" r="1" /><path {...s} d="M9 12h.01M15 12h.01" /><path {...s} d="M9.5 15.5h5" /><path {...s} d="M2.5 11v4M21.5 11v4" /></>),
    spark: (<><path {...s} d="m12 3 1.4 5 5 1.4-5 1.4-1.4 5-1.4-5-5-1.4 5-1.4L12 3Z" /><path {...s} d="m19 15 .8 2.7 2.7.8-2.7.8-.8 2.7-.8-2.7-2.7-.8 2.7-.8.8-2.7Z" /></>),
    folder: (<path {...s} d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />),
    cloud: (<path {...s} d="M7 18a4.5 4.5 0 0 1-.4-9A6 6 0 0 1 18 8.5 3.8 3.8 0 0 1 17.5 18H7Z" />),
    server: (<><rect {...s} x="4" y="4" width="16" height="6.5" rx="1.5" /><rect {...s} x="4" y="13.5" width="16" height="6.5" rx="1.5" /><path {...s} d="M8 7.2h.01M8 16.7h.01" /><path {...s} d="M12 7.2h4M12 16.7h4" /></>),
    check: (<path {...s} d="m5 12.5 4.5 4.5L19 7.5" />),
    plus: (<><path {...s} d="M12 5v14" /><path {...s} d="M5 12h14" /></>),
    chevronDown: (<path {...s} d="m6 9 6 6 6-6" />),
    search: (<><circle {...s} cx="11" cy="11" r="6.5" /><path {...s} d="m16 16 4.5 4.5" /></>),
    back: (<path {...s} d="M15 5l-7 7 7 7" />),
    grid: (<><rect {...s} x="4" y="4" width="7" height="7" rx="1.5" /><rect {...s} x="13" y="4" width="7" height="7" rx="1.5" /><rect {...s} x="4" y="13" width="7" height="7" rx="1.5" /><rect {...s} x="13" y="13" width="7" height="7" rx="1.5" /></>),
    home: (<><path {...s} d="m4 11 8-7 8 7" /><path {...s} d="M6 9.5V20h12V9.5" /></>),
    clock: (<><circle {...s} cx="12" cy="12" r="8.5" /><path {...s} d="M12 7v5l3.5 2" /></>),
    lock: (<><rect {...s} x="5.5" y="10.5" width="13" height="9" rx="2" /><path {...s} d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5" /></>),
    ban: (<><circle {...s} cx="12" cy="12" r="8.5" /><path {...s} d="m6 6 12 12" /></>),
};

export function TaskConfigIcon({ name, size = 15, style }: { name: TaskConfigIconName; size?: number; style?: CSSProperties }) {
    return (
        <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            aria-hidden="true"
            focusable="false"
            style={{ display: "block", flexShrink: 0, ...style }}
        >
            {PATHS[name]}
        </svg>
    );
}
