import { useId } from 'react';

export type EnvRuntimeId = 'node' | 'git' | 'python';

export const ENV_RUNTIME_ORDER: readonly EnvRuntimeId[] = ['node', 'git', 'python'];

export type RuntimeProgress = {
    active: EnvRuntimeId | null;
    done: ReadonlySet<EnvRuntimeId>;
    skipped: ReadonlySet<EnvRuntimeId>;
    percent: number;
};

const RUNTIME_LABEL_KEYS: Record<EnvRuntimeId, { title: string; desc: string }> = {
    node: { title: 'envCheckRuntimeNode', desc: 'envCheckRuntimeNodeDesc' },
    git: { title: 'envCheckRuntimeGit', desc: 'envCheckRuntimeGitDesc' },
    python: { title: 'envCheckRuntimePython', desc: 'envCheckRuntimePythonDesc' },
};

const STEP_PERCENT: Record<EnvRuntimeId, number> = {
    node: 34,
    git: 62,
    python: 86,
};

export function envRuntimeLabelKeys(id: EnvRuntimeId) {
    return RUNTIME_LABEL_KEYS[id];
}

function scanRuntimeLogs(logs: string[]): { seen: Set<EnvRuntimeId>; active: EnvRuntimeId | null } {
    const seen = new Set<EnvRuntimeId>();
    let active: EnvRuntimeId | null = null;
    let bestIdx = -1;
    for (const line of logs) {
        const id = matchRuntime(line);
        if (!id) continue;
        seen.add(id);
        const idx = ENV_RUNTIME_ORDER.indexOf(id);
        if (idx >= bestIdx) {
            active = id;
            bestIdx = idx;
        }
    }
    return { seen, active };
}

export function resolveActiveRuntime(logs: string[]): EnvRuntimeId | null {
    return scanRuntimeLogs(logs).active;
}

export function resolveRuntimeProgress(logs: string[]): RuntimeProgress {
    const done = new Set<EnvRuntimeId>();
    const skipped = new Set<EnvRuntimeId>();
    const { seen, active } = scanRuntimeLogs(logs);
    const joined = logs.join('\n').toLowerCase();
    if (/base environment check complete|环境检查完成|環境檢查完成/.test(joined)) {
        for (const id of ENV_RUNTIME_ORDER) {
            if (seen.has(id) || seen.size === 0) done.add(id);
            else skipped.add(id);
        }
        return { active: null, done, skipped, percent: 100 };
    }
    if (active) {
        const idx = ENV_RUNTIME_ORDER.indexOf(active);
        for (let i = 0; i < idx; i++) {
            const id = ENV_RUNTIME_ORDER[i];
            if (seen.has(id)) done.add(id);
        }
        return { active, done, skipped, percent: STEP_PERCENT[active] };
    }
    if (logs.length > 0) return { active: null, done, skipped, percent: 16 };
    return { active: null, done, skipped, percent: 8 };
}

function matchRuntime(msg: string): EnvRuntimeId | null {
    const s = msg.toLowerCase();
    if (/(python|pyenv|\buv\b|\bpip\b)/.test(s)) return 'python';
    if (/\bgit\b/.test(s)) return 'git';
    if (/(node\.?js|\bnpm\b|\bnode\b)/.test(s)) return 'node';
    return null;
}

function svgId(prefix: string, uid: string) {
    return `${prefix}-${uid.replace(/:/g, '')}`;
}

export function EnvCheckBackdrop({ className }: { className?: string }) {
    const uid = useId();
    const hex = svgId('eci-bg-hex', uid);
    const git = svgId('eci-bg-git', uid);
    const py = svgId('eci-bg-py', uid);
    return (
        <svg className={className} viewBox="0 0 520 460" preserveAspectRatio="xMidYMid slice" aria-hidden="true" focusable="false">
            <defs>
                <linearGradient id={hex} x1="360" y1="-20" x2="560" y2="220" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#68D391" />
                    <stop offset="1" stopColor="#2F855A" />
                </linearGradient>
                <linearGradient id={git} x1="20" y1="260" x2="220" y2="460" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#F6AD55" />
                    <stop offset="1" stopColor="#DD6B20" />
                </linearGradient>
                <linearGradient id={py} x1="280" y1="300" x2="520" y2="460" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#63B3ED" />
                    <stop offset="1" stopColor="#ED8936" />
                </linearGradient>
            </defs>
            <path
                d="M430-10 560 64v148L430 286 300 212V64Z"
                fill={`url(#${hex})`}
                opacity=".14"
            />
            <path
                d="M48 292c0-22 18-40 40-40h8c22 0 40 18 40 40v96c0 22-18 40-40 40h-8c-22 0-40-18-40-40Z"
                fill={`url(#${git})`}
                opacity=".13"
            />
            <circle cx="96" cy="292" r="18" fill={`url(#${git})`} opacity=".18" />
            <circle cx="96" cy="428" r="18" fill={`url(#${git})`} opacity=".18" />
            <circle cx="176" cy="360" r="16" fill={`url(#${git})`} opacity=".16" />
            <path
                d="M338 318c42-38 126-18 150 28 18 34 6 78-28 98-42 24-96 4-118-32-16-26-18-62-4-94Z"
                fill={`url(#${py})`}
                opacity=".14"
            />
        </svg>
    );
}

function NodeRuntimeIcon({ className }: { className?: string }) {
    const uid = useId();
    const fill = svgId('eci-node', uid);
    const shine = svgId('eci-node-s', uid);
    return (
        <svg className={className} viewBox="0 0 128 128" fill="none" aria-hidden="true" focusable="false">
            <defs>
                <linearGradient id={fill} x1="18" y1="8" x2="110" y2="120" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#86EFAC" />
                    <stop offset=".55" stopColor="#34C759" />
                    <stop offset="1" stopColor="#166534" />
                </linearGradient>
                <linearGradient id={shine} x1="40" y1="16" x2="90" y2="70" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#fff" stopOpacity=".55" />
                    <stop offset="1" stopColor="#fff" stopOpacity="0" />
                </linearGradient>
            </defs>
            <path d="M64 6 118 38v52L64 122 10 90V38Z" fill={`url(#${fill})`} />
            <path d="M64 18 106 43.5v41L64 110 22 84.5v-41Z" stroke="#F0FFF4" strokeWidth="3.2" opacity=".42" />
            <path d="M64 6 118 38 64 70 10 38Z" fill={`url(#${shine})`} />
            <path d="M50 50v36l28-16V58" stroke="#F7FFF9" strokeWidth="6.5" strokeLinejoin="round" strokeLinecap="round" />
            <circle cx="78" cy="56" r="5" fill="#F7FFF9" />
            <path d="M46 86h12" stroke="#BBF7D0" strokeWidth="4" strokeLinecap="round" opacity=".85" />
        </svg>
    );
}

function GitRuntimeIcon({ className }: { className?: string }) {
    const uid = useId();
    const fill = svgId('eci-git', uid);
    return (
        <svg className={className} viewBox="0 0 128 128" fill="none" aria-hidden="true" focusable="false">
            <defs>
                <linearGradient id={fill} x1="16" y1="8" x2="118" y2="120" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#FBD38D" />
                    <stop offset=".45" stopColor="#F97316" />
                    <stop offset="1" stopColor="#C2410C" />
                </linearGradient>
            </defs>
            <path d="M40 18v92" stroke={`url(#${fill})`} strokeWidth="12" strokeLinecap="round" />
            <path d="M40 58c4-6 22-24 60-22" stroke={`url(#${fill})`} strokeWidth="12" strokeLinecap="round" fill="none" />
            <circle cx="40" cy="22" r="16" fill={`url(#${fill})`} />
            <circle cx="40" cy="106" r="16" fill={`url(#${fill})`} />
            <circle cx="102" cy="40" r="16" fill={`url(#${fill})`} />
            <circle cx="40" cy="22" r="7" fill="#FFF7ED" />
            <circle cx="40" cy="106" r="7" fill="#FFF7ED" />
            <circle cx="102" cy="40" r="7" fill="#FFF7ED" />
        </svg>
    );
}

function PythonRuntimeIcon({ className }: { className?: string }) {
    const uid = useId();
    const blue = svgId('eci-py-b', uid);
    const gold = svgId('eci-py-g', uid);
    return (
        <svg className={className} viewBox="0 0 128 128" fill="none" aria-hidden="true" focusable="false">
            <defs>
                <linearGradient id={blue} x1="8" y1="4" x2="86" y2="92" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#90CDF4" />
                    <stop offset="1" stopColor="#2B6CB0" />
                </linearGradient>
                <linearGradient id={gold} x1="42" y1="36" x2="124" y2="124" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#FDE68A" />
                    <stop offset="1" stopColor="#D97706" />
                </linearGradient>
            </defs>
            <g transform="translate(8 6) scale(0.88)">
                <path
                    d="M36 10h36c16 0 28 12 28 28v12c0 9-7 16-16 16H52c-9 0-16 7-16 16v8c0 7-7 14-16 14h-2C7 104 0 95 0 82V38C0 20 16 10 36 10Z"
                    fill={`url(#${blue})`}
                />
                <circle cx="48" cy="28" r="7" fill="#EFF6FF" />
                <path
                    d="M92 118H56c-16 0-28-12-28-28V78c0-9 7-16 16-16h30c9 0 16-7 16-16v-8c0-7 7-14 16-14h2c11 0 18 9 18 22v44c0 18-16 28-34 28Z"
                    fill={`url(#${gold})`}
                />
                <circle cx="80" cy="100" r="7" fill="#1F2937" opacity=".5" />
            </g>
        </svg>
    );
}

const RUNTIME_ICONS: Record<EnvRuntimeId, typeof NodeRuntimeIcon> = {
    node: NodeRuntimeIcon,
    git: GitRuntimeIcon,
    python: PythonRuntimeIcon,
};

export function EnvRuntimeIcon({ id, className }: { id: EnvRuntimeId; className?: string }) {
    const Icon = RUNTIME_ICONS[id];
    return <Icon className={className} />;
}
