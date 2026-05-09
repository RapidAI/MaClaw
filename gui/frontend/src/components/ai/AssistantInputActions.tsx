import type { PointerEvent } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { UseVoiceInputResult } from "./useVoiceInput";
import { AssistantInputIcon, getInputActionButtonStyle, type Theme } from "./aiAssistantPanelTheme";
import { VoiceLevelVisualizer } from "./aiAssistantControls";

interface AssistantInputActionsProps {
    browseFile: () => void;
    canSend: boolean;
    cancelSession?: unknown;
    handleCancel: () => void;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: PointerEvent<HTMLButtonElement>) => void;
    finishVoicePointer: (event: PointerEvent<HTMLButtonElement>) => void;
    inputLocked: boolean;
    isBusy: boolean;
    lang: string;
    ready: boolean;
    showBusySpinner: boolean;
    theme: Theme;
    themeMode: "light" | "dark";
    voiceInput: UseVoiceInputResult;
}

export function AssistantInputActions({ browseFile, canSend, cancelSession, finishVoicePointer, handleCancel, handleSend, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave, inputLocked, isBusy, lang, ready, showBusySpinner, theme: t, themeMode, voiceInput }: AssistantInputActionsProps) {
    const voiceDisabled = !ready || voiceInput.state === "transcribing" || !voiceInput.asrReady;
    return (
        <>
            <button type="button" onClick={browseFile} disabled={!ready || inputLocked} style={getInputActionButtonStyle(t, themeMode, "attach", !ready || inputLocked)} title={localizeText(lang, "Choose file", "\u9009\u62e9\u6587\u4ef6")}>
                <AssistantInputIcon name="paperclip" />
            </button>
            <button type="button" onClick={handleVoiceClick} onPointerDown={handleVoicePointerDown} onPointerUp={finishVoicePointer} onPointerCancel={finishVoicePointer} onPointerLeave={handleVoicePointerLeave} onContextMenu={(e) => e.preventDefault()} disabled={voiceDisabled} data-testid="ai-voice-input" style={{ ...getInputActionButtonStyle(t, themeMode, voiceInput.state === "listening" ? "voiceHold" : "voice", voiceDisabled), position: "relative", touchAction: "none", overflow: "hidden" }} title={!voiceInput.asrReady ? localizeText(lang, "Voice input unavailable - enable ASR model first", "\u8bed\u97f3\u8f93\u5165\u4e0d\u53ef\u7528\uff0c\u8bf7\u5148\u542f\u7528 ASR \u6a21\u578b") : voiceInput.state === "listening" ? localizeText(lang, "Listening - click to stop", "\u76d1\u542c\u4e2d\uff0c\u70b9\u51fb\u505c\u6b62") : voiceInput.state === "transcribing" ? localizeText(lang, "Transcribing...", "\u8bc6\u522b\u4e2d...") : localizeText(lang, "Voice input", "\u8bed\u97f3\u8f93\u5165")} aria-label={localizeText(lang, "Voice input", "\u8bed\u97f3\u8f93\u5165")}>
                {voiceInput.state === "transcribing" ? (
                    <span aria-hidden="true" style={{ display: "inline-block", width: "14px", height: "14px", borderRadius: "50%", border: `2px solid ${t.textMuted}`, borderTopColor: "transparent", animation: "ai-spinner-spin 0.8s linear infinite" }} />
                ) : voiceInput.state === "listening" ? (
                    <VoiceLevelVisualizer onAudioLevelRef={voiceInput.onAudioLevelRef} isSpeaking={voiceInput.isSpeaking} themeColor="#ffffff" speakingColor={themeMode === "dark" ? "#fbbf24" : "#dc2626"} />
                ) : (
                    <AssistantInputIcon name="mic" />
                )}
            </button>
            {voiceInput.error && <span style={{ color: t.errorText, fontSize: "11px", alignSelf: "center", maxWidth: "160px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={voiceInput.error}>{voiceInput.error}</span>}
            {isBusy && cancelSession ? (
                <button type="button" onClick={handleCancel} data-testid="ai-cancel-progress" style={getInputActionButtonStyle(t, themeMode, "cancel")} title={localizeText(lang, "Cancel", "\u53d6\u6d88")} aria-label={localizeText(lang, "Cancel", "\u53d6\u6d88")}>
                    {showBusySpinner ? <span aria-hidden="true" style={{ width: "18px", height: "18px", borderRadius: "50%", border: `2px solid ${themeMode === "dark" ? "rgba(221, 214, 254, 0.24)" : "rgba(79, 70, 229, 0.18)"}`, borderTopColor: themeMode === "dark" ? "#ddd6fe" : "#4f46e5", borderRightColor: themeMode === "dark" ? "#ddd6fe" : "#4f46e5", animation: "ai-spinner-spin 0.8s linear infinite" }} /> : <AssistantInputIcon name="stop" />}
                    <span style={{ position: "absolute", opacity: 0, pointerEvents: "none" }}>{localizeText(lang, "Cancel", "\u53d6\u6d88")}</span>
                </button>
            ) : (
                <button type="button" onClick={handleSend} disabled={!canSend} style={getInputActionButtonStyle(t, themeMode, canSend ? "send" : "neutral", !canSend)} title={localizeText(lang, "Send", "\u53d1\u9001")}>
                    {isBusy ? <span style={{ width: "16px", height: "16px", borderRadius: "50%", border: `2px solid ${t.textMuted}`, borderTopColor: "transparent", animation: "ai-spinner-spin 0.8s linear infinite" }} /> : <AssistantInputIcon name="cornerDownLeft" />}
                </button>
            )}
        </>
    );
}
