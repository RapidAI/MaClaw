import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantUpdateNotice } from "../AssistantUpdateNotice";
import { lightTheme } from "../aiAssistantPanelTheme";

describe("AssistantUpdateNotice", () => {
    afterEach(() => {
        vi.restoreAllMocks();
    });

    it("opens update menu and routes online update", () => {
        const onOpen = vi.fn();
        const onDismiss = vi.fn();
        render(
            <AssistantUpdateNotice
                inline={false}
                lang="en"
                onDismissAppUpdate={onDismiss}
                onOpenAppUpdate={onOpen}
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ has_update: true, latest_version: "V1.2.3" }}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "New version V1.2.3 available" }));
        expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /Online update/i }));
        fireEvent.click(screen.getByRole("menuitem", { name: /Online update/i }));

        expect(onOpen).toHaveBeenCalledTimes(1);
        expect(onDismiss).not.toHaveBeenCalled();
    });

    it("dismisses only current version", () => {
        const onDismiss = vi.fn();
        render(
            <AssistantUpdateNotice
                inline={false}
                lang="en"
                onDismissAppUpdate={onDismiss}
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ has_update: true, latest_version: "V9.9.9" }}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "New version V9.9.9 available" }));
        fireEvent.click(screen.getByRole("menuitem", { name: /Do not remind this time/i }));

        expect(onDismiss).toHaveBeenCalledWith("V9.9.9");
    });

    it("accepts backend-style PascalCase version fields", () => {
        const onDismiss = vi.fn();
        render(
            <AssistantUpdateNotice
                inline={false}
                lang="en"
                onDismissAppUpdate={onDismiss}
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ HasUpdate: true, LatestVersion: "V2.0.0" }}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "New version V2.0.0 available" }));
        fireEvent.click(screen.getByRole("menuitem", { name: /Do not remind this time/i }));

        expect(onDismiss).toHaveBeenCalledWith("V2.0.0");
    });

    it("closes the menu with Escape", () => {
        render(
            <AssistantUpdateNotice
                inline={false}
                lang="en"
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ has_update: true, latest_version: "V1.2.3" }}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "New version V1.2.3 available" }));
        expect(screen.getByRole("menu")).toBeTruthy();

        fireEvent.keyDown(document, { key: "Escape" });

        expect(screen.queryByRole("menu")).toBeNull();
    });

    it("keeps inline mouse clicks from double-toggling the menu", () => {
        render(
            <AssistantUpdateNotice
                inline={true}
                lang="en"
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ has_update: true, latest_version: "V1.2.3" }}
            />
        );

        const trigger = screen.getByRole("button", { name: "New version V1.2.3 available" });
        fireEvent.mouseDown(trigger);
        fireEvent.click(trigger, { detail: 1 });

        expect(screen.getByRole("menu")).toBeTruthy();
    });

    it("opens inline menu from keyboard click", () => {
        render(
            <AssistantUpdateNotice
                inline={true}
                lang="en"
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ has_update: true, latest_version: "V1.2.3" }}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "New version V1.2.3 available" }), { detail: 0 });

        expect(screen.getByRole("menu")).toBeTruthy();
    });

    it("renders nothing when no update is available", () => {
        const { container } = render(
            <AssistantUpdateNotice
                inline={false}
                lang="en"
                theme={lightTheme}
                themeMode="light"
                updateAvailable={null}
            />
        );

        expect(container.firstChild).toBeNull();
    });

    it("renders nothing when payload says no update", () => {
        const { container } = render(
            <AssistantUpdateNotice
                inline={false}
                lang="en"
                theme={lightTheme}
                themeMode="light"
                updateAvailable={{ has_update: false, latest_version: "V1.2.3" }}
            />
        );

        expect(container.firstChild).toBeNull();
    });
});
