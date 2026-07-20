/**
 * Unit tests for agent change path collection logic (mirrors agentChangeTree).
 * Run: node test/agentChange.test.mjs
 */
import assert from "assert";

function normalizePath(p) {
  let s = p.trim().replace(/^remote:/, "").replace(/\\/g, "/");
  s = s.replace(/^[`'"]+|[`'"]+$/g, "");
  return s;
}

function collectPaths(update) {
  const out = [];
  const seen = new Set();
  const add = (p) => {
    if (typeof p !== "string") return;
    const n = normalizePath(p);
    if (!n || seen.has(n)) return;
    seen.add(n);
    out.push(n);
  };
  if (Array.isArray(update.locations)) {
    for (const loc of update.locations) {
      if (loc && typeof loc === "object") add(loc.path);
    }
  }
  const raw = update.rawInput;
  if (raw && typeof raw === "object") {
    for (const k of ["path", "file", "file_path", "filepath", "target", "to", "from"]) {
      add(raw[k]);
    }
  }
  return out;
}

function extractFileChangeCards(text) {
  const paths = [];
  const re = /###\s*File change:\s*`?([^\n`]+)`?/gi;
  let m;
  while ((m = re.exec(text)) !== null) {
    paths.push(normalizePath(m[1]));
  }
  return paths;
}

const paths = collectPaths({
  sessionUpdate: "tool_call",
  kind: "edit",
  locations: [{ path: "/home/proj/main.cpp" }],
  rawInput: { path: "/home/proj/main.cpp", content: "x" },
});
assert.deepStrictEqual(paths, ["/home/proj/main.cpp"]);

const cards = extractFileChangeCards(
  "### File change: `src/a.go`\n\n```diff\n+hi\n```\n### File change: /tmp/b.txt\n"
);
assert.deepStrictEqual(cards, ["src/a.go", "/tmp/b.txt"]);

function canonicalizePath(raw, workDir) {
  let n = (raw ?? "").trim().replace(/^remote:/, "").replace(/\\/g, "/");
  n = n.replace(/^[`'"]+|[`'"]+$/g, "");
  if (!n) return "";
  if (n.startsWith("/") || n.startsWith("~/")) return n;
  const wd = (workDir || "").replace(/\/+$/, "");
  if (!wd) return n;
  if (n.startsWith("./")) return `${wd}/${n.slice(2)}`;
  return `${wd}/${n.replace(/^\//, "")}`;
}
assert.strictEqual(canonicalizePath("src/a.go", "/home/proj"), "/home/proj/src/a.go");
assert.strictEqual(canonicalizePath("/abs/x", "/home/proj"), "/abs/x");
assert.strictEqual(canonicalizePath("`main.cpp`", "/home/proj"), "/home/proj/main.cpp");
// relative + absolute should map to same key
assert.strictEqual(
  canonicalizePath("main.cpp", "/home/sysinfo3"),
  canonicalizePath("/home/sysinfo3/main.cpp", "/home/sysinfo3")
);

function filterEntries(entries, hideDot, nameFilter) {
  const filter = (nameFilter || "").toLowerCase();
  return entries.filter((e) => {
    if (hideDot && e.name.startsWith(".")) return false;
    if (filter && !e.name.toLowerCase().includes(filter)) return false;
    return true;
  });
}
const sample = [
  { name: ".git", kind: "dir" },
  { name: "main.cpp", kind: "file" },
  { name: ".env", kind: "file" },
  { name: "src", kind: "dir" },
];
assert.deepStrictEqual(
  filterEntries(sample, true, "").map((e) => e.name),
  ["main.cpp", "src"]
);
assert.deepStrictEqual(
  filterEntries(sample, false, "main").map((e) => e.name),
  ["main.cpp"]
);

console.log("[agentChange.test] OK");
