import fs from "node:fs";
import { pathToFileURL } from "node:url";

// Turn job results into one stable token. A skipped job that the plan
// asked to run is a failure, not a pass.

export function decide(input) {
  const select = input.SELECT_RESULT || "";
  if (select !== "success") {
    return { ok: false, result: "fail", detail: `select job is ${select || "missing"}` };
  }
  let plan;
  try {
    plan = JSON.parse(input.PLAN_JSON || "");
  } catch {
    return { ok: false, result: "fail", detail: "select job did not publish a plan" };
  }
  if (!plan || plan.result === "rejected" || (plan.rejected && plan.rejected.length > 0)) {
    return { ok: false, result: "fail", detail: "plan was rejected" };
  }
  const wanted = [
    ["lifecycle", Boolean(plan.jobs && plan.jobs.lifecycle), input.LIFECYCLE_RESULT],
    ["hub-outcome", Boolean(plan.jobs && plan.jobs.hub), input.HUB_RESULT],
    ["frontend", Boolean(plan.jobs && plan.jobs.frontend), input.FRONTEND_RESULT],
    ["go-packages", Boolean(plan.jobs && plan.jobs.packages && plan.jobs.packages.length > 0), input.PACKAGES_RESULT],
  ];
  for (const [name, want, got] of wanted) {
    const status = got || "missing";
    if (want && status !== "success") {
      return { ok: false, result: "fail", detail: `${name} should have run, status ${status}` };
    }
    if (!want && (status === "failure" || status === "cancelled")) {
      return { ok: false, result: "fail", detail: `${name} ${status}` };
    }
  }
  const result = plan.result || "fail";
  if (!result.startsWith("pass") && result !== "selected") {
    return { ok: false, result: "fail", detail: `unexpected plan result ${result}` };
  }
  const token = result === "selected" ? "pass" : result;
  return { ok: true, result: token, detail: token, skips: plan.skips || [], docs: plan.docs || [] };
}

function summary(decision) {
  const lines = [`PR_GATE_RESULT=${decision.result}`];
  if (decision.detail && decision.detail !== decision.result) lines.push(decision.detail);
  if (decision.skips && decision.skips.length > 0) {
    lines.push("skips:");
    for (const skip of decision.skips) lines.push(`- ${skip.reason}: ${skip.path}`);
  }
  if (decision.docs && decision.docs.length > 0) {
    lines.push("docs:");
    for (const doc of decision.docs) lines.push(`- ${doc}`);
  }
  return lines.join("\n") + "\n";
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  const decision = decide(process.env);
  const text = summary(decision);
  process.stdout.write(text);
  if (process.env.GITHUB_STEP_SUMMARY) {
    fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, text);
  }
  process.exit(decision.ok ? 0 : 1);
}
