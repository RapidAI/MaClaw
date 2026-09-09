export const avatarImageMaxBytes = 1024 * 1024;
export const avatarSourceImageMaxBytes = 5 * 1024 * 1024;
const avatarDataURLMaxLength = "data:image/jpeg;base64,".length + Math.ceil(avatarImageMaxBytes / 3) * 4;

export function safeAvatarDataURL(value: string | undefined): string {
    const avatar = String(value || "").trim();
    if (avatar.length > avatarDataURLMaxLength) return "";
    return safeAvatarImageDataURL(avatar, avatarImageMaxBytes);
}

export function safeAvatarSourceDataURL(value: string | undefined): string {
    return safeAvatarImageDataURL(String(value || "").trim(), avatarSourceImageMaxBytes);
}

function safeAvatarImageDataURL(avatar: string, maxBytes: number): string {
    const match = avatar.match(/^data:image\/(png|jpe?g|webp);base64,([a-z0-9+/=]+)$/i);
    if (!match) return "";
    try {
        const bytes = atob(match[2]);
        if (bytes.length > maxBytes) return "";
        return hasImageSignature(match[1].toLowerCase(), bytes) ? avatar : "";
    } catch {
        return "";
    }
}

function hasImageSignature(type: string, bytes: string): boolean {
    if (type === "png") {
        return bytes.length >= 8 && bytes.charCodeAt(0) === 0x89 && bytes.slice(1, 4) === "PNG" && bytes.charCodeAt(4) === 0x0d && bytes.charCodeAt(5) === 0x0a && bytes.charCodeAt(6) === 0x1a && bytes.charCodeAt(7) === 0x0a;
    }
    if (type === "jpg" || type === "jpeg") {
        return bytes.length >= 3 && bytes.charCodeAt(0) === 0xff && bytes.charCodeAt(1) === 0xd8 && bytes.charCodeAt(2) === 0xff;
    }
    if (type === "webp") {
        return bytes.length >= 12 && bytes.slice(0, 4) === "RIFF" && bytes.slice(8, 12) === "WEBP";
    }
    return false;
}
