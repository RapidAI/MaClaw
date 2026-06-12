#!/usr/bin/env node

import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { TextDecoder } from 'node:util';

const targetArgs = process.argv.slice(2).filter((arg) => !arg.startsWith('-'));
const targets = (targetArgs.length > 0 ? targetArgs : ['.']).map((arg) => path.resolve(arg));
const displayRoot = process.cwd();
const strictMojibake = process.argv.includes('--strict-mojibake');
const decoder = new TextDecoder('utf-8', { fatal: true });

const sourceExtensions = new Set([
  '.bat', '.cmd', '.css', '.dart', '.go', '.html', '.js', '.json', '.jsx',
  '.md', '.mjs', '.ps1', '.sh', '.toml', '.ts', '.tsx', '.txt', '.yaml', '.yml',
]);

const ignoredDirectories = new Set([
  '.claude', '.git', '.gocache', '.gomodcache', '.testhome', 'build', 'dist', 'node_modules',
  'vendor', '.vite', '.npm_cache', '__pycache__', 'tmp',
]);

const ignoredPathFragments = [
  `${path.sep}corelib${path.sep}opus${path.sep}libopus${path.sep}`,
  `${path.sep}docs${path.sep}`,
  `${path.sep}MaClawSrv${path.sep}API_MANUAL.zh-CN.md`,
  `${path.sep}RapidSpeech.cpp${path.sep}models${path.sep}`,
  `${path.sep}RapidSpeech.cpp${path.sep}scripts${path.sep}py_output.txt`,
  `${path.sep}mobile${path.sep}terminal${path.sep}android${path.sep}app${path.sep}build${path.sep}`,
  `${path.sep}RapidSpeech.cpp${path.sep}build${path.sep}`,
];

// Strict mode catches common mojibake signatures without embedding corrupted text
// samples in this file. U+FFFD is handled as a hard failure above.
const mojibakePatterns = [
  /\?\?\?\?+/,
  /[\u00C0-\u00FF]{2,}/,
  /(?:\u95B3\u30EF\u62F7|\u95F3\u54F4|\u60E7\u95C2|\u7F01\u5815)/,
];

const findings = [];
const warnings = [];

function shouldSkipDir(name) {
  return ignoredDirectories.has(name);
}

function shouldScanFile(filePath) {
  const normalized = filePath.split(path.sep).join(path.sep);
  if (ignoredPathFragments.some((fragment) => normalized.includes(fragment))) return false;
  return sourceExtensions.has(path.extname(filePath).toLowerCase());
}

function lineAndColumn(text, offset) {
  const prefix = text.slice(0, offset);
  const lines = prefix.split(/\r\n|\n|\r/);
  return { line: lines.length, column: lines[lines.length - 1].length + 1 };
}

function pushFinding(filePath, kind, detail) {
  findings.push({ filePath, kind, detail });
}

function scanFile(filePath) {
  const bytes = readFileSync(filePath);
  if (bytes.includes(0)) {
    pushFinding(filePath, 'NUL byte', 'source files must not contain binary NUL bytes');
    return;
  }

  let text;
  try {
    text = decoder.decode(bytes);
  } catch (error) {
    pushFinding(filePath, 'invalid UTF-8', error.message || 'file is not valid UTF-8');
    return;
  }

  const replacementOffset = text.indexOf('\uFFFD');
  if (replacementOffset !== -1) {
    const pos = lineAndColumn(text, replacementOffset);
    pushFinding(filePath, 'replacement character', `U+FFFD at ${pos.line}:${pos.column}`);
  }

  if (strictMojibake) {
    for (const pattern of mojibakePatterns) {
      const match = text.match(pattern);
      if (match?.index !== undefined) {
        const pos = lineAndColumn(text, match.index);
        warnings.push({ filePath, kind: 'possible mojibake', detail: `${JSON.stringify(match[0])} at ${pos.line}:${pos.column}` });
        break;
      }
    }
  }
}

function scanPath(target) {
  const info = statSync(target);
  if (info.isFile()) {
    if (shouldScanFile(target)) scanFile(target);
    return;
  }
  if (info.isDirectory()) walk(target);
}

function walk(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (!shouldSkipDir(entry.name)) walk(path.join(dir, entry.name));
      continue;
    }
    if (!entry.isFile()) continue;
    const filePath = path.join(dir, entry.name);
    if (shouldScanFile(filePath)) scanFile(filePath);
  }
}

for (const target of targets) scanPath(target);

const allFailures = strictMojibake ? findings.concat(warnings) : findings;
if (allFailures.length > 0) {
  console.error('Encoding check failed. Source files must be valid UTF-8.');
  for (const item of allFailures.slice(0, 80)) {
    console.error(`- ${path.relative(displayRoot, item.filePath)}: ${item.kind}: ${item.detail}`);
  }
  if (allFailures.length > 80) {
    console.error(`...and ${allFailures.length - 80} more.`);
  }
  if (!strictMojibake && warnings.length > 0) {
    console.error('Tip: run with --strict-mojibake to also fail on suspicious mojibake text.');
  }
  process.exit(1);
}

console.log(`Encoding check passed (${strictMojibake ? 'strict mojibake mode' : 'UTF-8 mode'}).`);
