import { describe, expect, it, vi } from "vitest";
import {
    applyWindowRestoreGeometry,
    captureWindowRestoreGeometry,
    createWindowMaximizeRestoreSession,
    parseWindowRestoreGeometry,
    WINDOW_RESTORE_CLAMP_SUPPRESS_MS,
    WINDOW_RESTORE_REASSERT_LATE_MS,
    WINDOW_RESTORE_REASSERT_MS,
} from "../windowRestoreGeometry";

describe("parseWindowRestoreGeometry", () => {
    it("reads Wails size fields", () => {
        expect(parseWindowRestoreGeometry({ w: 1280, h: 800 }, { x: 40, y: 60 })).toEqual({
            width: 1280,
            height: 800,
            x: 40,
            y: 60,
        });
    });

    it("rejects geometry too small to be a restored window", () => {
        expect(parseWindowRestoreGeometry({ w: 80, h: 80 }, { x: 0, y: 0 })).toBeNull();
    });
});

describe("captureWindowRestoreGeometry", () => {
    it("snapshots the current window before maximize", async () => {
        const geo = await captureWindowRestoreGeometry({
            getSize: async () => ({ w: 1100, h: 720 }),
            getPosition: async () => ({ x: 12, y: 24 }),
            setSize: vi.fn(),
            setPosition: vi.fn(),
        });
        expect(geo).toEqual({ width: 1100, height: 720, x: 12, y: 24 });
    });

    it("returns null when the window APIs fail", async () => {
        await expect(captureWindowRestoreGeometry({
            getSize: async () => { throw new Error("gone"); },
            getPosition: async () => ({ x: 0, y: 0 }),
            setSize: vi.fn(),
            setPosition: vi.fn(),
        })).resolves.toBeNull();
    });
});

describe("applyWindowRestoreGeometry", () => {
    it("applies size before position so Unmaximise leftover bounds are replaced", () => {
        const calls: string[] = [];
        const applied = applyWindowRestoreGeometry({
            setSize: (width, height) => { calls.push(`size:${width}x${height}`); },
            setPosition: (x, y) => { calls.push(`pos:${x},${y}`); },
        }, { width: 1100, height: 720, x: 12, y: 24 });
        expect(applied).toBe(true);
        expect(calls).toEqual(["size:1100x720", "pos:12,24"]);
    });

    it("no-ops without a snapshot", () => {
        expect(applyWindowRestoreGeometry({ setSize: vi.fn(), setPosition: vi.fn() }, null)).toBe(false);
    });
});

describe("createWindowMaximizeRestoreSession", () => {
    function fakeApi(size = { w: 1100, h: 720 }, position = { x: 12, y: 24 }) {
        const calls: string[] = [];
        return {
            calls,
            api: {
                getSize: async () => size,
                getPosition: async () => position,
                setSize: (width: number, height: number) => { calls.push(`size:${width}x${height}`); },
                setPosition: (x: number, y: number) => { calls.push(`pos:${x},${y}`); },
            },
        };
    }

    it("applies the remembered size and skips later work-area clamps", async () => {
        const scheduled: Array<{ fn: () => void; ms: number }> = [];
        let now = 1_000;
        const { calls, api } = fakeApi();
        const session = createWindowMaximizeRestoreSession(api, (fn, ms) => { scheduled.push({ fn, ms }); }, () => now);
        const op = session.beginMaximize();
        await session.rememberNormal();
        expect(session.restoreNormal()).toBe(true);
        expect(session.isCurrent(op)).toBe(false);
        expect(session.shouldSkipClamp()).toBe(true);
        now += WINDOW_RESTORE_CLAMP_SUPPRESS_MS;
        expect(session.shouldSkipClamp()).toBe(false);
        expect(calls).toEqual(["size:1100x720", "pos:12,24"]);
        expect(scheduled.map((item) => item.ms)).toEqual([WINDOW_RESTORE_REASSERT_MS, WINDOW_RESTORE_REASSERT_LATE_MS]);
        scheduled[0].fn();
        scheduled[1].fn();
        expect(calls).toEqual([
            "size:1100x720", "pos:12,24",
            "size:1100x720", "pos:12,24",
            "size:1100x720", "pos:12,24",
        ]);
    });

    it("does not let a superseded restore reassert after a new maximize", async () => {
        const scheduled: Array<() => void> = [];
        const { calls, api } = fakeApi();
        const session = createWindowMaximizeRestoreSession(api, (fn) => { scheduled.push(fn); });
        await session.rememberNormal();
        session.restoreNormal();
        session.beginMaximize();
        scheduled[0]();
        expect(calls).toEqual(["size:1100x720", "pos:12,24"]);
    });

    it("abandons an in-flight maximize after restore", async () => {
        let now = 1_000;
        const { api } = fakeApi();
        const session = createWindowMaximizeRestoreSession(api, () => undefined, () => now);
        const op = session.beginMaximize();
        session.restoreNormal();
        expect(session.isCurrent(op)).toBe(false);
        now += 10;
        expect(session.shouldSkipClamp()).toBe(true);
    });

    it("ignores a snapshot captured after restore cancelled the maximize", async () => {
        let resolveSize: (size: { w: number; h: number }) => void = () => undefined;
        const { calls, api } = fakeApi();
        api.getSize = () => new Promise((resolve) => { resolveSize = resolve; });
        const session = createWindowMaximizeRestoreSession(api, () => undefined);
        const op = session.beginMaximize();
        const remember = session.rememberNormal(op);
        session.restoreNormal();
        resolveSize({ w: 1920, h: 1080 });
        await remember;
        expect(session.restoreNormal()).toBe(false);
        expect(calls).toEqual([]);
    });
});
