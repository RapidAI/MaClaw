import { parkReplacedStreamInReasoning } from "../ai/assistantReasoningBody";
import { sanitizeVisibleChatText } from "../ai/visibleChatText";

/** Split one companion token. A leading U+0001 is the shared reasoning lane.
 *  The same lane marker and undecodable glyphs (U+25A1 and the replacement
 *  block) have no font glyph and would render as squares inside the bubble. */
export function splitCompanionStreamDelta(delta: string): { reasoning: string; text: string } {
    if (!delta) return { reasoning: "", text: "" };
    if (delta.startsWith("\x01")) {
        return { reasoning: sanitizeVisibleChatText(delta.slice(1)), text: "" };
    }
    return { reasoning: "", text: sanitizeVisibleChatText(delta) };
}

export function companionVisibleText(value: string): string {
    return sanitizeVisibleChatText(value);
}

function mergeCompanionReasoning(prior: string, incoming: string): string {
    if (!incoming) return prior;
    if (!prior || prior === incoming) return incoming;
    if (incoming.startsWith(prior) || prior.startsWith(incoming)) {
        return incoming.length >= prior.length ? incoming : prior;
    }
    return `${prior}\n\n${incoming}`;
}

/** Keep the answer and the thought apart when the final payload arrives. */
export function finishCompanionReply(
    streamedText: string,
    streamedReasoning: string,
    finalText: string,
    finalReasoning: string,
): { text: string; reasoning: string } {
    const body = sanitizeVisibleChatText(finalText).trim();
    const think = mergeCompanionReasoning(
        sanitizeVisibleChatText(streamedReasoning).trim(),
        sanitizeVisibleChatText(finalReasoning).trim(),
    );
    return {
        text: body,
        reasoning: parkReplacedStreamInReasoning(streamedText, body, think),
    };
}
