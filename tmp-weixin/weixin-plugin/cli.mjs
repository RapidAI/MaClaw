#!/usr/bin/env node

import { execSync, spawnSync } from "node:child_process";

const PLUGIN_SPEC = "@tencent-weixin/openclaw-weixin";
const CHANNEL_ID = "openclaw-weixin";

// ── helpers ──────────────────────────────────────────────────────────────────

function log(msg) {
  console.log(`\x1b[36m[openclaw-weixin]\x1b[0m ${msg}`);
}

function error(msg) {
  console.error(`\x1b[31m[openclaw-weixin]\x1b[0m ${msg}`);
}

function run(cmd, { silent = true } = {}) {
  const stdio = silent ? ["pipe", "pipe", "pipe"] : "inherit";
  const result = spawnSync(cmd, { shell: true, stdio });
  if (result.status !== 0) {
    const err = new Error(`Command failed with exit code ${result.status}: ${cmd}`);
    err.stderr = silent ? (result.stderr || "").toString() : "";
    throw err;
  }
  return silent ? (result.stdout || "").toString().trim() : "";
}

function which(bin) {
  try {
    return execSync(`which ${bin}`, { encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"] }).trim();
  } catch {
    return null;
  }
}

// ── commands ─────────────────────────────────────────────────────────────────

function install() {
  // 1. Check openclaw is installed
  if (!which("openclaw")) {
    error("未找到 openclaw，请先安装：");
    console.log("  npm install -g openclaw");
    console.log("  详见 https://docs.openclaw.ai/install");
    process.exit(1);
  }
  log("已找到本地安装的 openclaw");

  // 2. Install plugin via openclaw
  log("正在安装插件...");
  try {
    const installOut = run(`openclaw plugins install "${PLUGIN_SPEC}"`);
    if (installOut) log(installOut);
  } catch (installErr) {
    if (installErr.stderr && installErr.stderr.includes("already exists")) {
      log("检测到本地已安装，正在更新...");
      try {
        const updateOut = run(`openclaw plugins update "${CHANNEL_ID}"`);
        if (updateOut) log(updateOut);
      } catch (updateErr) {
        error("插件更新失败，请手动执行：");
        if (updateErr.stderr) console.error(updateErr.stderr);
        console.log(`  openclaw plugins update "${CHANNEL_ID}"`);
        process.exit(1);
      }
    } else {
      error("插件安装失败，请手动执行：");
      if (installErr.stderr) console.error(installErr.stderr);
      console.log(`  openclaw plugins install "${PLUGIN_SPEC}"`);
      process.exit(1);
    }
  }

  // 3. Login (interactive QR scan)
  log("插件就绪，开始首次连接...");
  try {
    run(`openclaw channels login --channel ${CHANNEL_ID}`, { silent: false });
  } catch {
    console.log();
    error("首次连接未完成，可稍后手动重试：");
    console.log(`  openclaw channels login --channel ${CHANNEL_ID}`);
  }

  // 4. Restart gateway so it picks up the new account
  log("正在重启 OpenClaw Gateway...");
  try {
    run(`openclaw gateway restart`, { silent: false });
  } catch {
    error("重启失败，可手动执行：");
    console.log(`  openclaw gateway restart`);
  }

}

function help() {
  console.log(`
  用法: npx -y @tencent-weixin/openclaw-weixin-cli <命令>

  命令:
    install   安装微信插件并扫码连接
    help      显示帮助信息
`);
}

// ── main ─────────────────────────────────────────────────────────────────────

const command = process.argv[2];

switch (command) {
  case "install":
    install();
    break;
  case "help":
  case "--help":
  case "-h":
    help();
    break;
  default:
    if (command) {
      error(`未知命令: ${command}`);
    }
    help();
    process.exit(command ? 1 : 0);
}
