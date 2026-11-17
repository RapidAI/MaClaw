import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { agentPrerequisite } from "./src/agent-gate.mjs";
import { startFixtures } from "./src/fixture-server.mjs";
import { runOnce } from "./src/mechanical.mjs";
import { promptBundle } from "./src/prompts.mjs";
import { runEditorFaults } from "./src/editor-faults.mjs";
import { runRichtext } from "./src/richtext.mjs";

const root = path.dirname(fileURLToPath(import.meta.url));
const mode = process.argv.includes("--mode") ? process.argv[process.argv.indexOf("--mode") + 1] : "mechanical";
const repeats = Number(process.env.MACLAW_BENCH_REPEATS || 3);

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
  lines.push("# 合成夹具测量（不是模型 A/B）");
  lines.push("");
  lines.push("模型调用次数：0。未使用其他模型代替 GLM 5.3 Flash。");
  lines.push("");
  lines.push("## 效率策略");
  lines.push("");
  lines.push("数字是测试进程用地面真值驱动执行器的上界，不是模型选出的坐标或 ref。");
  lines.push("`observation_ms` 只计快照生成或截图生成。坐标臂的 bbox 探测记在 `harness_bbox_ms`，不计入 observation_ms，也不换成 token。");
  lines.push("`chat_progress_events` 是同一次工具回合里的进度，不是模型调用。`model_calls` 恒为 0。");
  lines.push("");
  lines.push("| 任务 | 臂 | 策略 | 成功 | 中位 observation_ms | 中位 primitive_ms | 中位 bytes | 观察次数 | 原语 | 重试 | 分开工具回合 | 批处理工具回合 |");
  lines.push("| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |");
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
      `| ${task} | ${arm} | ${policy} | ${success}/${items.length} | ${median(items.map((item) => item.observation_ms))} | ${median(items.map((item) => item.primitive_ms))} | ${median(items.map((item) => item.observation_bytes))} | ${items[0].observations} | ${items[0].browser_primitives} | ${items[0].retries} | ${items[0].tool_rounds_if_separate} | ${items[0].tool_rounds_if_batched} |`,
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
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const outDir = path.join(root, "results", `richtext-${stamp}`);
  await mkdir(outDir, { recursive: true });
  const fixtures = await startFixtures();
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
  const payload = {
    ...gate,
    executed: false,
    model_calls: 0,
    runner: gate.ok ? "src/agent.mjs#runProvisionedTrial" : null,
  };
  console.log(JSON.stringify(payload, null, 2));
  if (!gate.ok) process.exit(2);
  console.error("端点已配置。调用 src/agent.mjs 的 runProvisionedTrial 才会发模型请求；本次命令不发。");
  process.exit(2);
}

const stamp = new Date().toISOString().replace(/[:.]/g, "-");
const outDir = path.join(root, "results", stamp);
await mkdir(outDir, { recursive: true });
const fixtures = await startFixtures();
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
  kind: "mechanical_and_interface_upper_bound_not_model_ab",
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
