import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import { hubOutcomeSuite, lifecycleSuite, suiteRun } from "./suites.mjs";

function sourceHas(dir, name) {
  const absolute = dir.replace(/^\.\//, "");
  const files = fs.readdirSync(absolute).filter((file) => file.endsWith("_test.go"));
  return files.some((file) => fs.readFileSync(path.join(absolute, file), "utf8").includes(`func ${name}(`));
}

test("named lifecycle and hub tests exist in the packages the suite runs", () => {
  for (const suite of [lifecycleSuite, hubOutcomeSuite]) {
    const expression = suiteRun(suite);
    assert.match(expression, /^\^\(/);
    assert.match(expression, /\)\$$/);
    for (const [dir, names] of Object.entries(suite.packages)) {
      assert.ok(names.length > 0, dir);
      for (const name of names) assert.equal(sourceHas(dir, name), true, `${dir} ${name}`);
    }
  }
});

test("the pull-request workflow stays read-only and does not run untrusted code from pull_request_target", () => {
  const yaml = fs.readFileSync(".github/workflows/pr-check.yml", "utf8")
    .split("\n")
    .filter((line) => !/^\s*#/.test(line))
    .join("\n");
  assert.match(yaml, /pull_request:/);
  assert.doesNotMatch(yaml, /pull_request_target/);
  assert.match(yaml, /contents:\s*read/);
  assert.doesNotMatch(yaml, /contents:\s*write/);
  assert.doesNotMatch(yaml, /secrets\./);
  assert.match(yaml, /timeout-minutes:/);
  assert.match(yaml, /concurrency:/);
  assert.match(yaml, /npm ci/);
  assert.match(yaml, /# tests \[1-9\]/);
  assert.match(yaml, /steps\.vitest\.outcome/);
  assert.match(yaml, /steps\.tsc\.outcome/);
  assert.match(yaml, /steps\.vite\.outcome/);
  assert.match(yaml, /actions\/setup-go@v6/);
  assert.match(yaml, /cache:\s*true/);
  assert.match(yaml, /cache-dependency-path:\s*guiapp\/frontend\/package-lock.json/);
});
