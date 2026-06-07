export namespace main {
	
	export class APIError {
	    code: string;
	    message: string;
	    requestId?: string;
	
	    static createFrom(source: any = {}) {
	        return new APIError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.requestId = source["requestId"];
	    }
	}
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
	
	export class IncomingResponse {
	    ok: boolean;
	    code?: string;
	    message?: string;
	    requestId?: string;
	    accepted: boolean;
	    duplicate: boolean;
	    maclawMessageId: string;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new IncomingResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.code = source["code"];
	        this.message = source["message"];
	        this.requestId = source["requestId"];
	        this.accepted = source["accepted"];
	        this.duplicate = source["duplicate"];
	        this.maclawMessageId = source["maclawMessageId"];
	        this.error = this.convertValues(source["error"], APIError);
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
	export class OutgoingMessage {
	    id: string;
	    seq: number;
	    conversationId: string;
	    replyToMessageId?: string;
	    type: string;
	    text?: string;
	    caption?: string;
	    fileName?: string;
	    contentType?: string;
	    data?: string;
	    progress?: boolean;
	    error?: string;
	    createdAt: number;
	    extra?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new OutgoingMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.seq = source["seq"];
	        this.conversationId = source["conversationId"];
	        this.replyToMessageId = source["replyToMessageId"];
	        this.type = source["type"];
	        this.text = source["text"];
	        this.caption = source["caption"];
	        this.fileName = source["fileName"];
	        this.contentType = source["contentType"];
	        this.data = source["data"];
	        this.progress = source["progress"];
	        this.error = source["error"];
	        this.createdAt = source["createdAt"];
	        this.extra = source["extra"];
	    }
	}
	export class OutgoingResponse {
	    ok: boolean;
	    code?: string;
	    message?: string;
	    requestId?: string;
	    messages: OutgoingMessage[];
	    nextCursor: string;
	    hasMore: boolean;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new OutgoingResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.code = source["code"];
	        this.message = source["message"];
	        this.requestId = source["requestId"];
	        this.messages = this.convertValues(source["messages"], OutgoingMessage);
	        this.nextCursor = source["nextCursor"];
	        this.hasMore = source["hasMore"];
	        this.error = this.convertValues(source["error"], APIError);
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
	    }
	}

}

