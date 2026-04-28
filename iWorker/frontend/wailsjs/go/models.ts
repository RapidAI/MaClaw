export namespace main {

	export class AgentInstance {
	    id: string;
	    worker_id: string;
	    role: string;
	    status: string;
	    capabilities: string[];
	    started_at: string;
	    last_heartbeat_at: string;
	    heartbeat_age_seconds: number;
	    effective_status: string;

	    static createFrom(source: any = {}) {
	        return new AgentInstance(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.worker_id = source["worker_id"];
	        this.role = source["role"];
	        this.status = source["status"];
	        this.capabilities = source["capabilities"];
	        this.started_at = source["started_at"];
	        this.last_heartbeat_at = source["last_heartbeat_at"];
	        this.heartbeat_age_seconds = source["heartbeat_age_seconds"];
	        this.effective_status = source["effective_status"];
	    }
	}
	export class AgentRuntimeSnapshot {
	    worker_id: string;
	    tenant_id: string;
	    org_unit_id: string;
	    center_registered: boolean;
	    memory_authority: string;
	    local_memory_behavior: string;
	    parallel_model: string;
	    instances: AgentInstance[];

	    static createFrom(source: any = {}) {
	        return new AgentRuntimeSnapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.worker_id = source["worker_id"];
	        this.tenant_id = source["tenant_id"];
	        this.org_unit_id = source["org_unit_id"];
	        this.center_registered = source["center_registered"];
	        this.memory_authority = source["memory_authority"];
	        this.local_memory_behavior = source["local_memory_behavior"];
	        this.parallel_model = source["parallel_model"];
	        this.instances = this.convertValues(source["instances"], AgentInstance);
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

	export class CenterAgentInstance {
	    tenant_id: string;
	    worker_id: string;
	    instance_id: string;
	    role: string;
	    status: string;
	    org_unit_id?: string;
	    capabilities: string[];
	    memory_authority: string;
	    local_cache_mode: string;
	    host_id?: string;
	    process_id?: number;
	    started_at: string;
	    last_heartbeat_at: string;
	    heartbeat_age_seconds: number;
	    effective_status: string;

	    static createFrom(source: any = {}) {
	        return new CenterAgentInstance(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tenant_id = source["tenant_id"];
	        this.worker_id = source["worker_id"];
	        this.instance_id = source["instance_id"];
	        this.role = source["role"];
	        this.status = source["status"];
	        this.org_unit_id = source["org_unit_id"];
	        this.capabilities = source["capabilities"];
	        this.memory_authority = source["memory_authority"];
	        this.local_cache_mode = source["local_cache_mode"];
	        this.host_id = source["host_id"];
	        this.process_id = source["process_id"];
	        this.started_at = source["started_at"];
	        this.last_heartbeat_at = source["last_heartbeat_at"];
	        this.heartbeat_age_seconds = source["heartbeat_age_seconds"];
	        this.effective_status = source["effective_status"];
	    }
	}

	export class CenterGoalPush {
	    event_id?: string;
	    task_id: string;
	    title: string;
	    to_colleague_id: string;
	    to_role_code: string;
	    status: string;
	    reason: string;
	    recommended_action: string;
	    age_seconds: number;
	    executor_status?: string;
	    executor_heartbeat_age_seconds?: number;
	    created_at: string;

	    static createFrom(source: any = {}) {
	        return new CenterGoalPush(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event_id = source["event_id"];
	        this.task_id = source["task_id"];
	        this.title = source["title"];
	        this.to_colleague_id = source["to_colleague_id"];
	        this.to_role_code = source["to_role_code"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.recommended_action = source["recommended_action"];
	        this.age_seconds = source["age_seconds"];
	        this.executor_status = source["executor_status"];
	        this.executor_heartbeat_age_seconds = source["executor_heartbeat_age_seconds"];
	        this.created_at = source["created_at"];
	    }
	}
	export class CenterGoalPushAckResult {
	    event_id: string;
	    task_id: string;
	    ack_event_id: string;
	    status: string;
	    note?: string;
	    created_at: string;

	    static createFrom(source: any = {}) {
	        return new CenterGoalPushAckResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event_id = source["event_id"];
	        this.task_id = source["task_id"];
	        this.ack_event_id = source["ack_event_id"];
	        this.status = source["status"];
	        this.note = source["note"];
	        this.created_at = source["created_at"];
	    }
	}



	export class AutoHandleGoalPushResult {
	    event_id: string;
	    recommended_action: string;
	    ack_status: string;
	    note: string;
	    heartbeat_sent: boolean;
	    ack: CenterGoalPushAckResult;

	    static createFrom(source: any = {}) {
	        return new AutoHandleGoalPushResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event_id = source["event_id"];
	        this.recommended_action = source["recommended_action"];
	        this.ack_status = source["ack_status"];
	        this.note = source["note"];
	        this.heartbeat_sent = source["heartbeat_sent"];
	        this.ack = this.convertValues(source["ack"], CenterGoalPushAckResult);
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

	export class AppInfo {
	    name: string;
	    tagline: string;

	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.tagline = source["tagline"];
	    }
	}
	export class CenterConfig {
	    enabled: boolean;
	    host: string;
	    port: number;
	    base_url: string;
	    tenant_id: string;
	    department_id: string;
	    worker_id: string;
	    timeout_sec: number;
    goalwatch_auto_handle_enabled: boolean;
    goalwatch_interval_sec: number;
    goalwatch_max_duration_sec: number;

	    static createFrom(source: any = {}) {
	        return new CenterConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.base_url = source["base_url"];
	        this.tenant_id = source["tenant_id"];
	        this.department_id = source["department_id"];
	        this.worker_id = source["worker_id"];
	        this.timeout_sec = source["timeout_sec"];
        this.goalwatch_auto_handle_enabled = source["goalwatch_auto_handle_enabled"];
        this.goalwatch_interval_sec = source["goalwatch_interval_sec"];
        this.goalwatch_max_duration_sec = source["goalwatch_max_duration_sec"];
	    }
	}
	export class CenterHealthStatus {
	    reachable: boolean;
	    status: string;
	    provider_count: number;
	    config_path: string;
	    message: string;
	    resolved_base_url: string;

	    static createFrom(source: any = {}) {
	        return new CenterHealthStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reachable = source["reachable"];
	        this.status = source["status"];
	        this.provider_count = source["provider_count"];
	        this.config_path = source["config_path"];
	        this.message = source["message"];
	        this.resolved_base_url = source["resolved_base_url"];
	    }
	}
	export class Colleague {
	    id: string;
	    name: string;
	    role: string;
	    description: string;
	    strengths: string[];
	    tasks: string[];

	    static createFrom(source: any = {}) {
	        return new Colleague(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.role = source["role"];
	        this.description = source["description"];
	        this.strengths = source["strengths"];
	        this.tasks = source["tasks"];
	    }
	}
	export class ProviderCapabilities {
	    supports_stream: boolean;
	    supports_vision: boolean;
	    max_context: number;

	    static createFrom(source: any = {}) {
	        return new ProviderCapabilities(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supports_stream = source["supports_stream"];
	        this.supports_vision = source["supports_vision"];
	        this.max_context = source["max_context"];
	    }
	}
	export class UpstreamProvider {
	    id: string;
	    name: string;
	    enabled: boolean;
	    protocol: string;
	    base_url: string;
	    api_key: string;
	    model: string;
	    priority: number;
	    features: string[];
	    description: string;
	    capabilities: ProviderCapabilities;

	    static createFrom(source: any = {}) {
	        return new UpstreamProvider(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.protocol = source["protocol"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.priority = source["priority"];
	        this.features = source["features"];
	        this.description = source["description"];
	        this.capabilities = this.convertValues(source["capabilities"], ProviderCapabilities);
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
	export class RoutingPolicy {
	    mode: string;
	    default_provider: string;
	    allow_fallback: boolean;

	    static createFrom(source: any = {}) {
	        return new RoutingPolicy(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.default_provider = source["default_provider"];
	        this.allow_fallback = source["allow_fallback"];
	    }
	}
	export class RoleProfile {
	    name: string;
	    description: string;

	    static createFrom(source: any = {}) {
	        return new RoleProfile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class DiWorkerSettings {
	    role_profile: RoleProfile;
	    center: CenterConfig;
	    routing: RoutingPolicy;
	    providers: UpstreamProvider[];

	    static createFrom(source: any = {}) {
	        return new DiWorkerSettings(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role_profile = this.convertValues(source["role_profile"], RoleProfile);
	        this.center = this.convertValues(source["center"], CenterConfig);
	        this.routing = this.convertValues(source["routing"], RoutingPolicy);
	        this.providers = this.convertValues(source["providers"], UpstreamProvider);
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
	export class WorkerMemoryEntry {
	    id: string;
	    tenant_id: string;
	    department_id?: string;
	    worker_id?: string;
	    scope: string;
	    content: string;
	    category: string;
	    tags: string[];
	    source_type?: string;
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new WorkerMemoryEntry(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tenant_id = source["tenant_id"];
	        this.department_id = source["department_id"];
	        this.worker_id = source["worker_id"];
	        this.scope = source["scope"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.tags = source["tags"];
	        this.source_type = source["source_type"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class WorkerMemoryStats {
	    tenant_id: string;
	    department_id?: string;
	    worker_id?: string;
	    total: number;
	    by_scope: {[key: string]: number};
	    by_category: {[key: string]: number};
	    visible_scopes: string[];

	    static createFrom(source: any = {}) {
	        return new WorkerMemoryStats(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tenant_id = source["tenant_id"];
	        this.department_id = source["department_id"];
	        this.worker_id = source["worker_id"];
	        this.total = source["total"];
	        this.by_scope = source["by_scope"];
	        this.by_category = source["by_category"];
	        this.visible_scopes = source["visible_scopes"];
	    }
	}
	export class SaveWorkerMemoryRequest {
	    scope: string;
	    content: string;
	    category: string;
	    tags: string[];
	    source_type: string;

	    static createFrom(source: any = {}) {
	        return new SaveWorkerMemoryRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.tags = source["tags"];
	        this.source_type = source["source_type"];
	    }
	}

	export class HistoryTaskItem {
	    id: string;
	    title: string;
	    owner: string;
	    status: string;
	    updated_at: string;
	    description: string;
	    draft?: string;
	    expected_output?: string;
	    result?: string;
	    model?: string;

	    static createFrom(source: any = {}) {
	        return new HistoryTaskItem(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.owner = source["owner"];
	        this.status = source["status"];
	        this.updated_at = source["updated_at"];
	        this.description = source["description"];
	        this.draft = source["draft"];
	        this.expected_output = source["expected_output"];
	        this.result = source["result"];
	        this.model = source["model"];
	    }
	}



	export class SubmitTaskRequest {
	    task_type: string;
	    selected_colleague_name: string;
	    draft: string;
	    expected_output: string;

	    static createFrom(source: any = {}) {
	        return new SubmitTaskRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_type = source["task_type"];
	        this.selected_colleague_name = source["selected_colleague_name"];
	        this.draft = source["draft"];
	        this.expected_output = source["expected_output"];
	    }
	}
	export class SubmitTaskResult {
	    task_type: string;
	    colleague_name: string;
	    expected_output: string;
	    model: string;
	    content: string;

	    static createFrom(source: any = {}) {
	        return new SubmitTaskResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_type = source["task_type"];
	        this.colleague_name = source["colleague_name"];
	        this.expected_output = source["expected_output"];
	        this.model = source["model"];
	        this.content = source["content"];
	    }
	}
	export class TaskItem {
	    id: string;
	    title: string;
	    owner: string;
	    status: string;
	    updated_at: string;
	    description: string;

	    static createFrom(source: any = {}) {
	        return new TaskItem(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.owner = source["owner"];
	        this.status = source["status"];
	        this.updated_at = source["updated_at"];
	        this.description = source["description"];
	    }
	}

	export class WelcomeData {
	    greeting: string;
	    hint: string;
	    colleagues: Colleague[];
	    quick_tasks: string[];
	    recent_tasks: TaskItem[];

	    static createFrom(source: any = {}) {
	        return new WelcomeData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.greeting = source["greeting"];
	        this.hint = source["hint"];
	        this.colleagues = this.convertValues(source["colleagues"], Colleague);
	        this.quick_tasks = source["quick_tasks"];
	        this.recent_tasks = this.convertValues(source["recent_tasks"], TaskItem);
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

	export class GoalWatchAutoHandleStatus {
	    enabled: boolean;
	    running: boolean;
	    current_run_id: number;
	    run_count: number;
	    skip_count: number;
	    timeout_cancel_count: number;
	    last_handled_count: number;
	    total_handled_count: number;
	    last_error: string;
	    last_started_at: string;
	    last_finished_at: string;
	    last_timeout_at: string;
	    interval_seconds: number;
	    max_duration_seconds: number;

	    static createFrom(source: any = {}) {
	        return new GoalWatchAutoHandleStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.running = source["running"];
	        this.current_run_id = source["current_run_id"];
	        this.run_count = source["run_count"];
	        this.skip_count = source["skip_count"];
	        this.timeout_cancel_count = source["timeout_cancel_count"];
	        this.last_handled_count = source["last_handled_count"];
	        this.total_handled_count = source["total_handled_count"];
	        this.last_error = source["last_error"];
	        this.last_started_at = source["last_started_at"];
	        this.last_finished_at = source["last_finished_at"];
	        this.last_timeout_at = source["last_timeout_at"];
	        this.interval_seconds = source["interval_seconds"];
	        this.max_duration_seconds = source["max_duration_seconds"];
	    }
	}
}
