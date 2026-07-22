export namespace main {

	export class SkillArtifactRegistryEntry {
	    uri: string;
	    run_id: string;
	    owner_id?: string;
	    skill?: string;
	    artifact_id: string;
	    name?: string;
	    mime_type?: string;
	    size_bytes?: number;
	    remote_url?: string;
	    checksum?: string;
	    download_state?: string;
	    status?: string;
	    presentation?: string;
	    available: boolean;
	    created_at?: string;
	    updated_at?: string;

	    static createFrom(source: any = {}) {
	        return new SkillArtifactRegistryEntry(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uri = source["uri"];
	        this.run_id = source["run_id"];
	        this.owner_id = source["owner_id"];
	        this.skill = source["skill"];
	        this.artifact_id = source["artifact_id"];
	        this.name = source["name"];
	        this.mime_type = source["mime_type"];
	        this.size_bytes = source["size_bytes"];
	        this.remote_url = source["remote_url"];
	        this.checksum = source["checksum"];
	        this.download_state = source["download_state"];
	        this.status = source["status"];
	        this.presentation = source["presentation"];
	        this.available = source["available"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}

	export class VEApprovalCapabilityStatus {
	    ve_id: string;
	    has_capability: boolean;
	    enabled: boolean;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new VEApprovalCapabilityStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ve_id = source["ve_id"];
	        this.has_capability = source["has_capability"];
	        this.enabled = source["enabled"];
	        this.error = source["error"];
	    }
	}

	export class ProjectSearchArtifact {
	    title?: string;
	    source_type?: string;
	    source_url?: string;
	    source_hint?: string;
	    preview?: string;
	    updated_at?: string;

	    static createFrom(source: any = {}) {
	        return new ProjectSearchArtifact(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.source_type = source["source_type"];
	        this.source_url = source["source_url"];
	        this.source_hint = source["source_hint"];
	        this.preview = source["preview"];
	        this.updated_at = source["updated_at"];
	    }
	}

	export class CodingWorkbenchStepStatus {
	    index: number;
	    title?: string;
	    status: string;
	    summary?: string;
	    verify_cmd?: string;
	    verify_ok?: boolean;
	    updated_unix?: number;

	    static createFrom(source: any = {}) {
	        return new CodingWorkbenchStepStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.summary = source["summary"];
	        this.verify_cmd = source["verify_cmd"];
	        this.verify_ok = source["verify_ok"];
	        this.updated_unix = source["updated_unix"];
	    }
	}

	export class CodingWorkbenchStatus {
	    kind: string;
	    armed: boolean;
	    needs_reconnect: boolean;
	    turn_count: number;
	    session_full_access: boolean;
	    session_high_risk_access: boolean;
	    session_plan?: string;
	    execution_plan?: string;
	    plan_mode?: string;
	    pending_approval?: boolean;
	    step_statuses?: CodingWorkbenchStepStatus[];
	    project_instruction_sources?: string[];
	    checkpoint_label?: string;
	    session_input_tokens?: number;
	    session_output_tokens?: number;
	    session_est_cost_rmb?: number;
	    last_turn_input_tokens?: number;
	    last_turn_output_tokens?: number;
	    last_turn_est_cost_rmb?: number;
	    background_verify?: string;
	    worktree_mode?: string;
	    worktree_notes?: string[];
	    conflict_count?: number;
	    conflicts?: any[];
	    conflict_active_id?: string;
	    conflict_selected?: string[];
	    conflict_focus_file?: string;
	    conflict_log?: string[];
	    route_model?: string;
	    route_source?: string;
	    route_task?: string;
	    route_reason?: string;
	    route_pref?: string;
	    route_capabilities?: any[];
	    checkpoint_files?: string[];
	    checkpoint_history?: any[];
	    checkpoint_snapshots?: number;
	    hooks_active?: boolean;
	    hooks_phases?: string[];
	    hooks_command_count?: number;
	    hooks_fail_on_error?: boolean;
	    last_summary?: string;
	    remote_host?: string;
	    remote_user?: string;
	    remote_port?: number;
	    remote_work_dir?: string;
	    remote_session_id?: string;
	    message?: string;

	    static createFrom(source: any = {}) {
	        return new CodingWorkbenchStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.armed = source["armed"];
	        this.needs_reconnect = source["needs_reconnect"];
	        this.turn_count = source["turn_count"];
	        this.session_full_access = source["session_full_access"];
	        this.session_high_risk_access = source["session_high_risk_access"];
	        this.session_plan = source["session_plan"];
	        this.execution_plan = source["execution_plan"];
	        this.plan_mode = source["plan_mode"];
	        this.pending_approval = source["pending_approval"];
	        this.step_statuses = source["step_statuses"] ? source["step_statuses"].map((x: any) => new CodingWorkbenchStepStatus(x)) : undefined;
	        this.project_instruction_sources = source["project_instruction_sources"];
	        this.checkpoint_label = source["checkpoint_label"];
	        this.session_input_tokens = source["session_input_tokens"];
	        this.session_output_tokens = source["session_output_tokens"];
	        this.session_est_cost_rmb = source["session_est_cost_rmb"];
	        this.last_turn_input_tokens = source["last_turn_input_tokens"];
	        this.last_turn_output_tokens = source["last_turn_output_tokens"];
	        this.last_turn_est_cost_rmb = source["last_turn_est_cost_rmb"];
	        this.background_verify = source["background_verify"];
	        this.worktree_mode = source["worktree_mode"];
	        this.worktree_notes = source["worktree_notes"];
	        this.conflict_count = source["conflict_count"];
	        this.conflicts = source["conflicts"];
	        this.conflict_active_id = source["conflict_active_id"];
	        this.conflict_selected = source["conflict_selected"];
	        this.conflict_focus_file = source["conflict_focus_file"];
	        this.conflict_log = source["conflict_log"];
	        this.route_model = source["route_model"];
	        this.route_source = source["route_source"];
	        this.route_task = source["route_task"];
	        this.route_reason = source["route_reason"];
	        this.route_pref = source["route_pref"];
	        this.route_capabilities = source["route_capabilities"];
	        this.checkpoint_files = source["checkpoint_files"];
	        this.checkpoint_snapshots = source["checkpoint_snapshots"];
	        this.last_summary = source["last_summary"];
	        this.remote_host = source["remote_host"];
	        this.remote_user = source["remote_user"];
	        this.remote_port = source["remote_port"];
	        this.remote_work_dir = source["remote_work_dir"];
	        this.remote_session_id = source["remote_session_id"];
	        this.message = source["message"];
	    }
	}

	export class VirtualRepositoryCodingTaskLaunch {
	    project_path: string;
	    task_title: string;
	    agent_mode: string;
	    remote_host?: string;

	    static createFrom(source: any = {}) {
	        return new VirtualRepositoryCodingTaskLaunch(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_path = source["project_path"];
	        this.task_title = source["task_title"];
	        this.agent_mode = source["agent_mode"];
	        this.remote_host = source["remote_host"];
	    }
	}

	export class ProjectSearchResult {
	    id: string;
	    name: string;
	    project_path: string;
	    working_dir?: string;
	    workflow_type: string;
	    preview: string;
	    tags: string[];
	    last_activity: string;
	    entry_count: number;
	    pinned: boolean;
	    archived: boolean;
	    has_output: boolean;
	    source_urls?: string[];
	    recent_artifacts?: ProjectSearchArtifact[];

	    static createFrom(source: any = {}) {
	        return new ProjectSearchResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.project_path = source["project_path"];
	        this.working_dir = source["working_dir"];
	        this.workflow_type = source["workflow_type"];
	        this.preview = source["preview"];
	        this.tags = source["tags"];
	        this.last_activity = source["last_activity"];
	        this.entry_count = source["entry_count"];
	        this.pinned = source["pinned"];
	        this.archived = source["archived"];
	        this.has_output = source["has_output"];
	        this.source_urls = source["source_urls"];
	        this.recent_artifacts = this.convertValues(source["recent_artifacts"], ProjectSearchArtifact);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class ProjectSceneDetail {
	    project_path: string;
	    name?: string;
	    workflow_types?: string[];
	    tags?: string[];
	    source_urls?: string[];
	    recent_artifacts?: ProjectSearchArtifact[];
	    entry_count: number;
	    last_activity?: string;
	    preview?: string;

	    static createFrom(source: any = {}) {
	        return new ProjectSceneDetail(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_path = source["project_path"];
	        this.name = source["name"];
	        this.workflow_types = source["workflow_types"];
	        this.tags = source["tags"];
	        this.source_urls = source["source_urls"];
	        this.recent_artifacts = this.convertValues(source["recent_artifacts"], ProjectSearchArtifact);
	        this.entry_count = source["entry_count"];
	        this.last_activity = source["last_activity"];
	        this.preview = source["preview"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class ProjectContextArtifact {
	    title?: string;
	    source_type?: string;
	    source_url?: string;
	    source_hint?: string;
	    preview?: string;
	    updated_at?: string;

	    static createFrom(source: any = {}) {
	        return new ProjectContextArtifact(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.source_type = source["source_type"];
	        this.source_url = source["source_url"];
	        this.source_hint = source["source_hint"];
	        this.preview = source["preview"];
	        this.updated_at = source["updated_at"];
	    }
	}

	export class ProjectContextSummary {
	    project_name: string;
	    recent_progress: string;
	    key_artifacts: string[];
	    recent_artifacts?: ProjectContextArtifact[];
	    active_workflow: string;

	    static createFrom(source: any = {}) {
	        return new ProjectContextSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_name = source["project_name"];
	        this.recent_progress = source["recent_progress"];
	        this.key_artifacts = source["key_artifacts"];
	        this.recent_artifacts = this.convertValues(source["recent_artifacts"], ProjectContextArtifact);
	        this.active_workflow = source["active_workflow"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class ProjectConfig {
	    id: string;
	    name: string;
	    path: string;
	    yolo_mode: boolean;
	    admin_mode: boolean;
	    python_project: boolean;
	    python_env: string;
	    team_mode: boolean;
	    use_proxy: boolean;
	    proxy_host: string;
	    proxy_port: string;
	    proxy_username: string;
	    proxy_password: string;

	    static createFrom(source: any = {}) {
	        return new ProjectConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.yolo_mode = source["yolo_mode"];
	        this.admin_mode = source["admin_mode"];
	        this.python_project = source["python_project"];
	        this.python_env = source["python_env"];
	        this.team_mode = source["team_mode"];
	        this.use_proxy = source["use_proxy"];
	        this.proxy_host = source["proxy_host"];
	        this.proxy_port = source["proxy_port"];
	        this.proxy_username = source["proxy_username"];
	        this.proxy_password = source["proxy_password"];
	    }
	}
	export class ModelConfig {
	    model_name: string;
	    model_id: string;
	    model_url: string;
	    api_key: string;
	    wire_api: string;
	    agent_type: string;
	    is_custom: boolean;

	    static createFrom(source: any = {}) {
	        return new ModelConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model_name = source["model_name"];
	        this.model_id = source["model_id"];
	        this.model_url = source["model_url"];
	        this.api_key = source["api_key"];
	        this.wire_api = source["wire_api"];
	        this.agent_type = source["agent_type"];
	        this.is_custom = source["is_custom"];
	    }
	}
	export class ToolConfig {
	    current_model: string;
	    models: ModelConfig[];

	    static createFrom(source: any = {}) {
	        return new ToolConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_model = source["current_model"];
	        this.models = this.convertValues(source["models"], ModelConfig);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LLMPromptCacheConfig {
	    enabled: boolean;
	    openai_enabled?: boolean;
	    anthropic_enabled?: boolean;
	    stream_synthesis_enabled?: boolean;
	    cache_dir?: string;
	    ttl_seconds: number;
	    memory_max_entries: number;
	    memory_max_bytes: number;
	    disk_max_bytes: number;
	    normalize_deterministic_params: boolean;
	    ignore_model_field: boolean;
	    ignore_user_field: boolean;
	    ignore_metadata_field: boolean;
	    singleflight_wait_timeout_ms: number;

	    static createFrom(source: any = {}) {
	        return new LLMPromptCacheConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.openai_enabled = source["openai_enabled"];
	        this.anthropic_enabled = source["anthropic_enabled"];
	        this.stream_synthesis_enabled = source["stream_synthesis_enabled"];
	        this.cache_dir = source["cache_dir"];
	        this.ttl_seconds = source["ttl_seconds"];
	        this.memory_max_entries = source["memory_max_entries"];
	        this.memory_max_bytes = source["memory_max_bytes"];
	        this.disk_max_bytes = source["disk_max_bytes"];
	        this.normalize_deterministic_params = source["normalize_deterministic_params"];
	        this.ignore_model_field = source["ignore_model_field"];
	        this.ignore_user_field = source["ignore_user_field"];
	        this.ignore_metadata_field = source["ignore_metadata_field"];
	        this.singleflight_wait_timeout_ms = source["singleflight_wait_timeout_ms"];
	    }
	}
	export class AppConfig {
	    claude: ToolConfig;
	    codex: ToolConfig;
	    opencode: ToolConfig;
	    codebuddy: ToolConfig;
	    qoder: ToolConfig;
	    iflow: ToolConfig;
	    kilo: ToolConfig;
	    projects: ProjectConfig[];
	    current_project: string;
	    active_tool: string;
	    hide_startup_popup: boolean;
	    hide_maclaw_llm_popup: boolean;
	    show_codex: boolean;
	    show_opencode: boolean;
	    show_codebuddy: boolean;
	    show_qoder: boolean;
	    show_iflow: boolean;
	    show_kilo: boolean;
	    language: string;
	    power_optimization: boolean;
	    screen_dim_timeout_min: number;
	    workstation_mode: boolean;
	    check_update_on_startup: boolean;
	    pause_env_check: boolean;
	    env_check_done: boolean;
	    env_check_interval: number;
	    last_env_check_time: string;
	    default_proxy_enabled: boolean;
	    default_proxy_protocol: string;
	    default_proxy_host: string;
	    default_proxy_port: string;
	    default_proxy_username: string;
	    default_proxy_password: string;
	    default_proxy_bypass: string;
	    default_proxy_scope_maclaw: boolean;
	    default_proxy_scope_coding_tools: boolean;
	    default_proxy_scope_agent: boolean;
	    use_windows_terminal: boolean;
	    remote_enabled: boolean;
	    remote_hub_id: string;
	    remote_hub_url: string;
	    remote_hubcenter_url: string;
	    remote_email: string;
	    remote_mobile: string;
	    remote_sn: string;
	    remote_user_id: string;
	    remote_tenant_id: string;
	    remote_tenant_name: string;
	    remote_machine_id: string;
	    remote_machine_name: string;
	    remote_machine_token: string;
	    remote_heartbeat_sec: number;
	    remote_nickname: string;
	    remote_client_id: string;
	    default_launch_mode: string;
	    prefer_beta_channel: boolean;
	    maclaw_llm_url: string;
	    maclaw_llm_key: string;
	    maclaw_llm_model: string;
	    maclaw_role_name: string;
	    maclaw_role_description: string;
	    mis_data: any;
	    group_discussion: any;
	    maclaw_llm_protocol: string;
	    maclaw_llm_context_length: number;
	    maclaw_llm_timeout_sec: number;
	    agent_response_timeout_sec: number;
	    skill_runner_timeout_sec: number;
	    maclaw_llm_providers: any[];
	    maclaw_llm_current_provider: string;
	    llm_prompt_cache: LLMPromptCacheConfig;
	    tool_cache_maintenance: any;
	    web_search_providers: any[];
	    web_search_current_provider: string;
	    maclaw_agent_max_iterations: number;
	    subagent_concurrency: number;
	    subagent_full_access: boolean;
	    mcp_servers: any[];
	    local_mcp_servers: any[];
	    nl_skills: any[];
	    memory_auto_compress: boolean;
	    memory_max_backups: number;
	    skill_hub_urls: any[];
	    maclaw_debug_tool_calls: boolean;
	    show_ai_trace_entry: boolean;
	    show_app_entry?: boolean;
	    show_workflow_entry?: boolean;
	    log_detail_enabled: boolean;
	    security_policy_mode: string;
	    sandbox_mode: string;
	    network_level: string;
	    network_allowlist?: string[];
	    skill_sources_allowed?: string[];
	    yolo_mode_allowed: boolean;
	    smart_route_enabled: boolean;
	    gossip_enabled: boolean;
	    file_outbound_enabled: boolean;
	    image_outbound_enabled: boolean;
	    qqbot_enabled: boolean;
	    qqbot_app_id: string;
	    qqbot_app_secret: string;
	    qqbot_owner_openid: string;
	    telegram_bot_enabled: boolean;
	    telegram_bot_token: string;
	    telegram_owner_chat_id: string;
	    weixin_enabled: boolean;
	    weixin_token: string;
	    weixin_base_url: string;
	    weixin_cdn_url: string;
	    weixin_account_id: string;
	    weixin_local_mode?: boolean;
	    lansenger_enabled?: boolean;
	    lansenger_app_id: string;
	    lansenger_app_secret: string;
	    lansenger_gateway_url: string;
	    lansenger_wss_url: string;
	    lansenger_ignored_group_ids?: string[];
	    lansenger_group_policy?: string;
	    lansenger_allowed_group_ids?: string[];
	    lansenger_require_mention?: boolean;
	    lansenger_respond_to_at_all?: boolean;
	    lansenger_auto_mention_reply?: boolean;
	    lansenger_auto_quote_reply?: boolean;
	    lansenger_local_mode?: boolean;
	    thirdparty_gateway_enabled?: boolean;
	    thirdparty_gateway_token: string;
	    thirdparty_gateway_host: string;
	    thirdparty_gateway_port: number;
	    thirdparty_gateway_local_mode?: boolean;
	    im_progress_nudge_enabled?: boolean;
	    ui_mode: string;
	    skill_purchase_mode: string;
	    gossip_auto_publish: boolean;
	    llm_trajectory_logging: boolean;
	    memory_recall_log_enabled: boolean;
        show_assistant_entry: boolean;
	    pet_enabled?: boolean;
	    pet_skin?: string;
	    pet_size?: number;
	    pet_motion_enabled?: boolean;
	    pet_motion_sound_enabled?: boolean;
	    pet_motion_sound_preset?: string;
	    pet_text_interaction_enabled?: boolean;
	    pet_voice_input_enabled?: boolean;
	    pet_voice_readback_enabled?: boolean;
	    pet_file_drop_enabled?: boolean;
	    pet_interaction_mode?: string;
	    pet_conversation_mode?: string;
	    pet_readback_mode?: string;
	    pet_auto_retry_on_no_hear?: boolean;
	    pet_continuous_timeout_sec?: number;
	    pet_quiet_mode?: boolean;
	    pet_variant?: string;
	    pet_variant_migrated?: boolean;
	    pet_figurative_upgrade_prompt_pending?: boolean;
	    pet_reduced_motion?: boolean;
	    floating_btn_x?: number;
	    floating_btn_y?: number;
	    floating_btn_position_set?: boolean;
	    trial_reflect_enabled: boolean;
	    llm_token_usage: Record<string, any>;
	    onboarding_done: boolean;
	    vector_search_enabled: boolean;
	    default_tool: string;
	    default_tool_provider: string;
	    working_directory: string;
	    data_dir: string;
	    ui_zoom_factor: number;
	    chat_font_size: number;
	    workflow_enabled?: boolean;
	    skill_evolution_enabled?: boolean;
	    skill_evolution_repair_cooldown_hours?: number;
	    favorite_employees?: string[];
	    favorite_employee_names?: Record<string, string>;
	    show_coding_tool_entry?: boolean;
	    tts_enabled?: boolean;
	    tts_voice_id?: string;
	    asr_enabled?: boolean;
	    diarization_enabled?: boolean;
	    asr_voice_correction_enabled?: boolean;
	    noise_floor_calibrated?: number;
	    speech_level_calibrated?: number;
	    audio_input_device_id?: string;
	    audio_output_device_id?: string;

	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.claude = this.convertValues(source["claude"], ToolConfig);
	        this.codex = this.convertValues(source["codex"], ToolConfig);
	        this.opencode = this.convertValues(source["opencode"], ToolConfig);
	        this.codebuddy = this.convertValues(source["codebuddy"], ToolConfig);
	        this.qoder = this.convertValues(source["qoder"], ToolConfig);
	        this.iflow = this.convertValues(source["iflow"], ToolConfig);
	        this.kilo = this.convertValues(source["kilo"], ToolConfig);
	        this.projects = this.convertValues(source["projects"], ProjectConfig);
	        this.current_project = source["current_project"];
	        this.active_tool = source["active_tool"];
	        this.hide_startup_popup = source["hide_startup_popup"];
	        this.hide_maclaw_llm_popup = source["hide_maclaw_llm_popup"];
	        this.show_codex = source["show_codex"];
	        this.show_opencode = source["show_opencode"];
	        this.show_codebuddy = source["show_codebuddy"];
	        this.show_qoder = source["show_qoder"];
	        this.show_iflow = source["show_iflow"];
	        this.show_kilo = source["show_kilo"];
	        this.language = source["language"];
	        this.power_optimization = source["power_optimization"];
	        this.screen_dim_timeout_min = source["screen_dim_timeout_min"];
	        this.workstation_mode = source["workstation_mode"];
	        this.check_update_on_startup = source["check_update_on_startup"];
	        this.pause_env_check = source["pause_env_check"];
	        this.env_check_done = source["env_check_done"];
	        this.env_check_interval = source["env_check_interval"];
	        this.last_env_check_time = source["last_env_check_time"];
	        this.default_proxy_enabled = source["default_proxy_enabled"];
	        this.default_proxy_protocol = source["default_proxy_protocol"];
	        this.default_proxy_host = source["default_proxy_host"];
	        this.default_proxy_port = source["default_proxy_port"];
	        this.default_proxy_username = source["default_proxy_username"];
	        this.default_proxy_password = source["default_proxy_password"];
	        this.default_proxy_bypass = source["default_proxy_bypass"];
	        this.default_proxy_scope_maclaw = source["default_proxy_scope_maclaw"];
	        this.default_proxy_scope_coding_tools = source["default_proxy_scope_coding_tools"];
	        this.default_proxy_scope_agent = source["default_proxy_scope_agent"];
	        this.use_windows_terminal = source["use_windows_terminal"];
	        this.remote_enabled = source["remote_enabled"];
	        this.remote_hub_id = source["remote_hub_id"];
	        this.remote_hub_url = source["remote_hub_url"];
	        this.remote_hubcenter_url = source["remote_hubcenter_url"];
	        this.remote_email = source["remote_email"];
	        this.remote_mobile = source["remote_mobile"];
	        this.remote_sn = source["remote_sn"];
	        this.remote_user_id = source["remote_user_id"];
	        this.remote_tenant_id = source["remote_tenant_id"];
	        this.remote_tenant_name = source["remote_tenant_name"];
	        this.remote_machine_id = source["remote_machine_id"];
	        this.remote_machine_name = source["remote_machine_name"];
	        this.remote_machine_token = source["remote_machine_token"];
	        this.remote_heartbeat_sec = source["remote_heartbeat_sec"];
	        this.remote_nickname = source["remote_nickname"];
	        this.remote_client_id = source["remote_client_id"];
	        this.default_launch_mode = source["default_launch_mode"];
	        this.prefer_beta_channel = source["prefer_beta_channel"];
	        this.maclaw_llm_url = source["maclaw_llm_url"];
	        this.maclaw_llm_key = source["maclaw_llm_key"];
	        this.maclaw_llm_model = source["maclaw_llm_model"];
	        this.maclaw_role_name = source["maclaw_role_name"];
	        this.maclaw_role_description = source["maclaw_role_description"];
	        this.mis_data = source["mis_data"];
	        this.group_discussion = source["group_discussion"];
	        this.maclaw_llm_protocol = source["maclaw_llm_protocol"];
	        this.maclaw_llm_context_length = source["maclaw_llm_context_length"];
	        this.maclaw_llm_timeout_sec = source["maclaw_llm_timeout_sec"];
	        this.agent_response_timeout_sec = source["agent_response_timeout_sec"];
	        this.skill_runner_timeout_sec = source["skill_runner_timeout_sec"];
	        this.maclaw_llm_providers = source["maclaw_llm_providers"];
	        this.maclaw_llm_current_provider = source["maclaw_llm_current_provider"];
	        this.llm_prompt_cache = this.convertValues(source["llm_prompt_cache"], LLMPromptCacheConfig);
	        this.tool_cache_maintenance = source["tool_cache_maintenance"];
	        this.web_search_providers = source["web_search_providers"];
	        this.web_search_current_provider = source["web_search_current_provider"];
	        this.maclaw_agent_max_iterations = source["maclaw_agent_max_iterations"];
	        this.subagent_concurrency = source["subagent_concurrency"];
	        this.subagent_full_access = source["subagent_full_access"];
	        this.mcp_servers = source["mcp_servers"];
	        this.local_mcp_servers = source["local_mcp_servers"];
	        this.nl_skills = source["nl_skills"];
	        this.memory_auto_compress = source["memory_auto_compress"];
	        this.memory_max_backups = source["memory_max_backups"];
	        this.skill_hub_urls = source["skill_hub_urls"];
	        this.security_policy_mode = source["security_policy_mode"];
	        this.sandbox_mode = source["sandbox_mode"];
	        this.network_level = source["network_level"];
	        this.network_allowlist = source["network_allowlist"];
	        this.skill_sources_allowed = source["skill_sources_allowed"];
	        this.yolo_mode_allowed = source["yolo_mode_allowed"];
	        this.smart_route_enabled = source["smart_route_enabled"];
	        this.gossip_enabled = source["gossip_enabled"];
	        this.file_outbound_enabled = source["file_outbound_enabled"];
	        this.image_outbound_enabled = source["image_outbound_enabled"];
	        this.maclaw_debug_tool_calls = source["maclaw_debug_tool_calls"];
	        this.show_ai_trace_entry = source["show_ai_trace_entry"];
	        this.show_app_entry = source["show_app_entry"];
	        this.show_workflow_entry = source["show_workflow_entry"];
	        this.log_detail_enabled = source["log_detail_enabled"];
	        this.qqbot_enabled = source["qqbot_enabled"];
	        this.qqbot_app_id = source["qqbot_app_id"];
	        this.qqbot_app_secret = source["qqbot_app_secret"];
	        this.qqbot_owner_openid = source["qqbot_owner_openid"];
	        this.telegram_bot_enabled = source["telegram_bot_enabled"];
	        this.telegram_bot_token = source["telegram_bot_token"];
	        this.telegram_owner_chat_id = source["telegram_owner_chat_id"];
	        this.weixin_enabled = source["weixin_enabled"];
	        this.weixin_token = source["weixin_token"];
	        this.weixin_base_url = source["weixin_base_url"];
	        this.weixin_cdn_url = source["weixin_cdn_url"];
	        this.weixin_account_id = source["weixin_account_id"];
	        this.weixin_local_mode = source["weixin_local_mode"];
	        this.lansenger_enabled = source["lansenger_enabled"];
	        this.lansenger_app_id = source["lansenger_app_id"];
	        this.lansenger_app_secret = source["lansenger_app_secret"];
	        this.lansenger_gateway_url = source["lansenger_gateway_url"];
	        this.lansenger_wss_url = source["lansenger_wss_url"];
	        this.lansenger_ignored_group_ids = source["lansenger_ignored_group_ids"];
	        this.lansenger_group_policy = source["lansenger_group_policy"];
	        this.lansenger_allowed_group_ids = source["lansenger_allowed_group_ids"];
	        this.lansenger_require_mention = source["lansenger_require_mention"];
	        this.lansenger_respond_to_at_all = source["lansenger_respond_to_at_all"];
	        this.lansenger_auto_mention_reply = source["lansenger_auto_mention_reply"];
	        this.lansenger_auto_quote_reply = source["lansenger_auto_quote_reply"];
	        this.lansenger_local_mode = source["lansenger_local_mode"];
	        this.thirdparty_gateway_enabled = source["thirdparty_gateway_enabled"];
	        this.thirdparty_gateway_token = source["thirdparty_gateway_token"];
	        this.thirdparty_gateway_host = source["thirdparty_gateway_host"];
	        this.thirdparty_gateway_port = source["thirdparty_gateway_port"];
	        this.thirdparty_gateway_local_mode = source["thirdparty_gateway_local_mode"];
	        this.im_progress_nudge_enabled = source["im_progress_nudge_enabled"];
	        this.ui_mode = source["ui_mode"];
	        this.skill_purchase_mode = source["skill_purchase_mode"];
	        this.gossip_auto_publish = source["gossip_auto_publish"];
            this.show_assistant_entry = source["show_assistant_entry"];
	        this.pet_enabled = source["pet_enabled"];
	        this.pet_skin = source["pet_skin"];
	        this.pet_size = source["pet_size"];
	        this.pet_motion_enabled = source["pet_motion_enabled"];
	        this.pet_motion_sound_enabled = source["pet_motion_sound_enabled"];
	        this.pet_motion_sound_preset = source["pet_motion_sound_preset"];
	        this.pet_text_interaction_enabled = source["pet_text_interaction_enabled"];
	        this.pet_voice_input_enabled = source["pet_voice_input_enabled"];
	        this.pet_voice_readback_enabled = source["pet_voice_readback_enabled"];
	        this.pet_file_drop_enabled = source["pet_file_drop_enabled"];
	        this.pet_interaction_mode = source["pet_interaction_mode"];
	        this.pet_conversation_mode = source["pet_conversation_mode"];
	        this.pet_readback_mode = source["pet_readback_mode"];
	        this.pet_auto_retry_on_no_hear = source["pet_auto_retry_on_no_hear"];
	        this.pet_continuous_timeout_sec = source["pet_continuous_timeout_sec"];
	        this.pet_quiet_mode = source["pet_quiet_mode"];
	        this.pet_variant = source["pet_variant"];
	        this.pet_variant_migrated = source["pet_variant_migrated"];
	        this.pet_figurative_upgrade_prompt_pending = source["pet_figurative_upgrade_prompt_pending"];
	        this.pet_reduced_motion = source["pet_reduced_motion"];
	        this.floating_btn_x = source["floating_btn_x"];
	        this.floating_btn_y = source["floating_btn_y"];
	        this.floating_btn_position_set = source["floating_btn_position_set"];
	        this.llm_trajectory_logging = source["llm_trajectory_logging"];
	        this.memory_recall_log_enabled = source["memory_recall_log_enabled"];
	        this.trial_reflect_enabled = source["trial_reflect_enabled"];
	        this.llm_token_usage = source["llm_token_usage"];
	        this.onboarding_done = source["onboarding_done"];
	        this.vector_search_enabled = source["vector_search_enabled"];
	        this.default_tool = source["default_tool"];
	        this.default_tool_provider = source["default_tool_provider"];
	        this.working_directory = source["working_directory"];
	        this.data_dir = source["data_dir"];
	        this.ui_zoom_factor = source["ui_zoom_factor"];
	        this.chat_font_size = source["chat_font_size"];
	        this.workflow_enabled = source["workflow_enabled"];
	        this.skill_evolution_enabled = source["skill_evolution_enabled"];
	        this.skill_evolution_repair_cooldown_hours = source["skill_evolution_repair_cooldown_hours"];
	        this.favorite_employees = source["favorite_employees"];
	        this.favorite_employee_names = source["favorite_employee_names"];
	        this.show_coding_tool_entry = source["show_coding_tool_entry"];
	        this.tts_enabled = source["tts_enabled"];
	        this.tts_voice_id = source["tts_voice_id"];
	        this.asr_enabled = source["asr_enabled"];
	        this.diarization_enabled = source["diarization_enabled"];
	        this.asr_voice_correction_enabled = source["asr_voice_correction_enabled"];
	        this.noise_floor_calibrated = source["noise_floor_calibrated"];
	        this.speech_level_calibrated = source["speech_level_calibrated"];
	        this.audio_input_device_id = source["audio_input_device_id"];
	        this.audio_output_device_id = source["audio_output_device_id"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class MaclawLLMTestResult {
	    message: string;
	    supports_vision: boolean;

	    static createFrom(source: any = {}) {
	        return new MaclawLLMTestResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.supports_vision = source["supports_vision"];
	    }
	}

	export class MobileLLMQRCodeSession {
	    status: string;
	    session_id: string;
	    expires_at: string;
	    qr_payload: string;

	    static createFrom(source: any = {}) {
	        return new MobileLLMQRCodeSession(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.session_id = source["session_id"];
	        this.expires_at = source["expires_at"];
	        this.qr_payload = source["qr_payload"];
	    }
	}

	export class PythonEnvironment {
	    name: string;
	    path: string;
	    type: string;

	    static createFrom(source: any = {}) {
	        return new PythonEnvironment(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.type = source["type"];
	    }
	}
	export class SharedPythonRuntimeStatus {
	    schema: string;
	    id: string;
	    os: string;
	    arch: string;
	    manager: string;
	    python: string;
	    python_request: string;
	    packages: string[];
	    index_urls?: string[];
	    root_dir: string;
	    env_dir: string;
	    python_path: string;
	    lock_path: string;
	    cache_dir: string;
	    status: string;
	    used_by?: string[];
	    created_at?: string;
	    updated_at?: string;
	    last_used_at?: string;
	    has_lock: boolean;
	    has_python: boolean;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new SharedPythonRuntimeStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.id = source["id"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.manager = source["manager"];
	        this.python = source["python"];
	        this.python_request = source["python_request"];
	        this.packages = source["packages"];
	        this.index_urls = source["index_urls"];
	        this.root_dir = source["root_dir"];
	        this.env_dir = source["env_dir"];
	        this.python_path = source["python_path"];
	        this.lock_path = source["lock_path"];
	        this.cache_dir = source["cache_dir"];
	        this.status = source["status"];
	        this.used_by = source["used_by"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.last_used_at = source["last_used_at"];
	        this.has_lock = source["has_lock"];
	        this.has_python = source["has_python"];
	        this.error = source["error"];
	    }
	}
	export class Skill {
	    name: string;
	    description: string;
	    type: string;
	    value: string;
	    installed: boolean;

	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.value = source["value"];
	        this.installed = source["installed"];
	    }
	}
	export class SystemInfo {
	    os: string;
	    arch: string;
	    os_version: string;

	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.os_version = source["os_version"];
	    }
	}

	export class BrandInfo {
	    id: string;
	    displayName: string;
	    displayNameCN: string;
	    slogan: string;
	    author: string;
	    businessContact: string;
	    websiteURL: string;
	    githubURL: string;
	    iconPath: string;

	    static createFrom(source: any = {}) {
	        return new BrandInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.displayNameCN = source["displayNameCN"];
	        this.slogan = source["slogan"];
	        this.author = source["author"];
	        this.businessContact = source["businessContact"];
	        this.websiteURL = source["websiteURL"];
	        this.githubURL = source["githubURL"];
	        this.iconPath = source["iconPath"];
	    }
	}

	export class ToolStatus {
	    name: string;
	    installed: boolean;
	    version: string;
	    path: string;

	    static createFrom(source: any = {}) {
	        return new ToolStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.path = source["path"];
	    }
	}
	export class UpdateResult {
	    has_update: boolean;
	    latest_version: string;
	    release_url: string;
	    tag_name: string;
	    download_url: string;

	    static createFrom(source: any = {}) {
	        return new UpdateResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.has_update = source["has_update"];
	        this.latest_version = source["latest_version"];
	        this.release_url = source["release_url"];
	        this.tag_name = source["tag_name"];
	        this.download_url = source["download_url"];
	    }
	}
	export class VoiceCommandNormalizationResult {
	    is_command: boolean;
	    corrected_text: string;
	    confidence: number;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new VoiceCommandNormalizationResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.is_command = source["is_command"];
	        this.corrected_text = source["corrected_text"];
	        this.confidence = source["confidence"];
	        this.reason = source["reason"];
	    }
	}

	export class AnthropicOAuthInfo {
	    auth_url: string;

	    static createFrom(source: any = {}) {
	        return new AnthropicOAuthInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auth_url = source["auth_url"];
	    }
	}

	export class GitHubCopilotDeviceInfo {
	    user_code: string;
	    verification_uri: string;

	    static createFrom(source: any = {}) {
	        return new GitHubCopilotDeviceInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user_code = source["user_code"];
	        this.verification_uri = source["verification_uri"];
	    }
	}

	export class ConversationBranchResult {
	    success: boolean;
	    message: string;
	    new_length: number;
	    total_nodes: number;

	    static createFrom(source: any = {}) {
	        return new ConversationBranchResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.new_length = source["new_length"];
	        this.total_nodes = source["total_nodes"];
	    }
	}

	export class ConversationBranchPoint {
	    index: number;
	    entry_id: string;
	    role: string;
	    preview: string;
	    branches: number;
	    labels: string[];

	    static createFrom(source: any = {}) {
	        return new ConversationBranchPoint(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.entry_id = source["entry_id"];
	        this.role = source["role"];
	        this.preview = source["preview"];
	        this.branches = source["branches"];
	        this.labels = source["labels"] || [];
	    }
	}

}
