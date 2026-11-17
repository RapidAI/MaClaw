import { createHash } from "node:crypto";
import os from "node:os";
import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { agentPrerequisite } from "./agent-gate.mjs";
import { runBoundedArms } from "./agent.mjs";
import { startFixtures } from "./fixture-server.mjs";
import { runOnce } from "./mechanical.mjs";
import { promptBundle } from "./prompts.mjs";
import { runEditorFaults } from "./editor-faults.mjs";
import { runRichtext } from "./richtext.mjs";

const root = path.dirname(fileURLToPath(import.meta.url));
const mode = process.argv.includes("--mode") ? process.argv[process.argv.indexOf("--mode") + 1] : "";
const repeats = Number(process.env.MACLAW_BENCH_REPEATS || 3);
const stamp = new Date().toISOString().replace(/[:.]/g, "-");

function git(args) {
  try {
    return execFileSync("git", args, { cwd: path.resolve(root, "../.."), encoding: "utf8" }).trim();
  } catch {
    return "";
  }
}

function median(values) {
  const nums = values.filter((value) => Number.isFinite(value)).sort((a, b) => a - b);
  if (!nums.length) return null;
  const mid = Math.floor(nums.length / 2);
  return nums.length % 2 ? nums[mid] : Math.round((nums[mid - 1] + nums[mid]) / 2);
}

function report(summary) {
  const lines = [];
  lines.push("# 脚本化观察开销微基准（不是模型 A/B，也不是代理成功）");
  lines.push("");
  lines.push("模型调用次数：0。各任务的策略集合并不相同，不能从这张表里选胜者。");
  lines.push("");
  lines.push("## 效率策略");
  lines.push("");
  lines.push("数字是测试进程用地面真值驱动执行器的上界，不是模型选出的坐标或 ref。");
  lines.push("`observation_ms` 只计快照生成或截图生成。坐标臂的 bbox 探测记在 `harness_bbox_ms`，不计入 observation_ms，也不换成 token。");
  lines.push("`chat_progress_events` 是同一次工具回合里的进度，不是模型调用。`model_calls` 恒为 0。");
  lines.push("");
  lines.push("| 任务 | 脚本臂 | 策略 | 夹具检查 | 中位 observation_ms | 中位 primitive_ms | 中位 bytes | 观察次数 | 原语 | 重试 | 脚本驱动回合 |");
  lines.push("| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |");
  const groups = new Map();
  for (const repeat of summary.repeats) {
    for (const item of repeat.efficiency.summaries) {
      const key = [item.task, item.arm, item.policy].join("|");
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(item);
    }
  }
  for (const [key, items] of groups) {
    const [task, arm, policy] = key.split("|");
    const success = items.filter((item) => item.success).length;
    lines.push(
      `| ${task} | ${arm} | ${policy} | ${success}/${items.length} | ${median(items.map((item) => item.observation_ms))} | ${median(items.map((item) => item.primitive_ms))} | ${median(items.map((item) => item.observation_bytes))} | ${items[0].observations ?? ""} | ${items[0].browser_primitives ?? ""} | ${items[0].retries ?? ""} | ${items[0].scripted_driver_rounds ?? ""} |`,
    );
  }
  lines.push("");
  lines.push("## 抽取通道");
  lines.push("");
  const first = summary.repeats[0];
  for (const [task, value] of Object.entries(first.tasks)) {
    if (!value.observed && !value.sees) continue;
    const sees = value.observed ? value.observed.sees : value.sees;
    if (!sees) continue;
    lines.push(`- ${task}: ${JSON.stringify(sees)}`);
  }
  lines.push("");
  lines.push("失败断言：");
  lines.push("```");
  lines.push(JSON.stringify(summary.repeats.flatMap((repeat, index) => repeat.failures.map((failure) => ({ repeat: index, ...failure }))), null, 2));
  lines.push("```");
  return lines.join("\n");
}

const gate = agentPrerequisite();
if (mode === "richtext") {
  const outDir = process.env.MACLAW_BENCH_OUT || path.join(os.tmpdir(), `maclaw-richtext-${stamp}`);
  await mkdir(outDir, { recursive: true });
  const fixtures = await startFixtures();
  const { chromium } = await import("playwright");
  const browser = await chromium.launch({
    channel: "chrome",
    headless: true,
    args: ["--no-sandbox", "--disable-dev-shm-usage"],
  });
  let result;
  let faults;
  try {
    result = await runRichtext(browser, fixtures.origin, outDir);
    faults = await runEditorFaults(browser, fixtures.origin, path.join(outDir, "faults"));
  } finally {
    await browser.close();
    await fixtures.close();
  }
  result.model_calls = 0;
  result.llm_ab_executed = false;
  result.agent_gate = gate;
  result.editor_faults = faults;
  const failures = [...result.failures, ...faults.failures];
  await writeFile(path.join(outDir, "richtext.json"), JSON.stringify(result, null, 2));
  await writeFile(path.join(outDir, "trace.jsonl"), [...result.trace, ...faults.trace].map((event) => JSON.stringify(event)).join("\n") + "\n");
  process.stdout.write(`wrote ${outDir}\nfailures ${failures.length}\nmodel_calls 0\n`);
  if (failures.length) console.log(failures.join("\n"));
  process.exit(failures.length === 0 ? 0 : 1);
}
if (mode === "agent") {
  if (!gate.ok) {
    console.log(JSON.stringify({ ...gate, executed: false, ready: false, model_calls: 0, observed_llm: false }, null, 2));
    process.exit(2);
  }
  const outDir = process.env.MACLAW_BENCH_OUT || path.join(os.tmpdir(), `maclaw-bench-${stamp}`);
  await mkdir(outDir, { recursive: true });
  const fixtures = await startFixtures();
  const { chromium } = await import("playwright");
  const browser = await chromium.launch({ channel: "chrome", headless: true, args: ["--no-sandbox", "--disable-dev-shm-usage"] });
  try {
    const summary = await runBoundedArms({
      env: process.env,
      browser,
      origin: fixtures.origin,
      outDir,
    });
    await writeFile(path.join(outDir, "ab-summary.json"), JSON.stringify(summary, null, 2));
    console.log(JSON.stringify({ wrote: outDir, model_calls: summary.model_calls, ready: true, observed_llm: summary.observed_llm }, null, 2));
    process.exit(summary.aborted ? 1 : 0);
  } finally {
    await browser.close();
    await fixtures.close();
  }
}

if (mode !== "microbench" && mode !== "mechanical") {
  console.error("use --mode agent, --mode microbench, or --mode richtext");
  process.exit(2);
}
const outDir = process.env.MACLAW_BENCH_OUT || path.join(os.tmpdir(), `maclaw-microbench-${stamp}`);
await mkdir(outDir, { recursive: true });
const fixtures = await startFixtures();
const { chromium } = await import("playwright");
const browser = await chromium.launch({
  channel: "chrome",
  headless: true,
  args: ["--no-sandbox", "--disable-dev-shm-usage"],
});
const repeatsOut = [];
try {
  for (let index = 0; index < repeats; index += 1) {
    process.stdout.write(`repeat ${index + 1}/${repeats}\n`);
    const shotDir = path.join(outDir, `repeat-${index}`);
    await mkdir(shotDir, { recursive: true });
    const result = await runOnce(browser, fixtures.origin, async (name, buffer) => {
      if (index === 0) await writeFile(path.join(shotDir, name), buffer);
    });
    repeatsOut.push(result);
    await writeFile(path.join(shotDir, "trace.jsonl"), result.efficiency.events.map((event) => JSON.stringify(event)).join("\n") + "\n");
  }
} finally {
  await browser.close();
  await fixtures.close();
}

const summary = {
  kind: "scripted_observation_overhead_microbench",
  not_agent_success: true,
  comparable_across_tasks: false,
  model_calls: 0,
  llm_ab_executed: false,
  agent_gate: gate,
  repeats_requested: repeats,
  maclaw_commit: git(["rev-parse", "HEAD"]),
  openbot_commit_inspected: "f694dea63e971afc3fd856d6ccff6c5a286ce347",
  prompt_sha256: createHash("sha256").update(JSON.stringify(promptBundle())).digest("hex"),
  chrome: repeatsOut.length ? "google-chrome via Playwright channel" : null,
  origin: fixtures.origin,
  repeats: repeatsOut,
};
await writeFile(path.join(outDir, "summary.json"), JSON.stringify(summary, null, 2));
await writeFile(path.join(outDir, "REPORT.zh.md"), report(summary));
const failures = repeatsOut.reduce((sum, repeat) => sum + repeat.failures.length, 0);
process.stdout.write(`wrote ${outDir}\nfailures ${failures}\nmodel_calls 0\n`);
process.exit(failures === 0 ? 0 : 1);
