export interface ProblemReportAnnotationExport {
    annotated: string;
    failed: boolean;
}

// Each attached picture keeps its own editor. Submit reads that live export.
// An empty string is the original file. A picture the user has not opened yet
// has no export and stays on the original file too. One picture failing does
// not drop marks already exported for the others; the caller shows that picture.
export function problemReportAnnotatedExports(
    paths: readonly string[],
    exportsByPath: Readonly<Record<string, ProblemReportAnnotationExport>>,
): { annotated: string[] } | { error: "export"; path: string } {
    const annotated: string[] = [];
    for (const path of paths) {
        const exported = exportsByPath[path] ?? { annotated: "", failed: false };
        if (exported.failed) return { error: "export", path };
        annotated.push(exported.annotated || "");
    }
    return { annotated };
}

export function appendProblemReportScreenshotPaths(previous: readonly string[], incoming: readonly unknown[]): string[] {
    return [...previous, ...incoming]
        .filter((value): value is string => typeof value === 'string' && value.trim() !== '')
        .filter((value, index, all) => all.indexOf(value) === index)
        .slice(0, 12);
}

// The rendered index goes stale when a paste or another remove updates the
// list before React paints. The path is the picture.
export function removeProblemReportScreenshotPath(previous: readonly string[], path: string): string[] {
    if (!path) return [...previous];
    return previous.filter(item => item !== path);
}

// The picture just added is the one the user is about to mark. A duplicate
// or a full list leaves the current picture selected.
export function problemReportShotAdded(previous: readonly string[], next: readonly string[]): string {
    return next.find(path => !previous.includes(path)) ?? "";
}

export interface ProblemReportClipboardFiles {
    items?: ArrayLike<{ kind?: string; getAsFile?: () => File | null }> | null;
    files?: ArrayLike<File> | null;
}

// Upload sends png/jpeg/webp/bmp and skips .gif. A gif in the list would
// look attached and then never leave the machine.
const problemReportUploadMimeExtension: Record<string, string> = {
    "image/png": "png",
    "image/jpeg": "jpg",
    "image/jpg": "jpg",
    "image/webp": "webp",
    "image/bmp": "bmp",
    "image/x-ms-bmp": "bmp",
};

function problemReportUploadExtensionFromName(name: string): string {
    const match = /\.([a-z0-9]+)$/i.exec(name);
    const ext = (match?.[1] || "").toLowerCase();
    if (ext === "jpg" || ext === "jpeg") return "jpg";
    if (ext === "png" || ext === "webp" || ext === "bmp") return ext;
    return "";
}

function problemReportImageFile(file: File): boolean {
    const mime = file.type.trim().toLowerCase();
    if (mime === "image/gif" || (/\.gif$/i.test(file.name) && !problemReportUploadMimeExtension[mime])) return false;
    if (problemReportUploadMimeExtension[mime]) return true;
    if (mime.startsWith("image/")) return true;
    return problemReportUploadExtensionFromName(file.name) !== "";
}

// MIME names the bytes when the clipboard set one. Explorer paste often
// leaves the type empty and puts the real suffix on the filename. BMP is
// converted to PNG only when the saved suffix is .bmp; a BMP named .png
// is dropped on upload.
export function problemReportPasteExtension(file: { type?: string; name?: string }): string {
    const mime = (file.type || "").trim().toLowerCase();
    const fromMime = problemReportUploadMimeExtension[mime];
    if (fromMime) return fromMime;
    return problemReportUploadExtensionFromName(file.name || "") || "png";
}

// A Windows screenshot is often only on the item list, and a WebView may also
// put the same blob on the file list with a different name. Same type and
// size is one image; two different pathless images of equal size are not a
// real paste.
function problemReportImageKey(file: File): string {
    const path = (file as File & { path?: unknown }).path;
    if (typeof path === "string" && path.trim()) return `path|${path.trim()}`;
    return `blob|${file.type}|${file.size}`;
}

export function problemReportPastedImages(data: ProblemReportClipboardFiles | null | undefined): File[] {
    if (!data) return [];
    const seen = new Set<string>();
    const out: File[] = [];
    const push = (file: File | null | undefined) => {
        if (!file || !problemReportImageFile(file)) return;
        const key = problemReportImageKey(file);
        if (seen.has(key)) return;
        seen.add(key);
        out.push(file);
    };
    const items = data.items;
    if (items) {
        for (let i = 0; i < items.length; i++) {
            const item = items[i];
            if (item && item.kind === "file" && item.getAsFile) push(item.getAsFile());
        }
    }
    const files = data.files;
    if (files) {
        for (let i = 0; i < files.length; i++) push(files[i]);
    }
    return out;
}

// A picture stays mounted after the user switches away, so its strokes and
// undo stack remain on that picture. Removing the file drops it.
export function problemReportShotMounts(
    paths: readonly string[],
    active: string,
    seen: Iterable<string>,
): { mounts: string[]; seen: string[] } {
    const live = new Set<string>();
    for (const path of seen) {
        if (paths.includes(path)) live.add(path);
    }
    if (active && paths.includes(active)) live.add(active);
    const mounts = paths.filter(path => live.has(path));
    return { mounts, seen: mounts };
}
