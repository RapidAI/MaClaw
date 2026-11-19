import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

export function assertVitestReport(report) {
  const total = report && report.numTotalTests;
  const failed = report && report.numFailedTests;
  if (typeof total !== "number" || total < 1) {
    throw new Error(`vitest ran zero tests (numTotalTests=${total})`);
  }
  if (failed !== 0 || report.success === false) {
    throw new Error(`vitest reported ${failed} failed tests`);
  }
}

function main() {
  const frontend = path.resolve(path.dirname(new URL(import.meta.url).pathname), "../../guiapp/frontend");
  const out = path.join(frontend, "pr-gate-vitest.json");
  fs.rmSync(out, { force: true });
  const run = spawnSync(
    "npx",
    ["vitest", "--run", "--reporter=default", "--reporter=json", "--outputFile", out],
    { cwd: frontend, stdio: "inherit" },
  );
  if (!fs.existsSync(out)) {
    process.stderr.write("vitest did not write a JSON report\n");
    process.exit(run.status || 1);
  }
  let report;
  try {
    report = JSON.parse(fs.readFileSync(out, "utf8"));
  } catch (err) {
    process.stderr.write(`vitest JSON report is not readable: ${err.message}\n`);
    process.exit(1);
  } finally {
    fs.rmSync(out, { force: true });
  }
  try {
    assertVitestReport(report);
  } catch (err) {
    process.stderr.write(`${err.message}\n`);
    process.exit(1);
  }
  if (run.status !== 0) process.exit(run.status || 1);
  process.stdout.write(`PR_GATE_VITEST=${report.numTotalTests}\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
