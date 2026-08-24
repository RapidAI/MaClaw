import type React from "react";
import { AttachmentImageThumbnail } from "./AttachmentImagePreview";
import type { Theme } from "./aiAssistantPanelTheme";

/** Last path segment of a Windows or POSIX path. */
function baseName(filePath: string): string {
    const parts = filePath.split(/[\\/]/).filter(Boolean);
    return parts[parts.length - 1] || filePath;
}

export function renderScreenshotPreview(
    screenshotBase64: string,
    localFilePath: string | undefined,
    t: Theme,
    lang: string,
): React.ReactNode {
    const isZh = !lang.startsWith("en");
    const fileName = localFilePath ? baseName(localFilePath) : (isZh ? "截图" : "screenshot");
    return (
        <div data-testid="screenshot-preview-block" style={{ margin: "4px 0 6px 0", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", overflow: "hidden" }}>
            {/* The tool reports a downscaled screenshot; the overlay re-reads the
                saved file so the full-resolution capture is what gets zoomed. */}
            <AttachmentImageThumbnail
                src={`data:image/png;base64,${screenshotBase64}`}
                filePath={localFilePath || ""}
                fileName={fileName}
                lang={lang}
                theme={t}
                title={localFilePath}
                frameStyle={{
                    width: "180px",
                    maxWidth: "100%",
                    borderRadius: "4px",
                    border: `1px solid ${t.borderLeft}`,
                    boxSizing: "border-box",
                    background: "transparent",
                }}
                imageStyle={{ height: "auto", maxHeight: "120px", objectFit: "contain" }}
            />
        </div>
    );
}
