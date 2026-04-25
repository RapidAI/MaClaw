export namespace main {
	
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
	    timeout_sec: number;
	
	    static createFrom(source: any = {}) {
	        return new CenterConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.base_url = source["base_url"];
	        this.timeout_sec = source["timeout_sec"];
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

}

