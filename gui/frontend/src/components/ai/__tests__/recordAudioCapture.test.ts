import { describe, expect, it } from "vitest";
import { startRecordCapture } from "../recordAudioCapture";

describe("recordAudioCapture", () => {
    it("exports startRecordCapture function", () => {
        expect(typeof startRecordCapture).toBe("function");
    });

    it("startRecordCapture rejects when getUserMedia is unavailable", async () => {
        const original = navigator.mediaDevices;
        Object.defineProperty(navigator, "mediaDevices", {
            configurable: true,
            value: undefined,
        });
        try {
            await expect(
                startRecordCapture({ onPCM: () => {} }),
            ).rejects.toBeTruthy();
        } finally {
            Object.defineProperty(navigator, "mediaDevices", {
                configurable: true,
                value: original,
            });
        }
    });
});
