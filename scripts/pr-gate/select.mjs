import { spawnSync } from "node:child_process";
import fs from "node:fs";

import { classify } from "./policy.mjs";

function fail(message) {
  process.stderr.write(`${message}\n`);
  process.exit(1);
}

function changedFiles(base) {
  const seen = spawnSync("git", ["cat-file", "-e", `${base}^{commit}`], { encoding: "utf8" });
  if (seen.status !== 0) {
    fail(`cannot see base commit ${base}; refusing to treat the diff as empty`);
  }
  const diff = spawnSync(
    "git",
    ["diff", "--name-only", "--diff-filter=ACDMRTUXB", "-z", base, "HEAD"],
    { encoding: "utf8" },
  );
  if (diff.status !== 0) fail(diff.stderr || "git diff failed");
  return diff.stdout.split("\0").filter(Boolean);
}

function writeOutput(name, value) {
  const file = process.env.GITHUB_OUTPUT;
  if (!file) return;
  if (/[\r\n]/.test(value)) fail(`output ${name} contains a newline`);
  fs.appendFileSync(file, `${name}=${value}\n`);
}

function writeSummary(plan) {
  const lines = [`PR_GATE_PLAN=${plan.result}`];
  if (plan.rejected.length > 0) {
    lines.push("rejected:");
    for (const item of plan.rejected) lines.push(`- ${item}`);
  }
  lines.push(`lifecycle=${plan.jobs.lifecycle} hub=${plan.jobs.hub} frontend=${plan.jobs.frontend}`);
  if (plan.jobs.packages.length > 0) {
    lines.push("packages:");
    for (const pkg of plan.jobs.packages) lines.push(`- ${pkg}`);
  }
  if (plan.skips.length > 0) {
    lines.push("skips:");
    for (const skip of plan.skips) lines.push(`- ${skip.reason}: ${skip.path}`);
  }
  const text = lines.join("\n") + "\n";
  process.stdout.write(text);
  if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, text);
}

function argsAfter(flag) {
  const index = process.argv.indexOf(flag);
  if (index < 0) return [];
  const values = [];
  for (let i = index + 1; i < process.argv.length; i++) {
    if (process.argv[i].startsWith("--")) break;
    values.push(process.argv[i]);
  }
  return values;
}

const files = process.argv.includes("--git")
  ? changedFiles(argsAfter("--git")[0] || "")
  : argsAfter("--files");

if (process.argv.includes("--git") && !argsAfter("--git")[0]) fail("missing base sha");

const plan = classify(files);
writeSummary(plan);
if (plan.result === "rejected") process.exit(1);

writeOutput("plan", JSON.stringify(plan));
writeOutput("result", plan.result);
writeOutput("lifecycle", plan.jobs.lifecycle ? "true" : "false");
writeOutput("hub", plan.jobs.hub ? "true" : "false");
writeOutput("frontend", plan.jobs.frontend ? "true" : "false");
writeOutput("packages", plan.jobs.packages.join(" "));
