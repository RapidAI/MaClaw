import { spawn } from "node:child_process";

import { evaluateGoJSON } from "./go-json.mjs";
import { hubOutcomeSuite, lifecycleSuite, suiteRun } from "./suites.mjs";

function fail(message) {
  process.stderr.write(`${message}\n`);
  process.exit(1);
}

function parseArgs(argv) {
  const suiteName = argv.includes("--suite") ? argv[argv.indexOf("--suite") + 1] : "";
  const fromEnv = argv.includes("--from-env") ? process.env[argv[argv.indexOf("--from-env") + 1]] || "" : "";
  return { suiteName, fromEnv };
}

function goArgs(suite, packages) {
  const args = ["test", "-json", "-count=1", `-timeout=${suite.timeout}`, "-p", "2"];
  if (suite.race) args.push("-race");
  if (suite.run) args.push("-run", suite.run);
  args.push(...packages);
  return args;
}

function runGo(args) {
  return new Promise((resolve) => {
    const child = spawn("go", args, {
      env: {
        ...process.env,
        MACLAW_TEST_MODELS: "off",
        GOMAXPROCS: process.env.GOMAXPROCS || "2",
      },
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    child.stdout.on("data", (chunk) => {
      const text = chunk.toString();
      stdout += text;
      process.stdout.write(text);
    });
    child.stderr.on("data", (chunk) => process.stderr.write(chunk));
    child.on("close", (code) => resolve({ code: code ?? 1, stdout }));
  });
}

const { suiteName, fromEnv } = parseArgs(process.argv.slice(2));
let expected;
let packages;
let suite;
if (suiteName === "lifecycle" || suiteName === "hub-outcome") {
  suite = suiteName === "lifecycle" ? lifecycleSuite : hubOutcomeSuite;
  suite = { ...suite, run: suiteRun(suite) };
  packages = Object.keys(suite.packages);
  expected = suite.packages;
} else if (fromEnv) {
  packages = fromEnv.split(/\s+/).filter(Boolean);
  if (packages.length === 0) fail("package list is empty");
  for (const pkg of packages) {
    if (!/^\.\/[A-Za-z0-9._/-]+$/.test(pkg)) fail(`refusing package ${JSON.stringify(pkg)}`);
  }
  suite = { timeout: "20m", race: false, run: "" };
  expected = Object.fromEntries(packages.map((pkg) => [pkg, null]));
} else {
  fail("use --suite lifecycle|hub-outcome or --from-env NAME");
}

const args = goArgs(suite, packages);
process.stderr.write(`go ${args.join(" ")}\n`);
const result = await runGo(args);
const evaluated = evaluateGoJSON(result.stdout, expected);
if (result.code !== 0 || !evaluated.ok) {
  for (const problem of evaluated.problems) process.stderr.write(`${problem}\n`);
  fail(`go test failed (exit ${result.code})`);
}
process.stdout.write(`PR_GATE_GO=${suiteName || "packages"} ok\n`);
