import fs from "node:fs";
import path from "node:path";

import { readPullRequestPaths, workflowCovers } from "./mobilebot.mjs";

// Packages whose full `go test` is not this gate. Named suites cover the
// receipt and task-outcome contracts. See docs/pr-gate.md.
export const NAMED_SUITE_PACKAGES = new Set([
  "MaClawSrv",
  "corelib/agentservice",
  "hub/internal/botmgmt",
]);

const CONTRACT_PREFIXES = [
  "MaClawSrv/",
  "corelib/agentservice/",
  "corelib/agent/",
  "corelib/agentruntime/",
  "corelib/desktopbot/",
  "corelib/browser/",
  "hub/internal/botmgmt/",
];

const MODULE_MANIFESTS = new Set([
  "corelib/agentruntime/builtin.go",
  "corelib/plugin/manifest.go",
  "corelib/skill/package_manifest.go",
  "MaClawSrv/desktop_runtime.go",
  "MaClawSrv/bot_pdf.go",
  "MaClawSrv/desktop_local_relay.go",
]);

const GOVERNED = [
  ".github/workflows/pr-check.yml",
];

const MAX_PACKAGES = 30;

function realList(dir) {
  const absolute = path.join(process.cwd(), dir);
  if (!fs.existsSync(absolute)) return [];
  return fs.readdirSync(absolute);
}

export function normalizePath(file) {
  if (typeof file !== "string" || file.trim() === "") {
    return { ok: false, error: "empty path" };
  }
  const text = file.replace(/\\/g, "/").replace(/^\.\//, "");
  if (/[\0\r\n$`{}]|^\//.test(text)) {
    return { ok: false, error: `unsafe path ${JSON.stringify(file)}` };
  }
  const parts = text.split("/");
  if (parts.some((part) => part === "" || part === "." || part === "..")) {
    return { ok: false, error: `rejected path ${JSON.stringify(file)}` };
  }
  return { ok: true, path: text };
}

const DOC_EXTENSIONS = new Set([
  ".md", ".markdown", ".mdc", ".txt", ".rst", ".adoc",
  ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".pdf",
]);

const EXECUTABLE_EXTENSIONS = new Set([
  ".go", ".c", ".cc", ".cpp", ".h", ".hh", ".rs", ".py",
  ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx",
  ".sh", ".bash", ".ps1", ".bat", ".cmd",
  ".yml", ".yaml",
]);

function extensionOf(file) {
  const base = file.slice(file.lastIndexOf("/") + 1);
  const dot = base.lastIndexOf(".");
  if (dot <= 0) return "";
  return base.slice(dot).toLowerCase();
}

function isDocLocation(file) {
  return file.startsWith("docs/") || file.startsWith(".cursor/") || file.startsWith(".agents/");
}

function isDoc(file) {
  const ext = extensionOf(file);
  if (EXECUTABLE_EXTENSIONS.has(ext)) return false;
  if (ext === ".md" || ext === ".markdown" || ext === ".mdc") return true;
  if (!file.includes("/") && /^(LICENSE|NOTICE|PATENTS)/.test(file)) return true;
  return isDocLocation(file) && DOC_EXTENSIONS.has(ext);
}

// A Go file (or other executable) moved under docs/ is not documentation.
// An unknown type in a doc directory is not documentation either.
function isAmbiguousExecutable(file) {
  return isDocLocation(file) && !isDoc(file);
}

function isFrontend(file) {
  return file.startsWith("guiapp/frontend/");
}

function isNativeGui(file) {
  if (file === "wails.json") return true;
  if (file.startsWith("cmd/maclaw-gui/")) return true;
  if (file.startsWith("guiapp/") && !file.startsWith("guiapp/frontend/")) return true;
  return false;
}

function isGovernedSelector(file) {
  if (GOVERNED.includes(file)) return true;
  return file.startsWith("scripts/pr-gate/");
}

function isSharedModule(file) {
  return file === "go.mod" || file === "go.sum";
}

function packageDir(file, list) {
  let dir = path.posix.dirname(file);
  if (path.posix.basename(dir) === "testdata") dir = path.posix.dirname(dir);
  if (!dir || dir === "." || dir === "/") return null;
  if (list(dir).some((name) => name.endsWith(".go"))) return dir;
  return null;
}

function hasTests(dir, list) {
  return list(dir).some((name) => name.endsWith("_test.go"));
}

function emptyPlan() {
  return {
    result: "rejected",
    jobs: { lifecycle: false, hub: false, frontend: false, packages: [] },
    skips: [],
    docs: [],
    rejected: [],
  };
}

export function classify(files, options = {}) {
  const list = options.list || realList;
  const mobilebot = options.mobilebot || readPullRequestPaths(
    options.mobilebotYaml || fs.readFileSync(path.join(process.cwd(), ".github/workflows/mobilebot.yml"), "utf8"),
  );
  const plan = emptyPlan();
  if (!Array.isArray(files) || files.length === 0) {
    plan.rejected.push("empty change list");
    return plan;
  }

  const packages = new Set();
  let governed = false;
  let contract = false;
  let sawOther = false;

  for (const raw of files) {
    const parsed = normalizePath(raw);
    if (!parsed.ok) {
      plan.rejected.push(parsed.error);
      continue;
    }
    const file = parsed.path;

    if (isGovernedSelector(file)) {
      governed = true;
      sawOther = true;
      continue;
    }
    if (isFrontend(file)) {
      plan.jobs.frontend = true;
      sawOther = true;
      continue;
    }
    if (isAmbiguousExecutable(file)) {
      plan.rejected.push(`ambiguous executable path ${file}`);
      sawOther = true;
      continue;
    }
    if (isDoc(file)) {
      plan.docs.push(file);
      continue;
    }
    if (isSharedModule(file) || MODULE_MANIFESTS.has(file) || CONTRACT_PREFIXES.some((prefix) => file.startsWith(prefix))) {
      contract = true;
      sawOther = true;
    }
    if (isNativeGui(file)) {
      plan.skips.push({ path: file, reason: "native-gui-not-in-scope" });
      sawOther = true;
      continue;
    }
    if (file.startsWith("mobilebot/")) {
      if (!workflowCovers(mobilebot, file)) {
        plan.rejected.push(`mobilebot.yml would not run for ${file}`);
      } else {
        plan.skips.push({ path: file, reason: "delegated-mobilebot" });
      }
      sawOther = true;
      continue;
    }
    if (file.startsWith("third_party/")) {
      plan.skips.push({ path: file, reason: "third-party-not-in-scope" });
      sawOther = true;
      continue;
    }
    if (file.startsWith("experiments/")) {
      plan.skips.push({ path: file, reason: "isolated-experiment" });
      sawOther = true;
      continue;
    }
    if (file.startsWith("hub/web/")) {
      plan.skips.push({ path: file, reason: "hub-admin-baseline" });
      sawOther = true;
      continue;
    }
    if (file.startsWith(".github/workflows/mobilebot.yml")) {
      if (!mobilebot.ok) plan.rejected.push(mobilebot.error);
      plan.skips.push({ path: file, reason: "mobilebot-workflow" });
      sawOther = true;
      continue;
    }

    const dir = packageDir(file, list);
    if (!dir) {
      if (!isSharedModule(file)) plan.rejected.push(`unclassified path ${file}`);
      continue;
    }
    if (NAMED_SUITE_PACKAGES.has(dir)) {
      contract = true;
      sawOther = true;
      continue;
    }
    if (!/^[A-Za-z0-9._/-]+$/.test(dir)) {
      plan.rejected.push(`unsafe package path ${dir}`);
      continue;
    }
    if (!hasTests(dir, list)) {
      plan.rejected.push(`package ${dir} has no tests (${file})`);
      continue;
    }
    packages.add(`./${dir}`);
    sawOther = true;
  }

  if (governed || contract) {
    plan.jobs.lifecycle = true;
    plan.jobs.hub = true;
  }
  if (governed) plan.jobs.frontend = true;

  const sorted = [...packages].sort();
  if (sorted.length > MAX_PACKAGES) {
    plan.rejected.push(`too many Go packages (${sorted.length} > ${MAX_PACKAGES})`);
  } else {
    plan.jobs.packages = sorted;
  }

  if (plan.rejected.length > 0) {
    plan.result = "rejected";
    return plan;
  }
  const jobsOn = plan.jobs.lifecycle || plan.jobs.hub || plan.jobs.frontend || plan.jobs.packages.length > 0;
  if (jobsOn) {
    plan.result = "selected";
    return plan;
  }
  if (!sawOther && plan.docs.length > 0) {
    plan.result = "pass-docs-only";
    return plan;
  }
  const reasons = new Set(plan.skips.map((skip) => skip.reason));
  if (reasons.size === 1 && reasons.has("native-gui-not-in-scope")) {
    plan.result = "pass-native-gui-not-run";
    return plan;
  }
  if (reasons.size === 1 && reasons.has("delegated-mobilebot")) {
    plan.result = "pass-delegated-mobilebot";
    return plan;
  }
  if (plan.skips.length > 0) {
    plan.result = "pass-explicit-skips";
    return plan;
  }
  plan.rejected.push("no suite selected and no allowed skip");
  plan.result = "rejected";
  return plan;
}
