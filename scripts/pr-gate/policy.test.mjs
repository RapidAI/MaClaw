import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";

import { matchGitHubPath, readPullRequestPaths } from "./mobilebot.mjs";
import { classify } from "./policy.mjs";

test("receipt and contract paths select the named suites", () => {
  const plan = classify([
    "MaClawSrv/desktop_receipt_integration_test.go",
    "corelib/agentservice/task_outcome.go",
  ]);
  assert.equal(plan.result, "selected");
  assert.equal(plan.jobs.lifecycle, true);
  assert.equal(plan.jobs.hub, true);
  assert.equal(plan.jobs.frontend, false);
  assert.deepEqual(plan.jobs.packages, []);
  assert.equal(plan.rejected.length, 0);
});

test("module manifest selects dependents and the manifest package", () => {
  const plan = classify(["corelib/agentruntime/builtin.go"]);
  assert.equal(plan.jobs.lifecycle, true);
  assert.equal(plan.jobs.hub, true);
  assert.ok(plan.jobs.packages.includes("./corelib/agentruntime"));
  assert.equal(plan.jobs.frontend, false);
});

test("shared module file selects contract suites only", () => {
  const plan = classify(["go.mod"]);
  assert.equal(plan.result, "selected");
  assert.equal(plan.jobs.lifecycle, true);
  assert.equal(plan.jobs.hub, true);
  assert.equal(plan.jobs.frontend, false);
  assert.deepEqual(plan.jobs.packages, []);
});

test("selector and workflow changes run every governed suite", () => {
  for (const file of [".github/workflows/pr-check.yml", "scripts/pr-gate/policy.mjs"]) {
    const plan = classify([file]);
    assert.equal(plan.jobs.lifecycle, true, file);
    assert.equal(plan.jobs.hub, true, file);
    assert.equal(plan.jobs.frontend, true, file);
    assert.equal(plan.result, "selected", file);
  }
});

test("a nearby package with tests is selected on its own", () => {
  const plan = classify(["corelib/plugin/manifest.go"]);
  assert.equal(plan.jobs.lifecycle, true);
  assert.ok(plan.jobs.packages.includes("./corelib/plugin"));
  assert.equal(plan.jobs.frontend, false);
});

test("frontend sources do not select Go or native GUI", () => {
  const plan = classify(["guiapp/frontend/package.json", "guiapp/frontend/src/App.tsx"]);
  assert.equal(plan.result, "selected");
  assert.equal(plan.jobs.frontend, true);
  assert.equal(plan.jobs.lifecycle, false);
  assert.equal(plan.jobs.hub, false);
  assert.deepEqual(plan.jobs.packages, []);
  assert.equal(plan.skips.length, 0);
});

test("docs only skip expensive jobs", () => {
  const plan = classify(["docs/pr-gate.md", "README.md"]);
  assert.equal(plan.result, "pass-docs-only");
  assert.equal(plan.jobs.lifecycle, false);
  assert.equal(plan.jobs.hub, false);
  assert.equal(plan.jobs.frontend, false);
  assert.deepEqual(plan.jobs.packages, []);
});

test("native GUI is an explicit skip, not a frontend run and not a pass of Go", () => {
  const plan = classify(["guiapp/app.go", "wails.json"]);
  assert.equal(plan.result, "pass-native-gui-not-run");
  assert.equal(plan.jobs.frontend, false);
  assert.equal(plan.jobs.lifecycle, false);
  assert.ok(plan.skips.every((skip) => skip.reason === "native-gui-not-in-scope"));
});

test("a MaClawSrv lookalike is not a contract path", () => {
  const plan = classify(["MaClawSrvv/foo.go"]);
  assert.equal(plan.result, "rejected");
  assert.equal(plan.jobs.lifecycle, false);
  assert.match(plan.rejected.join("\n"), /unclassified/);
});

test("an untested package fails closed", () => {
  const plan = classify(["corelib/telegram/gateway.go"]);
  assert.equal(plan.result, "rejected");
  assert.match(plan.rejected.join("\n"), /corelib\/telegram has no tests/);
  assert.equal(plan.jobs.lifecycle, false);
});

test("empty input and path traversal fail closed", () => {
  assert.equal(classify([]).result, "rejected");
  assert.equal(classify(["MaClawSrv/../../etc/passwd"]).result, "rejected");
  assert.equal(classify(["MaClawSrv/foo.go\ncorelib/agent/x.go"]).result, "rejected");
  const injected = classify(["docs/${{ secrets.TOKEN }}"]);
  assert.equal(injected.result, "rejected");
  assert.equal(injected.jobs.lifecycle, false);
});

test("third_party and hub admin are explicit skips, not docs-only", () => {
  const plan = classify([
    "third_party/wails/go.mod",
    "hub/web/admin/desktop-tab.js",
  ]);
  assert.equal(plan.result, "pass-explicit-skips");
  assert.equal(plan.jobs.lifecycle, false);
  assert.ok(plan.skips.some((skip) => skip.reason === "third-party-not-in-scope"));
  assert.ok(plan.skips.some((skip) => skip.reason === "hub-admin-baseline"));
});

test("mobilebot changes delegate only when that workflow would run", () => {
  const plan = classify(["mobilebot/lib/main.dart"]);
  assert.equal(plan.result, "pass-delegated-mobilebot");
  assert.equal(plan.jobs.lifecycle, false);
  assert.equal(plan.jobs.frontend, false);

  const dropped = classify(["mobilebot/lib/main.dart"], {
    mobilebot: { ok: false, error: "mobilebot workflow has no pull_request trigger" },
  });
  assert.equal(dropped.result, "rejected");
  assert.match(dropped.rejected.join("\n"), /would not run/);
});

test("docs beside a contract file still run the contract suites", () => {
  const plan = classify(["README.md", "MaClawSrv/bot_pdf.go"]);
  assert.equal(plan.result, "selected");
  assert.equal(plan.jobs.lifecycle, true);
  assert.equal(plan.docs.length, 1);
});

test("mobilebot path filters match the real workflow and reject a near miss", () => {
  const yaml = fs.readFileSync(".github/workflows/mobilebot.yml", "utf8");
  const spec = readPullRequestPaths(yaml);
  assert.equal(spec.ok, true);
  assert.equal(matchGitHubPath("mobilebot/**", "mobilebot/lib/main.dart"), true);
  assert.equal(matchGitHubPath("mobilebot/**", "mobilebot-extra/lib/main.dart"), false);
  assert.ok(spec.paths.some((glob) => matchGitHubPath(glob, "hub/internal/httpapi/router.go")));
  assert.equal(spec.paths.some((glob) => matchGitHubPath(glob, "MaClawSrv/bot_pdf.go")), false);
  const removed = readPullRequestPaths("name: x\non:\n  push:\n    paths:\n      - mobilebot/**\n");
  assert.equal(removed.ok, false);
});
