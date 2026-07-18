/**
 * Resolve the maclaw-acp-bridge executable.
 *
 * Order: maclaw-acp.bridgePath setting → MACLAW_ACP_BRIDGE env →
 * <MACLAW_DATA_DIR or ~/.maclaw>/bin → PATH.
 */
import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import * as vscode from "vscode";

export function bridgeBinaryName(): string {
  return process.platform === "win32" ? "maclaw-acp-bridge.exe" : "maclaw-acp-bridge";
}

export function maclawDataDir(): string {
  const fromEnv = (process.env.MACLAW_DATA_DIR ?? "").trim();
  if (fromEnv !== "") {
    return fromEnv;
  }
  return path.join(os.homedir(), ".maclaw");
}

export function resolveBridgePath(): string | undefined {
  const candidates: string[] = [];

  const configured = vscode.workspace
    .getConfiguration("maclaw-acp")
    .get<string>("bridgePath", "")
    .trim();
  if (configured !== "") {
    candidates.push(configured);
  }
  const envPath = (process.env.MACLAW_ACP_BRIDGE ?? "").trim();
  if (envPath !== "") {
    candidates.push(envPath);
  }

  const exe = bridgeBinaryName();
  const dataDir = maclawDataDir();
  candidates.push(path.join(dataDir, "bin", exe));
  candidates.push(path.join(dataDir, exe));

  for (const dir of (process.env.PATH ?? "").split(path.delimiter)) {
    if (dir.trim() !== "") {
      candidates.push(path.join(dir, exe));
    }
  }

  for (const candidate of candidates) {
    try {
      if (candidate === "" || !fs.statSync(candidate).isFile()) {
        continue;
      }
      // On POSIX a file without +x would only fail later at spawn time.
      if (process.platform !== "win32") {
        fs.accessSync(candidate, fs.constants.X_OK);
      }
      return candidate;
    } catch {
      /* keep looking */
    }
  }
  return undefined;
}
