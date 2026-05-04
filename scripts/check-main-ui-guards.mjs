import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, '..');
const read = (rel) => fs.readFileSync(path.join(repoRoot, rel), 'utf8');
const exists = (rel) => fs.existsSync(path.join(repoRoot, rel));
const failures = [];
const requireFile = (rel) => {
  if (!exists(rel)) failures.push(`missing required file: ${rel}`);
};
const requireIncludes = (rel, needle, label = needle) => {
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
requireFile('gui/frontend/src/config/providerCatalog.ts');
requireFile('gui/frontend/src/components/common/MarkdownLink.tsx');
requireFile('gui/frontend/src/components/tools/ToolConfiguration.tsx');
requireFile('gui/frontend/src/config/toolCatalog.ts');
requireFile('gui/frontend/src/config/apiStoreProviders.ts');
requireFile('gui/frontend/src/config/settingsTabs.ts');
requireFile('gui/frontend/src/types/appShell.ts');
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
requireFile('gui/frontend/src/components/layout/SidebarNavRail.tsx');
requireFile('gui/frontend/src/components/layout/SidebarAiPane.tsx');
requireFile('gui/frontend/src/components/layout/SidebarToolSelector.tsx');
requireFile('gui/frontend/src/components/layout/SidebarRecentTasks.tsx');
requireFile('gui/frontend/src/components/layout/SidebarSystemStatus.tsx');
requireFile('gui/frontend/src/components/layout/MainTopHeader.tsx');
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
requireFile('gui/frontend/src/components/pages/AboutPage.tsx');
requireFile('gui/frontend/src/components/pages/AboutActions.tsx');
requireFile('gui/frontend/src/components/modals/StartupPopup.tsx');
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

if (lines > 4500) failures.push(`${appRel} has ${lines} lines; keep it under 4500 and extract UI instead of growing it`);

const extractedFileLineLimits = [
  ['gui/frontend/src/components/layout/AppSidebarShell.tsx', 260],
  ['gui/frontend/src/components/layout/SidebarNavRail.tsx', 220],
  ['gui/frontend/src/components/layout/SidebarAiPane.tsx', 220],
  ['gui/frontend/src/components/layout/MainTopHeader.tsx', 220],
  ['gui/frontend/src/components/layout/mainTopHeaderTitle.ts', 80],
  ['gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 180],
  ['gui/frontend/src/components/settings/UISettingsPanel.tsx', 180],
  ['gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 180],
  ['gui/frontend/src/components/settings/SystemSettingsPanel.tsx', 180],
  ['gui/frontend/src/components/settings/SystemDiagnosticsTable.tsx', 80],
  ['gui/frontend/src/components/settings/ProxySettingsPanel.tsx', 160],
  ['gui/frontend/src/components/settings/ProxyScopeSettings.tsx', 100],
  ['gui/frontend/src/components/settings/IMSettingsPanel.tsx', 180],
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
  ['gui/frontend/src/components/pages/AboutPage.tsx', 100],
  ['gui/frontend/src/components/pages/AboutActions.tsx', 120],
];
for (const [rel, max] of extractedFileLineLimits) requireMaxLines(rel, max);

if (app.charCodeAt(0) === 0xfeff) failures.push(`${appRel} starts with a UTF-8 BOM`);
if (app.includes('\ufffd')) failures.push(`${appRel} contains Unicode replacement characters`);

requireExcludes(appRel, 'const translations', 'inline translations; use i18n/appTranslations.ts');
requireExcludes(appRel, 'const knownProviderEndpoints', 'inline provider endpoint catalog; use config/providerCatalog.ts');
requireExcludes(appRel, 'const recommendedModels', 'inline recommended model catalog; use config/providerCatalog.ts');
requireExcludes(appRel, 'const ToolConfiguration =', 'inline ToolConfiguration; use components/tools/ToolConfiguration.tsx');
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
requireExcludes(appRel, 'recentProjects.map', 'inline recent tasks list; use components/layout/SidebarAiPane.tsx');
requireExcludes(appRel, 'className="top-header"', 'inline non-AI top header; use components/layout/MainTopHeader.tsx');
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
requireExcludes(appRel, 'brandDisplayTitle}</h2>', 'inline about page; use components/pages/AboutPage.tsx');
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

const criticalMarkers = [
  ['AIAssistantPanel', 'AI assistant panel'],
  ['IMAuditPanel', 'IM audit/watch panel'],
  ['PetSettingsPanel', 'pet settings tab'],
  ['GroupDiscussionSettingsPanel', 'group discussion settings'],
  ['AgentNetTabContainer', 'AgentNet main tab'],
  ['MCPPage', 'MCP main page'],
  ['GossipPage', 'gossip main page'],
  ['show_nav_mcp', 'left nav MCP visibility setting'],
  ['show_nav_gossip', 'left nav gossip visibility setting'],
  ['show_nav_agentnet', 'left nav AgentNet visibility setting'],
  ['chatFontSize', 'AI assistant font-size setting'],
  ['recentTasksPaneWidth', 'resizable recent tasks pane'],
  ['getSettingsTabOptions', 'settings tab registry import'],
  ['SettingsTabsRail', 'settings tabs rail'],
  ['GeneralSettingsPanel', 'general settings panel'],
  ['UISettingsPanel', 'UI settings panel'],
  ['ProgrammingToolsSettingsPanel', 'programming tools settings panel'],
  ['GeneralAdvancedSettingsPanel', 'advanced general settings panel'],
  ['SystemSettingsPanel', 'system settings panel'],
  ['ProxySettingsPanel', 'proxy settings panel'],
  ['IMSettingsPanel', 'IM settings panel'],
  ['AppSidebarShell', 'left sidebar shell'],
  ['MainTopHeader', 'non-AI top header'],
  ['AppStatusMessageBar', 'status message bar'],
  ['TutorialPage', 'tutorial page'],
  ['ApiStorePage', 'API Store page'],
  ['ProjectManagerPage', 'project manager page'],
  ['RemoteSessionsPage', 'remote sessions page'],
  ['SkillsPage', 'skills page'],
  ['StartupPopup', 'startup popup'],
  ['ThanksModal', 'thanks modal'],
  ['AboutPage', 'about page'],
  ['ToolRepairProgressDialog', 'tool repair progress dialog'],
  ['UpdateModal', 'update modal'],
  ['InstallLogModal', 'install log modal'],
  ['ProjectProxySettingsDialog', 'project proxy settings dialog'],
  ['InstallSkillModal', 'install skill modal'],
  ['RemoteActivationDialog', 'remote activation dialog'],
  ['ProviderSelectorDialog', 'provider selector dialog'],
  ['ConfirmDialog', 'confirm dialog'],
];
for (const [needle, label] of criticalMarkers) requireIncludes(appRel, needle, label);

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
requireIncludes('gui/frontend/src/config/settingsTabs.ts', "id: 'im'", 'IM settings tab registry entry');
requireIncludes('gui/frontend/src/components/settings/SettingsTabsRail.tsx', 'export const SettingsTabsRail', 'settings tabs rail export');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'export const GeneralSettingsPanel', 'general settings export');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'llm_trajectory_logging', 'general settings trajectory toggle');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'log_detail_enabled', 'general settings detailed logs toggle');
requireIncludes('gui/frontend/src/components/settings/GeneralSettingsPanel.tsx', 'SelectWorkingDir', 'general settings working directory picker');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'export const UISettingsPanel', 'UI settings export');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'SetUIZoomFactor', 'UI zoom persistence wiring');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'SetChatFontSize', 'AI assistant font size persistence wiring');
requireIncludes('gui/frontend/src/components/settings/UISettingsPanel.tsx', 'show_nav_agentnet', 'sidebar AgentNet visibility toggle');
requireIncludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'export const ProgrammingToolsSettingsPanel', 'programming tools settings export');
requireIncludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'default_tool_provider', 'default programming provider wiring');
requireIncludes('gui/frontend/src/components/settings/ProgrammingToolsSettingsPanel.tsx', 'remoteToolMetadata.map', 'default coding tool options');
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
  'gui/frontend/src/components/layout/SidebarRecentTasks.tsx',
  'gui/frontend/src/components/layout/SidebarSystemStatus.tsx',
  'gui/frontend/src/components/layout/MainTopHeader.tsx',
  'gui/frontend/src/components/layout/AppStatusMessageBar.tsx',
  'gui/frontend/src/components/modals/StartupPopup.tsx',
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
requireIncludes('gui/frontend/src/components/layout/SidebarNavRail.tsx', 'show_nav_agentnet', 'AgentNet nav visibility wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarNavRail.tsx', 'runningTaskCount', 'monitor running task badge');
requireIncludes('gui/frontend/src/components/layout/SidebarAiPane.tsx', 'export const SidebarAiPane', 'sidebar AI pane export');
requireIncludes('gui/frontend/src/components/layout/SidebarAiPane.tsx', 'handleRecentTasksResizeStart', 'recent tasks resize handle wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'export const SidebarToolSelector', 'sidebar tool selector export');
requireIncludes('gui/frontend/src/components/layout/SidebarRecentTasks.tsx', 'export const SidebarRecentTasks', 'sidebar recent tasks export');
requireIncludes('gui/frontend/src/components/layout/SidebarRecentTasks.tsx', 'renameTask', 'recent task rename wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarRecentTasks.tsx', 'pinTask', 'recent task pin wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarRecentTasks.tsx', 'hideTask', 'recent task hide wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'Claude Code', 'Claude Code selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'Gemini CLI', 'Gemini CLI selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'CodeBuddy', 'CodeBuddy selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarToolSelector.tsx', 'Kilo Code', 'Kilo Code selector entry');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubTotalCredits', 'hub credits total display wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubUsedCredits', 'hub credits used display wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'formatSidebarHubExpiry', 'hub credits expiry display wiring');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'sidebarCurrentProviderTokenUsage.isHubService', 'hub service credits visibility condition');
requireIncludes('gui/frontend/src/components/layout/SidebarRecentTasks.tsx', 'recentProjects.map', 'recent tasks list stays in sidebar recent tasks component');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'showHubCreditAction', 'hub credits action stays in sidebar system status');
requireIncludes('gui/frontend/src/components/layout/SidebarSystemStatus.tsx', 'openHubCreditsPage', 'hub credits purchase action wiring');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'export const MainTopHeader', 'non-AI top header export');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'getHeaderTitle', 'top header title resolver wiring');
requireIncludes('gui/frontend/src/components/layout/mainTopHeaderTitle.ts', 'export const getHeaderTitle', 'top header title resolver export');
requireIncludes('gui/frontend/src/components/layout/mainTopHeaderTitle.ts', 'agentNet', 'top header AgentNet title label');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'providerConfig', 'top header provider config label');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'handleWindowHide', 'minimize button wiring stays in top header');
requireIncludes('gui/frontend/src/components/layout/MainTopHeader.tsx', 'setShowModelSettings(true)', 'provider config button stays in top header');
requireIncludes('gui/frontend/src/components/layout/AppStatusMessageBar.tsx', 'className="status-message"', 'status message bar wrapper');
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
requireIncludes('gui/frontend/src/components/pages/ProjectManagerItem.tsx', 'SaveConfig', 'project manager save wiring');
requireIncludes('gui/frontend/src/components/pages/ProjectManagerItem.tsx', 'var(--theme-surface-muted)', 'project path theme-aware background');
requireIncludes('gui/frontend/src/components/pages/RemoteSessionsPage.tsx', 'RemoteSessionList', 'remote session list stays in RemoteSessionsPage');
requireIncludes('gui/frontend/src/components/pages/SkillsPage.tsx', 'SkillsManagementPanel', 'skills management stays in SkillsPage');
requireIncludes('gui/frontend/src/components/pages/MCPPage.tsx', 'MCPManagementPanel', 'MCP management stays in MCPPage');
requireIncludes('gui/frontend/src/components/pages/GossipPage.tsx', 'GossipPanel', 'gossip panel stays in GossipPage');
requireIncludes('gui/frontend/src/components/pages/AboutPage.tsx', 'AboutActions', 'about page action wiring');
requireIncludes('gui/frontend/src/components/pages/AboutActions.tsx', 'CheckUpdate', 'about page update check');
requireIncludes('gui/frontend/src/components/pages/AboutActions.tsx', 'officialWebsite', 'about page website button');
requireIncludes('gui/frontend/src/components/pages/AboutActions.tsx', 'bugReport', 'about page bug report link');
requireIncludes('gui/frontend/src/components/pages/AboutPage.tsx', 'var(--theme-text-primary)', 'about page theme-aware text');
requireIncludes('gui/frontend/src/components/modals/StartupPopup.tsx', 'hide_startup_popup', 'startup popup hide toggle wiring');
requireIncludes('gui/frontend/src/components/modals/StartupPopup.tsx', 'UserManual_CN.md', 'startup popup manual link');
requireIncludes('gui/frontend/src/components/modals/StartupPopup.tsx', 'var(--theme-surface)', 'startup popup theme-aware surface');
requireIncludes('gui/frontend/src/components/modals/StartupPopup.tsx', '\\u{1F3AC}', 'startup popup quick start icon');
requireIncludes('gui/frontend/src/components/modals/StartupPopup.tsx', '\\u{1F4D6}', 'startup popup manual icon');
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
requireIncludes('gui/frontend/src/components/modals/ProjectProxySettingsDialog.tsx', 'SaveConfig(newConfig)', 'project proxy save wiring');
requireIncludes('gui/frontend/src/components/modals/InstallSkillModal.tsx', 'InstallSkill', 'install skill action');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'InstallDefaultMarketplace', 'install default marketplace action');
requireIncludes('gui/frontend/src/components/modals/InstallSkillModal.tsx', 'skillZipOnlyError', 'skill compatibility error');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'export const InstallSkillFooter', 'install skill footer export');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'onInstallSelected', 'install selected footer action wiring');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'isMarketplaceInstalling', 'marketplace install loading state');
requireIncludes('gui/frontend/src/components/modals/InstallSkillFooter.tsx', 'var(--theme-success)', 'install footer theme-aware success color');
requireIncludes('gui/frontend/src/components/modals/InstallSkillList.tsx', 'export const InstallSkillList', 'install skill list export');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'export const InstallLocationSelector', 'install location selector export');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'setInstallLocation', 'install location update wiring');
requireIncludes('gui/frontend/src/components/modals/InstallLocationSelector.tsx', 'setInstallProject', 'install project update wiring');
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

if (failures.length) {
  console.error('Main UI guard check failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Main UI guard check passed (${lines} App.tsx lines).`);
