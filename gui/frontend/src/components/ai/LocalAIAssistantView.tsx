/**
 * LocalAIAssistantView — the original AI assistant conversation view.
 *
 * This component was extracted from AIAssistantPanel.tsx during the Tab system
 * refactoring. It renders the local AI assistant conversation (type="local")
 * and is displayed when the "AI 助手" tab is active.
 *
 * For now this is a thin wrapper that signals the parent to render the
 * existing conversation UI. The actual conversation logic remains in
 * AIAssistantPanel.tsx since it's deeply integrated with hooks and state.
 * This component serves as the conceptual boundary for the local tab content.
 */

export interface LocalAIAssistantViewProps {
    /** Whether this view is currently visible (active tab) */
    visible: boolean;
}

/**
 * Placeholder component for the local AI assistant view.
 * The actual rendering is handled by the parent AIAssistantPanel
 * via conditional visibility based on the active tab.
 *
 * This component exists to:
 * 1. Establish the conceptual boundary for the local tab content
 * 2. Provide a future extraction point when the conversation logic
 *    is fully decoupled from the panel container
 */
export function LocalAIAssistantView({ visible }: LocalAIAssistantViewProps) {
    if (!visible) return null;
    // The actual content is rendered by the parent AIAssistantPanel
    // This is a structural marker for the tab system architecture
    return null;
}
