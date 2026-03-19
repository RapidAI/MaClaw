export namespace main {
	
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
	export class AppConfig {
	    claude: ToolConfig;
	    gemini: ToolConfig;
	    codex: ToolConfig;
	    opencode: ToolConfig;
	    codebuddy: ToolConfig;
	    qoder: ToolConfig;
	    iflow: ToolConfig;
	    kilo: ToolConfig;
	    cursor: ToolConfig;
	    projects: ProjectConfig[];
	    current_project: string;
	    active_tool: string;
	    hide_startup_popup: boolean;
	    hide_maclaw_llm_popup: boolean;
	    show_gemini: boolean;
	    show_codex: boolean;
	    show_opencode: boolean;
	    show_codebuddy: boolean;
	    show_qoder: boolean;
	    show_iflow: boolean;
	    show_kilo: boolean;
	    show_cursor: boolean;
	    language: string;
	    power_optimization: boolean;
	    check_update_on_startup: boolean;
	    pause_env_check: boolean;
	    env_check_done: boolean;
	    env_check_interval: number;
	    last_env_check_time: string;
	    default_proxy_host: string;
	    default_proxy_port: string;
	    default_proxy_username: string;
	    default_proxy_password: string;
	    use_windows_terminal: boolean;
	    remote_enabled: boolean;
	    remote_hub_url: string;
	    remote_hubcenter_url: string;
	    remote_email: string;
	    remote_mobile: string;
	    remote_sn: string;
	    remote_user_id: string;
	    remote_machine_id: string;
	    remote_machine_token: string;
	    remote_heartbeat_sec: number;
	    remote_client_id: string;
	    default_launch_mode: string;
	    maclaw_llm_url: string;
	    maclaw_llm_key: string;
	    maclaw_llm_model: string;
	    maclaw_role_name: string;
	    maclaw_role_description: string;
	    maclaw_llm_protocol: string;
	    maclaw_llm_context_length: number;
	    maclaw_llm_providers: any[];
	    maclaw_llm_current_provider: string;
	    maclaw_agent_max_iterations: number;
	    screen_dim_timeout_min: number;
	    mcp_servers: any[];
	    local_mcp_servers: any[];
	    nl_skills: any[];
	    memory_auto_compress: boolean;
	    clawnet_enabled: boolean;
	    skill_hub_urls: any[];
	    maclaw_debug_tool_calls: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.claude = this.convertValues(source["claude"], ToolConfig);
	        this.gemini = this.convertValues(source["gemini"], ToolConfig);
	        this.codex = this.convertValues(source["codex"], ToolConfig);
	        this.opencode = this.convertValues(source["opencode"], ToolConfig);
	        this.codebuddy = this.convertValues(source["codebuddy"], ToolConfig);
	        this.qoder = this.convertValues(source["qoder"], ToolConfig);
	        this.iflow = this.convertValues(source["iflow"], ToolConfig);
	        this.kilo = this.convertValues(source["kilo"], ToolConfig);
	        this.cursor = this.convertValues(source["cursor"], ToolConfig);
	        this.projects = this.convertValues(source["projects"], ProjectConfig);
	        this.current_project = source["current_project"];
	        this.active_tool = source["active_tool"];
	        this.hide_startup_popup = source["hide_startup_popup"];
	        this.hide_maclaw_llm_popup = source["hide_maclaw_llm_popup"];
	        this.show_gemini = source["show_gemini"];
	        this.show_codex = source["show_codex"];
	        this.show_opencode = source["show_opencode"];
	        this.show_codebuddy = source["show_codebuddy"];
	        this.show_qoder = source["show_qoder"];
	        this.show_iflow = source["show_iflow"];
	        this.show_kilo = source["show_kilo"];
	        this.show_cursor = source["show_cursor"];
	        this.language = source["language"];
	        this.power_optimization = source["power_optimization"];
	        this.check_update_on_startup = source["check_update_on_startup"];
	        this.pause_env_check = source["pause_env_check"];
	        this.env_check_done = source["env_check_done"];
	        this.env_check_interval = source["env_check_interval"];
	        this.last_env_check_time = source["last_env_check_time"];
	        this.default_proxy_host = source["default_proxy_host"];
	        this.default_proxy_port = source["default_proxy_port"];
	        this.default_proxy_username = source["default_proxy_username"];
	        this.default_proxy_password = source["default_proxy_password"];
	        this.use_windows_terminal = source["use_windows_terminal"];
	        this.remote_enabled = source["remote_enabled"];
	        this.remote_hub_url = source["remote_hub_url"];
	        this.remote_hubcenter_url = source["remote_hubcenter_url"];
	        this.remote_email = source["remote_email"];
	        this.remote_mobile = source["remote_mobile"];
	        this.remote_sn = source["remote_sn"];
	        this.remote_user_id = source["remote_user_id"];
	        this.remote_machine_id = source["remote_machine_id"];
	        this.remote_machine_token = source["remote_machine_token"];
	        this.remote_heartbeat_sec = source["remote_heartbeat_sec"];
	        this.remote_client_id = source["remote_client_id"];
	        this.default_launch_mode = source["default_launch_mode"];
	        this.maclaw_llm_url = source["maclaw_llm_url"];
	        this.maclaw_llm_key = source["maclaw_llm_key"];
	        this.maclaw_llm_model = source["maclaw_llm_model"];
	        this.maclaw_role_name = source["maclaw_role_name"];
	        this.maclaw_role_description = source["maclaw_role_description"];
	        this.maclaw_llm_protocol = source["maclaw_llm_protocol"];
	        this.maclaw_llm_context_length = source["maclaw_llm_context_length"];
	        this.maclaw_llm_providers = source["maclaw_llm_providers"];
	        this.maclaw_llm_current_provider = source["maclaw_llm_current_provider"];
	        this.maclaw_agent_max_iterations = source["maclaw_agent_max_iterations"];
	        this.screen_dim_timeout_min = source["screen_dim_timeout_min"];
	        this.mcp_servers = source["mcp_servers"];
	        this.local_mcp_servers = source["local_mcp_servers"];
	        this.nl_skills = source["nl_skills"];
	        this.memory_auto_compress = source["memory_auto_compress"];
	        this.clawnet_enabled = source["clawnet_enabled"];
	        this.skill_hub_urls = source["skill_hub_urls"];
	        this.maclaw_debug_tool_calls = source["maclaw_debug_tool_calls"];
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

}
