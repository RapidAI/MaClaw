type TitleBarToolIconName = "cart" | "search" | "volumeOn" | "volumeOff" | "sun" | "moon" | "book" | "guide" | "refresh" | "trash" | "eraser" | "mobileDocs";

export function TitleBarToolIcon({ name }: { name: TitleBarToolIconName }) {
    const common = {
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.8,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
    };
    return (
        <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: "block" }}>
            {name === "cart" && (
                <>
                    <circle {...common} cx="9" cy="20" r="1.4" />
                    <circle {...common} cx="18" cy="20" r="1.4" />
                    <path {...common} d="M3 4h2l2.2 11.2a2 2 0 0 0 2 1.6h7.9a2 2 0 0 0 1.9-1.4L21 8H6.2" />
                </>
            )}
            {name === "search" && (
                <>
                    <circle {...common} cx="11" cy="11" r="6.2" />
                    <path {...common} d="m16 16 4 4" />
                </>
            )}
            {name === "volumeOn" && (
                <>
                    <path {...common} d="M4 9v6h4l5 4V5L8 9H4Z" />
                    <path {...common} d="M16 9.5a4 4 0 0 1 0 5" />
                    <path {...common} d="M18.5 7a7 7 0 0 1 0 10" />
                </>
            )}
            {name === "volumeOff" && (
                <>
                    <path {...common} d="M4 9v6h4l5 4V5L8 9H4Z" />
                    <path {...common} d="m17 9 4 4" />
                    <path {...common} d="m21 9-4 4" />
                </>
            )}
            {name === "sun" && (
                <>
                    <circle {...common} cx="12" cy="12" r="4" />
                    <path {...common} d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
                </>
            )}
            {name === "moon" && <path {...common} d="M20 14.5A7.5 7.5 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z" />}
            {name === "book" && (
                <>
                    <path {...common} d="M5 4h6a3 3 0 0 1 3 3v13a3 3 0 0 0-3-2H5V4Z" />
                    <path {...common} d="M19 4h-5a3 3 0 0 0-3 3" />
                    <path {...common} d="M19 4v14h-5" />
                </>
            )}
            {/* Phone + document: shared Hub library with MaClaw Mobile. */}
            {name === "mobileDocs" && (
                <>
                    <rect {...common} x="7" y="2.5" width="10" height="19" rx="2" />
                    <path {...common} d="M10 5.5h4" />
                    <path {...common} d="M10 9h4M10 12h4M10 15h2.5" />
                    <path {...common} d="M9.5 18.5h5" />
                </>
            )}
            {name === "guide" && (
                <>
                    <path {...common} d="M6 4h9a3 3 0 0 1 3 3v13H8a3 3 0 0 1-3-3V5a1 1 0 0 1 1-1Z" />
                    <path {...common} d="M8 8h7M8 12h6" />
                </>
            )}
            {name === "refresh" && (
                <>
                    <path {...common} d="M20 6v5h-5" />
                    <path {...common} d="M4 18v-5h5" />
                    <path {...common} d="M18 10a6 6 0 0 0-10-3l-4 4" />
                    <path {...common} d="M6 14a6 6 0 0 0 10 3l4-4" />
                </>
            )}
            {name === "trash" && (
                <>
                    <path {...common} d="M4 6h16" />
                    <path {...common} d="M9 6V4h6v2" />
                    <path {...common} d="m6 6 1 14h10l1-14" />
                </>
            )}
            {name === "eraser" && (
                <>
                    <path {...common} d="M21 12a9 9 0 0 1-9 9H3V12a9 9 0 0 1 18 0Z" />
                    <path {...common} d="M9 12h6" />
                    <path {...common} d="M12 9v6" />
                </>
            )}
        </svg>
    );
}
