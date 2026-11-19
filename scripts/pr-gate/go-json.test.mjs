import assert from "node:assert/strict";
import test from "node:test";

import { evaluateGoJSON } from "./go-json.mjs";

function line(event) {
  return JSON.stringify(event);
}

test("a package that passes with no matching tests is not a pass", () => {
  const text = [
    line({ Action: "output", Package: "example.com/MaClawSrv", Output: "ok  \texample.com/MaClawSrv\t0.01s [no tests to run]\n" }),
    line({ Action: "pass", Package: "example.com/MaClawSrv" }),
  ].join("\n");
  const result = evaluateGoJSON(text, { "./MaClawSrv": ["TestHTTPReadDuringFinalize"] });
  assert.equal(result.ok, false);
  assert.match(result.problems.join("\n"), /zero tests/);
});

test("a missing expected test fails even when the package passes", () => {
  const text = [
    line({ Action: "run", Package: "example.com/MaClawSrv", Test: "TestOther" }),
    line({ Action: "pass", Package: "example.com/MaClawSrv", Test: "TestOther" }),
    line({ Action: "pass", Package: "example.com/MaClawSrv" }),
  ].join("\n");
  const result = evaluateGoJSON(text, { "./MaClawSrv": ["TestHTTPReadDuringFinalize"] });
  assert.equal(result.ok, false);
  assert.match(result.problems.join("\n"), /did not run TestHTTPReadDuringFinalize/);
});

test("a real run of the expected tests passes", () => {
  const text = [
    line({ Action: "run", Package: "example.com/MaClawSrv", Test: "TestHTTPReadDuringFinalize" }),
    line({ Action: "run", Package: "example.com/MaClawSrv", Test: "TestHTTPReadDuringFinalize/sub" }),
    line({ Action: "pass", Package: "example.com/MaClawSrv", Test: "TestHTTPReadDuringFinalize" }),
    line({ Action: "output", Package: "example.com/MaClawSrv", Output: "ok  \texample.com/MaClawSrv\t1s\n" }),
    line({ Action: "pass", Package: "example.com/MaClawSrv" }),
  ].join("\n");
  const result = evaluateGoJSON(text, { "./MaClawSrv": ["TestHTTPReadDuringFinalize"] });
  assert.equal(result.ok, true, result.problems.join("\n"));
});

test("a failed test is not a pass", () => {
  const text = [
    line({ Action: "run", Package: "example.com/p", Test: "TestA" }),
    line({ Action: "fail", Package: "example.com/p", Test: "TestA" }),
    line({ Action: "fail", Package: "example.com/p" }),
  ].join("\n");
  const result = evaluateGoJSON(text, { "./p": ["TestA"] });
  assert.equal(result.ok, false);
});

test("non-JSON output fails closed", () => {
  const result = evaluateGoJSON("not json\n", { "./p": ["TestA"] });
  assert.equal(result.ok, false);
  assert.match(result.problems.join("\n"), /non-JSON/);
});

test("an unnamed package run must execute at least one test", () => {
  const text = [
    line({ Action: "output", Package: "example.com/corelib/plugin", Output: "?   \texample.com/corelib/plugin\t[no test files]\n" }),
    line({ Action: "pass", Package: "example.com/corelib/plugin" }),
  ].join("\n");
  const result = evaluateGoJSON(text, { "./corelib/plugin": null });
  assert.equal(result.ok, false);
});
