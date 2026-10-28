export function readFileCompanionBootFlag(): boolean {
    const flag = (window as unknown as { __MACLAW_FILE_COMPANION__?: boolean }).__MACLAW_FILE_COMPANION__;
    return flag === true;
}

const schemeToken = /^[a-z0-9](?:[a-z0-9-]{0,31})?$/;

function storedScheme(key: string, fallback: string): string {
    try {
        const id = (window.localStorage.getItem(key) ?? "").trim();
        return schemeToken.test(id) ? id : fallback;
    } catch {
        return fallback;
    }
}

type StoredAppearance = {
    themeMode: "light" | "dark";
    darkScheme: string;
    lightScheme: string;
};

// The primary key wins. A leftover legacy value must not turn an explicit
// light choice dark; the boot palette and the main window use this order.
function readStoredAppearance(): StoredAppearance | null {
    try {
        const mode = window.localStorage.getItem("ai_assistant_theme_mode")
            || window.localStorage.getItem("maclaw.ai.themeMode");
        return {
            themeMode: mode === "dark" ? "dark" : "light",
            darkScheme: storedScheme("maclaw.ai.darkScheme", "onyx"),
            lightScheme: storedScheme("maclaw.ai.lightScheme", "fluent"),
        };
    } catch {
        return null;
    }
}

type AppearancePublisher = (value: StoredAppearance) => Promise<unknown>;

function appearancePublisher(): AppearancePublisher | undefined {
    const set = (window as unknown as { go?: { main?: { App?: { SetUIAppearance?: AppearancePublisher } } } }).go?.main?.App?.SetUIAppearance;
    return typeof set === "function" ? set : undefined;
}

let publishTail: Promise<void> = Promise.resolve();

// One save at a time. A later choice has to win, so a slow earlier save
// finishes before the next one starts.
export function enqueueUIAppearancePublish(set: AppearancePublisher, payload: StoredAppearance): void {
    publishTail = publishTail.then(() => (
        Promise.resolve()
            .then(() => set(payload))
            .then(() => undefined, () => undefined)
    ));
}

// Write the stored shell theme before the main window's bundle finishes
// loading. The companion is another process and can open during that gap;
// waiting for the app chunk left it on the previous file.
export function publishStoredUIAppearance(): void {
    const set = appearancePublisher();
    const payload = readStoredAppearance();
    if (!set || !payload) return;
    enqueueUIAppearancePublish(set, payload);
}

// Stamp the shell theme before the companion bundle loads. The loading shell
// is on screen first, and pulling the palette tables into the entry would
// delay both windows.
export function applyCompanionBootTheme(): void {
    const root = document.documentElement;
    const stored = readStoredAppearance() ?? { themeMode: "light", darkScheme: "onyx", lightScheme: "fluent" };
    root.setAttribute("data-ai-theme", stored.themeMode);
    root.setAttribute("data-boot-theme", stored.themeMode);
    if (stored.themeMode === "dark") {
        root.setAttribute("data-ai-dark-scheme", stored.darkScheme);
        root.removeAttribute("data-ai-light-scheme");
        root.style.colorScheme = "dark";
        return;
    }
    root.setAttribute("data-ai-light-scheme", stored.lightScheme);
    root.removeAttribute("data-ai-dark-scheme");
    root.style.colorScheme = "light";
}
