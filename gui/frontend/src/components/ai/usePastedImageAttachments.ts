import { useCallback, useState } from "react";
import type { AttachmentInfo } from "./useBufferQueue";

async function savePastedImage(base64: string, ext: string): Promise<string> {
    const w = window as any;
    if (typeof window !== "undefined" && w.go?.main?.App?.SavePastedImage) {
        return w.go.main.App.SavePastedImage(base64, ext);
    }
    throw new Error("SavePastedImage binding not available");
}

export function usePastedImageAttachments() {
    const [pendingAttachments, setPendingAttachments] = useState<AttachmentInfo[]>([]);

    const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
        const items = e.clipboardData?.items;
        if (!items) return;
        for (const item of Array.from(items)) {
            if (!item.type.startsWith("image/")) continue;
            e.preventDefault();
            const blob = item.getAsFile();
            if (!blob) continue;
            const ext = blob.type === "image/png" ? "png" : "jpg";
            try {
                const base64 = await new Promise<string>((resolve, reject) => {
                    const reader = new FileReader();
                    reader.onload = () => resolve(String(reader.result || "").split(",")[1] || "");
                    reader.onerror = reject;
                    reader.readAsDataURL(blob);
                });
                const filePath = await savePastedImage(base64, ext);
                const thumbnailDataUrl = URL.createObjectURL(blob);
                const fileName = filePath.split(/[/\\]/).pop() || "paste." + ext;
                setPendingAttachments(prev => [...prev, { filePath, thumbnailDataUrl, isImage: true, fileName, extension: "." + ext }]);
            } catch (err) {
                console.error("Failed to save pasted image:", err);
            }
            return;
        }
    }, []);

    return { handlePaste, pendingAttachments, setPendingAttachments };
}
