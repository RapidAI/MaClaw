export async function shareAssistantTask(taskTitle: string, tabId?: string): Promise<void> {
    const title = String(taskTitle || "").trim();
    if (!title) return;
    const shareDetail = { title, tabId };
    if (typeof navigator !== "undefined" && typeof navigator.share === "function") {
        try {
            await navigator.share({ title, text: title });
            return;
        } catch (error) {
            if ((error as DOMException)?.name === "AbortError") return;
        }
    }
    try {
        if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
            await navigator.clipboard.writeText(title);
        }
    } catch {
        // Clipboard access is optional in Wails; keep the host event usable.
    }
    if (typeof window !== "undefined") {
        window.dispatchEvent(new CustomEvent("maclaw:share-task", { detail: shareDetail }));
    }
}
