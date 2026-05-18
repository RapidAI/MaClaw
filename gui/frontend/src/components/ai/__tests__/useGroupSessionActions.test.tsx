import { renderHook, act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useGroupSessionActions } from "../useGroupSessionActions";

vi.mock("../../../../wailsjs/go/main/App", () => ({}));

describe("useGroupSessionActions", () => {
    it("uses readable names for available VEs instead of raw ids", async () => {
        const listVirtualEmployees = vi.fn().mockResolvedValue([
            { id: "m_b1821505498d817c", machine_id: "m_b1821505498d817c", name: "m_b1821505498d817c", online_status: "online" },
            { id: "profile-raw", machine_id: "machine-raw", name: "m_d64c1196e8d03c53", online_status: "online" },
            { id: "profile-2", machine_id: "machine-2", name: "Contract Bot", online_status: "online" },
        ]);
        const { result } = renderHook(() => useGroupSessionActions({
            lang: "en",
            listVirtualEmployees,
            initiateConversation: vi.fn().mockResolvedValue({ session_id: "session-1" }),
        }));

        let availability: Awaited<ReturnType<typeof result.current.checkInviteAvailability>> | undefined;
        await act(async () => {
            availability = await result.current.checkInviteAvailability({
                sessionId: "session-1",
                veId: "ve-a",
                participants: ["ve-a"],
                maxParticipants: 5,
            });
        });

        expect(availability?.success).toBe(true);
        expect(availability?.available.map((ve) => ve.name)).toEqual(["Digital employee 1", "Digital employee 2", "Contract Bot"]);
        expect(availability?.available.map((ve) => ve.name).join(" ")).not.toContain("m_b1821505498d817c");
        expect(availability?.available.map((ve) => ve.name).join(" ")).not.toContain("m_d64c1196e8d03c53");
    });
});
