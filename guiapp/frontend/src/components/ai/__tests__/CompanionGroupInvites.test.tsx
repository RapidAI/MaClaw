// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useCompanionGroupInvites } from "../CompanionGroupInvites";

const list = vi.fn();
const accept = vi.fn();
const decline = vi.fn();

const invite = {
    id: "cg_0123456789abcdef0123456789abcdef",
    name: "论文组",
    owner: "owner@example.com",
};

function Harness({ active, lang = "zh" }: { active: boolean; lang?: string }) {
    const { notices, decide, error, listError } = useCompanionGroupInvites(active, lang);
    return (
        <div>
            {notices.map((notice) => (
                <article key={notice.id} data-testid="companion-group-notice" data-created={notice.created_at}>
                    <h2>{notice.title}</h2>
                    <p>{notice.content}</p>
                    <button type="button" data-testid="companion-group-invite-accept" onClick={() => decide(notice.groupInviteId || "", "accept")}>接受</button>
                    <button type="button" data-testid="companion-group-invite-decline" onClick={() => decide(notice.groupInviteId || "", "decline")}>拒绝</button>
                    <button type="button" data-testid="companion-group-invite-ignore" onClick={() => decide(notice.groupInviteId || "", "ignore")}>忽略</button>
                </article>
            ))}
            {error ? <span data-testid="companion-group-invite-error">{error.message}</span> : null}
            {listError ? <span data-testid="companion-group-invite-list-error">{listError}</span> : null}
        </div>
    );
}

beforeEach(() => {
    localStorage.clear();
    list.mockReset();
    accept.mockReset();
    decline.mockReset();
    list.mockResolvedValue([invite]);
    accept.mockResolvedValue(undefined);
    decline.mockResolvedValue(undefined);
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                ListFileCompanionGroupInvites: () => list(),
                AcceptFileCompanionGroupInvite: (id: string) => accept(id),
                DeclineFileCompanionGroupInvite: (id: string) => decline(id),
            },
        },
    };
});

afterEach(() => {
    delete (window as unknown as { go?: unknown }).go;
    localStorage.clear();
});

describe("useCompanionGroupInvites", () => {
    it("turns a pending invite into a notification and joins on 接受", async () => {
        render(<Harness active />);
        const row = await screen.findByTestId("companion-group-notice");
        expect(row.textContent).toContain("入群邀请");
        expect(row.textContent).toContain("owner@example.com");
        expect(row.textContent).toContain("论文组");
        fireEvent.click(screen.getByTestId("companion-group-invite-accept"));
        await waitFor(() => expect(accept).toHaveBeenCalledWith(invite.id));
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
        expect(decline).not.toHaveBeenCalled();
    });

    it("drops the invite after 拒绝", async () => {
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-decline"));
        await waitFor(() => expect(decline).toHaveBeenCalledWith(invite.id));
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
    });

    it("keeps the invite and says so when this build cannot ask the Hub to ignore", async () => {
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-ignore"));
        expect((await screen.findByTestId("companion-group-invite-error")).textContent).toBe("暂时无法忽略邀请");
        expect(screen.getByTestId("companion-group-notice")).toBeTruthy();
        expect(accept).not.toHaveBeenCalled();
        expect(decline).not.toHaveBeenCalled();
    });

    it("stays quiet until the window is active, then loads on focus", async () => {
        list.mockResolvedValue([invite]);
        const view = render(<Harness active={false} />);
        expect(screen.queryByTestId("companion-group-notice")).toBeNull();
        expect(list).not.toHaveBeenCalled();
        view.rerender(<Harness active />);
        expect((await screen.findByTestId("companion-group-notice")).textContent).toContain("论文组");
        expect(list).toHaveBeenCalledTimes(1);
        list.mockResolvedValue([invite, { ...invite, id: "cg_abcdef0123456789abcdef0123456789", name: "另一组" }]);
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(screen.getAllByTestId("companion-group-notice")).toHaveLength(2));
        expect(screen.getAllByTestId("companion-group-notice")[1].textContent).toContain("另一组");
    });

    it("does not bring an invite back when a slower refresh finishes after 接受", async () => {
        const pending: Array<(value: unknown) => void> = [];
        list.mockImplementation(() => new Promise((resolve) => { pending.push(resolve); }));
        render(<Harness active />);
        await waitFor(() => expect(pending.length).toBe(1));
        await act(async () => { pending[0]([invite]); });
        await screen.findByTestId("companion-group-notice");
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(pending.length).toBe(2));
        fireEvent.click(screen.getByTestId("companion-group-invite-accept"));
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
        await act(async () => { pending[1]([invite]); });
        expect(screen.queryByTestId("companion-group-notice")).toBeNull();
    });

    it("sends 接受 once when the button is clicked twice", async () => {
        let resolveAccept: (() => void) | undefined;
        accept.mockImplementation(() => new Promise<void>((resolve) => { resolveAccept = resolve; }));
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-accept"));
        fireEvent.click(screen.getByTestId("companion-group-invite-accept"));
        expect(accept).toHaveBeenCalledTimes(1);
        await act(async () => { resolveAccept?.(); });
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
    });

    it("keeps one list for the title-bar bell and the task-header bell", async () => {
        render(
            <>
                <Harness active />
                <Harness active={false} />
            </>,
        );
        expect(await screen.findAllByTestId("companion-group-notice")).toHaveLength(2);
        expect(list).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getAllByTestId("companion-group-invite-accept")[0]);
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
        expect(accept).toHaveBeenCalledTimes(1);
    });

    it("uses English copy when the window language is English", async () => {
        render(<Harness active lang="en" />);
        const row = await screen.findByTestId("companion-group-notice");
        expect(row.textContent).toContain("Group invitation");
        expect(row.textContent).toContain("invited you to the reading group");
        expect(row.textContent).toContain("论文组");
    });

    it("keeps a shown invite when a later refresh fails", async () => {
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        list.mockRejectedValue("暂时无法连接云端");
        window.dispatchEvent(new Event("focus"));
        expect((await screen.findByTestId("companion-group-invite-list-error")).textContent).toBe("暂时无法连接云端");
        expect(screen.getByTestId("companion-group-notice")).toBeTruthy();
    });

    it("says the invite list could not be read when the first load fails", async () => {
        list.mockRejectedValue("暂时无法连接云端");
        render(<Harness active />);
        expect((await screen.findByTestId("companion-group-invite-list-error")).textContent).toBe("暂时无法连接云端");
        expect(screen.queryByTestId("companion-group-notice")).toBeNull();
        list.mockResolvedValue([invite]);
        window.dispatchEvent(new Event("focus"));
        expect((await screen.findByTestId("companion-group-notice")).textContent).toContain("论文组");
        expect(screen.queryByTestId("companion-group-invite-list-error")).toBeNull();
    });

    it("uses a fallback when the invite list fails without a message", async () => {
        list.mockRejectedValue("");
        render(<Harness active />);
        expect((await screen.findByTestId("companion-group-invite-list-error")).textContent).toBe("入群邀请暂时无法读取");
    });

    it("treats an empty invite list as no invitations", async () => {
        list.mockResolvedValue([]);
        render(<Harness active />);
        await waitFor(() => expect(list).toHaveBeenCalled());
        await act(async () => { await list.mock.results[0]?.value; });
        expect(screen.queryByTestId("companion-group-invite-list-error")).toBeNull();
        expect(screen.queryByTestId("companion-group-notice")).toBeNull();
    });

    it("keeps a newer invite list when an older refresh fails", async () => {
        const pending: Array<{ resolve: (value: unknown) => void; reject: (reason: unknown) => void }> = [];
        list.mockImplementation(() => new Promise((resolve, reject) => { pending.push({ resolve, reject }); }));
        render(<Harness active />);
        await waitFor(() => expect(pending.length).toBe(1));
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(pending.length).toBe(2));
        await act(async () => { pending[1].resolve([invite]); });
        expect((await screen.findByTestId("companion-group-notice")).textContent).toContain("论文组");
        await act(async () => { pending[0].reject("暂时无法连接云端"); });
        expect(screen.getByTestId("companion-group-notice")).toBeTruthy();
        expect(screen.queryByTestId("companion-group-invite-list-error")).toBeNull();
    });

    it("does not restore invites from a refresh that a newer failure already replaced", async () => {
        const pending: Array<{ resolve: (value: unknown) => void; reject: (reason: unknown) => void }> = [];
        list.mockImplementation(() => new Promise((resolve, reject) => { pending.push({ resolve, reject }); }));
        render(<Harness active />);
        await waitFor(() => expect(pending.length).toBe(1));
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(pending.length).toBe(2));
        await act(async () => { pending[1].reject("暂时无法连接云端"); });
        expect((await screen.findByTestId("companion-group-invite-list-error")).textContent).toBe("暂时无法连接云端");
        await act(async () => { pending[0].resolve([invite]); });
        expect(screen.queryByTestId("companion-group-notice")).toBeNull();
        expect(screen.getByTestId("companion-group-invite-list-error").textContent).toBe("暂时无法连接云端");
    });

    it("keeps newest-first order when a later poll reverses the cloud list", async () => {
        const older = { ...invite, id: "cg_abcdef0123456789abcdef0123456789", name: "旧组", invited_at: "2026-10-01T00:00:00Z" };
        const newer = { ...invite, name: "新组", invited_at: "2026-10-03T00:00:00Z" };
        list.mockResolvedValue([older, newer]);
        render(<Harness active />);
        let rows = await screen.findAllByTestId("companion-group-notice");
        expect(rows[0].textContent).toContain("新组");
        expect(rows[0].getAttribute("data-created")).toBe("2026-10-03T00:00:00.000Z");
        expect(rows[1].textContent).toContain("旧组");
        expect(rows[1].getAttribute("data-created")).toBe("2026-10-01T00:00:00.000Z");
        list.mockResolvedValue([newer, older]);
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(list.mock.calls.length).toBeGreaterThan(1));
        rows = screen.getAllByTestId("companion-group-notice");
        expect(rows.map((row) => row.textContent)).toEqual([
            expect.stringContaining("新组"),
            expect.stringContaining("旧组"),
        ]);
    });

    it("keeps the invite when 接受 fails", async () => {
        accept.mockRejectedValue("没有找到入群邀请");
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-accept"));
        expect((await screen.findByTestId("companion-group-invite-error")).textContent).toBe("没有找到入群邀请");
        expect(screen.getByTestId("companion-group-notice")).toBeTruthy();
    });

    it("clears a failed decision once that invitation leaves the list", async () => {
        accept.mockRejectedValue("没有找到入群邀请");
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-accept"));
        await screen.findByTestId("companion-group-invite-error");
        list.mockResolvedValue([]);
        window.dispatchEvent(new Event("focus"));
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
        expect(screen.queryByTestId("companion-group-invite-error")).toBeNull();
    });
});

describe("useCompanionGroupInvites 忽略 on the Hub", () => {
    const ignore = vi.fn();

    beforeEach(() => {
        localStorage.clear();
        ignore.mockReset();
        list.mockReset();
        list.mockResolvedValue([invite]);
        ignore.mockResolvedValue(undefined);
        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    ListFileCompanionGroupInvites: () => list(),
                    IgnoreFileCompanionGroupInvite: (id: string) => ignore(id),
                },
            },
        };
    });

    afterEach(() => {
        delete (window as unknown as { go?: unknown }).go;
        localStorage.clear();
    });

    it("tells the Hub so the invite stays ignored on every device", async () => {
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-ignore"));
        await waitFor(() => expect(ignore).toHaveBeenCalledWith(invite.id));
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
        // A server-side ignore must not depend on this browser's storage.
        expect(localStorage.getItem("maclaw.companion-group-invite.ignored")).toBeNull();
    });

    it("keeps the invite when the Hub refuses the ignore", async () => {
        ignore.mockRejectedValue("暂时无法连接云端");
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-ignore"));
        expect((await screen.findByTestId("companion-group-invite-error")).textContent).toBe("暂时无法连接云端");
        expect(screen.getByTestId("companion-group-notice")).toBeTruthy();
    });

    it("sends 忽略 once when the button is clicked twice", async () => {
        let resolveIgnore: (() => void) | undefined;
        ignore.mockImplementation(() => new Promise<void>((resolve) => { resolveIgnore = resolve; }));
        render(<Harness active />);
        await screen.findByTestId("companion-group-notice");
        fireEvent.click(screen.getByTestId("companion-group-invite-ignore"));
        fireEvent.click(screen.getByTestId("companion-group-invite-ignore"));
        expect(ignore).toHaveBeenCalledTimes(1);
        await act(async () => { resolveIgnore?.(); });
        await waitFor(() => expect(screen.queryByTestId("companion-group-notice")).toBeNull());
    });

    it("shows the invitation even when an older build left a local ignore behind", async () => {
        // The old build hid the row only on this machine, and the product no
        // longer reads that record: the Hub is the only place a decision lives.
        localStorage.setItem("maclaw.companion-group-invite.ignored", JSON.stringify([`${invite.id}\t2026-10-01T00:00:00Z`]));
        list.mockResolvedValue([{ ...invite, contact: "a@example.com", invited_at: "2026-10-01T00:00:00Z" }]);
        render(<Harness active />);
        expect(await screen.findByTestId("companion-group-notice")).toBeTruthy();
        fireEvent.click(screen.getByTestId("companion-group-invite-ignore"));
        await waitFor(() => expect(ignore).toHaveBeenCalledWith(invite.id));
    });
});
