// Keep the cut rules in step with guiapp/conversation_view_window.go.
export const CONVERSATION_VIEW_HOT_MESSAGES = 40;
const CONVERSATION_VIEW_TURN_ALIGN = 12;
const CONVERSATION_VIEW_OPEN_CARD_LOOKBACK = 30;

export interface ConversationViewMessage {
    role?: string;
    unfinishedSlot?: { status?: string } | null;
    confirmation?: { status?: string } | null;
}

function conversationViewStayHot(message: ConversationViewMessage | undefined): boolean {
    const slotStatus = String(message?.unfinishedSlot?.status || "").trim().toLowerCase();
    if (message?.unfinishedSlot && slotStatus !== "completed" && slotStatus !== "resumed" && slotStatus !== "dismissed") {
        return true;
    }
    const confirmationStatus = String(message?.confirmation?.status || "").trim().toLowerCase();
    return !!message?.confirmation && (confirmationStatus === "" || confirmationStatus === "pending");
}

/** Index of the first message that stays in the live transcript. Earlier rows are archived. */
export function conversationViewCut<T extends ConversationViewMessage>(messages: T[], hot = CONVERSATION_VIEW_HOT_MESSAGES): number {
    const count = messages.length;
    if (count <= hot || hot < 1) return 0;
    let cut = count - hot;
    const roleAt = (index: number) => String(messages[index]?.role || "").trim().toLowerCase();
    if (roleAt(cut) !== "user") {
        const floor = Math.max(0, cut - CONVERSATION_VIEW_TURN_ALIGN);
        for (let index = cut; index >= floor; index--) {
            if (roleAt(index) === "user") {
                cut = index;
                break;
            }
        }
    }
    const cardFloor = Math.max(0, count - hot - CONVERSATION_VIEW_OPEN_CARD_LOOKBACK);
    for (let index = cut - 1; index >= cardFloor; index--) {
        if (conversationViewStayHot(messages[index])) cut = index;
    }
    return cut;
}

export function splitConversationView<T extends ConversationViewMessage>(messages: T[], hot = CONVERSATION_VIEW_HOT_MESSAGES): { visible: T[]; earlier: T[] } {
    const cut = conversationViewCut(messages, hot);
    return { visible: messages.slice(cut), earlier: messages.slice(0, cut) };
}
