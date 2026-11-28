const previewOpenGeneration = new Map<string, number>();

/** Generation captured when a cloud-agent file click starts. */
export function cloudAgentPreviewOpenGeneration(projectPath: string): number {
    return previewOpenGeneration.get(projectPath) || 0;
}

/**
 * Invalidate file clicks already in flight. A closed pane must not adopt
 * the file when the download returns, even if the pane was opened again.
 */
export function dismissCloudAgentPreviewOpens(projectPath: string): void {
    const key = projectPath.trim();
    if (!key) return;
    previewOpenGeneration.set(key, (previewOpenGeneration.get(key) || 0) + 1);
}

/** Local cache path for a cloud-agent pdf, docx, or pptx. The viewer reads disk. */
export async function materializeCloudAgentPreviewFile(projectPath: string, relativePath: string): Promise<string> {
    const app = (window as unknown as {
        go?: { main?: { App?: Record<string, unknown> } };
    }).go?.main?.App;
    const fn = app?.MaterializeCloudAgentPreviewFile;
    if (typeof fn !== "function") throw new Error("preview is not ready");
    const saved = String(await (fn as (...args: unknown[]) => Promise<unknown>)(projectPath, relativePath) || "").trim();
    if (!saved) throw new Error("preview is not ready");
    return saved;
}
