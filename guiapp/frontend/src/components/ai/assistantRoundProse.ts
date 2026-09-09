/**
 * Clear the previous round's answer draft when the first token of a later
 * round arrives. The reasoning trail is kept: the multi-round working
 * narrative belongs to the thinking panel and must accumulate across rounds.
 * A trailing newline separates the next round's thinking from the previous.
 */
export function clearAssistantRoundProse<T extends { content?: string; reasoning?: string }>(message: T): T {
    const reasoning = message.reasoning;
    const separated = reasoning && !reasoning.endsWith("\n") ? `${reasoning}\n` : reasoning;
    return { ...message, content: "", reasoning: separated };
}
