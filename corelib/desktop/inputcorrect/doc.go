// Package inputcorrect decides a bounded correction for text typed into an
// isolated bot desktop, and stores the strategy only after an exact read-back.
//
// The desktop is unattended: nobody else is using its keyboard or pointer.
// A failure is a wrong field, a stale frame or ref, a layout change, partial
// or duplicated text, or an editor or upload that has not settled. This
// package does not wait out a foreground lock and it does not run a shell
// workaround.
//
// Design compared with nightly-labs/openbot at f694dea (PolyForm
// Noncommercial; the installed build was not identified). That tree's
// browser host binds a ref to the snapshot revision and frame
// (src/backend/browser-host.ts act, src/backend/browser-cdp-snapshot.ts),
// focuses the node, and inserts text (src/backend/browser-cdp-input.ts).
// Site memories there are notes a snapshot can show, not a procedure the
// act path loads. MaClaw already has a CDP session, snapshot refs, FastBatch,
// and Input.insertText. This package does not add another browser. It
// compares the field the existing relay reads, retries with a different
// general strategy when the text is wrong, and lets a later call select a
// strategy only when its conditions still match.
package inputcorrect
