/**
 * Read/write the MaClaw GUI's LLM provider selection directly in config.json.
 *
 * Why this works: the running GUI watches config.json (fsnotify) and resolves
 * the active provider per prompt turn, so an external, atomic, single-field
 * write takes effect immediately — no GUI restart, no extra API.
 *
 * List reads `maclaw_llm_providers[]` + `maclaw_llm_current_provider`.
 * Switch writes ONLY `maclaw_llm_current_provider`, byte-preserving the rest
 * of the file when the key occurs exactly once; anything ambiguous (absent
 * key, duplicate key, the key text inside some other value) falls back to a
 * full JSON rewrite. Writes are atomic (tmp + fsync + rename).
 */
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

export interface MaclawProviderInfo {
  name: string;
  model: string;
  url: string;
}

export interface MaclawLLMConfigState {
  providers: MaclawProviderInfo[];
  current: string;
}

const BOM_CHAR = 0xfeff;

/**
 * config.json path. The GUI ALWAYS reads ~/.maclaw/config.json (its
 * getConfigPath ignores MACLAW_DATA_DIR), so we must resolve the same file —
 * otherwise the switch lands in a file the GUI never reads and silently does
 * nothing. MACLAW_VSEXT_CONFIG exists only for tests.
 */
export function maclawConfigPath(): string {
  const override = (process.env.MACLAW_VSEXT_CONFIG ?? "").trim();
  if (override !== "") {
    return override;
  }
  return path.join(os.homedir(), ".maclaw", "config.json");
}

export function readMaclawLLMConfig(): MaclawLLMConfigState | undefined {
  let raw: string;
  try {
    raw = fs.readFileSync(maclawConfigPath(), "utf8");
  } catch {
    return undefined;
  }
  let cfg: Record<string, unknown>;
  try {
    cfg = JSON.parse(stripBom(raw)) as Record<string, unknown>;
  } catch {
    return undefined;
  }
  const list = Array.isArray(cfg.maclaw_llm_providers) ? cfg.maclaw_llm_providers : [];
  const providers: MaclawProviderInfo[] = [];
  const seen = new Set<string>();
  for (const p of list) {
    if (p && typeof p === "object") {
      const rec = p as Record<string, unknown>;
      const name = typeof rec.name === "string" ? rec.name.trim() : "";
      if (name === "" || seen.has(name)) {
        continue; // name-keyed scheme: a later duplicate can never be addressed
      }
      seen.add(name);
      providers.push({
        name,
        model: typeof rec.model === "string" ? rec.model : "",
        url: typeof rec.url === "string" ? rec.url : "",
      });
    }
  }
  const current =
    typeof cfg.maclaw_llm_current_provider === "string" ? cfg.maclaw_llm_current_provider : "";
  return { providers, current };
}

/**
 * Switch the active provider by name. Re-reads and retries (bounded) if the
 * file changes under us, so a concurrent GUI write is not silently lost.
 */
export function writeCurrentProvider(name: string): void {
  if (typeof name !== "string" || name.trim() === "") {
    throw new Error("provider name is empty");
  }
  const file = maclawConfigPath();
  // JSON-string body with ALL escapes handled (quotes, backslashes, controls).
  const encoded = JSON.stringify(name).slice(1, -1);
  const keyPattern = /("maclaw_llm_current_provider"\s*:\s*")((?:[^"\\]|\\.)*)(")/g;

  for (let attempt = 0; attempt < 3; attempt++) {
    const raw = fs.readFileSync(file, "utf8");
    let out: string;
    const occurrences = raw.match(keyPattern);
    if (occurrences && occurrences.length === 1) {
      // Exactly one key occurrence: byte-preserving single-value swap
      // (function form keeps "$" in names literal).
      keyPattern.lastIndex = 0;
      out = raw.replace(keyPattern, (_m, p1: string, _v: string, p3: string) => p1 + encoded + p3);
    } else {
      // Absent or ambiguous (duplicate key, key text inside another value):
      // full JSON rewrite. Matches the GUI's own 2-space indent + trailing \n.
      const cfg = JSON.parse(stripBom(raw)) as Record<string, unknown>;
      cfg.maclaw_llm_current_provider = name;
      out = (raw.charCodeAt(0) === BOM_CHAR ? String.fromCharCode(BOM_CHAR) : "") +
        JSON.stringify(cfg, null, 2) + "\n";
    }

    // Narrow the read-modify-write race: bail out and retry if the file
    // changed between our read and the rename.
    let before: string;
    try {
      before = fs.readFileSync(file, "utf8");
    } catch {
      before = raw; // unreadable — proceed and let the write surface any error
    }
    if (before !== raw && attempt < 2) {
      continue;
    }
    atomicWrite(file, out);
    return;
  }
}

/**
 * Watch config.json (debounced) for external changes, e.g. GUI-side switches.
 * Watches the DIRECTORY, not the file: the GUI writes atomically (tmp+rename),
 * and a file-level fs.watch on Windows silently dies after the first replace.
 */
export function watchMaclawConfig(onChange: () => void): { close(): void } | undefined {
  const file = maclawConfigPath();
  const dir = path.dirname(file);
  const base = path.basename(file).toLowerCase();
  let timer: NodeJS.Timeout | undefined;
  try {
    const watcher = fs.watch(dir, { persistent: false }, (_event, filename) => {
      if (filename && filename.toLowerCase() !== base) {
        return;
      }
      if (timer) {
        clearTimeout(timer);
      }
      timer = setTimeout(onChange, 300);
    });
    watcher.on("error", () => {
      // The watch stays down until the view is re-created — accepted trade-off
      // (the ready handler always re-reads, so sync resumes on next open).
    });
    return {
      close() {
        if (timer) {
          clearTimeout(timer);
        }
        watcher.close();
      },
    };
  } catch {
    return undefined;
  }
}

function stripBom(s: string): string {
  return s.charCodeAt(0) === BOM_CHAR ? s.slice(1) : s;
}

function atomicWrite(file: string, content: string): void {
  // Unique tmp name: no collisions between concurrent writers, no stale
  // leftovers carrying config content (incl. API keys) after a failure.
  const tmp = `${file}.${process.pid}.${Date.now()}.tmp`;
  const fd = fs.openSync(tmp, "w");
  try {
    fs.writeFileSync(fd, content, "utf8");
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  try {
    fs.renameSync(tmp, file);
  } catch (err) {
    try {
      fs.unlinkSync(tmp);
    } catch {
      /* best effort */
    }
    throw err;
  }
}
