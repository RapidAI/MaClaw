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
      if (rel === 'gui/frontend/dist' || rel === 'gui/frontend/node_modules') continue;
      walkFiles(rel, out);
    }
    else out.push(rel);
  }
  return out;
};
const saveConfigAllowedSnippets = [
  ['gui/app.go', 'func (a *App) SaveConfig(config corelib.AppConfig) error', 'SaveConfig implementation'],
  ['gui/config_manager.go', 'm.app.SaveConfig(mergedCfg)', 'config import owns a full merged config snapshot'],
  ['gui/app_user_data_migration.go', 'return a.SaveConfig(cfg)', 'user data migration restores a validated full configuration snapshot'],
  ['gui/frontend/src/App.tsx', 'PatchConfigFields(patch)', 'model settings save uses atomic patch to avoid TOCTOU race'],
  ['gui/frontend/wailsjs/go/main/App.d.ts', 'export function SaveConfig', 'generated Wails binding declaration'],
  ['gui/frontend/wailsjs/go/main/App.js', 'export function SaveConfig', 'generated Wails binding implementation'],
  ['gui/frontend/wailsjs/go/main/App.js', "window['go']['main']['App']['SaveConfig'](arg1)", 'generated Wails binding forwards to backend SaveConfig'],
  ['gui/frontend/src/components/remote/useRemotePanel.ts', 'SaveConfig({ ...config, field: value })', 'comment documenting the stale snapshot bug pattern'],
  ['gui/tui_mode.go', 'a.app.SaveConfig(cfg)', 'TUI HasConfig path carries a full config snapshot'],
  ['gui/app_moa_config.go', 'a.SaveConfig(cfg)', 'MoA validation persists the complete normalized MoA configuration snapshot'],
  
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
  failures.push(...collectSaveConfigAllowlistFailures(walkFiles('gui'), read));
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
  const body = patchConfigFieldsFunctionBody(readFile('gui/app.go'));
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
    return ['gui/app.go PatchConfigFields has no discoverable supported field cases'];
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
  failures.push(...collectPatchConfigFieldFailures(walkFiles('gui'), read));
};

const patchConfigFieldsDynamicAllowedSnippets = [
  ['gui/app_proxy.go', 'a.PatchConfigFields(patch)', 'proxy setter builds a closed patch map from validated proxy option keys'],
  ['gui/frontend/src/App.tsx', 'PatchConfigFields(patch)).then((saved)', 'model settings panel builds a closed tool-config patch from TOOL_NAMES loop'],
  ['gui/frontend/src/App.tsx', 'PatchConfigFields(patch)', 'settings panels pass locally typed patch objects through the settings shell'],
  ['gui/frontend/src/App.tsx', '// Using PatchConfigFields (atomic load', 'comment explaining atomic patch mechanism'],
  ['gui/frontend/src/components/remote/useRemotePanel.ts', 'PatchConfigFields(patch).then((saved)', 'remote panel saveConfigPatch helper receives patches built by local typed setters'],
  ['gui/frontend/src/components/remote/useRemotePanel.ts', 'PatchConfigFields(patchWithLaunchMode as Record<string, any>)', 'remote quick-start augments a locally built patch with default_launch_mode'],
  ['gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'PatchConfigFields(patch)).then((saved)', 'general settings helper receives patches from same-file controls only'],
  ['gui/frontend/src/components/ai/AssistantQuickSettingsBar.tsx', 'PatchConfigFields({ [field]: next } as Record<string, any>)', 'quick settings toggles choose from a closed workstation/log-detail field union'],
  ['gui/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'PatchConfigFields(patch).then((saved)', 'advanced settings helper receives patches from same-file controls only'],
  ['gui/frontend/src/components/settings/programmingToolsConfig.ts', 'PatchConfigFields(patch).then((saved)', 'shared programming tools patch helper receives patches from panel controls only'],
  ['gui/remote_activation.go', 'a.PatchConfigFields(patch)', 'remote registration persists a closed patch map containing only normalized remote_email or remote_mobile'],
  ['gui/frontend/src/components/settings/ModelRoutesSettingsSection.tsx', 'PatchConfigFields({ model_routes })', 'model routes panel persists its closed model_routes patch'],
  ['gui/openhuman_wiring.go', 'PatchConfigFields(model_routes)', 'model router reload documents the closed model_routes patch'],
  ['gui/computer_use_warmup.go', 'a.PatchConfigFields(patch)', 'computer-use log prune policy patch is assembled from validated keep/max-age fields and optional auto-prune toggle'],
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
    if (rel.startsWith('gui/frontend/wailsjs/')) continue;
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
  failures.push(...collectDynamicPatchConfigFieldFailures(walkFiles('gui'), read));
};

if (selfTestConfigPersistence) {
  const files = ['gui/app.go', 'gui/new_partial_save.go', 'gui/spaced_partial_save.go', 'gui/optional_partial.go', 'gui/aliased_partial_save.go', 'gui/frontend/src/aliased.ts', 'gui/frontend/src/generated_partial.js', 'gui/frontend/src/optional_partial.ts', 'gui/frontend/src/dot_alias.ts', 'gui/frontend/src/destructure_plain_alias.ts', 'gui/frontend/src/bracket_partial.js', 'gui/frontend/src/bracket_optional_partial.js', 'gui/frontend/src/bracket_alias.js', 'gui/frontend/src/deep_bracket_alias.js', 'gui/frontend/src/destructure_alias.ts'];
  const contents = {
    'gui/app.go': 'func (a *App) SaveConfig(config corelib.AppConfig) error { return nil }',
    'gui/new_partial_save.go': 'func bad(a *App, cfg corelib.AppConfig) { _ = a.SaveConfig(cfg) }',
    'gui/spaced_partial_save.go': 'func bad(a *App, cfg corelib.AppConfig) { _ = a.SaveConfig (cfg) }',
    'gui/optional_partial.go': 'func bad(a *App, cfg corelib.AppConfig) { _ = a.SaveConfig?.(cfg) }',
    'gui/aliased_partial_save.go': 'func bad(a *App) { save := a.SaveConfig; _ = save }',
    'gui/frontend/src/aliased.ts': 'import { SaveConfig as saveConfig } from "../wailsjs/go/main/App";',
    'gui/frontend/src/generated_partial.js': 'export function bad(cfg) { return SaveConfig(cfg); }',
    'gui/frontend/src/optional_partial.ts': 'export function bad(cfg: any) { return SaveConfig?.(cfg); }',
    'gui/frontend/src/dot_alias.ts': 'const save = app.SaveConfig; export function bad(cfg: any) { return save(cfg); }',
    'gui/frontend/src/destructure_plain_alias.ts': 'const { SaveConfig } = app; export function bad(cfg: any) { return SaveConfig; }',
    'gui/frontend/src/bracket_partial.js': 'export function bad(app, cfg) { return app["SaveConfig"](cfg); }',
    'gui/frontend/src/bracket_optional_partial.js': 'export function bad(app, cfg) { return app["SaveConfig"]?.(cfg); }',
    'gui/frontend/src/bracket_alias.js': 'const save = app["SaveConfig"]; export function bad(cfg) { return save(cfg); }',
    'gui/frontend/src/deep_bracket_alias.js': 'const save = window["go"]["main"]["App"]["SaveConfig"]; export function bad(cfg) { return save(cfg); }',
    'gui/frontend/src/destructure_alias.ts': 'const { SaveConfig: save } = app; export function bad(cfg: any) { return save(cfg); }',
  };
  const result = collectSaveConfigAllowlistFailures(
    files,
    (rel) => contents[rel] || '',
    [['gui/app.go', 'func (a *App) SaveConfig(config corelib.AppConfig) error', 'SaveConfig implementation used by self-test']]
  );
  if (result.length !== 14 || !result.some((item) => item.includes('gui/new_partial_save.go:1')) || !result.some((item) => item.includes('gui/spaced_partial_save.go:1')) || !result.some((item) => item.includes('gui/optional_partial.go:1')) || !result.some((item) => item.includes('gui/aliased_partial_save.go:1')) || !result.some((item) => item.includes('gui/frontend/src/aliased.ts:1')) || !result.some((item) => item.includes('gui/frontend/src/generated_partial.js:1')) || !result.some((item) => item.includes('gui/frontend/src/optional_partial.ts:1')) || !result.some((item) => item.includes('gui/frontend/src/dot_alias.ts:1')) || !result.some((item) => item.includes('gui/frontend/src/destructure_plain_alias.ts:1')) || !result.some((item) => item.includes('gui/frontend/src/bracket_partial.js:1')) || !result.some((item) => item.includes('gui/frontend/src/bracket_optional_partial.js:1')) || !result.some((item) => item.includes('gui/frontend/src/bracket_alias.js:1')) || !result.some((item) => item.includes('gui/frontend/src/deep_bracket_alias.js:1')) || !result.some((item) => item.includes('gui/frontend/src/destructure_alias.ts:1'))) {
    console.error('SaveConfig allowlist self-test failed:', result);
    process.exit(1);
  }
  const missingReason = collectSaveConfigAllowlistFailures(
    ['gui/app.go'],
    (rel) => contents[rel] || '',
    [['gui/app.go', 'func (a *App) SaveConfig(config corelib.AppConfig) error', '']]
  );
  if (!missingReason.some((item) => item.includes('needs a clear reason'))) {
    console.error('SaveConfig allowlist reason self-test failed:', missingReason);
    process.exit(1);
  }
  const staleEntry = collectSaveConfigAllowlistFailures(
    ['gui/app.go'],
    (rel) => contents[rel] || '',
    [['gui/app.go', 'not present SaveConfig snippet', 'stale allowlist entries must fail clearly']]
  );
  if (!staleEntry.some((item) => item.includes('no longer matches current code'))) {
    console.error('SaveConfig allowlist stale-entry self-test failed:', staleEntry);
    process.exit(1);
  }
  const patchFieldFiles = ['gui/app.go', 'gui/frontend/src/good_patch.ts', 'gui/frontend/src/bad_patch.ts', 'gui/good_patch.go', 'gui/bad_patch.go'];
  const patchFieldContents = {
    'gui/app.go': 'func (a *App) PatchConfigFields(patch map[string]interface{}) { switch key { case "remote_email": case "projects": } }',
    'gui/frontend/src/good_patch.ts': 'PatchConfigFields({ remote_email: email, projects: list });',
    'gui/frontend/src/bad_patch.ts': 'PatchConfigFields({ remote_mail: email });',
    'gui/good_patch.go': 'func good() { PatchConfigFields(map[string]interface{}{"remote_email": email}) }',
    'gui/bad_patch.go': 'func bad() { PatchConfigFields(map[string]interface{}{"remote_mail": email}) }',
  };
  const patchFieldResult = collectPatchConfigFieldFailures(patchFieldFiles, (rel) => patchFieldContents[rel] || '');
  if (patchFieldResult.length !== 2 || !patchFieldResult.some((item) => item.includes('gui/frontend/src/bad_patch.ts:1')) || !patchFieldResult.some((item) => item.includes('gui/bad_patch.go:1'))) {
    console.error('PatchConfigFields supported-field self-test failed:', patchFieldResult);
    process.exit(1);
  }
  const dynamicPatchFiles = ['gui/app.go', 'gui/frontend/src/good_dynamic.ts', 'gui/frontend/src/bad_dynamic.ts', 'gui/frontend/src/optional_bad_dynamic.ts', 'gui/frontend/src/computed_key_patch.ts', 'gui/frontend/src/type_signature.ts'];
  const dynamicPatchContents = {
    'gui/app.go': 'func (a *App) PatchConfigFields(patch map[string]interface{}) { switch key { case "remote_email": } }',
    'gui/frontend/src/good_dynamic.ts': 'function save(patch: Record<string, any>) { return PatchConfigFields(patch); }',
    'gui/frontend/src/bad_dynamic.ts': 'function save(patch: Record<string, any>) { return PatchConfigFields(patch); }',
    'gui/frontend/src/optional_bad_dynamic.ts': 'function save(patch: Record<string, any>) { return PatchConfigFields?.(patch); }',
    'gui/frontend/src/computed_key_patch.ts': 'function save(key: string, value: boolean) { return PatchConfigFields({ [key]: value }); }',
    'gui/frontend/src/type_signature.ts': 'type API = { PatchConfigFields(patch: Record<string, unknown>): Promise<unknown>; };',
  };
  const dynamicPatchResult = collectDynamicPatchConfigFieldFailures(
    dynamicPatchFiles,
    (rel) => dynamicPatchContents[rel] || '',
    [['gui/frontend/src/good_dynamic.ts', 'PatchConfigFields(patch)', 'self-test allowlisted local dynamic patch helper']]
  );
  if (dynamicPatchResult.length !== 3 || !dynamicPatchResult.some((item) => item.includes('gui/frontend/src/bad_dynamic.ts:1')) || !dynamicPatchResult.some((item) => item.includes('gui/frontend/src/optional_bad_dynamic.ts:1')) || !dynamicPatchResult.some((item) => item.includes('gui/frontend/src/computed_key_patch.ts:1'))) {
    console.error('PatchConfigFields dynamic-patch self-test failed:', dynamicPatchResult);
    process.exit(1);
  }
  const dynamicPatchMissingReason = collectDynamicPatchConfigFieldFailures(
    dynamicPatchFiles,
    (rel) => dynamicPatchContents[rel] || '',
    [['gui/frontend/src/good_dynamic.ts', 'PatchConfigFields(patch)', '']]
  );
  if (!dynamicPatchMissingReason.some((item) => item.includes('needs a clear reason'))) {
    console.error('PatchConfigFields dynamic-patch reason self-test failed:', dynamicPatchMissingReason);
    process.exit(1);
  }
  const dynamicPatchStaleEntry = collectDynamicPatchConfigFieldFailures(
    dynamicPatchFiles,
    (rel) => dynamicPatchContents[rel] || '',
    [['gui/frontend/src/good_dynamic.ts', 'PatchConfigFields(missingPatch)', 'stale dynamic patch allowlist entries must fail clearly']]
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

const appRel = 'gui/frontend/src/App.tsx';
const app = read(appRel);
const lines = app.split(/\r?\n/).length;

requireFile('gui/frontend/src/i18n/appTranslations.ts');
requireFile('docs/config-persistence-guardrails.md');
requireIncludes('docs/config-persistence-guardrails.md', 'Use `PatchConfig` or `PatchConfigFields` for small, local config changes.', 'config persistence guardrail guidance');
requireIncludes('docs/config-persistence-guardrails.md', '`SaveConfig` is reserved for full authoritative snapshots', 'SaveConfig authoritative snapshot guidance');
requireExcludes('gui/app_maclaw_llm.go', 'injectCodeGenModelIntoToolConfigs: SaveConfig failed', 'stale SaveConfig failure log in CodeGen SSO patch path');
requireExcludes('gui/app_project_search.go', 'LoadConfig -> merge -> SaveConfig', 'stale SaveConfig project-switch persistence comment');
requireExcludes('gui/app_project_search.go', 'switchCurrentProjectByPath: SaveConfig failed', 'stale SaveConfig project-switch failure log');
requireExcludes('gui/floating_windows.go', 'SaveConfig triggers floatingSoundChanged', 'stale SaveConfig floating sound comment');
requireExcludes('docs/project-switch-context-contamination-fix.md', 'LoadConfig -> merge -> SaveConfig', 'stale SaveConfig project-switch docs');
requireIncludes('gui/frontend/package.json', '--strict-mojibake && node scripts/check-main-ui-guards.mjs', 'frontend prebuild strict mojibake and UI guard gate');
requireIncludes('package.json', 'node scripts/check-main-ui-guards.mjs --self-test-config-persistence && node scripts/check-main-ui-guards.mjs', 'UI guard script runs config persistence self-test before normal guard');
requireFile('gui/frontend/src/config/providerCatalog.ts');
requireFile('gui/frontend/src/components/common/MarkdownLink.tsx');
requireFile('gui/frontend/src/components/tools/ToolConfiguration.tsx');
requireFile('gui/frontend/src/config/toolCatalog.ts');
requireFile('gui/frontend/src/config/apiStoreProviders.ts');
requireFile('gui/frontend/src/config/settingsTabs.ts');
requireFile('gui/frontend/src/types/appShell.ts');
requireFile('gui/frontend/src/appLazyComponents.ts');
requireFile('gui/frontend/src/components/settings/SettingsTabsRail.tsx');
requireFile('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/UISettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/SystemSettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/SystemDiagnosticsTable.tsx');
requireFile('gui/frontend/src/components/settings/ProxySettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/ProxyScopeSettings.tsx');
requireFile('gui/frontend/src/components/settings/IMSettingsPanel.tsx');
requireFile('gui/frontend/src/components/settings/IMSubTabs.tsx');
requireFile('gui/frontend/src/components/settings/ThirdPartyAccessSettings.tsx');
requireFile('gui/frontend/src/components/settings/QQBotSettings.tsx');
requireFile('gui/frontend/src/components/settings/TelegramBotSettings.tsx');
requireFile('gui/frontend/src/components/settings/WeixinSettings.tsx');
requireFile('gui/frontend/src/components/settings/WeixinQRLoginPanel.tsx');
requireFile('gui/frontend/src/components/settings/imSettingsShared.ts');
requireFile('gui/frontend/src/components/layout/AppSidebarShell.tsx');
requireFile('gui/frontend/src/components/layout/sidebarLayout.ts');
requireFile('gui/frontend/src/components/layout/SidebarNavRail.tsx');
requireFile('gui/frontend/src/components/layout/SidebarAiPane.tsx');
requireFile('gui/frontend/src/components/layout/SidebarToolSelector.tsx');
requireFile('gui/frontend/src/components/layout/SidebarTaskManagement.tsx');
requireFile('gui/frontend/src/components/layout/SidebarSystemStatus.tsx');
requireFile('gui/frontend/src/components/layout/MainTopHeader.tsx');
requireFile('gui/frontend/src/components/layout/MainTopHeaderActions.tsx');
requireFile('gui/frontend/src/components/layout/mainTopHeaderTitle.ts');
requireFile('gui/frontend/src/components/layout/AppStatusMessageBar.tsx');
requireFile('gui/frontend/src/components/pages/TutorialPage.tsx');
requireFile('gui/frontend/src/components/pages/ApiStorePage.tsx');
requireFile('gui/frontend/src/components/pages/ApiStoreProviderCard.tsx');
requireFile('gui/frontend/src/components/pages/ProjectManagerPage.tsx');
requireFile('gui/frontend/src/components/pages/ProjectManagerItem.tsx');
requireFile('gui/frontend/src/components/pages/RemoteSessionsPage.tsx');
requireFile('gui/frontend/src/components/pages/SkillsPage.tsx');
requireFile('gui/frontend/src/components/pages/MCPPage.tsx');
requireFile('gui/frontend/src/components/pages/GossipPage.tsx');
requireFile('gui/frontend/src/components/AboutPanel.tsx');
requireFile('gui/frontend/src/components/MemoryHealthDialog.tsx');
requireFile('gui/frontend/src/components/SecurityEventsDialog.tsx');
requireFile('gui/frontend/src/components/modals/ThanksModal.tsx');
requireFile('gui/frontend/src/components/modals/ToolRepairProgressDialog.tsx');
requireFile('gui/frontend/src/components/modals/UpdateModal.tsx');
requireFile('gui/frontend/src/components/modals/InstallLogModal.tsx');
requireFile('gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx');
requireFile('gui/frontend/src/components/modals/InstallSkillModal.tsx');
requireFile('gui/frontend/src/components/modals/InstallSkillList.tsx');
requireFile('gui/frontend/src/components/modals/InstallLocationSelector.tsx');
requireFile('gui/frontend/src/components/modals/InstallSkillFooter.tsx');
requireFile('gui/frontend/src/components/modals/RemoteActivationDialog.tsx');
requireFile('gui/frontend/src/components/modals/ProviderSelectorDialog.tsx');
requireFile('gui/frontend/src/components/modals/ConfirmDialog.tsx');
requireFile('gui/frontend/src/components/ai/aiAssistantMarkdown.tsx');
requireFile('gui/frontend/src/components/ai/aiAssistantPanelTheme.tsx');
requireFile('gui/frontend/src/components/ai/aiAssistantI18n.ts');
requireFile('gui/frontend/src/components/ai/ProjectSearchPanel.tsx');
requireFile('gui/frontend/src/components/ai/aiAssistantControls.tsx');
requireFile('gui/frontend/src/components/ai/useTTSReadback.ts');
requireFile('gui/frontend/src/components/ai/aiAssistantPanelTypes.ts');
requireFile('gui/frontend/src/components/ai/useAIAssistantVoiceControls.ts');
requireFile('gui/frontend/src/components/ai/useAssistantOutputScroll.ts');
requireFile('gui/frontend/src/components/ai/useAssistantThemeMode.ts');
requireFile('gui/frontend/src/components/ai/assistantThemeStorage.ts');
requireFile('gui/frontend/src/components/ai/useResizableAssistantInput.ts');
requireFile('gui/frontend/src/components/ai/useAssistantInputHistory.ts');
requireFile('gui/frontend/src/components/ai/usePastedImageAttachments.ts');
requireFile('gui/frontend/src/components/ai/useGroupDiscussionControls.ts');
requireFile('gui/frontend/src/components/ai/AssistantAttachmentsStrip.tsx');
requireFile('gui/frontend/src/components/ai/AssistantPinnedNewsCards.tsx');
requireFile('gui/frontend/src/components/ai/AssistantConversationBody.tsx');
requireFile('gui/frontend/src/components/ai/AssistantInputActions.tsx');
requireFile('gui/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx');
requireFile('gui/frontend/src/components/ai/AssistantTitleBar.tsx');
requireFile('gui/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx');
requireFile('gui/frontend/src/components/ai/AssistantInputComposer.tsx');
requireFile('gui/frontend/src/components/ai/AIAssistantRenameGroupDialog.tsx');
requireFile('gui/frontend/src/components/ai/useAssistantPreviewResize.ts');
requireFile('gui/frontend/src/components/ai/aiAssistantStatusLabels.ts');

if (lines > 6000) failures.push(`${appRel} has ${lines} lines; keep it under 6000 and extract UI instead of growing it`);

const extractedFileLineLimits = [
  ['gui/frontend/src/components/layout/AppSidebarShell.tsx', 500],
  ['gui/frontend/src/components/layout/SidebarNavRail.tsx', 320],
  ['gui/frontend/src/components/layout/SidebarAiPane.tsx', 260],
  ['gui/frontend/src/components/layout/MainTopHeader.tsx', 240],
  ['gui/frontend/src/components/layout/MainTopHeaderActions.tsx', 140],
  ['gui/frontend/src/components/layout/mainTopHeaderTitle.ts', 80],
  ['gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 220],
  ['gui/frontend/src/components/settings/UISettingsPanel.tsx', 340],
  ['gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 480],
  ['gui/frontend/src/components/settings/CodingKnowledgeSection.tsx', 500],
  ['gui/frontend/src/components/settings/programmingToolsConfig.ts', 80],
  ['gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 180],
  ['gui/frontend/src/components/settings/SystemDiagnosticsTable.tsx', 80],
  ['gui/frontend/src/components/settings/ProxySettingsPanel.tsx', 160],
  ['gui/frontend/src/components/settings/ProxyScopeSettings.tsx', 100],
  ['gui/frontend/src/components/settings/IMSettingsPanel.tsx', 220],
  ['gui/frontend/src/components/settings/IMSubTabs.tsx', 100],
  ['gui/frontend/src/components/settings/WeixinSettings.tsx', 180],
  ['gui/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 220],
  ['gui/frontend/src/components/modals/InstallSkillModal.tsx', 220],
  ['gui/frontend/src/components/modals/InstallSkillList.tsx', 140],
  ['gui/frontend/src/components/modals/InstallLocationSelector.tsx', 140],
  ['gui/frontend/src/components/modals/InstallSkillFooter.tsx', 140],
  ['gui/frontend/src/components/pages/ProjectManagerPage.tsx', 130],
  ['gui/frontend/src/components/pages/ProjectManagerItem.tsx', 120],
  ['gui/frontend/src/components/pages/ApiStorePage.tsx', 120],
  ['gui/frontend/src/components/pages/ApiStoreProviderCard.tsx', 120],
  ['gui/frontend/src/config/apiStoreProviders.ts', 80],
  ['gui/frontend/src/components/AboutPanel.tsx', 920],
  ['gui/frontend/src/components/MemoryHealthDialog.tsx', 200],
  ['gui/frontend/src/components/SecurityEventsDialog.tsx', 170],
  ['gui/frontend/src/components/ai/AIAssistantPanel.tsx', 6200],
  ['gui/frontend/src/components/ai/aiAssistantMarkdown.tsx', 1550],
  ['gui/frontend/src/components/ai/aiAssistantPanelTheme.tsx', 700],
  ['gui/frontend/src/components/ai/aiAssistantI18n.ts', 40],
  ['gui/frontend/src/components/ai/ProjectSearchPanel.tsx', 320],
  ['gui/frontend/src/components/ai/aiAssistantControls.tsx', 120],
  ['gui/frontend/src/components/ai/useTTSReadback.ts', 120],
  ['gui/frontend/src/components/ai/aiAssistantPanelTypes.ts', 160],
  ['gui/frontend/src/components/ai/useAIAssistantVoiceControls.ts', 100],
  ['gui/frontend/src/components/ai/useAssistantOutputScroll.ts', 100],
  ['gui/frontend/src/components/ai/useResizableAssistantInput.ts', 80],
  ['gui/frontend/src/components/ai/useAssistantInputHistory.ts', 100],
  ['gui/frontend/src/components/ai/usePastedImageAttachments.ts', 320],
  ['gui/frontend/src/components/ai/useGroupDiscussionControls.ts', 90],
  ['gui/frontend/src/components/ai/AssistantAttachmentsStrip.tsx', 240],
  ['gui/frontend/src/components/ai/AssistantPinnedNewsCards.tsx', 80],
  ['gui/frontend/src/components/ai/AssistantConversationBody.tsx', 250],
  ['gui/frontend/src/components/ai/AssistantInputActions.tsx', 520],
  ['gui/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 100],
  ['gui/frontend/src/components/ai/AssistantTitleBar.tsx', 220],
  ['gui/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx', 50],
  ['gui/frontend/src/components/ai/AssistantInputComposer.tsx', 240],
  ['gui/frontend/src/components/ai/AIAssistantRenameGroupDialog.tsx', 110],
  ['gui/frontend/src/components/ai/useAssistantPreviewResize.ts', 50],
  ['gui/frontend/src/components/ai/aiAssistantStatusLabels.ts', 40],
];
for (const [rel, max] of extractedFileLineLimits) requireMaxLines(rel, max);

const highRiskRemoteFileLineLimits = [
  ['gui/frontend/src/components/remote/SkillsManagementPanel.tsx', 3000],
  // Implementation lives here; freeze growth until further extraction (entry is a thin re-export).
  ['gui/frontend/src/components/remote/SkillsManagementPanelView.tsx', 5700],
  ['gui/frontend/src/components/remote/OnboardingWizard.tsx', 2400],
  ['gui/frontend/src/components/remote/LLMConfigPanel.tsx', 1600],
  ['gui/frontend/src/components/remote/MCPManagementPanel.tsx', 1325],
  ['gui/frontend/src/components/remote/MemoryManagementPanel.tsx', 1100],
];
for (const [rel, max] of highRiskRemoteFileLineLimits) requireMaxLines(rel, max);

const modalThemeFiles = [
  'gui/frontend/src/components/modals/InstallSkillModal.tsx',
  'gui/frontend/src/components/modals/ConfirmDialog.tsx',
  'gui/frontend/src/components/modals/UpdateModal.tsx',
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
requireIncludes('gui/frontend/src/components/remote/OnboardingWizard.tsx', 'getOnboardingFlow({ brandId, freeTrial, offlineMode })', 'centralized onboarding flow');
requireExcludes('gui/frontend/src/components/remote/OnboardingWizard.tsx', 'brandId === \'qianxin\'', 'inline TigerClaw brand detection; use onboardingFlow.ts');
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
requireIncludes('gui/frontend/src/components/layout/sidebarLayout.ts', 'SIDEBAR_NAV_RAIL_WIDTH = 60', 'narrow 5.10.x sidebar rail width guard');
requireExcludes('gui/frontend/src/components/layout/AppSidebarShell.tsx', "'90px'", 'hard-coded old sidebar rail width');
requireExcludes('gui/frontend/src/components/layout/SidebarNavRail.tsx', "width: '90px'", 'hard-coded old sidebar rail width');
requireIncludes('gui/frontend/src/App.tsx', 'className="global-action-bar" data-ai-theme={aiThemeMode}', 'dark themed global action bar');
// Viewport shell must carry theme attrs so --theme-page-bg is not the light
// :root default; otherwise dark mode shows a white frame around #App.
requireIncludes('gui/frontend/src/App.tsx', 'className="app-viewport"', 'app viewport shell');
requireIncludes('gui/frontend/src/App.tsx', 'data-ai-theme={aiThemeMode}\n            data-ai-dark-scheme={aiThemeMode === \'dark\' ? aiDarkSchemeId : undefined}', 'app viewport dark theme attrs for page-bg shell');
requireIncludes('gui/frontend/src/App.css', ":where(#App, .app-viewport)[data-ai-theme='dark']", 'viewport/App dark theme rebinding (low-specificity :where so schemes win)');
requireIncludes('gui/frontend/src/App.css', "#App[data-maximized=\"true\"] {\n    border-radius: 0;\n    border: none;", 'maximized window drops CSS border (no edge frame)');
requireIncludes('gui/frontend/src/App.tsx', 'getAssistantDarkScheme(aiDarkSchemeId).cssVars.pageBg', 'document shell page-bg sync on theme change');
requireIncludes('gui/frontend/src/App.css', ".sidebar[data-ai-theme='dark'] {\n    --theme-primary", 'sidebar dark theme variables');
requireIncludes('gui/frontend/src/App.css', '--theme-page-bg: #0b1220;', 'sidebar dark theme page background');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'STATUS_DOT', 'system status decoded status dot');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'CREDIT_SEPARATOR', 'system status decoded credit separator');
requireExcludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', '>\\u', 'JSX unicode escape text that renders as code');
requireExcludes(appRel, 'taskItems.map', 'inline task management list; use components/layout/SidebarAiPane.tsx');
requireExcludes(appRel, 'className="top-header"', 'inline non-AI top header; use components/layout/MainTopHeader.tsx');
requireExcludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'ReadTutorial', 'inline top header actions; use components/layout/MainTopHeaderActions.tsx');
requireExcludes(appRel, 'className="status-message"', 'inline status message bar; use components/layout/AppStatusMessageBar.tsx');
requireExcludes(appRel, 'backgroundInstallStatus.startsWith', 'inline background install status; use components/layout/AppStatusMessageBar.tsx');
requireExcludes(appRel, "ChatFire', url:", 'inline API Store provider cards; use config/apiStoreProviders.ts');
requireExcludes('gui/frontend/src/components/pages/ApiStorePage.tsx', "ChatFire', url:", 'inline API Store provider cards; use config/apiStoreProviders.ts');
requireExcludes(appRel, 'key={refreshKey}', 'inline tutorial markdown page; use components/pages/TutorialPage.tsx');
requireExcludes(appRel, 'className="project-manager-panel"', 'inline project manager page; use components/pages/ProjectManagerPage.tsx');
requireExcludes(appRel, 'pagedProjects.map', 'inline project manager list; use components/pages/ProjectManagerPage.tsx');
requireExcludes('gui/frontend/src/components/pages/ProjectManagerPage.tsx', 'SelectProjectDir', 'inline project row actions; use components/pages/ProjectManagerItem.tsx');
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
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'function renderContentWithCodeBlocks', 'inline AI markdown/code-block renderer; use components/ai/aiAssistantMarkdown.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'function renderMessage', 'inline AI message renderer; use components/ai/aiAssistantMarkdown.tsx');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'renderMessage } from "./aiAssistantMarkdown"', 'AI markdown renderer import');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'const lightTheme', 'inline AI panel theme; use components/ai/aiAssistantPanelTheme.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'function AssistantInputIcon', 'inline AI input icons; use components/ai/aiAssistantPanelTheme.tsx');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./aiAssistantPanelTheme"', 'AI panel theme import');
requireIncludes('gui/frontend/src/App.tsx', "from './components/ai/assistantThemeStorage'", 'App reads pure AI theme storage helper');
requireIncludes('gui/frontend/src/App.tsx', "themeMode={aiThemeMode}", 'App controls AI assistant theme mode across tab switches');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', "themeMode: controlledThemeMode", 'AI assistant accepts controlled theme mode');
requireIncludes('gui/frontend/src/components/ai/assistantThemeStorage.ts', "window.localStorage.setItem(AI_THEME_MODE_STORAGE_KEY, themeMode)", 'AI assistant persists shared theme mode');
requireIncludes('gui/frontend/src/components/ai/aiAssistantPanelTheme.tsx', "AI_THEME_MODE_LEGACY_STORAGE_KEY", 'AI assistant legacy theme key is centralized');
requireIncludes('gui/frontend/src/components/ai/assistantThemeStorage.ts', "AI_THEME_MODE_LEGACY_STORAGE_KEY", 'AI assistant reads/writes legacy theme key for compatibility');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', "from \"./useAssistantThemeMode\"", 'AI assistant theme hook import');
requireIncludes('gui/frontend/src/components/ai/useAssistantThemeMode.ts', "writeStoredAssistantThemeMode(themeMode)", 'AI theme hook delegates storage writes');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'interface ProjectSearchItem', 'inline AI project search model; use components/ai/ProjectSearchPanel.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'function useProjectSearch', 'inline AI project search hook; use components/ai/ProjectSearchPanel.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'function ProjectSearchPanel', 'inline AI project search panel; use components/ai/ProjectSearchPanel.tsx');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./ProjectSearchPanel"', 'AI project search import');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'function VoiceLevelVisualizer', 'inline AI voice level visualizer; use components/ai/aiAssistantControls.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'const miniActionButtonStyle', 'inline AI mini action button style; use components/ai/aiAssistantControls.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'GetTTSEnabled', 'inline AI TTS readback hook; use components/ai/useTTSReadback.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'EventsOn("tts:audio"', 'inline AI TTS audio listener; use components/ai/useTTSReadback.ts');
requireIncludes('gui/frontend/src/components/ai/AssistantInputActions.tsx', 'from "./aiAssistantControls"', 'AI controls import');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useTTSReadback"', 'AI TTS hook import');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'interface AIAssistantPanelStateProps', 'inline AI assistant panel props; use components/ai/aiAssistantPanelTypes.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'voiceHoldTimerRef', 'inline AI voice hold controls; use components/ai/useAIAssistantVoiceControls.ts');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./aiAssistantPanelTypes"', 'AI panel types import');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAIAssistantVoiceControls"', 'AI voice controls hook import');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'prevMsgCountRef', 'inline AI output scroll manager; use components/ai/useAssistantOutputScroll.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'scrollTimerRef', 'inline AI output scroll debounce; use components/ai/useAssistantOutputScroll.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'setInputAreaHeight', 'inline AI input resize state; use components/ai/useResizableAssistantInput.ts');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAssistantOutputScroll"', 'AI output scroll hook import');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useResizableAssistantInput"', 'AI input resize hook import');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'historyEdits', 'inline AI input history edits; use components/ai/useAssistantInputHistory.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'SavePastedImage', 'inline pasted image saving; use components/ai/usePastedImageAttachments.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'setGroupDiscussionBusy', 'inline group discussion busy state; use components/ai/useGroupDiscussionControls.ts');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAssistantInputHistory"', 'AI input history hook import');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./usePastedImageAttachments"', 'AI pasted image hook import');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-pending-attachments"', 'inline AI pending attachments strip; use components/ai/AssistantAttachmentsStrip.tsx');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'from "./AssistantAttachmentsStrip"', 'AI attachments strip import');
requireIncludes('gui/frontend/src/components/ai/AssistantAttachmentsStrip.tsx', 'title={att.filePath}', 'pasted image path tooltip');
requireIncludes('gui/frontend/src/components/ai/AssistantAttachmentsStrip.tsx', 'thumbnailDataUrl', 'pasted image thumbnail rendering');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-workflow-docs-bar"', 'old left-side AI workflow docs bar');
requireExcludes('gui/frontend/src/components/ai/AssistantInputStack.tsx', 'AssistantWorkflowDocsBar', 'old left-side workflow docs bar wiring');
requireIncludes('gui/frontend/src/components/ai/WorkflowDocPreview.tsx', 'WorkflowProgressBoard', 'right-side workflow progress board');
requireIncludes('gui/frontend/src/components/ai/WorkflowDocPreview.tsx', 'workflowPhaseOrders', 'workflow phase order map');
requireFile('gui/frontend/src/components/ai/workflowPhaseMeta.generated.ts');
requireFile('gui/frontend/src/components/ai/__tests__/workflowPhaseMeta.contract.test.ts');
requireIncludes('gui/frontend/src/components/ai/__tests__/workflowPhaseMeta.contract.test.ts', 'workflowPhaseMeta.generated', 'contract test imports the generated artifact');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'className="pinned-news-card"', 'inline pinned news cards; use components/ai/AssistantPinnedNewsCards.tsx');
requireIncludes('gui/frontend/src/components/ai/AssistantConversationBody.tsx', 'from "./AssistantPinnedNewsCards"', 'AI pinned news cards import');
requireIncludes('gui/frontend/src/components/ai/AssistantPinnedNewsCards.tsx', 'className="pinned-news-card"', 'pinned news card rendering');
requireIncludes('gui/frontend/src/components/ai/AssistantPinnedNewsCards.tsx', 'renderInlineMarkdown', 'pinned news markdown rendering');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'Setup not completed', 'inline AI conversation body; use components/ai/AssistantConversationBody.tsx');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./AssistantConversationBody"', 'AI conversation body import');
requireIncludes('gui/frontend/src/components/ai/AssistantConversationBody.tsx', 'AssistantPinnedNewsCards', 'conversation body pinned news wiring');
requireIncludes('gui/frontend/src/components/ai/AssistantConversationBody.tsx', 'showProcessingState', 'conversation body busy state rendering');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-voice-input"', 'inline AI input action buttons; use components/ai/AssistantInputActions.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-cancel-progress"', 'inline AI cancel action button; use components/ai/AssistantInputActions.tsx');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'from "./AssistantInputActions"', 'AI input actions import');
requireIncludes('gui/frontend/src/components/ai/AssistantInputActions.tsx', 'data-testid="ai-voice-input"', 'voice input button rendering');
requireIncludes('gui/frontend/src/components/ai/AssistantInputActions.tsx', 'VoiceLevelVisualizer', 'voice level visualizer rendering');
requireIncludes('gui/frontend/src/components/ai/AssistantInputActions.tsx', 'data-testid="ai-cancel-progress"', 'cancel progress button rendering');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'miniActionButtonStyle', 'inline group discussion action menu; use components/ai/AssistantGroupDiscussionMenu.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'Experts', 'inline group discussion stats; use components/ai/AssistantGroupDiscussionMenu.tsx');
requireIncludes('gui/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'GD', 'group discussion titlebar button');
requireIncludes('gui/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'runGroupDiscussionAction("accept"', 'group discussion accept action');
requireIncludes('gui/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'runGroupDiscussionAction("publish"', 'group discussion publish action');
requireIncludes('gui/frontend/src/components/ai/AssistantGroupDiscussionMenu.tsx', 'calc(100vw - 96px)', 'group discussion popup viewport fit');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-title-bar"', 'inline AI title bar; use components/ai/AssistantTitleBar.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'ai-titlebar-window-group', 'inline AI titlebar window controls; use components/ai/AssistantTitleBar.tsx');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./AssistantTitleBar"', 'AI title bar import');
requireIncludes('gui/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-title-bar"', 'AI title bar wrapper');
requireIncludes('gui/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-titlebar-tools-group"', 'AI title bar tool group');
requireIncludes('gui/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-hide-toggle"', 'AI hide window control');
requireIncludes('gui/frontend/src/components/ai/AssistantTitleBar.tsx', 'data-testid="ai-maximize-toggle"', 'AI maximize window control');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-workflow-maximize-suggestion"', 'inline workflow maximize suggestion; use components/ai/AssistantWorkflowMaximizeSuggestion.tsx');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./AssistantWorkflowMaximizeSuggestion"', 'AI workflow maximize suggestion import');
requireIncludes('gui/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx', 'data-testid="ai-workflow-maximize-suggestion"', 'workflow maximize suggestion wrapper');
requireIncludes('gui/frontend/src/components/ai/AssistantWorkflowMaximizeSuggestion.tsx', 'onToggleMaximize(); onDismiss();', 'workflow maximize action preserves dismiss');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'data-testid="ai-input"', 'inline AI input composer; use components/ai/AssistantInputComposer.tsx');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'rememberHistoryEdit(e.target.value)', 'inline AI input history handling; use components/ai/AssistantInputComposer.tsx');
requireIncludes('gui/frontend/src/components/ai/AssistantInputStack.tsx', 'from "./AssistantInputComposer"', 'AI input composer import');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'data-testid={textareaTestId}', 'AI input textarea');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'AssistantAttachmentsStrip', 'AI input attachments strip wiring');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'AssistantInputActionsLeft', 'AI input action buttons wiring');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'recallHistory("up")', 'AI input history up recall');
requireIncludes('gui/frontend/src/components/ai/AssistantInputComposer.tsx', 'handleSend();', 'AI input enter submit wiring');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'const initStatusLabels', 'inline AI init status labels; use components/ai/aiAssistantStatusLabels.ts');
requireExcludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'document.body.style.cursor = "col-resize"', 'inline AI preview resize hook; use components/ai/useAssistantPreviewResize.ts');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./useAssistantPreviewResize"', 'AI preview resize hook import');
requireIncludes('gui/frontend/src/components/ai/AIAssistantPanel.tsx', 'from "./aiAssistantStatusLabels"', 'AI init status labels import');
requireIncludes('gui/frontend/src/components/ai/useAssistantPreviewResize.ts', 'setSplitRatio(nextRatio)', 'AI preview resize ratio update');
requireIncludes('gui/frontend/src/components/ai/aiAssistantStatusLabels.ts', 'getAssistantInitLabel', 'AI init status label helper');

const criticalMarkers = [
  ['gui/frontend/src/App.tsx', 'AIAssistantPanel', 'AI assistant panel'],
  ['gui/frontend/src/App.tsx', 'IMAuditPanel', 'IM audit/watch panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'PetSettingsPanel', 'pet settings tab'],
  ['MCPPage', 'MCP main page'],
  ['GossipPage', 'gossip main page'],
  ['chatFontSize', 'AI assistant font-size setting'],
  ['taskManagementPaneWidth', 'resizable task management pane'],
  ['gui/frontend/src/App.tsx', 'getSettingsTabOptions', 'settings tab registry import'],
  ['gui/frontend/src/components/settings/SettingsPage.tsx', 'SettingsTabsRail', 'settings tabs rail'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'GeneralSettingsPanel', 'general settings panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'UISettingsPanel', 'UI settings panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'ProgrammingToolsSettingsPanel', 'programming tools settings panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'GeneralAdvancedSettingsPanel', 'advanced general settings panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'SystemSettingsPanel', 'system settings panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'ProxySettingsPanel', 'proxy settings panel'],
  ['gui/frontend/src/components/settings/SettingsActiveContent.tsx', 'IMSettingsPanel', 'IM settings panel'],
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

requireIncludes('gui/frontend/src/i18n/appTranslations.ts', 'export const translations', 'central translations export');
requireIncludes('gui/frontend/src/config/providerCatalog.ts', 'export const knownProviderEndpoints', 'provider endpoint export');
requireIncludes('gui/frontend/src/config/providerCatalog.ts', 'export const recommendedModels', 'recommended models export');
requireIncludes('gui/frontend/src/components/tools/ToolConfiguration.tsx', 'export const ToolConfiguration', 'ToolConfiguration export');
requireIncludes('gui/frontend/src/components/common/MarkdownLink.tsx', 'export const MarkdownLink', 'MarkdownLink export');
requireIncludes('gui/frontend/src/config/toolCatalog.ts', 'export const TOOL_NAMES', 'tool catalog export');
requireIncludes('gui/frontend/src/config/settingsTabs.ts', 'export const getSettingsTabOptions', 'settings tab registry export');
requireIncludes('gui/frontend/src/config/settingsTabs.ts', "id: 'pet'", 'pet settings tab registry entry');
requireIncludes('gui/frontend/src/config/settingsTabs.ts', "id: 'display'", 'programming tools settings tab registry entry');
requireIncludes('gui/frontend/src/config/settingsTabs.ts', "id: 'redeem'", 'service redeem settings tab registry entry');
requireIncludes('gui/frontend/src/components/remote/HubServiceRedeemPanel.tsx', 'authorizedModelsTableStyle', 'authorized models fixed table layout');
requireIncludes('gui/frontend/src/components/remote/HubServiceRedeemPanel.tsx', 'authorizedGroupTagStyle', 'authorized model service group tag layout');
requireIncludes('gui/frontend/src/config/settingsTabs.ts', "id: 'im'", 'IM settings tab registry entry');
requireIncludes('gui/frontend/src/components/settings/SettingsTabsRail.tsx', 'export const SettingsTabsRail', 'settings tabs rail export');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'export const GeneralSettingsPanel', 'general settings export');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'llm_trajectory_logging', 'general settings trajectory toggle');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'log_detail_enabled', 'general settings detailed logs toggle');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'SelectWorkingDir', 'general settings working directory picker');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'export const UISettingsPanel', 'UI settings export');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'SetUIZoomFactor', 'UI zoom persistence wiring');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'SetChatFontSize', 'AI assistant font size persistence wiring');
requireIncludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'export const ProgrammingToolsSettingsPanel', 'programming tools settings export');
requireExcludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'default_tool_provider', 'removed default programming provider setting');
requireExcludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'remoteToolMetadata.map', 'removed default coding tool options');
requireExcludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'Default Coding Tool', 'removed default coding tool card');
requireExcludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'Default Provider', 'removed default programming provider card');
requireExcludes('gui/frontend/src/App.tsx', 'ListToolProviders', 'removed default programming provider list wiring');
requireExcludes('gui/frontend/wailsjs/go/main/App.d.ts', 'ListToolProviders', 'removed default programming provider binding');
requireExcludes('gui/frontend/wailsjs/go/main/App.js', 'ListToolProviders', 'removed default programming provider binding');
requireIncludes('gui/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'export const GeneralAdvancedSettingsPanel', 'advanced general settings export');
requireIncludes('gui/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'show_ai_trace_entry', 'AI trace entry toggle');
requireIncludes('gui/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'SetEnvCheckInterval', 'environment check interval wiring');
requireIncludes('gui/frontend/src/components/settings/GeneralAdvancedSettingsPanel.tsx', 'use_windows_terminal', 'Windows Terminal toggle');
requireIncludes('gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 'export const SystemSettingsPanel', 'system settings export');
requireIncludes('gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 'remote_heartbeat_sec', 'system heartbeat setting');
requireIncludes('gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 'workstation_mode', 'workstation mode toggle');
requireIncludes('gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 'audio_input_device_id', 'audio input device setting');
requireIncludes('gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 'SystemDiagnosticsTable', 'diagnostics table wiring');
requireIncludes('gui/frontend/src/components/settings/SystemDiagnosticsTable.tsx', '<table', 'diagnostics table rendering');
requireIncludes('gui/frontend/src/components/settings/SystemDiagnosticsTable.tsx', 'var(--theme-surface-muted)', 'diagnostics table dark-mode surface');
requireIncludes('gui/frontend/src/components/settings/ProxySettingsPanel.tsx', 'export const ProxySettingsPanel', 'proxy settings export');
requireIncludes('gui/frontend/src/components/settings/ProxySettingsPanel.tsx', 'default_proxy_enabled', 'proxy enabled setting');
requireIncludes('gui/frontend/src/components/settings/ProxySettingsPanel.tsx', 'SaveProxyConfig', 'proxy save backend wiring');
requireIncludes('gui/frontend/src/components/settings/ProxySettingsPanel.tsx', 'ProxyScopeSettings', 'proxy scope settings wiring');
requireIncludes('gui/frontend/src/components/settings/ProxyScopeSettings.tsx', 'default_proxy_scope_maclaw', 'proxy Maclaw scope setting');
requireIncludes('gui/frontend/src/components/settings/ProxyScopeSettings.tsx', 'default_proxy_scope_coding_tools', 'proxy coding tools scope setting');
requireIncludes('gui/frontend/src/components/settings/ProxyScopeSettings.tsx', 'default_proxy_scope_agent', 'proxy agent scope setting');
requireIncludes('gui/frontend/src/components/settings/IMSettingsPanel.tsx', 'export const IMSettingsPanel', 'IM settings export');
requireIncludes('gui/frontend/src/components/settings/IMSettingsPanel.tsx', 'IMSubTabs', 'IM sub-tabs wiring');
requireIncludes('gui/frontend/src/components/settings/IMSubTabs.tsx', 'export const IMSubTabs', 'IM sub-tabs export');
requireIncludes('gui/frontend/src/components/settings/IMSubTabs.tsx', "key: 'weixin'", 'WeChat tab before third-party access');
requireIncludes('gui/frontend/src/components/settings/IMSubTabs.tsx', "key: 'thirdparty'", 'third-party IM tab entry');
requireOrder('gui/frontend/src/components/settings/IMSubTabs.tsx', "key: 'weixin'", "key: 'thirdparty'", 'WeChat tab before third-party access');
requireIncludes('gui/frontend/src/components/settings/ThirdPartyAccessSettings.tsx', "setIMAuditPlatform('thirdparty')", 'third-party IM audit button');
requireIncludes('gui/frontend/src/components/settings/QQBotSettings.tsx', 'export const QQBotSettings', 'QQ bot settings export');
requireIncludes('gui/frontend/src/components/settings/QQBotSettings.tsx', 'qqbot_app_secret', 'QQ bot secret setting');
requireIncludes('gui/frontend/src/components/settings/QQBotSettings.tsx', 'RestartQQBot', 'QQ bot restart wiring');
requireIncludes('gui/frontend/src/components/settings/QQBotSettings.tsx', 'SetQQBotLocalMode', 'QQ bot local mode wiring');
requireIncludes('gui/frontend/src/components/settings/QQBotSettings.tsx', "setIMAuditPlatform('qq')", 'QQ bot audit/watch button');
requireIncludes('gui/frontend/src/components/settings/TelegramBotSettings.tsx', 'export const TelegramBotSettings', 'Telegram bot settings export');
requireIncludes('gui/frontend/src/components/settings/TelegramBotSettings.tsx', 'telegram_bot_token', 'Telegram token setting');
requireIncludes('gui/frontend/src/components/settings/TelegramBotSettings.tsx', 'RestartTelegram', 'Telegram restart wiring');
requireIncludes('gui/frontend/src/components/settings/TelegramBotSettings.tsx', 'SetTelegramLocalMode', 'Telegram local mode wiring');
requireIncludes('gui/frontend/src/components/settings/TelegramBotSettings.tsx', "setIMAuditPlatform('telegram')", 'Telegram audit/watch button');
requireIncludes('gui/frontend/src/components/settings/imSettingsShared.ts', 'export const localModeOptions', 'shared IM mode options');
for (const rel of [
  'gui/frontend/src/components/settings/IMSettingsPanel.tsx',
  'gui/frontend/src/components/settings/QQBotSettings.tsx',
  'gui/frontend/src/components/settings/TelegramBotSettings.tsx',
  'gui/frontend/src/components/settings/WeixinSettings.tsx',
  'gui/frontend/src/components/settings/WeixinQRLoginPanel.tsx',
  'gui/frontend/src/components/settings/ThirdPartyAccessSettings.tsx',
  'gui/frontend/src/components/settings/imSettingsShared.ts',
  'gui/frontend/src/components/layout/AppSidebarShell.tsx',
  'gui/frontend/src/components/layout/SidebarNavRail.tsx',
  'gui/frontend/src/components/layout/SidebarAiPane.tsx',
  'gui/frontend/src/components/layout/SidebarToolSelector.tsx',
  'gui/frontend/src/components/layout/SidebarTaskManagement.tsx',
  'gui/frontend/src/components/layout/SidebarSystemStatus.tsx',
  'gui/frontend/src/components/layout/MainTopHeader.tsx',
  'gui/frontend/src/components/layout/AppStatusMessageBar.tsx',
  'gui/frontend/src/components/modals/ThanksModal.tsx',
  'gui/frontend/src/components/modals/ToolRepairProgressDialog.tsx',
  'gui/frontend/src/components/modals/UpdateModal.tsx',
  'gui/frontend/src/components/modals/InstallLogModal.tsx',
  'gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx',
  'gui/frontend/src/components/modals/InstallSkillModal.tsx',
  'gui/frontend/src/components/modals/InstallSkillList.tsx',
  'gui/frontend/src/components/modals/InstallLocationSelector.tsx',
  'gui/frontend/src/components/modals/InstallSkillFooter.tsx',
  'gui/frontend/src/components/modals/RemoteActivationDialog.tsx',
  'gui/frontend/src/components/modals/ProviderSelectorDialog.tsx',
  'gui/frontend/src/components/modals/ConfirmDialog.tsx',
  'gui/frontend/src/components/ai/AIAssistantRenameGroupDialog.tsx',
  'gui/frontend/src/components/remote/HubServiceRedeemPanel.tsx',
]) {
  requireNoMojibake(rel);
  requireNoPlaceholderGlyphs(rel);
}
requireIncludes('gui/frontend/src/components/settings/ThirdPartyAccessSettings.tsx', 'thirdparty_gateway_enabled', 'third-party gateway toggle');
requireIncludes('gui/frontend/src/components/settings/WeixinSettings.tsx', 'export const WeixinSettings', 'WeChat settings export');
requireIncludes('gui/frontend/src/components/settings/WeixinSettings.tsx', 'WeixinQRLoginPanel', 'WeChat QR login panel wiring');
requireIncludes('gui/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 'StartWeixinQRLogin', 'WeChat QR login wiring');
requireIncludes('gui/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 'PollWeixinQRStatus', 'WeChat QR polling wiring');
requireIncludes('gui/frontend/src/components/settings/WeixinQRLoginPanel.tsx', 'QRCodeSVG', 'WeChat QR code rendering');
requireIncludes('gui/frontend/src/components/settings/WeixinSettings.tsx', 'RestartWeixin', 'WeChat restart wiring');
requireIncludes('gui/frontend/src/components/settings/WeixinSettings.tsx', 'StopWeixin', 'WeChat disconnect wiring');
requireIncludes('gui/frontend/src/components/settings/WeixinSettings.tsx', 'SetWeixinLocalMode', 'WeChat local mode wiring');
requireIncludes('gui/frontend/src/components/settings/WeixinSettings.tsx', "setIMAuditPlatform('weixin')", 'WeChat audit/watch button');
requireIncludes('gui/frontend/src/components/settings/IMSettingsPanel.tsx', 'IMAuditPanel', 'IM audit panel rendering');
requireIncludes('gui/frontend/src/components/settings/ThirdPartyAccessSettings.tsx', 'RestartThirdPartyGateway', 'third-party gateway restart wiring');
for (const color of ['#555', '#888', '#ddd', '#6366f1', '#eef2ff', '#dcfce7', '#fee2e2', '#fef9c3']) {
  requireExcludes('gui/frontend/src/components/settings/IMSettingsPanel.tsx', color, `hard-coded ${color} in IM settings; use theme variables`);
}
requireIncludes('gui/frontend/src/components/layout/AppSidebarShell.tsx', 'export const AppSidebarShell', 'left sidebar shell export');
requireIncludes('gui/frontend/src/components/layout/AppSidebarShell.tsx', 'SidebarNavRail', 'left sidebar nav rail wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarNavRail.tsx', 'export const SidebarNavRail', 'sidebar nav rail export');
requireIncludes('gui/frontend/src/components/layout/SidebarNavRail.tsx', 'left-nav-item--ai', 'AI nav rail button');
requireIncludes('gui/frontend/src/components/layout/SidebarNavRail.tsx', 'runningTaskCount', 'monitor running task badge');
requireIncludes('gui/frontend/src/components/layout/SidebarAiPane.tsx', 'export const SidebarAiPane', 'sidebar AI pane export');
requireIncludes('gui/frontend/src/components/layout/SidebarAiPane.tsx', 'handleTaskManagementResizeStart', 'task management resize handle wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'export const SidebarToolSelector', 'sidebar tool selector export');
requireIncludes('gui/frontend/src/components/layout/SidebarTaskManagement.tsx', 'export const SidebarTaskManagement', 'sidebar task management export');
requireIncludes('gui/frontend/src/components/layout/SidebarTaskManagement.tsx', 'renameTask', 'task rename wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarTaskManagement.tsx', 'pinTask', 'task pin wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarTaskManagement.tsx', 'hideTask', 'task hide wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'Claude Code', 'Claude Code selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'CodeBuddy', 'CodeBuddy selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'Kilo Code', 'Kilo Code selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubTotalCredits', 'hub credits total display wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubUsedCredits', 'hub credits used display wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubExpiry', 'hub credits expiry display wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'sidebarCurrentProviderTokenUsage.isHubService', 'hub service credits visibility condition');
requireIncludes('gui/frontend/src/components/layout/SidebarTaskManagement.tsx', 'visibleTasks.map', 'task list stays in sidebar task management component');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'showHubCreditAction', 'hub credits action stays in sidebar system status');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'openHubCreditsPage', 'hub credits purchase action wiring');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'export const MainTopHeader', 'non-AI top header export');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'getHeaderTitle', 'top header title resolver wiring');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'MainTopHeaderActions', 'top header actions wiring');
requireIncludes('gui/frontend/src/components/layout/MainTopHeaderActions.tsx', 'ReadTutorial', 'top header tutorial refresh action');
requireIncludes('gui/frontend/src/components/layout/MainTopHeaderActions.tsx', 'setShowModelSettings(true)', 'top header provider config action');
requireIncludes('gui/frontend/src/components/layout/MainTopHeaderActions.tsx', 'setShowInstallSkillModal(true)', 'top header install skill action');
requireIncludes('gui/frontend/src/components/layout/mainTopHeaderTitle.ts', 'export const getHeaderTitle', 'top header title resolver export');
requireIncludes('gui/frontend/src/components/layout/MainTopHeaderActions.tsx', 'providerConfig', 'top header provider config label');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'handleWindowHide', 'minimize button wiring stays in top header');
requireIncludes('gui/frontend/src/components/layout/MainTopHeaderActions.tsx', 'setShowModelSettings(true)', 'provider config button stays in top header actions');
requireIncludes('gui/frontend/src/components/layout/AppStatusMessageBar.tsx', "'status-message'", 'status message bar wrapper');
requireIncludes('gui/frontend/src/components/layout/AppStatusMessageBar.tsx', 'backgroundInstallStatus', 'background install status display');
requireIncludes('gui/frontend/src/components/layout/AppStatusMessageBar.tsx', 'onOpenLLMSettings', 'LLM warning navigation');
requireIncludes('gui/frontend/src/components/pages/TutorialPage.tsx', 'ReactMarkdown', 'tutorial markdown rendering stays in TutorialPage');
requireIncludes('gui/frontend/src/components/pages/TutorialPage.tsx', 'components={{ a: MarkdownLink }}', 'tutorial markdown link handling');
requireIncludes('gui/frontend/src/components/pages/ApiStorePage.tsx', 'SetChatFontSize', 'API Store chat font size setting');
requireIncludes('gui/frontend/src/components/pages/ApiStorePage.tsx', 'ApiStoreProviderCard', 'API Store provider card wiring');
requireIncludes('gui/frontend/src/config/apiStoreProviders.ts', 'ChatFire', 'API Store ChatFire card');
requireIncludes('gui/frontend/src/config/apiStoreProviders.ts', '\\u667a\\u8c31', 'API Store Zhipu card');
requireIncludes('gui/frontend/src/config/apiStoreProviders.ts', '\\u963f\\u91cc\\u4e91', 'API Store Aliyun card');
requireIncludes('gui/frontend/src/components/pages/ApiStoreProviderCard.tsx', 'BrowserOpenURL', 'API Store external link action');
requireIncludes('gui/frontend/src/components/pages/ApiStoreProviderCard.tsx', 'var(--theme-surface)', 'API Store card theme-aware surface');
requireIncludes('gui/frontend/src/components/pages/ProjectManagerPage.tsx', 'export const ProjectManagerPage', 'project manager page export');
requireIncludes('gui/frontend/src/components/pages/ProjectManagerPage.tsx', 'ProjectManagerItem', 'project manager item wiring');
requireIncludes('gui/frontend/src/components/pages/ProjectManagerItem.tsx', 'SelectProjectDir', 'project path picker wiring');
requireIncludes('gui/frontend/src/components/pages/ProjectManagerItem.tsx', 'PatchConfigFields', 'project manager save wiring');
requireIncludes('gui/frontend/src/components/pages/ProjectManagerItem.tsx', 'var(--theme-surface-muted)', 'project path theme-aware background');
requireExcludes('gui/frontend/src/components/pages/ProjectManagerItem.tsx', 'SaveConfig', 'project manager full-config save; use PatchConfigFields');
requireIncludes('gui/frontend/src/components/pages/RemoteSessionsPage.tsx', 'RemoteSessionList', 'remote session list stays in RemoteSessionsPage');
requireIncludes('gui/frontend/src/components/pages/SkillsPage.tsx', 'SkillsManagementPanel', 'skills management stays in SkillsPage');
requireIncludes('gui/frontend/src/components/pages/MCPPage.tsx', 'MCPManagementPanel', 'MCP management stays in MCPPage');
requireIncludes('gui/frontend/src/components/pages/GossipPage.tsx', 'GossipPanel', 'gossip panel stays in GossipPage');
requireIncludes('gui/frontend/src/App.tsx', 'onCheckUpdate={() => {', 'about page update check wiring');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'officialWebsite', 'about page website button');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'quickActionsTitle', 'about page localized quick actions title');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'buildNumber', 'about page build number prop usage');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'bugReport', 'about page bug report link');
requireIncludes('gui/frontend/src/App.tsx', 'MACLAW_CODE_REPOSITORY_URL = "https://github.com/rapidai/maclaw"', 'about page fixed code repository URL');
requireIncludes('gui/frontend/src/App.tsx', 'onOpenGithub={() => BrowserOpenURL(MACLAW_CODE_REPOSITORY_URL)}', 'about page code repository button uses fixed URL');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'MemoryHealthDialog', 'about page memory health dialog');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'SecurityEventsDialog', 'about page security events dialog');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'ReadErrorLog', 'about page error log backend wiring');
requireIncludes('gui/frontend/src/components/AboutPanel.tsx', 'ReactMarkdown', 'about page thanks markdown rendering');
requireIncludes('gui/frontend/src/i18n/appTranslations.ts', '"aboutProductName"', 'about page product name translation');
requireIncludes('gui/frontend/src/i18n/appTranslations.ts', '"quickActionsTitle"', 'about page quick actions title translation');
requireIncludes('gui/frontend/src/i18n/appTranslations.ts', '"errorLog"', 'about page error log translation');
requireIncludes('gui/frontend/src/i18n/appTranslations.ts', '"codeRepository"', 'about page code repository translation');
requireIncludes('gui/frontend/src/components/MemoryHealthDialog.tsx', 'GetMemoryHealth', 'memory health backend wiring');
requireIncludes('gui/frontend/src/components/SecurityEventsDialog.tsx', 'QuerySecurityEvents', 'security events backend wiring');
requireIncludes('gui/frontend/src/components/remote/onboardingFlow.ts', "['sso', 'wechat']", 'TigerClaw onboarding stays two steps');
requireIncludes('gui/frontend/src/components/remote/OnboardingWizard.tsx', "isCurrentOnboardingStep(onboardingFlow, step, 'sso')", 'TigerClaw SSO first step uses centralized flow');
requireIncludes('gui/frontend/src/components/remote/onboardingFlow.ts', "['register', 'wechat']", 'free trial skips LLM setup in centralized flow');
requireIncludes('gui/frontend/src/components/remote/__tests__/onboardingFlow.test.ts', 'keeps standard free trial to register plus WeChat', 'free trial onboarding flow regression test');
requireIncludes('gui/frontend/src/components/remote/__tests__/OnboardingWizard.test.tsx', 'keeps TigerClaw onboarding to SSO plus WeChat without LLM setup', 'TigerClaw onboarding regression test');
requireIncludes('gui/frontend/src/components/SecurityEventsDialog.tsx', "t('securityEventsDeniedSummary')", 'security events summary localization');
requireIncludes('gui/frontend/src/components/SecurityEventsDialog.tsx', "t('securityEventsTime')", 'security events table header localization');
requireIncludes('gui/frontend/src/i18n/appTranslations.ts', '"securityEventsDeniedSummary"', 'security events summary translations');
requireIncludes('gui/frontend/src/i18n/appTranslations.ts', '"securityRiskCritical"', 'security event risk translations');
requireIncludes('gui/frontend/src/components/modals/ThanksModal.tsx', 'ReactMarkdown', 'thanks modal markdown rendering');
requireIncludes('gui/frontend/src/components/modals/ThanksModal.tsx', 'components={{ a: MarkdownLink }}', 'thanks modal markdown links');
requireIncludes('gui/frontend/src/components/modals/ToolRepairProgressDialog.tsx', 'toolRepairInstalling', 'tool repair installing message');
requireIncludes('gui/frontend/src/components/modals/ToolRepairProgressDialog.tsx', 'status.message', 'tool repair failure details');
requireIncludes('gui/frontend/src/components/modals/UpdateModal.tsx', 'downloadAndUpdate', 'update modal download action');
requireIncludes('gui/frontend/src/components/modals/UpdateModal.tsx', 'cancelDownload', 'update modal cancel download action');
requireIncludes('gui/frontend/src/components/modals/UpdateModal.tsx', 'var(--theme-info-bg)', 'update modal theme-aware info panel');
requireIncludes('gui/frontend/src/components/modals/UpdateModal.tsx', '\\u2714\\uFE0F', 'update modal latest-version icon');
requireIncludes('gui/frontend/src/components/modals/InstallLogModal.tsx', 'installLogTitle', 'install log title');
requireIncludes('gui/frontend/src/components/modals/InstallLogModal.tsx', 'navigator.clipboard.writeText', 'install log copy action');
requireIncludes('gui/frontend/src/components/modals/InstallLogModal.tsx', 'onSendLog(hasError)', 'install log send action');
requireIncludes('gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'proxyHostPlaceholder', 'project proxy host input');
requireIncludes('gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'useDefaultProxy', 'project proxy default toggle');
requireIncludes('gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'PatchConfigFields({ projects: newConfig.projects })', 'project proxy save wiring');
requireExcludes('gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'SaveConfig', 'project proxy full-config save; use PatchConfigFields');
requireIncludes('gui/frontend/src/components/modals/InstallSkillModal.tsx', 'InstallSkill', 'install skill action');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'InstallDefaultMarketplace', 'install default marketplace action');
requireIncludes('gui/frontend/src/components/modals/InstallSkillModal.tsx', 'skillZipOnlyError', 'skill compatibility error');
requireIncludes('gui/frontend/src/components/modals/InstallSkillModal.tsx', 'var(--theme-success)', 'install skill modal theme-aware title');
requireIncludes('gui/frontend/src/components/modals/InstallSkillModal.tsx', 'var(--theme-primary)', 'install skill modal theme-aware skills link');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'export const InstallSkillFooter', 'install skill footer export');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'onInstallSelected', 'install selected footer action wiring');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'isMarketplaceInstalling', 'marketplace install loading state');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'var(--theme-success)', 'install footer theme-aware success color');
requireIncludes('gui/frontend/src/components/modals/InstallSkillList.tsx', 'export const InstallSkillList', 'install skill list export');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'export const InstallLocationSelector', 'install location selector export');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'setInstallLocation', 'install location update wiring');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'setInstallProject', 'install project update wiring');
requireIncludes('gui/frontend/src/components/PetSettingsPanel.tsx', 'patchConfig(patch)', 'pet settings atomic patch save wiring');
requireExcludes('gui/frontend/src/components/PetSettingsPanel.tsx', 'saveConfig', 'pet settings full-config save prop; use patchConfig');
requireIncludes('gui/frontend/src/App.tsx', 'PatchConfigFields(patch)', 'model settings atomic patch save stays explicit');
requireIncludes('gui/app_asr.go', 'PatchConfigFields(map[string]interface{}{"asr_enabled": enabled})', 'ASR enabled setter uses atomic config patch');
requireExcludes('gui/app_asr.go', 'a.SaveConfig(cfg)', 'ASR setter full-config save; use PatchConfigFields');
requireIncludes('gui/app_tts.go', 'PatchConfigFields(map[string]interface{}{"tts_enabled": enabled})', 'TTS enabled setter uses atomic config patch');
requireIncludes('gui/app_tts.go', 'PatchConfigFields(map[string]interface{}{"tts_voice_id": voiceID})', 'TTS voice setter uses atomic config patch');
requireExcludes('gui/app_tts.go', 'a.SaveConfig(cfg)', 'TTS setter full-config save; use PatchConfigFields');
requireIncludes('gui/app_websearch.go', 'return a.PatchConfig(func(cfg *corelib.AppConfig) {', 'web search provider save uses atomic config patch');
requireExcludes('gui/app_websearch.go', 'a.SaveConfig(cfg)', 'web search provider full-config save; use PatchConfig');
requireIncludes('gui/app_mis_data.go', 'return a.PatchConfig(func(cfg *corelib.AppConfig) {', 'MIS data settings save uses atomic config patch');
requireExcludes('gui/app_mis_data.go', 'a.SaveConfig(cfg)', 'MIS data full-config save; use PatchConfig');
requireIncludes('gui/app_wails_bindings.go', 'cfg.MemoryAutoCompress = enabled', 'memory auto-compress config write remains explicit');
requireIncludes('gui/app_wails_bindings.go', 'return a.PatchConfig(func(cfg *corelib.AppConfig) {', 'memory auto-compress save uses atomic config patch');
requireIncludes('gui/app_yolo_model.go', 'PatchConfigFields(map[string]interface{}{"screen_parsing_enabled": enabled})', 'screen parsing setter uses atomic config patch');
requireExcludes('gui/app_yolo_model.go', 'a.SaveConfig(cfg)', 'screen parsing setter full-config save; use PatchConfigFields');
requireIncludes('gui/env_check_api.go', 'PatchConfigFields(map[string]interface{}{"env_check_interval": days})', 'environment interval setter uses atomic config patch');
requireIncludes('gui/env_check_api.go', 'PatchConfigFields(map[string]interface{}{"last_env_check_time": time.Now().Format(time.RFC3339)})', 'environment check timestamp uses atomic config patch');
requireExcludes('gui/env_check_api.go', 'a.SaveConfig(config)', 'environment check full-config save; use PatchConfigFields');
requireIncludes('gui/app_nl_mcp.go', 'cfg.MCPServers = servers', 'remote MCP server save remains explicit');
requireIncludes('gui/app_nl_mcp.go', 'cfg.LocalMCPServers = servers', 'local MCP server save remains explicit');
requireIncludes('gui/app_nl_skills.go', 'cfg.ExternalSkillDirs = nextDirs', 'external skill dir add uses atomic config patch');
requireIncludes('gui/app_nl_skills.go', 'cfg.ExternalSkillDirs = filtered', 'external skill dir remove uses atomic config patch');
requireIncludes('gui/app_ve.go', 'cfg.VEAllowedDirectories = nextDirs', 'VE allowed directories save uses atomic config patch');
requireIncludes('gui/app_wails_bindings.go', 'cfg.MemoryMaxBackups = n', 'memory max backups save uses atomic config patch');
requireIncludes('gui/qqbot_gateway.go', 'cfg.SetQQBotLocal(enabled)', 'QQ bot local mode uses atomic config patch');
requireIncludes('gui/telegram_gateway.go', 'cfg.SetTelegramLocal(enabled)', 'Telegram local mode uses atomic config patch');
requireIncludes('gui/weixin_gateway.go', 'cfg.SetWeixinLocal(enabled)', 'Weixin local mode uses atomic config patch');
requireIncludes('gui/lansenger_gateway.go', 'cfg.SetLansengerLocal(enabled)', 'Lansenger local mode uses atomic config patch');
requireIncludes('gui/thirdparty_gateway.go', 'cfg.SetThirdPartyGatewayLocal(enabled)', 'third-party gateway local mode uses atomic config patch');
requireIncludes('gui/config_manager.go', 'm.app.PatchConfig(func(cfg *corelib.AppConfig) {', 'config manager updates use atomic config patch');
requireIncludes('gui/hub_update_cache.go', 'cfg.RemoteHubCenterURLs = discovered', 'hub center URL cache uses atomic config patch');
requireIncludes('gui/floating_assistant.go', 'config.FloatingBtnPositionSet = true', 'floating button position uses atomic config patch');
requireIncludes('gui/floating_assistant.go', 'PatchConfigFields(map[string]interface{}{"pet_enabled": false})', 'floating assistant disable uses pet atomic patch');
requireIncludes('gui/floating_windows.go', 'PatchConfigFields(map[string]interface{}{"pet_motion_sound_enabled": newEnabled})', 'floating window pet sound menu uses atomic config patch');
requireIncludes('gui/app_project_search.go', 'PatchConfigFields(map[string]interface{}{"current_project": p.Id})', 'project search current-project switch uses atomic config patch');
requireIncludes('gui/app_data_migration.go', 'PatchConfig(func(cfg *corelib.AppConfig) { cfg.DataDir = newDir })', 'data directory save uses atomic config patch');
requireExcludes('gui/app_data_migration.go', 'a.SaveConfig(config)', 'data directory full-config save; use PatchConfig');
requireIncludes('gui/remote_smoke.go', 'app.PatchConfig(func(cfg *corelib.AppConfig) {', 'remote smoke config overrides use atomic config patch');
requireExcludes('gui/remote_smoke.go', 'app.SaveConfig(cfg)', 'remote smoke full-config save; use PatchConfig');
requireIncludes('gui/im_tools_misc.go', 'cfg.RemoteNickname = nickname', 'nickname setter persists only nickname');
requireIncludes('gui/im_tools_misc.go', 'h.app.PatchConfig(func(cfg *corelib.AppConfig) {', 'nickname setter uses atomic config patch');
requireExcludes('gui/im_tools_misc.go', 'h.saveConfig(cfg)', 'nickname full-config save; use PatchConfig');
requireIncludes('gui/app_nl_skills.go', 'cfg.NLSkills = filtered', 'skill executor save uses atomic config patch');
requireIncludes('gui/weixin_gateway.go', 'saveWeixinLoginConfig(result)', 'Weixin QR login saves through atomic config patch helper');
requireIncludes('gui/weixin_gateway.go', 'cfg.WeixinToken = result.BotToken', 'Weixin login patch persists token');
requireIncludes('gui/app_maclaw_llm.go', 'a.PatchConfig(func(cfg *corelib.AppConfig) {', 'LLM token usage updates use atomic config patch');
requireIncludes('gui/app_maclaw_llm.go', 'delta.LocalCacheRequests++', 'LLM local cache usage counter preserved');
requireIncludes('gui/app_maclaw_llm.go', 'delete(cfg.LLMTokenUsage, provider)', 'LLM token reset patches usage map');
requireIncludes('gui/app_maclaw_llm.go', 'currentCfg.MaclawLLMProviders = cfg.MaclawLLMProviders', 'Maclaw LLM provider save uses atomic config patch');
requireIncludes('gui/app_maclaw_llm.go', 'PatchConfigFields(map[string]interface{}{"remote_email": result.Email})', 'CodeGen SSO email backfill uses atomic config patch');
requireIncludes('gui/hub_llm_service.go', 'syncHubLLMServiceStatusToConfig(status, false)', 'Hub LLM status refresh uses atomic config patch');
requireIncludes('gui/hub_llm_service.go', 'syncHubLLMServiceStatusToConfig(serviceStatus, false)', 'Hub LLM redemption uses atomic config patch');
requireExcludes('gui/hub_llm_service.go', 'a.SaveConfig(', 'Hub LLM service full-config save; use PatchConfig');
requireIncludes('gui/tui_mode.go', 'a.app.PatchConfig(func(cfg *corelib.AppConfig) {', 'TUI single-field config save uses atomic config patch');
requireIncludes('gui/platform_windows.go', 'PatchConfigFields(map[string]interface{}{"env_check_done": true, "pause_env_check": true})', 'Windows env-check completion uses atomic config patch');
requireIncludes('gui/platform_linux.go', 'PatchConfigFields(map[string]interface{}{"env_check_done": true, "pause_env_check": true})', 'Linux env-check completion uses atomic config patch');
requireIncludes('gui/platform_darwin.go', 'PatchConfigFields(map[string]interface{}{"env_check_done": true, "pause_env_check": true})', 'Darwin env-check completion uses atomic config patch');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'var(--theme-surface)', 'install location theme-aware surface');
requireIncludes('gui/frontend/src/components/modals/InstallSkillList.tsx', 'selectedSkillsToInstall.includes(skill.name)', 'install skill selection state');
requireIncludes('gui/frontend/src/components/modals/InstallSkillList.tsx', 'setSelectedSkillsToInstall', 'install skill selection update');
requireIncludes('gui/frontend/src/components/modals/InstallSkillList.tsx', 'var(--theme-surface)', 'install skill list theme-aware surface');
requireIncludes('gui/frontend/src/components/modals/RemoteActivationDialog.tsx', 'remoteActivationDialogTitle', 'remote activation dialog title');
requireIncludes('gui/frontend/src/components/modals/RemoteActivationDialog.tsx', 'remoteLoadRegisteredHubs', 'remote activation hub loading');
requireIncludes('gui/frontend/src/components/modals/RemoteActivationDialog.tsx', 'remoteActivateAndLaunch', 'remote activation launch action');
requireIncludes('gui/frontend/src/components/modals/ProviderSelectorDialog.tsx', 'selectProviderTitle', 'provider selector title');
requireIncludes('gui/frontend/src/components/modals/ProviderSelectorDialog.tsx', 'providers.map', 'provider selector grid');
requireIncludes('gui/frontend/src/components/modals/ProviderSelectorDialog.tsx', 'hoveredProvider.provider.url', 'provider selector tooltip');
requireIncludes('gui/frontend/src/components/modals/ConfirmDialog.tsx', 'stroke="#ef4444"', 'confirm dialog icon');
requireIncludes('gui/frontend/src/components/modals/ConfirmDialog.tsx', 'var(--theme-surface)', 'confirm dialog theme-aware surface');
requireIncludes('gui/frontend/src/components/modals/ConfirmDialog.tsx', 'var(--theme-text-primary)', 'confirm dialog theme-aware text');
requireIncludes('gui/frontend/src/components/modals/ConfirmDialog.tsx', 'onConfirm', 'confirm dialog confirm action');
requireIncludes('gui/frontend/src/components/modals/ConfirmDialog.tsx', 'onCancel', 'confirm dialog cancel action');
requireSaveConfigAllowlist();
requirePatchConfigFieldsSupported();
requireDynamicPatchConfigFieldsAllowlist();

if (failures.length) {
  console.error('Main UI guard check failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Main UI guard check passed (${lines} App.tsx lines).`);
