// Package i18n provides lightweight internationalisation for user-facing
// progress and status messages across IM channels (WeChat, Telegram, QQ, Feishu).
//
// Usage:
//
//	i18n.T(i18n.MsgAckProcessing, "en")       // English
//	i18n.T(i18n.MsgAckProcessing, "")         // fallback → zh
//	i18n.Tf(i18n.MsgAgentRoundOf, "en", 2, 5) // formatted
package i18n

import "fmt"

// ---------------------------------------------------------------------------
// Translation key constants
// ---------------------------------------------------------------------------

const (
	// im_message_handler.go – progress messages
	MsgAckProcessing = "msg.ack_processing"
	MsgTaskComplex   = "msg.task_complex"
	MsgAgentRoundOf  = "msg.agent_round_of"   // with max: %d/%d
	MsgAgentRound    = "msg.agent_round"      // without max: %d
	MsgRoundsExhaust = "msg.rounds_exhausted" // rounds used up
	MsgMaxRounds     = "msg.max_rounds"       // max rounds reached hint

	// im_message_handler.go – inferFileDeliveryMessage
	MsgFileRequirements = "msg.file_requirements"
	MsgFileDesign       = "msg.file_design"
	MsgFileTaskList     = "msg.file_task_list"
	MsgFileGeneric      = "msg.file_generic" // %s filename

	// gateway – LLM / Hub status
	MsgLLMNotConfigured   = "msg.llm_not_configured"
	MsgHubUnavailable     = "msg.hub_unavailable"
	MsgProgressPrefix     = "msg.progress_prefix"
	MsgMediaSingle        = "msg.media_single"
	MsgMediaMultiple      = "msg.media_multiple" // %d count
	MsgMessageQueued      = "msg.message_queued"
	MsgNoOnlineDevices    = "msg.no_online_devices"
	MsgLLMConcurrencyFull = "msg.llm_concurrency_full"
	MsgMultiDeviceReply   = "msg.multi_device_reply"
	MsgGroupChatReply     = "msg.group_chat_reply"
	MsgStallSuspected     = "msg.stall_suspected"
	MsgStallStuck         = "msg.stall_stuck"
	MsgToolWorking        = "msg.tool_working"
	MsgWaitingInput       = "msg.waiting_input"
	MsgSessionExited      = "msg.session_exited"
	MsgSessionError       = "msg.session_error"

	// TUI shared labels/views
	MsgTUITabSessions                     = "msg.tui_tab_sessions"
	MsgTUITabTools                        = "msg.tui_tab_tools"
	MsgTUITabSchedule                     = "msg.tui_tab_schedule"
	MsgTUITabMemory                       = "msg.tui_tab_memory"
	MsgTUITabAudit                        = "msg.tui_tab_audit"
	MsgTUITabAgentNet                     = "msg.tui_tab_agentnet"
	MsgTUITabConfig                       = "msg.tui_tab_config"
	MsgTUITabChat                         = "msg.tui_tab_chat"
	MsgTUIInitializing                    = "msg.tui_initializing"
	MsgTUIReady                           = "msg.tui_ready"
	MsgTUIKernelInitFailed                = "msg.tui_kernel_init_failed"
	MsgTUIConfigReloading                 = "msg.tui_config_reloading"
	MsgTUIToolStatusRefreshed             = "msg.tui_tool_status_refreshed"
	MsgTUIAIThinking                      = "msg.tui_ai_thinking"
	MsgTUIChatHistoryCleared              = "msg.tui_chat_history_cleared"
	MsgTUIToolExited                      = "msg.tui_tool_exited"
	MsgTUIToolExitedWithError             = "msg.tui_tool_exited_with_error"
	MsgTUISessionCreated                  = "msg.tui_session_created"
	MsgTUISessionTitle                    = "msg.tui_session_title"
	MsgTUILinesStatus                     = "msg.tui_lines_status"
	MsgTUIStatusConnectedHub              = "msg.tui_status_connected_hub"
	MsgTUIStatusConnectingHub             = "msg.tui_status_connecting_hub"
	MsgTUIStatusDisconnectedHub           = "msg.tui_status_disconnected_hub"
	MsgTUIStatusBarHelp                   = "msg.tui_status_bar_help"
	MsgTUIToolHeaderName                  = "msg.tui_tool_header_name"
	MsgTUIToolHeaderStatus                = "msg.tui_tool_header_status"
	MsgTUIToolHeaderVersion               = "msg.tui_tool_header_version"
	MsgTUIToolHeaderPath                  = "msg.tui_tool_header_path"
	MsgTUIToolDetecting                   = "msg.tui_tool_detecting"
	MsgTUIToolNone                        = "msg.tui_tool_none"
	MsgTUIToolNotInstalled                = "msg.tui_tool_not_installed"
	MsgTUIToolReady                       = "msg.tui_tool_ready"
	MsgTUIToolFooter                      = "msg.tui_tool_footer"
	MsgTUISessionLoading                  = "msg.tui_session_loading"
	MsgTUISessionEmpty                    = "msg.tui_session_empty"
	MsgTUISessionHeaderID                 = "msg.tui_session_header_id"
	MsgTUISessionHeaderTool               = "msg.tui_session_header_tool"
	MsgTUISessionHeaderStatus             = "msg.tui_session_header_status"
	MsgTUISessionHeaderTitle              = "msg.tui_session_header_title"
	MsgTUISessionFooter                   = "msg.tui_session_footer"
	MsgTUIStatusRunning                   = "msg.tui_status_running"
	MsgTUIStatusStopped                   = "msg.tui_status_stopped"
	MsgTUIStatusError                     = "msg.tui_status_error"
	MsgTUISessionCreateTitle              = "msg.tui_session_create_title"
	MsgTUISessionCreateTool               = "msg.tui_session_create_tool"
	MsgTUISessionCreateProject            = "msg.tui_session_create_project"
	MsgTUISessionCreateProjectPlaceholder = "msg.tui_session_create_project_placeholder"
	MsgTUISessionCreateNoTools            = "msg.tui_session_create_no_tools"
	MsgTUISessionCreateToolCount          = "msg.tui_session_create_tool_count"
	MsgTUISessionCreateFooter             = "msg.tui_session_create_footer"
	MsgTUIScheduleLoading                 = "msg.tui_schedule_loading"
	MsgTUIScheduleEmpty                   = "msg.tui_schedule_empty"
	MsgTUIScheduleHeaderName              = "msg.tui_schedule_header_name"
	MsgTUIScheduleHeaderStatus            = "msg.tui_schedule_header_status"
	MsgTUIScheduleHeaderTime              = "msg.tui_schedule_header_time"
	MsgTUIScheduleHeaderRuns              = "msg.tui_schedule_header_runs"
	MsgTUIScheduleHeaderAction            = "msg.tui_schedule_header_action"
	MsgTUIScheduleFooter                  = "msg.tui_schedule_footer"
	MsgTUITaskSubRemote                   = "msg.tui_task_sub_remote"
	MsgTUITaskSubBackground               = "msg.tui_task_sub_background"
	MsgTUITaskSubScheduled                = "msg.tui_task_sub_scheduled"
	MsgTUITaskRemoteEmpty                 = "msg.tui_task_remote_empty"
	MsgTUITaskBackgroundEmpty             = "msg.tui_task_background_empty"
	MsgTUITaskRemoteFooter                = "msg.tui_task_remote_footer"
	MsgTUITaskBackgroundFooter            = "msg.tui_task_background_footer"
	MsgTUILoopManagerUnavailable          = "msg.tui_loop_manager_unavailable"
	MsgTUILoopNoTasks                     = "msg.tui_loop_no_tasks"
	MsgTUILoopTaskNotFound                = "msg.tui_loop_task_not_found"
	MsgTUILoopTaskStopped                 = "msg.tui_loop_task_stopped"
	MsgTUILoopTaskContinued               = "msg.tui_loop_task_continued"
	MsgTUITelegramHubForwardUnsupported   = "msg.tui_telegram_hub_forward_unsupported"
	MsgTUIConfigLoadFailed                = "msg.tui_config_load_failed"
	MsgTUIBuildEnvFailed                  = "msg.tui_build_env_failed"
	MsgTUIToolMissing                     = "msg.tui_tool_missing"
	MsgTUIYoloBlocked                     = "msg.tui_yolo_blocked"
	MsgTUIMemoryLoading                   = "msg.tui_memory_loading"
	MsgTUIMemoryEmpty                     = "msg.tui_memory_empty"
	MsgTUIMemoryHeaderCategory            = "msg.tui_memory_header_category"
	MsgTUIMemoryHeaderAccess              = "msg.tui_memory_header_access"
	MsgTUIMemoryHeaderContent             = "msg.tui_memory_header_content"
	MsgTUIMemoryFooter                    = "msg.tui_memory_footer"
	MsgTUIAuditFilterPlaceholder          = "msg.tui_audit_filter_placeholder"
	MsgTUIAuditLoading                    = "msg.tui_audit_loading"
	MsgTUIAuditEmpty                      = "msg.tui_audit_empty"
	MsgTUIAuditFilterLabel                = "msg.tui_audit_filter_label"
	MsgTUIAuditFilterSummary              = "msg.tui_audit_filter_summary"
	MsgTUIAuditHeaderTime                 = "msg.tui_audit_header_time"
	MsgTUIAuditHeaderTool                 = "msg.tui_audit_header_tool"
	MsgTUIAuditHeaderRisk                 = "msg.tui_audit_header_risk"
	MsgTUIAuditHeaderPolicy               = "msg.tui_audit_header_policy"
	MsgTUIAuditHeaderResult               = "msg.tui_audit_header_result"
	MsgTUIAuditFooter                     = "msg.tui_audit_footer"
	MsgTUIChatInputPlaceholder            = "msg.tui_chat_input_placeholder"
	MsgTUIChatSystemReady                 = "msg.tui_chat_system_ready"
	MsgTUIChatError                       = "msg.tui_chat_error"
	MsgTUIChatClearedMessage              = "msg.tui_chat_cleared_message"
	MsgTUIChatModeSimple                  = "msg.tui_chat_mode_simple"
	MsgTUIChatModeAgent                   = "msg.tui_chat_mode_agent"
	MsgTUIChatModeSwitched                = "msg.tui_chat_mode_switched"
	MsgTUIChatUserPrefix                  = "msg.tui_chat_user_prefix"
	MsgTUIChatAssistantPrefix             = "msg.tui_chat_assistant_prefix"
	MsgTUIChatWaiting                     = "msg.tui_chat_waiting"
	MsgTUIChatSpinnerLabel                = "msg.tui_chat_spinner_label"
	MsgTUIChatHint                        = "msg.tui_chat_hint"
	MsgTUIChatAwaitingResponse            = "msg.tui_chat_awaiting_response"
	MsgTUIChatModeLabelSimple             = "msg.tui_chat_mode_label_simple"
	MsgTUIChatModeLabelAgent              = "msg.tui_chat_mode_label_agent"
	MsgTUIChatMessageCount                = "msg.tui_chat_message_count"
	MsgTUIHelpTitle                       = "msg.tui_help_title"
	MsgTUIHelpSectionGlobal               = "msg.tui_help_section_global"
	MsgTUIHelpSectionListNavigation       = "msg.tui_help_section_list_navigation"
	MsgTUIHelpSectionSessions             = "msg.tui_help_section_sessions"
	MsgTUIHelpSectionScheduledTasks       = "msg.tui_help_section_scheduled_tasks"
	MsgTUIHelpSectionMemory               = "msg.tui_help_section_memory"
	MsgTUIHelpSectionConfig               = "msg.tui_help_section_config"
	MsgTUIHelpSectionAgentNet             = "msg.tui_help_section_agentnet"
	MsgTUIHelpSectionSessionDetail        = "msg.tui_help_section_session_detail"
	MsgTUIHelpSectionAIAssistant          = "msg.tui_help_section_ai_assistant"
	MsgTUIHelpDescNextTab                 = "msg.tui_help_desc_next_tab"
	MsgTUIHelpDescPreviousTab             = "msg.tui_help_desc_previous_tab"
	MsgTUIHelpDescQuit                    = "msg.tui_help_desc_quit"
	MsgTUIHelpDescForceQuit               = "msg.tui_help_desc_force_quit"
	MsgTUIHelpDescShowCloseHelp           = "msg.tui_help_desc_show_close_help"
	MsgTUIHelpDescMoveUp                  = "msg.tui_help_desc_move_up"
	MsgTUIHelpDescMoveDown                = "msg.tui_help_desc_move_down"
	MsgTUIHelpDescJumpTop                 = "msg.tui_help_desc_jump_top"
	MsgTUIHelpDescJumpBottom              = "msg.tui_help_desc_jump_bottom"
	MsgTUIHelpDescRefresh                 = "msg.tui_help_desc_refresh"
	MsgTUIHelpDescViewDetails             = "msg.tui_help_desc_view_details"
	MsgTUIHelpDescNewSession              = "msg.tui_help_desc_new_session"
	MsgTUIHelpDescTerminateSession        = "msg.tui_help_desc_terminate_session"
	MsgTUIHelpDescPauseResume             = "msg.tui_help_desc_pause_resume"
	MsgTUIHelpDescDelete                  = "msg.tui_help_desc_delete"
	MsgTUIHelpDescEdit                    = "msg.tui_help_desc_edit"
	MsgTUIHelpDescCancelEdit              = "msg.tui_help_desc_cancel_edit"
	MsgTUIHelpDescSwitchSubTab            = "msg.tui_help_desc_switch_sub_tab"
	MsgTUIHelpDescScroll                  = "msg.tui_help_desc_scroll"
	MsgTUIHelpDescTopBottom               = "msg.tui_help_desc_top_bottom"
	MsgTUIHelpDescBackToList              = "msg.tui_help_desc_back_to_list"
	MsgTUIHelpDescStartInput              = "msg.tui_help_desc_start_input"
	MsgTUIHelpDescSendMessage             = "msg.tui_help_desc_send_message"
	MsgTUIHelpDescExitInput               = "msg.tui_help_desc_exit_input"
	MsgTUIHelpDescClearHistory            = "msg.tui_help_desc_clear_history"
	MsgTUIHelpDescScrollMessages          = "msg.tui_help_desc_scroll_messages"
	MsgTUIHelpClose                       = "msg.tui_help_close"
	MsgTUIPythonEnvError                  = "msg.tui_python_env_error"
	MsgTUIPythonEnvReady                  = "msg.tui_python_env_ready"
	MsgTUIPythonAvailable                 = "msg.tui_python_available"
	MsgTUIConfigSaveFailed                = "msg.tui_config_save_failed"
	MsgTUIConfigSaved                     = "msg.tui_config_saved"
	MsgTUIMemoryCompressHint              = "msg.tui_memory_compress_hint"
	MsgTUIMemoryBackupListHint            = "msg.tui_memory_backup_list_hint"
	MsgTUISessionMonitorEvent             = "msg.tui_session_monitor_event"
	MsgTUIErrorExit                       = "msg.tui_error_exit"
	MsgTUILLMNotConfiguredHint            = "msg.tui_llm_not_configured_hint"
	MsgTUIConfigTitle                     = "msg.tui_config_title"
	MsgTUIConfigNotSet                    = "msg.tui_config_not_set"
	MsgTUIConfigFooterEditing             = "msg.tui_config_footer_editing"
	MsgTUIConfigFooterNormal              = "msg.tui_config_footer_normal"
	MsgTUIConfigFooterSelect              = "msg.tui_config_footer_select"
	MsgTUIConfigDescHubURL                = "msg.tui_config_desc_hub_url"
	MsgTUIConfigDescToken                 = "msg.tui_config_desc_token"
	MsgTUIConfigDescDataDir               = "msg.tui_config_desc_data_dir"
	MsgTUIConfigDescMaxIterations         = "msg.tui_config_desc_max_iterations"
	MsgTUIConfigDescAgentNetEnabled       = "msg.tui_config_desc_agentnet_enabled"
	MsgTUIConfigDescLLMProviderPreset     = "msg.tui_config_desc_llm_provider_preset"
	MsgTUIConfigDescLLMURL                = "msg.tui_config_desc_llm_url"
	MsgTUIConfigDescLLMKey                = "msg.tui_config_desc_llm_key"
	MsgTUIConfigDescLLMModel              = "msg.tui_config_desc_llm_model"
	MsgTUIConfigDescLLMProtocol           = "msg.tui_config_desc_llm_protocol"
	MsgTUIConfigDescLLMContextLength      = "msg.tui_config_desc_llm_context_length"
	MsgTUIConfigDescQQBotEnabled          = "msg.tui_config_desc_qqbot_enabled"
	MsgTUIConfigDescQQBotAppID            = "msg.tui_config_desc_qqbot_app_id"
	MsgTUIConfigDescQQBotAppSecret        = "msg.tui_config_desc_qqbot_app_secret"
	MsgTUIConfigDescTelegramEnabled       = "msg.tui_config_desc_telegram_enabled"
	MsgTUIConfigDescTelegramToken         = "msg.tui_config_desc_telegram_token"
	MsgTUIConfigDescSkillPurchaseMode     = "msg.tui_config_desc_skill_purchase_mode"
	// Config tab names
	MsgTUIConfigTabGeneral  = "msg.tui_config_tab_general"
	MsgTUIConfigTabLLM      = "msg.tui_config_tab_llm"
	MsgTUIConfigTabIM       = "msg.tui_config_tab_im"
	MsgTUIConfigTabProxy    = "msg.tui_config_tab_proxy"
	MsgTUIConfigTabSecurity = "msg.tui_config_tab_security"
	MsgTUIConfigTabAdvanced = "msg.tui_config_tab_advanced"
	// New config field descriptions
	MsgTUIConfigDescWorkDir            = "msg.tui_config_desc_work_dir"
	MsgTUIConfigDescLanguage           = "msg.tui_config_desc_language"
	MsgTUIConfigDescCheckUpdate        = "msg.tui_config_desc_check_update"
	MsgTUIConfigDescAuxLLMURL          = "msg.tui_config_desc_aux_llm_url"
	MsgTUIConfigDescAuxLLMKey          = "msg.tui_config_desc_aux_llm_key"
	MsgTUIConfigDescAuxLLMModel        = "msg.tui_config_desc_aux_llm_model"
	MsgTUIConfigDescAuxLLMProtocol     = "msg.tui_config_desc_aux_llm_protocol"
	MsgTUIConfigDescWeixinEnabled      = "msg.tui_config_desc_weixin_enabled"
	MsgTUIConfigDescWeixinToken        = "msg.tui_config_desc_weixin_token"
	MsgTUIConfigDescWeixinBaseURL      = "msg.tui_config_desc_weixin_base_url"
	MsgTUIConfigDescLansengerEnabled   = "msg.tui_config_desc_lansenger_enabled"
	MsgTUIConfigDescLansengerAppID     = "msg.tui_config_desc_lansenger_app_id"
	MsgTUIConfigDescLansengerAppSecret = "msg.tui_config_desc_lansenger_app_secret"
	MsgTUIConfigDescLansengerGateway   = "msg.tui_config_desc_lansenger_gateway"
	MsgTUIConfigDescProxyEnabled       = "msg.tui_config_desc_proxy_enabled"
	MsgTUIConfigDescProxyProtocol      = "msg.tui_config_desc_proxy_protocol"
	MsgTUIConfigDescProxyHost          = "msg.tui_config_desc_proxy_host"
	MsgTUIConfigDescProxyPort          = "msg.tui_config_desc_proxy_port"
	MsgTUIConfigDescProxyUser          = "msg.tui_config_desc_proxy_user"
	MsgTUIConfigDescProxyPass          = "msg.tui_config_desc_proxy_pass"
	MsgTUIConfigDescProxyScopeLLM      = "msg.tui_config_desc_proxy_scope_llm"
	MsgTUIConfigDescProxyScopeAgent    = "msg.tui_config_desc_proxy_scope_agent"
	MsgTUIConfigDescSecurityMode       = "msg.tui_config_desc_security_mode"
	MsgTUIConfigDescSandbox            = "msg.tui_config_desc_sandbox"
	MsgTUIConfigDescNetworkLevel       = "msg.tui_config_desc_network_level"
	MsgTUIConfigDescYoloMode           = "msg.tui_config_desc_yolo_mode"
	MsgTUIConfigDescFileOutbound       = "msg.tui_config_desc_file_outbound"
	MsgTUIConfigDescImageOutbound      = "msg.tui_config_desc_image_outbound"
	MsgTUIConfigDescUIMode             = "msg.tui_config_desc_ui_mode"
	MsgTUIConfigDescMemoryCompress     = "msg.tui_config_desc_memory_compress"
	MsgTUIConfigDescLogDetail          = "msg.tui_config_desc_log_detail"
	MsgTUIConfigDescTrajectory         = "msg.tui_config_desc_trajectory"
	MsgTUIConfigDescDebugTools         = "msg.tui_config_desc_debug_tools"
	MsgTUIConfigDescGossip             = "msg.tui_config_desc_gossip"
	MsgTUIConfigDescTrialReflect       = "msg.tui_config_desc_trial_reflect"
	MsgTUIAgentNetLoading              = "msg.tui_agentnet_loading"
	MsgTUIAgentNetTabPeers             = "msg.tui_agentnet_tab_peers"
	MsgTUIAgentNetTabTasks             = "msg.tui_agentnet_tab_tasks"
	MsgTUIAgentNetTabStatus            = "msg.tui_agentnet_tab_status"
	MsgTUIAgentNetNoPeers              = "msg.tui_agentnet_no_peers"
	MsgTUIAgentNetNoTasks              = "msg.tui_agentnet_no_tasks"
	MsgTUIAgentNetHeaderPeerID         = "msg.tui_agentnet_header_peer_id"
	MsgTUIAgentNetHeaderAddr           = "msg.tui_agentnet_header_addr"
	MsgTUIAgentNetHeaderLatency        = "msg.tui_agentnet_header_latency"
	MsgTUIAgentNetHeaderCountry        = "msg.tui_agentnet_header_country"
	MsgTUIAgentNetHeaderReward         = "msg.tui_agentnet_header_reward"
	MsgTUIAgentNetFooterPeers          = "msg.tui_agentnet_footer_peers"
	MsgTUIAgentNetFooterTasks          = "msg.tui_agentnet_footer_tasks"
	MsgTUIAgentNetStatusTitle          = "msg.tui_agentnet_status_title"
	MsgTUIAgentNetCreditsTitle         = "msg.tui_agentnet_credits_title"
	MsgTUIAgentNetStatusPeerID         = "msg.tui_agentnet_status_peer_id"
	MsgTUIAgentNetStatusPeers          = "msg.tui_agentnet_status_peers"
	MsgTUIAgentNetStatusUnread         = "msg.tui_agentnet_status_unread"
	MsgTUIAgentNetStatusVersion        = "msg.tui_agentnet_status_version"
	MsgTUIAgentNetStatusUptime         = "msg.tui_agentnet_status_uptime"
	MsgTUIAgentNetStatusBalance        = "msg.tui_agentnet_status_balance"
	MsgTUIAgentNetStatusTier           = "msg.tui_agentnet_status_tier"
	MsgTUIAgentNetStatusEnergy         = "msg.tui_agentnet_status_energy"
	MsgTUIAgentNetFooterStatus         = "msg.tui_agentnet_footer_status"
	MsgTUIAgentLoadConfigFailed        = "msg.tui_agent_load_config_failed"
	MsgTUIAgentLLMCallFailed           = "msg.tui_agent_llm_call_failed"
	MsgTUIAgentNoValidReply            = "msg.tui_agent_no_valid_reply"
	MsgTUIAgentTruncated               = "msg.tui_agent_truncated"
	MsgTUIAgentMaxRoundsReached        = "msg.tui_agent_max_rounds_reached"
)

// defaultLang is the fallback language when lang is empty or unknown.
const defaultLang = "zh"

// ---------------------------------------------------------------------------
// Translation tables
// ---------------------------------------------------------------------------

// translations maps lang → key → translated string.
// Format verbs (e.g. %d, %s) are preserved for use with Tf.
var translations = map[string]map[string]string{
	"zh": {
		MsgAckProcessing:                      "⏳ 需要一点时间处理，请稍候...",
		MsgTaskComplex:                        "⏳ 任务较复杂，仍在处理中，请稍候...",
		MsgAgentRoundOf:                       "🔄 Agent 推理中（第 %d/%d 轮）…",
		MsgAgentRound:                         "🔄 Agent 推理中（第 %d 轮）…",
		MsgRoundsExhaust:                      "⏳ 推理轮次已用完，但编程会话仍在运行，正在检查状态…",
		MsgMaxRounds:                          "(已达到最大推理轮次，请继续发送消息以完成任务)",
		MsgFileRequirements:                   "📋 需求文档已生成，请查看并确认需求是否准确，或提出修改意见。\n\n请输入：确认 或 修改意见",
		MsgFileDesign:                         "🏗️ 技术设计文档已生成，请查看设计方案并确认，或提出修改意见。\n\n请输入：确认 或 修改意见",
		MsgFileTaskList:                       "📝 任务列表已生成，请查看任务拆分是否合理，确认后开始执行。\n\n请输入：确认 或 修改意见",
		MsgFileGeneric:                        "📄 已生成文件 %s，请查看并确认，或提出修改意见。\n\n请输入：确认 或 修改意见",
		MsgLLMNotConfigured:                   "⚠️ 本地 LLM 未配置，请先在设置中配置 MaClaw LLM。",
		MsgHubUnavailable:                     "⚠️ 当前为多机模式，但 Hub 未连接。消息已回退到本地处理。\n请检查 Hub 连接状态，或切换回单机模式。",
		MsgProgressPrefix:                     "⏳ ",
		MsgMediaSingle:                        "📎 收到文件/图片了，请告诉我你希望怎么处理",
		MsgMediaMultiple:                      "📎 收到 %d 个文件/图片了，请告诉我你希望怎么处理",
		MsgMessageQueued:                      "⏳ 上一条消息还在处理中，你的消息已排队，请稍候…",
		MsgNoOnlineDevices:                    "📴 当前没有在线设备。",
		MsgLLMConcurrencyFull:                 "LLM 并发已满，请稍后重试",
		MsgMultiDeviceReply:                   "多设备回复",
		MsgGroupChatReply:                     "群聊回复",
		MsgStallSuspected:                     "⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查",
		MsgStallStuck:                         "⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话",
		MsgToolWorking:                        "⏳ 编程工具正在工作中，请等待后再检查进度",
		MsgWaitingInput:                       "⚠️ 会话正在等待用户输入",
		MsgSessionExited:                      "会话已退出",
		MsgSessionError:                       "⚠️ 会话出错",
		MsgTUITabSessions:                     "编程",
		MsgTUITabTools:                        "工具",
		MsgTUITabSchedule:                     "任务",
		MsgTUITabMemory:                       "记忆",
		MsgTUITabAudit:                        "审计",
		MsgTUITabAgentNet:                     "智网",
		MsgTUITabConfig:                       "配置",
		MsgTUITabChat:                         "助手",
		MsgTUIInitializing:                    "正在初始化...",
		MsgTUIReady:                           "就绪",
		MsgTUIKernelInitFailed:                "内核初始化失败: %v",
		MsgTUIConfigReloading:                 "配置文件已变更，正在重新加载...",
		MsgTUIToolStatusRefreshed:             "工具状态已刷新",
		MsgTUIAIThinking:                      "AI 思考中...",
		MsgTUIChatHistoryCleared:              "聊天历史已清空",
		MsgTUIToolExited:                      "工具 %s 已退出",
		MsgTUIToolExitedWithError:             "工具 %s 退出: %v",
		MsgTUISessionCreated:                  "创建会话: tool=%s project=%s",
		MsgTUISessionTitle:                    "会话: %s  [%s]  %s",
		MsgTUILinesStatus:                     "行 %d/%d  ↑↓:滚动  g/G:首/尾  Esc:返回",
		MsgTUIStatusConnectedHub:              "已连接 Hub",
		MsgTUIStatusConnectingHub:             "连接 Hub 中",
		MsgTUIStatusDisconnectedHub:           "未连接 Hub",
		MsgTUIStatusBarHelp:                   "Tab:切换 q:退出",
		MsgTUIToolHeaderName:                  "工具",
		MsgTUIToolHeaderStatus:                "状态",
		MsgTUIToolHeaderVersion:               "版本",
		MsgTUIToolHeaderPath:                  "路径",
		MsgTUIToolDetecting:                   "正在检测工具状态...",
		MsgTUIToolNone:                        "未检测到任何工具",
		MsgTUIToolNotInstalled:                "✗ 未安装",
		MsgTUIToolReady:                       "✓ 就绪 ",
		MsgTUIToolFooter:                      "↑↓:选择  r:刷新  Enter:启动",
		MsgTUISessionLoading:                  "正在加载会话列表...",
		MsgTUISessionEmpty:                    "暂无活跃会话\n\n按 Enter 创建新会话",
		MsgTUISessionHeaderID:                 "ID",
		MsgTUISessionHeaderTool:               "工具",
		MsgTUISessionHeaderStatus:             "状态",
		MsgTUISessionHeaderTitle:              "标题",
		MsgTUISessionFooter:                   "↑↓:选择  Enter:详情  n:新建  d:终止",
		MsgTUIStatusRunning:                   "● 运行中",
		MsgTUIStatusStopped:                   "○ 已停止",
		MsgTUIStatusError:                     "✗ 错误",
		MsgTUISessionCreateTitle:              "╭─ 新建会话 ─╮",
		MsgTUISessionCreateTool:               "工具选择:",
		MsgTUISessionCreateProject:            "项目路径:",
		MsgTUISessionCreateProjectPlaceholder: "项目路径（可选）",
		MsgTUISessionCreateNoTools:            "(无可用工具)",
		MsgTUISessionCreateToolCount:          "... 共 %d 个工具",
		MsgTUISessionCreateFooter:             "Tab:切换字段  ↑↓:选工具  Enter:确认/下一步  Esc:取消",
		MsgTUIScheduleLoading:                 "正在加载定时任务...",
		MsgTUIScheduleEmpty:                   "暂无定时任务\n\n使用 CLI 创建: maclaw-tui schedule create --name <name> --action <text>",
		MsgTUIScheduleHeaderName:              "NAME",
		MsgTUIScheduleHeaderStatus:            "STATUS",
		MsgTUIScheduleHeaderTime:              "TIME",
		MsgTUIScheduleHeaderRuns:              "RUNS",
		MsgTUIScheduleHeaderAction:            "ACTION",
		MsgTUIScheduleFooter:                  "↑↓:选择  p:暂停/恢复  d:删除  1/2/3:切换子标签",
		MsgTUITaskSubRemote:                   "远程",
		MsgTUITaskSubBackground:               "后台",
		MsgTUITaskSubScheduled:                "计划任务",
		MsgTUITaskRemoteEmpty:                 "暂无远程任务\n\n通过 SSH 工具提交的远程后台任务将显示在此处",
		MsgTUITaskBackgroundEmpty:             "暂无后台任务\n\n通过 Agent 启动的后台循环任务将显示在此处",
		MsgTUITaskRemoteFooter:                "↑↓:选择  Enter:详情  1/2/3:切换子标签",
		MsgTUITaskBackgroundFooter:            "↑↓:选择  Enter:详情  s:停止  1/2/3:切换子标签",
		MsgTUILoopManagerUnavailable:          "后台任务管理器未初始化（需要在 daemon 或 TUI 模式下运行）",
		MsgTUILoopNoTasks:                     "无运行中的后台任务。",
		MsgTUILoopTaskNotFound:                "后台任务 '%s' 不存在",
		MsgTUILoopTaskStopped:                 "后台任务 '%s' 已停止。",
		MsgTUILoopTaskContinued:               "已向后台任务 '%s' 追加 %d 轮迭代。",
		MsgTUITelegramHubForwardUnsupported:   "⚠️ TUI 模式暂不支持 Hub 转发，请使用 GUI 客户端。",
		MsgTUIConfigLoadFailed:                "加载配置失败: %w",
		MsgTUIBuildEnvFailed:                  "构建环境变量失败: %w",
		MsgTUIToolMissing:                     "工具 %s 未安装（在 %s 中未找到）",
		MsgTUIYoloBlocked:                     "⚠ YOLO 模式已被 Hub 安全策略禁止，将以普通模式启动",
		MsgTUIMemoryLoading:                   "正在加载记忆...",
		MsgTUIMemoryEmpty:                     "暂无记忆\n\n使用 CLI 保存: maclaw-tui memory save --content <text>",
		MsgTUIMemoryHeaderCategory:            "CATEGORY",
		MsgTUIMemoryHeaderAccess:              "ACCESS",
		MsgTUIMemoryHeaderContent:             "CONTENT",
		MsgTUIMemoryFooter:                    "↑↓:选择  d:删除  c:压缩  b:备份列表",
		MsgTUIAuditFilterPlaceholder:          "输入工具名或风险等级过滤...",
		MsgTUIAuditLoading:                    "正在加载审计日志...",
		MsgTUIAuditEmpty:                      "暂无审计日志\n\n使用 CLI 查询: maclaw-tui audit list",
		MsgTUIAuditFilterLabel:                "过滤: ",
		MsgTUIAuditFilterSummary:              "过滤: %s (/:修改  Esc:清除)",
		MsgTUIAuditHeaderTime:                 "TIME",
		MsgTUIAuditHeaderTool:                 "TOOL",
		MsgTUIAuditHeaderRisk:                 "RISK",
		MsgTUIAuditHeaderPolicy:               "POLICY",
		MsgTUIAuditHeaderResult:               "RESULT",
		MsgTUIAuditFooter:                     "共 %d/%d 条  ↑↓:选择  g/G:首/尾  /:过滤  r:刷新",
		MsgTUIChatInputPlaceholder:            "输入消息... (Enter 发送)",
		MsgTUIChatSystemReady:                 "AI 助手就绪 [Agent 模式]。支持工具调用（bash/文件操作/会话管理）。",
		MsgTUIChatError:                       "错误: %s",
		MsgTUIChatClearedMessage:              "聊天历史已清除。",
		MsgTUIChatModeSimple:                  "简单问答",
		MsgTUIChatModeAgent:                   "Agent（工具调用）",
		MsgTUIChatModeSwitched:                "已切换到 %s 模式",
		MsgTUIChatUserPrefix:                  "你: ",
		MsgTUIChatAssistantPrefix:             "AI: ",
		MsgTUIChatWaiting:                     "  ⏳ 思考中...",
		MsgTUIChatSpinnerLabel:                "思考中...",
		MsgTUIChatHint:                        "i:输入  Enter:发送  Esc:退出输入  c:清除  a:切换模式  ↑↓:滚动",
		MsgTUIChatAwaitingResponse:            "等待响应中...",
		MsgTUIChatModeLabelSimple:             "问答",
		MsgTUIChatModeLabelAgent:              "Agent",
		MsgTUIChatMessageCount:                "消息:%d",
		MsgTUIHelpTitle:                       "  -- 快捷键 --",
		MsgTUIHelpSectionGlobal:               "全局",
		MsgTUIHelpSectionListNavigation:       "列表导航",
		MsgTUIHelpSectionSessions:             "编程",
		MsgTUIHelpSectionScheduledTasks:       "任务",
		MsgTUIHelpSectionMemory:               "记忆",
		MsgTUIHelpSectionConfig:               "配置",
		MsgTUIHelpSectionAgentNet:             "智网",
		MsgTUIHelpSectionSessionDetail:        "会话详情",
		MsgTUIHelpSectionAIAssistant:          "AI 助手",
		MsgTUIHelpDescNextTab:                 "下一个标签页",
		MsgTUIHelpDescPreviousTab:             "上一个标签页",
		MsgTUIHelpDescQuit:                    "退出",
		MsgTUIHelpDescForceQuit:               "强制退出",
		MsgTUIHelpDescShowCloseHelp:           "显示/关闭帮助",
		MsgTUIHelpDescMoveUp:                  "上移",
		MsgTUIHelpDescMoveDown:                "下移",
		MsgTUIHelpDescJumpTop:                 "跳到顶部",
		MsgTUIHelpDescJumpBottom:              "跳到底部",
		MsgTUIHelpDescRefresh:                 "刷新",
		MsgTUIHelpDescViewDetails:             "查看详情",
		MsgTUIHelpDescNewSession:              "新建会话",
		MsgTUIHelpDescTerminateSession:        "终止会话",
		MsgTUIHelpDescPauseResume:             "暂停/恢复",
		MsgTUIHelpDescDelete:                  "删除",
		MsgTUIHelpDescEdit:                    "编辑",
		MsgTUIHelpDescCancelEdit:              "取消编辑",
		MsgTUIHelpDescSwitchSubTab:            "切换子标签页",
		MsgTUIHelpDescScroll:                  "滚动",
		MsgTUIHelpDescTopBottom:               "顶部/底部",
		MsgTUIHelpDescBackToList:              "返回列表",
		MsgTUIHelpDescStartInput:              "开始输入",
		MsgTUIHelpDescSendMessage:             "发送消息",
		MsgTUIHelpDescExitInput:               "退出输入",
		MsgTUIHelpDescClearHistory:            "清空历史",
		MsgTUIHelpDescScrollMessages:          "滚动消息",
		MsgTUIHelpClose:                       "按 ? 或 Esc 关闭",
		MsgTUIPythonEnvError:                  "⚠ Python 环境异常: %s",
		MsgTUIPythonEnvReady:                  "🐍 Python %s 就绪 (venv: %s)",
		MsgTUIPythonAvailable:                 "🐍 Python %s 可用",
		MsgTUIConfigSaveFailed:                "保存失败: %s: %v",
		MsgTUIConfigSaved:                     "已保存: %s = %s",
		MsgTUIMemoryCompressHint:              "记忆压缩中... 请使用 CLI: maclaw-tui memory compress",
		MsgTUIMemoryBackupListHint:            "备份列表请使用 CLI: maclaw-tui memory backup list",
		MsgTUISessionMonitorEvent:             "🔔 [%s] %s",
		MsgTUIErrorExit:                       "错误: %v\n\n按 q 退出\n",
		MsgTUILLMNotConfiguredHint:            "LLM 未配置。运行 maclaw llm setup 快速配置。",
		MsgTUIConfigTitle:                     "配置",
		MsgTUIConfigNotSet:                    "(未设置)",
		MsgTUIConfigFooterEditing:             "Enter:确认  Esc:取消",
		MsgTUIConfigFooterNormal:              "1-6:切换标签  ↑↓:移动  Enter:编辑  Space:切换开关",
		MsgTUIConfigFooterSelect:              "←→:选择  Enter:确认  Esc:取消",
		MsgTUIConfigDescHubURL:                "Hub 服务地址",
		MsgTUIConfigDescToken:                 "认证令牌",
		MsgTUIConfigDescDataDir:               "数据目录",
		MsgTUIConfigDescMaxIterations:         "Agent 最大迭代次数 (30-300)",
		MsgTUIConfigDescAgentNetEnabled:       "启用 AgentNet",
		MsgTUIConfigDescLLMURL:                "LLM API 地址",
		MsgTUIConfigDescLLMKey:                "LLM API 密钥",
		MsgTUIConfigDescLLMModel:              "LLM 模型名称",
		MsgTUIConfigDescLLMProtocol:           "LLM 协议 (openai/anthropic)",
		MsgTUIConfigDescLLMContextLength:      "上下文长度 (tokens)",
		MsgTUIConfigDescQQBotEnabled:          "启用 QQ 机器人",
		MsgTUIConfigDescQQBotAppID:            "QQ 机器人 AppID",
		MsgTUIConfigDescQQBotAppSecret:        "QQ 机器人 AppSecret",
		MsgTUIConfigDescTelegramEnabled:       "启用 Telegram 机器人",
		MsgTUIConfigDescTelegramToken:         "Telegram 机器人 Token",
		MsgTUIConfigDescSkillPurchaseMode:     "技能购买模式 (auto/free_only)",
		// Config tab names
		MsgTUIConfigTabGeneral:  "基本",
		MsgTUIConfigTabLLM:      "LLM",
		MsgTUIConfigTabIM:       "IM通道",
		MsgTUIConfigTabProxy:    "代理",
		MsgTUIConfigTabSecurity: "安全",
		MsgTUIConfigTabAdvanced: "高级",
		// New config field descriptions
		MsgTUIConfigDescWorkDir:            "默认工作目录",
		MsgTUIConfigDescLanguage:           "界面语言",
		MsgTUIConfigDescCheckUpdate:        "启动时检查更新",
		MsgTUIConfigDescAuxLLMURL:          "辅助 LLM API 地址",
		MsgTUIConfigDescAuxLLMKey:          "辅助 LLM API 密钥",
		MsgTUIConfigDescAuxLLMModel:        "辅助 LLM 模型名称",
		MsgTUIConfigDescAuxLLMProtocol:     "辅助 LLM 协议",
		MsgTUIConfigDescWeixinEnabled:      "启用微信",
		MsgTUIConfigDescWeixinToken:        "微信 Token",
		MsgTUIConfigDescWeixinBaseURL:      "微信 API 地址",
		MsgTUIConfigDescLansengerEnabled:   "启用蓝信",
		MsgTUIConfigDescLansengerAppID:     "蓝信 AppID",
		MsgTUIConfigDescLansengerAppSecret: "蓝信 AppSecret",
		MsgTUIConfigDescLansengerGateway:   "蓝信网关地址",
		MsgTUIConfigDescProxyEnabled:       "启用代理",
		MsgTUIConfigDescProxyProtocol:      "代理协议",
		MsgTUIConfigDescProxyHost:          "代理主机",
		MsgTUIConfigDescProxyPort:          "代理端口",
		MsgTUIConfigDescProxyUser:          "代理用户名",
		MsgTUIConfigDescProxyPass:          "代理密码",
		MsgTUIConfigDescProxyScopeLLM:      "代理范围: LLM 请求",
		MsgTUIConfigDescProxyScopeAgent:    "代理范围: Agent 网络",
		MsgTUIConfigDescSecurityMode:       "安全策略模式",
		MsgTUIConfigDescSandbox:            "沙箱模式",
		MsgTUIConfigDescNetworkLevel:       "网络访问级别",
		MsgTUIConfigDescYoloMode:           "允许 YOLO 模式",
		MsgTUIConfigDescFileOutbound:       "允许文件外发",
		MsgTUIConfigDescImageOutbound:      "允许图片外发",
		MsgTUIConfigDescUIMode:             "界面模式 (pro=完整/lite=精简)",
		MsgTUIConfigDescMemoryCompress:     "记忆自动压缩",
		MsgTUIConfigDescLogDetail:          "详细日志",
		MsgTUIConfigDescTrajectory:         "LLM 轨迹记录",
		MsgTUIConfigDescDebugTools:         "调试工具调用",
		MsgTUIConfigDescGossip:             "启用 Gossip",
		MsgTUIConfigDescTrialReflect:       "启用试错反思",
		MsgTUIAgentNetLoading:              "正在加载 AgentNet...",
		MsgTUIAgentNetTabPeers:             "Peers",
		MsgTUIAgentNetTabTasks:             "Tasks",
		MsgTUIAgentNetTabStatus:            "Status",
		MsgTUIAgentNetNoPeers:              "暂无已连接节点",
		MsgTUIAgentNetNoTasks:              "暂无任务",
		MsgTUIAgentNetHeaderPeerID:         "PEER ID",
		MsgTUIAgentNetHeaderAddr:           "ADDR",
		MsgTUIAgentNetHeaderLatency:        "LATENCY",
		MsgTUIAgentNetHeaderCountry:        "COUNTRY",
		MsgTUIAgentNetHeaderReward:         "REWARD",
		MsgTUIAgentNetFooterPeers:          "共 %d 个节点  Up/Down:选择  r:刷新",
		MsgTUIAgentNetFooterTasks:          "共 %d 个任务  Up/Down:选择  r:刷新",
		MsgTUIAgentNetStatusTitle:          "AgentNet 状态",
		MsgTUIAgentNetCreditsTitle:         "积分信息",
		MsgTUIAgentNetStatusPeerID:         "PeerID",
		MsgTUIAgentNetStatusPeers:          "Peers",
		MsgTUIAgentNetStatusUnread:         "Unread",
		MsgTUIAgentNetStatusVersion:        "Version",
		MsgTUIAgentNetStatusUptime:         "Uptime",
		MsgTUIAgentNetStatusBalance:        "Balance",
		MsgTUIAgentNetStatusTier:           "Tier",
		MsgTUIAgentNetStatusEnergy:         "Energy",
		MsgTUIAgentNetFooterStatus:         "r:刷新  1:peers  2:tasks  3:status",
		MsgTUIAgentLoadConfigFailed:        "加载 LLM 配置失败: %v",
		MsgTUIAgentLLMCallFailed:           "LLM 调用失败: %v",
		MsgTUIAgentNoValidReply:            "LLM 未返回有效回复",
		MsgTUIAgentTruncated:               "\n...(已截断)",
		MsgTUIAgentMaxRoundsReached:        "(已达到最大推理轮次)",
	},
	"en": {
		MsgAckProcessing:                      "⏳ Processing, please wait...",
		MsgTaskComplex:                        "⏳ This task is complex, still working on it...",
		MsgAgentRoundOf:                       "🔄 Agent reasoning (round %d/%d)…",
		MsgAgentRound:                         "🔄 Agent reasoning (round %d)…",
		MsgRoundsExhaust:                      "⏳ Reasoning rounds exhausted, but the coding session is still running, checking status…",
		MsgMaxRounds:                          "(Max reasoning rounds reached, please send another message to continue)",
		MsgFileRequirements:                   "📋 Requirements document generated. Please review and confirm, or suggest changes.\n\nPlease reply: confirm or your feedback",
		MsgFileDesign:                         "🏗️ Technical design document generated. Please review and confirm, or suggest changes.\n\nPlease reply: confirm or your feedback",
		MsgFileTaskList:                       "📝 Task list generated. Please review the task breakdown and confirm to start execution.\n\nPlease reply: confirm or your feedback",
		MsgFileGeneric:                        "📄 File %s generated. Please review and confirm, or suggest changes.\n\nPlease reply: confirm or your feedback",
		MsgLLMNotConfigured:                   "⚠️ Local LLM not configured. Please configure MaClaw LLM in settings first.",
		MsgHubUnavailable:                     "⚠️ Multi-device mode is active but Hub is disconnected. Message has been processed locally.\nPlease check Hub connection or switch to standalone mode.",
		MsgProgressPrefix:                     "⏳ ",
		MsgMediaSingle:                        "📎 File/image received. How would you like me to handle it?",
		MsgMediaMultiple:                      "📎 Received %d files/images. How would you like me to handle them?",
		MsgMessageQueued:                      "⏳ Previous message is still being processed. Your message has been queued, please wait…",
		MsgNoOnlineDevices:                    "📴 No devices are currently online.",
		MsgLLMConcurrencyFull:                 "LLM concurrency limit reached, please try again later",
		MsgMultiDeviceReply:                   "Multi-device reply",
		MsgGroupChatReply:                     "Group chat reply",
		MsgStallSuspected:                     "⏳ Coding tool output paused, system is attempting recovery, please check back later",
		MsgStallStuck:                         "⚠️ Coding tool may be stuck. Consider sending a specific command or terminating the session",
		MsgToolWorking:                        "⏳ Coding tool is working, please wait before checking progress",
		MsgWaitingInput:                       "⚠️ Session is waiting for user input",
		MsgSessionExited:                      "Session exited",
		MsgSessionError:                       "⚠️ Session error",
		MsgTUITabSessions:                     "Coding",
		MsgTUITabTools:                        "Tools",
		MsgTUITabSchedule:                     "Tasks",
		MsgTUITabMemory:                       "Memory",
		MsgTUITabAudit:                        "Audit",
		MsgTUITabAgentNet:                     "AgentNet",
		MsgTUITabConfig:                       "Config",
		MsgTUITabChat:                         "Assistant",
		MsgTUIInitializing:                    "Initializing...",
		MsgTUIReady:                           "Ready",
		MsgTUIKernelInitFailed:                "Kernel initialization failed: %v",
		MsgTUIConfigReloading:                 "Config file changed, reloading...",
		MsgTUIToolStatusRefreshed:             "Tool status refreshed",
		MsgTUIAIThinking:                      "AI is thinking...",
		MsgTUIChatHistoryCleared:              "Chat history cleared",
		MsgTUIToolExited:                      "Tool %s exited",
		MsgTUIToolExitedWithError:             "Tool %s exited: %v",
		MsgTUISessionCreated:                  "Session created: tool=%s project=%s",
		MsgTUISessionTitle:                    "Session: %s  [%s]  %s",
		MsgTUILinesStatus:                     "Line %d/%d  ↑↓:scroll  g/G:top/end  Esc:back",
		MsgTUIStatusConnectedHub:              "Hub connected",
		MsgTUIStatusConnectingHub:             "Connecting Hub",
		MsgTUIStatusDisconnectedHub:           "Hub disconnected",
		MsgTUIStatusBarHelp:                   "Tab:switch q:quit",
		MsgTUIToolHeaderName:                  "Tool",
		MsgTUIToolHeaderStatus:                "Status",
		MsgTUIToolHeaderVersion:               "Version",
		MsgTUIToolHeaderPath:                  "Path",
		MsgTUIToolDetecting:                   "Detecting tool status...",
		MsgTUIToolNone:                        "No tools detected",
		MsgTUIToolNotInstalled:                "✗ Missing",
		MsgTUIToolReady:                       "✓ Ready  ",
		MsgTUIToolFooter:                      "↑↓:select  r:refresh  Enter:launch",
		MsgTUISessionLoading:                  "Loading sessions...",
		MsgTUISessionEmpty:                    "No active sessions\n\nPress Enter to create one",
		MsgTUISessionHeaderID:                 "ID",
		MsgTUISessionHeaderTool:               "Tool",
		MsgTUISessionHeaderStatus:             "Status",
		MsgTUISessionHeaderTitle:              "Title",
		MsgTUISessionFooter:                   "↑↓:select  Enter:details  n:new  d:terminate",
		MsgTUIStatusRunning:                   "● Running",
		MsgTUIStatusStopped:                   "○ Stopped",
		MsgTUIStatusError:                     "✗ Error",
		MsgTUISessionCreateTitle:              "╭─ New Session ─╮",
		MsgTUISessionCreateTool:               "Tool:",
		MsgTUISessionCreateProject:            "Project path:",
		MsgTUISessionCreateProjectPlaceholder: "Project path (optional)",
		MsgTUISessionCreateNoTools:            "(no tools available)",
		MsgTUISessionCreateToolCount:          "... %d tools total",
		MsgTUISessionCreateFooter:             "Tab:switch field  ↑↓:choose tool  Enter:confirm/next  Esc:cancel",
		MsgTUIScheduleLoading:                 "Loading scheduled tasks...",
		MsgTUIScheduleEmpty:                   "No scheduled tasks\n\nUse CLI: maclaw-tui schedule create --name <name> --action <text>",
		MsgTUIScheduleHeaderName:              "NAME",
		MsgTUIScheduleHeaderStatus:            "STATUS",
		MsgTUIScheduleHeaderTime:              "TIME",
		MsgTUIScheduleHeaderRuns:              "RUNS",
		MsgTUIScheduleHeaderAction:            "ACTION",
		MsgTUIScheduleFooter:                  "↑↓:select  p:pause/resume  d:delete  1/2/3:switch sub-tab",
		MsgTUITaskSubRemote:                   "Remote",
		MsgTUITaskSubBackground:               "Background",
		MsgTUITaskSubScheduled:                "Scheduled",
		MsgTUITaskRemoteEmpty:                 "No remote tasks\n\nRemote background tasks submitted via SSH will appear here",
		MsgTUITaskBackgroundEmpty:             "No background tasks\n\nBackground loop tasks started by Agent will appear here",
		MsgTUITaskRemoteFooter:                "↑↓:select  Enter:details  1/2/3:switch sub-tab",
		MsgTUITaskBackgroundFooter:            "↑↓:select  Enter:details  s:stop  1/2/3:switch sub-tab",
		MsgTUILoopManagerUnavailable:          "Background loop manager is not initialized (run in daemon or TUI mode)",
		MsgTUILoopNoTasks:                     "No running background tasks.",
		MsgTUILoopTaskNotFound:                "Background task '%s' not found",
		MsgTUILoopTaskStopped:                 "Background task '%s' stopped.",
		MsgTUILoopTaskContinued:               "Added %d more rounds to background task '%s'.",
		MsgTUITelegramHubForwardUnsupported:   "⚠️ Hub forwarding is not supported in TUI mode yet. Please use the GUI client.",
		MsgTUIConfigLoadFailed:                "Failed to load config: %w",
		MsgTUIBuildEnvFailed:                  "Failed to build environment variables: %w",
		MsgTUIToolMissing:                     "Tool %s is not installed (not found in %s)",
		MsgTUIYoloBlocked:                     "⚠ YOLO mode is blocked by Hub security policy; launching in normal mode",
		MsgTUIMemoryLoading:                   "Loading memory...",
		MsgTUIMemoryEmpty:                     "No memory entries\n\nUse CLI: maclaw-tui memory save --content <text>",
		MsgTUIMemoryHeaderCategory:            "CATEGORY",
		MsgTUIMemoryHeaderAccess:              "ACCESS",
		MsgTUIMemoryHeaderContent:             "CONTENT",
		MsgTUIMemoryFooter:                    "↑↓:select  d:delete  c:compress  b:list backups",
		MsgTUIAuditFilterPlaceholder:          "Filter by tool or risk level...",
		MsgTUIAuditLoading:                    "Loading audit logs...",
		MsgTUIAuditEmpty:                      "No audit logs\n\nUse CLI: maclaw-tui audit list",
		MsgTUIAuditFilterLabel:                "Filter: ",
		MsgTUIAuditFilterSummary:              "Filter: %s (/:edit  Esc:clear)",
		MsgTUIAuditHeaderTime:                 "TIME",
		MsgTUIAuditHeaderTool:                 "TOOL",
		MsgTUIAuditHeaderRisk:                 "RISK",
		MsgTUIAuditHeaderPolicy:               "POLICY",
		MsgTUIAuditHeaderResult:               "RESULT",
		MsgTUIAuditFooter:                     "%d/%d entries  ↑↓:select  g/G:top/end  /:filter  r:refresh",
		MsgTUIChatInputPlaceholder:            "Type a message... (Enter to send)",
		MsgTUIChatSystemReady:                 "AI assistant is ready [Agent mode]. Tool calling is enabled (bash/file ops/session management).",
		MsgTUIChatError:                       "Error: %s",
		MsgTUIChatClearedMessage:              "Chat history cleared.",
		MsgTUIChatModeSimple:                  "Simple chat",
		MsgTUIChatModeAgent:                   "Agent (tool calling)",
		MsgTUIChatModeSwitched:                "Switched to %s mode",
		MsgTUIChatUserPrefix:                  "You: ",
		MsgTUIChatAssistantPrefix:             "AI: ",
		MsgTUIChatWaiting:                     "  ⏳ Thinking...",
		MsgTUIChatSpinnerLabel:                "Thinking...",
		MsgTUIChatHint:                        "i:input  Enter:send  Esc:exit input  c:clear  a:switch mode  ↑↓:scroll",
		MsgTUIChatAwaitingResponse:            "Waiting for response...",
		MsgTUIChatModeLabelSimple:             "Chat",
		MsgTUIChatModeLabelAgent:              "Agent",
		MsgTUIChatMessageCount:                "Messages:%d",
		MsgTUIHelpTitle:                       "  -- Keyboard Shortcuts --",
		MsgTUIHelpSectionGlobal:               "Global",
		MsgTUIHelpSectionListNavigation:       "List Navigation",
		MsgTUIHelpSectionSessions:             "Coding",
		MsgTUIHelpSectionScheduledTasks:       "Tasks",
		MsgTUIHelpSectionMemory:               "Memory",
		MsgTUIHelpSectionConfig:               "Config",
		MsgTUIHelpSectionAgentNet:             "AgentNet",
		MsgTUIHelpSectionSessionDetail:        "Session Detail",
		MsgTUIHelpSectionAIAssistant:          "AI Assistant",
		MsgTUIHelpDescNextTab:                 "next tab",
		MsgTUIHelpDescPreviousTab:             "previous tab",
		MsgTUIHelpDescQuit:                    "quit",
		MsgTUIHelpDescForceQuit:               "force quit",
		MsgTUIHelpDescShowCloseHelp:           "show/close help",
		MsgTUIHelpDescMoveUp:                  "move up",
		MsgTUIHelpDescMoveDown:                "move down",
		MsgTUIHelpDescJumpTop:                 "jump to top",
		MsgTUIHelpDescJumpBottom:              "jump to bottom",
		MsgTUIHelpDescRefresh:                 "refresh",
		MsgTUIHelpDescViewDetails:             "view details",
		MsgTUIHelpDescNewSession:              "new session",
		MsgTUIHelpDescTerminateSession:        "terminate session",
		MsgTUIHelpDescPauseResume:             "pause/resume",
		MsgTUIHelpDescDelete:                  "delete",
		MsgTUIHelpDescEdit:                    "edit",
		MsgTUIHelpDescCancelEdit:              "cancel edit",
		MsgTUIHelpDescSwitchSubTab:            "switch sub-tab",
		MsgTUIHelpDescScroll:                  "scroll",
		MsgTUIHelpDescTopBottom:               "top/bottom",
		MsgTUIHelpDescBackToList:              "back to list",
		MsgTUIHelpDescStartInput:              "start input",
		MsgTUIHelpDescSendMessage:             "send message",
		MsgTUIHelpDescExitInput:               "exit input",
		MsgTUIHelpDescClearHistory:            "clear history",
		MsgTUIHelpDescScrollMessages:          "scroll messages",
		MsgTUIHelpClose:                       "Press ? or Esc to close",
		MsgTUIPythonEnvError:                  "⚠ Python environment error: %s",
		MsgTUIPythonEnvReady:                  "🐍 Python %s ready (venv: %s)",
		MsgTUIPythonAvailable:                 "🐍 Python %s available",
		MsgTUIConfigSaveFailed:                "Save failed: %s: %v",
		MsgTUIConfigSaved:                     "Saved: %s = %s",
		MsgTUIMemoryCompressHint:              "Compressing memory... Use CLI: maclaw-tui memory compress",
		MsgTUIMemoryBackupListHint:            "Use CLI for backup list: maclaw-tui memory backup list",
		MsgTUISessionMonitorEvent:             "🔔 [%s] %s",
		MsgTUIErrorExit:                       "Error: %v\n\nPress q to quit\n",
		MsgTUILLMNotConfiguredHint:            "LLM is not configured. Run maclaw llm setup to get started.",
		MsgTUIConfigTitle:                     "Configuration",
		MsgTUIConfigNotSet:                    "(not set)",
		MsgTUIConfigFooterEditing:             "Enter:confirm  Esc:cancel",
		MsgTUIConfigFooterNormal:              "1-6:switch tab  Up/Down:move  Enter:edit  Space:toggle",
		MsgTUIConfigFooterSelect:              "Left/Right:select  Enter:confirm  Esc:cancel",
		MsgTUIConfigDescHubURL:                "Hub server URL",
		MsgTUIConfigDescToken:                 "auth token",
		MsgTUIConfigDescDataDir:               "data directory",
		MsgTUIConfigDescMaxIterations:         "Agent max iterations (30-300)",
		MsgTUIConfigDescAgentNetEnabled:       "enable AgentNet",
		MsgTUIConfigDescLLMProviderPreset:     "LLM provider preset",
		MsgTUIConfigDescLLMURL:                "LLM API URL",
		MsgTUIConfigDescLLMKey:                "LLM API Key",
		MsgTUIConfigDescLLMModel:              "LLM model name",
		MsgTUIConfigDescLLMProtocol:           "LLM protocol (openai/anthropic)",
		MsgTUIConfigDescLLMContextLength:      "context length (tokens)",
		MsgTUIConfigDescQQBotEnabled:          "enable QQ bot",
		MsgTUIConfigDescQQBotAppID:            "QQ Bot AppID",
		MsgTUIConfigDescQQBotAppSecret:        "QQ Bot AppSecret",
		MsgTUIConfigDescTelegramEnabled:       "enable Telegram bot",
		MsgTUIConfigDescTelegramToken:         "Telegram Bot Token",
		MsgTUIConfigDescSkillPurchaseMode:     "Skill purchase mode (auto/free_only)",
		// Config tab names
		MsgTUIConfigTabGeneral:  "General",
		MsgTUIConfigTabLLM:      "LLM",
		MsgTUIConfigTabIM:       "IM",
		MsgTUIConfigTabProxy:    "Proxy",
		MsgTUIConfigTabSecurity: "Security",
		MsgTUIConfigTabAdvanced: "Advanced",
		// New config field descriptions
		MsgTUIConfigDescWorkDir:            "Default working directory",
		MsgTUIConfigDescLanguage:           "Interface language",
		MsgTUIConfigDescCheckUpdate:        "Check for updates on startup",
		MsgTUIConfigDescAuxLLMURL:          "Auxiliary LLM API URL",
		MsgTUIConfigDescAuxLLMKey:          "Auxiliary LLM API key",
		MsgTUIConfigDescAuxLLMModel:        "Auxiliary LLM model name",
		MsgTUIConfigDescAuxLLMProtocol:     "Auxiliary LLM protocol",
		MsgTUIConfigDescWeixinEnabled:      "Enable WeChat",
		MsgTUIConfigDescWeixinToken:        "WeChat Token",
		MsgTUIConfigDescWeixinBaseURL:      "WeChat API URL",
		MsgTUIConfigDescLansengerEnabled:   "Enable Lansenger",
		MsgTUIConfigDescLansengerAppID:     "Lansenger AppID",
		MsgTUIConfigDescLansengerAppSecret: "Lansenger AppSecret",
		MsgTUIConfigDescLansengerGateway:   "Lansenger gateway URL",
		MsgTUIConfigDescProxyEnabled:       "Enable proxy",
		MsgTUIConfigDescProxyProtocol:      "Proxy protocol",
		MsgTUIConfigDescProxyHost:          "Proxy host",
		MsgTUIConfigDescProxyPort:          "Proxy port",
		MsgTUIConfigDescProxyUser:          "Proxy username",
		MsgTUIConfigDescProxyPass:          "Proxy password",
		MsgTUIConfigDescProxyScopeLLM:      "Proxy scope: LLM requests",
		MsgTUIConfigDescProxyScopeAgent:    "Proxy scope: Agent network",
		MsgTUIConfigDescSecurityMode:       "Security policy mode",
		MsgTUIConfigDescSandbox:            "Sandbox mode",
		MsgTUIConfigDescNetworkLevel:       "Network access level",
		MsgTUIConfigDescYoloMode:           "Allow YOLO mode",
		MsgTUIConfigDescFileOutbound:       "Allow file outbound",
		MsgTUIConfigDescImageOutbound:      "Allow image outbound",
		MsgTUIConfigDescUIMode:             "UI mode (pro=full/lite=simplified)",
		MsgTUIConfigDescMemoryCompress:     "Auto-compress memory",
		MsgTUIConfigDescLogDetail:          "Detailed logging",
		MsgTUIConfigDescTrajectory:         "LLM trajectory logging",
		MsgTUIConfigDescDebugTools:         "Debug tool calls",
		MsgTUIConfigDescGossip:             "Enable Gossip",
		MsgTUIConfigDescTrialReflect:       "Enable trial-and-reflect",
		MsgTUIAgentNetLoading:              "Loading AgentNet...",
		MsgTUIAgentNetTabPeers:             "Peers",
		MsgTUIAgentNetTabTasks:             "Tasks",
		MsgTUIAgentNetTabStatus:            "Status",
		MsgTUIAgentNetNoPeers:              "No connected peers",
		MsgTUIAgentNetNoTasks:              "No tasks",
		MsgTUIAgentNetHeaderPeerID:         "PEER ID",
		MsgTUIAgentNetHeaderAddr:           "ADDR",
		MsgTUIAgentNetHeaderLatency:        "LATENCY",
		MsgTUIAgentNetHeaderCountry:        "COUNTRY",
		MsgTUIAgentNetHeaderReward:         "REWARD",
		MsgTUIAgentNetFooterPeers:          "Total %d peers  Up/Down:select  r:refresh",
		MsgTUIAgentNetFooterTasks:          "Total %d tasks  Up/Down:select  r:refresh",
		MsgTUIAgentNetStatusTitle:          "AgentNet Status",
		MsgTUIAgentNetCreditsTitle:         "Credits Info",
		MsgTUIAgentNetStatusPeerID:         "PeerID",
		MsgTUIAgentNetStatusPeers:          "Peers",
		MsgTUIAgentNetStatusUnread:         "Unread",
		MsgTUIAgentNetStatusVersion:        "Version",
		MsgTUIAgentNetStatusUptime:         "Uptime",
		MsgTUIAgentNetStatusBalance:        "Balance",
		MsgTUIAgentNetStatusTier:           "Tier",
		MsgTUIAgentNetStatusEnergy:         "Energy",
		MsgTUIAgentNetFooterStatus:         "r:refresh  1:peers  2:tasks  3:status",
		MsgTUIAgentLoadConfigFailed:        "Failed to load LLM config: %v",
		MsgTUIAgentLLMCallFailed:           "LLM call failed: %v",
		MsgTUIAgentNoValidReply:            "LLM returned no valid reply",
		MsgTUIAgentTruncated:               "\n...(truncated)",
		MsgTUIAgentMaxRoundsReached:        "(maximum reasoning rounds reached)",
	},
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// NormalizeLang maps common language codes to the keys used in the
// translations table. An empty or unrecognised code falls back to "zh".
func NormalizeLang(lang string) string {
	switch lang {
	case "zh", "zh-CN", "zh-Hans", "zh-TW", "zh-Hant":
		return "zh"
	case "en", "en-US", "en-GB":
		return "en"
	case "":
		return defaultLang
	}
	// Unknown language → fallback
	return defaultLang
}

// T returns the translated string for the given key and language.
// If lang is empty or unknown it falls back to "zh".
// If the key is not found the key itself is returned.
func T(key, lang string) string {
	lang = NormalizeLang(lang)
	if table, ok := translations[lang]; ok {
		if s, ok := table[key]; ok {
			return s
		}
	}
	// Fallback to default language if the requested lang table exists but
	// the key is missing.
	if lang != defaultLang {
		if table, ok := translations[defaultLang]; ok {
			if s, ok := table[key]; ok {
				return s
			}
		}
	}
	return key
}

// Tf is like T but applies fmt.Sprintf formatting with the provided args.
func Tf(key, lang string, args ...interface{}) string {
	return fmt.Sprintf(T(key, lang), args...)
}
