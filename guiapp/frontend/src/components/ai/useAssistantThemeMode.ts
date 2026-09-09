import { useEffect, useRef, useState } from "react";
import { isAssistantThemeMode, readStoredAssistantThemeMode, writeStoredAssistantThemeMode, type AssistantThemeMode } from "./assistantThemeStorage";

export function useAssistantThemeMode(
    controlledThemeMode: unknown,
    onThemeModeChange?: (mode: AssistantThemeMode) => void,
) {
    const [themeMode, setThemeMode] = useState<AssistantThemeMode>(() => {
        if (isAssistantThemeMode(controlledThemeMode)) return controlledThemeMode;
        return readStoredAssistantThemeMode();
    });

    const lastControlledThemeModeRef = useRef(controlledThemeMode);

    useEffect(() => {
        if (Object.is(lastControlledThemeModeRef.current, controlledThemeMode)) return;
        lastControlledThemeModeRef.current = controlledThemeMode;
        if (isAssistantThemeMode(controlledThemeMode)) {
            setThemeMode(controlledThemeMode);
        }
    }, [controlledThemeMode]);

    useEffect(() => {
        writeStoredAssistantThemeMode(themeMode);
        onThemeModeChange?.(themeMode);
    }, [onThemeModeChange, themeMode]);

    return { themeMode, setThemeMode };
}
