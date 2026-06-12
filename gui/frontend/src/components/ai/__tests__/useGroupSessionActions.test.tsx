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

    it("filters already-added participants case-insensitively by profile or machine id", async () => {
        const listVirtualEmployees = vi.fn().mockResolvedValue([
            { id: "profile-ve", machine_id: "Machine-VE", name: "Already added", online_status: "online" },
            { id: "profile-new", machine_id: "machine-new", name: "New Bot", online_status: "online" },
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
                participants: ["machine-ve"],
                maxParticipants: 5,
            });
        });

        expect(availability?.success).toBe(true);
        expect(availability?.available.map((ve) => ve.id)).toEqual(["profile-new"]);
    });

    it("treats malformed employee list responses as empty availability", async () => {
        const listVirtualEmployees = vi.fn().mockResolvedValue({ employees: [] });
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
                participants: ["machine-ve"],
                maxParticipants: 5,
            });
        });

        expect(availability?.success).toBe(false);
        expect(availability?.available).toEqual([]);
        expect(result.current.feedback?.message).toBe("No digital employees available to invite");
    });

    it("filters already-added participants across ve aliases", async () => {
        const listVirtualEmployees = vi.fn().mockResolvedValue([
            { id: "profile-ve", machine_id: "machine-ve", name: "Already added", online_status: "online" },
            { id: "profile-new", machine_id: "machine-new", name: "New Bot", online_status: "online" },
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
                participants: ["ve-machine-ve"],
                maxParticipants: 5,
            });
        });

        expect(availability?.success).toBe(true);
        expect(availability?.available.map((ve) => ve.id)).toEqual(["profile-new"]);
    });

    it("does not count duplicate VE aliases against group capacity", async () => {
        const listVirtualEmployees = vi.fn().mockResolvedValue([
            { id: "profile-new", machine_id: "machine-new", name: "New Bot", online_status: "online" },
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
                participants: ["machine-ve", "ve-machine-ve"],
                maxParticipants: 2,
            });
        });

        expect(availability?.success).toBe(true);
        expect(availability?.available.map((ve) => ve.id)).toEqual(["profile-new"]);
        expect(result.current.feedback?.message).not.toBe("Group is full (max 2)");
    });

    it("detects an existing local AI participant case-insensitively", async () => {
        const registerLocalExecutor = vi.fn().mockResolvedValue({ participant_id: "machine-local", display_name: "Local AI" });
        const { result } = renderHook(() => useGroupSessionActions({ lang: "en", registerLocalExecutor }));

        let added: Awaited<ReturnType<typeof result.current.addLocalAI>> | undefined;
        await act(async () => {
            added = await result.current.addLocalAI({
                sessionId: "session-1",
                veId: "ve-a",
                participants: [" LOCAL-MACLAW "],
                maxParticipants: 5,
            });
        });

        expect(added?.success).toBe(false);
        expect(registerLocalExecutor).not.toHaveBeenCalled();
        expect(result.current.feedback?.message).toBe("Local AI assistant is already in the session");
    });

    it("returns canonical local AI participant identity after registration", async () => {
        const registerLocalExecutor = vi.fn().mockResolvedValue({ participant_id: "machine-local", display_name: "Local AI" });
        const { result } = renderHook(() => useGroupSessionActions({ lang: "en", registerLocalExecutor }));

        let added: Awaited<ReturnType<typeof result.current.addLocalAI>> | undefined;
        await act(async () => {
            added = await result.current.addLocalAI({
                sessionId: "session-1",
                veId: "ve-a",
                participants: ["ve-a"],
                maxParticipants: 5,
            });
        });

        expect(added).toMatchObject({ success: true, sessionId: "session-1", participantId: "machine-local", displayName: "Local AI" });
        expect(result.current.feedback?.message).toBe("Local AI assistant added to session");
    });

    it("detects an existing local AI participant by canonical id", async () => {
        const registerLocalExecutor = vi.fn();
        const { result } = renderHook(() => useGroupSessionActions({ lang: "en", registerLocalExecutor }));

        let added: Awaited<ReturnType<typeof result.current.addLocalAI>> | undefined;
        await act(async () => {
            added = await result.current.addLocalAI({
                sessionId: "session-1",
                veId: "ve-a",
                participants: ["machine-local"],
                localParticipantIds: ["MACHINE-LOCAL"],
                maxParticipants: 5,
            });
        });

        expect(added?.success).toBe(false);
        expect(registerLocalExecutor).not.toHaveBeenCalled();
        expect(result.current.feedback?.message).toBe("Local AI assistant is already in the session");
    });
});
