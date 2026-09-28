import { contentHasAssistantToolCall } from "./assistantToolCall";

/**
 * Clear the previous round's answer draft when the first token of a later
 * round arrives. The reasoning trail is kept: the multi-round working
 * narrative belongs to the thinking panel and must accumulate across rounds.
 * A trailing newline separates the next round's thinking from the previous.
 *
 * 2026-09-18: only SHORT filler drafts are wiped. An assistant round can
 * carry substantive prose alongside its tool calls (e.g. a full status
 * report followed by a write_file attempt); wiping it erased the only
 * user-visible copy of the report when the tool call was denied and the
 * final round only said "已汇报" (production: ssh status report invisible).
 * Substantive prose survives and the next round's tokens append after it.
 * A recorded tool-call marker is substantive too: wiping it would drop the
 * only copy of that call from the transcript.
 */
const ROUND_PROSE_PRESERVE_MIN_CHARS = 40;

export function clearAssistantRoundProse<T extends { content?: string; reasoning?: string }>(message: T): T {
    const reasoning = message.reasoning;
    const separated = reasoning && !reasoning.endsWith("\n") ? `${reasoning}\n` : reasoning;
    const keep = (message.content?.length ?? 0) >= ROUND_PROSE_PRESERVE_MIN_CHARS
        || contentHasAssistantToolCall(message.content);
    // The next round's tokens append directly after the preserved prose; a
    // blank line between rounds keeps markdown blocks from gluing together
    // (the original wipe design needed no separator because content was "" ).
    const content = keep ? ((message.content?.endsWith("\n\n") ?? false) ? message.content : `${message.content ?? ""}\n\n`) : "";
    return { ...message, content, reasoning: separated };
}
