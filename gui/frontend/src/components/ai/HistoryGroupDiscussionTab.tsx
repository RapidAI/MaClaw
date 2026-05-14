import { useCallback, useEffect, useMemo, useState } from "react";
import { GroupDiscussionDownloadAttachment, GroupDiscussionGetConsultationDetail, GroupDiscussionSendHistoryMessage, OpenFileOrShowInFolder } from "../../../wailsjs/go/main/App";
import { VEGroupChatView, type GroupMessage, type GroupParticipant } from "./VEGroupChat";

type HistoryDiscussionDetail = {
    discussion?: {
        id?: string;
        topic?: string;
        question?: string;
        status?: string;
        participant_ids?: string[];
    };
    messages?: Array<{
        id?: string;
        from_id?: string;
        from_name?: string;
        kind?: string;
        content?: string;
        created_at?: string;
        attachments?: Array<NonNullable<GroupMessage["attachments"]>[number] & { file_url?: string; local_path?: string }>;
        text_attachments?: Array<{ filename?: string; mime_type?: string; local_path?: string }>;
        image_attachments?: Array<{ filename?: string; file_url?: string; local_path?: string; mime_type?: string }>;
        file_attachments?: Array<{ filename?: string; file_url?: string; local_path?: string; mime_type?: string; size_bytes?: number }>;
    }>;
};

type HistoryGroupDiscussionTabProps = {
    discussionId: string;
    title: string;
    readOnly: boolean;
    theme: any;
    lang?: string;
};

const textForLang = (lang: string | undefined, en: string, zhHans: string, zhHant = zhHans) => (
    lang === "zh-Hant" ? zhHant : lang?.startsWith("zh") || !lang ? zhHans : en
);

export function HistoryGroupDiscussionTab({ discussionId, title, readOnly, theme, lang }: HistoryGroupDiscussionTabProps) {
    const [detail, setDetail] = useState<HistoryDiscussionDetail | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [input, setInput] = useState("");
    const [sending, setSending] = useState(false);
    const [downloadingKey, setDownloadingKey] = useState("");
    const [downloadedPaths, setDownloadedPaths] = useState<Record<string, string>>({});

    const load = useCallback(async () => {
        if (!discussionId) return;
        setLoading(true);
        setError("");
        try {
            setDetail(await GroupDiscussionGetConsultationDetail(discussionId));
        } catch (e) {
            setError(String(e));
        } finally {
            setLoading(false);
        }
    }, [discussionId]);

    useEffect(() => { void load(); }, [load]);

    const send = useCallback(async () => {
        const content = input.trim();
        if (!content || readOnly || sending) return;
        setSending(true);
        setError("");
        try {
            await GroupDiscussionSendHistoryMessage(discussionId, { kind: "statement", content, created_at: new Date().toISOString() });
            setInput("");
            await load();
        } catch (e) {
            setError(String(e));
        } finally {
            setSending(false);
        }
    }, [discussionId, input, load, readOnly, sending]);

    const participants: GroupParticipant[] = useMemo(() => (
        (detail?.discussion?.participant_ids || []).map((id) => ({ id, name: id, online: true }))
    ), [detail?.discussion?.participant_ids]);

    const buildMessageAttachments = useCallback((m: NonNullable<HistoryDiscussionDetail["messages"]>[number]): NonNullable<GroupMessage["attachments"]> => {
        const attachments: NonNullable<GroupMessage["attachments"]> = [];
        for (const att of m.attachments || []) {
            const fileUrl = att.fileUrl || att.file_url || "";
            attachments.push({ type: att.type, filename: att.filename || textForLang(lang, "Attachment", "\u9644\u4ef6", "\u9644\u4ef6"), fileUrl, localPath: downloadedPaths[fileUrl] || att.localPath || att.local_path || "", sizeBytes: att.sizeBytes });
        }
        for (const att of m.text_attachments || []) {
            attachments.push({ type: "text", filename: att.filename || textForLang(lang, "Text", "\u6587\u672c", "\u6587\u672c"), localPath: att.local_path || "" });
        }
        for (const att of m.image_attachments || []) {
            const fileUrl = att.file_url || "";
            attachments.push({ type: "image", filename: att.filename || textForLang(lang, "Image", "\u56fe\u7247", "\u5716\u7247"), fileUrl, localPath: downloadedPaths[fileUrl] || att.local_path || "" });
        }
        for (const att of m.file_attachments || []) {
            const fileUrl = att.file_url || "";
            attachments.push({ type: "file", filename: att.filename || textForLang(lang, "File", "\u6587\u4ef6", "\u6a94\u6848"), fileUrl, localPath: downloadedPaths[fileUrl] || att.local_path || "", sizeBytes: att.size_bytes });
        }
        return attachments;
    }, [downloadedPaths, lang]);

    const messages: GroupMessage[] = useMemo(() => (
        (detail?.messages || []).map((m, idx) => ({
            id: m.id || `m-${idx}`,
            fromId: m.from_id || "unknown",
            fromName: m.from_name || m.from_id || textForLang(lang, "Unknown", "\u672a\u77e5", "\u672a\u77e5"),
            content: m.content || "",
            timestamp: m.created_at ? Date.parse(m.created_at) || Date.now() : Date.now(),
            attachments: buildMessageAttachments(m),
        }))
    ), [buildMessageAttachments, detail?.messages, lang]);

    const downloadAttachment = useCallback(async (attachment: NonNullable<GroupMessage["attachments"]>[number], message: GroupMessage) => {
        if (attachment.localPath) {
            setError("");
            try {
                await OpenFileOrShowInFolder(attachment.localPath);
            } catch (e) {
                setError(String(e));
            }
            return;
        }
        if (!attachment.fileUrl || downloadingKey) return;
        const key = `${message.id}:${attachment.fileUrl}`;
        setDownloadingKey(key);
        setError("");
        try {
            const result = await GroupDiscussionDownloadAttachment(discussionId, attachment.fileUrl, attachment.filename);
            const localPath = result?.local_path || result?.LocalPath || "";
            attachment.localPath = localPath;
            setDownloadedPaths((prev) => ({ ...prev, [attachment.fileUrl || key]: localPath }));
        } catch (e) {
            setError(String(e));
        } finally {
            setDownloadingKey("");
        }
    }, [discussionId, downloadingKey]);

    const subtitle = readOnly
        ? textForLang(lang, "Read-only - invited digital employee", "\u53ea\u8bfb - \u6211\u7684\u6570\u5b57\u5458\u5de5\u53d7\u9080\u53c2\u52a0", "\u552f\u8b80 - \u6211\u7684\u6578\u5b57\u54e1\u5de5\u53d7\u9080\u53c3\u52a0")
        : textForLang(lang, "Started by me - can continue", "\u6211\u53d1\u8d77 - \u53ef\u7ee7\u7eed\u8ba8\u8bba", "\u6211\u767c\u8d77 - \u53ef\u7e7c\u7e8c\u8a0e\u8ad6");

    return <div data-testid={`ai-history-group-tab-${discussionId}`} style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0, background: theme.bg }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "7px 12px", borderBottom: `1px solid ${theme.divider}`, background: theme.inputBarBg }}>
            <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
                    <div style={{ color: theme.text, fontSize: 13, fontWeight: 700, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{title}</div>
                    {readOnly && <span style={{ flexShrink: 0, border: `1px solid ${theme.fieldBorder}`, borderRadius: 4, padding: "1px 5px", color: theme.textMuted, fontSize: 11 }}>{textForLang(lang, "Read-only", "\u53ea\u8bfb", "\u552f\u8b80")}</span>}
                </div>
                <div style={{ color: theme.textMuted, fontSize: 11 }}>{subtitle}</div>
            </div>
            <button type="button" onClick={() => void load()} disabled={loading} style={{ border: `1px solid ${theme.fieldBorder}`, background: theme.fieldBg, color: theme.text, borderRadius: 6, padding: "4px 10px", cursor: loading ? "default" : "pointer", fontSize: 12 }}>{loading ? "..." : textForLang(lang, "Refresh", "\u5237\u65b0", "\u91cd\u65b0\u6574\u7406")}</button>
        </div>
        {participants.length > 0 && <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", padding: "6px 12px", borderBottom: `1px solid ${theme.divider}`, background: theme.bg }}>
            <span style={{ color: theme.textMuted, fontSize: 11 }}>{textForLang(lang, "Participants", "\u53c2\u4e0e\u8005", "\u53c3\u8207\u8005")}</span>
            {participants.map((p) => <span key={p.id} title={p.id} style={{ border: `1px solid ${theme.fieldBorder}`, borderRadius: 999, padding: "2px 7px", color: theme.text, background: theme.fieldBg, fontSize: 11, maxWidth: 180, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{p.name}</span>)}
        </div>}
        {error && <div role="alert" style={{ padding: "7px 12px", color: theme.errorText, background: theme.errorBg, borderBottom: `1px solid ${theme.errorBorder}`, fontSize: 12 }}>{error}</div>}
        {loading && !detail
            ? <div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", color: theme.textMuted }}>{textForLang(lang, "Loading...", "\u52a0\u8f7d\u4e2d...", "\u8f09\u5165\u4e2d...")}</div>
            : <VEGroupChatView sessionId={discussionId} participants={participants} messages={messages} theme={theme} lang={lang} onDownloadAttachment={downloadAttachment} allowParticipantAdd={false} />}
        <div style={{ display: "flex", gap: 8, padding: "8px 12px", borderTop: `1px solid ${theme.divider}`, background: theme.inputBarBg, opacity: readOnly ? 0.72 : 1 }}>
            <textarea value={input} onChange={(e) => setInput(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(); } }} disabled={readOnly || sending} placeholder={readOnly ? textForLang(lang, "Read-only session", "\u53ea\u8bfb\u4f1a\u8bdd\uff0c\u4e0d\u80fd\u7ee7\u7eed\u53d1\u8a00", "\u552f\u8b80\u6703\u8a71\uff0c\u4e0d\u80fd\u7e7c\u7e8c\u767c\u8a00") : textForLang(lang, "Continue discussion...", "\u7ee7\u7eed\u8ba8\u8bba...", "\u7e7c\u7e8c\u8a0e\u8ad6...")} rows={1} style={{ flex: 1, resize: "none", border: `1px solid ${theme.fieldBorder}`, borderRadius: 6, padding: "6px 10px", color: theme.inputText, background: readOnly ? theme.fieldBg : theme.bg, outline: "none", fontSize: 13 }} />
            <button type="button" onClick={() => void send()} disabled={readOnly || sending || !input.trim()} style={{ border: `1px solid ${theme.sendBtnBorder}`, background: theme.sendBtnColor, color: "#fff", borderRadius: 6, padding: "6px 12px", cursor: readOnly || sending || !input.trim() ? "default" : "pointer", opacity: readOnly || sending || !input.trim() ? 0.5 : 1, fontSize: 13 }}>{sending ? "..." : textForLang(lang, "Send", "\u53d1\u9001", "\u50b3\u9001")}</button>
        </div>
    </div>;
}
