/**
 * Unit tests for ls parsing / relative path helpers (no vscode).
 * Run: node test/remoteFs.test.mjs
 */
import assert from "assert";
import { createRequire } from "module";
import { fileURLToPath } from "url";
import path from "path";

// remoteFs.ts is compiled into extension bundle; re-implement pure helpers here
// to avoid vscode import — keep in sync with src/remoteFs.ts parseLsListing.

function parseLsListing(listing, parentDir) {
  const parent = parentDir.replace(/\/+$/, "") || "/";
  const out = [];
  for (const line of listing.split(/\r?\n/)) {
    const trimmed = line.trimEnd();
    if (!trimmed || /^total\s+\d+/i.test(trimmed)) continue;
    const m = trimmed.match(/^([dlcbps\-])[rwxstST\-+]{9}/);
    if (!m) continue;
    const kindChar = m[1];
    const parts = trimmed.split(/\s+/);
    if (parts.length < 9) continue;
    let name = parts.slice(8).join(" ");
    const arrow = name.indexOf(" -> ");
    if (arrow >= 0) name = name.slice(0, arrow);
    name = name.trim();
    if (!name || name === "." || name === "..") continue;
    let kind = "other";
    if (kindChar === "d") kind = "dir";
    else if (kindChar === "-") kind = "file";
    else if (kindChar === "l") kind = "link";
    const abs = name.startsWith("/")
      ? name
      : parent === "/"
        ? `/${name}`
        : `${parent}/${name}`;
    out.push({ kind, name, path: abs });
  }
  return out;
}

function remotePathRelativeToWorkDir(remotePath, workDir) {
  const p = remotePath.replace(/\\/g, "/").replace(/^remote:/, "");
  const w = workDir.replace(/\\/g, "/").replace(/\/+$/, "");
  if (w && (p === w || p.startsWith(w + "/"))) {
    return p.slice(w.length).replace(/^\//, "") || p.split("/").pop();
  }
  return p.split("/").pop();
}

const sample = `
total 20
drwxr-xr-x  3 root root 4096 Jul 20 01:00 .
drwxr-xr-x 10 root root 4096 Jul 19 13:22 ..
-rw-r--r--  1 root root  120 Jul 20 01:01 main.cpp
drwxr-xr-x  2 root root 4096 Jul 20 01:02 src
lrwxrwxrwx  1 root root    8 Jul 20 01:03 link -> main.cpp
`;

const entries = parseLsListing(sample, "/home/proj");
assert.strictEqual(entries.length, 3, "skip . and ..");
assert.strictEqual(entries[0].name, "main.cpp");
assert.strictEqual(entries[0].kind, "file");
assert.strictEqual(entries[0].path, "/home/proj/main.cpp");
assert.strictEqual(entries[1].name, "src");
assert.strictEqual(entries[1].kind, "dir");
assert.strictEqual(entries[2].kind, "link");
assert.strictEqual(entries[2].name, "link");

assert.strictEqual(
  remotePathRelativeToWorkDir("/home/proj/src/a.go", "/home/proj"),
  "src/a.go"
);
assert.strictEqual(
  remotePathRelativeToWorkDir("/other/x.go", "/home/proj"),
  "x.go"
);

function countRemotePreviewHeaderLines(doc) {
  let i = 0;
  while (i < doc.lineCount) {
    const t = doc.lineAt(i).text;
    if (t.startsWith("//")) {
      i++;
      continue;
    }
    if (t.trim() === "") {
      i++;
      break;
    }
    break;
  }
  return i;
}

function remoteSourceLineToDocLine(doc, remoteLine) {
  const header = countRemotePreviewHeaderLines(doc);
  return Math.min(
    Math.max(0, header + remoteLine - 1),
    Math.max(0, doc.lineCount - 1)
  );
}

const fakeDoc = {
  lines: [
    "// remote: /home/proj/main.cpp",
    "// work_dir: /home/proj",
    "",
    "int main() {",
    "  return 0;",
    "}",
  ],
  get lineCount() {
    return this.lines.length;
  },
  lineAt(i) {
    return { text: this.lines[i] ?? "" };
  },
};
assert.strictEqual(countRemotePreviewHeaderLines(fakeDoc), 3);
assert.strictEqual(remoteSourceLineToDocLine(fakeDoc, 1), 3);
assert.strictEqual(remoteSourceLineToDocLine(fakeDoc, 2), 4);

console.log("[remoteFs.test] OK");
