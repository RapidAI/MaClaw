// Material files a person attaches to a desktop bot message. Each file is
// uploaded in chunks to the bot's own folder (~/素材/<bot>/) on its remote
// desktop; the message then carries a [素材] list of those paths so the
// reading and the worker know to use them.

export const MATERIAL_MAX_BYTES = 200 * 1024 * 1024;
export const MATERIAL_CHUNK_BYTES = 4 * 1024 * 1024;
const BLOCK_HEAD = '[素材]';

export type MaterialStatus = 'uploading' | 'done' | 'error';

export interface MaterialItem {
    id: string;
    name: string;
    size: number;
    sent: number;
    status: MaterialStatus;
    path?: string;
    error?: string;
}

export interface MaterialChunkReply {
    received?: number;
    done?: boolean;
    path?: string;
}

export type MaterialChunkUploader = (botId: string, uploadId: string, name: string, offset: number, total: number, dataBase64: string) => Promise<MaterialChunkReply | null>;

export function materialUploadId(): string {
    const random = Math.random().toString(36).slice(2, 10);
    return `up-${Date.now().toString(36)}-${random}`;
}

function toBase64(bytes: Uint8Array): string {
    let binary = '';
    const step = 0x8000;
    for (let i = 0; i < bytes.length; i += step) {
        binary += String.fromCharCode(...bytes.subarray(i, i + step));
    }
    return btoa(binary);
}

async function blobBytes(blob: Blob): Promise<Uint8Array> {
    if (typeof blob.arrayBuffer === 'function') return new Uint8Array(await blob.arrayBuffer());
    return new Uint8Array(await new Response(blob).arrayBuffer());
}

export function materialTooLarge(size: number): boolean {
    return size > MATERIAL_MAX_BYTES;
}

// uploadMaterial sends one file chunk by chunk and reports bytes sent after
// each chunk. It resolves to the remote ~/素材/… path.
export async function uploadMaterial(
    file: Blob & { name: string },
    botId: string,
    upload: MaterialChunkUploader,
    onProgress: (sent: number) => void,
    uploadId = materialUploadId(),
    chunkBytes = MATERIAL_CHUNK_BYTES,
): Promise<string> {
    const total = file.size;
    if (total <= 0) throw new Error('empty');
    if (materialTooLarge(total)) throw new Error('too_large');
    let offset = 0;
    while (offset < total) {
        const end = Math.min(total, offset + chunkBytes);
        const data = toBase64(await blobBytes(file.slice(offset, end)));
        const reply = await upload(botId, uploadId, file.name, offset, total, data);
        offset = end;
        onProgress(offset);
        if (reply?.done) {
            const path = String(reply.path || '').trim();
            if (!path) throw new Error('no_path');
            return path;
        }
    }
    throw new Error('no_path');
}

// materialsBlock is the [素材] list appended to the message text.
export function materialsBlock(paths: string[]): string {
    const clean = paths.map(path => path.trim()).filter(Boolean);
    if (clean.length === 0) return '';
    return `${BLOCK_HEAD}\n${clean.map(path => `- ${path}`).join('\n')}`;
}

export function withMaterials(text: string, paths: string[], lang: string): string {
    const block = materialsBlock(paths);
    if (!block) return text.trim();
    const typed = text.trim() || (String(lang).toLowerCase().startsWith('en') ? 'Here are the materials for you.' : '这些是给你的素材。');
    return `${typed}\n\n${block}`;
}

// splitMaterials takes a trailing [素材] list off a message for display.
export function splitMaterials(content: string): { text: string; paths: string[] } {
    const raw = content || '';
    const at = raw.lastIndexOf(`\n${BLOCK_HEAD}\n`);
    const start = at >= 0 ? at + 1 : raw.startsWith(`${BLOCK_HEAD}\n`) ? 0 : -1;
    if (start < 0) return { text: raw, paths: [] };
    const lines = raw.slice(start + BLOCK_HEAD.length + 1).split('\n');
    const paths: string[] = [];
    for (const line of lines) {
        if (!line.trim()) continue;
        if (!line.startsWith('- ')) return { text: raw, paths: [] };
        paths.push(line.slice(2).trim());
    }
    if (paths.length === 0) return { text: raw, paths: [] };
    return { text: raw.slice(0, start).trimEnd(), paths };
}

export function materialFileName(path: string): string {
    const parts = path.split('/');
    return parts[parts.length - 1] || path;
}

export function formatMaterialSize(bytes: number): string {
    if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
    if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${bytes} B`;
}

// Files from a paste or drop event; plain text pastes give none.
export function filesFrom(list: FileList | DataTransferItemList | null | undefined): File[] {
    if (!list) return [];
    const out: File[] = [];
    for (let i = 0; i < list.length; i++) {
        const item = (list as ArrayLike<File | DataTransferItem>)[i];
        if (item instanceof File) out.push(item);
        else if (item && (item as DataTransferItem).kind === 'file') {
            const file = (item as DataTransferItem).getAsFile();
            if (file) out.push(file);
        }
    }
    return out;
}
