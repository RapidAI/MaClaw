import type React from "react";
import type { Theme } from "./aiAssistantPanelTheme";

export function renderScreenshotPreview(
    screenshotBase64: string,
    localFilePath: string | undefined,
    openFile: (event: React.MouseEvent, filePath: string) => void,
    t: Theme,
): React.ReactNode {
    const image = (
        <img
            src={`data:image/png;base64,${screenshotBase64}`}
            alt="screenshot"
            style={{
                width: "180px",
                maxWidth: "100%",
                maxHeight: "120px",
                borderRadius: "4px",
                border: `1px solid ${t.borderLeft}`,
                boxSizing: "border-box",
                display: "block",
                objectFit: "contain",
            }}
        />
    );
    return (
        <div data-testid="screenshot-preview-block" style={{ margin: "4px 0 6px 0", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", overflow: "hidden" }}>
            {localFilePath ? (
                <a href="#" onClick={(event) => openFile(event, localFilePath)} style={{ display: "inline-block", maxWidth: "100%", cursor: "pointer" }} title={localFilePath}>
                    {image}
                </a>
            ) : image}
        </div>
    );
}
