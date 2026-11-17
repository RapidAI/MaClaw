import assert from "node:assert/strict";
import test from "node:test";

import { assertVitestReport } from "./run-vitest.mjs";

test("a zero-test vitest report is not a pass", () => {
  assert.throws(() => assertVitestReport({ numTotalTests: 0, numFailedTests: 0, success: true }), /zero tests/);
  assert.throws(() => assertVitestReport({ success: true }), /zero tests/);
});

test("a failed vitest report is not a pass", () => {
  assert.throws(() => assertVitestReport({ numTotalTests: 3, numFailedTests: 1, success: false }), /failed/);
});

test("a non-zero passing report is accepted", () => {
  assert.doesNotThrow(() => assertVitestReport({ numTotalTests: 39, numFailedTests: 0, success: true }));
});
