import { spawnSync } from "node:child_process";

// git diff --name-only keeps only the destination of a rename or copy.
// --name-status -z keeps both paths and does not quote or split on spaces.

const STATUS = /^[ACDMRTUXB][0-9]*$/;

function decodePath(bytes) {
  if (!bytes || bytes.length === 0) {
    throw new Error("diff record has an empty path");
  }
  const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  if (text.includes("\u0000")) throw new Error("diff path contains NUL");
  return text;
}

export function parseNameStatus(buffer) {
  const raw = Buffer.isBuffer(buffer) ? buffer : Buffer.from(buffer);
  const fields = [];
  let start = 0;
  for (let i = 0; i <= raw.length; i++) {
    if (i === raw.length || raw[i] === 0) {
      if (i > start) fields.push(raw.subarray(start, i));
      start = i + 1;
    }
  }
  const paths = [];
  for (let i = 0; i < fields.length;) {
    let status;
    try {
      status = new TextDecoder("utf-8", { fatal: true }).decode(fields[i]);
    } catch {
      throw new Error("diff status is not UTF-8");
    }
    if (!STATUS.test(status)) {
      throw new Error(`unrecognized diff status ${JSON.stringify(status)}`);
    }
    const kind = status[0];
    if (kind === "R" || kind === "C") {
      if (i + 2 >= fields.length) throw new Error(`truncated ${status} record`);
      paths.push(decodePath(fields[i + 1]), decodePath(fields[i + 2]));
      i += 3;
      continue;
    }
    if (i + 1 >= fields.length) throw new Error(`truncated ${status} record`);
    paths.push(decodePath(fields[i + 1]));
    i += 2;
  }
  return paths;
}

function git(cwd, args) {
  return spawnSync("git", ["-C", cwd, ...args], { encoding: "buffer" });
}

export function collectChangedPaths(base, head, cwd = process.cwd()) {
  if (!base || !head) throw new Error("missing base or head commit");
  for (const rev of [base, head]) {
    const seen = git(cwd, ["cat-file", "-e", `${rev}^{commit}`]);
    if (seen.status !== 0) {
      throw new Error(`cannot see commit ${rev}; refusing to treat the diff as empty`);
    }
  }
  const diff = git(cwd, ["diff", "-M", "-C", "--name-status", "-z", base, head]);
  if (diff.status !== 0) {
    const detail = diff.stderr ? diff.stderr.toString("utf8") : "git diff failed";
    throw new Error(detail.trim() || "git diff failed");
  }
  return parseNameStatus(diff.stdout || Buffer.alloc(0));
}
