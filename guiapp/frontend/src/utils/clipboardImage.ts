/**
 * Clipboard image helpers shared by the bug-report screenshot flow.
 * Extracted from App.tsx to keep the app shell under the guard line limit.
 */

/** Map a clipboard MIME type to a screenshot file extension (PNG fallback). */
export function clipboardImageExtension(mimeType: string): string {
    switch (mimeType.toLowerCase()) {
        case 'image/png': return 'png';
        case 'image/jpeg': return 'jpg';
        case 'image/webp': return 'webp';
        case 'image/bmp': return 'bmp';
        case 'image/gif': return 'gif';
        default: return 'png';
    }
}

/** Read a pasted image blob into a bare base64 payload (no data: prefix). */
export function readClipboardImageBase64(file: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result || '').split(',')[1] || '');
        reader.onerror = () => reject(reader.error || new Error('Unable to read pasted image'));
        reader.readAsDataURL(file);
    });
}
