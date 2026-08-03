import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { GroupDiscussionDownloadAttachment, GroupDiscussionGetConsultationDetail, GroupDiscussionSendHistoryMessage, GroupDiscussionSendInvitation, OpenFileOrShowInFolder } from '../../../wailsjs/go/main/App';
import { a2a } from '../../../wailsjs/go/models';
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { localizeText } from "../../i18n";
import { GroupParticipantPanel } from "./GroupParticipantPanel";
import { MentionPopover, useMentionKeyboard, type MentionParticipant } from "./MentionPopover";
import { insertTextareaLineBreak, isLineBreakShortcut, isPlainEnter } from "./assistantInputShortcuts";
import { VEGroupChatView, type GroupMessage, type GroupParticipant } from "./VEGroupChat";
import { isHistoryDiscussionReadOnly } from "./historyDiscussionUtils";
import { LEGACY_LOCAL_AI_PARTICIPANT_ID, LOCAL_AI_DISPLAY_NAME_EN, LOCAL_AI_DISPLAY_NAME_ZH_HANS, LOCAL_AI_DISPLAY_NAME_ZH_HANT, isLocalAIName, isLocalParticipantId, localAINameForLang, looksLikeRawParticipantId, normalizeParticipantId } from "./localAIIdentity";
import { addParticipantIdentityKeys, participantIdentityMatches, participantNameForIdentity } from "./participantIdentity";

type HistoryDiscussionDetail = {
    discussion?: {
        id?: string;
        topic?: string;
        question?: string;
        status?: string;
        participant_ids?: string[];
        local_relation?: string;
        role?: string;
        readonly?: boolean;
    };
    session?: {
        participants?: Array<{ id?: string; name?: string; role_code?: string }>;
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

const historyDetailReloadDebounceMs = 120;

const textForLang = localizeText;

const eventDiscussionPayload = (event: any) => {
    const payload = event?.payload || event || {};
    const message = payload?.message || payload?.Message || {};
    return { payload, message };
};

const eventDiscussionId = (event: any): string => {
    const { payload, message } = eventDiscussionPayload(event);
    return String(
        event?.session_id ||
        event?.discussion_id ||
        payload?.session_id ||
        payload?.discussion_id ||
        payload?.SessionID ||
        payload?.DiscussionID ||
        message?.session_id ||
        message?.discussion_id ||
        message?.SessionID ||
        message?.DiscussionID ||
        ""
    ).trim();
};

const eventDiscussionKind = (event: any): string => {
    const { message } = eventDiscussionPayload(event);
    return String(message?.kind || message?.Kind || "").trim().toLowerCase();
};

const mentionLabelFromParticipant = (participant: Pick<GroupParticipant, "id" | "name">): string => {
    const name = String(participant.name || participant.id || "").trim();
    return name.replace(/\s+\([^()]+\)$/, "").trim() || String(participant.id || "").trim();
};


const readableHistorySpeakerName = (
    candidate: string | undefined,
    fromId: string,
    participants: GroupParticipant[],
    fallback: string,
): string => {
    const name = String(candidate || "").trim();
    const participant = participants.find((p) => participantIdentityMatches(p.id, fromId));
    if (name && name !== fromId && !looksLikeRawParticipantId(name)) return name;
    return participant ? mentionLabelFromParticipant(participant) : fallback;
};

const sameHistoryParticipant = (left: string | undefined, right: string | undefined): boolean => {
    const a = String(left || "").trim();
    const b = String(right || "").trim();
    if (!a || !b) return a === b;
    return participantIdentityMatches(a, b);
};

const participantIdentityMatchesAny = (keys: Set<string>, participantId: string | undefined): boolean => {
    if (!keys.size) return false;
    const aliases = new Set<string>();
    addParticipantIdentityKeys(aliases, participantId);
    for (const alias of aliases) {
        if (keys.has(alias)) return true;
    }
    return false;
};

function dedupeByHistoryParticipantIdentity<T extends { id?: string }>(participants: T[]): T[] {
    const seen = new Set<string>();
    const out: T[] = [];
    for (const participant of participants) {
        const id = String(participant.id || "").trim();
        if (!id) continue;
        const before = seen.size;
        addParticipantIdentityKeys(seen, id);
        if (seen.size !== before) out.push(participant);
    }
    return out;
}

const MENTION_TRIGGER_PATTERN = /(^|[^A-Za-z0-9_.-])@([^\s@]*)$/;

const normalizeHistoryParticipantId = normalizeParticipantId;

const HISTORY_MENTION_BOUNDARY = "[^A-Za-z0-9_.-]";

const hasHistoryMention = (content: string, label: string): boolean => {
    const escaped = String(label || "").trim().replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    if (!escaped) return false;
    return new RegExp(`(^|${HISTORY_MENTION_BOUNDARY})@${escaped}(?=$|${HISTORY_MENTION_BOUNDARY})`).test(content);
};

const mentionLabelsForHistoryParticipant = (participant: MentionParticipant): string[] => {
    const labels = new Set<string>();
    const name = String(participant.name || "").trim();
    if (name) labels.add(name);
    if (isLocalParticipantId(participant.id) || isLocalAIName(name)) {
        labels.add(LOCAL_AI_DISPLAY_NAME_EN);
        labels.add(LOCAL_AI_DISPLAY_NAME_ZH_HANS);
        labels.add(LOCAL_AI_DISPLAY_NAME_ZH_HANT);
        labels.add("\u672c\u5730AI");
        labels.add("\u672c\u5730 AI");
        labels.add("\u672c\u673aAI");
        labels.add("\u672c\u673a AI");
        labels.add("\u672c\u6a5f AI");
        labels.add("\u672c\u5730\u667a\u80fd\u4f53");
        labels.add("\u672c\u673a\u667a\u80fd\u4f53");
    }
    return [...labels];
};

const mentionedHistoryParticipantIds = (content: string, participants: MentionParticipant[]): string[] => {
    const mentioned = new Set<string>();
    for (const participant of participants) {
        if (mentionLabelsForHistoryParticipant(participant).some((label) => hasHistoryMention(content, label))) {
            mentioned.add(participant.id);
        }
    }
    return [...mentioned];
};

const hasUnresolvedHistoryMentionTrigger = (content: string): boolean => /(^|[^A-Za-z0-9_.-])@[^\s@]+/.test(content);

const historyTargetParticipantIds = (content: string, participants: MentionParticipant[]): string[] => {
    const mentioned = mentionedHistoryParticipantIds(content, participants);
    if (mentioned.length > 0 || hasUnresolvedHistoryMentionTrigger(content)) return mentioned;
    return [];
};

const participantFallbackName = (id: string, index: number, lang: string | undefined): string => {
    const normalized = normalizeHistoryParticipantId(id);
    if (normalized === "me" || normalized === "user") return textForLang(lang, "Me", "\u6211", "\u6211");
    if (normalized === LEGACY_LOCAL_AI_PARTICIPANT_ID) return localAINameForLang(lang);
    return textForLang(lang, `Participant ${index + 1}`, `\u53c2\u4e0e\u8005${index + 1}`, `\u53c3\u8207\u8005${index + 1}`);
};

const readableParticipantName = (name: string | undefined, id: string, index: number, lang: string | undefined): string => {
    const candidate = String(name || "").trim();
    if (candidate && candidate !== id && !looksLikeRawParticipantId(candidate)) return candidate;
    return participantFallbackName(id, index, lang);
};
export function HistoryGroupDiscussionTab({ discussionId, title, readOnly, theme, lang }: HistoryGroupDiscussionTabProps) {
    const [detail, setDetail] = useState<HistoryDiscussionDetail | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [input, setInput] = useState("");
    const [sending, setSending] = useState(false);
    const [downloadingKey, setDownloadingKey] = useState("");
    const [downloadedPaths, setDownloadedPaths] = useState<Record<string, string>>({});
    const [optimisticMessages, setOptimisticMessages] = useState<NonNullable<HistoryDiscussionDetail["messages"]>>([]);
    const [mentionOpen, setMentionOpen] = useState(false);
    const [mentionQuery, setMentionQuery] = useState("");
    const [mentionStart, setMentionStart] = useState(-1);
    const [mentionSelectedIndex, setMentionSelectedIndex] = useState(0);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const focusTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const reloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const loadSeqRef = useRef(0);
    const mentionParticipantsRef = useRef<MentionParticipant[]>([]);
    const load = useCallback(async (options?: { silent?: boolean }) => {
        if (!discussionId) return;
        const seq = loadSeqRef.current + 1;
        loadSeqRef.current = seq;
        if (!options?.silent) setLoading(true);
        setError("");
        try {
            const nextDetail = await GroupDiscussionGetConsultationDetail(discussionId);
            if (loadSeqRef.current === seq) {
                setDetail(nextDetail);
                // Remove optimistic messages that are now present in the
                // authoritative detail. Match by content (created_at format may
                // differ between client and Hub, so we cannot rely on it).
                const detailMsgs = nextDetail?.messages || [];
                if (detailMsgs.length > 0) {
                    setOptimisticMessages((prev) => prev.filter((pending) =>
                        !detailMsgs.some((m: any) => String(m.content || m.Content || "") === String(pending.content || ""))
                    ));
                }
            }
        } catch (e) {
            if (loadSeqRef.current === seq) setError(String(e));
        } finally {
            if (loadSeqRef.current === seq) setLoading(false);
        }
    }, [discussionId]);

    useEffect(() => { void load(); }, [load]);

    useEffect(() => {
        setOptimisticMessages([]);
    }, [discussionId]);

    const scheduleSilentLoad = useCallback(() => {
        if (reloadTimerRef.current) clearTimeout(reloadTimerRef.current);
        reloadTimerRef.current = setTimeout(() => {
            reloadTimerRef.current = null;
            void load({ silent: true });
        }, historyDetailReloadDebounceMs);
    }, [load]);

    useEffect(() => () => {
        if (focusTimerRef.current) clearTimeout(focusTimerRef.current);
        if (reloadTimerRef.current) clearTimeout(reloadTimerRef.current);
    }, []);

    useEffect(() => {
        if (!discussionId) return;
        const maybeReload = (event: any) => {
            if (eventDiscussionId(event) === discussionId) scheduleSilentLoad();
        };
        const maybeReloadNonStream = (event: any) => {
            const kind = eventDiscussionKind(event);
            if (kind === "stream_chunk" || kind === "stream_end") return;
            maybeReload(event);
        };
        const offDiscussion = EventsOn("ve-event", maybeReloadNonStream);
        const offStreamEnd = EventsOn("ve:stream_end", maybeReload);
        return () => {
            if (reloadTimerRef.current) {
                clearTimeout(reloadTimerRef.current);
                reloadTimerRef.current = null;
            }
            if (typeof offDiscussion === "function") offDiscussion();
            else EventsOff("ve-event");
            if (typeof offStreamEnd === "function") offStreamEnd();
            else EventsOff("ve:stream_end");
        };
    }, [discussionId, scheduleSilentLoad]);

    const detailStatus = String(detail?.discussion?.status || "").trim().toLowerCase();
    const detailRelation = String(detail?.discussion?.local_relation || detail?.discussion?.role || "").trim();
    const detailHasReadonly = typeof detail?.discussion?.readonly === "boolean";
    const detailHasAccessState = !!detail?.discussion && ((detailStatus !== "" && detailStatus !== "open") || !!detailRelation || detailHasReadonly);
    const detailAccessReadOnly = detailHasAccessState ? isHistoryDiscussionReadOnly(detail.discussion) : false;
    const effectiveReadOnly = detailHasAccessState ? detailAccessReadOnly : readOnly;

    const scheduleInputFocus = useCallback((position?: number) => {
        if (focusTimerRef.current) clearTimeout(focusTimerRef.current);
        focusTimerRef.current = setTimeout(() => {
            focusTimerRef.current = null;
            const target = position ?? inputRef.current?.value.length ?? 0;
            inputRef.current?.focus();
            inputRef.current?.setSelectionRange(target, target);
        }, 0);
    }, []);

    const send = useCallback(async () => {
        const content = input.trim();
        if (!content || effectiveReadOnly || sending) return;
        setSending(true);
        setError("");
        const createdAt = new Date().toISOString();
        try {
            const toIDs = historyTargetParticipantIds(content, mentionParticipantsRef.current);
            const outgoing = { kind: "statement", content, created_at: createdAt, ...(toIDs.length ? { to_ids: toIDs } : {}) };
            await GroupDiscussionSendHistoryMessage(discussionId, outgoing as a2a.GroupDiscussionMessage);
            setInput("");
            setOptimisticMessages((prev) => [...prev, {
                id: `local-${Date.now()}`,
                from_id: "me",
                from_name: textForLang(lang, "Me", "\u6211", "\u6211"),
                kind: "statement",
                content,
                created_at: createdAt,
            }]);
            void load({ silent: true });
            return true;
        } catch (e) {
            setError(String(e));
            return false;
        } finally {
            setSending(false);
        }
    }, [discussionId, effectiveReadOnly, input, lang, load, sending]);

    const insertMention = useCallback((participant: GroupParticipant) => {
        if (effectiveReadOnly) return;
        const name = mentionLabelFromParticipant(participant);
        if (!name) return;
        const mention = `@${name} `;
        const textarea = inputRef.current;
        const caret = textarea?.selectionStart ?? input.length;
        const prefix = input.slice(0, caret);
        const suffix = input.slice(caret).replace(/^\s+/, "");
        const spacer = prefix && !prefix.endsWith(" ") ? " " : "";
        const next = `${prefix}${spacer}${mention}${suffix}`;
        setInput(next);
        scheduleInputFocus(prefix.length + spacer.length + mention.length);
    }, [effectiveReadOnly, input, scheduleInputFocus]);
    const addHistoryParticipant = useCallback(async (veId: string) => {
        const toId = String(veId || "").trim();
        if (!toId || effectiveReadOnly || sending) return false;
        setSending(true);
        setError("");
        try {
            await GroupDiscussionSendInvitation(discussionId, { to_id: toId, role: "speak", trusted: true } as a2a.GroupInvitation);
            await load();
            return true;
        } catch (e) {
            setError(String(e));
            return false;
        } finally {
            setSending(false);
        }
    }, [discussionId, effectiveReadOnly, load, sending]);

    const participants: GroupParticipant[] = useMemo(() => {
        const messageNameMap = new Map<string, string>();
        for (const m of detail?.messages || []) {
            const fromId = String(m.from_id || "").trim();
            const fromName = String(m.from_name || "").trim();
            if (fromId && fromName && fromName !== fromId && !looksLikeRawParticipantId(fromName)) {
                messageNameMap.set(fromId, fromName);
            }
        }
        const messageNames = Object.fromEntries(messageNameMap);

        const sessionParticipants = detail?.session?.participants || [];
        if (sessionParticipants.length > 0) {
            return dedupeByHistoryParticipantIdentity(sessionParticipants
                .filter((p) => String(p.id || "").trim())
                .map((p, index) => {
                    const id = String(p.id || "").trim();
                    const resolvedName = readableParticipantName(p.name || participantNameForIdentity(messageNames, id), id, index, lang);
                    const role = String(p.role_code || "").trim();
                    return { id, name: role ? `${resolvedName} (${role})` : resolvedName, online: true };
                }));
        }

        return dedupeByHistoryParticipantIdentity((detail?.discussion?.participant_ids || []).map((id, index) => {
            const resolved = participantNameForIdentity(messageNames, id);
            const displayName = resolved || participantFallbackName(id, index, lang);
            return { id, name: displayName, online: true };
        }));
    }, [detail?.discussion?.participant_ids, detail?.messages, detail?.session?.participants, lang]);

    const localHistoryUserIds = useMemo(() => {
        const relation = String(detail?.discussion?.local_relation || "").trim().toLowerCase();
        if (relation !== "initiated_by_me") return [];

        const ids = new Set(["initiator", "me", "user"]);
        for (const participant of detail?.session?.participants || []) {
            const id = String(participant.id || "").trim();
            const role = String(participant.role_code || "").trim().toLowerCase();
            if (id && role === "initiator") ids.add(id);
        }
        return [...ids];
    }, [detail?.discussion?.local_relation, detail?.session?.participants]);

    const visibleParticipants = useMemo(() => {
        const localIDs = new Set<string>();
        localHistoryUserIds.forEach((id) => addParticipantIdentityKeys(localIDs, id));
        return dedupeByHistoryParticipantIdentity(participants
            .filter((participant) => !participantIdentityMatchesAny(localIDs, participant.id))
            .map((participant) => ({
                ...participant,
                isLocal: isLocalParticipantId(participant.id) || isLocalAIName(participant.name),
            })));
    }, [localHistoryUserIds, participants]);

    const closeMentionPopover = useCallback(() => {
        setMentionOpen(false);
        setMentionQuery("");
        setMentionStart(-1);
        setMentionSelectedIndex(0);
    }, []);

    const mentionParticipants: MentionParticipant[] = useMemo(() =>
        visibleParticipants.map((p) => ({ id: p.id, name: mentionLabelFromParticipant(p), online: p.online })),
        [visibleParticipants]
    );

    useEffect(() => {
        mentionParticipantsRef.current = mentionParticipants;
    }, [mentionParticipants]);

    const updateMentionState = useCallback((value: string, caret: number | null | undefined) => {
        if (effectiveReadOnly || !mentionParticipants.length || caret == null) {
            closeMentionPopover();
            return;
        }
        const beforeCaret = value.slice(0, caret);
        const match = beforeCaret.match(MENTION_TRIGGER_PATTERN);
        if (!match) {
            closeMentionPopover();
            return;
        }
        const query = match[2] || "";
        const normalizedQuery = query.trim().toLowerCase();
        const hasMatches = !normalizedQuery || mentionParticipants.some((p) =>
            p.name.toLowerCase().includes(normalizedQuery)
        );
        if (!hasMatches) {
            closeMentionPopover();
            return;
        }
        setMentionStart(beforeCaret.length - query.length - 1);
        setMentionQuery(query);
        setMentionSelectedIndex(0);
        setMentionOpen(true);
    }, [closeMentionPopover, effectiveReadOnly, mentionParticipants]);

    const mentionFiltered = useMemo(() => {
        const query = mentionQuery.trim().toLowerCase();
        if (!query) return mentionParticipants;
        return mentionParticipants.filter((p) =>
            p.name.toLowerCase().includes(query)
        );
    }, [mentionParticipants, mentionQuery]);

    const insertMentionParticipant = useCallback((participant: MentionParticipant) => {
        if (effectiveReadOnly) return;
        const textarea = inputRef.current;
        const caret = textarea?.selectionStart ?? input.length;
        const start = mentionStart >= 0 ? mentionStart : caret;
        const mention = `@${participant.name} `;
        const next = `${input.slice(0, start)}${mention}${input.slice(caret)}`;
        const nextCaret = start + mention.length;
        setInput(next);
        closeMentionPopover();
        scheduleInputFocus(nextCaret);
    }, [closeMentionPopover, effectiveReadOnly, input, mentionStart, scheduleInputFocus]);

    const mentionKeyDown = useMentionKeyboard(mentionOpen, mentionFiltered, mentionSelectedIndex, setMentionSelectedIndex, insertMentionParticipant, closeMentionPopover);

    useEffect(() => {
        if (effectiveReadOnly) closeMentionPopover();
    }, [closeMentionPopover, effectiveReadOnly]);

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

    const messages: GroupMessage[] = useMemo(() => {
        const merged: GroupMessage[] = [];
        let lastStreamFrom = "";
        let lastStreamIndex = -1;

        const detailMessages = detail?.messages || [];
        const pendingOptimisticMessages = optimisticMessages.filter((pending) =>
            !detailMessages.some((m) => String(m.content || "") === String(pending.content || ""))
        );

        [...detailMessages, ...pendingOptimisticMessages].forEach((m, idx) => {
            const kind = String(m.kind || "").trim().toLowerCase();
            if (kind === "stream_end") {
                lastStreamFrom = "";
                lastStreamIndex = -1;
                return;
            }

            const attachments = buildMessageAttachments(m);
            const fromId = m.from_id || "unknown";
            const content = String(m.content || "");
            if (kind === "stream_chunk" && !content && attachments.length === 0) return;

            if (kind === "stream_chunk" && lastStreamIndex >= 0 && sameHistoryParticipant(lastStreamFrom, fromId)) {
                const existing = merged[lastStreamIndex];
                existing.content += content;
                if (attachments.length > 0) {
                    existing.attachments = [...(existing.attachments || []), ...attachments];
                }
                return;
            }

            const isLocalHistoryUser = localHistoryUserIds.some((id) => participantIdentityMatches(id, fromId));
            const message: GroupMessage = {
                id: m.id || `m-${idx}`,
                fromId,
                fromName: isLocalHistoryUser
                    ? textForLang(lang, "Me", "\u6211", "\u6211")
                    : readableHistorySpeakerName(m.from_name, fromId, participants, participantFallbackName(fromId, idx, lang)),
                content,
                timestamp: m.created_at ? Date.parse(m.created_at) || Date.now() : Date.now(),
                attachments,
            };
            merged.push(message);
            if (kind === "stream_chunk") {
                lastStreamFrom = fromId;
                lastStreamIndex = merged.length - 1;
            } else {
                lastStreamFrom = "";
                lastStreamIndex = -1;
            }
        });

        return merged;
    }, [buildMessageAttachments, detail?.messages, lang, localHistoryUserIds, optimisticMessages, participants]);

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
            const localPath = result?.local_path || "";
            attachment.localPath = localPath;
            setDownloadedPaths((prev) => ({ ...prev, [attachment.fileUrl || key]: localPath }));
        } catch (e) {
            setError(String(e));
        } finally {
            setDownloadingKey("");
        }
    }, [discussionId, downloadingKey]);

    const subtitle = effectiveReadOnly
        ? (detailStatus && detailStatus !== "open"
            ? textForLang(lang, "Ended - read-only", "\u5df2\u7ed3\u675f - \u53ea\u8bfb", "\u5df2\u7d50\u675f - \u552f\u8b80")
            : textForLang(lang, "Read-only - invited digital employee", "\u53ea\u8bfb - \u6211\u7684\u6570\u5b57\u5458\u5de5\u53d7\u9080\u53c2\u52a0", "\u552f\u8b80 - \u6211\u7684\u6578\u5b57\u54e1\u5de5\u53d7\u9080\u53c3\u52a0"))
        : textForLang(lang, "Started by me - can continue", "\u6211\u53d1\u8d77 - \u53ef\u7ee7\u7eed\u8ba8\u8bba", "\u6211\u767c\u8d77 - \u53ef\u7e7c\u7e8c\u8a0e\u8ad6");

    const composerDisabled = effectiveReadOnly || sending;
    const sendDisabled = composerDisabled || !input.trim();
    const composerPlaceholder = effectiveReadOnly
        ? textForLang(lang, "Read-only session", "\u53ea\u8bfb\u4f1a\u8bdd\uff0c\u4e0d\u80fd\u7ee7\u7eed\u53d1\u8a00", "\u552f\u8b80\u6703\u8a71\uff0c\u4e0d\u80fd\u7e7c\u7e8c\u767c\u8a00")
        : textForLang(lang, "Continue discussion...", "\u7ee7\u7eed\u8ba8\u8bba...", "\u7e7c\u7e8c\u8a0e\u8ad6...");

    const composer = <div data-testid="history-group-composer-row" style={{ display: "flex", alignItems: "flex-end", gap: 8, padding: "8px 12px", borderTop: `1px solid ${theme.divider}`, background: theme.inputBarBg, opacity: effectiveReadOnly ? 0.72 : 1 }}>
        <div style={{ position: "relative", flex: "1 1 auto", minWidth: 0, display: "flex" }}>
            {mentionOpen && (
                <MentionPopover
                    filtered={mentionFiltered}
                    selectedIndex={mentionSelectedIndex}
                    onSelect={insertMentionParticipant}
                    onHover={setMentionSelectedIndex}
                    onClose={closeMentionPopover}
                    anchorRef={inputRef}
                    theme={theme}
                    lang={lang}
                />
            )}
            <textarea
                ref={inputRef}
                value={input}
                onChange={(e) => {
                    setInput(e.target.value);
                    updateMentionState(e.target.value, e.currentTarget.selectionStart);
                }}
                onClick={(e) => updateMentionState(input, e.currentTarget.selectionStart)}
                onKeyUp={(e) => {
                    if (["ArrowDown", "ArrowUp", "Enter", "Escape"].includes(e.key)) return;
                    updateMentionState(e.currentTarget.value, e.currentTarget.selectionStart);
                }}
                onKeyDown={(e) => {
                    if (mentionKeyDown(e)) return;
                    if (isLineBreakShortcut(e)) {
                        e.preventDefault();
                        insertTextareaLineBreak(e.currentTarget, setInput);
                        return;
                    }
                    if (isPlainEnter(e)) {
                        e.preventDefault();
                        void send();
                    }
                }}
                disabled={composerDisabled}
                placeholder={composerPlaceholder}
                rows={1}
                style={{ width: "100%", boxSizing: "border-box", display: "block", resize: "none", border: `1px solid ${theme.fieldBorder}`, borderRadius: 6, padding: "6px 10px", color: theme.inputText, background: effectiveReadOnly ? theme.fieldBg : theme.bg, outline: "none", fontSize: 13 }}
            />
        </div>
        <button type="button" onClick={() => void send()} disabled={sendDisabled} style={{ border: "none", background: theme.sendBtnBg, color: theme.sendBtnColor, borderRadius: 6, width: 54, minWidth: 54, height: 34, padding: "0 10px", cursor: sendDisabled ? "default" : "pointer", opacity: sendDisabled ? 0.4 : 1, fontSize: 13, fontWeight: 500, transition: "opacity 0.15s", flexShrink: 0, whiteSpace: "nowrap", display: "inline-flex", alignItems: "center", justifyContent: "center" }}>{sending ? "..." : textForLang(lang, "Send", "\u53d1\u9001", "\u50b3\u9001")}</button>
    </div>;

    return <div data-testid={`ai-history-group-tab-${discussionId}`} style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0, background: theme.bg }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "7px 12px", borderBottom: `1px solid ${theme.divider}`, background: theme.inputBarBg }}>
            <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
                    <div style={{ color: theme.text, fontSize: 13, fontWeight: 700, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{title}</div>
                    {effectiveReadOnly && <span style={{ flexShrink: 0, border: `1px solid ${theme.fieldBorder}`, borderRadius: 4, padding: "1px 5px", color: theme.textMuted, fontSize: 11 }}>{textForLang(lang, "Read-only", "\u53ea\u8bfb", "\u552f\u8b80")}</span>}
                </div>
                <div style={{ color: theme.textMuted, fontSize: 11 }}>{subtitle}</div>
            </div>
            <button type="button" onClick={() => void load()} disabled={loading} style={{ border: `1px solid ${theme.fieldBorder}`, background: theme.fieldBg, color: theme.text, borderRadius: 6, padding: "4px 10px", cursor: loading ? "default" : "pointer", fontSize: 12 }}>{loading ? "..." : textForLang(lang, "Refresh", "\u5237\u65b0", "\u91cd\u65b0\u6574\u7406")}</button>
        </div>

        {error && <div role="alert" style={{ padding: "7px 12px", color: theme.errorText, background: theme.errorBg, borderBottom: `1px solid ${theme.errorBorder}`, fontSize: 12 }}>{error}</div>}
        {loading && !detail
            ? <div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", color: theme.textMuted }}>{textForLang(lang, "Loading...", "\u52a0\u8f7d\u4e2d...", "\u8f09\u5165\u4e2d...")}</div>
            : <div style={{ flex: 1, minHeight: 0, overflow: "hidden", display: "flex", flexDirection: "row" }}>
                <div data-testid="history-group-main-column" style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}>
                    <VEGroupChatView sessionId={discussionId} participants={participants} messages={messages} theme={theme} lang={lang} onDownloadAttachment={downloadAttachment} allowParticipantAdd={false} showHeader={false} localUserIds={localHistoryUserIds} containerStyle={{ flex: 1, minHeight: 0 }} />
                    {composer}
                </div>
                <GroupParticipantPanel participants={visibleParticipants} theme={theme} lang={lang} sessionId={discussionId} readOnly={effectiveReadOnly} onTalkTo={insertMention} onAddParticipant={addHistoryParticipant} />
            </div>}
    </div>;
}
