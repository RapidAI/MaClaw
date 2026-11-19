import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

import { collectChangedPaths, parseNameStatus } from "./diff-names.mjs";
import { classify } from "./policy.mjs";

function git(cwd, args) {
  const result = spawnSync("git", args, { cwd, encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`${args.join(" ")}\n${result.stderr || result.stdout}`);
  }
  return result.stdout.trim();
}

function initRepo() {
  const cwd = fs.mkdtempSync(path.join(os.tmpdir(), "pr-gate-diff-"));
  git(cwd, ["init", "-b", "main"]);
  git(cwd, ["config", "user.email", "gate@example.com"]);
  git(cwd, ["config", "user.name", "gate"]);
  git(cwd, ["config", "core.autocrlf", "false"]);
  return cwd;
}

function write(cwd, rel, text) {
  const abs = path.join(cwd, rel);
  fs.mkdirSync(path.dirname(abs), { recursive: true });
  fs.writeFileSync(abs, text);
}

function commit(cwd, message) {
  git(cwd, ["add", "-A"]);
  git(cwd, ["-c", "user.email=gate@example.com", "-c", "user.name=gate", "commit", "-m", message]);
  return git(cwd, ["rev-parse", "HEAD"]);
}

function nameOnly(cwd, base, head) {
  const result = spawnSync("git", ["-C", cwd, "diff", "-M", "--name-only", "-z", base, head], { encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.split("\0").filter(Boolean);
}

test("NUL name-status keeps rename sources and spaced names", () => {
  const buffer = Buffer.from(
    "R100\0MaClawSrv/a.go\0docs/a.go\0M\0MaClawSrv/a file.go\0D\0corelib/plugin/manifest.go\0",
    "utf8",
  );
  assert.deepEqual(parseNameStatus(buffer), [
    "MaClawSrv/a.go",
    "docs/a.go",
    "MaClawSrv/a file.go",
    "corelib/plugin/manifest.go",
  ]);
  assert.throws(() => parseNameStatus(Buffer.from("R100\0only-one\0", "utf8")), /truncated R100/);
  assert.throws(() => parseNameStatus(Buffer.from("Z\0file\0", "utf8")), /unrecognized diff status/);
});

test("a real rename into docs keeps the Go source and cannot pass as docs-only", () => {
  const cwd = initRepo();
  try {
    write(cwd, "MaClawSrv/a.go", "package main\nfunc A() {}\n");
    write(cwd, "docs/readme.md", "# notes\n");
    const base = commit(cwd, "base");
    git(cwd, ["mv", "MaClawSrv/a.go", "docs/a.go"]);
    const head = commit(cwd, "rename");
    const dropped = nameOnly(cwd, base, head);
    assert.deepEqual(dropped, ["docs/a.go"]);
    const files = collectChangedPaths(base, head, cwd);
    assert.ok(files.includes("MaClawSrv/a.go"), files.join("\n"));
    assert.ok(files.includes("docs/a.go"), files.join("\n"));
    const plan = classify(files);
    assert.equal(plan.jobs.lifecycle, true);
    assert.equal(plan.jobs.hub, true);
    assert.notEqual(plan.result, "pass-docs-only");
    assert.equal(plan.result, "rejected");
    assert.match(plan.rejected.join("\n"), /ambiguous executable path docs\/a.go/);
  } finally {
    fs.rmSync(cwd, { recursive: true, force: true });
  }
});

test("rename from docs into a contract package, extension change, delete, copy, and spaced names", () => {
  const cwd = initRepo();
  try {
    write(cwd, "docs/note.md", "package main\nfunc Moved() {}\n");
    write(cwd, "MaClawSrv/keep.go", "package main\n");
    write(cwd, "MaClawSrv/ext.go", "package main\nfunc Ext() {}\n");
    write(cwd, "MaClawSrv/gone.go", "package main\nfunc Gone() {}\n");
    write(cwd, "MaClawSrv/origin.go", "package main\nfunc Origin() {}\n");
    write(cwd, "MaClawSrv/a file.go", "package main\nfunc Spaced() {}\n");
    write(cwd, "corelib/plugin/manifest.go", "package plugin\n");
    write(cwd, "docs/only.md", "# only\n");
    const base = commit(cwd, "base");

    git(cwd, ["mv", "docs/note.md", "MaClawSrv/moved.go"]);
    git(cwd, ["mv", "MaClawSrv/ext.go", "MaClawSrv/ext.md"]);
    fs.rmSync(path.join(cwd, "MaClawSrv/gone.go"));
    fs.rmSync(path.join(cwd, "corelib/plugin/manifest.go"));
    write(cwd, "MaClawSrv/origin.go", "package main\nfunc Origin() { /* edited */ }\n");
    write(cwd, "MaClawSrv/copied.go", "package main\nfunc Origin() {}\n");
    write(cwd, "MaClawSrv/a file.go", "package main\nfunc Spaced() { /* edited */ }\n");
    write(cwd, "docs/only.md", "# only\n\nmore\n");
    const head = commit(cwd, "edit");
    const files = collectChangedPaths(base, head, cwd);
    for (const want of [
      "docs/note.md",
      "MaClawSrv/moved.go",
      "MaClawSrv/ext.go",
      "MaClawSrv/ext.md",
      "MaClawSrv/gone.go",
      "corelib/plugin/manifest.go",
      "MaClawSrv/origin.go",
      "MaClawSrv/copied.go",
      "MaClawSrv/a file.go",
      "docs/only.md",
    ]) {
      assert.ok(files.includes(want), `missing ${want} in\n${files.join("\n")}`);
    }
    const plan = classify(files);
    assert.equal(plan.result, "selected");
    assert.equal(plan.jobs.lifecycle, true);
    assert.equal(plan.jobs.hub, true);
    assert.equal(plan.rejected.length, 0);
    assert.ok(plan.docs.includes("docs/only.md"));
    assert.ok(plan.docs.includes("docs/note.md"));
    assert.ok(plan.docs.includes("MaClawSrv/ext.md"));
  } finally {
    fs.rmSync(cwd, { recursive: true, force: true });
  }
});

test("a markdown-only edit stays docs-only", () => {
  const cwd = initRepo();
  try {
    write(cwd, "docs/only.md", "# only\n");
    write(cwd, "README.md", "# readme\n");
    const base = commit(cwd, "base");
    write(cwd, "docs/only.md", "# only\n\nchanged\n");
    write(cwd, "README.md", "# readme\n\nchanged\n");
    const head = commit(cwd, "docs");
    const files = collectChangedPaths(base, head, cwd);
    assert.deepEqual(files.sort(), ["README.md", "docs/only.md"]);
    const plan = classify(files);
    assert.equal(plan.result, "pass-docs-only");
    assert.equal(plan.jobs.lifecycle, false);
    assert.equal(plan.jobs.hub, false);
  } finally {
    fs.rmSync(cwd, { recursive: true, force: true });
  }
});

test("select does not use name-only diff output", () => {
  const source = fs.readFileSync(new URL("./select.mjs", import.meta.url), "utf8");
  assert.match(source, /collectChangedPaths/);
  assert.doesNotMatch(source, /--name-only/);
});
