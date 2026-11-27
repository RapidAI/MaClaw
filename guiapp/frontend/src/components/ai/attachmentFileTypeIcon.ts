import { AIAssistantFileTypeIconDataURL } from "../../../wailsjs/go/main/App";

const fileTypeIconPromises = new Map<string, Promise<string>>();
export const resolvedFileTypeIcons = new Map<string, string>();
const FILE_TYPE_ICON_CACHE_MAX = 96;

export function resetAttachmentFileTypeIconCacheForTests(): void {
    fileTypeIconPromises.clear();
    resolvedFileTypeIcons.clear();
}

function cacheFileTypeIcon(key: string, url: string): void {
    resolvedFileTypeIcons.delete(key);
    resolvedFileTypeIcons.set(key, url);
    if (resolvedFileTypeIcons.size > FILE_TYPE_ICON_CACHE_MAX) resolvedFileTypeIcons.delete(resolvedFileTypeIcons.keys().next().value!);
}

// Own-icon extensions match fileTypeIconUsesOwnIcon in guiapp/file_type_icon.go.
const ownFileTypeIconExtensions = new Set(["", ".exe", ".lnk", ".ico", ".url", ".scr"]);

// fileTypeIconExtension matches path/filepath.Ext: the suffix from the last dot
// in the final path element, including a name that itself starts with a dot.
function fileTypeIconExtension(filePath: string): string {
    const name = filePath.split(/[\\/]/).pop() || filePath;
    const dot = name.lastIndexOf(".");
    if (dot < 0) return "";
    return name.slice(dot).toLowerCase();
}

export function fileTypeIconCacheKey(filePath: string): string {
    const trimmed = filePath.trim();
    const ext = fileTypeIconExtension(trimmed);
    if (ownFileTypeIconExtensions.has(ext)) return trimmed.toLowerCase();
    return ext;
}

export function attachmentKindLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    if (!raw) return "FILE";
    return raw.slice(0, 4).toUpperCase();
}

export function attachmentFileName(filePath: string): string {
    return filePath.split(/[\/\\]/).pop() || filePath;
}

export function loadFileTypeIcon(filePath: string): Promise<string> {
    const key = fileTypeIconCacheKey(filePath);
    const ready = resolvedFileTypeIcons.get(key);
    if (ready) {
        cacheFileTypeIcon(key, ready);
        return Promise.resolve(ready);
    }
    const cached = fileTypeIconPromises.get(key);
    if (cached) return cached;
    const pending = (async () => {
        try {
            return String(await AIAssistantFileTypeIconDataURL(filePath) || "");
        } catch {
            return "";
        }
    })();
    fileTypeIconPromises.set(key, pending);
    void pending.then(url => {
        // A newer lookup, or a test reset, may have replaced this entry.
        if (fileTypeIconPromises.get(key) !== pending) return;
        if (url) cacheFileTypeIcon(key, url);
        fileTypeIconPromises.delete(key);
    });
    return pending;
}
