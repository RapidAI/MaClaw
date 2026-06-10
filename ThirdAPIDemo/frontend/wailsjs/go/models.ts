export namespace im {

	export class ThirdPartyMediaReference {
	    id?: string;
	    type?: string;
	    fileName?: string;
	    contentType?: string;
	    mimeType?: string;
	    data?: string;
	    url?: string;
	    sizeBytes?: number;
	    durationMs?: number;
	    sha256?: string;
	    metadata?: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new ThirdPartyMediaReference(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.fileName = source["fileName"];
	        this.contentType = source["contentType"];
	        this.mimeType = source["mimeType"];
	        this.data = source["data"];
	        this.url = source["url"];
	        this.sizeBytes = source["sizeBytes"];
	        this.durationMs = source["durationMs"];
	        this.sha256 = source["sha256"];
	        this.metadata = source["metadata"];
	    }
	}
	export class ThirdPartyToolCancel {
	    toolCallId?: string;
	    toolPlanId?: string;
	    stepId?: string;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new ThirdPartyToolCancel(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.toolCallId = source["toolCallId"];
	        this.toolPlanId = source["toolPlanId"];
	        this.stepId = source["stepId"];
	        this.reason = source["reason"];
	    }
	}
	export class ThirdPartyToolPlanStep {
	    id: string;
	    tool: string;
	    arguments?: Record<string, any>;
	    dependsOn?: string[];
	    risk?: string;
	    requiresApproval?: boolean;
	    idempotencyKey?: string;
	    timeoutMs?: number;
	    metadata?: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new ThirdPartyToolPlanStep(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tool = source["tool"];
	        this.arguments = source["arguments"];
	        this.dependsOn = source["dependsOn"];
	        this.risk = source["risk"];
	        this.requiresApproval = source["requiresApproval"];
	        this.idempotencyKey = source["idempotencyKey"];
	        this.timeoutMs = source["timeoutMs"];
	        this.metadata = source["metadata"];
	    }
	}
	export class ThirdPartyToolPlan {
	    id: string;
	    mode?: string;
	    steps: ThirdPartyToolPlanStep[];
	    requiresApproval?: boolean;
	    metadata?: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new ThirdPartyToolPlan(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.mode = source["mode"];
	        this.steps = this.convertValues(source["steps"], ThirdPartyToolPlanStep);
	        this.requiresApproval = source["requiresApproval"];
	        this.metadata = source["metadata"];
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
	export class ThirdPartyToolCall {
	    id: string;
	    name: string;
	    arguments?: Record<string, any>;
	    risk?: string;
	    requiresApproval?: boolean;
	    idempotencyKey?: string;
	    timeoutMs?: number;
	    metadata?: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new ThirdPartyToolCall(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.arguments = source["arguments"];
	        this.risk = source["risk"];
	        this.requiresApproval = source["requiresApproval"];
	        this.idempotencyKey = source["idempotencyKey"];
	        this.timeoutMs = source["timeoutMs"];
	        this.metadata = source["metadata"];
	    }
	}
	export class ThirdPartyOutgoingMessage {
	    id: string;
	    seq?: number;
	    cursor?: string;
	    replyTo?: string;
	    clientId?: string;
	    conversationId?: string;
	    replyToMessageId?: string;
	    type: string;
	    text?: string;
	    caption?: string;
	    fileName?: string;
	    contentType?: string;
	    mimeType?: string;
	    data?: string;
	    url?: string;
	    sizeBytes?: number;
	    durationMs?: number;
	    attachments?: ThirdPartyMediaReference[];
	    toolCall?: ThirdPartyToolCall;
	    toolPlan?: ThirdPartyToolPlan;
	    toolCancel?: ThirdPartyToolCancel;
	    progress?: boolean;
	    error?: string;
	    createdAt: number;
	    metadata?: Record<string, string>;
	    extra?: Record<string, any>;

	    static createFrom(source: any = {}) {
	        return new ThirdPartyOutgoingMessage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.seq = source["seq"];
	        this.cursor = source["cursor"];
	        this.replyTo = source["replyTo"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.replyToMessageId = source["replyToMessageId"];
	        this.type = source["type"];
	        this.text = source["text"];
	        this.caption = source["caption"];
	        this.fileName = source["fileName"];
	        this.contentType = source["contentType"];
	        this.mimeType = source["mimeType"];
	        this.data = source["data"];
	        this.url = source["url"];
	        this.sizeBytes = source["sizeBytes"];
	        this.durationMs = source["durationMs"];
	        this.attachments = this.convertValues(source["attachments"], ThirdPartyMediaReference);
	        this.toolCall = this.convertValues(source["toolCall"], ThirdPartyToolCall);
	        this.toolPlan = this.convertValues(source["toolPlan"], ThirdPartyToolPlan);
	        this.toolCancel = this.convertValues(source["toolCancel"], ThirdPartyToolCancel);
	        this.progress = source["progress"];
	        this.error = source["error"];
	        this.createdAt = source["createdAt"];
	        this.metadata = source["metadata"];
	        this.extra = source["extra"];
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

export namespace main {

	export class ConnectInput {
	    baseUrl: string;
	    apiKey: string;
	    clientId: string;
	    conversationId: string;
	    userId: string;
	    userName: string;

	    static createFrom(source: any = {}) {
	        return new ConnectInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.userId = source["userId"];
	        this.userName = source["userName"];
	    }
	}
	export class GatewayConfig {
	    baseUrl: string;
	    apiKey: string;
	    clientId: string;
	    conversationId: string;
	    userId: string;
	    userName: string;

	    static createFrom(source: any = {}) {
	        return new GatewayConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.userId = source["userId"];
	        this.userName = source["userName"];
	    }
	}
	export class ConnectResult {
	    config: GatewayConfig;
	    handshake: any;
	    cursor: string;

	    static createFrom(source: any = {}) {
	        return new ConnectResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], GatewayConfig);
	        this.handshake = source["handshake"];
	        this.cursor = source["cursor"];
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
	export class DownloadInput {
	    baseUrl: string;
	    apiKey: string;
	    clientId: string;
	    conversationId: string;
	    userId: string;
	    userName: string;
	    url: string;
	    fileName?: string;

	    static createFrom(source: any = {}) {
	        return new DownloadInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.userId = source["userId"];
	        this.userName = source["userName"];
	        this.url = source["url"];
	        this.fileName = source["fileName"];
	    }
	}
	export class DownloadResult {
	    path: string;
	    bytes: number;
	    fileName: string;
	    mimeType?: string;

	    static createFrom(source: any = {}) {
	        return new DownloadResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.bytes = source["bytes"];
	        this.fileName = source["fileName"];
	        this.mimeType = source["mimeType"];
	    }
	}

	export class IncomingResponse {
	    ok: boolean;
	    requestId?: string;
	    accepted: boolean;
	    duplicate: boolean;
	    maclawMessageId: string;

	    static createFrom(source: any = {}) {
	        return new IncomingResponse(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.requestId = source["requestId"];
	        this.accepted = source["accepted"];
	        this.duplicate = source["duplicate"];
	        this.maclawMessageId = source["maclawMessageId"];
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
	export class OutgoingResponse {
	    ok: boolean;
	    requestId?: string;
	    messages: im.ThirdPartyOutgoingMessage[];
	    nextCursor: string;
	    hasMore: boolean;

	    static createFrom(source: any = {}) {
	        return new OutgoingResponse(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.requestId = source["requestId"];
	        this.messages = this.convertValues(source["messages"], im.ThirdPartyOutgoingMessage);
	        this.nextCursor = source["nextCursor"];
	        this.hasMore = source["hasMore"];
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
	export class PollInput {
	    baseUrl: string;
	    apiKey: string;
	    clientId: string;
	    conversationId: string;
	    userId: string;
	    userName: string;
	    cursor: string;
	    timeout: number;
	    limit: number;

	    static createFrom(source: any = {}) {
	        return new PollInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.userId = source["userId"];
	        this.userName = source["userName"];
	        this.cursor = source["cursor"];
	        this.timeout = source["timeout"];
	        this.limit = source["limit"];
	    }
	}
	export class SendInput {
	    baseUrl: string;
	    apiKey: string;
	    clientId: string;
	    conversationId: string;
	    userId: string;
	    userName: string;
	    text: string;
	    messageType?: string;
	    attachments?: im.ThirdPartyMediaReference[];

	    static createFrom(source: any = {}) {
	        return new SendInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.userId = source["userId"];
	        this.userName = source["userName"];
	        this.text = source["text"];
	        this.messageType = source["messageType"];
	        this.attachments = this.convertValues(source["attachments"], im.ThirdPartyMediaReference);
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
	export class ToolExecuteInput {
	    baseUrl: string;
	    apiKey: string;
	    clientId: string;
	    conversationId: string;
	    userId: string;
	    userName: string;
	    message: im.ThirdPartyOutgoingMessage;

	    static createFrom(source: any = {}) {
	        return new ToolExecuteInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.clientId = source["clientId"];
	        this.conversationId = source["conversationId"];
	        this.userId = source["userId"];
	        this.userName = source["userName"];
	        this.message = this.convertValues(source["message"], im.ThirdPartyOutgoingMessage);
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
	export class ToolExecuteResult {
	    ok: boolean;
	    message?: string;

	    static createFrom(source: any = {}) {
	        return new ToolExecuteResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	    }
	}

}
