// The reader's saves are fire-and-forget Wails calls. Nothing in the bridge
// promises that two calls run in the order they were made, so a quick
// mark-then-clear or two fast submissions could reach SQLite reversed. This
// queue starts each write only after the previous one settled. A failed write
// is swallowed so it cannot block the writes behind it; each caller reports its
// own failure through the work it enqueues.

let chain: Promise<unknown> = Promise.resolve();

export function enqueueCompanionReaderWrite(write: () => Promise<unknown>): void {
    chain = chain.then(write).catch(() => undefined);
}
