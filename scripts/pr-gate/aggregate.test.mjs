import assert from "node:assert/strict";
import test from "node:test";

import { decide } from "./aggregate.mjs";

function plan(overrides) {
  return JSON.stringify({
    result: "selected",
    jobs: { lifecycle: true, hub: true, frontend: false, packages: [] },
    skips: [],
    docs: [],
    rejected: [],
    ...overrides,
  });
}

test("a selected plan passes only when every requested job succeeded", () => {
  const ok = decide({
    SELECT_RESULT: "success",
    PLAN_JSON: plan(),
    LIFECYCLE_RESULT: "success",
    HUB_RESULT: "success",
    FRONTEND_RESULT: "skipped",
    PACKAGES_RESULT: "skipped",
  });
  assert.equal(ok.ok, true);
  assert.equal(ok.result, "pass");
});

test("a skipped job that was selected is a failure", () => {
  const missed = decide({
    SELECT_RESULT: "success",
    PLAN_JSON: plan(),
    LIFECYCLE_RESULT: "skipped",
    HUB_RESULT: "success",
    FRONTEND_RESULT: "skipped",
    PACKAGES_RESULT: "skipped",
  });
  assert.equal(missed.ok, false);
  assert.match(missed.detail, /lifecycle/);
});

test("docs-only stays a distinct pass when expensive jobs did not run", () => {
  const docs = decide({
    SELECT_RESULT: "success",
    PLAN_JSON: plan({
      result: "pass-docs-only",
      jobs: { lifecycle: false, hub: false, frontend: false, packages: [] },
      docs: ["README.md"],
    }),
    LIFECYCLE_RESULT: "skipped",
    HUB_RESULT: "skipped",
    FRONTEND_RESULT: "skipped",
    PACKAGES_RESULT: "skipped",
  });
  assert.equal(docs.ok, true);
  assert.equal(docs.result, "pass-docs-only");
});

test("select failure and a rejected plan do not pass", () => {
  assert.equal(decide({ SELECT_RESULT: "failure", PLAN_JSON: plan() }).ok, false);
  assert.equal(decide({ SELECT_RESULT: "success", PLAN_JSON: "" }).ok, false);
  const rejected = decide({
    SELECT_RESULT: "success",
    PLAN_JSON: plan({ result: "rejected", rejected: ["unclassified path x"] }),
    LIFECYCLE_RESULT: "skipped",
    HUB_RESULT: "skipped",
    FRONTEND_RESULT: "skipped",
    PACKAGES_RESULT: "skipped",
  });
  assert.equal(rejected.ok, false);
});

test("a failed job that the plan did not request still fails the aggregate", () => {
  const extra = decide({
    SELECT_RESULT: "success",
    PLAN_JSON: plan({
      result: "pass-docs-only",
      jobs: { lifecycle: false, hub: false, frontend: false, packages: [] },
    }),
    LIFECYCLE_RESULT: "failure",
    HUB_RESULT: "skipped",
    FRONTEND_RESULT: "skipped",
    PACKAGES_RESULT: "skipped",
  });
  assert.equal(extra.ok, false);
});
