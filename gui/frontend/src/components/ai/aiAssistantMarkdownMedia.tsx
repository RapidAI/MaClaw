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
                maxWidth: "180px",
                maxHeight: "120px",
                borderRadius: "4px",
                border: `1px solid ${t.borderLeft}`,
                objectFit: "contain",
            }}
        />
    );
    return (
        <div style={{ margin: "4px 0 6px 0" }}>
            {localFilePath ? (
                <a href="#" onClick={(event) => openFile(event, localFilePath)} style={{ display: "inline-block", cursor: "pointer" }} title={localFilePath}>
                    {image}
                </a>
            ) : image}
        </div>
    );
}
