import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, '..');
const selfTestConfigPersistence = process.argv.includes('--self-test-config-persistence') || process.argv.includes('--self-test-saveconfig');
const read = (rel) => fs.readFileSync(path.join(repoRoot, rel), 'utf8');
const exists = (rel) => fs.existsSync(path.join(repoRoot, rel));
const failures = [];
const requireFile = (rel) => {
  if (!exists(rel)) failures.push(`missing required file: ${rel}`);
};
const requireIncludes = (rel, needle, label = needle) => {
  if (!exists(rel)) {
    failures.push(`missing required file: ${rel} (cannot check ${label})`);
    return;
  }
  const text = read(rel);
  if (!text.includes(needle)) failures.push(`${rel} is missing ${label}`);
};
const requireExcludes = (rel, needle, label = needle) => {
  const text = read(rel);
  if (text.includes(needle)) failures.push(`${rel} still contains ${label}`);
};
const requireOrder = (rel, before, after, label) => {
  const text = read(rel);
  const beforeIndex = text.indexOf(before);
  const afterIndex = text.indexOf(after);
  if (beforeIndex === -1 || afterIndex === -1 || beforeIndex > afterIndex) failures.push(`${rel} has wrong order for ${label}`);
};
const walkFiles = (dir, out = []) => {
  for (const entry of fs.readdirSync(path.join(repoRoot, dir), { withFileTypes: true })) {
    const rel = path.join(dir, entry.name).replace(/\\/g, '/');
    if (entry.isDirectory()) {
      if (rel === 'guiapp/frontend/dist' || rel === 'guiapp/frontend/node_modules') continue;
      walkFiles(rel, out);
    }
    else out.push(rel);
  }
  return out;
};
const saveConfigAllowedSnippets = [
  ['guiapp/app.go', 'func (a *App) SaveConfig(config corelib.AppConfig) error', 'SaveConfig implementation'],
  ['guiapp/config_manager.go', 'm.app.SaveConfig(mergedCfg)', 'config import owns a full merged config snapshot'],
  ['guiapp/app_user_data_migration.go', 'return a.SaveConfig(cfg)', 'user data migration restores a validated full configuration snapshot'],
  ['guiapp/frontend/src/App.tsx', 'PatchConfigFields(patch)', 'model settings save uses atomic patch to avoid TOCTOU race'],
  ['guiapp/frontend/wailsjs/go/main/App.d.ts', 'export function SaveConfig', 'generated Wails binding declaration'],
  ['guiapp/frontend/wailsjs/go/main/App.js', 'export function SaveConfig', 'generated Wails binding implementation'],
  ['guiapp/frontend/wailsjs/go/main/App.js', "window['go']['main']['App']['SaveConfig'](arg1)", 'generated Wails binding forwards to backend SaveConfig'],
  ['guiapp/frontend/src/components/remote/useRemotePanel.ts', 'SaveConfig({ ...config, field: value })', 'comment documenting the stale snapshot bug pattern'],
  ['guiapp/tui_mode.go', 'a.app.SaveConfig(cfg)', 'TUI HasConfig path carries a full config snapshot'],
  ['guiapp/app_moa_config.go', 'a.SaveConfig(cfg)', 'MoA validation persists the complete normalized MoA configuration snapshot'],
  
];
const saveConfigCallPatterns = [
  /SaveConfig\s*\(/,
  /SaveConfig\s*\?\.\s*\(/,
  /\.SaveConfig\s*\(/,
  /\.SaveConfig\s*\?\.\s*\(/,
  /\[['"]SaveConfig['"]\]\s*\(/,
  /\[['"]SaveConfig['"]\]\s*\?\.\s*\(/,
];
const patchConfigFieldsCallPattern = /\bPatchConfigFields\s*(?:\?\.)?\s*\(/;
const saveConfigAliasPatterns = [
  /\b(?:const|let|var)\s+\w+\s*=\s*SaveConfig\b/,
  /\b(?:const|let|var)\s+\w+\s*=\s*[\w.]+\.SaveConfig\b/,
  /\b(?:const|let|var)\s+\w+\s*=\s*[^;\n]*\[['"]SaveConfig['"]\]/,
  /\{[^}]*\bSaveConfig\s*:\s*\w+\b/,
  /\{[^}]*\bSaveConfig\b[^}]*\}\s*=\s*\w+/,
  /\bSaveConfig\s+as\s+\w+\b/,
  /\b[A-Za-z]\w*\s*(?::=|=)\s*\w+\.SaveConfig\b/,
];
const collectSaveConfigAllowlistFailures = (files, readFile, allowedEntries = saveConfigAllowedSnippets) => {
  const allowed = [
    ...allowedEntries,
  ];
  const fileSet = new Set(files);
  const allowedByFile = new Map();
  const found = [];
  for (const [rel, snippet, reason] of allowed) {
    if (!reason || reason.trim().length < 12) {
      found.push(`${rel} SaveConfig allowlist entry for ${JSON.stringify(snippet)} needs a clear reason`);
      continue;
    }
    if (!fileSet.has(rel)) {
      found.push(`${rel} SaveConfig allowlist entry for ${JSON.stringify(snippet)} points to a missing scanned file`);
      continue;
    }
    if (!readFile(rel).includes(snippet)) {
      found.push(`${rel} SaveConfig allowlist entry for ${JSON.stringify(snippet)} no longer matches current code`);
      continue;
    }
    if (!allowedByFile.has(rel)) allowedByFile.set(rel, []);
    allowedByFile.get(rel).push(snippet);
  }
  for (const rel of files) {
    if (!/\.(go|js|ts|tsx|d\.ts)$/.test(rel) || /_test\.go$|\.test\.tsx$|\.test\.ts$|\.test\.js$/.test(rel)) continue;
    const text = readFile(rel);
    const hasSaveConfigCall = saveConfigCallPatterns.some((pattern) => pattern.test(text));
    const hasSaveConfigAlias = saveConfigAliasPatterns.some((pattern) => pattern.test(text));
    if (!hasSaveConfigCall && !hasSaveConfigAlias) continue;
    const snippets = allowedByFile.get(rel) || [];
    text.split(/\r?\n/).forEach((line, index) => {
      if (saveConfigCallPatterns.some((pattern) => pattern.test(line)) && !snippets.some((snippet) => line.includes(snippet))) {
        found.push(`${rel}:${index + 1} has unallowlisted SaveConfig usage; use PatchConfig/PatchConfigFields for partial saves`);
      }
      if (saveConfigAliasPatterns.some((pattern) => pattern.test(line)) && !snippets.some((snippet) => line.includes(snippet))) {
        found.push(`${rel}:${index + 1} aliases SaveConfig; call PatchConfig/PatchConfigFields directly for partial saves`);
      }
    });
  }
  return found;
};
const requireSaveConfigAllowlist = () => {
  failures.push(...collectSaveConfigAllowlistFailures(walkFiles('guiapp'), read));
};

const findMatchingBrace = (text, openIndex) => {
  let depth = 0;
  let quote = '';
  let escaped = false;
  for (let i = openIndex; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (escaped) escaped = false;
      else if (ch === '\\') escaped = true;
      else if (ch === quote) quote = '';
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === '{') depth += 1;
    else if (ch === '}') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
};
const patchConfigFieldsFunctionBody = (text) => {
  const start = text.indexOf('func (a *App) PatchConfigFields');
  if (start === -1) return '';
  const signatureEnd = text.indexOf('\n', start);
  const open = text.lastIndexOf('{', signatureEnd === -1 ? text.length : signatureEnd);
  if (open === -1) return '';
  const close = findMatchingBrace(text, open);
  return close === -1 ? '' : text.slice(open + 1, close);
};
const supportedPatchConfigFields = (readFile) => {
  const body = patchConfigFieldsFunctionBody(readFile('guiapp/app.go'));
  return new Set(Array.from(body.matchAll(/case\s+"([^"]+)"\s*:/g), (match) => match[1]));
};
const extractPatchConfigFieldKeys = (text, startIndex) => {
  const callOpen = text.indexOf('(', startIndex);
  if (callOpen === -1) return [];
  let cursor = callOpen + 1;
  while (/\s/.test(text[cursor] || '')) cursor += 1;
  if (text.startsWith('map[string]interface{}', cursor)) {
    cursor += 'map[string]interface{}'.length;
  }
  while (/\s/.test(text[cursor] || '')) cursor += 1;
  if (text[cursor] !== '{') return [];
  const close = findMatchingBrace(text, cursor);
  if (close === -1) return [];
  const body = text.slice(cursor + 1, close);
  const keys = [];
  let depth = 0;
  let quote = '';
  let escaped = false;
  let segmentStart = 0;
  const pushSegment = (segment) => {
    const match = segment.match(/^\s*(?:["']([^"']+)["']|([A-Za-z_]\w*))\s*:/);
    if (match) keys.push(match[1] || match[2]);
  };
  for (let i = 0; i <= body.length; i += 1) {
    const ch = body[i] || ',';
    if (quote) {
      if (escaped) escaped = false;
      else if (ch === '\\') escaped = true;
      else if (ch === quote) quote = '';
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === '{' || ch === '[' || ch === '(') depth += 1;
    else if (ch === '}' || ch === ']' || ch === ')') depth -= 1;
    else if (ch === ',' && depth === 0) {
      pushSegment(body.slice(segmentStart, i));
      segmentStart = i + 1;
    }
  }
  return keys;
};
const collectPatchConfigFieldFailures = (files, readFile) => {
  const supported = supportedPatchConfigFields(readFile);
  const found = [];
  if (supported.size === 0) {
    return ['guiapp/app.go PatchConfigFields has no discoverable supported field cases'];
  }
  for (const rel of files) {
    if (!/\.(go|js|ts|tsx)$/.test(rel) || /_test\.go$|\.test\.tsx$|\.test\.ts$|\.test\.js$/.test(rel)) continue;
    const text = readFile(rel);
    let index = -1;
    while ((index = text.indexOf('PatchConfigFields', index + 1)) !== -1) {
      const keys = extractPatchConfigFieldKeys(text, index);
      for (const key of keys) {
        if (!supported.has(key)) {
          const line = text.slice(0, index).split(/\r?\n/).length;
          found.push(`${rel}:${line} patches unsupported config field ${JSON.stringify(key)}; add it to App.PatchConfigFields or fix the key`);
        }
      }
    }
  }
  return found;
};
const requirePatchConfigFieldsSupported = () => {
  failures.push(...collectPatchConfigFieldFailures(walkFiles('guiapp'), read));
};

const patchConfigFieldsDynamicAllowedSnippets = [
  ['guiapp/app_proxy.go', 'a.PatchConfigFields(patch)', 'proxy setter builds a closed patch map from validated proxy option keys'],
  ['guiapp/frontend/src/App.tsx', 'PatchConfigFields(patch)).then((saved)', 'model settings panel builds a closed tool-config patch from TOOL_NAMES loop'],
  ['guiapp/frontend/src/App.tsx', 'PatchConfigFields(patch)', 'settings panels pass locally typed patch objects through the settings shell'],
  ['guiapp/frontend/src/App.tsx', '// Using PatchConfigFields (atomic load', 'comment explaining atomic patch mechanism'],
  ['guiapp/frontend/src/components/remote/useRemotePanel.ts', 'PatchConfigFields(patch).then((saved)', 'remote panel saveConfigPatch helper receives patches built by local typed setters'],
  ['guiapp/frontend/src/components/remote/useRemotePanel.ts', 'PatchConfigFields(patchWithLaunchMode as Record<string, any>)', 'remote quick-start augments a locally built patch with default_launch_mode'],
  ['guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'PatchConfigFields(patch)).then((saved)', 'general settings helper receives patches from same-file controls only'],
  ['guiapp/frontend/src/components/ai/AssistantQuickSettingsBar.tsx', 'PatchConfigFields({ [field]: next } as Record<string, any>)', 'quick settings toggles choose from a closed workstation/log-detail field union'],
  ['guiapp/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'PatchConfigFields(patch).then((saved)', 'advanced settings helper receives patches from same-file controls only'],
  ['guiapp/frontend/src/components/settings/programmingToolsConfig.ts', 'PatchConfigFields(patch).then((saved)', 'shared programming tools patch helper receives patches from panel controls only'],
  ['guiapp/remote_activation.go', 'a.PatchConfigFields(patch)', 'remote registration persists a closed patch map containing only normalized remote_email or remote_mobile'],
  ['guiapp/frontend/src/components/settings/ModelRoutesSettingsSection.tsx', 'PatchConfigFields({ model_routes })', 'model routes panel persists its closed model_routes patch'],
  ['guiapp/openhuman_wiring.go', 'PatchConfigFields(model_routes)', 'model router reload documents the closed model_routes patch'],
  ['guiapp/computer_use_warmup.go', 'a.PatchConfigFields(patch)', 'computer-use log prune policy patch is assembled from validated keep/max-age fields and optional auto-prune toggle'],
];
const collectDynamicPatchConfigFieldFailures = (files, readFile, allowedEntries = patchConfigFieldsDynamicAllowedSnippets) => {
  const fileSet = new Set(files);
  const allowedByFile = new Map();
  const found = [];
  for (const [rel, snippet, reason] of allowedEntries) {
    if (!reason || reason.trim().length < 12) {
      found.push(`${rel} PatchConfigFields dynamic allowlist entry for ${JSON.stringify(snippet)} needs a clear reason`);
      continue;
    }
    if (!fileSet.has(rel)) {
      found.push(`${rel} PatchConfigFields dynamic allowlist entry for ${JSON.stringify(snippet)} points to a missing scanned file`);
      continue;
    }
    if (!readFile(rel).includes(snippet)) {
      found.push(`${rel} PatchConfigFields dynamic allowlist entry for ${JSON.stringify(snippet)} no longer matches current code`);
      continue;
    }
    if (!allowedByFile.has(rel)) allowedByFile.set(rel, []);
    allowedByFile.get(rel).push(snippet);
  }
  for (const rel of files) {
    if (!/\.(go|js|ts|tsx)$/.test(rel) || /_test\.go$|\.test\.tsx$|\.test\.ts$|\.test\.js$/.test(rel)) continue;
    if (rel.startsWith('guiapp/frontend/wailsjs/')) continue;
    const text = readFile(rel);
    const snippets = allowedByFile.get(rel) || [];
    let index = -1;
    while ((index = text.indexOf('PatchConfigFields', index + 1)) !== -1) {
      const lineStart = text.lastIndexOf('\n', index) + 1;
      const lineEnd = text.indexOf('\n', index);
      const line = text.slice(lineStart, lineEnd === -1 ? text.length : lineEnd);
      if (!patchConfigFieldsCallPattern.test(line)) continue;
      if (line.includes('func (a *App) PatchConfigFields')) continue;
      if (/\bPatchConfigFields\s*(?:\?\.)?\s*\(\s*[A-Za-z_$]\w*\s*:\s*[^)]*\)\s*(?::|;)/.test(line)) continue;
      const keys = extractPatchConfigFieldKeys(text, index);
      if (keys.length > 0) continue;
      if (snippets.some((snippet) => line.includes(snippet))) continue;
      const lineNo = text.slice(0, index).split(/\r?\n/).length;
      found.push(`${rel}:${lineNo} has dynamic PatchConfigFields patch; use an object literal or add a reasoned allowlist entry`);
    }
  }
  return found;
};
const requireDynamicPatchConfigFieldsAllowlist = () => {
  failures.push(...collectDynamicPatchConfigFieldFailures(walkFiles('guiapp'), read));
};

if (selfTestConfigPersistence) {
  const files = ['guiapp/app.go', 'guiapp/new_partial_save.go', 'guiapp/spaced_partial_save.go', 'guiapp/optional_partial.go', 'guiapp/aliased_partial_save.go', 'guiapp/frontend/src/aliased.ts', 'guiapp/frontend/src/generated_partial.js', 'guiapp/frontend/src/optional_partial.ts', 'guiapp/frontend/src/dot_alias.ts', 'guiapp/frontend/src/destructure_plain_alias.ts', 'guiapp/frontend/src/bracket_partial.js', 'guiapp/frontend/src/bracket_optional_partial.js', 'guiapp/frontend/src/bracket_alias.js', 'guiapp/frontend/src/deep_bracket_alias.js', 'guiapp/frontend/src/destructure_alias.ts'];
  const contents = {
    'guiapp/app.go': 'func (a *App) SaveConfig(config corelib.AppConfig) error { return nil }',
    'guiapp/new_partial_save.go': 'func bad(a *App, cfg corelib.AppConfig) { _ = a.SaveConfig(cfg) }',
    'guiapp/spaced_partial_save.go': 'func bad(a *App, cfg corelib.AppConfig) { _ = a.SaveConfig (cfg) }',
    'guiapp/optional_partial.go': 'func bad(a *App, cfg corelib.AppConfig) { _ = a.SaveConfig?.(cfg) }',
    'guiapp/aliased_partial_save.go': 'func bad(a *App) { save := a.SaveConfig; _ = save }',
    'guiapp/frontend/src/aliased.ts': 'import { SaveConfig as saveConfig } from "../wailsjs/go/main/App";',
    'guiapp/frontend/src/generated_partial.js': 'export function bad(cfg) { return SaveConfig(cfg); }',
    'guiapp/frontend/src/optional_partial.ts': 'export function bad(cfg: any) { return SaveConfig?.(cfg); }',
    'guiapp/frontend/src/dot_alias.ts': 'const save = app.SaveConfig; export function bad(cfg: any) { return save(cfg); }',
    'guiapp/frontend/src/destructure_plain_alias.ts': 'const { SaveConfig } = app; export function bad(cfg: any) { return SaveConfig; }',
    'guiapp/frontend/src/bracket_partial.js': 'export function bad(app, cfg) { return app["SaveConfig"](cfg); }',
    'guiapp/frontend/src/bracket_optional_partial.js': 'export function bad(app, cfg) { return app["SaveConfig"]?.(cfg); }',
    'guiapp/frontend/src/bracket_alias.js': 'const save = app["SaveConfig"]; export function bad(cfg) { return save(cfg); }',
    'guiapp/frontend/src/deep_bracket_alias.js': 'const save = window["go"]["main"]["App"]["SaveConfig"]; export function bad(cfg) { return save(cfg); }',
    'guiapp/frontend/src/destructure_alias.ts': 'const { SaveConfig: save } = app; export function bad(cfg: any) { return save(cfg); }',
  };
  const result = collectSaveConfigAllowlistFailures(
    files,
    (rel) => contents[rel] || '',
    [['guiapp/app.go', 'func (a *App) SaveConfig(config corelib.AppConfig) error', 'SaveConfig implementation used by self-test']]
  );
  if (result.length !== 14 || !result.some((item) => item.includes('guiapp/new_partial_save.go:1')) || !result.some((item) => item.includes('guiapp/spaced_partial_save.go:1')) || !result.some((item) => item.includes('guiapp/optional_partial.go:1')) || !result.some((item) => item.includes('guiapp/aliased_partial_save.go:1')) || !result.some((item) => item.includes('guiapp/frontend/src/aliased.ts:1')) || !result.some((item) => item.includes('guiapp/frontend/src/generated_partial.js:1')) || !result.some((item) => item.includes('guiapp/frontend/src/optional_partial.ts:1')) || !result.some((item) => item.includes('guiapp/frontend/src/dot_alias.ts:1')) || !result.some((item) => item.includes('guiapp/frontend/src/destructure_plain_alias.ts:1')) || !result.some((item) => item.includes('guiapp/frontend/src/bracket_partial.js:1')) || !result.some((item) => item.includes('guiapp/frontend/src/bracket_optional_partial.js:1')) || !result.some((item) => item.includes('guiapp/frontend/src/bracket_alias.js:1')) || !result.some((item) => item.includes('guiapp/frontend/src/deep_bracket_alias.js:1')) || !result.some((item) => item.includes('guiapp/frontend/src/destructure_alias.ts:1'))) {
    console.error('SaveConfig allowlist self-test failed:', result);
    process.exit(1);
  }
  const missingReason = collectSaveConfigAllowlistFailures(
    ['guiapp/app.go'],
    (rel) => contents[rel] || '',
    [['guiapp/app.go', 'func (a *App) SaveConfig(config corelib.AppConfig) error', '']]
  );
  if (!missingReason.some((item) => item.includes('needs a clear reason'))) {
    console.error('SaveConfig allowlist reason self-test failed:', missingReason);
    process.exit(1);
  }
  const staleEntry = collectSaveConfigAllowlistFailures(
    ['guiapp/app.go'],
    (rel) => contents[rel] || '',
    [['guiapp/app.go', 'not present SaveConfig snippet', 'stale allowlist entries must fail clearly']]
  );
  if (!staleEntry.some((item) => item.includes('no longer matches current code'))) {
    console.error('SaveConfig allowlist stale-entry self-test failed:', staleEntry);
    process.exit(1);
  }
  const patchFieldFiles = ['guiapp/app.go', 'guiapp/frontend/src/good_patch.ts', 'guiapp/frontend/src/bad_patch.ts', 'guiapp/good_patch.go', 'guiapp/bad_patch.go'];
  const patchFieldContents = {
    'guiapp/app.go': 'func (a *App) PatchConfigFields(patch map[string]interface{}) { switch key { case "remote_email": case "projects": } }',
    'guiapp/frontend/src/good_patch.ts': 'PatchConfigFields({ remote_email: email, projects: list });',
    'guiapp/frontend/src/bad_patch.ts': 'PatchConfigFields({ remote_mail: email });',
    'guiapp/good_patch.go': 'func good() { PatchConfigFields(map[string]interface{}{"remote_email": email}) }',
    'guiapp/bad_patch.go': 'func bad() { PatchConfigFields(map[string]interface{}{"remote_mail": email}) }',
  };
  const patchFieldResult = collectPatchConfigFieldFailures(patchFieldFiles, (rel) => patchFieldContents[rel] || '');
  if (patchFieldResult.length !== 2 || !patchFieldResult.some((item) => item.includes('guiapp/frontend/src/bad_patch.ts:1')) || !patchFieldResult.some((item) => item.includes('guiapp/bad_patch.go:1'))) {
    console.error('PatchConfigFields supported-field self-test failed:', patchFieldResult);
    process.exit(1);
  }
  const dynamicPatchFiles = ['guiapp/app.go', 'guiapp/frontend/src/good_dynamic.ts', 'guiapp/frontend/src/bad_dynamic.ts', 'guiapp/frontend/src/optional_bad_dynamic.ts', 'guiapp/frontend/src/computed_key_patch.ts', 'guiapp/frontend/src/type_signature.ts'];
  const dynamicPatchContents = {
    'guiapp/app.go': 'func (a *App) PatchConfigFields(patch map[string]interface{}) { switch key { case "remote_email": } }',
    'guiapp/frontend/src/good_dynamic.ts': 'function save(patch: Record<string, any>) { return PatchConfigFields(patch); }',
    'guiapp/frontend/src/bad_dynamic.ts': 'function save(patch: Record<string, any>) { return PatchConfigFields(patch); }',
    'guiapp/frontend/src/optional_bad_dynamic.ts': 'function save(patch: Record<string, any>) { return PatchConfigFields?.(patch); }',
    'guiapp/frontend/src/computed_key_patch.ts': 'function save(key: string, value: boolean) { return PatchConfigFields({ [key]: value }); }',
    'guiapp/frontend/src/type_signature.ts': 'type API = { PatchConfigFields(patch: Record<string, unknown>): Promise<unknown>; };',
  };
  const dynamicPatchResult = collectDynamicPatchConfigFieldFailures(
    dynamicPatchFiles,
    (rel) => dynamicPatchContents[rel] || '',
    [['guiapp/frontend/src/good_dynamic.ts', 'PatchConfigFields(patch)', 'self-test allowlisted local dynamic patch helper']]
  );
  if (dynamicPatchResult.length !== 3 || !dynamicPatchResult.some((item) => item.includes('guiapp/frontend/src/bad_dynamic.ts:1')) || !dynamicPatchResult.some((item) => item.includes('guiapp/frontend/src/optional_bad_dynamic.ts:1')) || !dynamicPatchResult.some((item) => item.includes('guiapp/frontend/src/computed_key_patch.ts:1'))) {
    console.error('PatchConfigFields dynamic-patch self-test failed:', dynamicPatchResult);
    process.exit(1);
  }
  const dynamicPatchMissingReason = collectDynamicPatchConfigFieldFailures(
    dynamicPatchFiles,
    (rel) => dynamicPatchContents[rel] || '',
    [['guiapp/frontend/src/good_dynamic.ts', 'PatchConfigFields(patch)', '']]
  );
  if (!dynamicPatchMissingReason.some((item) => item.includes('needs a clear reason'))) {
    console.error('PatchConfigFields dynamic-patch reason self-test failed:', dynamicPatchMissingReason);
    process.exit(1);
  }
  const dynamicPatchStaleEntry = collectDynamicPatchConfigFieldFailures(
    dynamicPatchFiles,
    (rel) => dynamicPatchContents[rel] || '',
    [['guiapp/frontend/src/good_dynamic.ts', 'PatchConfigFields(missingPatch)', 'stale dynamic patch allowlist entries must fail clearly']]
  );
  if (!dynamicPatchStaleEntry.some((item) => item.includes('no longer matches current code'))) {
    console.error('PatchConfigFields dynamic-patch stale-entry self-test failed:', dynamicPatchStaleEntry);
    process.exit(1);
  }
  console.log('Config persistence guard self-test passed.');
  process.exit(0);
}

const mojibakeMarkers = [
  0x95b0, 0x935a, 0x935f, 0x923c, 0x9983, 0x93c8, 0x59d7, 0x9352,
  0x9427, 0x6d5c, 0x7edb, 0x7487, 0x9359, 0x93c2, 0x95ab, 0x6fb6,
  0x9357, 0x675e, 0x7ead, 0x93c1, 0x947e, 0x95b2,
  0x9354, 0x589c, 0x9429, 0x621e, 0x93c5, 0x923b, 0x9983,
  0x93b7, 0x6434, 0x922f, 0x9229,
].map((codePoint) => String.fromCodePoint(codePoint));
const requireNoMojibake = (rel) => {
  const text = read(rel);
  for (const marker of mojibakeMarkers) {
    if (text.includes(marker)) failures.push(rel + ' contains probable mojibake marker ' + JSON.stringify(marker));
  }
};
const requireNoPlaceholderGlyphs = (rel) => {
  const text = read(rel);
  if (text.includes('>??<') || text.includes('{"??"}') || text.includes(">??")) failures.push(rel + ' contains placeholder glyphs (??)');
};
const requireMaxLines = (rel, max) => {
  const count = read(rel).split(/\r?\n/).length;
  if (count > max) failures.push(rel + ' has ' + count + ' lines; keep it under ' + max + ' and extract UI instead of growing it');
};

const appRel = 'guiapp/frontend/src/App.tsx';
const app = read(appRel);
const lines = app.split(/\r?\n/).length;

requireFile('guiapp/frontend/src/i18n/appTranslations.ts');
requireFile('docs/config-persistence-guardrails.md');
requireIncludes('docs/config-persistence-guardrails.md', 'Use `PatchConfig` or `PatchConfigFields` for small, local config changes.', 'config persistence guardrail guidance');
requireIncludes('docs/config-persistence-guardrails.md', '`SaveConfig` is reserved for full authoritative snapshots', 'SaveConfig authoritative snapshot guidance');
requireExcludes('guiapp/app_maclaw_llm.go', 'injectCodeGenModelIntoToolConfigs: SaveConfig failed', 'stale SaveConfig failure log in CodeGen SSO patch path');
requireExcludes('guiapp/app_project_search.go', 'LoadConfig -> merge -> SaveConfig', 'stale SaveConfig project-switch persistence comment');
requireExcludes('guiapp/app_project_search.go', 'switchCurrentProjectByPath: SaveConfig failed', 'stale SaveConfig project-switch failure log');
requireExcludes('guiapp/floating_windows.go', 'SaveConfig triggers floatingSoundChanged', 'stale SaveConfig floating sound comment');
requireExcludes('docs/project-switch-context-contamination-fix.md', 'LoadConfig -> merge -> SaveConfig', 'stale SaveConfig project-switch docs');
requireIncludes('guiapp/frontend/package.json', '--strict-mojibake && node scripts/check-main-ui-guards.mjs', 'frontend prebuild strict mojibake and UI guard gate');
requireIncludes('package.json', 'node scripts/check-main-ui-guards.mjs --self-test-config-persistence && node scripts/check-main-ui-guards.mjs', 'UI guard script runs config persistence self-test before normal guard');
requireFile('guiapp/frontend/src/config/providerCatalog.ts');
requireFile('guiapp/frontend/src/components/common/MarkdownLink.tsx');
requireFile('guiapp/frontend/src/components/tools/ToolConfiguration.tsx');
requireFile('guiapp/frontend/src/config/toolCatalog.ts');
requireFile('guiapp/frontend/src/config/apiStoreProviders.ts');
requireFile('guiapp/frontend/src/config/settingsTabs.ts');
requireFile('guiapp/frontend/src/types/appShell.ts');
requireFile('guiapp/frontend/src/appLazyComponents.ts');
requireFile('guiapp/frontend/src/components/settings/SettingsTabsRail.tsx');
requireFile('guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/UISettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/CodingKnowledgeSection.tsx');
requireFile('guiapp/frontend/src/components/settings/codingKnowledgeHelpers.ts');
requireFile('guiapp/frontend/src/components/settings/CodingKnowledgeDialogs.tsx');
requireFile('guiapp/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/systemSettingsDiagnostics.ts');
requireFile('guiapp/frontend/src/components/settings/SystemDiagnosticsTable.tsx');
requireFile('guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/ProxySettingsFields.tsx');
requireFile('guiapp/frontend/src/components/settings/proxySettingsHelpers.ts');
requireFile('guiapp/frontend/src/components/settings/ProxyScopeSettings.tsx');
requireFile('guiapp/frontend/src/components/settings/IMSettingsPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/IMSubTabs.tsx');
requireFile('guiapp/frontend/src/components/settings/ThirdPartyAccessSettings.tsx');
requireFile('guiapp/frontend/src/components/settings/QQBotSettings.tsx');
requireFile('guiapp/frontend/src/components/settings/TelegramBotSettings.tsx');
requireFile('guiapp/frontend/src/components/settings/WeixinSettings.tsx');
requireFile('guiapp/frontend/src/components/settings/WeixinQRLoginPanel.tsx');
requireFile('guiapp/frontend/src/components/settings/imSettingsShared.ts');
requireFile('guiapp/frontend/src/components/layout/AppSidebarShell.tsx');
requireFile('guiapp/frontend/src/components/layout/sidebarLayout.ts');
requireFile('guiapp/frontend/src/components/layout/SidebarNavRail.tsx');
requireFile('guiapp/frontend/src/components/layout/SidebarAiPane.tsx');
requireFile('guiapp/frontend/src/components/layout/SidebarToolSelector.tsx');
requireFile('guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx');
requireFile('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx');
requireFile('guiapp/frontend/src/components/layout/MainTopHeader.tsx');
requireFile('guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx');
requireFile('guiapp/frontend/src/components/layout/mainTopHeaderTitle.ts');
requireFile('guiapp/frontend/src/components/layout/AppStatusMessageBar.tsx');
requireFile('guiapp/frontend/src/components/pages/TutorialPage.tsx');
requireFile('guiapp/frontend/src/components/pages/ApiStorePage.tsx');
requireFile('guiapp/frontend/src/components/pages/ApiStoreProviderCard.tsx');
requireFile('guiapp/frontend/src/components/pages/ProjectManagerPage.tsx');
requireFile('guiapp/frontend/src/components/pages/ProjectManagerItem.tsx');
requireFile('guiapp/frontend/src/components/pages/RemoteSessionsPage.tsx');
requireFile('guiapp/frontend/src/components/pages/SkillsPage.tsx');
requireFile('guiapp/frontend/src/components/pages/MCPPage.tsx');
requireFile('guiapp/frontend/src/components/pages/GossipPage.tsx');
requireFile('guiapp/frontend/src/components/remote/LLMConfigOAuthFields.tsx');
requireFile('guiapp/frontend/src/components/remote/LLMConfigDialogSaveError.tsx');
requireFile('guiapp/frontend/src/components/AboutPanel.tsx');
requireFile('guiapp/frontend/src/components/MemoryHealthDialog.tsx');
requireFile('guiapp/frontend/src/components/SecurityEventsDialog.tsx');
requireFile('guiapp/frontend/src/components/modals/ThanksModal.tsx');
requireFile('guiapp/frontend/src/components/modals/ToolRepairProgressDialog.tsx');
requireFile('guiapp/frontend/src/components/modals/UpdateModal.tsx');
requireFile('guiapp/frontend/src/components/modals/InstallLogModal.tsx');
requireFile('guiapp/frontend/src/components/modals/ProjectProxySettingsDialog.tsx');
requireFile('guiapp/frontend/src/components/modals/InstallSkillModal.tsx');
requireFile('guiapp/frontend/src/components/modals/InstallSkillList.tsx');
requireFile('guiapp/frontend/src/components/modals/InstallLocationSelector.tsx');
requireFile('guiapp/frontend/src/components/modals/InstallSkillFooter.tsx');
requireFile('guiapp/frontend/src/components/modals/RemoteActivationDialog.tsx');
requireFile('guiapp/frontend/src/components/modals/ProviderSelectorDialog.tsx');
requireFile('guiapp/frontend/src/components/modals/ConfirmDialog.tsx');
requireFile('guiapp/frontend/src/components/ai/aiAssistantMarkdown.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantReplyCopyButton.tsx');
requireFile('guiapp/frontend/src/components/ai/aiAssistantPanelTheme.tsx');
requireFile('guiapp/frontend/src/components/ai/aiAssistantI18n.ts');
requireFile('guiapp/frontend/src/components/ai/ProjectSearchPanel.tsx');
requireFile('guiapp/frontend/src/components/ai/aiAssistantControls.tsx');
requireFile('guiapp/frontend/src/components/ai/useTTSReadback.ts');
requireFile('guiapp/frontend/src/components/ai/aiAssistantPanelTypes.ts');
requireFile('guiapp/frontend/src/components/ai/useAIAssistantVoiceControls.ts');
requireFile('guiapp/frontend/src/components/ai/useAssistantOutputScroll.ts');
requireFile('guiapp/frontend/src/components/ai/assistantOutputScrollLogic.ts');
requireFile('guiapp/frontend/src/components/ai/assistantOutputScrollFollow.ts');
requireFile('guiapp/frontend/src/components/ai/useAssistantThemeMode.ts');
requireFile('guiapp/frontend/src/components/ai/assistantThemeStorage.ts');
requireFile('guiapp/frontend/src/components/ai/useResizableAssistantInput.ts');
requireFile('guiapp/frontend/src/components/ai/useAssistantInputHistory.ts');
requireFile('guiapp/frontend/src/components/ai/usePastedImageAttachments.ts');
requireFile('guiapp/frontend/src/components/ai/useGroupDiscussionControls.ts');
requireFile('guiapp/frontend/src/components/ai/AssistantAttachmentsStrip.tsx');
requireFile('guiapp/frontend/src/components/ai/AttachmentImagePreview.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantPinnedNewsCards.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantConversationBody.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantInputActions.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantTitleBar.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantTitleBarNotifications.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx');
requireFile('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx');
requireFile('guiapp/frontend/src/components/ai/AIAssistantRenameGroupDialog.tsx');
requireFile('guiapp/frontend/src/components/ai/useAssistantPreviewResize.ts');
requireFile('guiapp/frontend/src/components/ai/aiAssistantStatusLabels.ts');

if (lines > 6800) failures.push(`${appRel} has ${lines} lines; keep it under 6800 and extract UI instead of growing it`);

const extractedFileLineLimits = [
  ['guiapp/frontend/src/components/layout/AppSidebarShell.tsx', 500],
  ['guiapp/frontend/src/components/layout/SidebarNavRail.tsx', 380],
  ['guiapp/frontend/src/components/layout/SidebarAiPane.tsx', 360],
  ['guiapp/frontend/src/components/layout/MainTopHeader.tsx', 240],
  ['guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx', 140],
  ['guiapp/frontend/src/components/layout/mainTopHeaderTitle.ts', 80],
  ['guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx', 330],
  ['guiapp/frontend/src/components/settings/UISettingsPanel.tsx', 340],
  ['guiapp/frontend/src/components/settings/CodingKnowledgeSection.tsx', 520],
  ['guiapp/frontend/src/components/settings/codingKnowledgeHelpers.ts', 80],
  ['guiapp/frontend/src/components/settings/CodingKnowledgeDialogs.tsx', 100],
  ['guiapp/frontend/src/components/settings/programmingToolsConfig.ts', 80],
  ['guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 180],
  ['guiapp/frontend/src/components/settings/systemSettingsDiagnostics.ts', 30],
  ['guiapp/frontend/src/components/settings/SystemDiagnosticsTable.tsx', 80],
  ['guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx', 160],
  ['guiapp/frontend/src/components/settings/ProxySettingsFields.tsx', 120],
  ['guiapp/frontend/src/components/settings/proxySettingsHelpers.ts', 40],
  ['guiapp/frontend/src/components/settings/ProxyScopeSettings.tsx', 100],
  ['guiapp/frontend/src/components/settings/IMSettingsPanel.tsx', 220],
  ['guiapp/frontend/src/components/settings/IMSubTabs.tsx', 100],
  ['guiapp/frontend/src/components/settings/WeixinSettings.tsx', 180],
  ['guiapp/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 220],
  ['guiapp/frontend/src/components/modals/InstallSkillModal.tsx', 220],
  ['guiapp/frontend/src/components/modals/InstallSkillList.tsx', 140],
  ['guiapp/frontend/src/components/modals/InstallLocationSelector.tsx', 140],
  ['guiapp/frontend/src/components/modals/InstallSkillFooter.tsx', 140],
  ['guiapp/frontend/src/components/pages/ProjectManagerPage.tsx', 130],
  ['guiapp/frontend/src/components/pages/ProjectManagerItem.tsx', 120],
  ['guiapp/frontend/src/components/pages/ApiStorePage.tsx', 120],
  ['guiapp/frontend/src/components/pages/ApiStoreProviderCard.tsx', 120],
  ['guiapp/frontend/src/config/apiStoreProviders.ts', 80],
  ['guiapp/frontend/src/components/AboutPanel.tsx', 1000],
  ['guiapp/frontend/src/components/MemoryHealthDialog.tsx', 200],
  ['guiapp/frontend/src/components/SecurityEventsDialog.tsx', 170],
  ['guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 6800],
  ['guiapp/frontend/src/components/ai/aiAssistantMarkdown.tsx', 2000],
  ['guiapp/frontend/src/components/ai/AssistantReplyCopyButton.tsx', 180],
  ['guiapp/frontend/src/components/ai/aiAssistantPanelTheme.tsx', 700],
  ['guiapp/frontend/src/components/ai/aiAssistantI18n.ts', 240],
  ['guiapp/frontend/src/components/ai/ProjectSearchPanel.tsx', 320],
  ['guiapp/frontend/src/components/ai/aiAssistantControls.tsx', 120],
  ['guiapp/frontend/src/components/ai/useTTSReadback.ts', 120],
  ['guiapp/frontend/src/components/ai/aiAssistantPanelTypes.ts', 220],
  ['guiapp/frontend/src/components/ai/useAIAssistantVoiceControls.ts', 100],
  ['guiapp/frontend/src/components/ai/useAssistantOutputScroll.ts', 240],
  ['guiapp/frontend/src/components/ai/assistantOutputScrollLogic.ts', 120],
  ['guiapp/frontend/src/components/ai/assistantOutputScrollFollow.ts', 60],
  ['guiapp/frontend/src/components/ai/useResizableAssistantInput.ts', 80],
  ['guiapp/frontend/src/components/ai/useAssistantInputHistory.ts', 100],
  ['guiapp/frontend/src/components/ai/usePastedImageAttachments.ts', 340],
  ['guiapp/frontend/src/components/ai/useGroupDiscussionControls.ts', 90],
  ['guiapp/frontend/src/components/ai/AssistantAttachmentsStrip.tsx', 280],
  ['guiapp/frontend/src/components/ai/AttachmentImagePreview.tsx', 340],
  ['guiapp/frontend/src/components/ai/AssistantPinnedNewsCards.tsx', 80],
  ['guiapp/frontend/src/components/ai/AssistantConversationBody.tsx', 250],
  ['guiapp/frontend/src/components/ai/AssistantInputActions.tsx', 520],
  ['guiapp/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 100],
  ['guiapp/frontend/src/components/ai/AssistantTitleBar.tsx', 220],
  ['guiapp/frontend/src/components/ai/AssistantTitleBarNotifications.tsx', 180],
  ['guiapp/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx', 50],
  ['guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 240],
  ['guiapp/frontend/src/components/ai/AIAssistantRenameGroupDialog.tsx', 110],
  ['guiapp/frontend/src/components/ai/useAssistantPreviewResize.ts', 50],
  ['guiapp/frontend/src/components/ai/aiAssistantStatusLabels.ts', 40],
];
for (const [rel, max] of extractedFileLineLimits) requireMaxLines(rel, max);

const highRiskRemoteFileLineLimits = [
  ['guiapp/frontend/src/components/remote/SkillsManagementPanel.tsx', 3000],
  // Implementation lives here; freeze growth until further extraction (entry is a thin re-export).
  ['guiapp/frontend/src/components/remote/SkillsManagementPanelView.tsx', 6600],
  ['guiapp/frontend/src/components/remote/OnboardingWizard.tsx', 2650],
  ['guiapp/frontend/src/components/remote/LLMConfigPanel.tsx', 1600],
  ['guiapp/frontend/src/components/remote/LLMConfigProviderLimitsFields.tsx', 90],
  ['guiapp/frontend/src/components/remote/LLMConfigDialogFooter.tsx', 80],
  ['guiapp/frontend/src/components/remote/LLMConfigDialogSaveError.tsx', 40],
  ['guiapp/frontend/src/components/remote/LLMConfigApiKeyFields.tsx', 80],
  ['guiapp/frontend/src/components/remote/LLMConfigOAuthFields.tsx', 120],
  ['guiapp/frontend/src/components/remote/MCPManagementPanel.tsx', 1400],
  ['guiapp/frontend/src/components/remote/MemoryManagementPanel.tsx', 1100],
];
for (const [rel, max] of highRiskRemoteFileLineLimits) requireMaxLines(rel, max);

const modalThemeFiles = [
  'guiapp/frontend/src/components/modals/InstallSkillModal.tsx',
  'guiapp/frontend/src/components/modals/ConfirmDialog.tsx',
  'guiapp/frontend/src/components/modals/UpdateModal.tsx',
];
for (const rel of modalThemeFiles) {
  for (const color of ['#ffffff', '#374151', '#6b7280', '#9ca3af', '#f9fafb', '#e5e7eb', '#e2e8f0', '#eef2ff', '#e0e7ff', '#4338ca']) {
    requireExcludes(rel, color, 'hard-coded modal color ' + color + '; use theme variables');
  }
}


if (app.charCodeAt(0) === 0xfeff) failures.push(`${appRel} starts with a UTF-8 BOM`);
if (app.includes('\ufffd')) failures.push(`${appRel} contains Unicode replacement characters`);

requireExcludes(appRel, 'const translations', 'inline translations; use i18n/appTranslations.ts');
requireExcludes(appRel, 'const knownProviderEndpoints', 'inline provider endpoint catalog; use config/providerCatalog.ts');
requireExcludes(appRel, 'const recommendedModels', 'inline recommended model catalog; use config/providerCatalog.ts');
requireExcludes(appRel, 'const ToolConfiguration =', 'inline ToolConfiguration; use components/tools/ToolConfiguration.tsx');
requireIncludes('guiapp/frontend/src/components/remote/OnboardingWizard.tsx', 'getOnboardingFlow({ brandId, freeTrial, offlineMode })', 'centralized onboarding flow');
requireExcludes('guiapp/frontend/src/components/remote/OnboardingWizard.tsx', 'brandId === \'qianxin\'', 'inline TigerClaw brand detection; use onboardingFlow.ts');
requireExcludes(appRel, 'const MarkdownLink =', 'inline MarkdownLink; use components/common/MarkdownLink.tsx');
requireExcludes(appRel, 'const TOOL_NAMES', 'inline tool tab catalog; use config/toolCatalog.ts');
requireExcludes(appRel, 'const SKILL_TOOLS', 'inline skill tool catalog; use config/toolCatalog.ts');
requireExcludes(appRel, 'const settingsTabOptions = [', 'inline settings tab registry; use config/settingsTabs.ts');
requireExcludes(appRel, 'settingsTabOptions.map', 'inline settings tab rail; use components/settings/SettingsTabsRail.tsx');
requireExcludes(appRel, 'llm_trajectory_logging', 'inline general settings panel; use components/settings/GeneralSettingsPanel.tsx');
requireExcludes(appRel, 'log_detail_enabled', 'inline general settings panel; use components/settings/GeneralSettingsPanel.tsx');
requireExcludes(appRel, 'SetUIZoomFactor', 'inline UI settings panel; use components/settings/UISettingsPanel.tsx');
requireExcludes(appRel, 'SetChatFontSize', 'inline UI settings panel; use components/settings/UISettingsPanel.tsx');
requireExcludes(appRel, 'default_tool_provider', 'inline programming tools settings panel; use components/settings/ProgrammingToolsSettingsPanel.tsx');
requireExcludes(appRel, 'checked={config?.show_ai_trace_entry', 'inline AI trace toggle; use components/settings/GeneralAdvancedSettingsPanel.tsx');
requireExcludes(appRel, 'SetEnvCheckInterval', 'inline advanced general settings panel; use components/settings/GeneralAdvancedSettingsPanel.tsx');
requireExcludes(appRel, 'remote_heartbeat_sec', 'inline system settings panel; use components/settings/SystemSettingsPanel.tsx');
requireExcludes(appRel, 'value={(config as any)?.audio_input_device_id', 'inline system audio device setting; use components/settings/SystemSettingsPanel.tsx');
requireExcludes(appRel, 'workstation_mode', 'inline workstation mode setting; use components/settings/SystemSettingsPanel.tsx');
requireExcludes(appRel, 'default_proxy_enabled', 'inline proxy settings panel; use components/settings/ProxySettingsPanel.tsx');
requireExcludes(appRel, 'SaveProxyConfig', 'inline proxy save wiring; use components/settings/ProxySettingsPanel.tsx');
requireExcludes(appRel, "setIMAuditPlatform('thirdparty')", 'inline third-party IM audit button; use components/settings/IMSettingsPanel.tsx');
requireExcludes(appRel, 'qqbot_app_secret', 'inline QQ bot settings; use components/settings/QQBotSettings.tsx');
requireExcludes(appRel, 'telegram_bot_token', 'inline Telegram bot settings; use components/settings/TelegramBotSettings.tsx');
requireExcludes(appRel, 'StartWeixinQRLogin', 'inline WeChat QR login settings; use components/settings/WeixinSettings.tsx');
requireExcludes(appRel, 'thirdparty_gateway_enabled', 'inline third-party IM gateway settings; use components/settings/IMSettingsPanel.tsx');
requireExcludes(appRel, 'className="sidebar"', 'inline left sidebar shell; use components/layout/AppSidebarShell.tsx');
requireIncludes('guiapp/frontend/src/components/layout/sidebarLayout.ts', 'SIDEBAR_NAV_RAIL_WIDTH = 60', 'narrow 5.10.x sidebar rail width guard');
requireExcludes('guiapp/frontend/src/components/layout/AppSidebarShell.tsx', "'90px'", 'hard-coded old sidebar rail width');
requireExcludes('guiapp/frontend/src/components/layout/SidebarNavRail.tsx', "width: '90px'", 'hard-coded old sidebar rail width');
requireIncludes('guiapp/frontend/src/App.tsx', 'className="global-action-bar" data-ai-theme={aiThemeMode}', 'dark themed global action bar');
// Viewport shell must carry theme attrs so --theme-page-bg is not the light
// :root default; otherwise dark mode shows a white frame around #App.
requireIncludes('guiapp/frontend/src/App.tsx', 'className="app-viewport"', 'app viewport shell');
requireIncludes('guiapp/frontend/src/App.tsx', 'data-ai-dark-scheme={aiThemeMode === \'dark\' ? aiDarkSchemeId : undefined}', 'app viewport dark theme attrs for page-bg shell');
requireIncludes('guiapp/frontend/src/App.css', ":where(#App, .app-viewport)[data-ai-theme='dark']", 'viewport/App dark theme rebinding (low-specificity :where so schemes win)');
requireIncludes('guiapp/frontend/src/App.css', "#App[data-maximized=\"true\"] {\n    border-radius: 0;\n    border: none;", 'maximized window drops CSS border (no edge frame)');
requireIncludes('guiapp/frontend/src/App.tsx', 'getAssistantDarkScheme(aiDarkSchemeId).cssVars.pageBg', 'document shell page-bg sync on theme change');
requireIncludes('guiapp/frontend/src/App.css', ".sidebar[data-ai-theme='dark'] {\n    --theme-primary", 'sidebar dark theme variables');
requireIncludes('guiapp/frontend/src/App.css', '--theme-page-bg: #0b1220;', 'sidebar dark theme page background');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'STATUS_DOT', 'system status decoded status dot');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'CREDIT_SEPARATOR', 'system status decoded credit separator');
requireExcludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', '>\\u', 'JSX unicode escape text that renders as code');
requireExcludes(appRel, 'taskItems.map', 'inline task management list; use components/layout/SidebarAiPane.tsx');
requireExcludes(appRel, 'className="top-header"', 'inline non-AI top header; use components/layout/MainTopHeader.tsx');
requireExcludes('guiapp/frontend/src/components/layout/MainTopHeader.tsx', 'ReadTutorial', 'inline top header actions; use components/layout/MainTopHeaderActions.tsx');
requireExcludes(appRel, 'className="status-message"', 'inline status message bar; use components/layout/AppStatusMessageBar.tsx');
requireExcludes(appRel, 'backgroundInstallStatus.startsWith', 'inline background install status; use components/layout/AppStatusMessageBar.tsx');
requireExcludes(appRel, "ChatFire', url:", 'inline API Store provider cards; use config/apiStoreProviders.ts');
requireExcludes('guiapp/frontend/src/components/pages/ApiStorePage.tsx', "ChatFire', url:", 'inline API Store provider cards; use config/apiStoreProviders.ts');
requireExcludes(appRel, 'key={refreshKey}', 'inline tutorial markdown page; use components/pages/TutorialPage.tsx');
requireExcludes(appRel, 'className="project-manager-panel"', 'inline project manager page; use components/pages/ProjectManagerPage.tsx');
requireExcludes(appRel, 'pagedProjects.map', 'inline project manager list; use components/pages/ProjectManagerPage.tsx');
requireExcludes('guiapp/frontend/src/components/pages/ProjectManagerPage.tsx', 'SelectProjectDir', 'inline project row actions; use components/pages/ProjectManagerItem.tsx');
requireExcludes(appRel, 'RemoteSessionList', 'inline remote sessions page; use components/pages/RemoteSessionsPage.tsx');
requireExcludes(appRel, 'SkillsManagementPanel', 'inline skills page; use components/pages/SkillsPage.tsx');
requireExcludes(appRel, 'MCPManagementPanel', 'inline MCP page; use components/pages/MCPPage.tsx');
requireExcludes(appRel, 'GossipPanel', 'inline gossip page; use components/pages/GossipPage.tsx');
requireExcludes(appRel, 'ReactMarkdown', 'inline thanks markdown modal; use components/modals/ThanksModal.tsx');
requireExcludes(appRel, 'startupTitle', 'inline startup popup; use components/modals/StartupPopup.tsx');
requireExcludes(appRel, 'brandDisplayTitle}</h2>', 'inline about page; use components/AboutPanel.tsx');
requireExcludes(appRel, 'toolRepairInstalling', 'inline tool repair progress dialog; use components/modals/ToolRepairProgressDialog.tsx');
requireExcludes(appRel, 'downloadAndUpdate', 'inline update modal; use components/modals/UpdateModal.tsx');
requireExcludes(appRel, 'installLogTitle', 'inline install log modal; use components/modals/InstallLogModal.tsx');
requireExcludes(appRel, 'useDefaultProxy', 'inline project proxy dialog; use components/modals/ProjectProxySettingsDialog.tsx');
requireExcludes(appRel, 'installDefaultMarketplace', 'inline install skill modal; use components/modals/InstallSkillModal.tsx');
requireExcludes(appRel, 'selectedSkillsToInstall.includes(skill.name)', 'inline install skill list; use components/modals/InstallSkillModal.tsx');
requireExcludes(appRel, 'remoteActivationDialogTitle', 'inline remote activation dialog; use components/modals/RemoteActivationDialog.tsx');
requireExcludes(appRel, 'remoteHubManualOrSelect', 'inline remote activation dialog body; use components/modals/RemoteActivationDialog.tsx');
requireExcludes(appRel, 'selectProviderTitle', 'inline provider selector dialog; use components/modals/ProviderSelectorDialog.tsx');
requireExcludes(appRel, 'getFilteredProviders().map', 'inline provider selector grid; use components/modals/ProviderSelectorDialog.tsx');
requireExcludes(appRel, 'stroke="#ef4444"', 'inline confirm dialog; use components/modals/ConfirmDialog.tsx');
requireExcludes(appRel, 'confirmDialog.message}</p>', 'inline confirm dialog body; use components/modals/ConfirmDialog.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'function renderContentWithCodeBlocks', 'inline AI markdown/code-block renderer; use components/ai/aiAssistantMarkdown.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'function renderMessage', 'inline AI message renderer; use components/ai/aiAssistantMarkdown.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'renderMessage } from "./aiAssistantMarkdown"', 'AI markdown renderer import');
requireIncludes('guiapp/frontend/src/components/ai/aiAssistantMarkdown.tsx', 'from "./AssistantReplyCopyButton"', 'AI reply copy button import');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'const lightTheme', 'inline AI panel theme; use components/ai/aiAssistantPanelTheme.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'function AssistantInputIcon', 'inline AI input icons; use components/ai/aiAssistantPanelTheme.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./aiAssistantPanelTheme"', 'AI panel theme import');
requireIncludes('guiapp/frontend/src/App.tsx', "from './components/ai/assistantThemeStorage'", 'App reads pure AI theme storage helper');
requireIncludes('guiapp/frontend/src/App.tsx', "themeMode={aiThemeMode}", 'App controls AI assistant theme mode across tab switches');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', "themeMode: controlledThemeMode", 'AI assistant accepts controlled theme mode');
requireIncludes('guiapp/frontend/src/components/ai/assistantThemeStorage.ts', "window.localStorage.setItem(AI_THEME_MODE_STORAGE_KEY, themeMode)", 'AI assistant persists shared theme mode');
requireIncludes('guiapp/frontend/src/components/ai/aiAssistantPanelTheme.tsx', "AI_THEME_MODE_LEGACY_STORAGE_KEY", 'AI assistant legacy theme key is centralized');
requireIncludes('guiapp/frontend/src/components/ai/assistantThemeStorage.ts', "AI_THEME_MODE_LEGACY_STORAGE_KEY", 'AI assistant reads/writes legacy theme key for compatibility');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', "from \"./useAssistantThemeMode\"", 'AI assistant theme hook import');
requireIncludes('guiapp/frontend/src/components/ai/useAssistantThemeMode.ts', "writeStoredAssistantThemeMode(themeMode)", 'AI theme hook delegates storage writes');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'interface ProjectSearchItem', 'inline AI project search model; use components/ai/ProjectSearchPanel.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'function useProjectSearch', 'inline AI project search hook; use components/ai/ProjectSearchPanel.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'function ProjectSearchPanel', 'inline AI project search panel; use components/ai/ProjectSearchPanel.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./ProjectSearchPanel"', 'AI project search import');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'function VoiceLevelVisualizer', 'inline AI voice level visualizer; use components/ai/aiAssistantControls.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'const miniActionButtonStyle', 'inline AI mini action button style; use components/ai/aiAssistantControls.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'GetTTSEnabled', 'inline AI TTS readback hook; use components/ai/useTTSReadback.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'EventsOn("tts:audio"', 'inline AI TTS audio listener; use components/ai/useTTSReadback.ts');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputActions.tsx', 'from "./aiAssistantControls"', 'AI controls import');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useTTSReadback"', 'AI TTS hook import');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'interface AIAssistantPanelStateProps', 'inline AI assistant panel props; use components/ai/aiAssistantPanelTypes.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'voiceHoldTimerRef', 'inline AI voice hold controls; use components/ai/useAIAssistantVoiceControls.ts');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./aiAssistantPanelTypes"', 'AI panel types import');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAIAssistantVoiceControls"', 'AI voice controls hook import');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'prevMsgCountRef', 'inline AI output scroll manager; use components/ai/useAssistantOutputScroll.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'scrollTimerRef', 'inline AI output scroll debounce; use components/ai/useAssistantOutputScroll.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'setInputAreaHeight', 'inline AI input resize state; use components/ai/useResizableAssistantInput.ts');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAssistantOutputScroll"', 'AI output scroll hook import');
requireIncludes('guiapp/frontend/src/components/ai/useAssistantOutputScroll.ts', 'from "./assistantOutputScrollLogic"', 'AI output scroll logic import');
requireIncludes('guiapp/frontend/src/components/ai/assistantOutputScrollLogic.ts', 'from "./assistantOutputScrollFollow"', 'AI output scroll follow import');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useResizableAssistantInput"', 'AI input resize hook import');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'historyEdits', 'inline AI input history edits; use components/ai/useAssistantInputHistory.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'SavePastedImage', 'inline pasted image saving; use components/ai/usePastedImageAttachments.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'setGroupDiscussionBusy', 'inline group discussion busy state; use components/ai/useGroupDiscussionControls.ts');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAssistantInputHistory"', 'AI input history hook import');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./usePastedImageAttachments"', 'AI pasted image hook import');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-pending-attachments"', 'inline AI pending attachments strip; use components/ai/AssistantAttachmentsStrip.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'from "./AssistantAttachmentsStrip"', 'AI attachments strip import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantAttachmentsStrip.tsx', 'title={att.filePath}', 'pasted image path tooltip');
requireIncludes('guiapp/frontend/src/components/ai/AssistantAttachmentsStrip.tsx', 'AttachmentImageThumbnail', 'pasted image thumbnail rendering');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-workflow-docs-bar"', 'old left-side AI workflow docs bar');
requireExcludes('guiapp/frontend/src/components/ai/AssistantInputStack.tsx', 'AssistantWorkflowDocsBar', 'old left-side workflow docs bar wiring');
requireIncludes('guiapp/frontend/src/components/ai/WorkflowDocPreview.tsx', 'WorkflowProgressBoard', 'right-side workflow progress board');
requireIncludes('guiapp/frontend/src/components/ai/WorkflowDocPreview.tsx', 'workflowPhaseOrders', 'workflow phase order map');
requireFile('guiapp/frontend/src/components/ai/workflowPhaseMeta.generated.ts');
requireFile('guiapp/frontend/src/components/ai/__tests__/workflowPhaseMeta.contract.test.ts');
requireIncludes('guiapp/frontend/src/components/ai/__tests__/workflowPhaseMeta.contract.test.ts', 'workflowPhaseMeta.generated', 'contract test imports the generated artifact');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'className="pinned-news-card"', 'inline pinned news cards; use components/ai/AssistantPinnedNewsCards.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AssistantConversationBody.tsx', 'from "./AssistantPinnedNewsCards"', 'AI pinned news cards import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantPinnedNewsCards.tsx', 'className="pinned-news-card"', 'pinned news card rendering');
requireIncludes('guiapp/frontend/src/components/ai/AssistantPinnedNewsCards.tsx', 'renderInlineMarkdown', 'pinned news markdown rendering');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'Setup not completed', 'inline AI conversation body; use components/ai/AssistantConversationBody.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./AssistantConversationBody"', 'AI conversation body import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantConversationBody.tsx', 'AssistantPinnedNewsCards', 'conversation body pinned news wiring');
requireIncludes('guiapp/frontend/src/components/ai/AssistantConversationBody.tsx', 'showProcessingState', 'conversation body busy state rendering');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-voice-input"', 'inline AI input action buttons; use components/ai/AssistantInputActions.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-cancel-progress"', 'inline AI cancel action button; use components/ai/AssistantInputActions.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'from "./AssistantInputActions"', 'AI input actions import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputActions.tsx', 'data-testid="ai-voice-input"', 'voice input button rendering');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputActions.tsx', 'VoiceLevelVisualizer', 'voice level visualizer rendering');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputActions.tsx', 'data-testid="ai-cancel-progress"', 'cancel progress button rendering');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'miniActionButtonStyle', 'inline group discussion action menu; use components/ai/AssistantGroupDiscussionMenu.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'Experts', 'inline group discussion stats; use components/ai/AssistantGroupDiscussionMenu.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'GD', 'group discussion titlebar button');
requireIncludes('guiapp/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'runGroupDiscussionAction("accept"', 'group discussion accept action');
requireIncludes('guiapp/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'runGroupDiscussionAction("publish"', 'group discussion publish action');
requireIncludes('guiapp/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'calc(100vw - 96px)', 'group discussion popup viewport fit');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-title-bar"', 'inline AI title bar; use components/ai/AssistantTitleBar.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'ai-titlebar-window-group', 'inline AI titlebar window controls; use components/ai/AssistantTitleBar.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./AssistantTitleBar"', 'AI title bar import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantTitleBar.tsx', 'from "./AssistantTitleBarNotifications"', 'AI title bar notifications import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-title-bar"', 'AI title bar wrapper');
requireIncludes('guiapp/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-titlebar-tools-group"', 'AI title bar tool group');
requireIncludes('guiapp/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-hide-toggle"', 'AI hide window control');
requireIncludes('guiapp/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-maximize-toggle"', 'AI maximize window control');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-workflow-maximize-suggestion"', 'inline workflow maximize suggestion; use components/ai/AssistantWorkflowMaximizeSuggestion.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./AssistantWorkflowMaximizeSuggestion"', 'AI workflow maximize suggestion import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx', 'data-testid="ai-workflow-maximize-suggestion"', 'workflow maximize suggestion wrapper');
requireIncludes('guiapp/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx', 'onToggleMaximize(); onDismiss();', 'workflow maximize action preserves dismiss');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-input"', 'inline AI input composer; use components/ai/AssistantInputComposer.tsx');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'rememberHistoryEdit(e.target.value)', 'inline AI input history handling; use components/ai/AssistantInputComposer.tsx');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputStack.tsx', 'from "./AssistantInputComposer"', 'AI input composer import');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'data-testid={textareaTestId}', 'AI input textarea');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'AssistantAttachmentsStrip', 'AI input attachments strip wiring');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'AssistantInputActionsLeft', 'AI input action buttons wiring');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'recallHistory("up")', 'AI input history up recall');
requireIncludes('guiapp/frontend/src/components/ai/AssistantInputComposer.tsx', 'handleSend();', 'AI input enter submit wiring');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'const initStatusLabels', 'inline AI init status labels; use components/ai/aiAssistantStatusLabels.ts');
requireExcludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'document.body.style.cursor = "col-resize"', 'inline AI preview resize hook; use components/ai/useAssistantPreviewResize.ts');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAssistantPreviewResize"', 'AI preview resize hook import');
requireIncludes('guiapp/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./aiAssistantStatusLabels"', 'AI init status labels import');
requireIncludes('guiapp/frontend/src/components/ai/useAssistantPreviewResize.ts', 'setSplitRatio(nextRatio)', 'AI preview resize ratio update');
requireIncludes('guiapp/frontend/src/components/ai/aiAssistantStatusLabels.ts', 'getAssistantInitLabel', 'AI init status label helper');

const criticalMarkers = [
  ['guiapp/frontend/src/App.tsx', 'AIAssistantPanel', 'AI assistant panel'],
  ['guiapp/frontend/src/App.tsx', 'IMAuditPanel', 'IM audit/watch panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'PetSettingsPanel', 'pet settings tab'],
  ['MCPPage', 'MCP main page'],
  ['GossipPage', 'gossip main page'],
  ['chatFontSize', 'AI assistant font-size setting'],
  ['taskManagementPaneWidth', 'resizable task management pane'],
  ['guiapp/frontend/src/App.tsx', 'getSettingsTabOptions', 'settings tab registry import'],
  ['guiapp/frontend/src/components/settings/SettingsPage.tsx', 'SettingsTabsRail', 'settings tabs rail'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'GeneralSettingsPanel', 'general settings panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'UISettingsPanel', 'UI settings panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'GeneralAdvancedSettingsPanel', 'advanced general settings panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'SystemSettingsPanel', 'system settings panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'ProxySettingsPanel', 'proxy settings panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'IMSettingsPanel', 'IM settings panel'],
  ['guiapp/frontend/src/components/settings/SettingsActiveContent.tsx', 'ProgrammingToolsSettingsPanel', 'built-in coding tools settings panel'],
  ['AppSidebarShell', 'left sidebar shell'],
  ['MainTopHeader', 'non-AI top header'],
  ['AppStatusMessageBar', 'status message bar'],
  ['TutorialPage', 'tutorial page'],
  ['ApiStorePage', 'API Store page'],
  ['ProjectManagerPage', 'project manager page'],
  ['RemoteSessionsPage', 'remote sessions page'],
  ['SkillsPage', 'skills page'],
  ['ThanksModal', 'thanks modal'],
  ['AboutPanel', 'about page'],
  ['ToolRepairProgressDialog', 'tool repair progress dialog'],
  ['UpdateModal', 'update modal'],
  ['InstallLogModal', 'install log modal'],
  ['ProjectProxySettingsDialog', 'project proxy settings dialog'],
  ['InstallSkillModal', 'install skill modal'],
  ['RemoteActivationDialog', 'remote activation dialog'],
  ['ProviderSelectorDialog', 'provider selector dialog'],
  ['ConfirmDialog', 'confirm dialog'],
];
for (const marker of criticalMarkers) {
  const [first, second, third] = marker;
  requireIncludes(third ? first : appRel, third ? second : first, third || second);
}

requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', 'export const translations', 'central translations export');
requireIncludes('guiapp/frontend/src/config/providerCatalog.ts', 'export const knownProviderEndpoints', 'provider endpoint export');
requireIncludes('guiapp/frontend/src/config/providerCatalog.ts', 'export const recommendedModels', 'recommended models export');
requireIncludes('guiapp/frontend/src/components/tools/ToolConfiguration.tsx', 'export const ToolConfiguration', 'ToolConfiguration export');
requireIncludes('guiapp/frontend/src/components/common/MarkdownLink.tsx', 'export const MarkdownLink', 'MarkdownLink export');
requireIncludes('guiapp/frontend/src/config/toolCatalog.ts', 'export const TOOL_NAMES', 'tool catalog export');
requireIncludes('guiapp/frontend/src/config/settingsTabs.ts', 'export const getSettingsTabOptions', 'settings tab registry export');
requireIncludes('guiapp/frontend/src/config/settingsTabs.ts', "id: 'pet'", 'pet settings tab registry entry');
requireIncludes('guiapp/frontend/src/config/settingsTabs.ts', "id: 'redeem'", 'service redeem settings tab registry entry');
requireIncludes('guiapp/frontend/src/components/remote/HubServiceRedeemPanel.tsx', 'authorizedModelsTableStyle', 'authorized models fixed table layout');
requireIncludes('guiapp/frontend/src/components/remote/HubServiceRedeemPanel.tsx', 'authorizedGroupTagStyle', 'authorized model service group tag layout');
requireIncludes('guiapp/frontend/src/config/settingsTabs.ts', "id: 'im'", 'IM settings tab registry entry');
requireIncludes('guiapp/frontend/src/components/settings/SettingsTabsRail.tsx', 'export const SettingsTabsRail', 'settings tabs rail export');
requireIncludes('guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'export const GeneralSettingsPanel', 'general settings export');
requireIncludes('guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'llm_trajectory_logging', 'general settings trajectory toggle');
requireIncludes('guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'log_detail_enabled', 'general settings detailed logs toggle');
requireIncludes('guiapp/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'SelectWorkingDir', 'general settings working directory picker');
requireIncludes('guiapp/frontend/src/components/settings/UISettingsPanel.tsx', 'export const UISettingsPanel', 'UI settings export');
requireIncludes('guiapp/frontend/src/components/settings/UISettingsPanel.tsx', 'SetUIZoomFactor', 'UI zoom persistence wiring');
requireIncludes('guiapp/frontend/src/components/settings/UISettingsPanel.tsx', 'SetChatFontSize', 'AI assistant font size persistence wiring');
requireExcludes('guiapp/frontend/src/App.tsx', 'ListToolProviders', 'removed default programming provider list wiring');
requireExcludes('guiapp/frontend/wailsjs/go/main/App.d.ts', 'ListToolProviders', 'removed default programming provider binding');
requireExcludes('guiapp/frontend/wailsjs/go/main/App.js', 'ListToolProviders', 'removed default programming provider binding');
requireIncludes('guiapp/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'export const GeneralAdvancedSettingsPanel', 'advanced general settings export');
requireIncludes('guiapp/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'show_ai_trace_entry', 'AI trace entry toggle');
requireIncludes('guiapp/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'SetEnvCheckInterval', 'environment check interval wiring');
requireIncludes('guiapp/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'use_windows_terminal', 'Windows Terminal toggle');
requireIncludes('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 'export const SystemSettingsPanel', 'system settings export');
requireIncludes('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 'remote_heartbeat_sec', 'system heartbeat setting');
requireIncludes('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 'workstation_mode', 'workstation mode toggle');
requireIncludes('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 'audio_input_device_id', 'audio input device setting');
requireIncludes('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 'SystemDiagnosticsTable', 'diagnostics table wiring');
requireIncludes('guiapp/frontend/src/components/settings/SystemSettingsPanel.tsx', 'buildSystemDiagnostics', 'system diagnostics helper wiring');
requireIncludes('guiapp/frontend/src/components/settings/SystemDiagnosticsTable.tsx', '<table', 'diagnostics table rendering');
requireIncludes('guiapp/frontend/src/components/settings/SystemDiagnosticsTable.tsx', 'var(--theme-surface-muted)', 'diagnostics table dark-mode surface');
requireIncludes('guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx', 'export const ProxySettingsPanel', 'proxy settings export');
requireIncludes('guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx', 'default_proxy_enabled', 'proxy enabled setting');
requireIncludes('guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx', 'SaveProxyConfig', 'proxy save backend wiring');
requireIncludes('guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx', 'ProxyScopeSettings', 'proxy scope settings wiring');
requireIncludes('guiapp/frontend/src/components/settings/ProxySettingsPanel.tsx', 'ProxySettingsFields', 'proxy form fields wiring');
requireIncludes('guiapp/frontend/src/components/settings/ProxyScopeSettings.tsx', 'default_proxy_scope_maclaw', 'proxy Maclaw scope setting');
requireIncludes('guiapp/frontend/src/components/settings/ProxyScopeSettings.tsx', 'default_proxy_scope_agent', 'proxy agent scope setting');
requireIncludes('guiapp/frontend/src/components/settings/IMSettingsPanel.tsx', 'export const IMSettingsPanel', 'IM settings export');
requireIncludes('guiapp/frontend/src/components/settings/IMSettingsPanel.tsx', 'IMSubTabs', 'IM sub-tabs wiring');
requireIncludes('guiapp/frontend/src/components/settings/IMSubTabs.tsx', 'export const IMSubTabs', 'IM sub-tabs export');
requireIncludes('guiapp/frontend/src/components/settings/IMSubTabs.tsx', "key: 'weixin'", 'WeChat tab before third-party access');
requireIncludes('guiapp/frontend/src/components/settings/IMSubTabs.tsx', "key: 'thirdparty'", 'third-party IM tab entry');
requireOrder('guiapp/frontend/src/components/settings/IMSubTabs.tsx', "key: 'weixin'", "key: 'thirdparty'", 'WeChat tab before third-party access');
requireIncludes('guiapp/frontend/src/components/settings/ThirdPartyAccessSettings.tsx', "setIMAuditPlatform('thirdparty')", 'third-party IM audit button');
requireIncludes('guiapp/frontend/src/components/settings/QQBotSettings.tsx', 'export const QQBotSettings', 'QQ bot settings export');
requireIncludes('guiapp/frontend/src/components/settings/QQBotSettings.tsx', 'qqbot_app_secret', 'QQ bot secret setting');
requireIncludes('guiapp/frontend/src/components/settings/QQBotSettings.tsx', 'RestartQQBot', 'QQ bot restart wiring');
requireIncludes('guiapp/frontend/src/components/settings/QQBotSettings.tsx', 'SetQQBotLocalMode', 'QQ bot local mode wiring');
requireIncludes('guiapp/frontend/src/components/settings/QQBotSettings.tsx', "setIMAuditPlatform('qq')", 'QQ bot audit/watch button');
requireIncludes('guiapp/frontend/src/components/settings/TelegramBotSettings.tsx', 'export const TelegramBotSettings', 'Telegram bot settings export');
requireIncludes('guiapp/frontend/src/components/settings/TelegramBotSettings.tsx', 'telegram_bot_token', 'Telegram token setting');
requireIncludes('guiapp/frontend/src/components/settings/TelegramBotSettings.tsx', 'RestartTelegram', 'Telegram restart wiring');
requireIncludes('guiapp/frontend/src/components/settings/TelegramBotSettings.tsx', 'SetTelegramLocalMode', 'Telegram local mode wiring');
requireIncludes('guiapp/frontend/src/components/settings/TelegramBotSettings.tsx', "setIMAuditPlatform('telegram')", 'Telegram audit/watch button');
requireIncludes('guiapp/frontend/src/components/settings/imSettingsShared.ts', 'export const localModeOptions', 'shared IM mode options');
for (const rel of [
  'guiapp/frontend/src/components/settings/IMSettingsPanel.tsx',
  'guiapp/frontend/src/components/settings/QQBotSettings.tsx',
  'guiapp/frontend/src/components/settings/TelegramBotSettings.tsx',
  'guiapp/frontend/src/components/settings/WeixinSettings.tsx',
  'guiapp/frontend/src/components/settings/WeixinQRLoginPanel.tsx',
  'guiapp/frontend/src/components/settings/ThirdPartyAccessSettings.tsx',
  'guiapp/frontend/src/components/settings/imSettingsShared.ts',
  'guiapp/frontend/src/components/layout/AppSidebarShell.tsx',
  'guiapp/frontend/src/components/layout/SidebarNavRail.tsx',
  'guiapp/frontend/src/components/layout/SidebarAiPane.tsx',
  'guiapp/frontend/src/components/layout/SidebarToolSelector.tsx',
  'guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx',
  'guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx',
  'guiapp/frontend/src/components/layout/MainTopHeader.tsx',
  'guiapp/frontend/src/components/layout/AppStatusMessageBar.tsx',
  'guiapp/frontend/src/components/modals/ThanksModal.tsx',
  'guiapp/frontend/src/components/modals/ToolRepairProgressDialog.tsx',
  'guiapp/frontend/src/components/modals/UpdateModal.tsx',
  'guiapp/frontend/src/components/modals/InstallLogModal.tsx',
  'guiapp/frontend/src/components/modals/ProjectProxySettingsDialog.tsx',
  'guiapp/frontend/src/components/modals/InstallSkillModal.tsx',
  'guiapp/frontend/src/components/modals/InstallSkillList.tsx',
  'guiapp/frontend/src/components/modals/InstallLocationSelector.tsx',
  'guiapp/frontend/src/components/modals/InstallSkillFooter.tsx',
  'guiapp/frontend/src/components/modals/RemoteActivationDialog.tsx',
  'guiapp/frontend/src/components/modals/ProviderSelectorDialog.tsx',
  'guiapp/frontend/src/components/modals/ConfirmDialog.tsx',
  'guiapp/frontend/src/components/ai/AIAssistantRenameGroupDialog.tsx',
  'guiapp/frontend/src/components/remote/HubServiceRedeemPanel.tsx',
]) {
  requireNoMojibake(rel);
  requireNoPlaceholderGlyphs(rel);
}
requireIncludes('guiapp/frontend/src/components/settings/ThirdPartyAccessSettings.tsx', 'thirdparty_gateway_enabled', 'third-party gateway toggle');
requireIncludes('guiapp/frontend/src/components/settings/WeixinSettings.tsx', 'export const WeixinSettings', 'WeChat settings export');
requireIncludes('guiapp/frontend/src/components/settings/WeixinSettings.tsx', 'WeixinQRLoginPanel', 'WeChat QR login panel wiring');
requireIncludes('guiapp/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 'StartWeixinQRLogin', 'WeChat QR login wiring');
requireIncludes('guiapp/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 'PollWeixinQRStatus', 'WeChat QR polling wiring');
requireIncludes('guiapp/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 'QRCodeSVG', 'WeChat QR code rendering');
requireIncludes('guiapp/frontend/src/components/settings/WeixinSettings.tsx', 'RestartWeixin', 'WeChat restart wiring');
requireIncludes('guiapp/frontend/src/components/settings/WeixinSettings.tsx', 'StopWeixin', 'WeChat disconnect wiring');
requireIncludes('guiapp/frontend/src/components/settings/WeixinSettings.tsx', 'SetWeixinLocalMode', 'WeChat local mode wiring');
requireIncludes('guiapp/frontend/src/components/settings/WeixinSettings.tsx', "setIMAuditPlatform('weixin')", 'WeChat audit/watch button');
requireIncludes('guiapp/frontend/src/components/settings/IMSettingsPanel.tsx', 'IMAuditPanel', 'IM audit panel rendering');
requireIncludes('guiapp/frontend/src/components/settings/ThirdPartyAccessSettings.tsx', 'RestartThirdPartyGateway', 'third-party gateway restart wiring');
for (const color of ['#555', '#888', '#ddd', '#6366f1', '#eef2ff', '#dcfce7', '#fee2e2', '#fef9c3']) {
  requireExcludes('guiapp/frontend/src/components/settings/IMSettingsPanel.tsx', color, `hard-coded ${color} in IM settings; use theme variables`);
}
requireIncludes('guiapp/frontend/src/components/layout/AppSidebarShell.tsx', 'export const AppSidebarShell', 'left sidebar shell export');
requireIncludes('guiapp/frontend/src/components/layout/AppSidebarShell.tsx', 'SidebarNavRail', 'left sidebar nav rail wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarNavRail.tsx', 'export const SidebarNavRail', 'sidebar nav rail export');
requireIncludes('guiapp/frontend/src/components/layout/SidebarNavRail.tsx', 'left-nav-item--ai', 'AI nav rail button');
requireIncludes('guiapp/frontend/src/components/layout/SidebarNavRail.tsx', 'runningTaskCount', 'monitor running task badge');
requireIncludes('guiapp/frontend/src/components/layout/SidebarAiPane.tsx', 'export const SidebarAiPane', 'sidebar AI pane export');
requireIncludes('guiapp/frontend/src/components/layout/SidebarAiPane.tsx', 'handleTaskManagementResizeStart', 'task management resize handle wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarToolSelector.tsx', 'export const SidebarToolSelector', 'sidebar tool selector export');
requireIncludes('guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx', 'export const SidebarTaskManagement', 'sidebar task management export');
requireIncludes('guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx', 'renameTask', 'task rename wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx', 'pinTask', 'task pin wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx', 'hideTask', 'task hide wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarToolSelector.tsx', 'Claude Code', 'Claude Code selector entry');
requireIncludes('guiapp/frontend/src/components/layout/SidebarToolSelector.tsx', 'CodeBuddy', 'CodeBuddy selector entry');
requireIncludes('guiapp/frontend/src/components/layout/SidebarToolSelector.tsx', 'Kilo Code', 'Kilo Code selector entry');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubTotalCredits', 'hub credits total display wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubUsedCredits', 'hub credits used display wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubExpiry', 'hub credits expiry display wiring');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'sidebarCurrentProviderTokenUsage.isHubService', 'hub service credits visibility condition');
requireIncludes('guiapp/frontend/src/components/layout/SidebarTaskManagement.tsx', 'visibleTasks.map', 'task list stays in sidebar task management component');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'showHubCreditAction', 'hub credits action stays in sidebar system status');
requireIncludes('guiapp/frontend/src/components/layout/SidebarSystemStatus.tsx', 'openHubCreditsPage', 'hub credits purchase action wiring');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeader.tsx', 'export const MainTopHeader', 'non-AI top header export');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeader.tsx', 'getHeaderTitle', 'top header title resolver wiring');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeader.tsx', 'MainTopHeaderActions', 'top header actions wiring');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx', 'ReadTutorial', 'top header tutorial refresh action');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx', 'setShowModelSettings(true)', 'top header provider config action');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx', 'setShowInstallSkillModal(true)', 'top header install skill action');
requireIncludes('guiapp/frontend/src/components/layout/mainTopHeaderTitle.ts', 'export const getHeaderTitle', 'top header title resolver export');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx', 'providerConfig', 'top header provider config label');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeader.tsx', 'handleWindowHide', 'minimize button wiring stays in top header');
requireIncludes('guiapp/frontend/src/components/layout/MainTopHeaderActions.tsx', 'setShowModelSettings(true)', 'provider config button stays in top header actions');
requireIncludes('guiapp/frontend/src/components/layout/AppStatusMessageBar.tsx', "'status-message'", 'status message bar wrapper');
requireIncludes('guiapp/frontend/src/components/layout/AppStatusMessageBar.tsx', 'backgroundInstallStatus', 'background install status display');
requireIncludes('guiapp/frontend/src/components/layout/AppStatusMessageBar.tsx', 'onOpenLLMSettings', 'LLM warning navigation');
requireIncludes('guiapp/frontend/src/components/pages/TutorialPage.tsx', 'ReactMarkdown', 'tutorial markdown rendering stays in TutorialPage');
requireIncludes('guiapp/frontend/src/components/pages/TutorialPage.tsx', 'components={{ a: MarkdownLink }}', 'tutorial markdown link handling');
requireIncludes('guiapp/frontend/src/components/pages/ApiStorePage.tsx', 'SetChatFontSize', 'API Store chat font size setting');
requireIncludes('guiapp/frontend/src/components/pages/ApiStorePage.tsx', 'ApiStoreProviderCard', 'API Store provider card wiring');
requireIncludes('guiapp/frontend/src/config/apiStoreProviders.ts', 'ChatFire', 'API Store ChatFire card');
requireIncludes('guiapp/frontend/src/config/apiStoreProviders.ts', '\\u667a\\u8c31', 'API Store Zhipu card');
requireIncludes('guiapp/frontend/src/config/apiStoreProviders.ts', '\\u963f\\u91cc\\u4e91', 'API Store Aliyun card');
requireIncludes('guiapp/frontend/src/components/pages/ApiStoreProviderCard.tsx', 'BrowserOpenURL', 'API Store external link action');
requireIncludes('guiapp/frontend/src/components/pages/ApiStoreProviderCard.tsx', 'var(--theme-surface)', 'API Store card theme-aware surface');
requireIncludes('guiapp/frontend/src/components/pages/ProjectManagerPage.tsx', 'export const ProjectManagerPage', 'project manager page export');
requireIncludes('guiapp/frontend/src/components/pages/ProjectManagerPage.tsx', 'ProjectManagerItem', 'project manager item wiring');
requireIncludes('guiapp/frontend/src/components/pages/ProjectManagerItem.tsx', 'SelectProjectDir', 'project path picker wiring');
requireIncludes('guiapp/frontend/src/components/pages/ProjectManagerItem.tsx', 'PatchConfigFields', 'project manager save wiring');
requireIncludes('guiapp/frontend/src/components/pages/ProjectManagerItem.tsx', 'var(--theme-surface-muted)', 'project path theme-aware background');
requireExcludes('guiapp/frontend/src/components/pages/ProjectManagerItem.tsx', 'SaveConfig', 'project manager full-config save; use PatchConfigFields');
requireIncludes('guiapp/frontend/src/components/pages/RemoteSessionsPage.tsx', 'RemoteSessionList', 'remote session list stays in RemoteSessionsPage');
requireIncludes('guiapp/frontend/src/components/pages/SkillsPage.tsx', 'SkillsManagementPanel', 'skills management stays in SkillsPage');
requireIncludes('guiapp/frontend/src/components/pages/MCPPage.tsx', 'MCPManagementPanel', 'MCP management stays in MCPPage');
requireIncludes('guiapp/frontend/src/components/pages/GossipPage.tsx', 'GossipPanel', 'gossip panel stays in GossipPage');
requireIncludes('guiapp/frontend/src/App.tsx', 'onCheckUpdate={() => {', 'about page update check wiring');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'officialWebsite', 'about page website button');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'quickActionsTitle', 'about page localized quick actions title');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'buildNumber', 'about page build number prop usage');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'bugReport', 'about page bug report link');
requireIncludes('guiapp/frontend/src/App.tsx', 'MACLAW_CODE_REPOSITORY_URL = "https://github.com/rapidai/maclaw"', 'about page fixed code repository URL');
requireIncludes('guiapp/frontend/src/App.tsx', 'onOpenGithub={() => BrowserOpenURL(MACLAW_CODE_REPOSITORY_URL)}', 'about page code repository button uses fixed URL');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'MemoryHealthDialog', 'about page memory health dialog');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'SecurityEventsDialog', 'about page security events dialog');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'ReadErrorLog', 'about page error log backend wiring');
requireIncludes('guiapp/frontend/src/components/AboutPanel.tsx', 'ReactMarkdown', 'about page thanks markdown rendering');
requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', '"aboutProductName"', 'about page product name translation');
requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', '"quickActionsTitle"', 'about page quick actions title translation');
requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', '"errorLog"', 'about page error log translation');
requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', '"codeRepository"', 'about page code repository translation');
requireIncludes('guiapp/frontend/src/components/MemoryHealthDialog.tsx', 'GetMemoryHealth', 'memory health backend wiring');
requireIncludes('guiapp/frontend/src/components/SecurityEventsDialog.tsx', 'QuerySecurityEvents', 'security events backend wiring');
requireIncludes('guiapp/frontend/src/components/remote/onboardingFlow.ts', "['sso', 'wechat']", 'TigerClaw onboarding stays two steps');
requireIncludes('guiapp/frontend/src/components/remote/OnboardingWizard.tsx', "isCurrentOnboardingStep(onboardingFlow, step, 'sso')", 'TigerClaw SSO first step uses centralized flow');
requireIncludes('guiapp/frontend/src/components/remote/onboardingFlow.ts', "['register', 'wechat']", 'free trial skips LLM setup in centralized flow');
requireIncludes('guiapp/frontend/src/components/remote/__tests__/onboardingFlow.test.ts', 'keeps standard free trial to register plus WeChat', 'free trial onboarding flow regression test');
requireIncludes('guiapp/frontend/src/components/remote/__tests__/OnboardingWizard.test.tsx', 'keeps TigerClaw onboarding to SSO plus WeChat without LLM setup', 'TigerClaw onboarding regression test');
requireIncludes('guiapp/frontend/src/components/SecurityEventsDialog.tsx', "t('securityEventsDeniedSummary')", 'security events summary localization');
requireIncludes('guiapp/frontend/src/components/SecurityEventsDialog.tsx', "t('securityEventsTime')", 'security events table header localization');
requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', '"securityEventsDeniedSummary"', 'security events summary translations');
requireIncludes('guiapp/frontend/src/i18n/appTranslations.ts', '"securityRiskCritical"', 'security event risk translations');
requireIncludes('guiapp/frontend/src/components/modals/ThanksModal.tsx', 'ReactMarkdown', 'thanks modal markdown rendering');
requireIncludes('guiapp/frontend/src/components/modals/ThanksModal.tsx', 'components={{ a: MarkdownLink }}', 'thanks modal markdown links');
requireIncludes('guiapp/frontend/src/components/modals/ToolRepairProgressDialog.tsx', 'toolRepairInstalling', 'tool repair installing message');
requireIncludes('guiapp/frontend/src/components/modals/ToolRepairProgressDialog.tsx', 'status.message', 'tool repair failure details');
requireIncludes('guiapp/frontend/src/components/modals/UpdateModal.tsx', 'downloadAndUpdate', 'update modal download action');
requireIncludes('guiapp/frontend/src/components/modals/UpdateModal.tsx', 'cancelDownload', 'update modal cancel download action');
requireIncludes('guiapp/frontend/src/components/modals/UpdateModal.tsx', 'var(--theme-info-bg)', 'update modal theme-aware info panel');
requireIncludes('guiapp/frontend/src/components/modals/UpdateModal.tsx', '\\u2714\\uFE0F', 'update modal latest-version icon');
requireIncludes('guiapp/frontend/src/components/modals/InstallLogModal.tsx', 'installLogTitle', 'install log title');
requireIncludes('guiapp/frontend/src/components/modals/InstallLogModal.tsx', 'navigator.clipboard.writeText', 'install log copy action');
requireIncludes('guiapp/frontend/src/components/modals/InstallLogModal.tsx', 'onSendLog(hasError)', 'install log send action');
requireIncludes('guiapp/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'proxyHostPlaceholder', 'project proxy host input');
requireIncludes('guiapp/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'useDefaultProxy', 'project proxy default toggle');
requireIncludes('guiapp/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'PatchConfigFields({ projects: newConfig.projects })', 'project proxy save wiring');
requireExcludes('guiapp/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'SaveConfig', 'project proxy full-config save; use PatchConfigFields');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillModal.tsx', 'InstallSkill', 'install skill action');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillFooter.tsx', 'InstallDefaultMarketplace', 'install default marketplace action');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillModal.tsx', 'skillZipOnlyError', 'skill compatibility error');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillModal.tsx', 'var(--theme-success)', 'install skill modal theme-aware title');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillModal.tsx', 'var(--theme-primary)', 'install skill modal theme-aware skills link');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillFooter.tsx', 'export const InstallSkillFooter', 'install skill footer export');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillFooter.tsx', 'onInstallSelected', 'install selected footer action wiring');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillFooter.tsx', 'isMarketplaceInstalling', 'marketplace install loading state');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillFooter.tsx', 'var(--theme-success)', 'install footer theme-aware success color');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillList.tsx', 'export const InstallSkillList', 'install skill list export');
requireIncludes('guiapp/frontend/src/components/modals/InstallLocationSelector.tsx', 'export const InstallLocationSelector', 'install location selector export');
requireIncludes('guiapp/frontend/src/components/modals/InstallLocationSelector.tsx', 'setInstallLocation', 'install location update wiring');
requireIncludes('guiapp/frontend/src/components/modals/InstallLocationSelector.tsx', 'setInstallProject', 'install project update wiring');
requireIncludes('guiapp/frontend/src/components/PetSettingsPanel.tsx', 'patchConfig(patch)', 'pet settings atomic patch save wiring');
requireExcludes('guiapp/frontend/src/components/PetSettingsPanel.tsx', 'saveConfig', 'pet settings full-config save prop; use patchConfig');
requireIncludes('guiapp/frontend/src/App.tsx', 'PatchConfigFields(patch)', 'model settings atomic patch save stays explicit');
requireIncludes('guiapp/app_asr.go', 'PatchConfigFields(map[string]interface{}{"asr_enabled": enabled})', 'ASR enabled setter uses atomic config patch');
requireExcludes('guiapp/app_asr.go', 'a.SaveConfig(cfg)', 'ASR setter full-config save; use PatchConfigFields');
requireIncludes('guiapp/app_tts.go', 'PatchConfigFields(map[string]interface{}{"tts_enabled": enabled})', 'TTS enabled setter uses atomic config patch');
requireIncludes('guiapp/app_tts.go', 'PatchConfigFields(map[string]interface{}{"tts_voice_id": voiceID})', 'TTS voice setter uses atomic config patch');
requireExcludes('guiapp/app_tts.go', 'a.SaveConfig(cfg)', 'TTS setter full-config save; use PatchConfigFields');
requireIncludes('guiapp/app_websearch.go', 'return a.PatchConfig(func(cfg *corelib.AppConfig) {', 'web search provider save uses atomic config patch');
requireExcludes('guiapp/app_websearch.go', 'a.SaveConfig(cfg)', 'web search provider full-config save; use PatchConfig');
requireIncludes('guiapp/app_mis_data.go', 'return a.PatchConfig(func(cfg *corelib.AppConfig) {', 'MIS data settings save uses atomic config patch');
requireExcludes('guiapp/app_mis_data.go', 'a.SaveConfig(cfg)', 'MIS data full-config save; use PatchConfig');
requireIncludes('guiapp/app_wails_bindings.go', 'cfg.MemoryAutoCompress = enabled', 'memory auto-compress config write remains explicit');
requireIncludes('guiapp/app_wails_bindings.go', 'return a.PatchConfig(func(cfg *corelib.AppConfig) {', 'memory auto-compress save uses atomic config patch');
requireIncludes('guiapp/app_yolo_model.go', 'PatchConfigFields(map[string]interface{}{"screen_parsing_enabled": enabled})', 'screen parsing setter uses atomic config patch');
requireExcludes('guiapp/app_yolo_model.go', 'a.SaveConfig(cfg)', 'screen parsing setter full-config save; use PatchConfigFields');
requireIncludes('guiapp/env_check_api.go', 'PatchConfigFields(map[string]interface{}{"env_check_interval": days})', 'environment interval setter uses atomic config patch');
requireIncludes('guiapp/env_check_api.go', 'PatchConfigFields(map[string]interface{}{"last_env_check_time": time.Now().Format(time.RFC3339)})', 'environment check timestamp uses atomic config patch');
requireExcludes('guiapp/env_check_api.go', 'a.SaveConfig(config)', 'environment check full-config save; use PatchConfigFields');
requireIncludes('guiapp/app_nl_mcp.go', 'cfg.MCPServers = servers', 'remote MCP server save remains explicit');
requireIncludes('guiapp/app_nl_mcp.go', 'cfg.LocalMCPServers = servers', 'local MCP server save remains explicit');
requireIncludes('guiapp/app_nl_skills.go', 'cfg.ExternalSkillDirs = nextDirs', 'external skill dir add uses atomic config patch');
requireIncludes('guiapp/app_nl_skills.go', 'cfg.ExternalSkillDirs = filtered', 'external skill dir remove uses atomic config patch');
requireIncludes('guiapp/app_ve.go', 'cfg.VEAllowedDirectories = nextDirs', 'VE allowed directories save uses atomic config patch');
requireIncludes('guiapp/app_wails_bindings.go', 'cfg.MemoryMaxBackups = n', 'memory max backups save uses atomic config patch');
requireIncludes('guiapp/qqbot_gateway.go', 'cfg.SetQQBotLocal(enabled)', 'QQ bot local mode uses atomic config patch');
requireIncludes('guiapp/telegram_gateway.go', 'cfg.SetTelegramLocal(enabled)', 'Telegram local mode uses atomic config patch');
requireIncludes('guiapp/weixin_gateway.go', 'cfg.SetWeixinLocal(enabled)', 'Weixin local mode uses atomic config patch');
requireIncludes('guiapp/lansenger_gateway.go', 'cfg.SetLansengerLocal(enabled)', 'Lansenger local mode uses atomic config patch');
requireIncludes('guiapp/thirdparty_gateway.go', 'cfg.SetThirdPartyGatewayLocal(enabled)', 'third-party gateway local mode uses atomic config patch');
requireIncludes('guiapp/config_manager.go', 'm.app.PatchConfig(func(cfg *corelib.AppConfig) {', 'config manager updates use atomic config patch');
requireIncludes('guiapp/hub_update_cache.go', 'cfg.RemoteHubCenterURLs = discovered', 'hub center URL cache uses atomic config patch');
requireIncludes('guiapp/floating_assistant.go', 'config.FloatingBtnPositionSet = true', 'floating button position uses atomic config patch');
requireIncludes('guiapp/floating_assistant.go', 'PatchConfigFields(map[string]interface{}{"pet_enabled": false})', 'floating assistant disable uses pet atomic patch');
requireIncludes('guiapp/floating_windows.go', 'PatchConfigFields(map[string]interface{}{"pet_motion_sound_enabled": newEnabled})', 'floating window pet sound menu uses atomic config patch');
requireIncludes('guiapp/app_project_search.go', 'PatchConfigFields(map[string]interface{}{"current_project": p.Id})', 'project search current-project switch uses atomic config patch');
requireIncludes('guiapp/app_data_migration.go', 'PatchConfig(func(cfg *corelib.AppConfig) { cfg.DataDir = newDir })', 'data directory save uses atomic config patch');
requireExcludes('guiapp/app_data_migration.go', 'a.SaveConfig(config)', 'data directory full-config save; use PatchConfig');
requireIncludes('guiapp/remote_smoke.go', 'app.PatchConfig(func(cfg *corelib.AppConfig) {', 'remote smoke config overrides use atomic config patch');
requireExcludes('guiapp/remote_smoke.go', 'app.SaveConfig(cfg)', 'remote smoke full-config save; use PatchConfig');
requireIncludes('guiapp/im_tools_misc.go', 'cfg.RemoteNickname = nickname', 'nickname setter persists only nickname');
requireIncludes('guiapp/im_tools_misc.go', 'h.app.PatchConfig(func(cfg *corelib.AppConfig) {', 'nickname setter uses atomic config patch');
requireExcludes('guiapp/im_tools_misc.go', 'h.saveConfig(cfg)', 'nickname full-config save; use PatchConfig');
requireIncludes('guiapp/app_nl_skills.go', 'cfg.NLSkills = filtered', 'skill executor save uses atomic config patch');
requireIncludes('guiapp/weixin_gateway.go', 'saveWeixinLoginConfig(result)', 'Weixin QR login saves through atomic config patch helper');
requireIncludes('guiapp/weixin_gateway.go', 'cfg.WeixinToken = result.BotToken', 'Weixin login patch persists token');
requireIncludes('guiapp/app_maclaw_llm.go', 'a.PatchConfig(func(cfg *corelib.AppConfig) {', 'LLM token usage updates use atomic config patch');
requireIncludes('guiapp/app_maclaw_llm.go', 'delta.LocalCacheRequests++', 'LLM local cache usage counter preserved');
requireIncludes('guiapp/app_maclaw_llm.go', 'delete(cfg.LLMTokenUsage, provider)', 'LLM token reset patches usage map');
requireIncludes('guiapp/app_maclaw_llm.go', 'currentCfg.MaclawLLMProviders = cfg.MaclawLLMProviders', 'Maclaw LLM provider save uses atomic config patch');
requireIncludes('guiapp/app_maclaw_llm.go', 'PatchConfigFields(map[string]interface{}{"remote_email": result.Email})', 'CodeGen SSO email backfill uses atomic config patch');
requireIncludes('guiapp/hub_llm_service.go', 'syncHubLLMServiceStatusToConfig(status, false)', 'Hub LLM status refresh uses atomic config patch');
requireIncludes('guiapp/hub_llm_service.go', 'syncHubLLMServiceStatusToConfig(serviceStatus, false)', 'Hub LLM redemption uses atomic config patch');
requireExcludes('guiapp/hub_llm_service.go', 'a.SaveConfig(', 'Hub LLM service full-config save; use PatchConfig');
requireIncludes('guiapp/tui_mode.go', 'a.app.PatchConfig(func(cfg *corelib.AppConfig) {', 'TUI single-field config save uses atomic config patch');
requireIncludes('guiapp/platform_windows.go', 'PatchConfigFields(map[string]interface{}{"env_check_done": true, "pause_env_check": true})', 'Windows env-check completion uses atomic config patch');
requireIncludes('guiapp/platform_linux.go', 'PatchConfigFields(map[string]interface{}{"env_check_done": true, "pause_env_check": true})', 'Linux env-check completion uses atomic config patch');
requireIncludes('guiapp/platform_darwin.go', 'PatchConfigFields(map[string]interface{}{"env_check_done": true, "pause_env_check": true})', 'Darwin env-check completion uses atomic config patch');
requireIncludes('guiapp/frontend/src/components/modals/InstallLocationSelector.tsx', 'var(--theme-surface)', 'install location theme-aware surface');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillList.tsx', 'selectedSkillsToInstall.includes(skill.name)', 'install skill selection state');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillList.tsx', 'setSelectedSkillsToInstall', 'install skill selection update');
requireIncludes('guiapp/frontend/src/components/modals/InstallSkillList.tsx', 'var(--theme-surface)', 'install skill list theme-aware surface');
requireIncludes('guiapp/frontend/src/components/modals/RemoteActivationDialog.tsx', 'remoteActivationDialogTitle', 'remote activation dialog title');
requireIncludes('guiapp/frontend/src/components/modals/RemoteActivationDialog.tsx', 'remoteLoadRegisteredHubs', 'remote activation hub loading');
requireIncludes('guiapp/frontend/src/components/modals/RemoteActivationDialog.tsx', 'remoteActivateAndLaunch', 'remote activation launch action');
requireIncludes('guiapp/frontend/src/components/modals/ProviderSelectorDialog.tsx', 'selectProviderTitle', 'provider selector title');
requireIncludes('guiapp/frontend/src/components/modals/ProviderSelectorDialog.tsx', 'providers.map', 'provider selector grid');
requireIncludes('guiapp/frontend/src/components/modals/ProviderSelectorDialog.tsx', 'hoveredProvider.provider.url', 'provider selector tooltip');
requireIncludes('guiapp/frontend/src/components/modals/ConfirmDialog.tsx', 'stroke="#ef4444"', 'confirm dialog icon');
requireIncludes('guiapp/frontend/src/components/modals/ConfirmDialog.tsx', 'var(--theme-surface)', 'confirm dialog theme-aware surface');
requireIncludes('guiapp/frontend/src/components/modals/ConfirmDialog.tsx', 'var(--theme-text-primary)', 'confirm dialog theme-aware text');
requireIncludes('guiapp/frontend/src/components/modals/ConfirmDialog.tsx', 'onConfirm', 'confirm dialog confirm action');
requireIncludes('guiapp/frontend/src/components/modals/ConfirmDialog.tsx', 'onCancel', 'confirm dialog cancel action');
requireSaveConfigAllowlist();
requirePatchConfigFieldsSupported();
requireDynamicPatchConfigFieldsAllowlist();

if (failures.length) {
  console.error('Main UI guard check failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Main UI guard check passed (${lines} App.tsx lines).`);
