import { lazy } from 'react';

export const AIAssistantPanel = lazy(() => import('./components/ai/AIAssistantPanel').then((module) => ({ default: module.AIAssistantPanel })));
export const WebSearchConfigPanel = lazy(() => import('./components/remote/WebSearchConfigPanel').then((module) => ({ default: module.WebSearchConfigPanel })));
export const SecurityPolicyPanel = lazy(() => import('./components/remote/SecurityPolicyPanel').then((module) => ({ default: module.SecurityPolicyPanel })));
export const LLMConfigPanel = lazy(() => import('./components/remote/LLMConfigPanel').then((module) => ({ default: module.LLMConfigPanel })));
export const HubServiceRedeemPanel = lazy(() => import('./components/remote/HubServiceRedeemPanel').then((module) => ({ default: module.HubServiceRedeemPanel })));
export const EmbeddingConfigPanel = lazy(() => import('./components/remote/EmbeddingConfigPanel').then((module) => ({ default: module.EmbeddingConfigPanel })));
export const ASRConfigPanel = lazy(() => import('./components/remote/ASRConfigPanel').then((module) => ({ default: module.ASRConfigPanel })));
export const DiarizationConfigPanel = lazy(() => import('./components/remote/DiarizationConfigPanel').then((module) => ({ default: module.DiarizationConfigPanel })));
export const OCRConfigPanel = lazy(() => import('./components/remote/OCRConfigPanel').then((module) => ({ default: module.OCRConfigPanel })));
export const TTSConfigPanel = lazy(() => import('./components/remote/TTSConfigPanel').then((module) => ({ default: module.TTSConfigPanel })));
export const MemoryManagementPanel = lazy(() => import('./components/remote/MemoryManagementPanel').then((module) => ({ default: module.MemoryManagementPanel })));
export const KnowledgeSettingsPanel = lazy(() => import('./components/settings/KnowledgeSettingsPanel').then((module) => ({ default: module.KnowledgeSettingsPanel })));
export const MISDataSettingsPanel = lazy(() => import('./components/settings/MISDataSettingsPanel').then((module) => ({ default: module.MISDataSettingsPanel })));
export const UISettingsPanel = lazy(() => import('./components/settings/UISettingsPanel').then((module) => ({ default: module.UISettingsPanel })));
export const ProgrammingToolsSettingsPanel = lazy(() => import('./components/settings/ProgrammingToolsSettingsPanel').then((module) => ({ default: module.ProgrammingToolsSettingsPanel })));
// GeneralSettingsPanel / GeneralAdvancedSettingsPanel are eager in SettingsActiveContent
// so the default settings tab never suspends (OEM intermittent blank fix).
export const SystemSettingsPanel = lazy(() => import('./components/settings/SystemSettingsPanel').then((module) => ({ default: module.SystemSettingsPanel })));
export const AssetManagementPanel = lazy(() => import('./components/settings/AssetManagementPanel').then((module) => ({ default: module.AssetManagementPanel })));
export const MigrationSettingsPanel = lazy(() => import('./components/settings/MigrationSettingsPanel').then((module) => ({ default: module.MigrationSettingsPanel })));
export const ProxySettingsPanel = lazy(() => import('./components/settings/ProxySettingsPanel').then((module) => ({ default: module.ProxySettingsPanel })));
export const LLMCacheSettingsPanel = lazy(() => import('./components/settings/LLMCacheSettingsPanel').then((module) => ({ default: module.LLMCacheSettingsPanel })));
export const VirtualEmployeeSettingsPanel = lazy(() => import('./components/settings/VirtualEmployeeSettingsPanel').then((module) => ({ default: module.VirtualEmployeeSettingsPanel })));
export const TutorialPage = lazy(() => import('./components/pages/TutorialPage').then((module) => ({ default: module.TutorialPage })));
export const ApiStorePage = lazy(() => import('./components/pages/ApiStorePage').then((module) => ({ default: module.ApiStorePage })));
export const ProjectManagerPage = lazy(() => import('./components/pages/ProjectManagerPage').then((module) => ({ default: module.ProjectManagerPage })));
export const RemoteSessionsPage = lazy(() => import('./components/pages/RemoteSessionsPage').then((module) => ({ default: module.RemoteSessionsPage })));
export const AppsPage = lazy(() => import('./components/pages/AppsPage').then((module) => ({ default: module.AppsPage })));
export const SkillsPage = lazy(() => import('./components/pages/SkillsPage').then((module) => ({ default: module.SkillsPage })));
export const MCPPage = lazy(() => import('./components/pages/MCPPage').then((module) => ({ default: module.MCPPage })));
export const GossipPage = lazy(() => import('./components/pages/GossipPage').then((module) => ({ default: module.GossipPage })));
export const WorkflowsPage = lazy(() => import('./components/pages/WorkflowsPage').then((module) => ({ default: module.WorkflowsPage })));
export const UtilitiesPage = lazy(() => import('./components/pages/UtilitiesPage').then((module) => ({ default: module.UtilitiesPage })));
