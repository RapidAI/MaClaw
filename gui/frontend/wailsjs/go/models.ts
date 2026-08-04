export namespace a2a {
	
	export class GroupConsultationRequest {
	    id: string;
	    from_id: string;
	    topic?: string;
	    question: string;
	    context_summary?: string;
	    skills_wanted?: string[];
	    risk_level?: string;
	    max_rounds?: number;
	    timeout_seconds?: number;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new GroupConsultationRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.from_id = source["from_id"];
	        this.topic = source["topic"];
	        this.question = source["question"];
	        this.context_summary = source["context_summary"];
	        this.skills_wanted = source["skills_wanted"];
	        this.risk_level = source["risk_level"];
	        this.max_rounds = source["max_rounds"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class HubDiscussionSummary {
	    id: string;
	    role?: string;
	    local_relation?: string;
	    readonly: boolean;
	    status?: string;
	    topic?: string;
	    question?: string;
	    result_summary?: string;
	    participant_ids?: string[];
	    message_count?: number;
	    answer_count?: number;
	    expected_answer_count?: number;
	    ready_to_summarize?: boolean;
	    readiness_reason?: string;
	    // Go type: time
	    created_at?: any;
	    // Go type: time
	    updated_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new HubDiscussionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.local_relation = source["local_relation"];
	        this.readonly = source["readonly"];
	        this.status = source["status"];
	        this.topic = source["topic"];
	        this.question = source["question"];
	        this.result_summary = source["result_summary"];
	        this.participant_ids = source["participant_ids"];
	        this.message_count = source["message_count"];
	        this.answer_count = source["answer_count"];
	        this.expected_answer_count = source["expected_answer_count"];
	        this.ready_to_summarize = source["ready_to_summarize"];
	        this.readiness_reason = source["readiness_reason"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class ConsultationCreateResponse {
	    discussion: HubDiscussionSummary;
	    request?: GroupConsultationRequest;
	
	    static createFrom(source: any = {}) {
	        return new ConsultationCreateResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.discussion = this.convertValues(source["discussion"], HubDiscussionSummary);
	        this.request = this.convertValues(source["request"], GroupConsultationRequest);
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
	export class Decision {
	    id: string;
	    session_id: string;
	    proposal_id: string;
	    summary: string;
	    rationale?: string;
	    decided_by: string[];
	    // Go type: time
	    valid_until?: any;
	    rollback_on?: string[];
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Decision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.proposal_id = source["proposal_id"];
	        this.summary = source["summary"];
	        this.rationale = source["rationale"];
	        this.decided_by = source["decided_by"];
	        this.valid_until = this.convertValues(source["valid_until"], null);
	        this.rollback_on = source["rollback_on"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class Escalation {
	    id: string;
	    session_id: string;
	    raised_by: string;
	    reason: string;
	    target: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Escalation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.raised_by = source["raised_by"];
	        this.reason = source["reason"];
	        this.target = source["target"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class FileAttachment {
	    file_url: string;
	    filename: string;
	    mime_type?: string;
	    size_bytes?: number;
	    local_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_url = source["file_url"];
	        this.filename = source["filename"];
	        this.mime_type = source["mime_type"];
	        this.size_bytes = source["size_bytes"];
	        this.local_path = source["local_path"];
	    }
	}
	
	export class ImageAttachment {
	    file_url: string;
	    filename: string;
	    mime_type?: string;
	    width?: number;
	    height?: number;
	    local_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_url = source["file_url"];
	        this.filename = source["filename"];
	        this.mime_type = source["mime_type"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.local_path = source["local_path"];
	    }
	}
	export class TextAttachment {
	    content: string;
	    filename: string;
	    mime_type?: string;
	    local_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new TextAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.filename = source["filename"];
	        this.mime_type = source["mime_type"];
	        this.local_path = source["local_path"];
	    }
	}
	export class GroupDiscussionMessage {
	    id: string;
	    session_id: string;
	    from_id: string;
	    to_ids?: string[];
	    kind: string;
	    content: string;
	    text_attachments?: TextAttachment[];
	    image_attachments?: ImageAttachment[];
	    file_attachments?: FileAttachment[];
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.from_id = source["from_id"];
	        this.to_ids = source["to_ids"];
	        this.kind = source["kind"];
	        this.content = source["content"];
	        this.text_attachments = this.convertValues(source["text_attachments"], TextAttachment);
	        this.image_attachments = this.convertValues(source["image_attachments"], ImageAttachment);
	        this.file_attachments = this.convertValues(source["file_attachments"], FileAttachment);
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class GroupDiscussionResult {
	    session_id: string;
	    summary: string;
	    rationale?: string;
	    risks?: string[];
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.summary = source["summary"];
	        this.rationale = source["rationale"];
	        this.risks = source["risks"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class GroupInvitation {
	    request_id: string;
	    from_id: string;
	    to_id: string;
	    role: string;
	    trusted?: boolean;
	    security_group_id?: string;
	    context_policy?: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupInvitation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.from_id = source["from_id"];
	        this.to_id = source["to_id"];
	        this.role = source["role"];
	        this.trusted = source["trusted"];
	        this.security_group_id = source["security_group_id"];
	        this.context_policy = source["context_policy"];
	    }
	}
	export class GroupInvitationResponse {
	    request_id: string;
	    from_id: string;
	    to_id: string;
	    decision: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupInvitationResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.from_id = source["from_id"];
	        this.to_id = source["to_id"];
	        this.decision = source["decision"];
	        this.reason = source["reason"];
	    }
	}
	export class GroupInviteSummary {
	    id: string;
	    session_id: string;
	    request_id?: string;
	    from_id: string;
	    to_id: string;
	    role: string;
	    trusted?: boolean;
	    security_group_id?: string;
	    context_policy?: string;
	    status: string;
	    reason?: string;
	    topic?: string;
	    question?: string;
	    // Go type: time
	    created_at?: any;
	    // Go type: time
	    responded_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new GroupInviteSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.request_id = source["request_id"];
	        this.from_id = source["from_id"];
	        this.to_id = source["to_id"];
	        this.role = source["role"];
	        this.trusted = source["trusted"];
	        this.security_group_id = source["security_group_id"];
	        this.context_policy = source["context_policy"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.topic = source["topic"];
	        this.question = source["question"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.responded_at = this.convertValues(source["responded_at"], null);
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
	export class GroupProfile {
	    agent_id: string;
	    display_name?: string;
	    skills?: string[];
	    description?: string;
	    model_class?: string;
	    languages?: string[];
	    security_group_id?: string;
	    contribution_score?: number;
	    contribution_evidence?: number;
	    discoverable: boolean;
	    available: boolean;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new GroupProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_id = source["agent_id"];
	        this.display_name = source["display_name"];
	        this.skills = source["skills"];
	        this.description = source["description"];
	        this.model_class = source["model_class"];
	        this.languages = source["languages"];
	        this.security_group_id = source["security_group_id"];
	        this.contribution_score = source["contribution_score"];
	        this.contribution_evidence = source["contribution_evidence"];
	        this.discoverable = source["discoverable"];
	        this.available = source["available"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class ReviewSummary {
	    approvals: number;
	    rejections: number;
	    concerns: number;
	    abstains: number;
	    reviewed_by: string[];
	
	    static createFrom(source: any = {}) {
	        return new ReviewSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.approvals = source["approvals"];
	        this.rejections = source["rejections"];
	        this.concerns = source["concerns"];
	        this.abstains = source["abstains"];
	        this.reviewed_by = source["reviewed_by"];
	    }
	}
	export class Review {
	    id: string;
	    session_id: string;
	    proposal_id: string;
	    reviewer_id: string;
	    position: string;
	    comment?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Review(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.proposal_id = source["proposal_id"];
	        this.reviewer_id = source["reviewer_id"];
	        this.position = source["position"];
	        this.comment = source["comment"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class Proposal {
	    id: string;
	    session_id: string;
	    author_id: string;
	    title: string;
	    content: string;
	    goals?: string[];
	    constraints?: string[];
	    risks?: string[];
	    status: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Proposal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.author_id = source["author_id"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.goals = source["goals"];
	        this.constraints = source["constraints"];
	        this.risks = source["risks"];
	        this.status = source["status"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class Message {
	    id: string;
	    session_id: string;
	    from_id: string;
	    to_ids?: string[];
	    kind: string;
	    content: string;
	    evidence?: string[];
	    text_attachments?: TextAttachment[];
	    image_attachments?: ImageAttachment[];
	    file_attachments?: FileAttachment[];
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.from_id = source["from_id"];
	        this.to_ids = source["to_ids"];
	        this.kind = source["kind"];
	        this.content = source["content"];
	        this.evidence = source["evidence"];
	        this.text_attachments = this.convertValues(source["text_attachments"], TextAttachment);
	        this.image_attachments = this.convertValues(source["image_attachments"], ImageAttachment);
	        this.file_attachments = this.convertValues(source["file_attachments"], FileAttachment);
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class Participant {
	    id: string;
	    role_code?: string;
	    name?: string;
	    skills?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Participant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role_code = source["role_code"];
	        this.name = source["name"];
	        this.skills = source["skills"];
	    }
	}
	export class Session {
	    id: string;
	    tenant_id?: string;
	    org_unit_id?: string;
	    topic: string;
	    goal?: string;
	    status: string;
	    decision_policy: string;
	    participants: Participant[];
	    context_summary?: string;
	    summary_up_to_id?: string;
	    // Go type: time
	    summary_updated_at?: any;
	    default_reply_targets?: Record<string, Array<string>>;
	    messages?: Message[];
	    proposals?: Proposal[];
	    reviews?: Review[];
	    decision?: Decision;
	    escalation?: Escalation;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tenant_id = source["tenant_id"];
	        this.org_unit_id = source["org_unit_id"];
	        this.topic = source["topic"];
	        this.goal = source["goal"];
	        this.status = source["status"];
	        this.decision_policy = source["decision_policy"];
	        this.participants = this.convertValues(source["participants"], Participant);
	        this.context_summary = source["context_summary"];
	        this.summary_up_to_id = source["summary_up_to_id"];
	        this.summary_updated_at = this.convertValues(source["summary_updated_at"], null);
	        this.default_reply_targets = source["default_reply_targets"];
	        this.messages = this.convertValues(source["messages"], Message);
	        this.proposals = this.convertValues(source["proposals"], Proposal);
	        this.reviews = this.convertValues(source["reviews"], Review);
	        this.decision = this.convertValues(source["decision"], Decision);
	        this.escalation = this.convertValues(source["escalation"], Escalation);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class HubDiscussionDetail {
	    discussion: HubDiscussionSummary;
	    session?: Session;
	    messages?: Message[];
	    proposals?: Proposal[];
	    reviews?: Review[];
	    review_summaries?: Record<string, ReviewSummary>;
	    decision?: Decision;
	
	    static createFrom(source: any = {}) {
	        return new HubDiscussionDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.discussion = this.convertValues(source["discussion"], HubDiscussionSummary);
	        this.session = this.convertValues(source["session"], Session);
	        this.messages = this.convertValues(source["messages"], Message);
	        this.proposals = this.convertValues(source["proposals"], Proposal);
	        this.reviews = this.convertValues(source["reviews"], Review);
	        this.review_summaries = this.convertValues(source["review_summaries"], ReviewSummary, true);
	        this.decision = this.convertValues(source["decision"], Decision);
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

export namespace agent {
	
	export class TurnUsage {
	    model?: string;
	    provider?: string;
	    input_tokens?: number;
	    output_tokens?: number;
	    cached_tokens?: number;
	    cache_write_tokens?: number;
	    est_cost_rmb?: number;
	    est_cost_usd?: number;
	    requests?: number;
	
	    static createFrom(source: any = {}) {
	        return new TurnUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.provider = source["provider"];
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.cached_tokens = source["cached_tokens"];
	        this.cache_write_tokens = source["cache_write_tokens"];
	        this.est_cost_rmb = source["est_cost_rmb"];
	        this.est_cost_usd = source["est_cost_usd"];
	        this.requests = source["requests"];
	    }
	}

}

export namespace corelib {
	
	export class MoAModelRef {
	    provider?: string;
	    task_route?: string;
	    model?: string;
	    url?: string;
	    key?: string;
	    protocol?: string;
	    wire_api?: string;
	    use_primary?: boolean;
	    use_aux?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MoAModelRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.task_route = source["task_route"];
	        this.model = source["model"];
	        this.url = source["url"];
	        this.key = source["key"];
	        this.protocol = source["protocol"];
	        this.wire_api = source["wire_api"];
	        this.use_primary = source["use_primary"];
	        this.use_aux = source["use_aux"];
	    }
	}
	export class MoAPresetConfig {
	    enabled: boolean;
	    reference_models?: MoAModelRef[];
	    aggregator: MoAModelRef;
	    reference_max_tokens?: number;
	    max_tokens?: number;
	    fanout_max_iterations?: number;
	    only_before_first_tool?: boolean;
	    display_name?: string;
	
	    static createFrom(source: any = {}) {
	        return new MoAPresetConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.reference_models = this.convertValues(source["reference_models"], MoAModelRef);
	        this.aggregator = this.convertValues(source["aggregator"], MoAModelRef);
	        this.reference_max_tokens = source["reference_max_tokens"];
	        this.max_tokens = source["max_tokens"];
	        this.fanout_max_iterations = source["fanout_max_iterations"];
	        this.only_before_first_tool = source["only_before_first_tool"];
	        this.display_name = source["display_name"];
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
	export class MoAConfig {
	    enabled: boolean;
	    default_preset?: string;
	    allow_auto?: boolean;
	    max_references?: number;
	    reference_timeout_sec?: number;
	    fanout_max_iterations?: number;
	    only_before_first_tool?: boolean;
	    presets?: Record<string, MoAPresetConfig>;
	
	    static createFrom(source: any = {}) {
	        return new MoAConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.default_preset = source["default_preset"];
	        this.allow_auto = source["allow_auto"];
	        this.max_references = source["max_references"];
	        this.reference_timeout_sec = source["reference_timeout_sec"];
	        this.fanout_max_iterations = source["fanout_max_iterations"];
	        this.only_before_first_tool = source["only_before_first_tool"];
	        this.presets = this.convertValues(source["presets"], MoAPresetConfig, true);
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
	export class ModelRouteConfig {
	    model: string;
	    url?: string;
	    key?: string;
	    protocol?: string;
	    provider?: string;
	
	    static createFrom(source: any = {}) {
	        return new ModelRouteConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.url = source["url"];
	        this.key = source["key"];
	        this.protocol = source["protocol"];
	        this.provider = source["provider"];
	    }
	}
	export class AuxiliaryLLMConfig {
	    url: string;
	    key: string;
	    model: string;
	    protocol?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuxiliaryLLMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.key = source["key"];
	        this.model = source["model"];
	        this.protocol = source["protocol"];
	    }
	}
	export class KnowledgeVisionLLMConfig {
	    enabled: boolean;
	    base_url?: string;
	    api_key?: string;
	    model?: string;
	    max_tokens?: number;
	    timeout_sec?: number;
	    verified?: boolean;
	    from_main_llm?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeVisionLLMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.max_tokens = source["max_tokens"];
	        this.timeout_sec = source["timeout_sec"];
	        this.verified = source["verified"];
	        this.from_main_llm = source["from_main_llm"];
	    }
	}
	export class SSHHostEntry {
	    label: string;
	    host: string;
	    port?: number;
	    user: string;
	    auth_method?: string;
	    key_path?: string;
	    password?: string;
	    passphrase?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHHostEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.auth_method = source["auth_method"];
	        this.key_path = source["key_path"];
	        this.password = source["password"];
	        this.passphrase = source["passphrase"];
	    }
	}
	export class TokenUsageStat {
	    input_tokens: number;
	    output_tokens: number;
	    total_tokens: number;
	    cached_input_tokens?: number;
	    cache_write_tokens?: number;
	    input_price_per_m_tokens_rmb?: number;
	    output_price_per_m_tokens_rmb?: number;
	    input_cost_rmb?: number;
	    output_cost_rmb?: number;
	    total_cost_rmb?: number;
	    requests?: number;
	    cached_requests?: number;
	    local_cache_requests?: number;
	    local_cache_hits?: number;
	
	    static createFrom(source: any = {}) {
	        return new TokenUsageStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.total_tokens = source["total_tokens"];
	        this.cached_input_tokens = source["cached_input_tokens"];
	        this.cache_write_tokens = source["cache_write_tokens"];
	        this.input_price_per_m_tokens_rmb = source["input_price_per_m_tokens_rmb"];
	        this.output_price_per_m_tokens_rmb = source["output_price_per_m_tokens_rmb"];
	        this.input_cost_rmb = source["input_cost_rmb"];
	        this.output_cost_rmb = source["output_cost_rmb"];
	        this.total_cost_rmb = source["total_cost_rmb"];
	        this.requests = source["requests"];
	        this.cached_requests = source["cached_requests"];
	        this.local_cache_requests = source["local_cache_requests"];
	        this.local_cache_hits = source["local_cache_hits"];
	    }
	}
	export class CapabilityResourceTypePolicy {
	    allowed_sources?: string[];
	    default_sources?: string[];
	    user_configurable_sources?: string[];
	
	    static createFrom(source: any = {}) {
	        return new CapabilityResourceTypePolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allowed_sources = source["allowed_sources"];
	        this.default_sources = source["default_sources"];
	        this.user_configurable_sources = source["user_configurable_sources"];
	    }
	}
	export class CapabilitySourceUpdatePolicy {
	    default?: string;
	    free_capability?: string;
	    paid_capability?: string;
	    license_or_price_changed?: string;
	    apply_to?: string[];
	    options?: string[];
	
	    static createFrom(source: any = {}) {
	        return new CapabilitySourceUpdatePolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default = source["default"];
	        this.free_capability = source["free_capability"];
	        this.paid_capability = source["paid_capability"];
	        this.license_or_price_changed = source["license_or_price_changed"];
	        this.apply_to = source["apply_to"];
	        this.options = source["options"];
	    }
	}
	export class CapabilityUpdatePolicy {
	    enterprise_hub?: CapabilitySourceUpdatePolicy;
	    hubcenter?: CapabilitySourceUpdatePolicy;
	
	    static createFrom(source: any = {}) {
	        return new CapabilityUpdatePolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enterprise_hub = this.convertValues(source["enterprise_hub"], CapabilitySourceUpdatePolicy);
	        this.hubcenter = this.convertValues(source["hubcenter"], CapabilitySourceUpdatePolicy);
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
	export class CapabilityRecommendedCapabilityPolicy {
	    enabled?: boolean;
	    allow_user_dismiss?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CapabilityRecommendedCapabilityPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.allow_user_dismiss = source["allow_user_dismiss"];
	    }
	}
	export class CapabilityManagedDeploymentPolicy {
	    enabled?: boolean;
	    retry_interval_minutes?: number;
	    reinstall_if_removed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CapabilityManagedDeploymentPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.retry_interval_minutes = source["retry_interval_minutes"];
	        this.reinstall_if_removed = source["reinstall_if_removed"];
	    }
	}
	export class CapabilityMarketPolicy {
	    view_mode?: string;
	    preferred_upload_target?: string;
	    enterprise_only_install?: boolean;
	    enterprise_only_search?: boolean;
	    managed_deployment?: CapabilityManagedDeploymentPolicy;
	    recommended_capability?: CapabilityRecommendedCapabilityPolicy;
	    update_policy?: CapabilityUpdatePolicy;
	    source_priority?: Record<string, number>;
	    resource_types?: Record<string, CapabilityResourceTypePolicy>;
	
	    static createFrom(source: any = {}) {
	        return new CapabilityMarketPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.view_mode = source["view_mode"];
	        this.preferred_upload_target = source["preferred_upload_target"];
	        this.enterprise_only_install = source["enterprise_only_install"];
	        this.enterprise_only_search = source["enterprise_only_search"];
	        this.managed_deployment = this.convertValues(source["managed_deployment"], CapabilityManagedDeploymentPolicy);
	        this.recommended_capability = this.convertValues(source["recommended_capability"], CapabilityRecommendedCapabilityPolicy);
	        this.update_policy = this.convertValues(source["update_policy"], CapabilityUpdatePolicy);
	        this.source_priority = source["source_priority"];
	        this.resource_types = this.convertValues(source["resource_types"], CapabilityResourceTypePolicy, true);
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
	export class SkillHubEntry {
	    label: string;
	    url: string;
	    type?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillHubEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.url = source["url"];
	        this.type = source["type"];
	    }
	}
	export class SkillPipelineStep {
	    skill: string;
	    params?: Record<string, string>;
	    checkpoint?: boolean;
	    checkpoint_message?: string;
	    continue_on_fail?: boolean;
	    time_impact_on_reject?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillPipelineStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skill = source["skill"];
	        this.params = source["params"];
	        this.checkpoint = source["checkpoint"];
	        this.checkpoint_message = source["checkpoint_message"];
	        this.continue_on_fail = source["continue_on_fail"];
	        this.time_impact_on_reject = source["time_impact_on_reject"];
	    }
	}
	export class SkillReference {
	    filename: string;
	    description?: string;
	    token_count?: number;
	
	    static createFrom(source: any = {}) {
	        return new SkillReference(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filename = source["filename"];
	        this.description = source["description"];
	        this.token_count = source["token_count"];
	    }
	}
	export class SolidificationCandidate {
	    step_index: number;
	    script_path: string;
	    language: string;
	    param_slots?: string[];
	    success_count: number;
	    signature?: string;
	    last_used?: string;
	
	    static createFrom(source: any = {}) {
	        return new SolidificationCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.step_index = source["step_index"];
	        this.script_path = source["script_path"];
	        this.language = source["language"];
	        this.param_slots = source["param_slots"];
	        this.success_count = source["success_count"];
	        this.signature = source["signature"];
	        this.last_used = source["last_used"];
	    }
	}
	export class NLSkillParam {
	    name: string;
	    description?: string;
	    type?: string;
	    aliases?: string[];
	    cli_flag?: string;
	    default?: string;
	    required?: boolean;
	    synthetic?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NLSkillParam(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.aliases = source["aliases"];
	        this.cli_flag = source["cli_flag"];
	        this.default = source["default"];
	        this.required = source["required"];
	        this.synthetic = source["synthetic"];
	    }
	}
	export class SkillRepairRecord {
	    timestamp: string;
	    error_class?: string;
	    explanation: string;
	    success: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillRepairRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.error_class = source["error_class"];
	        this.explanation = source["explanation"];
	        this.success = source["success"];
	    }
	}
	export class NLSkillOperation {
	    name: string;
	    description: string;
	    params?: string[];
	    labels?: string[];
	
	    static createFrom(source: any = {}) {
	        return new NLSkillOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.params = source["params"];
	        this.labels = source["labels"];
	    }
	}
	export class SkillCapabilityRef {
	    capability_id: string;
	    version_key?: string;
	    source?: string;
	    global_key?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillCapabilityRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.capability_id = source["capability_id"];
	        this.version_key = source["version_key"];
	        this.source = source["source"];
	        this.global_key = source["global_key"];
	    }
	}
	export class StepLoopConfig {
	    max_iterations: number;
	    until_step?: string;
	    until_match?: string;
	    on_fail_step?: string;
	
	    static createFrom(source: any = {}) {
	        return new StepLoopConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_iterations = source["max_iterations"];
	        this.until_step = source["until_step"];
	        this.until_match = source["until_match"];
	        this.on_fail_step = source["on_fail_step"];
	    }
	}
	export class StepPollConfig {
	    interval: number;
	    max_attempts: number;
	    until_match?: string;
	    until_status?: string;
	
	    static createFrom(source: any = {}) {
	        return new StepPollConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.interval = source["interval"];
	        this.max_attempts = source["max_attempts"];
	        this.until_match = source["until_match"];
	        this.until_status = source["until_status"];
	    }
	}
	export class NLSkillStep {
	    action: string;
	    params: Record<string, any>;
	    on_error: string;
	    name?: string;
	    condition?: string;
	    when?: string;
	    label?: string;
	    capture?: Record<string, string>;
	    poll?: StepPollConfig;
	    loop?: StepLoopConfig;
	    fallback_step?: NLSkillStep;
	
	    static createFrom(source: any = {}) {
	        return new NLSkillStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.params = source["params"];
	        this.on_error = source["on_error"];
	        this.name = source["name"];
	        this.condition = source["condition"];
	        this.when = source["when"];
	        this.label = source["label"];
	        this.capture = source["capture"];
	        this.poll = this.convertValues(source["poll"], StepPollConfig);
	        this.loop = this.convertValues(source["loop"], StepLoopConfig);
	        this.fallback_step = this.convertValues(source["fallback_step"], NLSkillStep);
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
	export class NLSkillEntry {
	    skill_id?: string;
	    name: string;
	    dir_name?: string;
	    description: string;
	    triggers: string[];
	    steps: NLSkillStep[];
	    status: string;
	    created_at: string;
	    source: string;
	    source_project: string;
	    hub_skill_id?: string;
	    hub_version?: string;
	    version?: string;
	    capability?: SkillCapabilityRef;
	    trust_level?: string;
	    type?: string;
	    content?: string;
	    platforms?: string[];
	    requires_gui?: boolean;
	    capabilities?: string[];
	    requires_tools?: string[];
	    fallback_for_tools?: string[];
	    requires_toolsets?: string[];
	    fallback_for_toolsets?: string[];
	    required_credential_files?: string[];
	    requires_python?: string[];
	    requires_node?: string[];
	    requires_bins?: string[];
	    publisher?: string;
	    skill_dir?: string;
	    mode?: string;
	    exec_mode?: string;
	    global_timeout?: number;
	    produces_artifact: boolean;
	    operations?: NLSkillOperation[];
	    required_args?: string[];
	    required_env?: string[];
	    preferred_shell?: string;
	    usage_count: number;
	    success_count: number;
	    failure_count: number;
	    workaround_count: number;
	    last_used_at?: string;
	    last_error?: string;
	    repair_attempt_count?: number;
	    last_repair_at?: string;
	    repair_history?: SkillRepairRecord[];
	    optimization_count?: number;
	    last_optimized_at?: string;
	    discovered_from?: string;
	    total_tokens_cost?: number;
	    params?: NLSkillParam[];
	    solidification_candidates?: SolidificationCandidate[];
	    stateful?: boolean;
	    references?: SkillReference[];
	    pipeline?: SkillPipelineStep[];
	
	    static createFrom(source: any = {}) {
	        return new NLSkillEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skill_id = source["skill_id"];
	        this.name = source["name"];
	        this.dir_name = source["dir_name"];
	        this.description = source["description"];
	        this.triggers = source["triggers"];
	        this.steps = this.convertValues(source["steps"], NLSkillStep);
	        this.status = source["status"];
	        this.created_at = source["created_at"];
	        this.source = source["source"];
	        this.source_project = source["source_project"];
	        this.hub_skill_id = source["hub_skill_id"];
	        this.hub_version = source["hub_version"];
	        this.version = source["version"];
	        this.capability = this.convertValues(source["capability"], SkillCapabilityRef);
	        this.trust_level = source["trust_level"];
	        this.type = source["type"];
	        this.content = source["content"];
	        this.platforms = source["platforms"];
	        this.requires_gui = source["requires_gui"];
	        this.capabilities = source["capabilities"];
	        this.requires_tools = source["requires_tools"];
	        this.fallback_for_tools = source["fallback_for_tools"];
	        this.requires_toolsets = source["requires_toolsets"];
	        this.fallback_for_toolsets = source["fallback_for_toolsets"];
	        this.required_credential_files = source["required_credential_files"];
	        this.requires_python = source["requires_python"];
	        this.requires_node = source["requires_node"];
	        this.requires_bins = source["requires_bins"];
	        this.publisher = source["publisher"];
	        this.skill_dir = source["skill_dir"];
	        this.mode = source["mode"];
	        this.exec_mode = source["exec_mode"];
	        this.global_timeout = source["global_timeout"];
	        this.produces_artifact = source["produces_artifact"];
	        this.operations = this.convertValues(source["operations"], NLSkillOperation);
	        this.required_args = source["required_args"];
	        this.required_env = source["required_env"];
	        this.preferred_shell = source["preferred_shell"];
	        this.usage_count = source["usage_count"];
	        this.success_count = source["success_count"];
	        this.failure_count = source["failure_count"];
	        this.workaround_count = source["workaround_count"];
	        this.last_used_at = source["last_used_at"];
	        this.last_error = source["last_error"];
	        this.repair_attempt_count = source["repair_attempt_count"];
	        this.last_repair_at = source["last_repair_at"];
	        this.repair_history = this.convertValues(source["repair_history"], SkillRepairRecord);
	        this.optimization_count = source["optimization_count"];
	        this.last_optimized_at = source["last_optimized_at"];
	        this.discovered_from = source["discovered_from"];
	        this.total_tokens_cost = source["total_tokens_cost"];
	        this.params = this.convertValues(source["params"], NLSkillParam);
	        this.solidification_candidates = this.convertValues(source["solidification_candidates"], SolidificationCandidate);
	        this.stateful = source["stateful"];
	        this.references = this.convertValues(source["references"], SkillReference);
	        this.pipeline = this.convertValues(source["pipeline"], SkillPipelineStep);
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
	export class LocalMCPServerEntry {
	    id: string;
	    name: string;
	    command: string;
	    args?: string[];
	    env?: Record<string, string>;
	    disabled?: boolean;
	    auto_start?: boolean;
	    created_at: string;
	    source?: string;
	    capability?: MCPServerCapabilityRef;
	
	    static createFrom(source: any = {}) {
	        return new LocalMCPServerEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.disabled = source["disabled"];
	        this.auto_start = source["auto_start"];
	        this.created_at = source["created_at"];
	        this.source = source["source"];
	        this.capability = this.convertValues(source["capability"], MCPServerCapabilityRef);
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
	export class MCPServerCapabilityRef {
	    capability_id: string;
	    version_key?: string;
	    source?: string;
	    global_key?: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerCapabilityRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.capability_id = source["capability_id"];
	        this.version_key = source["version_key"];
	        this.source = source["source"];
	        this.global_key = source["global_key"];
	    }
	}
	export class MCPServerEntry {
	    id: string;
	    name: string;
	    endpoint_url: string;
	    auth_type: string;
	    auth_secret: string;
	    headers?: Record<string, string>;
	    created_at: string;
	    source: string;
	    capability?: MCPServerCapabilityRef;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.endpoint_url = source["endpoint_url"];
	        this.auth_type = source["auth_type"];
	        this.auth_secret = source["auth_secret"];
	        this.headers = source["headers"];
	        this.created_at = source["created_at"];
	        this.source = source["source"];
	        this.capability = this.convertValues(source["capability"], MCPServerCapabilityRef);
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
	export class GroupDiscussionConfig {
	    enabled: boolean;
	    discoverable: boolean;
	    availability?: string;
	    suggest_consultation: boolean;
	    confirm_before_start: boolean;
	    display_name?: string;
	    security_group_id?: string;
	    skills?: string[];
	    description?: string;
	    model_visibility?: string;
	    languages?: string[];
	    invite_policy?: string;
	    allow_security_group_free_discussion: boolean;
	    use_cross_agent_experience?: boolean;
	    allowed_roles?: string[];
	    max_risk_level?: string;
	    context_policy?: string;
	    reject_when_dnd: boolean;
	    max_rounds?: number;
	    timeout_seconds?: number;
	    concurrent_limit?: number;
	    contribution_score?: number;
	    contribution_evidence?: number;
	    sensitive_query_policy?: string;
	    auth_request_sound_preset?: string;
	    auth_request_sound_muted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.discoverable = source["discoverable"];
	        this.availability = source["availability"];
	        this.suggest_consultation = source["suggest_consultation"];
	        this.confirm_before_start = source["confirm_before_start"];
	        this.display_name = source["display_name"];
	        this.security_group_id = source["security_group_id"];
	        this.skills = source["skills"];
	        this.description = source["description"];
	        this.model_visibility = source["model_visibility"];
	        this.languages = source["languages"];
	        this.invite_policy = source["invite_policy"];
	        this.allow_security_group_free_discussion = source["allow_security_group_free_discussion"];
	        this.use_cross_agent_experience = source["use_cross_agent_experience"];
	        this.allowed_roles = source["allowed_roles"];
	        this.max_risk_level = source["max_risk_level"];
	        this.context_policy = source["context_policy"];
	        this.reject_when_dnd = source["reject_when_dnd"];
	        this.max_rounds = source["max_rounds"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.concurrent_limit = source["concurrent_limit"];
	        this.contribution_score = source["contribution_score"];
	        this.contribution_evidence = source["contribution_evidence"];
	        this.sensitive_query_policy = source["sensitive_query_policy"];
	        this.auth_request_sound_preset = source["auth_request_sound_preset"];
	        this.auth_request_sound_muted = source["auth_request_sound_muted"];
	    }
	}
	export class MISDataConfig {
	    enabled: boolean;
	    endpoint?: string;
	    token?: string;
	    tenant_id?: string;
	    user_id?: string;
	    role?: string;
	
	    static createFrom(source: any = {}) {
	        return new MISDataConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.endpoint = source["endpoint"];
	        this.token = source["token"];
	        this.tenant_id = source["tenant_id"];
	        this.user_id = source["user_id"];
	        this.role = source["role"];
	    }
	}
	export class WebSearchEngineConfig {
	    id: string;
	    enabled: boolean;
	    priority: number;
	    transport: string;
	    api_key?: string;
	    base_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new WebSearchEngineConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.enabled = source["enabled"];
	        this.priority = source["priority"];
	        this.transport = source["transport"];
	        this.api_key = source["api_key"];
	        this.base_url = source["base_url"];
	    }
	}
	export class WebSearchStrategy {
	    version: number;
	    preset: string;
	    mode: string;
	    engines: WebSearchEngineConfig[];
	    browser_fallback_enabled: boolean;
	    browser_fallback_engine_id: string;
	    browser_human_assist_enabled?: boolean;
	    hedging_delay_ms?: number;
	    min_results_before_hedge?: number;
	
	    static createFrom(source: any = {}) {
	        return new WebSearchStrategy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.preset = source["preset"];
	        this.mode = source["mode"];
	        this.engines = this.convertValues(source["engines"], WebSearchEngineConfig);
	        this.browser_fallback_enabled = source["browser_fallback_enabled"];
	        this.browser_fallback_engine_id = source["browser_fallback_engine_id"];
	        this.browser_human_assist_enabled = source["browser_human_assist_enabled"];
	        this.hedging_delay_ms = source["hedging_delay_ms"];
	        this.min_results_before_hedge = source["min_results_before_hedge"];
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
	export class WebSearchProvider {
	    name: string;
	    type: string;
	    key?: string;
	    base_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new WebSearchProvider(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.key = source["key"];
	        this.base_url = source["base_url"];
	    }
	}
	export class ToolCacheMaintenanceConfig {
	    enabled: boolean;
	    max_bytes?: number;
	    min_interval_hours?: number;
	    clean_on_startup: boolean;
	    clean_on_exit: boolean;
	    last_cleanup_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolCacheMaintenanceConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.max_bytes = source["max_bytes"];
	        this.min_interval_hours = source["min_interval_hours"];
	        this.clean_on_startup = source["clean_on_startup"];
	        this.clean_on_exit = source["clean_on_exit"];
	        this.last_cleanup_at = source["last_cleanup_at"];
	    }
	}
	export class LLMPromptCacheConfig {
	    enabled: boolean;
	    openai_enabled?: boolean;
	    anthropic_enabled?: boolean;
	    stream_synthesis_enabled?: boolean;
	    cache_dir?: string;
	    ttl_seconds?: number;
	    memory_max_entries?: number;
	    memory_max_bytes?: number;
	    disk_max_bytes?: number;
	    normalize_deterministic_params?: boolean;
	    ignore_model_field?: boolean;
	    ignore_user_field?: boolean;
	    ignore_metadata_field?: boolean;
	    singleflight_wait_timeout_ms?: number;
	
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
	export class MaclawLLMProvider {
	    name: string;
	    url: string;
	    key: string;
	    model: string;
	    protocol?: string;
	    context_length?: number;
	    timeout_sec?: number;
	    max_output_tokens?: number;
	    models?: string[];
	    is_custom?: boolean;
	    is_hub_service?: boolean;
	    supports_vision: boolean;
	    agent_type?: string;
	    auth_type?: string;
	    refresh_token?: string;
	    token_expires_at?: number;
	    oauth_access_token?: string;
	    wire_api?: string;
	    input_price_per_m_tokens_rmb?: number;
	    output_price_per_m_tokens_rmb?: number;
	
	    static createFrom(source: any = {}) {
	        return new MaclawLLMProvider(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	        this.key = source["key"];
	        this.model = source["model"];
	        this.protocol = source["protocol"];
	        this.context_length = source["context_length"];
	        this.timeout_sec = source["timeout_sec"];
	        this.max_output_tokens = source["max_output_tokens"];
	        this.models = source["models"];
	        this.is_custom = source["is_custom"];
	        this.is_hub_service = source["is_hub_service"];
	        this.supports_vision = source["supports_vision"];
	        this.agent_type = source["agent_type"];
	        this.auth_type = source["auth_type"];
	        this.refresh_token = source["refresh_token"];
	        this.token_expires_at = source["token_expires_at"];
	        this.oauth_access_token = source["oauth_access_token"];
	        this.wire_api = source["wire_api"];
	        this.input_price_per_m_tokens_rmb = source["input_price_per_m_tokens_rmb"];
	        this.output_price_per_m_tokens_rmb = source["output_price_per_m_tokens_rmb"];
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
	    agent_type?: string;
	    is_custom: boolean;
	    is_builtin: boolean;
	    has_subscription: boolean;
	
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
	        this.is_builtin = source["is_builtin"];
	        this.has_subscription = source["has_subscription"];
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
	    codex: ToolConfig;
	    opencode: ToolConfig;
	    codebuddy: ToolConfig;
	    iflow: ToolConfig;
	    kilo: ToolConfig;
	    projects: ProjectConfig[];
	    current_project: string;
	    active_tool: string;
	    default_tool: string;
	    default_tool_provider: string;
	    hide_startup_popup: boolean;
	    hide_maclaw_llm_popup: boolean;
	    show_codex: boolean;
	    show_opencode: boolean;
	    show_codebuddy: boolean;
	    show_iflow: boolean;
	    show_kilo: boolean;
	    language: string;
	    power_optimization: boolean;
	    screen_dim_timeout_min: number;
	    workstation_mode: boolean;
	    check_update_on_startup: boolean;
	    prefer_beta_channel?: boolean;
	    pause_env_check: boolean;
	    env_check_done: boolean;
	    env_check_interval: number;
	    last_env_check_time: string;
	    default_proxy_enabled: boolean;
	    default_proxy_protocol?: string;
	    default_proxy_host: string;
	    default_proxy_port: string;
	    default_proxy_username: string;
	    default_proxy_password: string;
	    default_proxy_bypass?: string;
	    default_proxy_scope_maclaw?: boolean;
	    default_proxy_scope_coding_tools?: boolean;
	    default_proxy_scope_agent?: boolean;
	    use_windows_terminal: boolean;
	    remote_enabled: boolean;
	    remote_hub_id?: string;
	    remote_hub_url: string;
	    remote_hubcenter_url: string;
	    remote_hubcenter_urls?: string[];
	    remote_email: string;
	    remote_mobile: string;
	    remote_sn: string;
	    remote_user_id: string;
	    remote_tenant_id?: string;
	    remote_tenant_name?: string;
	    remote_machine_id: string;
	    remote_machine_name?: string;
	    remote_machine_token: string;
	    remote_viewer_token?: string;
	    skill_market_session_token?: string;
	    remote_heartbeat_sec: number;
	    remote_nickname?: string;
	    remote_client_id: string;
	    default_launch_mode: string;
	    maclaw_llm_url: string;
	    maclaw_llm_key: string;
	    maclaw_llm_model: string;
	    maclaw_llm_protocol?: string;
	    maclaw_llm_context_length?: number;
	    maclaw_llm_timeout_sec?: number;
	    agent_response_timeout_sec?: number;
	    skill_runner_timeout_sec?: number;
	    maclaw_llm_providers?: MaclawLLMProvider[];
	    maclaw_llm_current_provider?: string;
	    llm_prompt_cache?: LLMPromptCacheConfig;
	    tool_cache_maintenance?: ToolCacheMaintenanceConfig;
	    web_search_providers?: WebSearchProvider[];
	    web_search_current_provider?: string;
	    web_search_strategy?: WebSearchStrategy;
	    maclaw_agent_max_iterations?: number;
	    maclaw_llm_thinking_mode?: string;
	    subagent_concurrency?: number;
	    subagent_full_access?: boolean;
	    maclaw_role_name?: string;
	    maclaw_role_description?: string;
	    mis_data?: MISDataConfig;
	    group_discussion?: GroupDiscussionConfig;
	    mcp_servers?: MCPServerEntry[];
	    local_mcp_servers?: LocalMCPServerEntry[];
	    nl_skills?: NLSkillEntry[];
	    skill_hub_urls?: SkillHubEntry[];
	    external_skill_dirs?: string[];
	    skill_evolution_repair_cooldown_hours?: number;
	    skill_evolution_enabled?: boolean;
	    skill_auto_upload_enabled?: boolean;
	    skill_auto_upload_min_successes?: number;
	    memory_auto_compress: boolean;
	    memory_max_backups: number;
	    security_policy_mode?: string;
	    hub_security_centralized?: boolean;
	    sandbox_mode?: string;
	    network_level?: string;
	    network_allowlist?: string[];
	    yolo_mode_allowed: boolean;
	    computer_use_enabled?: boolean;
	    computer_use_log_keep_newest?: number;
	    computer_use_log_max_age_days?: number;
	    computer_use_log_auto_prune?: boolean;
	    smart_route_enabled: boolean;
	    gossip_enabled: boolean;
	    file_outbound_enabled: boolean;
	    image_outbound_enabled: boolean;
	    skill_sources_allowed?: string[];
	    trusted_skill_package_key_fingerprints?: string[];
	    capability_market_policy?: CapabilityMarketPolicy;
	    maclaw_debug_tool_calls?: boolean;
	    show_ai_trace_entry?: boolean;
	    show_app_entry?: boolean;
	    show_workflow_entry?: boolean;
	    show_utilities_entry?: boolean;
	    survey_enabled?: boolean;
	    show_assistant_entry: boolean;
	    show_hub_ranking?: boolean;
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
	    pet_ambient_city?: string;
	    floating_btn_x?: number;
	    floating_btn_y?: number;
	    floating_btn_position_set?: boolean;
	    log_detail_enabled?: boolean;
	    qqbot_enabled?: boolean;
	    qqbot_app_id?: string;
	    qqbot_app_secret?: string;
	    qqbot_owner_openid?: string;
	    telegram_bot_enabled?: boolean;
	    telegram_bot_token?: string;
	    telegram_owner_chat_id?: string;
	    weixin_enabled?: boolean;
	    weixin_token?: string;
	    weixin_base_url?: string;
	    weixin_cdn_url?: string;
	    weixin_account_id?: string;
	    weixin_local_mode?: boolean;
	    lansenger_enabled?: boolean;
	    lansenger_app_id?: string;
	    lansenger_app_secret?: string;
	    lansenger_gateway_url?: string;
	    lansenger_wss_url?: string;
	    lansenger_ignored_group_ids?: string[];
	    lansenger_group_policy?: string;
	    lansenger_allowed_group_ids?: string[];
	    lansenger_group_file_max_bytes?: {[key: string]: number};
	    lansenger_require_mention?: boolean;
	    lansenger_respond_to_at_all?: boolean;
	    lansenger_auto_mention_reply?: boolean;
	    lansenger_auto_quote_reply?: boolean;
	    lansenger_group_knowledge_source_ids?: string[];
	    lansenger_group_allow_web_search?: boolean;
	    lansenger_group_allow_all_directories?: boolean;
	    lansenger_group_allowed_directories?: string[];
	    qqbot_local_mode?: boolean;
	    telegram_local_mode?: boolean;
	    lansenger_local_mode?: boolean;
	    thirdparty_gateway_enabled?: boolean;
	    thirdparty_gateway_token?: string;
	    thirdparty_gateway_host?: string;
	    thirdparty_gateway_port?: number;
	    thirdparty_gateway_local_mode?: boolean;
	    hardware_welcome_enabled?: boolean;
	    hardware_welcome_text?: string;
	    hardware_welcome_audio_path?: string;
	    hardware_volume?: number;
	    acp_host_enabled?: boolean;
	    acp_host_port?: number;
	    acp_host_mirror_ui?: boolean;
	    im_progress_nudge_enabled?: boolean;
	    extra_tool_configs?: Record<string, ToolConfig>;
	    ui_mode?: string;
	    skill_purchase_mode?: string;
	    gossip_auto_publish: boolean;
	    llm_trajectory_logging?: boolean;
	    bug_report_enabled?: boolean;
	    bug_report_previous_trajectory?: boolean;
	    bug_report_previous_log_detail?: boolean;
	    memory_recall_log_enabled?: boolean;
	    knowledge_auto_recall_enabled?: boolean;
	    knowledge_auto_recall_min_score?: number;
	    trial_reflect_enabled?: boolean;
	    local_needle_enabled?: boolean;
	    local_needle_log_enabled?: boolean;
	    local_needle_training_export_enabled?: boolean;
	    local_needle_model_path?: string;
	    local_needle_min_confidence?: number;
	    llm_token_usage?: Record<string, TokenUsageStat>;
	    onboarding_done: boolean;
	    vector_search_enabled: boolean;
	    asr_enabled: boolean;
	    diarization_enabled: boolean;
	    asr_voice_correction_enabled: boolean;
	    noise_floor_calibrated?: number;
	    speech_level_calibrated?: number;
	    tts_enabled: boolean;
	    tts_voice_id?: string;
	    tts_auto_voice_summary?: boolean;
	    audio_input_device_id?: string;
	    audio_output_device_id?: string;
	    screen_parsing_enabled?: boolean;
	    ui_zoom_factor?: number;
	    chat_font_size?: number;
	    ssh_hosts?: SSHHostEntry[];
	    knowledge_skill_token_budget: number;
	    knowledge_vision_llm?: KnowledgeVisionLLMConfig;
	    knowledge_include_images?: boolean;
	    auxiliary_llm?: AuxiliaryLLMConfig;
	    model_routes?: Record<string, ModelRouteConfig>;
	    coding_route_pref?: string;
	    coding_checkpoint_sidecar_max_mb?: number;
	    coding_route_pref_mirror?: boolean;
	    moa?: MoAConfig;
	    shared_agent_loop_enabled: boolean;
	    shared_agent_loop_migrated: boolean;
	    shared_agent_loop_canary_percent?: number;
	    shared_agent_loop_workflow?: boolean;
	    daily_llm_budget_usd?: number;
	    auto_fetch_enabled?: boolean;
	    auto_fetch_interval_min?: number;
	    auto_fetch_rss_feeds?: string[];
	    auto_fetch_watch_dirs?: string[];
	    nudge_disabled?: boolean;
	    working_directory?: string;
	    data_dir?: string;
	    workflow_enabled?: boolean;
	    coding_knowledge_auto_save_mode?: string;
	    coding_knowledge_save_strategy?: string;
	    coding_knowledge_max_per_project?: number;
	    coding_knowledge_max_total?: number;
	    favorite_employees?: string[];
	    favorite_employee_names?: Record<string, string>;
	    show_coding_tool_entry?: boolean;
	    ve_allowed_directories?: string[];
	    ve_approval_config?: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.claude = this.convertValues(source["claude"], ToolConfig);
	        this.codex = this.convertValues(source["codex"], ToolConfig);
	        this.opencode = this.convertValues(source["opencode"], ToolConfig);
	        this.codebuddy = this.convertValues(source["codebuddy"], ToolConfig);
	        this.iflow = this.convertValues(source["iflow"], ToolConfig);
	        this.kilo = this.convertValues(source["kilo"], ToolConfig);
	        this.projects = this.convertValues(source["projects"], ProjectConfig);
	        this.current_project = source["current_project"];
	        this.active_tool = source["active_tool"];
	        this.default_tool = source["default_tool"];
	        this.default_tool_provider = source["default_tool_provider"];
	        this.hide_startup_popup = source["hide_startup_popup"];
	        this.hide_maclaw_llm_popup = source["hide_maclaw_llm_popup"];
	        this.show_codex = source["show_codex"];
	        this.show_opencode = source["show_opencode"];
	        this.show_codebuddy = source["show_codebuddy"];
	        this.show_iflow = source["show_iflow"];
	        this.show_kilo = source["show_kilo"];
	        this.language = source["language"];
	        this.power_optimization = source["power_optimization"];
	        this.screen_dim_timeout_min = source["screen_dim_timeout_min"];
	        this.workstation_mode = source["workstation_mode"];
	        this.check_update_on_startup = source["check_update_on_startup"];
	        this.prefer_beta_channel = source["prefer_beta_channel"];
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
	        this.remote_hubcenter_urls = source["remote_hubcenter_urls"];
	        this.remote_email = source["remote_email"];
	        this.remote_mobile = source["remote_mobile"];
	        this.remote_sn = source["remote_sn"];
	        this.remote_user_id = source["remote_user_id"];
	        this.remote_tenant_id = source["remote_tenant_id"];
	        this.remote_tenant_name = source["remote_tenant_name"];
	        this.remote_machine_id = source["remote_machine_id"];
	        this.remote_machine_name = source["remote_machine_name"];
	        this.remote_machine_token = source["remote_machine_token"];
	        this.remote_viewer_token = source["remote_viewer_token"];
	        this.skill_market_session_token = source["skill_market_session_token"];
	        this.remote_heartbeat_sec = source["remote_heartbeat_sec"];
	        this.remote_nickname = source["remote_nickname"];
	        this.remote_client_id = source["remote_client_id"];
	        this.default_launch_mode = source["default_launch_mode"];
	        this.maclaw_llm_url = source["maclaw_llm_url"];
	        this.maclaw_llm_key = source["maclaw_llm_key"];
	        this.maclaw_llm_model = source["maclaw_llm_model"];
	        this.maclaw_llm_protocol = source["maclaw_llm_protocol"];
	        this.maclaw_llm_context_length = source["maclaw_llm_context_length"];
	        this.maclaw_llm_timeout_sec = source["maclaw_llm_timeout_sec"];
	        this.agent_response_timeout_sec = source["agent_response_timeout_sec"];
	        this.skill_runner_timeout_sec = source["skill_runner_timeout_sec"];
	        this.maclaw_llm_providers = this.convertValues(source["maclaw_llm_providers"], MaclawLLMProvider);
	        this.maclaw_llm_current_provider = source["maclaw_llm_current_provider"];
	        this.llm_prompt_cache = this.convertValues(source["llm_prompt_cache"], LLMPromptCacheConfig);
	        this.tool_cache_maintenance = this.convertValues(source["tool_cache_maintenance"], ToolCacheMaintenanceConfig);
	        this.web_search_providers = this.convertValues(source["web_search_providers"], WebSearchProvider);
	        this.web_search_current_provider = source["web_search_current_provider"];
	        this.web_search_strategy = this.convertValues(source["web_search_strategy"], WebSearchStrategy);
	        this.maclaw_agent_max_iterations = source["maclaw_agent_max_iterations"];
	        this.maclaw_llm_thinking_mode = source["maclaw_llm_thinking_mode"];
	        this.subagent_concurrency = source["subagent_concurrency"];
	        this.subagent_full_access = source["subagent_full_access"];
	        this.maclaw_role_name = source["maclaw_role_name"];
	        this.maclaw_role_description = source["maclaw_role_description"];
	        this.mis_data = this.convertValues(source["mis_data"], MISDataConfig);
	        this.group_discussion = this.convertValues(source["group_discussion"], GroupDiscussionConfig);
	        this.mcp_servers = this.convertValues(source["mcp_servers"], MCPServerEntry);
	        this.local_mcp_servers = this.convertValues(source["local_mcp_servers"], LocalMCPServerEntry);
	        this.nl_skills = this.convertValues(source["nl_skills"], NLSkillEntry);
	        this.skill_hub_urls = this.convertValues(source["skill_hub_urls"], SkillHubEntry);
	        this.external_skill_dirs = source["external_skill_dirs"];
	        this.skill_evolution_repair_cooldown_hours = source["skill_evolution_repair_cooldown_hours"];
	        this.skill_evolution_enabled = source["skill_evolution_enabled"];
	        this.skill_auto_upload_enabled = source["skill_auto_upload_enabled"];
	        this.skill_auto_upload_min_successes = source["skill_auto_upload_min_successes"];
	        this.memory_auto_compress = source["memory_auto_compress"];
	        this.memory_max_backups = source["memory_max_backups"];
	        this.security_policy_mode = source["security_policy_mode"];
	        this.hub_security_centralized = source["hub_security_centralized"];
	        this.sandbox_mode = source["sandbox_mode"];
	        this.network_level = source["network_level"];
	        this.network_allowlist = source["network_allowlist"];
	        this.yolo_mode_allowed = source["yolo_mode_allowed"];
	        this.computer_use_enabled = source["computer_use_enabled"];
	        this.computer_use_log_keep_newest = source["computer_use_log_keep_newest"];
	        this.computer_use_log_max_age_days = source["computer_use_log_max_age_days"];
	        this.computer_use_log_auto_prune = source["computer_use_log_auto_prune"];
	        this.smart_route_enabled = source["smart_route_enabled"];
	        this.gossip_enabled = source["gossip_enabled"];
	        this.file_outbound_enabled = source["file_outbound_enabled"];
	        this.image_outbound_enabled = source["image_outbound_enabled"];
	        this.skill_sources_allowed = source["skill_sources_allowed"];
	        this.trusted_skill_package_key_fingerprints = source["trusted_skill_package_key_fingerprints"];
	        this.capability_market_policy = this.convertValues(source["capability_market_policy"], CapabilityMarketPolicy);
	        this.maclaw_debug_tool_calls = source["maclaw_debug_tool_calls"];
	        this.show_ai_trace_entry = source["show_ai_trace_entry"];
	        this.show_app_entry = source["show_app_entry"];
	        this.show_workflow_entry = source["show_workflow_entry"];
	        this.show_utilities_entry = source["show_utilities_entry"];
	        this.survey_enabled = source["survey_enabled"];
	        this.show_assistant_entry = source["show_assistant_entry"];
	        this.show_hub_ranking = source["show_hub_ranking"];
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
	        this.pet_ambient_city = source["pet_ambient_city"];
	        this.floating_btn_x = source["floating_btn_x"];
	        this.floating_btn_y = source["floating_btn_y"];
	        this.floating_btn_position_set = source["floating_btn_position_set"];
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
	        this.lansenger_group_file_max_bytes = source["lansenger_group_file_max_bytes"];
	        this.lansenger_require_mention = source["lansenger_require_mention"];
	        this.lansenger_respond_to_at_all = source["lansenger_respond_to_at_all"];
	        this.lansenger_auto_mention_reply = source["lansenger_auto_mention_reply"];
	        this.lansenger_auto_quote_reply = source["lansenger_auto_quote_reply"];
	        this.lansenger_group_knowledge_source_ids = source["lansenger_group_knowledge_source_ids"];
	        this.lansenger_group_allow_web_search = source["lansenger_group_allow_web_search"];
	        this.lansenger_group_allow_all_directories = source["lansenger_group_allow_all_directories"];
	        this.lansenger_group_allowed_directories = source["lansenger_group_allowed_directories"];
	        this.qqbot_local_mode = source["qqbot_local_mode"];
	        this.telegram_local_mode = source["telegram_local_mode"];
	        this.lansenger_local_mode = source["lansenger_local_mode"];
	        this.thirdparty_gateway_enabled = source["thirdparty_gateway_enabled"];
	        this.thirdparty_gateway_token = source["thirdparty_gateway_token"];
	        this.thirdparty_gateway_host = source["thirdparty_gateway_host"];
	        this.thirdparty_gateway_port = source["thirdparty_gateway_port"];
	        this.thirdparty_gateway_local_mode = source["thirdparty_gateway_local_mode"];
	        this.hardware_welcome_enabled = source["hardware_welcome_enabled"];
	        this.hardware_welcome_text = source["hardware_welcome_text"];
	        this.hardware_welcome_audio_path = source["hardware_welcome_audio_path"];
	        this.hardware_volume = source["hardware_volume"];
	        this.acp_host_enabled = source["acp_host_enabled"];
	        this.acp_host_port = source["acp_host_port"];
	        this.acp_host_mirror_ui = source["acp_host_mirror_ui"];
	        this.im_progress_nudge_enabled = source["im_progress_nudge_enabled"];
	        this.extra_tool_configs = this.convertValues(source["extra_tool_configs"], ToolConfig, true);
	        this.ui_mode = source["ui_mode"];
	        this.skill_purchase_mode = source["skill_purchase_mode"];
	        this.gossip_auto_publish = source["gossip_auto_publish"];
	        this.llm_trajectory_logging = source["llm_trajectory_logging"];
	        this.bug_report_enabled = source["bug_report_enabled"];
	        this.bug_report_previous_trajectory = source["bug_report_previous_trajectory"];
	        this.bug_report_previous_log_detail = source["bug_report_previous_log_detail"];
	        this.memory_recall_log_enabled = source["memory_recall_log_enabled"];
	        this.knowledge_auto_recall_enabled = source["knowledge_auto_recall_enabled"];
	        this.knowledge_auto_recall_min_score = source["knowledge_auto_recall_min_score"];
	        this.trial_reflect_enabled = source["trial_reflect_enabled"];
	        this.local_needle_enabled = source["local_needle_enabled"];
	        this.local_needle_log_enabled = source["local_needle_log_enabled"];
	        this.local_needle_training_export_enabled = source["local_needle_training_export_enabled"];
	        this.local_needle_model_path = source["local_needle_model_path"];
	        this.local_needle_min_confidence = source["local_needle_min_confidence"];
	        this.llm_token_usage = this.convertValues(source["llm_token_usage"], TokenUsageStat, true);
	        this.onboarding_done = source["onboarding_done"];
	        this.vector_search_enabled = source["vector_search_enabled"];
	        this.asr_enabled = source["asr_enabled"];
	        this.diarization_enabled = source["diarization_enabled"];
	        this.asr_voice_correction_enabled = source["asr_voice_correction_enabled"];
	        this.noise_floor_calibrated = source["noise_floor_calibrated"];
	        this.speech_level_calibrated = source["speech_level_calibrated"];
	        this.tts_enabled = source["tts_enabled"];
	        this.tts_voice_id = source["tts_voice_id"];
	        this.tts_auto_voice_summary = source["tts_auto_voice_summary"];
	        this.audio_input_device_id = source["audio_input_device_id"];
	        this.audio_output_device_id = source["audio_output_device_id"];
	        this.screen_parsing_enabled = source["screen_parsing_enabled"];
	        this.ui_zoom_factor = source["ui_zoom_factor"];
	        this.chat_font_size = source["chat_font_size"];
	        this.ssh_hosts = this.convertValues(source["ssh_hosts"], SSHHostEntry);
	        this.knowledge_skill_token_budget = source["knowledge_skill_token_budget"];
	        this.knowledge_vision_llm = this.convertValues(source["knowledge_vision_llm"], KnowledgeVisionLLMConfig);
	        this.knowledge_include_images = source["knowledge_include_images"];
	        this.auxiliary_llm = this.convertValues(source["auxiliary_llm"], AuxiliaryLLMConfig);
	        this.model_routes = this.convertValues(source["model_routes"], ModelRouteConfig, true);
	        this.coding_route_pref = source["coding_route_pref"];
	        this.coding_checkpoint_sidecar_max_mb = source["coding_checkpoint_sidecar_max_mb"];
	        this.coding_route_pref_mirror = source["coding_route_pref_mirror"];
	        this.moa = this.convertValues(source["moa"], MoAConfig);
	        this.shared_agent_loop_enabled = source["shared_agent_loop_enabled"];
	        this.shared_agent_loop_migrated = source["shared_agent_loop_migrated"];
	        this.shared_agent_loop_canary_percent = source["shared_agent_loop_canary_percent"];
	        this.shared_agent_loop_workflow = source["shared_agent_loop_workflow"];
	        this.daily_llm_budget_usd = source["daily_llm_budget_usd"];
	        this.auto_fetch_enabled = source["auto_fetch_enabled"];
	        this.auto_fetch_interval_min = source["auto_fetch_interval_min"];
	        this.auto_fetch_rss_feeds = source["auto_fetch_rss_feeds"];
	        this.auto_fetch_watch_dirs = source["auto_fetch_watch_dirs"];
	        this.nudge_disabled = source["nudge_disabled"];
	        this.working_directory = source["working_directory"];
	        this.data_dir = source["data_dir"];
	        this.workflow_enabled = source["workflow_enabled"];
	        this.coding_knowledge_auto_save_mode = source["coding_knowledge_auto_save_mode"];
	        this.coding_knowledge_save_strategy = source["coding_knowledge_save_strategy"];
	        this.coding_knowledge_max_per_project = source["coding_knowledge_max_per_project"];
	        this.coding_knowledge_max_total = source["coding_knowledge_max_total"];
	        this.favorite_employees = source["favorite_employees"];
	        this.favorite_employee_names = source["favorite_employee_names"];
	        this.show_coding_tool_entry = source["show_coding_tool_entry"];
	        this.ve_allowed_directories = source["ve_allowed_directories"];
	        this.ve_approval_config = source["ve_approval_config"];
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
	
	
	
	
	
	
	
	export class DigitalEmployeeAuthorization {
	    quota: number;
	    enabled: boolean;
	    expires_at?: string;
	    active: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new DigitalEmployeeAuthorization(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.quota = source["quota"];
	        this.enabled = source["enabled"];
	        this.expires_at = source["expires_at"];
	        this.active = source["active"];
	        this.reason = source["reason"];
	    }
	}
	
	
	
	
	
	
	
	export class MaclawLLMConfig {
	    url: string;
	    key: string;
	    model: string;
	    protocol?: string;
	    context_length?: number;
	    timeout_sec?: number;
	    max_output_tokens?: number;
	    supports_vision: boolean;
	    agent_type?: string;
	    wire_api?: string;
	    provider_name?: string;
	    auth_type?: string;
	    maclaw_agent_max_iterations?: number;
	    enable_prompt_cache?: boolean;
	    reasoning_effort?: string;
	    thinking_mode?: string;
	
	    static createFrom(source: any = {}) {
	        return new MaclawLLMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.key = source["key"];
	        this.model = source["model"];
	        this.protocol = source["protocol"];
	        this.context_length = source["context_length"];
	        this.timeout_sec = source["timeout_sec"];
	        this.max_output_tokens = source["max_output_tokens"];
	        this.supports_vision = source["supports_vision"];
	        this.agent_type = source["agent_type"];
	        this.wire_api = source["wire_api"];
	        this.provider_name = source["provider_name"];
	        this.auth_type = source["auth_type"];
	        this.maclaw_agent_max_iterations = source["maclaw_agent_max_iterations"];
	        this.enable_prompt_cache = source["enable_prompt_cache"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.thinking_mode = source["thinking_mode"];
	    }
	}
	
	export class MaclawLLMTestResult {
	    message: string;
	    supports_vision: boolean;
	    vision_probe_status?: string;
	
	    static createFrom(source: any = {}) {
	        return new MaclawLLMTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.supports_vision = source["supports_vision"];
	        this.vision_probe_status = source["vision_probe_status"];
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
	
	
	
	
	
	
	
	
	
	
	
	
	

}

export namespace doctor {
	
	export class Check {
	    id: string;
	    status: string;
	    message: string;
	    hint?: string;
	    detail?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new Check(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.hint = source["hint"];
	        this.detail = source["detail"];
	    }
	}
	export class Report {
	    ok: boolean;
	    summary: string;
	    // Go type: time
	    generated_at: any;
	    config_path?: string;
	    base_dir?: string;
	    checks: Check[];
	    blockers: number;
	    warnings: number;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.summary = source["summary"];
	        this.generated_at = this.convertValues(source["generated_at"], null);
	        this.config_path = source["config_path"];
	        this.base_dir = source["base_dir"];
	        this.checks = this.convertValues(source["checks"], Check);
	        this.blockers = source["blockers"];
	        this.warnings = source["warnings"];
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

export namespace experience {
	
	export class EvidenceReport {
	    score: number;
	    reasons?: string[];
	    unsupported_steps?: string[];
	
	    static createFrom(source: any = {}) {
	        return new EvidenceReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.score = source["score"];
	        this.reasons = source["reasons"];
	        this.unsupported_steps = source["unsupported_steps"];
	    }
	}
	export class QualityReport {
	    score: number;
	    reasons?: string[];
	
	    static createFrom(source: any = {}) {
	        return new QualityReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.score = source["score"];
	        this.reasons = source["reasons"];
	    }
	}
	export class Decision {
	    pattern_name: string;
	    action: string;
	    reason?: string;
	    matched_skill_name?: string;
	    quality: QualityReport;
	    evidence: EvidenceReport;
	
	    static createFrom(source: any = {}) {
	        return new Decision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pattern_name = source["pattern_name"];
	        this.action = source["action"];
	        this.reason = source["reason"];
	        this.matched_skill_name = source["matched_skill_name"];
	        this.quality = this.convertValues(source["quality"], QualityReport);
	        this.evidence = this.convertValues(source["evidence"], EvidenceReport);
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
	export class ResultSummary {
	    total_candidates: number;
	    registered: number;
	    updated: number;
	    skipped: number;
	    skip_reasons?: Record<string, number>;
	    unsupported_steps?: Record<string, number>;
	
	    static createFrom(source: any = {}) {
	        return new ResultSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_candidates = source["total_candidates"];
	        this.registered = source["registered"];
	        this.updated = source["updated"];
	        this.skipped = source["skipped"];
	        this.skip_reasons = source["skip_reasons"];
	        this.unsupported_steps = source["unsupported_steps"];
	    }
	}
	export class AuditEntry {
	    timestamp: string;
	    session_id?: string;
	    tool?: string;
	    title?: string;
	    project_path?: string;
	    status?: string;
	    duration_ms?: number;
	    summary: ResultSummary;
	    decisions?: Decision[];
	    upserted?: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuditEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.session_id = source["session_id"];
	        this.tool = source["tool"];
	        this.title = source["title"];
	        this.project_path = source["project_path"];
	        this.status = source["status"];
	        this.duration_ms = source["duration_ms"];
	        this.summary = this.convertValues(source["summary"], ResultSummary);
	        this.decisions = this.convertValues(source["decisions"], Decision);
	        this.upserted = source["upserted"];
	        this.error = source["error"];
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
	export class AuditHealth {
	    runs: number;
	    completed: number;
	    no_candidates: number;
	    failed: number;
	    total_candidates: number;
	    registered: number;
	    updated: number;
	    skipped: number;
	    avg_duration_ms?: number;
	    latest_timestamp?: string;
	    status: string;
	    issue_code?: string;
	    primary_issue?: string;
	    suggested_action?: string;
	    skip_reasons?: Record<string, number>;
	    unsupported_steps?: Record<string, number>;
	
	    static createFrom(source: any = {}) {
	        return new AuditHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runs = source["runs"];
	        this.completed = source["completed"];
	        this.no_candidates = source["no_candidates"];
	        this.failed = source["failed"];
	        this.total_candidates = source["total_candidates"];
	        this.registered = source["registered"];
	        this.updated = source["updated"];
	        this.skipped = source["skipped"];
	        this.avg_duration_ms = source["avg_duration_ms"];
	        this.latest_timestamp = source["latest_timestamp"];
	        this.status = source["status"];
	        this.issue_code = source["issue_code"];
	        this.primary_issue = source["primary_issue"];
	        this.suggested_action = source["suggested_action"];
	        this.skip_reasons = source["skip_reasons"];
	        this.unsupported_steps = source["unsupported_steps"];
	    }
	}
	
	
	

}

export namespace knowledge {
	
	export class Fact {
	    id: string;
	    card_id: string;
	    source_id: string;
	    subject: string;
	    predicate: string;
	    object: string;
	    negated?: boolean;
	    // Go type: time
	    valid_at?: any;
	    // Go type: time
	    invalid_at?: any;
	    confidence?: number;
	
	    static createFrom(source: any = {}) {
	        return new Fact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.card_id = source["card_id"];
	        this.source_id = source["source_id"];
	        this.subject = source["subject"];
	        this.predicate = source["predicate"];
	        this.object = source["object"];
	        this.negated = source["negated"];
	        this.valid_at = this.convertValues(source["valid_at"], null);
	        this.invalid_at = this.convertValues(source["invalid_at"], null);
	        this.confidence = source["confidence"];
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
	export class Card {
	    id: string;
	    source_id: string;
	    node_id?: string;
	    title?: string;
	    claim: string;
	    summary?: string;
	    entities?: string[];
	    topics?: string[];
	    tags?: string[];
	    facts?: Fact[];
	    project_path?: string;
	    owner_id?: string;
	    tenant_id?: string;
	    // Go type: time
	    valid_at?: any;
	    // Go type: time
	    invalid_at?: any;
	    confidence?: number;
	    importance?: number;
	    source_trust?: number;
	    embedding?: number[];
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Card(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.node_id = source["node_id"];
	        this.title = source["title"];
	        this.claim = source["claim"];
	        this.summary = source["summary"];
	        this.entities = source["entities"];
	        this.topics = source["topics"];
	        this.tags = source["tags"];
	        this.facts = this.convertValues(source["facts"], Fact);
	        this.project_path = source["project_path"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.valid_at = this.convertValues(source["valid_at"], null);
	        this.invalid_at = this.convertValues(source["invalid_at"], null);
	        this.confidence = source["confidence"];
	        this.importance = source["importance"];
	        this.source_trust = source["source_trust"];
	        this.embedding = source["embedding"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class CardSuppression {
	    card_id: string;
	    source_id?: string;
	    claim?: string;
	    source_title?: string;
	    relative_path?: string;
	    reason?: string;
	    // Go type: time
	    created_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new CardSuppression(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.card_id = source["card_id"];
	        this.source_id = source["source_id"];
	        this.claim = source["claim"];
	        this.source_title = source["source_title"];
	        this.relative_path = source["relative_path"];
	        this.reason = source["reason"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class CardSuppressionResult {
	    suppressed: number;
	    restored?: number;
	    kept_card_id?: string;
	    card_ids?: string[];
	    items?: CardSuppression[];
	
	    static createFrom(source: any = {}) {
	        return new CardSuppressionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.suppressed = source["suppressed"];
	        this.restored = source["restored"];
	        this.kept_card_id = source["kept_card_id"];
	        this.card_ids = source["card_ids"];
	        this.items = this.convertValues(source["items"], CardSuppression);
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
	export class SourceRefreshFailure {
	    source_id: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceRefreshFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.error = source["error"];
	    }
	}
	export class SourceRefreshResult {
	    requested: number;
	    refreshed: number;
	    failed: number;
	    sources?: Source[];
	    failures?: SourceRefreshFailure[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceRefreshResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.refreshed = source["refreshed"];
	        this.failed = source["failed"];
	        this.sources = this.convertValues(source["sources"], Source);
	        this.failures = this.convertValues(source["failures"], SourceRefreshFailure);
	        this.warnings = source["warnings"];
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
	export class SourceChangePreviewFailure {
	    source_id: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceChangePreviewFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.error = source["error"];
	    }
	}
	export class SourceChangeSample {
	    kind: string;
	    title?: string;
	    snippet?: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceChangeSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.snippet = source["snippet"];
	    }
	}
	export class Source {
	    id: string;
	    kind: string;
	    uri: string;
	    canonical_uri?: string;
	    title?: string;
	    author?: string;
	    site_name?: string;
	    // Go type: time
	    published_at?: any;
	    // Go type: time
	    fetched_at: any;
	    content_hash: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    topic_hint?: string;
	    source_trust?: number;
	    batch_id?: string;
	    relative_path?: string;
	    status: string;
	    error_message?: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    node_count?: number;
	    card_count?: number;
	    fact_count?: number;
	    labels?: string[];
	    save_status?: string;
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.uri = source["uri"];
	        this.canonical_uri = source["canonical_uri"];
	        this.title = source["title"];
	        this.author = source["author"];
	        this.site_name = source["site_name"];
	        this.published_at = this.convertValues(source["published_at"], null);
	        this.fetched_at = this.convertValues(source["fetched_at"], null);
	        this.content_hash = source["content_hash"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.topic_hint = source["topic_hint"];
	        this.source_trust = source["source_trust"];
	        this.batch_id = source["batch_id"];
	        this.relative_path = source["relative_path"];
	        this.status = source["status"];
	        this.error_message = source["error_message"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.node_count = source["node_count"];
	        this.card_count = source["card_count"];
	        this.fact_count = source["fact_count"];
	        this.labels = source["labels"];
	        this.save_status = source["save_status"];
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
	export class SourceChangePreview {
	    source_id: string;
	    source: Source;
	    next_source?: Source;
	    refreshable: boolean;
	    changed: boolean;
	    hash_changed: boolean;
	    requires_refresh: boolean;
	    old_hash?: string;
	    new_hash?: string;
	    old_status?: string;
	    new_status?: string;
	    old_node_count: number;
	    new_node_count: number;
	    added_nodes?: number;
	    removed_nodes?: number;
	    unchanged_nodes?: number;
	    error?: string;
	    samples?: SourceChangeSample[];
	    // Go type: time
	    generated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceChangePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.source = this.convertValues(source["source"], Source);
	        this.next_source = this.convertValues(source["next_source"], Source);
	        this.refreshable = source["refreshable"];
	        this.changed = source["changed"];
	        this.hash_changed = source["hash_changed"];
	        this.requires_refresh = source["requires_refresh"];
	        this.old_hash = source["old_hash"];
	        this.new_hash = source["new_hash"];
	        this.old_status = source["old_status"];
	        this.new_status = source["new_status"];
	        this.old_node_count = source["old_node_count"];
	        this.new_node_count = source["new_node_count"];
	        this.added_nodes = source["added_nodes"];
	        this.removed_nodes = source["removed_nodes"];
	        this.unchanged_nodes = source["unchanged_nodes"];
	        this.error = source["error"];
	        this.samples = this.convertValues(source["samples"], SourceChangeSample);
	        this.generated_at = this.convertValues(source["generated_at"], null);
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
	export class SourceChangePreviewResult {
	    requested: number;
	    changed: number;
	    unchanged: number;
	    failed: number;
	    previews?: SourceChangePreview[];
	    failures?: SourceChangePreviewFailure[];
	
	    static createFrom(source: any = {}) {
	        return new SourceChangePreviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.changed = source["changed"];
	        this.unchanged = source["unchanged"];
	        this.failed = source["failed"];
	        this.previews = this.convertValues(source["previews"], SourceChangePreview);
	        this.failures = this.convertValues(source["failures"], SourceChangePreviewFailure);
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
	export class ChangedSourceRefreshResult {
	    preview: SourceChangePreviewResult;
	    refresh: SourceRefreshResult;
	    source_ids?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ChangedSourceRefreshResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = this.convertValues(source["preview"], SourceChangePreviewResult);
	        this.refresh = this.convertValues(source["refresh"], SourceRefreshResult);
	        this.source_ids = source["source_ids"];
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
	export class Citation {
	    label: string;
	    source_id?: string;
	    source_title?: string;
	    source_kind?: string;
	    uri?: string;
	    relative_path?: string;
	    result_type?: string;
	    node_id?: string;
	    card_id?: string;
	    fact_id?: string;
	    page?: number;
	    sheet_name?: string;
	    row_range?: string;
	    col_range?: string;
	    snippet?: string;
	    score?: number;
	
	    static createFrom(source: any = {}) {
	        return new Citation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.source_id = source["source_id"];
	        this.source_title = source["source_title"];
	        this.source_kind = source["source_kind"];
	        this.uri = source["uri"];
	        this.relative_path = source["relative_path"];
	        this.result_type = source["result_type"];
	        this.node_id = source["node_id"];
	        this.card_id = source["card_id"];
	        this.fact_id = source["fact_id"];
	        this.page = source["page"];
	        this.sheet_name = source["sheet_name"];
	        this.row_range = source["row_range"];
	        this.col_range = source["col_range"];
	        this.snippet = source["snippet"];
	        this.score = source["score"];
	    }
	}
	export class CodingExperience {
	    id: string;
	    title: string;
	    category: string;
	    scope: string;
	    language?: string;
	    frameworks?: string[];
	    trigger_condition: string;
	    content: string;
	    code_snippet?: string;
	    failed_attempts?: string[];
	    contraindications?: string[];
	    labels?: string[];
	    project_path?: string;
	    source_task_title?: string;
	    language_version?: string;
	    valid_until?: string;
	    confidence: number;
	    recall_count: number;
	    success_count: number;
	    failure_count: number;
	    status: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    // Go type: time
	    last_recalled_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new CodingExperience(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.category = source["category"];
	        this.scope = source["scope"];
	        this.language = source["language"];
	        this.frameworks = source["frameworks"];
	        this.trigger_condition = source["trigger_condition"];
	        this.content = source["content"];
	        this.code_snippet = source["code_snippet"];
	        this.failed_attempts = source["failed_attempts"];
	        this.contraindications = source["contraindications"];
	        this.labels = source["labels"];
	        this.project_path = source["project_path"];
	        this.source_task_title = source["source_task_title"];
	        this.language_version = source["language_version"];
	        this.valid_until = source["valid_until"];
	        this.confidence = source["confidence"];
	        this.recall_count = source["recall_count"];
	        this.success_count = source["success_count"];
	        this.failure_count = source["failure_count"];
	        this.status = source["status"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.last_recalled_at = this.convertValues(source["last_recalled_at"], null);
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
	export class CodingKnowledgeStats {
	    total_count: number;
	    active_count: number;
	    verified_count: number;
	    candidate_count: number;
	    deprecated_count: number;
	    by_project: Record<string, number>;
	    by_category: Record<string, number>;
	    by_language: Record<string, number>;
	    avg_confidence: number;
	
	    static createFrom(source: any = {}) {
	        return new CodingKnowledgeStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_count = source["total_count"];
	        this.active_count = source["active_count"];
	        this.verified_count = source["verified_count"];
	        this.candidate_count = source["candidate_count"];
	        this.deprecated_count = source["deprecated_count"];
	        this.by_project = source["by_project"];
	        this.by_category = source["by_category"];
	        this.by_language = source["by_language"];
	        this.avg_confidence = source["avg_confidence"];
	    }
	}
	export class CodingListFilter {
	    scope?: string;
	    language?: string;
	    category?: string;
	    status?: string;
	    project_path?: string;
	    labels?: string[];
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new CodingListFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.language = source["language"];
	        this.category = source["category"];
	        this.status = source["status"];
	        this.project_path = source["project_path"];
	        this.labels = source["labels"];
	        this.limit = source["limit"];
	    }
	}
	export class ContextPackItem {
	    label: string;
	    result_type?: string;
	    title?: string;
	    text?: string;
	    source_id?: string;
	    citation?: string;
	    score?: number;
	
	    static createFrom(source: any = {}) {
	        return new ContextPackItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.result_type = source["result_type"];
	        this.title = source["title"];
	        this.text = source["text"];
	        this.source_id = source["source_id"];
	        this.citation = source["citation"];
	        this.score = source["score"];
	    }
	}
	export class ContextPackOptions {
	    query: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    search_scope?: string;
	    topic_hint?: string;
	    context_terms?: string[];
	    result_types?: string[];
	    source_kinds?: string[];
	    source_ids?: string[];
	    source_id?: string;
	    labels?: string[];
	    domain?: string;
	    entity?: string;
	    predicate?: string;
	    limit?: number;
	    include_disabled?: boolean;
	    prefer_embedding?: boolean;
	    max_items?: number;
	    max_chars?: number;
	
	    static createFrom(source: any = {}) {
	        return new ContextPackOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.search_scope = source["search_scope"];
	        this.topic_hint = source["topic_hint"];
	        this.context_terms = source["context_terms"];
	        this.result_types = source["result_types"];
	        this.source_kinds = source["source_kinds"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.labels = source["labels"];
	        this.domain = source["domain"];
	        this.entity = source["entity"];
	        this.predicate = source["predicate"];
	        this.limit = source["limit"];
	        this.include_disabled = source["include_disabled"];
	        this.prefer_embedding = source["prefer_embedding"];
	        this.max_items = source["max_items"];
	        this.max_chars = source["max_chars"];
	    }
	}
	export class ContextPackResult {
	    query: string;
	    count: number;
	    character_count: number;
	    items: ContextPackItem[];
	    citations: Citation[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ContextPackResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.count = source["count"];
	        this.character_count = source["character_count"];
	        this.items = this.convertValues(source["items"], ContextPackItem);
	        this.citations = this.convertValues(source["citations"], Citation);
	        this.notes = source["notes"];
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
	export class DateRange {
	    start?: string;
	    end?: string;
	
	    static createFrom(source: any = {}) {
	        return new DateRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	    }
	}
	export class DeepCrawlDepthSummary {
	    depth: number;
	    total: number;
	    saved: number;
	    failed: number;
	    urls?: string[];
	
	    static createFrom(source: any = {}) {
	        return new DeepCrawlDepthSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.depth = source["depth"];
	        this.total = source["total"];
	        this.saved = source["saved"];
	        this.failed = source["failed"];
	        this.urls = source["urls"];
	    }
	}
	export class DeepCrawlItem {
	    url: string;
	    depth: number;
	    status: string;
	    title?: string;
	    error?: string;
	    source_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeepCrawlItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.depth = source["depth"];
	        this.status = source["status"];
	        this.title = source["title"];
	        this.error = source["error"];
	        this.source_id = source["source_id"];
	    }
	}
	export class DeepCrawlRequest {
	    seed_url: string;
	    max_depth: number;
	    same_domain_only: boolean;
	    save_scope?: string;
	    topic_hint?: string;
	    distill_mode?: string;
	    labels?: string[];
	    auto_labels?: boolean;
	    preview_only: boolean;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    client_run_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeepCrawlRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seed_url = source["seed_url"];
	        this.max_depth = source["max_depth"];
	        this.same_domain_only = source["same_domain_only"];
	        this.save_scope = source["save_scope"];
	        this.topic_hint = source["topic_hint"];
	        this.distill_mode = source["distill_mode"];
	        this.labels = source["labels"];
	        this.auto_labels = source["auto_labels"];
	        this.preview_only = source["preview_only"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.client_run_id = source["client_run_id"];
	    }
	}
	export class DeepCrawlResult {
	    job_id: string;
	    status: string;
	    total_discovered: number;
	    total_saved: number;
	    duplicates: number;
	    failed: number;
	    skipped: number;
	    items?: DeepCrawlItem[];
	    by_depth?: DeepCrawlDepthSummary[];
	
	    static createFrom(source: any = {}) {
	        return new DeepCrawlResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job_id = source["job_id"];
	        this.status = source["status"];
	        this.total_discovered = source["total_discovered"];
	        this.total_saved = source["total_saved"];
	        this.duplicates = source["duplicates"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.items = this.convertValues(source["items"], DeepCrawlItem);
	        this.by_depth = this.convertValues(source["by_depth"], DeepCrawlDepthSummary);
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
	export class DirectoryImportRequest {
	    root_path: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    topic_hint?: string;
	    save_scope?: string;
	    distill_mode?: string;
	    labels?: string[];
	    auto_labels?: boolean;
	    recursive: boolean;
	    include_exts?: string[];
	    exclude_globs?: string[];
	    max_file_bytes?: number;
	    dry_run?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root_path = source["root_path"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.topic_hint = source["topic_hint"];
	        this.save_scope = source["save_scope"];
	        this.distill_mode = source["distill_mode"];
	        this.labels = source["labels"];
	        this.auto_labels = source["auto_labels"];
	        this.recursive = source["recursive"];
	        this.include_exts = source["include_exts"];
	        this.exclude_globs = source["exclude_globs"];
	        this.max_file_bytes = source["max_file_bytes"];
	        this.dry_run = source["dry_run"];
	    }
	}
	export class ImportFailedItem {
	    file_path: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportFailedItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_path = source["file_path"];
	        this.error = source["error"];
	    }
	}
	export class ImportItem {
	    id: string;
	    batch_id: string;
	    source_id?: string;
	    file_path: string;
	    relative_path?: string;
	    file_hash?: string;
	    file_size: number;
	    kind?: string;
	    status: string;
	    error_message?: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new ImportItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.batch_id = source["batch_id"];
	        this.source_id = source["source_id"];
	        this.file_path = source["file_path"];
	        this.relative_path = source["relative_path"];
	        this.file_hash = source["file_hash"];
	        this.file_size = source["file_size"];
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.error_message = source["error_message"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class DirectoryImportResult {
	    batch_id?: string;
	    status: string;
	    root_path: string;
	    total_files: number;
	    queued_files: number;
	    duplicate_files: number;
	    skipped_files: number;
	    imported_files: number;
	    failed_files: number;
	    processed_files?: number;
	    current_file?: string;
	    current_step?: string;
	    step_progress?: number;
	    total_steps?: number;
	    current_step_num?: number;
	    estimated_bytes: number;
	    warnings?: string[];
	    items?: ImportItem[];
	    failed_items?: ImportFailedItem[];
	    last_item_path?: string;
	    last_item_status?: string;
	    last_item_reason?: string;
	    ext_counts?: Record<string, number>;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.batch_id = source["batch_id"];
	        this.status = source["status"];
	        this.root_path = source["root_path"];
	        this.total_files = source["total_files"];
	        this.queued_files = source["queued_files"];
	        this.duplicate_files = source["duplicate_files"];
	        this.skipped_files = source["skipped_files"];
	        this.imported_files = source["imported_files"];
	        this.failed_files = source["failed_files"];
	        this.processed_files = source["processed_files"];
	        this.current_file = source["current_file"];
	        this.current_step = source["current_step"];
	        this.step_progress = source["step_progress"];
	        this.total_steps = source["total_steps"];
	        this.current_step_num = source["current_step_num"];
	        this.estimated_bytes = source["estimated_bytes"];
	        this.warnings = source["warnings"];
	        this.items = this.convertValues(source["items"], ImportItem);
	        this.failed_items = this.convertValues(source["failed_items"], ImportFailedItem);
	        this.last_item_path = source["last_item_path"];
	        this.last_item_status = source["last_item_status"];
	        this.last_item_reason = source["last_item_reason"];
	        this.ext_counts = source["ext_counts"];
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
	export class ListSourcesOptions {
	    owner_id?: string;
	    tenant_id?: string;
	    batch_id?: string;
	    search_scope?: string;
	    project_path?: string;
	    source_ids?: string[];
	    source_id?: string;
	    status?: string;
	    include_disabled?: boolean;
	    kind?: string;
	    source_kinds?: string[];
	    domain?: string;
	    label?: string;
	    labels?: string[];
	    query?: string;
	    coverage_filter?: string;
	    quality_grade?: string;
	    quality_grades?: string[];
	    min_quality_score?: number;
	    max_quality_score?: number;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new ListSourcesOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.batch_id = source["batch_id"];
	        this.search_scope = source["search_scope"];
	        this.project_path = source["project_path"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.status = source["status"];
	        this.include_disabled = source["include_disabled"];
	        this.kind = source["kind"];
	        this.source_kinds = source["source_kinds"];
	        this.domain = source["domain"];
	        this.label = source["label"];
	        this.labels = source["labels"];
	        this.query = source["query"];
	        this.coverage_filter = source["coverage_filter"];
	        this.quality_grade = source["quality_grade"];
	        this.quality_grades = source["quality_grades"];
	        this.min_quality_score = source["min_quality_score"];
	        this.max_quality_score = source["max_quality_score"];
	        this.limit = source["limit"];
	    }
	}
	export class DoctorFinding {
	    severity: string;
	    code: string;
	    title: string;
	    detail?: string;
	    count?: number;
	    action?: string;
	    source_ids?: string[];
	    examples?: string[];
	    filter?: ListSourcesOptions;
	
	    static createFrom(source: any = {}) {
	        return new DoctorFinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.severity = source["severity"];
	        this.code = source["code"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.count = source["count"];
	        this.action = source["action"];
	        this.source_ids = source["source_ids"];
	        this.examples = source["examples"];
	        this.filter = this.convertValues(source["filter"], ListSourcesOptions);
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
	export class VectorIndexStats {
	    enabled: boolean;
	    backend: string;
	    fallback: string;
	
	    static createFrom(source: any = {}) {
	        return new VectorIndexStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.backend = source["backend"];
	        this.fallback = source["fallback"];
	    }
	}
	export class Stats {
	    sources: number;
	    document_nodes: number;
	    cards: number;
	    facts: number;
	    source_links?: number;
	    source_link_events?: number;
	    batches: number;
	    sources_without_nodes?: number;
	    sources_without_cards?: number;
	    sources_without_facts?: number;
	    sources_rebuild_cards?: number;
	    sources_rebuild_facts?: number;
	    sources_without_links?: number;
	    sources_by_kind?: Record<string, number>;
	    sources_by_status?: Record<string, number>;
	    sources_by_domain?: Record<string, number>;
	    sources_by_label?: Record<string, number>;
	    link_events_by_action?: Record<string, number>;
	    batches_by_status?: Record<string, number>;
	    import_items_by_status?: Record<string, number>;
	    languages?: Record<string, number>;
	    scripts?: Record<string, number>;
	    vector_index?: VectorIndexStats;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sources = source["sources"];
	        this.document_nodes = source["document_nodes"];
	        this.cards = source["cards"];
	        this.facts = source["facts"];
	        this.source_links = source["source_links"];
	        this.source_link_events = source["source_link_events"];
	        this.batches = source["batches"];
	        this.sources_without_nodes = source["sources_without_nodes"];
	        this.sources_without_cards = source["sources_without_cards"];
	        this.sources_without_facts = source["sources_without_facts"];
	        this.sources_rebuild_cards = source["sources_rebuild_cards"];
	        this.sources_rebuild_facts = source["sources_rebuild_facts"];
	        this.sources_without_links = source["sources_without_links"];
	        this.sources_by_kind = source["sources_by_kind"];
	        this.sources_by_status = source["sources_by_status"];
	        this.sources_by_domain = source["sources_by_domain"];
	        this.sources_by_label = source["sources_by_label"];
	        this.link_events_by_action = source["link_events_by_action"];
	        this.batches_by_status = source["batches_by_status"];
	        this.import_items_by_status = source["import_items_by_status"];
	        this.languages = source["languages"];
	        this.scripts = source["scripts"];
	        this.vector_index = this.convertValues(source["vector_index"], VectorIndexStats);
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
	export class DoctorResult {
	    status: string;
	    score: number;
	    stats: Stats;
	    findings?: DoctorFinding[];
	    // Go type: time
	    generated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new DoctorResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.score = source["score"];
	        this.stats = this.convertValues(source["stats"], Stats);
	        this.findings = this.convertValues(source["findings"], DoctorFinding);
	        this.generated_at = this.convertValues(source["generated_at"], null);
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
	export class DocumentNode {
	    id: string;
	    source_id: string;
	    parent_id?: string;
	    type: string;
	    title?: string;
	    text?: string;
	    level?: number;
	    page?: number;
	    sheet_name?: string;
	    row_range?: string;
	    col_range?: string;
	    xpath?: string;
	    offset?: number;
	    metadata?: Record<string, string>;
	    token_count?: number;
	
	    static createFrom(source: any = {}) {
	        return new DocumentNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.parent_id = source["parent_id"];
	        this.type = source["type"];
	        this.title = source["title"];
	        this.text = source["text"];
	        this.level = source["level"];
	        this.page = source["page"];
	        this.sheet_name = source["sheet_name"];
	        this.row_range = source["row_range"];
	        this.col_range = source["col_range"];
	        this.xpath = source["xpath"];
	        this.offset = source["offset"];
	        this.metadata = source["metadata"];
	        this.token_count = source["token_count"];
	    }
	}
	export class DuplicateCardGroup {
	    key: string;
	    claim: string;
	    count: number;
	    card_ids?: string[];
	    source_ids?: string[];
	    examples?: string[];
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateCardGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.claim = source["claim"];
	        this.count = source["count"];
	        this.card_ids = source["card_ids"];
	        this.source_ids = source["source_ids"];
	        this.examples = source["examples"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	    }
	}
	export class DuplicateCardSuppressionRequest {
	    key: string;
	    keep_card_id?: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateCardSuppressionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.keep_card_id = source["keep_card_id"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.reason = source["reason"];
	    }
	}
	export class FactIndexItem {
	    label: string;
	    kind: string;
	    count: number;
	    source_count?: number;
	    card_count?: number;
	    predicates?: string[];
	    examples?: string[];
	
	    static createFrom(source: any = {}) {
	        return new FactIndexItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.count = source["count"];
	        this.source_count = source["source_count"];
	        this.card_count = source["card_count"];
	        this.predicates = source["predicates"];
	        this.examples = source["examples"];
	    }
	}
	export class FactGraphEdge {
	    id: string;
	    fact_id: string;
	    card_id?: string;
	    source_id?: string;
	    subject: string;
	    predicate: string;
	    object: string;
	    source_title?: string;
	    citation?: string;
	    confidence?: number;
	
	    static createFrom(source: any = {}) {
	        return new FactGraphEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.fact_id = source["fact_id"];
	        this.card_id = source["card_id"];
	        this.source_id = source["source_id"];
	        this.subject = source["subject"];
	        this.predicate = source["predicate"];
	        this.object = source["object"];
	        this.source_title = source["source_title"];
	        this.citation = source["citation"];
	        this.confidence = source["confidence"];
	    }
	}
	export class EntityProfileResult {
	    entity: string;
	    count: number;
	    facts?: FactGraphEdge[];
	    related_entities?: FactIndexItem[];
	    predicates?: FactIndexItem[];
	    citations?: Citation[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new EntityProfileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity = source["entity"];
	        this.count = source["count"];
	        this.facts = this.convertValues(source["facts"], FactGraphEdge);
	        this.related_entities = this.convertValues(source["related_entities"], FactIndexItem);
	        this.predicates = this.convertValues(source["predicates"], FactIndexItem);
	        this.citations = this.convertValues(source["citations"], Citation);
	        this.notes = source["notes"];
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
	export class SearchResult {
	    source: Source;
	    result_type?: string;
	    node_id?: string;
	    parent_node_id?: string;
	    language?: string;
	    script?: string;
	    node_title?: string;
	    node_type?: string;
	    page?: number;
	    sheet_name?: string;
	    row_range?: string;
	    col_range?: string;
	    citation?: string;
	    card_id?: string;
	    card_title?: string;
	    fact_id?: string;
	    table_id?: string;
	    row_id?: string;
	    cell_id?: string;
	    row_index?: number;
	    column_name?: string;
	    subject?: string;
	    predicate?: string;
	    object?: string;
	    claim?: string;
	    summary?: string;
	    snippet?: string;
	    score?: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = this.convertValues(source["source"], Source);
	        this.result_type = source["result_type"];
	        this.node_id = source["node_id"];
	        this.parent_node_id = source["parent_node_id"];
	        this.language = source["language"];
	        this.script = source["script"];
	        this.node_title = source["node_title"];
	        this.node_type = source["node_type"];
	        this.page = source["page"];
	        this.sheet_name = source["sheet_name"];
	        this.row_range = source["row_range"];
	        this.col_range = source["col_range"];
	        this.citation = source["citation"];
	        this.card_id = source["card_id"];
	        this.card_title = source["card_title"];
	        this.fact_id = source["fact_id"];
	        this.table_id = source["table_id"];
	        this.row_id = source["row_id"];
	        this.cell_id = source["cell_id"];
	        this.row_index = source["row_index"];
	        this.column_name = source["column_name"];
	        this.subject = source["subject"];
	        this.predicate = source["predicate"];
	        this.object = source["object"];
	        this.claim = source["claim"];
	        this.summary = source["summary"];
	        this.snippet = source["snippet"];
	        this.score = source["score"];
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
	export class ExplainResult {
	    query: string;
	    count: number;
	    results: SearchResult[];
	    citations: Citation[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExplainResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.count = source["count"];
	        this.results = this.convertValues(source["results"], SearchResult);
	        this.citations = this.convertValues(source["citations"], Citation);
	        this.notes = source["notes"];
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
	export class ExportOptions {
	    output_path?: string;
	    format?: string;
	    redact_sensitive: boolean;
	    source_ids?: string[];
	    tenant_id?: string;
	    owner_id?: string;
	    title?: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.output_path = source["output_path"];
	        this.format = source["format"];
	        this.redact_sensitive = source["redact_sensitive"];
	        this.source_ids = source["source_ids"];
	        this.tenant_id = source["tenant_id"];
	        this.owner_id = source["owner_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	    }
	}
	export class ExportResult {
	    output_path: string;
	    format: string;
	    redact_sensitive: boolean;
	    scoped?: boolean;
	    source_ids?: string[];
	    url_policies?: number;
	    sources: number;
	    source_labels?: number;
	    source_versions?: number;
	    source_links?: number;
	    source_link_events?: number;
	    nodes: number;
	    cards: number;
	    facts: number;
	    bytes?: number;
	    // Go type: time
	    generated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.output_path = source["output_path"];
	        this.format = source["format"];
	        this.redact_sensitive = source["redact_sensitive"];
	        this.scoped = source["scoped"];
	        this.source_ids = source["source_ids"];
	        this.url_policies = source["url_policies"];
	        this.sources = source["sources"];
	        this.source_labels = source["source_labels"];
	        this.source_versions = source["source_versions"];
	        this.source_links = source["source_links"];
	        this.source_link_events = source["source_link_events"];
	        this.nodes = source["nodes"];
	        this.cards = source["cards"];
	        this.facts = source["facts"];
	        this.bytes = source["bytes"];
	        this.generated_at = this.convertValues(source["generated_at"], null);
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
	
	
	export class FactGraphNode {
	    id: string;
	    label: string;
	    kind: string;
	    count?: number;
	
	    static createFrom(source: any = {}) {
	        return new FactGraphNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.count = source["count"];
	    }
	}
	export class FactGraphResult {
	    query?: string;
	    entity?: string;
	    predicate?: string;
	    count: number;
	    nodes?: FactGraphNode[];
	    edges?: FactGraphEdge[];
	    top_entities?: FactGraphNode[];
	    top_predicates?: FactGraphNode[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new FactGraphResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.entity = source["entity"];
	        this.predicate = source["predicate"];
	        this.count = source["count"];
	        this.nodes = this.convertValues(source["nodes"], FactGraphNode);
	        this.edges = this.convertValues(source["edges"], FactGraphEdge);
	        this.top_entities = this.convertValues(source["top_entities"], FactGraphNode);
	        this.top_predicates = this.convertValues(source["top_predicates"], FactGraphNode);
	        this.notes = source["notes"];
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
	
	export class FactIndexOptions {
	    query: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    search_scope?: string;
	    topic_hint?: string;
	    context_terms?: string[];
	    result_types?: string[];
	    source_kinds?: string[];
	    source_ids?: string[];
	    source_id?: string;
	    labels?: string[];
	    domain?: string;
	    entity?: string;
	    predicate?: string;
	    limit?: number;
	    include_disabled?: boolean;
	    prefer_embedding?: boolean;
	    kind?: string;
	
	    static createFrom(source: any = {}) {
	        return new FactIndexOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.search_scope = source["search_scope"];
	        this.topic_hint = source["topic_hint"];
	        this.context_terms = source["context_terms"];
	        this.result_types = source["result_types"];
	        this.source_kinds = source["source_kinds"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.labels = source["labels"];
	        this.domain = source["domain"];
	        this.entity = source["entity"];
	        this.predicate = source["predicate"];
	        this.limit = source["limit"];
	        this.include_disabled = source["include_disabled"];
	        this.prefer_embedding = source["prefer_embedding"];
	        this.kind = source["kind"];
	    }
	}
	export class FactIndexResult {
	    query?: string;
	    kind?: string;
	    count: number;
	    items?: FactIndexItem[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new FactIndexResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.kind = source["kind"];
	        this.count = source["count"];
	        this.items = this.convertValues(source["items"], FactIndexItem);
	        this.notes = source["notes"];
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
	export class FormatCapability {
	    kind: string;
	    extensions?: string[];
	    parser: string;
	    search_unit?: string;
	    status: string;
	    refreshable: boolean;
	    default_import: boolean;
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new FormatCapability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.extensions = source["extensions"];
	        this.parser = source["parser"];
	        this.search_unit = source["search_unit"];
	        this.status = source["status"];
	        this.refreshable = source["refreshable"];
	        this.default_import = source["default_import"];
	        this.notes = source["notes"];
	    }
	}
	export class ImportBatch {
	    id: string;
	    root_path: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    topic_hint?: string;
	    recursive: boolean;
	    include_exts?: string[];
	    exclude_globs?: string[];
	    max_file_bytes?: number;
	    status: string;
	    total_files: number;
	    queued_files: number;
	    imported_files: number;
	    skipped_files: number;
	    failed_files: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new ImportBatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.root_path = source["root_path"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.topic_hint = source["topic_hint"];
	        this.recursive = source["recursive"];
	        this.include_exts = source["include_exts"];
	        this.exclude_globs = source["exclude_globs"];
	        this.max_file_bytes = source["max_file_bytes"];
	        this.status = source["status"];
	        this.total_files = source["total_files"];
	        this.queued_files = source["queued_files"];
	        this.imported_files = source["imported_files"];
	        this.skipped_files = source["skipped_files"];
	        this.failed_files = source["failed_files"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	
	
	export class ImportRetryRequest {
	    batch_id: string;
	    item_ids?: string[];
	    statuses?: string[];
	    include_skipped?: boolean;
	    include_exts?: string[];
	    max_file_bytes?: number;
	    topic_hint?: string;
	    distill_mode?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportRetryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.batch_id = source["batch_id"];
	        this.item_ids = source["item_ids"];
	        this.statuses = source["statuses"];
	        this.include_skipped = source["include_skipped"];
	        this.include_exts = source["include_exts"];
	        this.max_file_bytes = source["max_file_bytes"];
	        this.topic_hint = source["topic_hint"];
	        this.distill_mode = source["distill_mode"];
	    }
	}
	export class KnowledgeCapabilities {
	    default_include_exts?: string[];
	    default_auto_labels: boolean;
	    auto_label_rules?: string[];
	    formats?: FormatCapability[];
	    query_requires_llm: boolean;
	    write_llm_optional: boolean;
	    distill_modes?: string[];
	    coverage_filters?: string[];
	    coverage_aliases?: Record<string, string>;
	    local_indexes?: string[];
	    storage_backend?: string;
	    search_backend?: string;
	    // Go type: time
	    generated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeCapabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default_include_exts = source["default_include_exts"];
	        this.default_auto_labels = source["default_auto_labels"];
	        this.auto_label_rules = source["auto_label_rules"];
	        this.formats = this.convertValues(source["formats"], FormatCapability);
	        this.query_requires_llm = source["query_requires_llm"];
	        this.write_llm_optional = source["write_llm_optional"];
	        this.distill_modes = source["distill_modes"];
	        this.coverage_filters = source["coverage_filters"];
	        this.coverage_aliases = source["coverage_aliases"];
	        this.local_indexes = source["local_indexes"];
	        this.storage_backend = source["storage_backend"];
	        this.search_backend = source["search_backend"];
	        this.generated_at = this.convertValues(source["generated_at"], null);
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
	export class KnowledgeSuggestOptions {
	    query: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    search_scope?: string;
	    topic_hint?: string;
	    context_terms?: string[];
	    result_types?: string[];
	    source_kinds?: string[];
	    source_ids?: string[];
	    source_id?: string;
	    labels?: string[];
	    domain?: string;
	    entity?: string;
	    predicate?: string;
	    limit?: number;
	    include_disabled?: boolean;
	    prefer_embedding?: boolean;
	    kinds?: string[];
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSuggestOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.search_scope = source["search_scope"];
	        this.topic_hint = source["topic_hint"];
	        this.context_terms = source["context_terms"];
	        this.result_types = source["result_types"];
	        this.source_kinds = source["source_kinds"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.labels = source["labels"];
	        this.domain = source["domain"];
	        this.entity = source["entity"];
	        this.predicate = source["predicate"];
	        this.limit = source["limit"];
	        this.include_disabled = source["include_disabled"];
	        this.prefer_embedding = source["prefer_embedding"];
	        this.kinds = source["kinds"];
	    }
	}
	export class KnowledgeSuggestion {
	    label: string;
	    kind: string;
	    count?: number;
	    source_id?: string;
	    source_kind?: string;
	    domain?: string;
	    source_label?: string;
	    uri?: string;
	    examples?: string[];
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSuggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.count = source["count"];
	        this.source_id = source["source_id"];
	        this.source_kind = source["source_kind"];
	        this.domain = source["domain"];
	        this.source_label = source["source_label"];
	        this.uri = source["uri"];
	        this.examples = source["examples"];
	    }
	}
	export class KnowledgeSuggestResult {
	    query?: string;
	    count: number;
	    items?: KnowledgeSuggestion[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSuggestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.count = source["count"];
	        this.items = this.convertValues(source["items"], KnowledgeSuggestion);
	        this.notes = source["notes"];
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
	
	export class KnowledgeTableColumn {
	    id: string;
	    table_id: string;
	    column_index: number;
	    column_name: string;
	    normalized_name: string;
	    value_type: string;
	    aliases_json?: string;
	    stats_json?: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeTableColumn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.table_id = source["table_id"];
	        this.column_index = source["column_index"];
	        this.column_name = source["column_name"];
	        this.normalized_name = source["normalized_name"];
	        this.value_type = source["value_type"];
	        this.aliases_json = source["aliases_json"];
	        this.stats_json = source["stats_json"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	
	export class MaintenanceResult {
	    integrity_ok: boolean;
	    integrity_check?: string;
	    optimized_fts?: string[];
	    checkpointed: boolean;
	    vacuumed: boolean;
	    warnings?: string[];
	    errors?: string[];
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    completed_at: any;
	
	    static createFrom(source: any = {}) {
	        return new MaintenanceResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.integrity_ok = source["integrity_ok"];
	        this.integrity_check = source["integrity_check"];
	        this.optimized_fts = source["optimized_fts"];
	        this.checkpointed = source["checkpointed"];
	        this.vacuumed = source["vacuumed"];
	        this.warnings = source["warnings"];
	        this.errors = source["errors"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.completed_at = this.convertValues(source["completed_at"], null);
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
	export class NumberRange {
	    min?: number;
	    max?: number;
	
	    static createFrom(source: any = {}) {
	        return new NumberRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.min = source["min"];
	        this.max = source["max"];
	    }
	}
	export class SearchFacetBucket {
	    label: string;
	    kind?: string;
	    count: number;
	    source_id?: string;
	    source_kind?: string;
	    domain?: string;
	    examples?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SearchFacetBucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.count = source["count"];
	        this.source_id = source["source_id"];
	        this.source_kind = source["source_kind"];
	        this.domain = source["domain"];
	        this.examples = source["examples"];
	    }
	}
	export class SearchFacetsResult {
	    query: string;
	    count: number;
	    result_types?: SearchFacetBucket[];
	    source_kinds?: SearchFacetBucket[];
	    domains?: SearchFacetBucket[];
	    labels?: SearchFacetBucket[];
	    sources?: SearchFacetBucket[];
	    entities?: SearchFacetBucket[];
	    predicates?: SearchFacetBucket[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SearchFacetsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.count = source["count"];
	        this.result_types = this.convertValues(source["result_types"], SearchFacetBucket);
	        this.source_kinds = this.convertValues(source["source_kinds"], SearchFacetBucket);
	        this.domains = this.convertValues(source["domains"], SearchFacetBucket);
	        this.labels = this.convertValues(source["labels"], SearchFacetBucket);
	        this.sources = this.convertValues(source["sources"], SearchFacetBucket);
	        this.entities = this.convertValues(source["entities"], SearchFacetBucket);
	        this.predicates = this.convertValues(source["predicates"], SearchFacetBucket);
	        this.notes = source["notes"];
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
	export class SearchOptions {
	    query: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    search_scope?: string;
	    topic_hint?: string;
	    context_terms?: string[];
	    result_types?: string[];
	    source_kinds?: string[];
	    source_ids?: string[];
	    source_id?: string;
	    labels?: string[];
	    domain?: string;
	    entity?: string;
	    predicate?: string;
	    limit?: number;
	    include_disabled?: boolean;
	    prefer_embedding?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SearchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.search_scope = source["search_scope"];
	        this.topic_hint = source["topic_hint"];
	        this.context_terms = source["context_terms"];
	        this.result_types = source["result_types"];
	        this.source_kinds = source["source_kinds"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.labels = source["labels"];
	        this.domain = source["domain"];
	        this.entity = source["entity"];
	        this.predicate = source["predicate"];
	        this.limit = source["limit"];
	        this.include_disabled = source["include_disabled"];
	        this.prefer_embedding = source["prefer_embedding"];
	    }
	}
	
	export class SensitiveFinding {
	    kind: string;
	    severity: string;
	    source_id?: string;
	    source_title?: string;
	    relative_path?: string;
	    uri?: string;
	    node_id?: string;
	    card_id?: string;
	    field?: string;
	    redacted?: string;
	    snippet?: string;
	
	    static createFrom(source: any = {}) {
	        return new SensitiveFinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.severity = source["severity"];
	        this.source_id = source["source_id"];
	        this.source_title = source["source_title"];
	        this.relative_path = source["relative_path"];
	        this.uri = source["uri"];
	        this.node_id = source["node_id"];
	        this.card_id = source["card_id"];
	        this.field = source["field"];
	        this.redacted = source["redacted"];
	        this.snippet = source["snippet"];
	    }
	}
	export class SourceStatusUpdateFailure {
	    source_id: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceStatusUpdateFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.error = source["error"];
	    }
	}
	export class SourceStatusUpdateResult {
	    requested: number;
	    updated: number;
	    failed: number;
	    status?: string;
	    sources?: Source[];
	    failures?: SourceStatusUpdateFailure[];
	
	    static createFrom(source: any = {}) {
	        return new SourceStatusUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.updated = source["updated"];
	        this.failed = source["failed"];
	        this.status = source["status"];
	        this.sources = this.convertValues(source["sources"], Source);
	        this.failures = this.convertValues(source["failures"], SourceStatusUpdateFailure);
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
	export class SensitiveScanResult {
	    count: number;
	    max_severity?: string;
	    findings?: SensitiveFinding[];
	
	    static createFrom(source: any = {}) {
	        return new SensitiveScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.max_severity = source["max_severity"];
	        this.findings = this.convertValues(source["findings"], SensitiveFinding);
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
	export class SensitiveIsolationResult {
	    scan: SensitiveScanResult;
	    source_ids?: string[];
	    update: SourceStatusUpdateResult;
	
	    static createFrom(source: any = {}) {
	        return new SensitiveIsolationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scan = this.convertValues(source["scan"], SensitiveScanResult);
	        this.source_ids = source["source_ids"];
	        this.update = this.convertValues(source["update"], SourceStatusUpdateResult);
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
	
	export class SnapshotImportConflict {
	    line?: number;
	    type?: string;
	    id?: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotImportConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.type = source["type"];
	        this.id = source["id"];
	    }
	}
	export class SnapshotImportFailure {
	    line?: number;
	    type?: string;
	    id?: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotImportFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.type = source["type"];
	        this.id = source["id"];
	        this.error = source["error"];
	    }
	}
	export class SnapshotImportOptions {
	    input_path: string;
	    dry_run?: boolean;
	    overwrite?: boolean;
	    replace_all?: boolean;
	    abort_on_error?: boolean;
	    skip_safety_backup?: boolean;
	    safety_backup_path?: string;
	    safety_backup_redact?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotImportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_path = source["input_path"];
	        this.dry_run = source["dry_run"];
	        this.overwrite = source["overwrite"];
	        this.replace_all = source["replace_all"];
	        this.abort_on_error = source["abort_on_error"];
	        this.skip_safety_backup = source["skip_safety_backup"];
	        this.safety_backup_path = source["safety_backup_path"];
	        this.safety_backup_redact = source["safety_backup_redact"];
	    }
	}
	export class SnapshotImportResult {
	    input_path: string;
	    dry_run?: boolean;
	    overwrite?: boolean;
	    safety_backup_path?: string;
	    safety_backup?: ExportResult;
	    records: number;
	    would_import?: number;
	    imported: number;
	    skipped: number;
	    conflicts?: number;
	    unknown_records?: number;
	    missing_references?: number;
	    failed: number;
	    url_policies?: number;
	    sources: number;
	    source_labels?: number;
	    source_versions?: number;
	    source_links?: number;
	    source_link_events?: number;
	    nodes: number;
	    cards: number;
	    facts: number;
	    conflict_items?: SnapshotImportConflict[];
	    failures?: SnapshotImportFailure[];
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    completed_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_path = source["input_path"];
	        this.dry_run = source["dry_run"];
	        this.overwrite = source["overwrite"];
	        this.safety_backup_path = source["safety_backup_path"];
	        this.safety_backup = this.convertValues(source["safety_backup"], ExportResult);
	        this.records = source["records"];
	        this.would_import = source["would_import"];
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.conflicts = source["conflicts"];
	        this.unknown_records = source["unknown_records"];
	        this.missing_references = source["missing_references"];
	        this.failed = source["failed"];
	        this.url_policies = source["url_policies"];
	        this.sources = source["sources"];
	        this.source_labels = source["source_labels"];
	        this.source_versions = source["source_versions"];
	        this.source_links = source["source_links"];
	        this.source_link_events = source["source_link_events"];
	        this.nodes = source["nodes"];
	        this.cards = source["cards"];
	        this.facts = source["facts"];
	        this.conflict_items = this.convertValues(source["conflict_items"], SnapshotImportConflict);
	        this.failures = this.convertValues(source["failures"], SnapshotImportFailure);
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.completed_at = this.convertValues(source["completed_at"], null);
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
	
	export class SourceAutoLabelBackfillRequest {
	    source_ids?: string[];
	    filter?: ListSourcesOptions;
	    dry_run?: boolean;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new SourceAutoLabelBackfillRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_ids = source["source_ids"];
	        this.filter = this.convertValues(source["filter"], ListSourcesOptions);
	        this.dry_run = source["dry_run"];
	        this.limit = source["limit"];
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
	
	
	
	
	export class SourceTimelineEvent {
	    id: string;
	    source_id: string;
	    kind: string;
	    action?: string;
	    title?: string;
	    detail?: string;
	    status?: string;
	    relation?: string;
	    related_source_id?: string;
	    score?: number;
	    terms?: string[];
	    evidence?: string[];
	    version_id?: string;
	    content_hash?: string;
	    node_count?: number;
	    card_count?: number;
	    fact_count?: number;
	    // Go type: time
	    occurred_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceTimelineEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.kind = source["kind"];
	        this.action = source["action"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.status = source["status"];
	        this.relation = source["relation"];
	        this.related_source_id = source["related_source_id"];
	        this.score = source["score"];
	        this.terms = source["terms"];
	        this.evidence = source["evidence"];
	        this.version_id = source["version_id"];
	        this.content_hash = source["content_hash"];
	        this.node_count = source["node_count"];
	        this.card_count = source["card_count"];
	        this.fact_count = source["fact_count"];
	        this.occurred_at = this.convertValues(source["occurred_at"], null);
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
	export class SourceLink {
	    source_id: string;
	    related_source_id: string;
	    relation: string;
	    score?: number;
	    terms?: string[];
	    evidence?: string[];
	    related_source?: Source;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.related_source_id = source["related_source_id"];
	        this.relation = source["relation"];
	        this.score = source["score"];
	        this.terms = source["terms"];
	        this.evidence = source["evidence"];
	        this.related_source = this.convertValues(source["related_source"], Source);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class SourceDigestResult {
	    source_id: string;
	    source: Source;
	    title?: string;
	    labels?: string[];
	    topics?: string[];
	    entities?: string[];
	    tags?: string[];
	    node_count: number;
	    card_count: number;
	    fact_count: number;
	    link_count: number;
	    timeline_count: number;
	    nodes?: DocumentNode[];
	    cards?: Card[];
	    facts?: Fact[];
	    links?: SourceLink[];
	    timeline?: SourceTimelineEvent[];
	    notes?: string[];
	    // Go type: time
	    generated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceDigestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.source = this.convertValues(source["source"], Source);
	        this.title = source["title"];
	        this.labels = source["labels"];
	        this.topics = source["topics"];
	        this.entities = source["entities"];
	        this.tags = source["tags"];
	        this.node_count = source["node_count"];
	        this.card_count = source["card_count"];
	        this.fact_count = source["fact_count"];
	        this.link_count = source["link_count"];
	        this.timeline_count = source["timeline_count"];
	        this.nodes = this.convertValues(source["nodes"], DocumentNode);
	        this.cards = this.convertValues(source["cards"], Card);
	        this.facts = this.convertValues(source["facts"], Fact);
	        this.links = this.convertValues(source["links"], SourceLink);
	        this.timeline = this.convertValues(source["timeline"], SourceTimelineEvent);
	        this.notes = source["notes"];
	        this.generated_at = this.convertValues(source["generated_at"], null);
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
	export class SourceGraphComponent {
	    id: number;
	    count: number;
	    edge_count: number;
	    density?: number;
	    average_degree?: number;
	    top_node_ids?: string[];
	    top_labels?: string[];
	    terms?: string[];
	    isolated?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SourceGraphComponent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.count = source["count"];
	        this.edge_count = source["edge_count"];
	        this.density = source["density"];
	        this.average_degree = source["average_degree"];
	        this.top_node_ids = source["top_node_ids"];
	        this.top_labels = source["top_labels"];
	        this.terms = source["terms"];
	        this.isolated = source["isolated"];
	    }
	}
	export class SourceGraphEdge {
	    id: string;
	    source_id: string;
	    related_source_id: string;
	    relation: string;
	    score?: number;
	    terms?: string[];
	    evidence?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceGraphEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.related_source_id = source["related_source_id"];
	        this.relation = source["relation"];
	        this.score = source["score"];
	        this.terms = source["terms"];
	        this.evidence = source["evidence"];
	    }
	}
	export class SourceGraphNode {
	    id: string;
	    label?: string;
	    kind?: string;
	    status?: string;
	    topic_hint?: string;
	    project_path?: string;
	    labels?: string[];
	    node_count?: number;
	    card_count?: number;
	    fact_count?: number;
	    degree?: number;
	    component_id?: number;
	    source_trust?: number;
	    relative_path?: string;
	    uri?: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceGraphNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.topic_hint = source["topic_hint"];
	        this.project_path = source["project_path"];
	        this.labels = source["labels"];
	        this.node_count = source["node_count"];
	        this.card_count = source["card_count"];
	        this.fact_count = source["fact_count"];
	        this.degree = source["degree"];
	        this.component_id = source["component_id"];
	        this.source_trust = source["source_trust"];
	        this.relative_path = source["relative_path"];
	        this.uri = source["uri"];
	    }
	}
	export class SourceGraphResult {
	    count: number;
	    edge_count: number;
	    focus_source_id?: string;
	    depth?: number;
	    component_count?: number;
	    largest_component_size?: number;
	    density?: number;
	    nodes?: SourceGraphNode[];
	    edges?: SourceGraphEdge[];
	    components?: SourceGraphComponent[];
	    isolates?: SourceGraphNode[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceGraphResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.edge_count = source["edge_count"];
	        this.focus_source_id = source["focus_source_id"];
	        this.depth = source["depth"];
	        this.component_count = source["component_count"];
	        this.largest_component_size = source["largest_component_size"];
	        this.density = source["density"];
	        this.nodes = this.convertValues(source["nodes"], SourceGraphNode);
	        this.edges = this.convertValues(source["edges"], SourceGraphEdge);
	        this.components = this.convertValues(source["components"], SourceGraphComponent);
	        this.isolates = this.convertValues(source["isolates"], SourceGraphNode);
	        this.notes = source["notes"];
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
	export class SourceLabelChange {
	    source_id: string;
	    before?: string[];
	    after?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceLabelChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class SourceLabelSummary {
	    label: string;
	    count: number;
	    source_ids?: string[];
	    source_names?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceLabelSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.count = source["count"];
	        this.source_ids = source["source_ids"];
	        this.source_names = source["source_names"];
	    }
	}
	export class SourceLabelUpdateFail {
	    source_id: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceLabelUpdateFail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.error = source["error"];
	    }
	}
	export class SourceLabelUpdateRequest {
	    source_ids?: string[];
	    filter?: ListSourcesOptions;
	    add_labels?: string[];
	    remove_labels?: string[];
	    replace_labels?: string[];
	    rename_from?: string;
	    rename_to?: string;
	    clear_labels?: boolean;
	    dry_run?: boolean;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new SourceLabelUpdateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_ids = source["source_ids"];
	        this.filter = this.convertValues(source["filter"], ListSourcesOptions);
	        this.add_labels = source["add_labels"];
	        this.remove_labels = source["remove_labels"];
	        this.replace_labels = source["replace_labels"];
	        this.rename_from = source["rename_from"];
	        this.rename_to = source["rename_to"];
	        this.clear_labels = source["clear_labels"];
	        this.dry_run = source["dry_run"];
	        this.limit = source["limit"];
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
	export class SourceLabelUpdateResult {
	    requested: number;
	    updated: number;
	    failed: number;
	    dry_run?: boolean;
	    mode?: string;
	    source_ids?: string[];
	    sources?: Source[];
	    label_changes?: SourceLabelChange[];
	    failures?: SourceLabelUpdateFail[];
	
	    static createFrom(source: any = {}) {
	        return new SourceLabelUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.updated = source["updated"];
	        this.failed = source["failed"];
	        this.dry_run = source["dry_run"];
	        this.mode = source["mode"];
	        this.source_ids = source["source_ids"];
	        this.sources = this.convertValues(source["sources"], Source);
	        this.label_changes = this.convertValues(source["label_changes"], SourceLabelChange);
	        this.failures = this.convertValues(source["failures"], SourceLabelUpdateFail);
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
	
	export class SourceLinkEvent {
	    id: string;
	    source_id: string;
	    related_source_id: string;
	    relation: string;
	    action: string;
	    score?: number;
	    terms?: string[];
	    evidence?: string[];
	    note?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceLinkEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.related_source_id = source["related_source_id"];
	        this.relation = source["relation"];
	        this.action = source["action"];
	        this.score = source["score"];
	        this.terms = source["terms"];
	        this.evidence = source["evidence"];
	        this.note = source["note"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class SourcePathStep {
	    from_source_id: string;
	    to_source_id: string;
	    relation: string;
	    score?: number;
	    terms?: string[];
	    evidence?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourcePathStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from_source_id = source["from_source_id"];
	        this.to_source_id = source["to_source_id"];
	        this.relation = source["relation"];
	        this.score = source["score"];
	        this.terms = source["terms"];
	        this.evidence = source["evidence"];
	    }
	}
	export class SourcePathResult {
	    from_source_id: string;
	    to_source_id: string;
	    found: boolean;
	    hop_count?: number;
	    max_depth: number;
	    visited_count?: number;
	    searched_edge_count?: number;
	    truncated?: boolean;
	    nodes?: SourceGraphNode[];
	    steps?: SourcePathStep[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourcePathResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from_source_id = source["from_source_id"];
	        this.to_source_id = source["to_source_id"];
	        this.found = source["found"];
	        this.hop_count = source["hop_count"];
	        this.max_depth = source["max_depth"];
	        this.visited_count = source["visited_count"];
	        this.searched_edge_count = source["searched_edge_count"];
	        this.truncated = source["truncated"];
	        this.nodes = this.convertValues(source["nodes"], SourceGraphNode);
	        this.steps = this.convertValues(source["steps"], SourcePathStep);
	        this.notes = source["notes"];
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
	
	export class SourceQualityItem {
	    source: Source;
	    score: number;
	    grade: string;
	    signals?: string[];
	    actions?: string[];
	    sensitive_findings?: number;
	    duplicate_claims?: number;
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = this.convertValues(source["source"], Source);
	        this.score = source["score"];
	        this.grade = source["grade"];
	        this.signals = source["signals"];
	        this.actions = source["actions"];
	        this.sensitive_findings = source["sensitive_findings"];
	        this.duplicate_claims = source["duplicate_claims"];
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
	export class SourceQualityMaintenanceAction {
	    kind: string;
	    title?: string;
	    description?: string;
	    severity?: string;
	    count: number;
	    source_ids?: string[];
	    signals?: string[];
	    tool?: string;
	    args?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityMaintenanceAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.severity = source["severity"];
	        this.count = source["count"];
	        this.source_ids = source["source_ids"];
	        this.signals = source["signals"];
	        this.tool = source["tool"];
	        this.args = source["args"];
	    }
	}
	export class SourceQualityMaintenanceActionResult {
	    kind: string;
	    requested: number;
	    updated?: number;
	    failed?: number;
	    skipped?: number;
	    dry_run?: boolean;
	    source_ids?: string[];
	    result?: any;
	    warnings?: string[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityMaintenanceActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.requested = source["requested"];
	        this.updated = source["updated"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.dry_run = source["dry_run"];
	        this.source_ids = source["source_ids"];
	        this.result = source["result"];
	        this.warnings = source["warnings"];
	        this.error = source["error"];
	    }
	}
	export class SourceQualityMaintenanceExecuteRequest {
	    filter?: ListSourcesOptions;
	    policy?: string;
	    actions?: string[];
	    dry_run?: boolean;
	    distill_mode?: string;
	    max_sources_per_action?: number;
	    allow_sensitive_disable?: boolean;
	    allow_duplicate_suppression?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityMaintenanceExecuteRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filter = this.convertValues(source["filter"], ListSourcesOptions);
	        this.policy = source["policy"];
	        this.actions = source["actions"];
	        this.dry_run = source["dry_run"];
	        this.distill_mode = source["distill_mode"];
	        this.max_sources_per_action = source["max_sources_per_action"];
	        this.allow_sensitive_disable = source["allow_sensitive_disable"];
	        this.allow_duplicate_suppression = source["allow_duplicate_suppression"];
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
	export class SourceQualityReport {
	    count: number;
	    average_score?: number;
	    grades?: Record<string, number>;
	    signals?: Record<string, number>;
	    actions?: Record<string, number>;
	    items?: SourceQualityItem[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.average_score = source["average_score"];
	        this.grades = source["grades"];
	        this.signals = source["signals"];
	        this.actions = source["actions"];
	        this.items = this.convertValues(source["items"], SourceQualityItem);
	        this.notes = source["notes"];
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
	export class SourceQualityMaintenancePlan {
	    quality: SourceQualityReport;
	    count: number;
	    actions?: SourceQualityMaintenanceAction[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityMaintenancePlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.quality = this.convertValues(source["quality"], SourceQualityReport);
	        this.count = source["count"];
	        this.actions = this.convertValues(source["actions"], SourceQualityMaintenanceAction);
	        this.notes = source["notes"];
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
	export class SourceQualityMaintenanceExecuteResult {
	    plan: SourceQualityMaintenancePlan;
	    dry_run?: boolean;
	    count: number;
	    results?: SourceQualityMaintenanceActionResult[];
	    warnings?: string[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityMaintenanceExecuteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plan = this.convertValues(source["plan"], SourceQualityMaintenancePlan);
	        this.dry_run = source["dry_run"];
	        this.count = source["count"];
	        this.results = this.convertValues(source["results"], SourceQualityMaintenanceActionResult);
	        this.warnings = source["warnings"];
	        this.notes = source["notes"];
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
	
	export class SourceQualityMaintenancePolicy {
	    name: string;
	    title?: string;
	    description?: string;
	    actions?: string[];
	    default_dry_run?: boolean;
	    distill_mode?: string;
	    max_sources_per_action?: number;
	    allow_sensitive_disable?: boolean;
	    allow_duplicate_suppression?: boolean;
	    query_requires_llm: boolean;
	    may_use_llm_for_structuring?: boolean;
	    requires_explicit_write?: boolean;
	    requires_explicit_sensitive?: boolean;
	    requires_explicit_duplicate?: boolean;
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceQualityMaintenancePolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.actions = source["actions"];
	        this.default_dry_run = source["default_dry_run"];
	        this.distill_mode = source["distill_mode"];
	        this.max_sources_per_action = source["max_sources_per_action"];
	        this.allow_sensitive_disable = source["allow_sensitive_disable"];
	        this.allow_duplicate_suppression = source["allow_duplicate_suppression"];
	        this.query_requires_llm = source["query_requires_llm"];
	        this.may_use_llm_for_structuring = source["may_use_llm_for_structuring"];
	        this.requires_explicit_write = source["requires_explicit_write"];
	        this.requires_explicit_sensitive = source["requires_explicit_sensitive"];
	        this.requires_explicit_duplicate = source["requires_explicit_duplicate"];
	        this.notes = source["notes"];
	    }
	}
	
	export class SourceRebuildFailure {
	    source_id: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceRebuildFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.error = source["error"];
	    }
	}
	export class SourceRebuildResult {
	    requested: number;
	    rebuilt: number;
	    failed: number;
	    sources?: Source[];
	    failures?: SourceRebuildFailure[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceRebuildResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.rebuilt = source["rebuilt"];
	        this.failed = source["failed"];
	        this.sources = this.convertValues(source["sources"], Source);
	        this.failures = this.convertValues(source["failures"], SourceRebuildFailure);
	        this.warnings = source["warnings"];
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
	
	
	
	
	
	export class SourceTimelineResult {
	    source_id: string;
	    source: Source;
	    count: number;
	    limit: number;
	    events: SourceTimelineEvent[];
	    notes?: string[];
	    // Go type: time
	    generated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceTimelineResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.source = this.convertValues(source["source"], Source);
	        this.count = source["count"];
	        this.limit = source["limit"];
	        this.events = this.convertValues(source["events"], SourceTimelineEvent);
	        this.notes = source["notes"];
	        this.generated_at = this.convertValues(source["generated_at"], null);
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
	export class SourceTopicLinkBuildResult {
	    source_id: string;
	    scanned: number;
	    candidates?: number;
	    linked: number;
	    skipped?: number;
	    links?: SourceLink[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceTopicLinkBuildResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.scanned = source["scanned"];
	        this.candidates = source["candidates"];
	        this.linked = source["linked"];
	        this.skipped = source["skipped"];
	        this.links = this.convertValues(source["links"], SourceLink);
	        this.notes = source["notes"];
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
	export class SourceUnlinkResult {
	    source_id: string;
	    related_source_id: string;
	    relation: string;
	    deleted: number;
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceUnlinkResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source_id = source["source_id"];
	        this.related_source_id = source["related_source_id"];
	        this.relation = source["relation"];
	        this.deleted = source["deleted"];
	        this.notes = source["notes"];
	    }
	}
	export class SourceUpdateRequest {
	    id: string;
	    title?: string;
	    topic_hint?: string;
	    source_trust?: number;
	    labels?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SourceUpdateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.topic_hint = source["topic_hint"];
	        this.source_trust = source["source_trust"];
	        this.labels = source["labels"];
	    }
	}
	export class SourceVersion {
	    id: string;
	    source_id: string;
	    kind?: string;
	    uri?: string;
	    canonical_uri?: string;
	    title?: string;
	    content_hash?: string;
	    status?: string;
	    reason?: string;
	    // Go type: time
	    fetched_at?: any;
	    node_count?: number;
	    card_count?: number;
	    fact_count?: number;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SourceVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.kind = source["kind"];
	        this.uri = source["uri"];
	        this.canonical_uri = source["canonical_uri"];
	        this.title = source["title"];
	        this.content_hash = source["content_hash"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.fetched_at = this.convertValues(source["fetched_at"], null);
	        this.node_count = source["node_count"];
	        this.card_count = source["card_count"];
	        this.fact_count = source["fact_count"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	
	export class StructuredCatalogOptions {
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    search_scope?: string;
	    source_ids?: string[];
	    source_id?: string;
	    sheet_names?: string[];
	    limit?: number;
	    include_disabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StructuredCatalogOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.search_scope = source["search_scope"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.sheet_names = source["sheet_names"];
	        this.limit = source["limit"];
	        this.include_disabled = source["include_disabled"];
	    }
	}
	export class StructuredTableCatalog {
	    id: string;
	    source_id: string;
	    source_title?: string;
	    source_kind?: string;
	    sheet_name: string;
	    table_title?: string;
	    row_count: number;
	    column_count: number;
	    columns?: KnowledgeTableColumn[];
	
	    static createFrom(source: any = {}) {
	        return new StructuredTableCatalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source_id = source["source_id"];
	        this.source_title = source["source_title"];
	        this.source_kind = source["source_kind"];
	        this.sheet_name = source["sheet_name"];
	        this.table_title = source["table_title"];
	        this.row_count = source["row_count"];
	        this.column_count = source["column_count"];
	        this.columns = this.convertValues(source["columns"], KnowledgeTableColumn);
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
	export class StructuredCatalogResult {
	    count: number;
	    tables?: StructuredTableCatalog[];
	
	    static createFrom(source: any = {}) {
	        return new StructuredCatalogResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.tables = this.convertValues(source["tables"], StructuredTableCatalog);
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
	export class StructuredSearchOptions {
	    query?: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    search_scope?: string;
	    source_ids?: string[];
	    source_id?: string;
	    sheet_names?: string[];
	    column_equals?: Record<string, string>;
	    column_contains?: Record<string, string>;
	    number_ranges?: Record<string, NumberRange>;
	    date_ranges?: Record<string, DateRange>;
	    limit?: number;
	    include_disabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StructuredSearchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.search_scope = source["search_scope"];
	        this.source_ids = source["source_ids"];
	        this.source_id = source["source_id"];
	        this.sheet_names = source["sheet_names"];
	        this.column_equals = source["column_equals"];
	        this.column_contains = source["column_contains"];
	        this.number_ranges = this.convertValues(source["number_ranges"], NumberRange, true);
	        this.date_ranges = this.convertValues(source["date_ranges"], DateRange, true);
	        this.limit = source["limit"];
	        this.include_disabled = source["include_disabled"];
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
	
	export class TextSaveRequest {
	    text: string;
	    title?: string;
	    kind?: string;
	    owner_id?: string;
	    tenant_id?: string;
	    project_path?: string;
	    topic_hint?: string;
	    save_scope?: string;
	    distill_mode?: string;
	    labels?: string[];
	    auto_labels?: boolean;
	    batch_id?: string;
	    force_id?: string;
	    // Go type: time
	    force_created_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new TextSaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.title = source["title"];
	        this.kind = source["kind"];
	        this.owner_id = source["owner_id"];
	        this.tenant_id = source["tenant_id"];
	        this.project_path = source["project_path"];
	        this.topic_hint = source["topic_hint"];
	        this.save_scope = source["save_scope"];
	        this.distill_mode = source["distill_mode"];
	        this.labels = source["labels"];
	        this.auto_labels = source["auto_labels"];
	        this.batch_id = source["batch_id"];
	        this.force_id = source["force_id"];
	        this.force_created_at = this.convertValues(source["force_created_at"], null);
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
	export class TopicRelevanceSource {
	    source: Source;
	    score?: number;
	    matched_terms?: string[];
	    label_matches?: string[];
	    source_hits?: number;
	    card_hits?: number;
	    fact_hits?: number;
	    node_hits?: number;
	
	    static createFrom(source: any = {}) {
	        return new TopicRelevanceSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = this.convertValues(source["source"], Source);
	        this.score = source["score"];
	        this.matched_terms = source["matched_terms"];
	        this.label_matches = source["label_matches"];
	        this.source_hits = source["source_hits"];
	        this.card_hits = source["card_hits"];
	        this.fact_hits = source["fact_hits"];
	        this.node_hits = source["node_hits"];
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
	export class TopicRelevanceReport {
	    topic_hint?: string;
	    query?: string;
	    terms?: string[];
	    count: number;
	    sources?: TopicRelevanceSource[];
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new TopicRelevanceReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.topic_hint = source["topic_hint"];
	        this.query = source["query"];
	        this.terms = source["terms"];
	        this.count = source["count"];
	        this.sources = this.convertValues(source["sources"], TopicRelevanceSource);
	        this.notes = source["notes"];
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
	
	export class URLBatchSaveItem {
	    url: string;
	    source_id?: string;
	    title?: string;
	    status: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new URLBatchSaveItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.source_id = source["source_id"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.error = source["error"];
	    }
	}
	export class URLBatchSaveResult {
	    requested: number;
	    saved: number;
	    duplicates: number;
	    failed: number;
	    skipped: number;
	    items?: URLBatchSaveItem[];
	    sources?: Source[];
	
	    static createFrom(source: any = {}) {
	        return new URLBatchSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.saved = source["saved"];
	        this.duplicates = source["duplicates"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.items = this.convertValues(source["items"], URLBatchSaveItem);
	        this.sources = this.convertValues(source["sources"], Source);
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
	export class URLDiscoveryItem {
	    url: string;
	    host?: string;
	    status: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new URLDiscoveryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.host = source["host"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	    }
	}
	export class URLDiscoveryRequest {
	    text: string;
	    base_url?: string;
	    same_domain_only?: boolean;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new URLDiscoveryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.base_url = source["base_url"];
	        this.same_domain_only = source["same_domain_only"];
	        this.limit = source["limit"];
	    }
	}
	export class URLDiscoveryResult {
	    requested: number;
	    candidates: number;
	    rejected: number;
	    skipped: number;
	    items?: URLDiscoveryItem[];
	    urls?: string[];
	
	    static createFrom(source: any = {}) {
	        return new URLDiscoveryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requested = source["requested"];
	        this.candidates = source["candidates"];
	        this.rejected = source["rejected"];
	        this.skipped = source["skipped"];
	        this.items = this.convertValues(source["items"], URLDiscoveryItem);
	        this.urls = source["urls"];
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
	export class URLDomainPolicy {
	    domain: string;
	    action: string;
	    reason?: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new URLDomainPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.domain = source["domain"];
	        this.action = source["action"];
	        this.reason = source["reason"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class URLDomainPolicyCheck {
	    url?: string;
	    host?: string;
	    allowed: boolean;
	    reason?: string;
	    matched_policy?: URLDomainPolicy;
	
	    static createFrom(source: any = {}) {
	        return new URLDomainPolicyCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.host = source["host"];
	        this.allowed = source["allowed"];
	        this.reason = source["reason"];
	        this.matched_policy = this.convertValues(source["matched_policy"], URLDomainPolicy);
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
	export class URLDomainPolicyUpdateRequest {
	    allow_domains?: string[];
	    block_domains?: string[];
	    replace?: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new URLDomainPolicyUpdateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allow_domains = source["allow_domains"];
	        this.block_domains = source["block_domains"];
	        this.replace = source["replace"];
	        this.reason = source["reason"];
	    }
	}
	export class URLDomainPolicyUpdateResult {
	    policies?: URLDomainPolicy[];
	    updated: number;
	    deleted?: number;
	
	    static createFrom(source: any = {}) {
	        return new URLDomainPolicyUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.policies = this.convertValues(source["policies"], URLDomainPolicy);
	        this.updated = source["updated"];
	        this.deleted = source["deleted"];
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
	
	export class AIAssistantBackgroundTaskRequest {
	    text: string;
	    project_path?: string;
	    force_background?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantBackgroundTaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.project_path = source["project_path"];
	        this.force_background = source["force_background"];
	    }
	}
	export class AIAssistantBackgroundTaskResult {
	    accepted: boolean;
	    mode: string;
	    session_id?: string;
	    job_id?: string;
	    run_id?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantBackgroundTaskResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accepted = source["accepted"];
	        this.mode = source["mode"];
	        this.session_id = source["session_id"];
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.error = source["error"];
	    }
	}
	export class AIAssistantContextMessage {
	    role: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantContextMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	    }
	}
	export class AIAssistantExternalCallbacks {
	
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantExternalCallbacks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class AIAssistantSendRequest {
	    text: string;
	    request_id?: string;
	    recent_messages?: AIAssistantContextMessage[];
	    resume_slot_id?: string;
	    start_new_task?: boolean;
	    dismiss_slot_id?: string;
	    lang?: string;
	    resume_session_id?: string;
	    dismiss_recoverable_session_id?: string;
	    ui_action?: boolean;
	    project_path?: string;
	    expert_id?: string;
	    event_scope_id?: string;
	    im_platform?: string;
	    im_target_uid?: string;
	    im_task_title?: string;
	    im_is_group?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantSendRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.request_id = source["request_id"];
	        this.recent_messages = this.convertValues(source["recent_messages"], AIAssistantContextMessage);
	        this.resume_slot_id = source["resume_slot_id"];
	        this.start_new_task = source["start_new_task"];
	        this.dismiss_slot_id = source["dismiss_slot_id"];
	        this.lang = source["lang"];
	        this.resume_session_id = source["resume_session_id"];
	        this.dismiss_recoverable_session_id = source["dismiss_recoverable_session_id"];
	        this.ui_action = source["ui_action"];
	        this.project_path = source["project_path"];
	        this.expert_id = source["expert_id"];
	        this.event_scope_id = source["event_scope_id"];
	        this.im_platform = source["im_platform"];
	        this.im_target_uid = source["im_target_uid"];
	        this.im_task_title = source["im_task_title"];
	        this.im_is_group = source["im_is_group"];
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
	export class EvidenceRecord {
	    evidence_id: string;
	    job_id: string;
	    run_id: string;
	    project_path?: string;
	    source_kind: string;
	    category?: string;
	    summary: string;
	    content_snippet?: string;
	    related_file?: string;
	    command?: string;
	    created_at: number;
	
	    static createFrom(source: any = {}) {
	        return new EvidenceRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.evidence_id = source["evidence_id"];
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.project_path = source["project_path"];
	        this.source_kind = source["source_kind"];
	        this.category = source["category"];
	        this.summary = source["summary"];
	        this.content_snippet = source["content_snippet"];
	        this.related_file = source["related_file"];
	        this.command = source["command"];
	        this.created_at = source["created_at"];
	    }
	}
	export class TraceToolObservation {
	    tool_name: string;
	    outcome: string;
	
	    static createFrom(source: any = {}) {
	        return new TraceToolObservation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_name = source["tool_name"];
	        this.outcome = source["outcome"];
	    }
	}
	export class TraceEvent {
	    event_id: string;
	    job_id: string;
	    run_id: string;
	    project_path?: string;
	    kind: string;
	    severity?: string;
	    title: string;
	    summary?: string;
	    related_file?: string;
	    command?: string;
	    tool_outcomes?: TraceToolObservation[];
	    created_at: number;
	
	    static createFrom(source: any = {}) {
	        return new TraceEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event_id = source["event_id"];
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.project_path = source["project_path"];
	        this.kind = source["kind"];
	        this.severity = source["severity"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.related_file = source["related_file"];
	        this.command = source["command"];
	        this.tool_outcomes = this.convertValues(source["tool_outcomes"], TraceToolObservation);
	        this.created_at = source["created_at"];
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
	export class TrialReflectSummary {
	    attempt_count?: number;
	    attempted_tools?: string[];
	    failure_count?: number;
	    failure_categories?: string[];
	    repeat_guard?: boolean;
	    recovered?: boolean;
	    final_outcome?: string;
	    strategy_note?: string;
	
	    static createFrom(source: any = {}) {
	        return new TrialReflectSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attempt_count = source["attempt_count"];
	        this.attempted_tools = source["attempted_tools"];
	        this.failure_count = source["failure_count"];
	        this.failure_categories = source["failure_categories"];
	        this.repeat_guard = source["repeat_guard"];
	        this.recovered = source["recovered"];
	        this.final_outcome = source["final_outcome"];
	        this.strategy_note = source["strategy_note"];
	    }
	}
	export class AIAssistantTraceView {
	    job_id: string;
	    run_id: string;
	    kind: string;
	    title: string;
	    source?: string;
	    user_id?: string;
	    project_path?: string;
	    session_id?: string;
	    loop_id?: string;
	    linked_run_ids?: string[];
	    status: string;
	    summary?: string;
	    error?: string;
	    started_at: number;
	    updated_at: number;
	    ended_at?: number;
	    event_count: number;
	    evidence_count: number;
	    trial_reflect_summary?: TrialReflectSummary;
	    events: TraceEvent[];
	    evidence: EvidenceRecord[];
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantTraceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source = source["source"];
	        this.user_id = source["user_id"];
	        this.project_path = source["project_path"];
	        this.session_id = source["session_id"];
	        this.loop_id = source["loop_id"];
	        this.linked_run_ids = source["linked_run_ids"];
	        this.status = source["status"];
	        this.summary = source["summary"];
	        this.error = source["error"];
	        this.started_at = source["started_at"];
	        this.updated_at = source["updated_at"];
	        this.ended_at = source["ended_at"];
	        this.event_count = source["event_count"];
	        this.evidence_count = source["evidence_count"];
	        this.trial_reflect_summary = this.convertValues(source["trial_reflect_summary"], TrialReflectSummary);
	        this.events = this.convertValues(source["events"], TraceEvent);
	        this.evidence = this.convertValues(source["evidence"], EvidenceRecord);
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
	export class AIAssistantUIState {
	    messages: any[];
	    prompts: string[];
	    context_boundary_message_id?: string;
	    updated_at?: string;
	    storage_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new AIAssistantUIState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = source["messages"];
	        this.prompts = source["prompts"];
	        this.context_boundary_message_id = source["context_boundary_message_id"];
	        this.updated_at = source["updated_at"];
	        this.storage_path = source["storage_path"];
	    }
	}
	export class AccessControlList {
	    mode: string;
	    departments?: string[];
	    roles?: string[];
	    skills?: string[];
	    entities?: string[];
	
	    static createFrom(source: any = {}) {
	        return new AccessControlList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.departments = source["departments"];
	        this.roles = source["roles"];
	        this.skills = source["skills"];
	        this.entities = source["entities"];
	    }
	}
	export class AgentViewDismissPayload {
	    view_id: string;
	    data?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new AgentViewDismissPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.view_id = source["view_id"];
	        this.data = source["data"];
	    }
	}
	export class AgentViewSubmitPayload {
	    view_id: string;
	    data: Record<string, any>;
	    request_id?: string;
	    view_revision?: number;
	    schema_version?: string;
	    app_id?: string;
	    session_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentViewSubmitPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.view_id = source["view_id"];
	        this.data = source["data"];
	        this.request_id = source["request_id"];
	        this.view_revision = source["view_revision"];
	        this.schema_version = source["schema_version"];
	        this.app_id = source["app_id"];
	        this.session_id = source["session_id"];
	    }
	}
	export class AndroidPWAShellRequest {
	    output_dir: string;
	    app_name: string;
	    application_id: string;
	    hubcenter_url: string;
	    start_url: string;
	
	    static createFrom(source: any = {}) {
	        return new AndroidPWAShellRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.output_dir = source["output_dir"];
	        this.app_name = source["app_name"];
	        this.application_id = source["application_id"];
	        this.hubcenter_url = source["hubcenter_url"];
	        this.start_url = source["start_url"];
	    }
	}
	export class AndroidPWAShellResult {
	    project_dir: string;
	    readme_path: string;
	    manifest_path: string;
	    main_activity_path: string;
	    start_url: string;
	    hubcenter_url: string;
	
	    static createFrom(source: any = {}) {
	        return new AndroidPWAShellResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_dir = source["project_dir"];
	        this.readme_path = source["readme_path"];
	        this.manifest_path = source["manifest_path"];
	        this.main_activity_path = source["main_activity_path"];
	        this.start_url = source["start_url"];
	        this.hubcenter_url = source["hubcenter_url"];
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
	export class RuleCondition {
	    field: string;
	    operator: string;
	    value: any;
	
	    static createFrom(source: any = {}) {
	        return new RuleCondition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.operator = source["operator"];
	        this.value = source["value"];
	    }
	}
	export class ApprovalRule {
	    id: string;
	    name: string;
	    position: number;
	    conditions: RuleCondition[];
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApprovalRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.position = source["position"];
	        this.conditions = this.convertValues(source["conditions"], RuleCondition);
	        this.reason = source["reason"];
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
	export class ApprovalRules {
	    auto_reject: ApprovalRule[];
	    auto_approve: ApprovalRule[];
	    require_human: ApprovalRule[];
	
	    static createFrom(source: any = {}) {
	        return new ApprovalRules(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auto_reject = this.convertValues(source["auto_reject"], ApprovalRule);
	        this.auto_approve = this.convertValues(source["auto_approve"], ApprovalRule);
	        this.require_human = this.convertValues(source["require_human"], ApprovalRule);
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
	export class ArchiveResult {
	    archived: boolean;
	    experience_extracted: boolean;
	    experience_summary?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ArchiveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.archived = source["archived"];
	        this.experience_extracted = source["experience_extracted"];
	        this.experience_summary = source["experience_summary"];
	        this.message = source["message"];
	    }
	}
	export class AuditLog {
	
	
	    static createFrom(source: any = {}) {
	        return new AuditLog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class AutoPublishTrigger {
	
	
	    static createFrom(source: any = {}) {
	        return new AutoPublishTrigger(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class BackgroundLoopView {
	    id: string;
	    slot_kind: string;
	    description: string;
	    iteration: number;
	    max_iter: number;
	    status: string;
	    session_id: string;
	    started_at: string;
	    queued_count: number;
	
	    static createFrom(source: any = {}) {
	        return new BackgroundLoopView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.slot_kind = source["slot_kind"];
	        this.description = source["description"];
	        this.iteration = source["iteration"];
	        this.max_iter = source["max_iter"];
	        this.status = source["status"];
	        this.session_id = source["session_id"];
	        this.started_at = source["started_at"];
	        this.queued_count = source["queued_count"];
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
	export class BrowserAgentManager {
	
	
	    static createFrom(source: any = {}) {
	        return new BrowserAgentManager(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class CapabilityGapDetector {
	
	
	    static createFrom(source: any = {}) {
	        return new CapabilityGapDetector(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class CapabilitySyncStatus {
	    managed_checked: number;
	    managed_installed: number;
	    updated: number;
	    inventory_reported: number;
	    recommended_count: number;
	    needs_user_config?: string[];
	    errors?: string[];
	
	    static createFrom(source: any = {}) {
	        return new CapabilitySyncStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.managed_checked = source["managed_checked"];
	        this.managed_installed = source["managed_installed"];
	        this.updated = source["updated"];
	        this.inventory_reported = source["inventory_reported"];
	        this.recommended_count = source["recommended_count"];
	        this.needs_user_config = source["needs_user_config"];
	        this.errors = source["errors"];
	    }
	}
	export class ClientNotification {
	    id: string;
	    title: string;
	    content: string;
	    category: string;
	    priority: string;
	    is_read: boolean;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientNotification(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.priority = source["priority"];
	        this.is_read = source["is_read"];
	        this.created_at = source["created_at"];
	    }
	}
	export class CodeGenModelItem {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeGenModelItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class CodeGenSSOEmbeddedResult {
	    qr_code_url: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeGenSSOEmbeddedResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.qr_code_url = source["qr_code_url"];
	    }
	}
	export class CodeGenSSOInfo {
	    message: string;
	    email: string;
	    model_id: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeGenSSOInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.email = source["email"];
	        this.model_id = source["model_id"];
	    }
	}
	export class CodingKnowledgeProjectCapacity {
	    project_path: string;
	    count: number;
	    over: number;
	
	    static createFrom(source: any = {}) {
	        return new CodingKnowledgeProjectCapacity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_path = source["project_path"];
	        this.count = source["count"];
	        this.over = source["over"];
	    }
	}
	export class CodingKnowledgeCapacityStatus {
	    total_count: number;
	    max_total: number;
	    max_per_project: number;
	    over_total: number;
	    would_evict: number;
	    within_limit: boolean;
	    projects_over?: CodingKnowledgeProjectCapacity[];
	
	    static createFrom(source: any = {}) {
	        return new CodingKnowledgeCapacityStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_count = source["total_count"];
	        this.max_total = source["max_total"];
	        this.max_per_project = source["max_per_project"];
	        this.over_total = source["over_total"];
	        this.would_evict = source["would_evict"];
	        this.within_limit = source["within_limit"];
	        this.projects_over = this.convertValues(source["projects_over"], CodingKnowledgeProjectCapacity);
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
	export class CodingKnowledgeExportPack {
	    version: string;
	    // Go type: time
	    exported_at: any;
	    description?: string;
	    count: number;
	    experiences: knowledge.CodingExperience[];
	
	    static createFrom(source: any = {}) {
	        return new CodingKnowledgeExportPack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.exported_at = this.convertValues(source["exported_at"], null);
	        this.description = source["description"];
	        this.count = source["count"];
	        this.experiences = this.convertValues(source["experiences"], knowledge.CodingExperience);
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
	
	export class CodingSessionStarter {
	
	
	    static createFrom(source: any = {}) {
	        return new CodingSessionStarter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class CodingWorkbenchDirectoryEntry {
	    name: string;
	    path: string;
	    is_dir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CodingWorkbenchDirectoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.is_dir = source["is_dir"];
	    }
	}
	export class CodingWorkbenchDirectoryResponse {
	    root: string;
	    path: string;
	    entries: CodingWorkbenchDirectoryEntry[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CodingWorkbenchDirectoryResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.path = source["path"];
	        this.entries = this.convertValues(source["entries"], CodingWorkbenchDirectoryEntry);
	        this.truncated = source["truncated"];
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
	export class CodingWorkbenchEntryProperties {
	    name: string;
	    path: string;
	    abs_path: string;
	    is_dir: boolean;
	    size: number;
	    size_known: boolean;
	    modified_at: number;
	    mode: string;
	    extension: string;
	
	    static createFrom(source: any = {}) {
	        return new CodingWorkbenchEntryProperties(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.abs_path = source["abs_path"];
	        this.is_dir = source["is_dir"];
	        this.size = source["size"];
	        this.size_known = source["size_known"];
	        this.modified_at = source["modified_at"];
	        this.mode = source["mode"];
	        this.extension = source["extension"];
	    }
	}
	export class CodingWorkbenchFilePreview {
	    path: string;
	    abs_path: string;
	    content: string;
	    language: string;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CodingWorkbenchFilePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.abs_path = source["abs_path"];
	        this.content = source["content"];
	        this.language = source["language"];
	        this.truncated = source["truncated"];
	    }
	}
	export class codingCheckpointListEntry {
	    label: string;
	    summary?: string;
	    session_plan?: string;
	    file_count?: number;
	    snapshot_count?: number;
	    created_at?: number;
	    current?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new codingCheckpointListEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.summary = source["summary"];
	        this.session_plan = source["session_plan"];
	        this.file_count = source["file_count"];
	        this.snapshot_count = source["snapshot_count"];
	        this.created_at = source["created_at"];
	        this.current = source["current"];
	    }
	}
	export class codingRouteCapability {
	    pref: string;
	    available: boolean;
	    model?: string;
	    source?: string;
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new codingRouteCapability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pref = source["pref"];
	        this.available = source["available"];
	        this.model = source["model"];
	        this.source = source["source"];
	        this.note = source["note"];
	    }
	}
	export class codingWorkbenchConflict {
	    id: string;
	    step_index: number;
	    branch?: string;
	    path: string;
	    project_path?: string;
	    git_root?: string;
	    main_project?: string;
	    error?: string;
	    kind: string;
	    files?: string[];
	    created_at: number;
	
	    static createFrom(source: any = {}) {
	        return new codingWorkbenchConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.step_index = source["step_index"];
	        this.branch = source["branch"];
	        this.path = source["path"];
	        this.project_path = source["project_path"];
	        this.git_root = source["git_root"];
	        this.main_project = source["main_project"];
	        this.error = source["error"];
	        this.kind = source["kind"];
	        this.files = source["files"];
	        this.created_at = source["created_at"];
	    }
	}
	export class codingWorkbenchStepStatus {
	    index: number;
	    title?: string;
	    status: string;
	    summary?: string;
	    verify_cmd?: string;
	    verify_ok?: boolean;
	    updated_unix?: number;
	
	    static createFrom(source: any = {}) {
	        return new codingWorkbenchStepStatus(source);
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
	    step_statuses?: codingWorkbenchStepStatus[];
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
	    conflicts?: codingWorkbenchConflict[];
	    conflict_active_id?: string;
	    conflict_selected?: string[];
	    conflict_focus_file?: string;
	    conflict_log?: string[];
	    route_model?: string;
	    route_source?: string;
	    route_task?: string;
	    route_reason?: string;
	    route_pref?: string;
	    route_capabilities?: codingRouteCapability[];
	    checkpoint_files?: string[];
	    checkpoint_snapshots?: number;
	    checkpoint_history?: codingCheckpointListEntry[];
	    hooks_active?: boolean;
	    hooks_phases?: string[];
	    hooks_command_count?: number;
	    hooks_fail_on_error?: boolean;
	    last_summary?: string;
	    remote_host?: string;
	    remote_user?: string;
	    remote_port?: number;
	    remote_work_dir?: string;
	    remote_safety?: string;
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
	        this.step_statuses = this.convertValues(source["step_statuses"], codingWorkbenchStepStatus);
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
	        this.conflicts = this.convertValues(source["conflicts"], codingWorkbenchConflict);
	        this.conflict_active_id = source["conflict_active_id"];
	        this.conflict_selected = source["conflict_selected"];
	        this.conflict_focus_file = source["conflict_focus_file"];
	        this.conflict_log = source["conflict_log"];
	        this.route_model = source["route_model"];
	        this.route_source = source["route_source"];
	        this.route_task = source["route_task"];
	        this.route_reason = source["route_reason"];
	        this.route_pref = source["route_pref"];
	        this.route_capabilities = this.convertValues(source["route_capabilities"], codingRouteCapability);
	        this.checkpoint_files = source["checkpoint_files"];
	        this.checkpoint_snapshots = source["checkpoint_snapshots"];
	        this.checkpoint_history = this.convertValues(source["checkpoint_history"], codingCheckpointListEntry);
	        this.hooks_active = source["hooks_active"];
	        this.hooks_phases = source["hooks_phases"];
	        this.hooks_command_count = source["hooks_command_count"];
	        this.hooks_fail_on_error = source["hooks_fail_on_error"];
	        this.last_summary = source["last_summary"];
	        this.remote_host = source["remote_host"];
	        this.remote_user = source["remote_user"];
	        this.remote_port = source["remote_port"];
	        this.remote_work_dir = source["remote_work_dir"];
	        this.remote_safety = source["remote_safety"];
	        this.remote_session_id = source["remote_session_id"];
	        this.message = source["message"];
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
	        this.labels = source["labels"];
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
	export class DigitalEmployeeFeatureStatus {
	    authorization?: corelib.DigitalEmployeeAuthorization;
	    actual_count: number;
	    visible: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new DigitalEmployeeFeatureStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authorization = this.convertValues(source["authorization"], corelib.DigitalEmployeeAuthorization);
	        this.actual_count = source["actual_count"];
	        this.visible = source["visible"];
	        this.reason = source["reason"];
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
	export class DirectoryItem {
	    instance_id: string;
	    workflow_name: string;
	    status: string;
	    current_node?: string;
	    initiator_name?: string;
	    initiated_at: string;
	    completed_at?: string;
	    result?: string;
	    user_role?: string;
	    urgency?: string;
	    time_remaining_hours?: number;
	    confirm_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instance_id = source["instance_id"];
	        this.workflow_name = source["workflow_name"];
	        this.status = source["status"];
	        this.current_node = source["current_node"];
	        this.initiator_name = source["initiator_name"];
	        this.initiated_at = source["initiated_at"];
	        this.completed_at = source["completed_at"];
	        this.result = source["result"];
	        this.user_role = source["user_role"];
	        this.urgency = source["urgency"];
	        this.time_remaining_hours = source["time_remaining_hours"];
	        this.confirm_type = source["confirm_type"];
	    }
	}
	export class DirectoryResponse {
	    items: DirectoryItem[];
	    total: number;
	    page: number;
	    page_size: number;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], DirectoryItem);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.page_size = source["page_size"];
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
	
	export class ExperienceReviewAffordanceInput {
	    name: string;
	    label: string;
	    type: string;
	    required: boolean;
	    placeholder?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceReviewAffordanceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.placeholder = source["placeholder"];
	    }
	}
	export class ExperienceReviewAffordance {
	    id: string;
	    label: string;
	    intent: string;
	    variant?: string;
	    description?: string;
	    required_inputs?: ExperienceReviewAffordanceInput[];
	    tool_call?: Record<string, any>;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceReviewAffordance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.intent = source["intent"];
	        this.variant = source["variant"];
	        this.description = source["description"];
	        this.required_inputs = this.convertValues(source["required_inputs"], ExperienceReviewAffordanceInput);
	        this.tool_call = source["tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	export class ExperienceApprovedSkillDraftReviewSummary {
	    trace_id: string;
	    title?: string;
	    draft_id: string;
	    execution_status?: string;
	    execution_at?: string;
	    execution_note?: string;
	    stale?: boolean;
	    stale_days?: number;
	    stale_recommendation?: string;
	    source_trace_id?: string;
	    latest_status?: string;
	    latest_note?: string;
	    latest_updated_at?: string;
	    execution_affordances?: ExperienceReviewAffordance[];
	
	    static createFrom(source: any = {}) {
	        return new ExperienceApprovedSkillDraftReviewSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.title = source["title"];
	        this.draft_id = source["draft_id"];
	        this.execution_status = source["execution_status"];
	        this.execution_at = source["execution_at"];
	        this.execution_note = source["execution_note"];
	        this.stale = source["stale"];
	        this.stale_days = source["stale_days"];
	        this.stale_recommendation = source["stale_recommendation"];
	        this.source_trace_id = source["source_trace_id"];
	        this.latest_status = source["latest_status"];
	        this.latest_note = source["latest_note"];
	        this.latest_updated_at = source["latest_updated_at"];
	        this.execution_affordances = this.convertValues(source["execution_affordances"], ExperienceReviewAffordance);
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
	export class ExperienceBlockedSkillDraft {
	    trace_id: string;
	    kind: string;
	    title: string;
	    source_trace_id?: string;
	    draft_id: string;
	    execution_status: string;
	    execution_note?: string;
	    current_plan_matched: boolean;
	    current_plan_actions: any[];
	    review_options?: Record<string, any>;
	    review_affordances?: ExperienceReviewAffordance[];
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    draft_markdown: string;
	    checks: string[];
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceBlockedSkillDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source_trace_id = source["source_trace_id"];
	        this.draft_id = source["draft_id"];
	        this.execution_status = source["execution_status"];
	        this.execution_note = source["execution_note"];
	        this.current_plan_matched = source["current_plan_matched"];
	        this.current_plan_actions = source["current_plan_actions"];
	        this.review_options = source["review_options"];
	        this.review_affordances = this.convertValues(source["review_affordances"], ExperienceReviewAffordance);
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.draft_markdown = source["draft_markdown"];
	        this.checks = source["checks"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	export class ExperienceConflictReconciliationDraft {
	    trace_id: string;
	    kind: string;
	    title: string;
	    source_url?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    topic?: string;
	    question?: string;
	    new_discussion?: string;
	    new_summary?: string;
	    existing_memory?: string;
	    existing_summary?: string;
	    draft_markdown: string;
	    checks: string[];
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceConflictReconciliationDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source_url = source["source_url"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.topic = source["topic"];
	        this.question = source["question"];
	        this.new_discussion = source["new_discussion"];
	        this.new_summary = source["new_summary"];
	        this.existing_memory = source["existing_memory"];
	        this.existing_summary = source["existing_summary"];
	        this.draft_markdown = source["draft_markdown"];
	        this.checks = source["checks"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceDraftReviewRecord {
	    trace_id: string;
	    memory_id: string;
	    kind: string;
	    status: string;
	    source_trace_id?: string;
	    draft_id?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceDraftReviewRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.memory_id = source["memory_id"];
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.source_trace_id = source["source_trace_id"];
	        this.draft_id = source["draft_id"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceDraftReviewRequest {
	    kind: string;
	    status: string;
	    source_trace_id?: string;
	    draft_id?: string;
	    query?: string;
	    note?: string;
	    actor?: string;
	    draft_markdown?: string;
	    non_executing_boundary?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceDraftReviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.source_trace_id = source["source_trace_id"];
	        this.draft_id = source["draft_id"];
	        this.query = source["query"];
	        this.note = source["note"];
	        this.actor = source["actor"];
	        this.draft_markdown = source["draft_markdown"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceEscalationBrief {
	    trace_id: string;
	    kind: string;
	    title: string;
	    source_url?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    reason?: string;
	    target?: string;
	    raised_by?: string;
	    brief_markdown: string;
	    checks: string[];
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceEscalationBrief(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source_url = source["source_url"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.reason = source["reason"];
	        this.target = source["target"];
	        this.raised_by = source["raised_by"];
	        this.brief_markdown = source["brief_markdown"];
	        this.checks = source["checks"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceExtractor {
	
	
	    static createFrom(source: any = {}) {
	        return new ExperienceExtractor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ExperienceTraceDetail {
	    id: string;
	    kind: string;
	    title: string;
	    summary?: string;
	    detail?: string;
	    source_type?: string;
	    source_url?: string;
	    source_trace_id?: string;
	    draft_id?: string;
	    draft_execution_status?: string;
	    draft_execution_at?: string;
	    draft_execution_note?: string;
	    tags?: string[];
	    evidence?: number;
	    confidence?: number;
	    impact?: string;
	    review_required?: boolean;
	    review_action?: string;
	    review_status?: string;
	    next_action_kind?: string;
	    next_action?: string;
	    reviewed_at?: string;
	    reviewer?: string;
	    review_note?: string;
	    review_count?: number;
	    follow_up_status?: string;
	    follow_up_action_kind?: string;
	    follow_up_at?: string;
	    follow_up_actor?: string;
	    follow_up_note?: string;
	    follow_up_count?: number;
	    updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.detail = source["detail"];
	        this.source_type = source["source_type"];
	        this.source_url = source["source_url"];
	        this.source_trace_id = source["source_trace_id"];
	        this.draft_id = source["draft_id"];
	        this.draft_execution_status = source["draft_execution_status"];
	        this.draft_execution_at = source["draft_execution_at"];
	        this.draft_execution_note = source["draft_execution_note"];
	        this.tags = source["tags"];
	        this.evidence = source["evidence"];
	        this.confidence = source["confidence"];
	        this.impact = source["impact"];
	        this.review_required = source["review_required"];
	        this.review_action = source["review_action"];
	        this.review_status = source["review_status"];
	        this.next_action_kind = source["next_action_kind"];
	        this.next_action = source["next_action"];
	        this.reviewed_at = source["reviewed_at"];
	        this.reviewer = source["reviewer"];
	        this.review_note = source["review_note"];
	        this.review_count = source["review_count"];
	        this.follow_up_status = source["follow_up_status"];
	        this.follow_up_action_kind = source["follow_up_action_kind"];
	        this.follow_up_at = source["follow_up_at"];
	        this.follow_up_actor = source["follow_up_actor"];
	        this.follow_up_note = source["follow_up_note"];
	        this.follow_up_count = source["follow_up_count"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ExperienceFollowUpActionSummary {
	    kind: string;
	    count: number;
	    status_counts?: Record<string, number>;
	    triggered_rollback?: boolean;
	    triggered_count?: number;
	    latest_trace_id?: string;
	    latest_title?: string;
	    recommended_trace_id?: string;
	    recommended_title?: string;
	    recommended_reason?: string;
	    latest_status?: string;
	    latest_note?: string;
	    latest_updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceFollowUpActionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.count = source["count"];
	        this.status_counts = source["status_counts"];
	        this.triggered_rollback = source["triggered_rollback"];
	        this.triggered_count = source["triggered_count"];
	        this.latest_trace_id = source["latest_trace_id"];
	        this.latest_title = source["latest_title"];
	        this.recommended_trace_id = source["recommended_trace_id"];
	        this.recommended_title = source["recommended_title"];
	        this.recommended_reason = source["recommended_reason"];
	        this.latest_status = source["latest_status"];
	        this.latest_note = source["latest_note"];
	        this.latest_updated_at = source["latest_updated_at"];
	    }
	}
	export class ExperienceFollowUpSummary {
	    status: string;
	    count: number;
	    triggered_rollback?: boolean;
	    triggered_count?: number;
	    latest_trace_id?: string;
	    latest_title?: string;
	    recommended_trace_id?: string;
	    recommended_title?: string;
	    recommended_reason?: string;
	    latest_action_kind?: string;
	    latest_note?: string;
	    latest_updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceFollowUpSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.count = source["count"];
	        this.triggered_rollback = source["triggered_rollback"];
	        this.triggered_count = source["triggered_count"];
	        this.latest_trace_id = source["latest_trace_id"];
	        this.latest_title = source["latest_title"];
	        this.recommended_trace_id = source["recommended_trace_id"];
	        this.recommended_title = source["recommended_title"];
	        this.recommended_reason = source["recommended_reason"];
	        this.latest_action_kind = source["latest_action_kind"];
	        this.latest_note = source["latest_note"];
	        this.latest_updated_at = source["latest_updated_at"];
	    }
	}
	export class ExperienceTraceDetailQuery {
	    filter?: string;
	    review_status?: string;
	    action_kind?: string;
	    follow_up_status?: string;
	    follow_up_action_kind?: string;
	    triggered_rollback_only?: boolean;
	    kind?: string;
	    source_type?: string;
	    trace_id?: string;
	    source_trace_id?: string;
	    query?: string;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceDetailQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filter = source["filter"];
	        this.review_status = source["review_status"];
	        this.action_kind = source["action_kind"];
	        this.follow_up_status = source["follow_up_status"];
	        this.follow_up_action_kind = source["follow_up_action_kind"];
	        this.triggered_rollback_only = source["triggered_rollback_only"];
	        this.kind = source["kind"];
	        this.source_type = source["source_type"];
	        this.trace_id = source["trace_id"];
	        this.source_trace_id = source["source_trace_id"];
	        this.query = source["query"];
	        this.limit = source["limit"];
	    }
	}
	export class ExperienceFollowUpActionAuditResult {
	    query: ExperienceTraceDetailQuery;
	    total: number;
	    count: number;
	    returned: number;
	    recommended_trace_id?: string;
	    recommended_trace_title?: string;
	    recommended_reason?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    follow_up_status_counts: Record<string, number>;
	    follow_up_action_counts: Record<string, number>;
	    follow_up_summaries: ExperienceFollowUpSummary[];
	    follow_up_action_summaries: ExperienceFollowUpActionSummary[];
	    follow_up_details: ExperienceTraceDetail[];
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceFollowUpActionAuditResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceTraceDetailQuery);
	        this.total = source["total"];
	        this.count = source["count"];
	        this.returned = source["returned"];
	        this.recommended_trace_id = source["recommended_trace_id"];
	        this.recommended_trace_title = source["recommended_trace_title"];
	        this.recommended_reason = source["recommended_reason"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.follow_up_status_counts = source["follow_up_status_counts"];
	        this.follow_up_action_counts = source["follow_up_action_counts"];
	        this.follow_up_summaries = this.convertValues(source["follow_up_summaries"], ExperienceFollowUpSummary);
	        this.follow_up_action_summaries = this.convertValues(source["follow_up_action_summaries"], ExperienceFollowUpActionSummary);
	        this.follow_up_details = this.convertValues(source["follow_up_details"], ExperienceTraceDetail);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	
	export class ExperienceToolRecoverySummary {
	    tool_name: string;
	    category: string;
	    trace_id: string;
	    title: string;
	    action?: string;
	    provider_name?: string;
	    model?: string;
	    wire_api?: string;
	    failure_count?: number;
	    reviewed_failure_count?: number;
	    review_required?: boolean;
	    review_status?: string;
	    disabled?: boolean;
	    first_observed_at?: string;
	    last_observed_at?: string;
	    updated_at?: string;
	    tags?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExperienceToolRecoverySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_name = source["tool_name"];
	        this.category = source["category"];
	        this.trace_id = source["trace_id"];
	        this.title = source["title"];
	        this.action = source["action"];
	        this.provider_name = source["provider_name"];
	        this.model = source["model"];
	        this.wire_api = source["wire_api"];
	        this.failure_count = source["failure_count"];
	        this.reviewed_failure_count = source["reviewed_failure_count"];
	        this.review_required = source["review_required"];
	        this.review_status = source["review_status"];
	        this.disabled = source["disabled"];
	        this.first_observed_at = source["first_observed_at"];
	        this.last_observed_at = source["last_observed_at"];
	        this.updated_at = source["updated_at"];
	        this.tags = source["tags"];
	    }
	}
	export class ExperienceSkillDraftReviewQueues {
	    approved_unpreviewed: ExperienceApprovedSkillDraftReviewSummary[];
	    previewed_waiting_confirm: ExperienceApprovedSkillDraftReviewSummary[];
	    applied: ExperienceApprovedSkillDraftReviewSummary[];
	    blocked: ExperienceApprovedSkillDraftReviewSummary[];
	    reopened: ExperienceApprovedSkillDraftReviewSummary[];
	    closed: ExperienceApprovedSkillDraftReviewSummary[];
	
	    static createFrom(source: any = {}) {
	        return new ExperienceSkillDraftReviewQueues(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.approved_unpreviewed = this.convertValues(source["approved_unpreviewed"], ExperienceApprovedSkillDraftReviewSummary);
	        this.previewed_waiting_confirm = this.convertValues(source["previewed_waiting_confirm"], ExperienceApprovedSkillDraftReviewSummary);
	        this.applied = this.convertValues(source["applied"], ExperienceApprovedSkillDraftReviewSummary);
	        this.blocked = this.convertValues(source["blocked"], ExperienceApprovedSkillDraftReviewSummary);
	        this.reopened = this.convertValues(source["reopened"], ExperienceApprovedSkillDraftReviewSummary);
	        this.closed = this.convertValues(source["closed"], ExperienceApprovedSkillDraftReviewSummary);
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
	export class ExperienceNextActionSummary {
	    kind: string;
	    count: number;
	    latest_trace_id?: string;
	    latest_title?: string;
	    latest_action?: string;
	    latest_updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceNextActionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.count = source["count"];
	        this.latest_trace_id = source["latest_trace_id"];
	        this.latest_title = source["latest_title"];
	        this.latest_action = source["latest_action"];
	        this.latest_updated_at = source["latest_updated_at"];
	    }
	}
	export class ExperienceReviewSummary {
	    status: string;
	    count: number;
	    required_count?: number;
	    latest_trace_id?: string;
	    latest_title?: string;
	    latest_kind?: string;
	    latest_action?: string;
	    latest_reviewer?: string;
	    latest_note?: string;
	    latest_reviewed_at?: string;
	    latest_updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceReviewSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.count = source["count"];
	        this.required_count = source["required_count"];
	        this.latest_trace_id = source["latest_trace_id"];
	        this.latest_title = source["latest_title"];
	        this.latest_kind = source["latest_kind"];
	        this.latest_action = source["latest_action"];
	        this.latest_reviewer = source["latest_reviewer"];
	        this.latest_note = source["latest_note"];
	        this.latest_reviewed_at = source["latest_reviewed_at"];
	        this.latest_updated_at = source["latest_updated_at"];
	    }
	}
	export class ExperienceLearningSnapshot {
	    routing_hints: tool.ToolRoutingHint[];
	    skill_nudge_candidates: tool.ToolSkillNudgeCandidate[];
	    recovery_patterns: tool.ToolRecoveryPattern[];
	    usage_patterns: tool.UsagePattern[];
	    trace_details: ExperienceTraceDetail[];
	    memory_experience?: memory.ExperienceDistillResult;
	    trace_kind_counts: Record<string, number>;
	    trace_source_counts: Record<string, number>;
	    review_status_counts: Record<string, number>;
	    next_action_kind_counts: Record<string, number>;
	    follow_up_status_counts: Record<string, number>;
	    follow_up_action_kind_counts: Record<string, number>;
	    review_summaries: ExperienceReviewSummary[];
	    next_action_summaries: ExperienceNextActionSummary[];
	    follow_up_summaries: ExperienceFollowUpSummary[];
	    follow_up_action_summaries: ExperienceFollowUpActionSummary[];
	    approved_skill_draft_reviews: ExperienceApprovedSkillDraftReviewSummary[];
	    skill_draft_review_queues: ExperienceSkillDraftReviewQueues;
	    tool_recovery_summaries: ExperienceToolRecoverySummary[];
	    trace_detail_count: number;
	    routing_hint_count: number;
	    skill_nudge_count: number;
	    recovery_pattern_count: number;
	    usage_pattern_count: number;
	    protected_memory_count: number;
	    review_required_trace_count: number;
	    next_action_trace_count: number;
	    follow_up_trace_count: number;
	    approved_skill_draft_review_count: number;
	    blocked_skill_draft_review_count: number;
	    reopened_skill_draft_review_count: number;
	    closed_skill_draft_review_count: number;
	    stale_blocked_skill_draft_review_count: number;
	    layered_memory_recommended: boolean;
	    layered_memory_reason?: string;
	    memory_maintenance_recommendation?: string;
	    memory_maintenance_boundary?: string;
	    skill_maintenance_hints: skill.MaintenanceExperienceHint[];
	    skill_maintenance_hint_count: number;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceLearningSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.routing_hints = this.convertValues(source["routing_hints"], tool.ToolRoutingHint);
	        this.skill_nudge_candidates = this.convertValues(source["skill_nudge_candidates"], tool.ToolSkillNudgeCandidate);
	        this.recovery_patterns = this.convertValues(source["recovery_patterns"], tool.ToolRecoveryPattern);
	        this.usage_patterns = this.convertValues(source["usage_patterns"], tool.UsagePattern);
	        this.trace_details = this.convertValues(source["trace_details"], ExperienceTraceDetail);
	        this.memory_experience = this.convertValues(source["memory_experience"], memory.ExperienceDistillResult);
	        this.trace_kind_counts = source["trace_kind_counts"];
	        this.trace_source_counts = source["trace_source_counts"];
	        this.review_status_counts = source["review_status_counts"];
	        this.next_action_kind_counts = source["next_action_kind_counts"];
	        this.follow_up_status_counts = source["follow_up_status_counts"];
	        this.follow_up_action_kind_counts = source["follow_up_action_kind_counts"];
	        this.review_summaries = this.convertValues(source["review_summaries"], ExperienceReviewSummary);
	        this.next_action_summaries = this.convertValues(source["next_action_summaries"], ExperienceNextActionSummary);
	        this.follow_up_summaries = this.convertValues(source["follow_up_summaries"], ExperienceFollowUpSummary);
	        this.follow_up_action_summaries = this.convertValues(source["follow_up_action_summaries"], ExperienceFollowUpActionSummary);
	        this.approved_skill_draft_reviews = this.convertValues(source["approved_skill_draft_reviews"], ExperienceApprovedSkillDraftReviewSummary);
	        this.skill_draft_review_queues = this.convertValues(source["skill_draft_review_queues"], ExperienceSkillDraftReviewQueues);
	        this.tool_recovery_summaries = this.convertValues(source["tool_recovery_summaries"], ExperienceToolRecoverySummary);
	        this.trace_detail_count = source["trace_detail_count"];
	        this.routing_hint_count = source["routing_hint_count"];
	        this.skill_nudge_count = source["skill_nudge_count"];
	        this.recovery_pattern_count = source["recovery_pattern_count"];
	        this.usage_pattern_count = source["usage_pattern_count"];
	        this.protected_memory_count = source["protected_memory_count"];
	        this.review_required_trace_count = source["review_required_trace_count"];
	        this.next_action_trace_count = source["next_action_trace_count"];
	        this.follow_up_trace_count = source["follow_up_trace_count"];
	        this.approved_skill_draft_review_count = source["approved_skill_draft_review_count"];
	        this.blocked_skill_draft_review_count = source["blocked_skill_draft_review_count"];
	        this.reopened_skill_draft_review_count = source["reopened_skill_draft_review_count"];
	        this.closed_skill_draft_review_count = source["closed_skill_draft_review_count"];
	        this.stale_blocked_skill_draft_review_count = source["stale_blocked_skill_draft_review_count"];
	        this.layered_memory_recommended = source["layered_memory_recommended"];
	        this.layered_memory_reason = source["layered_memory_reason"];
	        this.memory_maintenance_recommendation = source["memory_maintenance_recommendation"];
	        this.memory_maintenance_boundary = source["memory_maintenance_boundary"];
	        this.skill_maintenance_hints = this.convertValues(source["skill_maintenance_hints"], skill.MaintenanceExperienceHint);
	        this.skill_maintenance_hint_count = source["skill_maintenance_hint_count"];
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
	export class ExperienceMemoryCandidateQuery {
	    reason?: string;
	    source?: string;
	    query?: string;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceMemoryCandidateQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reason = source["reason"];
	        this.source = source["source"];
	        this.query = source["query"];
	        this.limit = source["limit"];
	    }
	}
	export class ExperienceMemoryCandidateResult {
	    query: ExperienceMemoryCandidateQuery;
	    scanned_entries: number;
	    active_entries: number;
	    total: number;
	    count: number;
	    returned: number;
	    reason_counts: Record<string, number>;
	    source_counts: Record<string, number>;
	    layered_recommended: boolean;
	    layered_reason?: string;
	    maintenance_recommendation: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary: string;
	    candidates: memory.ProtectedExperienceCandidate[];
	
	    static createFrom(source: any = {}) {
	        return new ExperienceMemoryCandidateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceMemoryCandidateQuery);
	        this.scanned_entries = source["scanned_entries"];
	        this.active_entries = source["active_entries"];
	        this.total = source["total"];
	        this.count = source["count"];
	        this.returned = source["returned"];
	        this.reason_counts = source["reason_counts"];
	        this.source_counts = source["source_counts"];
	        this.layered_recommended = source["layered_recommended"];
	        this.layered_reason = source["layered_reason"];
	        this.maintenance_recommendation = source["maintenance_recommendation"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	        this.candidates = this.convertValues(source["candidates"], memory.ProtectedExperienceCandidate);
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
	export class ExperienceMemoryMaintenanceDraft {
	    query: ExperienceMemoryCandidateQuery;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    layered_recommended: boolean;
	    layered_reason?: string;
	    maintenance_recommendation: string;
	    protected_total: number;
	    protected_returned: number;
	    reason_counts?: Record<string, number>;
	    source_counts?: Record<string, number>;
	    retention_anchors: memory.ProtectedExperienceCandidate[];
	    checks: string[];
	    draft_markdown: string;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceMemoryMaintenanceDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceMemoryCandidateQuery);
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.layered_recommended = source["layered_recommended"];
	        this.layered_reason = source["layered_reason"];
	        this.maintenance_recommendation = source["maintenance_recommendation"];
	        this.protected_total = source["protected_total"];
	        this.protected_returned = source["protected_returned"];
	        this.reason_counts = source["reason_counts"];
	        this.source_counts = source["source_counts"];
	        this.retention_anchors = this.convertValues(source["retention_anchors"], memory.ProtectedExperienceCandidate);
	        this.checks = source["checks"];
	        this.draft_markdown = source["draft_markdown"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	
	
	
	export class ExperienceRollbackWorkflowDraft {
	    trace_id: string;
	    kind: string;
	    title: string;
	    source_url?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    decision_summary?: string;
	    decision_rationale?: string;
	    rollback_triggers: string[];
	    draft_markdown: string;
	    checks: string[];
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceRollbackWorkflowDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source_url = source["source_url"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.decision_summary = source["decision_summary"];
	        this.decision_rationale = source["decision_rationale"];
	        this.rollback_triggers = source["rollback_triggers"];
	        this.draft_markdown = source["draft_markdown"];
	        this.checks = source["checks"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceRoutingToolCandidate {
	    tool_name: string;
	    adjustment: number;
	    direction: string;
	    reasons?: string[];
	    recommendation: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceRoutingToolCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_name = source["tool_name"];
	        this.adjustment = source["adjustment"];
	        this.direction = source["direction"];
	        this.reasons = source["reasons"];
	        this.recommendation = source["recommendation"];
	    }
	}
	export class ExperienceRoutingSignalQuery {
	    task_type?: string;
	    tool?: string;
	    query?: string;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceRoutingSignalQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_type = source["task_type"];
	        this.tool = source["tool"];
	        this.query = source["query"];
	        this.limit = source["limit"];
	    }
	}
	export class ExperienceRoutingAdjustmentDraft {
	    query: ExperienceRoutingSignalQuery;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    counts: Record<string, number>;
	    returned: Record<string, number>;
	    tool_candidates: ExperienceRoutingToolCandidate[];
	    score_adjustments: tool.RoutingHintAdjustmentExplanation[];
	    routing_recommendation: string;
	    checks: string[];
	    draft_markdown: string;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceRoutingAdjustmentDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceRoutingSignalQuery);
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.counts = source["counts"];
	        this.returned = source["returned"];
	        this.tool_candidates = this.convertValues(source["tool_candidates"], ExperienceRoutingToolCandidate);
	        this.score_adjustments = this.convertValues(source["score_adjustments"], tool.RoutingHintAdjustmentExplanation);
	        this.routing_recommendation = source["routing_recommendation"];
	        this.checks = source["checks"];
	        this.draft_markdown = source["draft_markdown"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	export class ExperienceRoutingSignalResult {
	    query: ExperienceRoutingSignalQuery;
	    counts: Record<string, number>;
	    returned: Record<string, number>;
	    routing_hints: tool.ToolRoutingHint[];
	    recovery_patterns: tool.ToolRecoveryPattern[];
	    skill_nudge_candidates: tool.ToolSkillNudgeCandidate[];
	    usage_patterns: tool.UsagePattern[];
	    score_adjustments: tool.RoutingHintAdjustmentExplanation[];
	    tool_candidates: ExperienceRoutingToolCandidate[];
	    routing_recommendation: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceRoutingSignalResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceRoutingSignalQuery);
	        this.counts = source["counts"];
	        this.returned = source["returned"];
	        this.routing_hints = this.convertValues(source["routing_hints"], tool.ToolRoutingHint);
	        this.recovery_patterns = this.convertValues(source["recovery_patterns"], tool.ToolRecoveryPattern);
	        this.skill_nudge_candidates = this.convertValues(source["skill_nudge_candidates"], tool.ToolSkillNudgeCandidate);
	        this.usage_patterns = this.convertValues(source["usage_patterns"], tool.UsagePattern);
	        this.score_adjustments = this.convertValues(source["score_adjustments"], tool.RoutingHintAdjustmentExplanation);
	        this.tool_candidates = this.convertValues(source["tool_candidates"], ExperienceRoutingToolCandidate);
	        this.routing_recommendation = source["routing_recommendation"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	export class ExperienceSkillDraft {
	    trace_id: string;
	    kind: string;
	    title: string;
	    source_url?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    suggested_name: string;
	    task_type?: string;
	    query_tokens?: string[];
	    tool_sequence: string[];
	    evidence?: string;
	    description?: string;
	    draft_markdown: string;
	    checks: string[];
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceSkillDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source_url = source["source_url"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.suggested_name = source["suggested_name"];
	        this.task_type = source["task_type"];
	        this.query_tokens = source["query_tokens"];
	        this.tool_sequence = source["tool_sequence"];
	        this.evidence = source["evidence"];
	        this.description = source["description"];
	        this.draft_markdown = source["draft_markdown"];
	        this.checks = source["checks"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	
	export class ExperienceToolRecoveryQuery {
	    tool?: string;
	    category?: string;
	    review_only?: boolean;
	    provider?: string;
	    model?: string;
	    wire_api?: string;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceToolRecoveryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.category = source["category"];
	        this.review_only = source["review_only"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.wire_api = source["wire_api"];
	        this.limit = source["limit"];
	    }
	}
	export class ExperienceToolRecoveryQueryResult {
	    query: ExperienceToolRecoveryQuery;
	    count: number;
	    returned: number;
	    review_required_count: number;
	    disabled_count: number;
	    tool_counts: Record<string, number>;
	    provider_counts?: Record<string, number>;
	    model_counts?: Record<string, number>;
	    wire_api_counts?: Record<string, number>;
	    category_counts: Record<string, number>;
	    summaries: ExperienceToolRecoverySummary[];
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceToolRecoveryQueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceToolRecoveryQuery);
	        this.count = source["count"];
	        this.returned = source["returned"];
	        this.review_required_count = source["review_required_count"];
	        this.disabled_count = source["disabled_count"];
	        this.tool_counts = source["tool_counts"];
	        this.provider_counts = source["provider_counts"];
	        this.model_counts = source["model_counts"];
	        this.wire_api_counts = source["wire_api_counts"];
	        this.category_counts = source["category_counts"];
	        this.summaries = this.convertValues(source["summaries"], ExperienceToolRecoverySummary);
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	
	
	export class ExperienceTraceDetailQueryResult {
	    query: ExperienceTraceDetailQuery;
	    total: number;
	    count: number;
	    returned: number;
	    recommended_trace_id?: string;
	    recommended_trace_title?: string;
	    recommended_reason?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary: string;
	    details: ExperienceTraceDetail[];
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceDetailQueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = this.convertValues(source["query"], ExperienceTraceDetailQuery);
	        this.total = source["total"];
	        this.count = source["count"];
	        this.returned = source["returned"];
	        this.recommended_trace_id = source["recommended_trace_id"];
	        this.recommended_trace_title = source["recommended_trace_title"];
	        this.recommended_reason = source["recommended_reason"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	        this.details = this.convertValues(source["details"], ExperienceTraceDetail);
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
	export class ExperienceTraceFollowUpDraft {
	    trace_id: string;
	    kind: string;
	    title: string;
	    source_url?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary?: string;
	    action_kind: string;
	    action: string;
	    draft_title: string;
	    draft: string;
	    checks: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceFollowUpDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.source_url = source["source_url"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	        this.action_kind = source["action_kind"];
	        this.action = source["action"];
	        this.draft_title = source["draft_title"];
	        this.draft = source["draft"];
	        this.checks = source["checks"];
	    }
	}
	export class ExperienceTraceFollowUpRecord {
	    trace_id: string;
	    memory_id: string;
	    status: string;
	    action_kind?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceFollowUpRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.memory_id = source["memory_id"];
	        this.status = source["status"];
	        this.action_kind = source["action_kind"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceTraceFollowUpRequest {
	    status: string;
	    note?: string;
	    actor?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceFollowUpRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.note = source["note"];
	        this.actor = source["actor"];
	    }
	}
	export class ExperienceTraceReviewRecord {
	    trace_id: string;
	    memory_id: string;
	    kind: string;
	    outcome: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: Record<string, any>;
	    non_executing_boundary?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceReviewRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trace_id = source["trace_id"];
	        this.memory_id = source["memory_id"];
	        this.kind = source["kind"];
	        this.outcome = source["outcome"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = source["recommended_tool_call"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class ExperienceTraceReviewRequest {
	    outcome: string;
	    note?: string;
	    reviewer?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceTraceReviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.note = source["note"];
	        this.reviewer = source["reviewer"];
	    }
	}
	export class ExpertDefinition {
	    id: string;
	    name: string;
	    description: string;
	    icon: string;
	    system_prompt: string;
	    tools: string[];
	    skills: string[];
	    builtin: boolean;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ExpertDefinition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.icon = source["icon"];
	        this.system_prompt = source["system_prompt"];
	        this.tools = source["tools"];
	        this.skills = source["skills"];
	        this.builtin = source["builtin"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ExpertPackageImportResult {
	    expert: ExpertDefinition;
	    installed_skills: string[];
	    skipped_skills: string[];
	    already_imported: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExpertPackageImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expert = this.convertValues(source["expert"], ExpertDefinition);
	        this.installed_skills = source["installed_skills"];
	        this.skipped_skills = source["skipped_skills"];
	        this.already_imported = source["already_imported"];
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
	export class ExternalSkillDirInfo {
	    path: string;
	    skill_count: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalSkillDirInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.skill_count = source["skill_count"];
	        this.error = source["error"];
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
	export class GossipPost {
	    id: string;
	    nickname: string;
	    content: string;
	    category: string;
	    score: number;
	    votes: number;
	    locked: boolean;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new GossipPost(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.nickname = source["nickname"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.score = source["score"];
	        this.votes = source["votes"];
	        this.locked = source["locked"];
	        this.created_at = source["created_at"];
	    }
	}
	export class GossipBrowseResult {
	    ok: boolean;
	    posts: GossipPost[];
	    total: number;
	    page: number;
	
	    static createFrom(source: any = {}) {
	        return new GossipBrowseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.posts = this.convertValues(source["posts"], GossipPost);
	        this.total = source["total"];
	        this.page = source["page"];
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
	export class GossipClient {
	
	
	    static createFrom(source: any = {}) {
	        return new GossipClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class GossipComment {
	    id: string;
	    nickname: string;
	    content: string;
	    rating: number;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new GossipComment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.nickname = source["nickname"];
	        this.content = source["content"];
	        this.rating = source["rating"];
	        this.created_at = source["created_at"];
	    }
	}
	export class GossipCommentResult {
	    ok: boolean;
	    comment: GossipComment;
	
	    static createFrom(source: any = {}) {
	        return new GossipCommentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.comment = this.convertValues(source["comment"], GossipComment);
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
	export class GossipCommentsResult {
	    ok: boolean;
	    comments: GossipComment[];
	    total: number;
	    page: number;
	
	    static createFrom(source: any = {}) {
	        return new GossipCommentsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.comments = this.convertValues(source["comments"], GossipComment);
	        this.total = source["total"];
	        this.page = source["page"];
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
	
	export class GossipPublishResult {
	    ok: boolean;
	    post: GossipPost;
	
	    static createFrom(source: any = {}) {
	        return new GossipPublishResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.post = this.convertValues(source["post"], GossipPost);
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
	export class GossipSnapshotResult {
	    changed: boolean;
	    posts?: GossipPost[];
	    total?: number;
	    etag?: string;
	
	    static createFrom(source: any = {}) {
	        return new GossipSnapshotResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.changed = source["changed"];
	        this.posts = this.convertValues(source["posts"], GossipPost);
	        this.total = source["total"];
	        this.etag = source["etag"];
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
	export class GroupDiscussionAttachmentDownloadResult {
	    discussion_id: string;
	    attachment_id: string;
	    filename: string;
	    local_path: string;
	    size_bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionAttachmentDownloadResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.discussion_id = source["discussion_id"];
	        this.attachment_id = source["attachment_id"];
	        this.filename = source["filename"];
	        this.local_path = source["local_path"];
	        this.size_bytes = source["size_bytes"];
	    }
	}
	export class GroupDiscussionAuthorizedStartRequest {
	    request: a2a.GroupConsultationRequest;
	    invitee_ids?: string[];
	    role?: string;
	    trusted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionAuthorizedStartRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request = this.convertValues(source["request"], a2a.GroupConsultationRequest);
	        this.invitee_ids = source["invitee_ids"];
	        this.role = source["role"];
	        this.trusted = source["trusted"];
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
	export class GroupDiscussionInviteError {
	    invitee_id: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionInviteError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.invitee_id = source["invitee_id"];
	        this.error = source["error"];
	    }
	}
	export class GroupDiscussionAuthorizedStartResult {
	    consultation: a2a.ConsultationCreateResponse;
	    invite_ids?: string[];
	    experts?: a2a.GroupProfile[];
	    invite_errors?: GroupDiscussionInviteError[];
	    partial?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionAuthorizedStartResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation = this.convertValues(source["consultation"], a2a.ConsultationCreateResponse);
	        this.invite_ids = source["invite_ids"];
	        this.experts = this.convertValues(source["experts"], a2a.GroupProfile);
	        this.invite_errors = this.convertValues(source["invite_errors"], GroupDiscussionInviteError);
	        this.partial = source["partial"];
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
	export class GroupDiscussionToolCallSuggestion {
	    tool: string;
	    args?: Record<string, any>;
	    recommended_focus_context?: Record<string, any>;
	    discussion_focus_context?: Record<string, any>;
	    non_executing: boolean;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionToolCallSuggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.args = source["args"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.discussion_focus_context = source["discussion_focus_context"];
	        this.non_executing = source["non_executing"];
	        this.non_executing_boundary = source["non_executing_boundary"];
	    }
	}
	export class GroupDiscussionEscalationRouteSuggestion {
	    consultation_id: string;
	    status?: string;
	    target: string;
	    reason: string;
	    suggested: boolean;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    triggers?: string[];
	    policy_evidence?: string[];
	    suggested_next_action_kind?: string;
	    blocking_review_count?: number;
	    decidable_proposal_count?: number;
	    existing_escalation?: a2a.Escalation;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionEscalationRouteSuggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.status = source["status"];
	        this.target = source["target"];
	        this.reason = source["reason"];
	        this.suggested = source["suggested"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.triggers = source["triggers"];
	        this.policy_evidence = source["policy_evidence"];
	        this.suggested_next_action_kind = source["suggested_next_action_kind"];
	        this.blocking_review_count = source["blocking_review_count"];
	        this.decidable_proposal_count = source["decidable_proposal_count"];
	        this.existing_escalation = this.convertValues(source["existing_escalation"], a2a.Escalation);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	export class GroupDiscussionExpertRank {
	    agent_id: string;
	    display_name?: string;
	    score: number;
	    selected?: boolean;
	    reasons?: string[];
	    matched_skills?: string[];
	    skills?: string[];
	    security_group_id?: string;
	    contribution_score?: number;
	    contribution_evidence?: number;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionExpertRank(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_id = source["agent_id"];
	        this.display_name = source["display_name"];
	        this.score = source["score"];
	        this.selected = source["selected"];
	        this.reasons = source["reasons"];
	        this.matched_skills = source["matched_skills"];
	        this.skills = source["skills"];
	        this.security_group_id = source["security_group_id"];
	        this.contribution_score = source["contribution_score"];
	        this.contribution_evidence = source["contribution_evidence"];
	    }
	}
	export class GroupDiscussionExpertRankingResult {
	    invitee_ids?: string[];
	    ranked?: GroupDiscussionExpertRank[];
	    limit: number;
	    use_cross_agent_experience: boolean;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionExpertRankingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.invitee_ids = source["invitee_ids"];
	        this.ranked = this.convertValues(source["ranked"], GroupDiscussionExpertRank);
	        this.limit = source["limit"];
	        this.use_cross_agent_experience = source["use_cross_agent_experience"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	export class GroupDiscussionWorkflowBlocker {
	    code: string;
	    severity?: string;
	    message: string;
	    proposal_id?: string;
	    proposal_ids?: string[];
	    participants?: string[];
	    count?: number;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionWorkflowBlocker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.severity = source["severity"];
	        this.message = source["message"];
	        this.proposal_id = source["proposal_id"];
	        this.proposal_ids = source["proposal_ids"];
	        this.participants = source["participants"];
	        this.count = source["count"];
	    }
	}
	export class GroupDiscussionProposalWorkflowState {
	    id: string;
	    title?: string;
	    author_id?: string;
	    status?: string;
	    review_summary: a2a.ReviewSummary;
	    review_count: number;
	    policy_satisfied: boolean;
	    blocking_reviews: boolean;
	    missing_reviewers?: string[];
	    blockers?: GroupDiscussionWorkflowBlocker[];
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionProposalWorkflowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.author_id = source["author_id"];
	        this.status = source["status"];
	        this.review_summary = this.convertValues(source["review_summary"], a2a.ReviewSummary);
	        this.review_count = source["review_count"];
	        this.policy_satisfied = source["policy_satisfied"];
	        this.blocking_reviews = source["blocking_reviews"];
	        this.missing_reviewers = source["missing_reviewers"];
	        this.blockers = this.convertValues(source["blockers"], GroupDiscussionWorkflowBlocker);
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
	export class GroupDiscussionReadiness {
	    consultation_id: string;
	    status?: string;
	    participant_count: number;
	    expected_answer_count: number;
	    answer_count: number;
	    has_result: boolean;
	    ready: boolean;
	    reason?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    non_executing_boundary?: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionReadiness(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.status = source["status"];
	        this.participant_count = source["participant_count"];
	        this.expected_answer_count = source["expected_answer_count"];
	        this.answer_count = source["answer_count"];
	        this.has_result = source["has_result"];
	        this.ready = source["ready"];
	        this.reason = source["reason"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	export class GroupDiscussionRollbackReadiness {
	    consultation_id: string;
	    has_decision: boolean;
	    proposal_id?: string;
	    decision_summary?: string;
	    decision_rationale?: string;
	    rollback_on?: string[];
	    matched_triggers?: string[];
	    unmatched_triggers?: string[];
	    evidence?: string[];
	    suggested: boolean;
	    recommended_focus_context?: Record<string, any>;
	    suggested_next_action_kind?: string;
	    suggested_next_action?: string;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionRollbackReadiness(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.has_decision = source["has_decision"];
	        this.proposal_id = source["proposal_id"];
	        this.decision_summary = source["decision_summary"];
	        this.decision_rationale = source["decision_rationale"];
	        this.rollback_on = source["rollback_on"];
	        this.matched_triggers = source["matched_triggers"];
	        this.unmatched_triggers = source["unmatched_triggers"];
	        this.evidence = source["evidence"];
	        this.suggested = source["suggested"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.suggested_next_action_kind = source["suggested_next_action_kind"];
	        this.suggested_next_action = source["suggested_next_action"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	export class GroupDiscussionStaleCleanupRequest {
	    dry_run?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionStaleCleanupRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dry_run = source["dry_run"];
	    }
	}
	export class GroupDiscussionStaleCleanupResult {
	    timeout_seconds: number;
	    dry_run: boolean;
	    stale?: a2a.HubDiscussionSummary[];
	    cancelled_ids?: string[];
	    errors?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionStaleCleanupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeout_seconds = source["timeout_seconds"];
	        this.dry_run = source["dry_run"];
	        this.stale = this.convertValues(source["stale"], a2a.HubDiscussionSummary);
	        this.cancelled_ids = source["cancelled_ids"];
	        this.errors = source["errors"];
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
	export class GroupDiscussionStatus {
	    enabled: boolean;
	    discoverable: boolean;
	    confirm_before_start: boolean;
	    allow_security_group_free_discussion: boolean;
	    invite_policy?: string;
	    security_group_id?: string;
	    context_policy?: string;
	    profile?: a2a.GroupProfile;
	    experts?: a2a.GroupProfile[];
	    discussions?: a2a.HubDiscussionSummary[];
	    active_discussion_count: number;
	    ready_discussion_count: number;
	    waiting_discussion_count: number;
	    stale_discussion_count: number;
	    pending_invites?: a2a.GroupInviteSummary[];
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    non_executing_boundary?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.discoverable = source["discoverable"];
	        this.confirm_before_start = source["confirm_before_start"];
	        this.allow_security_group_free_discussion = source["allow_security_group_free_discussion"];
	        this.invite_policy = source["invite_policy"];
	        this.security_group_id = source["security_group_id"];
	        this.context_policy = source["context_policy"];
	        this.profile = this.convertValues(source["profile"], a2a.GroupProfile);
	        this.experts = this.convertValues(source["experts"], a2a.GroupProfile);
	        this.discussions = this.convertValues(source["discussions"], a2a.HubDiscussionSummary);
	        this.active_discussion_count = source["active_discussion_count"];
	        this.ready_discussion_count = source["ready_discussion_count"];
	        this.waiting_discussion_count = source["waiting_discussion_count"];
	        this.stale_discussion_count = source["stale_discussion_count"];
	        this.pending_invites = this.convertValues(source["pending_invites"], a2a.GroupInviteSummary);
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.non_executing_boundary = source["non_executing_boundary"];
	        this.error = source["error"];
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
	export class GroupDiscussionSummarizeRequest {
	    consultation_id: string;
	    user_id?: string;
	    submit?: boolean;
	    inject?: boolean;
	    force?: boolean;
	    preview?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionSummarizeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.user_id = source["user_id"];
	        this.submit = source["submit"];
	        this.inject = source["inject"];
	        this.force = source["force"];
	        this.preview = source["preview"];
	    }
	}
	export class GroupDiscussionSummarizeResult {
	    consultation_id: string;
	    summary: string;
	    rationale?: string;
	    risks?: string[];
	    disagreements?: string[];
	    open_questions?: string[];
	    participant_contributions?: Record<string, string>;
	    confidence?: number;
	    answer_count: number;
	    used_llm: boolean;
	    submitted: boolean;
	    injected: boolean;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    non_executing_boundary?: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionSummarizeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.summary = source["summary"];
	        this.rationale = source["rationale"];
	        this.risks = source["risks"];
	        this.disagreements = source["disagreements"];
	        this.open_questions = source["open_questions"];
	        this.participant_contributions = source["participant_contributions"];
	        this.confidence = source["confidence"];
	        this.answer_count = source["answer_count"];
	        this.used_llm = source["used_llm"];
	        this.submitted = source["submitted"];
	        this.injected = source["injected"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	export class GroupDiscussionWorkflowActionDraft {
	    consultation_id: string;
	    action_kind: string;
	    title: string;
	    summary: string;
	    recommended_focus_context?: Record<string, any>;
	    suggested_next_action_kind?: string;
	    proposal_id?: string;
	    target_participants?: string[];
	    target_proposal_ids?: string[];
	    escalation_target?: string;
	    escalation_reason?: string;
	    evidence?: string[];
	    risk_boundaries?: string[];
	    checklist?: string[];
	    suggested_arguments?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    requires_confirmation: boolean;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionWorkflowActionDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.action_kind = source["action_kind"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.suggested_next_action_kind = source["suggested_next_action_kind"];
	        this.proposal_id = source["proposal_id"];
	        this.target_participants = source["target_participants"];
	        this.target_proposal_ids = source["target_proposal_ids"];
	        this.escalation_target = source["escalation_target"];
	        this.escalation_reason = source["escalation_reason"];
	        this.evidence = source["evidence"];
	        this.risk_boundaries = source["risk_boundaries"];
	        this.checklist = source["checklist"];
	        this.suggested_arguments = source["suggested_arguments"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.requires_confirmation = source["requires_confirmation"];
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	
	export class GroupDiscussionWorkflowState {
	    consultation_id: string;
	    topic?: string;
	    question?: string;
	    status?: string;
	    readiness: GroupDiscussionReadiness;
	    message_count: number;
	    proposal_count: number;
	    review_count: number;
	    open_proposal_count: number;
	    decidable_proposal_count: number;
	    blocking_review_count: number;
	    missing_answer_participants?: string[];
	    workflow_blockers?: GroupDiscussionWorkflowBlocker[];
	    has_decision: boolean;
	    has_escalation: boolean;
	    has_result: boolean;
	    decision?: a2a.Decision;
	    escalation?: a2a.Escalation;
	    proposals?: GroupDiscussionProposalWorkflowState[];
	    suggested_next_action_kind?: string;
	    suggested_next_action?: string;
	    recommended_focus_context?: Record<string, any>;
	    recommended_tool_call?: GroupDiscussionToolCallSuggestion;
	    escalation_route?: GroupDiscussionEscalationRouteSuggestion;
	    rollback_readiness?: GroupDiscussionRollbackReadiness;
	    workflow_action_draft?: GroupDiscussionWorkflowActionDraft;
	    non_executing_boundary: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupDiscussionWorkflowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consultation_id = source["consultation_id"];
	        this.topic = source["topic"];
	        this.question = source["question"];
	        this.status = source["status"];
	        this.readiness = this.convertValues(source["readiness"], GroupDiscussionReadiness);
	        this.message_count = source["message_count"];
	        this.proposal_count = source["proposal_count"];
	        this.review_count = source["review_count"];
	        this.open_proposal_count = source["open_proposal_count"];
	        this.decidable_proposal_count = source["decidable_proposal_count"];
	        this.blocking_review_count = source["blocking_review_count"];
	        this.missing_answer_participants = source["missing_answer_participants"];
	        this.workflow_blockers = this.convertValues(source["workflow_blockers"], GroupDiscussionWorkflowBlocker);
	        this.has_decision = source["has_decision"];
	        this.has_escalation = source["has_escalation"];
	        this.has_result = source["has_result"];
	        this.decision = this.convertValues(source["decision"], a2a.Decision);
	        this.escalation = this.convertValues(source["escalation"], a2a.Escalation);
	        this.proposals = this.convertValues(source["proposals"], GroupDiscussionProposalWorkflowState);
	        this.suggested_next_action_kind = source["suggested_next_action_kind"];
	        this.suggested_next_action = source["suggested_next_action"];
	        this.recommended_focus_context = source["recommended_focus_context"];
	        this.recommended_tool_call = this.convertValues(source["recommended_tool_call"], GroupDiscussionToolCallSuggestion);
	        this.escalation_route = this.convertValues(source["escalation_route"], GroupDiscussionEscalationRouteSuggestion);
	        this.rollback_readiness = this.convertValues(source["rollback_readiness"], GroupDiscussionRollbackReadiness);
	        this.workflow_action_draft = this.convertValues(source["workflow_action_draft"], GroupDiscussionWorkflowActionDraft);
	        this.non_executing_boundary = source["non_executing_boundary"];
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
	export class HubCapabilityInstallIntent {
	    capability_id: string;
	    capability_type: string;
	    version?: string;
	    source: string;
	    pricing?: string;
	    price?: Record<string, any>;
	    license?: Record<string, any>;
	    user_reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new HubCapabilityInstallIntent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.capability_id = source["capability_id"];
	        this.capability_type = source["capability_type"];
	        this.version = source["version"];
	        this.source = source["source"];
	        this.pricing = source["pricing"];
	        this.price = source["price"];
	        this.license = source["license"];
	        this.user_reason = source["user_reason"];
	    }
	}
	export class HubCapabilitySummary {
	    external?: boolean;
	    id: string;
	    capability_type: string;
	    capability_id: string;
	    display_name: string;
	    description?: string;
	    source: string;
	    status: string;
	    global_key: string;
	    current_version_key?: string;
	    metadata_json?: string;
	    package_sha256?: string;
	    package_checksum?: string;
	    package_signature?: string;
	    package_download_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new HubCapabilitySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.external = source["external"];
	        this.id = source["id"];
	        this.capability_type = source["capability_type"];
	        this.capability_id = source["capability_id"];
	        this.display_name = source["display_name"];
	        this.description = source["description"];
	        this.source = source["source"];
	        this.status = source["status"];
	        this.global_key = source["global_key"];
	        this.current_version_key = source["current_version_key"];
	        this.metadata_json = source["metadata_json"];
	        this.package_sha256 = source["package_sha256"];
	        this.package_checksum = source["package_checksum"];
	        this.package_signature = source["package_signature"];
	        this.package_download_url = source["package_download_url"];
	    }
	}
	export class HubCapabilityInstallIntentResult {
	    action: string;
	    reason?: string;
	    request_id?: string;
	    capability?: HubCapabilitySummary;
	
	    static createFrom(source: any = {}) {
	        return new HubCapabilityInstallIntentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.reason = source["reason"];
	        this.request_id = source["request_id"];
	        this.capability = this.convertValues(source["capability"], HubCapabilitySummary);
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
	export class HubCapabilityRecommendation {
	    id: string;
	    capability_ref: string;
	    capability_version_key?: string;
	    scope_json: string;
	    recommendation_reason?: string;
	    allow_user_dismiss: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HubCapabilityRecommendation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.capability_ref = source["capability_ref"];
	        this.capability_version_key = source["capability_version_key"];
	        this.scope_json = source["scope_json"];
	        this.recommendation_reason = source["recommendation_reason"];
	        this.allow_user_dismiss = source["allow_user_dismiss"];
	    }
	}
	
	export class HubEffectivePolicy {
	    file_outbound_enabled: boolean;
	    image_outbound_enabled: boolean;
	    gossip_enabled: boolean;
	    guardrail_mode: string;
	    sandbox_mode: string;
	    network_level: string;
	    network_allowlist?: string[];
	    yolo_mode_allowed: boolean;
	    smart_route_enabled: boolean;
	    skill_sources_allowed?: string[];
	    skill_sources_restricted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HubEffectivePolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_outbound_enabled = source["file_outbound_enabled"];
	        this.image_outbound_enabled = source["image_outbound_enabled"];
	        this.gossip_enabled = source["gossip_enabled"];
	        this.guardrail_mode = source["guardrail_mode"];
	        this.sandbox_mode = source["sandbox_mode"];
	        this.network_level = source["network_level"];
	        this.network_allowlist = source["network_allowlist"];
	        this.yolo_mode_allowed = source["yolo_mode_allowed"];
	        this.smart_route_enabled = source["smart_route_enabled"];
	        this.skill_sources_allowed = source["skill_sources_allowed"];
	        this.skill_sources_restricted = source["skill_sources_restricted"];
	    }
	}
	export class HubLLMPeriodUsageWindow {
	    window_start?: string;
	    window_end?: string;
	    credits_used?: number;
	
	    static createFrom(source: any = {}) {
	        return new HubLLMPeriodUsageWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.window_start = source["window_start"];
	        this.window_end = source["window_end"];
	        this.credits_used = source["credits_used"];
	    }
	}
	export class HubLLMPeriodUsage {
	    five_hour?: HubLLMPeriodUsageWindow;
	    daily?: HubLLMPeriodUsageWindow;
	    weekly?: HubLLMPeriodUsageWindow;
	    monthly?: HubLLMPeriodUsageWindow;
	
	    static createFrom(source: any = {}) {
	        return new HubLLMPeriodUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.five_hour = this.convertValues(source["five_hour"], HubLLMPeriodUsageWindow);
	        this.daily = this.convertValues(source["daily"], HubLLMPeriodUsageWindow);
	        this.weekly = this.convertValues(source["weekly"], HubLLMPeriodUsageWindow);
	        this.monthly = this.convertValues(source["monthly"], HubLLMPeriodUsageWindow);
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
	export class HubLLMPeriodLimits {
	    five_hour?: number;
	    daily?: number;
	    weekly?: number;
	    monthly?: number;
	
	    static createFrom(source: any = {}) {
	        return new HubLLMPeriodLimits(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.five_hour = source["five_hour"];
	        this.daily = source["daily"];
	        this.weekly = source["weekly"];
	        this.monthly = source["monthly"];
	    }
	}
	export class HubLLMActiveGrant {
	    id?: string;
	    service_group_id: string;
	    source: string;
	    card_id?: string;
	    card_order_id?: string;
	    starts_at: string;
	    expires_at: string;
	    active: boolean;
	    status?: string;
	    status_reason?: string;
	    credits_total?: number;
	    credits_used?: number;
	    credits_available?: number;
	    retry_after_seconds?: number;
	    retry_after_at?: string;
	    credits_remaining?: number;
	    period_limits?: HubLLMPeriodLimits;
	    period_usage?: HubLLMPeriodUsage;
	
	    static createFrom(source: any = {}) {
	        return new HubLLMActiveGrant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.service_group_id = source["service_group_id"];
	        this.source = source["source"];
	        this.card_id = source["card_id"];
	        this.card_order_id = source["card_order_id"];
	        this.starts_at = source["starts_at"];
	        this.expires_at = source["expires_at"];
	        this.active = source["active"];
	        this.status = source["status"];
	        this.status_reason = source["status_reason"];
	        this.credits_total = source["credits_total"];
	        this.credits_used = source["credits_used"];
	        this.credits_available = source["credits_available"];
	        this.retry_after_seconds = source["retry_after_seconds"];
	        this.retry_after_at = source["retry_after_at"];
	        this.credits_remaining = source["credits_remaining"];
	        this.period_limits = this.convertValues(source["period_limits"], HubLLMPeriodLimits);
	        this.period_usage = this.convertValues(source["period_usage"], HubLLMPeriodUsage);
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
	export class HubLLMAuthorizedModel {
	    name: string;
	    provider_ids?: string[];
	    service_group_ids?: string[];
	
	    static createFrom(source: any = {}) {
	        return new HubLLMAuthorizedModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider_ids = source["provider_ids"];
	        this.service_group_ids = source["service_group_ids"];
	    }
	}
	
	
	
	export class HubLLMServiceStatus {
	    active: boolean;
	    skip_llm_config: boolean;
	    auth_mode: string;
	    service_group_ids?: string[];
	    service_group_names?: string[];
	    available_models?: string[];
	    authorized_models?: HubLLMAuthorizedModel[];
	    active_grants?: HubLLMActiveGrant[];
	    credit_grants?: HubLLMActiveGrant[];
	    inactive_reasons?: string[];
	    nearest_expires_at?: string;
	    effective_expires_at?: string;
	    default_model?: string;
	    hub_llm_base_url?: string;
	    credits_total?: number;
	    credits_used?: number;
	    credits_remaining?: number;
	    credits_available?: number;
	    tokens_per_credit?: number;
	
	    static createFrom(source: any = {}) {
	        return new HubLLMServiceStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.skip_llm_config = source["skip_llm_config"];
	        this.auth_mode = source["auth_mode"];
	        this.service_group_ids = source["service_group_ids"];
	        this.service_group_names = source["service_group_names"];
	        this.available_models = source["available_models"];
	        this.authorized_models = this.convertValues(source["authorized_models"], HubLLMAuthorizedModel);
	        this.active_grants = this.convertValues(source["active_grants"], HubLLMActiveGrant);
	        this.credit_grants = this.convertValues(source["credit_grants"], HubLLMActiveGrant);
	        this.inactive_reasons = source["inactive_reasons"];
	        this.nearest_expires_at = source["nearest_expires_at"];
	        this.effective_expires_at = source["effective_expires_at"];
	        this.default_model = source["default_model"];
	        this.hub_llm_base_url = source["hub_llm_base_url"];
	        this.credits_total = source["credits_total"];
	        this.credits_used = source["credits_used"];
	        this.credits_remaining = source["credits_remaining"];
	        this.credits_available = source["credits_available"];
	        this.tokens_per_credit = source["tokens_per_credit"];
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
	export class HubMCPHubSecret {
	    id: string;
	    user_id: string;
	    mcp_server_id: string;
	    requirement_name: string;
	    secret_digest: string;
	    metadata_json: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new HubMCPHubSecret(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.user_id = source["user_id"];
	        this.mcp_server_id = source["mcp_server_id"];
	        this.requirement_name = source["requirement_name"];
	        this.secret_digest = source["secret_digest"];
	        this.metadata_json = source["metadata_json"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class HubMCPHubSecretInput {
	    mcp_server_id: string;
	    requirement_name: string;
	    secret_value: string;
	    metadata?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new HubMCPHubSecretInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mcp_server_id = source["mcp_server_id"];
	        this.requirement_name = source["requirement_name"];
	        this.secret_value = source["secret_value"];
	        this.metadata = source["metadata"];
	    }
	}
	export class HubMCPSecretBinding {
	    mcp_server_id: string;
	    requirement_name: string;
	    storage: string;
	    hub_secret_ref?: string;
	    local_secret_ref?: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new HubMCPSecretBinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mcp_server_id = source["mcp_server_id"];
	        this.requirement_name = source["requirement_name"];
	        this.storage = source["storage"];
	        this.hub_secret_ref = source["hub_secret_ref"];
	        this.local_secret_ref = source["local_secret_ref"];
	        this.status = source["status"];
	    }
	}
	export class HubMCPSecretRequirement {
	    name: string;
	    label?: string;
	    scope: string;
	    storage_policy: string;
	    required: boolean;
	    help_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new HubMCPSecretRequirement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.scope = source["scope"];
	        this.storage_policy = source["storage_policy"];
	        this.required = source["required"];
	        this.help_url = source["help_url"];
	    }
	}
	export class HubSecurityPolicy {
	    centralized_security: boolean;
	    policy?: HubEffectivePolicy;
	    skill_sources_allowed?: string[];
	    skill_sources_restricted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HubSecurityPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.centralized_security = source["centralized_security"];
	        this.policy = this.convertValues(source["policy"], HubEffectivePolicy);
	        this.skill_sources_allowed = source["skill_sources_allowed"];
	        this.skill_sources_restricted = source["skill_sources_restricted"];
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
	export class MaclawAppTestEvidence {
	    run_id?: string;
	    verified_at?: string;
	    definition_fingerprint?: string;
	    artifact_present?: boolean;
	    artifact_name?: string;
	    output_count?: number;
	    primary_result?: string;
	    result_payload?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new MaclawAppTestEvidence(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.verified_at = source["verified_at"];
	        this.definition_fingerprint = source["definition_fingerprint"];
	        this.artifact_present = source["artifact_present"];
	        this.artifact_name = source["artifact_name"];
	        this.output_count = source["output_count"];
	        this.primary_result = source["primary_result"];
	        this.result_payload = source["result_payload"];
	    }
	}
	export class HubSkillMeta {
	    id: string;
	    name: string;
	    description: string;
	    tags: string[];
	    version: string;
	    author: string;
	    trust_level: string;
	    downloads: number;
	    hub_url: string;
	    avg_rating: number;
	    rating_count: number;
	    product_kind?: string;
	    is_maclaw_app?: boolean;
	    maclaw_app_id?: string;
	    maclaw_app_name?: string;
	    maclaw_app_description?: string;
	    maclaw_app_category?: string;
	    maclaw_app_icon?: string;
	    maclaw_app_input_mode?: string;
	    maclaw_app_output_modes?: string[];
	    maclaw_app_definition_sha256?: string;
	    maclaw_app_test_evidence?: MaclawAppTestEvidence;
	    artifact_contract_required?: boolean;
	    artifact_contract_output_modes?: string[];
	    artifact_contract_presentation?: string;
	
	    static createFrom(source: any = {}) {
	        return new HubSkillMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.tags = source["tags"];
	        this.version = source["version"];
	        this.author = source["author"];
	        this.trust_level = source["trust_level"];
	        this.downloads = source["downloads"];
	        this.hub_url = source["hub_url"];
	        this.avg_rating = source["avg_rating"];
	        this.rating_count = source["rating_count"];
	        this.product_kind = source["product_kind"];
	        this.is_maclaw_app = source["is_maclaw_app"];
	        this.maclaw_app_id = source["maclaw_app_id"];
	        this.maclaw_app_name = source["maclaw_app_name"];
	        this.maclaw_app_description = source["maclaw_app_description"];
	        this.maclaw_app_category = source["maclaw_app_category"];
	        this.maclaw_app_icon = source["maclaw_app_icon"];
	        this.maclaw_app_input_mode = source["maclaw_app_input_mode"];
	        this.maclaw_app_output_modes = source["maclaw_app_output_modes"];
	        this.maclaw_app_definition_sha256 = source["maclaw_app_definition_sha256"];
	        this.maclaw_app_test_evidence = this.convertValues(source["maclaw_app_test_evidence"], MaclawAppTestEvidence);
	        this.artifact_contract_required = source["artifact_contract_required"];
	        this.artifact_contract_output_modes = source["artifact_contract_output_modes"];
	        this.artifact_contract_presentation = source["artifact_contract_presentation"];
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
	export class HubSkillUpdateInfo {
	    skill_name: string;
	    hub_skill_id: string;
	    current_version: string;
	    latest_version: string;
	    hub_url: string;
	
	    static createFrom(source: any = {}) {
	        return new HubSkillUpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skill_name = source["skill_name"];
	        this.hub_skill_id = source["hub_skill_id"];
	        this.current_version = source["current_version"];
	        this.latest_version = source["latest_version"];
	        this.hub_url = source["hub_url"];
	    }
	}
	export class HubUserRanking {
	    total_tokens: number;
	    duration_seconds: number;
	    token_rank: number;
	    duration_rank: number;
	    total_users: number;
	    period: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new HubUserRanking(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_tokens = source["total_tokens"];
	        this.duration_seconds = source["duration_seconds"];
	        this.token_rank = source["token_rank"];
	        this.duration_rank = source["duration_rank"];
	        this.total_users = source["total_users"];
	        this.period = source["period"];
	        this.error = source["error"];
	    }
	}
	export class IMResponseRecoverableSession {
	    session_id?: string;
	    tool?: string;
	    title?: string;
	    summary?: string;
	    project_path?: string;
	    status?: string;
	    exit_reason?: string;
	    resume_session_id?: string;
	    resume_count?: number;
	    last_progress?: string;
	    actions?: IMResponseAction[];
	
	    static createFrom(source: any = {}) {
	        return new IMResponseRecoverableSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.tool = source["tool"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.project_path = source["project_path"];
	        this.status = source["status"];
	        this.exit_reason = source["exit_reason"];
	        this.resume_session_id = source["resume_session_id"];
	        this.resume_count = source["resume_count"];
	        this.last_progress = source["last_progress"];
	        this.actions = this.convertValues(source["actions"], IMResponseAction);
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
	export class IMResponseUnfinishedTask {
	    slot_id?: string;
	    title?: string;
	    summary?: string;
	    project_path?: string;
	    status?: string;
	    actions?: IMResponseAction[];
	
	    static createFrom(source: any = {}) {
	        return new IMResponseUnfinishedTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slot_id = source["slot_id"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.project_path = source["project_path"];
	        this.status = source["status"];
	        this.actions = this.convertValues(source["actions"], IMResponseAction);
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
	export class IMResponseConfirmLabels {
	    title: string;
	    status: string;
	    target_paths: string;
	    planned_actions: string;
	    risk_flags: string;
	    revision_hints: string;
	
	    static createFrom(source: any = {}) {
	        return new IMResponseConfirmLabels(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.status = source["status"];
	        this.target_paths = source["target_paths"];
	        this.planned_actions = source["planned_actions"];
	        this.risk_flags = source["risk_flags"];
	        this.revision_hints = source["revision_hints"];
	    }
	}
	export class IMResponseConfirmation {
	    id: string;
	    summary: string;
	    task_type?: string;
	    target_paths?: string[];
	    planned_actions?: string[];
	    risk_flags?: string[];
	    revision_hints?: string[];
	    status?: string;
	    labels?: IMResponseConfirmLabels;
	
	    static createFrom(source: any = {}) {
	        return new IMResponseConfirmation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.summary = source["summary"];
	        this.task_type = source["task_type"];
	        this.target_paths = source["target_paths"];
	        this.planned_actions = source["planned_actions"];
	        this.risk_flags = source["risk_flags"];
	        this.revision_hints = source["revision_hints"];
	        this.status = source["status"];
	        this.labels = this.convertValues(source["labels"], IMResponseConfirmLabels);
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
	export class IMResponseAction {
	    label: string;
	    command: string;
	    style: string;
	
	    static createFrom(source: any = {}) {
	        return new IMResponseAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.command = source["command"];
	        this.style = source["style"];
	    }
	}
	export class IMResponseField {
	    label: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new IMResponseField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	    }
	}
	export class IMAgentResponse {
	    text: string;
	    reasoning?: string;
	    clear_ui?: boolean;
	    fields?: IMResponseField[];
	    actions?: IMResponseAction[];
	    confirmation?: IMResponseConfirmation;
	    unfinished_task?: IMResponseUnfinishedTask;
	    unfinished_slot?: IMResponseUnfinishedTask;
	    recoverable_session?: IMResponseRecoverableSession;
	    image_key?: string;
	    file_data?: string;
	    file_name?: string;
	    file_mime_type?: string;
	    voice_data?: string;
	    voice_file_name?: string;
	    voice_mime_type?: string;
	    local_file_path?: string;
	    local_file_paths?: string[];
	    thumbnail_base64?: string;
	    error?: string;
	    response_source?: string;
	    deferred?: boolean;
	    keep_panel?: boolean;
	    confirmed_resume?: boolean;
	    job_id?: string;
	    run_id?: string;
	    request_id?: string;
	    session_key?: string;
	    trace_status?: string;
	    trace_summary?: string;
	    trace_event_count?: number;
	    evidence_count?: number;
	    trial_reflect_summary?: string;
	    trial_reflect_status?: string;
	    trial_reflect_failures?: number;
	    input_tokens?: number;
	    output_tokens?: number;
	    total_tokens?: number;
	    cache_read_tokens?: number;
	    cache_write_tokens?: number;
	    est_cost_rmb?: number;
	    prompt_profile?: string;
	    prompt_full_tokens?: number;
	    prompt_light_tokens?: number;
	    prompt_saved_tokens?: number;
	    prompt_upgraded?: boolean;
	    prompt_ab_sample?: boolean;
	    prompt_soft_full?: boolean;
	    route_task?: string;
	    route_source?: string;
	    route_model?: string;
	    route_reason?: string;
	    route_escalated?: boolean;
	    corrections?: progress.CorrectionOption[];
	
	    static createFrom(source: any = {}) {
	        return new IMAgentResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.reasoning = source["reasoning"];
	        this.clear_ui = source["clear_ui"];
	        this.fields = this.convertValues(source["fields"], IMResponseField);
	        this.actions = this.convertValues(source["actions"], IMResponseAction);
	        this.confirmation = this.convertValues(source["confirmation"], IMResponseConfirmation);
	        this.unfinished_task = this.convertValues(source["unfinished_task"], IMResponseUnfinishedTask);
	        this.unfinished_slot = this.convertValues(source["unfinished_slot"], IMResponseUnfinishedTask);
	        this.recoverable_session = this.convertValues(source["recoverable_session"], IMResponseRecoverableSession);
	        this.image_key = source["image_key"];
	        this.file_data = source["file_data"];
	        this.file_name = source["file_name"];
	        this.file_mime_type = source["file_mime_type"];
	        this.voice_data = source["voice_data"];
	        this.voice_file_name = source["voice_file_name"];
	        this.voice_mime_type = source["voice_mime_type"];
	        this.local_file_path = source["local_file_path"];
	        this.local_file_paths = source["local_file_paths"];
	        this.thumbnail_base64 = source["thumbnail_base64"];
	        this.error = source["error"];
	        this.response_source = source["response_source"];
	        this.deferred = source["deferred"];
	        this.keep_panel = source["keep_panel"];
	        this.confirmed_resume = source["confirmed_resume"];
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.request_id = source["request_id"];
	        this.session_key = source["session_key"];
	        this.trace_status = source["trace_status"];
	        this.trace_summary = source["trace_summary"];
	        this.trace_event_count = source["trace_event_count"];
	        this.evidence_count = source["evidence_count"];
	        this.trial_reflect_summary = source["trial_reflect_summary"];
	        this.trial_reflect_status = source["trial_reflect_status"];
	        this.trial_reflect_failures = source["trial_reflect_failures"];
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.total_tokens = source["total_tokens"];
	        this.cache_read_tokens = source["cache_read_tokens"];
	        this.cache_write_tokens = source["cache_write_tokens"];
	        this.est_cost_rmb = source["est_cost_rmb"];
	        this.prompt_profile = source["prompt_profile"];
	        this.prompt_full_tokens = source["prompt_full_tokens"];
	        this.prompt_light_tokens = source["prompt_light_tokens"];
	        this.prompt_saved_tokens = source["prompt_saved_tokens"];
	        this.prompt_upgraded = source["prompt_upgraded"];
	        this.prompt_ab_sample = source["prompt_ab_sample"];
	        this.prompt_soft_full = source["prompt_soft_full"];
	        this.route_task = source["route_task"];
	        this.route_source = source["route_source"];
	        this.route_model = source["route_model"];
	        this.route_reason = source["route_reason"];
	        this.route_escalated = source["route_escalated"];
	        this.corrections = this.convertValues(source["corrections"], progress.CorrectionOption);
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
	export class IMAuditMessage {
	    id: number;
	    timestamp: string;
	    user_id: string;
	    platform: string;
	    role: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new IMAuditMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.timestamp = source["timestamp"];
	        this.user_id = source["user_id"];
	        this.platform = source["platform"];
	        this.role = source["role"];
	        this.content = source["content"];
	    }
	}
	export class IMAuditQueryResult {
	    messages: IMAuditMessage[];
	    total: number;
	    page: number;
	    page_size: number;
	
	    static createFrom(source: any = {}) {
	        return new IMAuditQueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = this.convertValues(source["messages"], IMAuditMessage);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.page_size = source["page_size"];
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
	export class IMAuditStats {
	    qq: number;
	    telegram: number;
	    weixin: number;
	    lansenger: number;
	    thirdparty: number;
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new IMAuditStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.qq = source["qq"];
	        this.telegram = source["telegram"];
	        this.weixin = source["weixin"];
	        this.lansenger = source["lansenger"];
	        this.thirdparty = source["thirdparty"];
	        this.total = source["total"];
	    }
	}
	
	
	
	
	
	
	export class IOSPWAShellRequest {
	    output_dir: string;
	    app_name: string;
	    bundle_id: string;
	    hubcenter_url: string;
	    start_url: string;
	
	    static createFrom(source: any = {}) {
	        return new IOSPWAShellRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.output_dir = source["output_dir"];
	        this.app_name = source["app_name"];
	        this.bundle_id = source["bundle_id"];
	        this.hubcenter_url = source["hubcenter_url"];
	        this.start_url = source["start_url"];
	    }
	}
	export class IOSPWAShellResult {
	    project_dir: string;
	    xcode_project_path: string;
	    readme_path: string;
	    info_plist_path: string;
	    view_controller_path: string;
	    start_url: string;
	    hubcenter_url: string;
	
	    static createFrom(source: any = {}) {
	        return new IOSPWAShellResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_dir = source["project_dir"];
	        this.xcode_project_path = source["xcode_project_path"];
	        this.readme_path = source["readme_path"];
	        this.info_plist_path = source["info_plist_path"];
	        this.view_controller_path = source["view_controller_path"];
	        this.start_url = source["start_url"];
	        this.hubcenter_url = source["hubcenter_url"];
	    }
	}
	export class ImportantEvent {
	    event_id: string;
	    session_id: string;
	    machine_id: string;
	    type: string;
	    severity: string;
	    title: string;
	    summary: string;
	    count?: number;
	    grouped?: boolean;
	    related_file?: string;
	    command?: string;
	    created_at: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportantEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event_id = source["event_id"];
	        this.session_id = source["session_id"];
	        this.machine_id = source["machine_id"];
	        this.type = source["type"];
	        this.severity = source["severity"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.count = source["count"];
	        this.grouped = source["grouped"];
	        this.related_file = source["related_file"];
	        this.command = source["command"];
	        this.created_at = source["created_at"];
	    }
	}
	export class KnowledgeHubShareDeleteRequest {
	    hub_url: string;
	    hub_token: string;
	    knowledge_id: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareDeleteRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.knowledge_id = source["knowledge_id"];
	    }
	}
	export class KnowledgeHubShareImportRequest {
	    hub_url: string;
	    hub_token: string;
	    knowledge_id: string;
	    share_link: string;
	    dry_run: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.knowledge_id = source["knowledge_id"];
	        this.share_link = source["share_link"];
	        this.dry_run = source["dry_run"];
	    }
	}
	export class KnowledgeHubShareImportResult {
	    import_status: string;
	    knowledge_id: string;
	    package_id?: string;
	    title?: string;
	    dry_run: boolean;
	    imported: number;
	    skipped: number;
	    failed: number;
	    imported_source_ids?: string[];
	    skipped_source_ids?: string[];
	    failed_source_ids?: string[];
	    retry_source_ids?: string[];
	    warnings?: string[];
	    share?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.import_status = source["import_status"];
	        this.knowledge_id = source["knowledge_id"];
	        this.package_id = source["package_id"];
	        this.title = source["title"];
	        this.dry_run = source["dry_run"];
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.imported_source_ids = source["imported_source_ids"];
	        this.skipped_source_ids = source["skipped_source_ids"];
	        this.failed_source_ids = source["failed_source_ids"];
	        this.retry_source_ids = source["retry_source_ids"];
	        this.warnings = source["warnings"];
	        this.share = source["share"];
	    }
	}
	export class KnowledgeHubShareListItem {
	    knowledge_id: string;
	    title: string;
	    description?: string;
	    visibility_scope?: string;
	    status?: string;
	    share_url?: string;
	    agent_import?: string;
	    source_count?: number;
	    view_count?: number;
	    import_count?: number;
	    created_at?: string;
	    updated_at?: string;
	    expires_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareListItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.knowledge_id = source["knowledge_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.visibility_scope = source["visibility_scope"];
	        this.status = source["status"];
	        this.share_url = source["share_url"];
	        this.agent_import = source["agent_import"];
	        this.source_count = source["source_count"];
	        this.view_count = source["view_count"];
	        this.import_count = source["import_count"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.expires_at = source["expires_at"];
	    }
	}
	export class KnowledgeHubShareListRequest {
	    hub_url: string;
	    hub_token: string;
	    limit?: number;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareListRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class KnowledgeHubShareListResult {
	    items: KnowledgeHubShareListItem[];
	    total: number;
	    offset: number;
	    limit: number;
	    hub_url: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], KnowledgeHubShareListItem);
	        this.total = source["total"];
	        this.offset = source["offset"];
	        this.limit = source["limit"];
	        this.hub_url = source["hub_url"];
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
	export class KnowledgeHubShareRequest {
	    hub_url: string;
	    hub_token: string;
	    title: string;
	    description: string;
	    visibility_scope: string;
	    visibility_users: string[];
	    ttl: string;
	    source_ids: string[];
	    redact_sensitive: boolean;
	    include_disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.visibility_scope = source["visibility_scope"];
	        this.visibility_users = source["visibility_users"];
	        this.ttl = source["ttl"];
	        this.source_ids = source["source_ids"];
	        this.redact_sensitive = source["redact_sensitive"];
	        this.include_disabled = source["include_disabled"];
	    }
	}
	export class KnowledgeHubShareResult {
	    knowledge_id: string;
	    share_url: string;
	    agent_import: string;
	    package_url?: string;
	    expires_at?: string;
	    hub_url: string;
	    source_count: number;
	    content_sources?: number;
	    warnings?: string[];
	    source_summary?: Record<string, any>;
	    raw?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.knowledge_id = source["knowledge_id"];
	        this.share_url = source["share_url"];
	        this.agent_import = source["agent_import"];
	        this.package_url = source["package_url"];
	        this.expires_at = source["expires_at"];
	        this.hub_url = source["hub_url"];
	        this.source_count = source["source_count"];
	        this.content_sources = source["content_sources"];
	        this.warnings = source["warnings"];
	        this.source_summary = source["source_summary"];
	        this.raw = source["raw"];
	    }
	}
	export class KnowledgeHubShareUpdateRequest {
	    hub_url: string;
	    hub_token: string;
	    knowledge_id: string;
	    title: string;
	    description: string;
	    visibility_scope: string;
	    visibility_users?: string[];
	    ttl?: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeHubShareUpdateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.knowledge_id = source["knowledge_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.visibility_scope = source["visibility_scope"];
	        this.visibility_users = source["visibility_users"];
	        this.ttl = source["ttl"];
	    }
	}
	export class KnowledgeImportJob {
	    id: string;
	    status: string;
	    error?: string;
	    result: knowledge.DirectoryImportResult;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeImportJob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.result = this.convertValues(source["result"], knowledge.DirectoryImportResult);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class KnowledgeSyncConflict {
	    remote_id?: string;
	    local_id?: string;
	    title?: string;
	    uri?: string;
	    conflict_key?: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSyncConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.remote_id = source["remote_id"];
	        this.local_id = source["local_id"];
	        this.title = source["title"];
	        this.uri = source["uri"];
	        this.conflict_key = source["conflict_key"];
	        this.reason = source["reason"];
	    }
	}
	export class KnowledgeSyncRequest {
	    hub_url: string;
	    hub_token: string;
	    tenant_id?: string;
	    email?: string;
	    password: string;
	    conflict_strategy?: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSyncRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.tenant_id = source["tenant_id"];
	        this.email = source["email"];
	        this.password = source["password"];
	        this.conflict_strategy = source["conflict_strategy"];
	    }
	}
	export class KnowledgeSyncResult {
	    owner_user_id?: string;
	    tenant_id?: string;
	    package_id?: string;
	    package_version?: number;
	    compressed_size_bytes?: number;
	    stored_size_bytes?: number;
	    created_at?: string;
	    updated_at?: string;
	    expires_at?: string;
	    service_status: string;
	    readonly_reason?: string;
	    limit_bytes: number;
	    retention_days?: number;
	    encryption?: Record<string, any>;
	    password_verifier?: Record<string, any>;
	    has_package: boolean;
	    message?: string;
	    import_status?: string;
	    imported?: number;
	    skipped?: number;
	    failed?: number;
	    imported_source_ids?: string[];
	    skipped_source_ids?: string[];
	    failed_source_ids?: string[];
	    retry_source_ids?: string[];
	    warnings?: string[];
	    conflicts?: KnowledgeSyncConflict[];
	    requires_resolution?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner_user_id = source["owner_user_id"];
	        this.tenant_id = source["tenant_id"];
	        this.package_id = source["package_id"];
	        this.package_version = source["package_version"];
	        this.compressed_size_bytes = source["compressed_size_bytes"];
	        this.stored_size_bytes = source["stored_size_bytes"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.expires_at = source["expires_at"];
	        this.service_status = source["service_status"];
	        this.readonly_reason = source["readonly_reason"];
	        this.limit_bytes = source["limit_bytes"];
	        this.retention_days = source["retention_days"];
	        this.encryption = source["encryption"];
	        this.password_verifier = source["password_verifier"];
	        this.has_package = source["has_package"];
	        this.message = source["message"];
	        this.import_status = source["import_status"];
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.imported_source_ids = source["imported_source_ids"];
	        this.skipped_source_ids = source["skipped_source_ids"];
	        this.failed_source_ids = source["failed_source_ids"];
	        this.retry_source_ids = source["retry_source_ids"];
	        this.warnings = source["warnings"];
	        this.conflicts = this.convertValues(source["conflicts"], KnowledgeSyncConflict);
	        this.requires_resolution = source["requires_resolution"];
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
	export class KnowledgeSyncStatus {
	    owner_user_id?: string;
	    tenant_id?: string;
	    package_id?: string;
	    package_version?: number;
	    compressed_size_bytes?: number;
	    stored_size_bytes?: number;
	    created_at?: string;
	    updated_at?: string;
	    expires_at?: string;
	    service_status: string;
	    readonly_reason?: string;
	    limit_bytes: number;
	    retention_days?: number;
	    encryption?: Record<string, any>;
	    password_verifier?: Record<string, any>;
	    has_package: boolean;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new KnowledgeSyncStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner_user_id = source["owner_user_id"];
	        this.tenant_id = source["tenant_id"];
	        this.package_id = source["package_id"];
	        this.package_version = source["package_version"];
	        this.compressed_size_bytes = source["compressed_size_bytes"];
	        this.stored_size_bytes = source["stored_size_bytes"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.expires_at = source["expires_at"];
	        this.service_status = source["service_status"];
	        this.readonly_reason = source["readonly_reason"];
	        this.limit_bytes = source["limit_bytes"];
	        this.retention_days = source["retention_days"];
	        this.encryption = source["encryption"];
	        this.password_verifier = source["password_verifier"];
	        this.has_package = source["has_package"];
	        this.message = source["message"];
	    }
	}
	export class LLMSecurityReview {
	
	
	    static createFrom(source: any = {}) {
	        return new LLMSecurityReview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class LansengerGroupListEntry {
	    group_id: string;
	    name: string;
	    avatar_url?: string;
	    description?: string;
	    owner_id?: string;
	    owner_name?: string;
	    state: number;
	    total_members: number;
	    max_members?: number;
	    is_public?: boolean;
	    ignored: boolean;
	    allowed: boolean;
	    orphan?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LansengerGroupListEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group_id = source["group_id"];
	        this.name = source["name"];
	        this.avatar_url = source["avatar_url"];
	        this.description = source["description"];
	        this.owner_id = source["owner_id"];
	        this.owner_name = source["owner_name"];
	        this.state = source["state"];
	        this.total_members = source["total_members"];
	        this.max_members = source["max_members"];
	        this.is_public = source["is_public"];
	        this.ignored = source["ignored"];
	        this.allowed = source["allowed"];
	        this.orphan = source["orphan"];
	    }
	}
	export class LansengerGroupListResult {
	    total: number;
	    groups: LansengerGroupListEntry[];
	
	    static createFrom(source: any = {}) {
	        return new LansengerGroupListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.groups = this.convertValues(source["groups"], LansengerGroupListEntry);
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
	export class LocalGroupExecutorRegistration {
	    session_id: string;
	    participant_id: string;
	    display_name: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalGroupExecutorRegistration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.participant_id = source["participant_id"];
	        this.display_name = source["display_name"];
	    }
	}
	export class LocalMCPManager {
	
	
	    static createFrom(source: any = {}) {
	        return new LocalMCPManager(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class LocalMCPServerStatus {
	    id: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LocalMCPServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.running = source["running"];
	    }
	}
	export class LocalMCPServerView {
	    id: string;
	    name: string;
	    command: string;
	    args?: string[];
	    env?: Record<string, string>;
	    disabled?: boolean;
	    auto_start?: boolean;
	    created_at: string;
	    source?: string;
	    capability?: corelib.MCPServerCapabilityRef;
	    managed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LocalMCPServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.disabled = source["disabled"];
	        this.auto_start = source["auto_start"];
	        this.created_at = source["created_at"];
	        this.source = source["source"];
	        this.capability = this.convertValues(source["capability"], corelib.MCPServerCapabilityRef);
	        this.managed = source["managed"];
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
	export class localStartMenuRemoteEnv {
	    host?: string;
	    port?: number;
	    user?: string;
	    workDir?: string;
	
	    static createFrom(source: any = {}) {
	        return new localStartMenuRemoteEnv(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.workDir = source["workDir"];
	    }
	}
	export class localStartMenuCodingEnv {
	    workingDir?: string;
	    remote?: localStartMenuRemoteEnv;
	
	    static createFrom(source: any = {}) {
	        return new localStartMenuCodingEnv(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workingDir = source["workingDir"];
	        this.remote = this.convertValues(source["remote"], localStartMenuRemoteEnv);
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
	export class LocalStartMenuTemplate {
	    title: string;
	    body: string;
	    agentMode?: string;
	    remoteSafety?: string;
	    codingEnv?: localStartMenuCodingEnv;
	
	    static createFrom(source: any = {}) {
	        return new LocalStartMenuTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.body = source["body"];
	        this.agentMode = source["agentMode"];
	        this.remoteSafety = source["remoteSafety"];
	        this.codingEnv = this.convertValues(source["codingEnv"], localStartMenuCodingEnv);
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
	export class MCPToolView {
	    name: string;
	    description: string;
	    input_schema: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new MCPToolView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.input_schema = source["input_schema"];
	    }
	}
	export class MCPEndpointTestResult {
	    success: boolean;
	    message: string;
	    tools: MCPToolView[];
	    latency_ms: number;
	
	    static createFrom(source: any = {}) {
	        return new MCPEndpointTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.tools = this.convertValues(source["tools"], MCPToolView);
	        this.latency_ms = source["latency_ms"];
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
	export class MCPRegistry {
	
	
	    static createFrom(source: any = {}) {
	        return new MCPRegistry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class MCPServerView {
	    id: string;
	    name: string;
	    endpoint_url: string;
	    auth_type: string;
	    auth_secret: string;
	    headers?: Record<string, string>;
	    source: string;
	    capability?: corelib.MCPServerCapabilityRef;
	    tools: MCPToolView[];
	    health_status: string;
	    fail_count: number;
	    // Go type: time
	    last_check_at: any;
	    // Go type: time
	    created_at: any;
	    managed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.endpoint_url = source["endpoint_url"];
	        this.auth_type = source["auth_type"];
	        this.auth_secret = source["auth_secret"];
	        this.headers = source["headers"];
	        this.source = source["source"];
	        this.capability = this.convertValues(source["capability"], corelib.MCPServerCapabilityRef);
	        this.tools = this.convertValues(source["tools"], MCPToolView);
	        this.health_status = source["health_status"];
	        this.fail_count = source["fail_count"];
	        this.last_check_at = this.convertValues(source["last_check_at"], null);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.managed = source["managed"];
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
	
	export class MDNSScanner {
	
	
	    static createFrom(source: any = {}) {
	        return new MDNSScanner(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class MISDataConnectionStatus {
	    ok: boolean;
	    auth_ok: boolean;
	    endpoint: string;
	    status?: string;
	    engine?: string;
	    schema_version?: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new MISDataConnectionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.auth_ok = source["auth_ok"];
	        this.endpoint = source["endpoint"];
	        this.status = source["status"];
	        this.engine = source["engine"];
	        this.schema_version = source["schema_version"];
	        this.error = source["error"];
	    }
	}
	export class MaclawAppApprovalDecisionInput {
	    app_id: string;
	    instance_id?: string;
	    approval_id?: string;
	    record_id?: string;
	    decision: string;
	    actor?: string;
	    note?: string;
	    open_app_view?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MaclawAppApprovalDecisionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_id = source["app_id"];
	        this.instance_id = source["instance_id"];
	        this.approval_id = source["approval_id"];
	        this.record_id = source["record_id"];
	        this.decision = source["decision"];
	        this.actor = source["actor"];
	        this.note = source["note"];
	        this.open_app_view = source["open_app_view"];
	    }
	}
	export class MaclawAppApprovalWorkflowStartInput {
	    app_id: string;
	    app_name?: string;
	    dataset_id?: string;
	    object_role?: string;
	    blueprint_id?: string;
	    record_id: string;
	    approval_id?: string;
	    instance_id?: string;
	    continue_from_instance_id?: string;
	    title?: string;
	    applicant?: string;
	    owner?: string;
	    approver?: string;
	    current_assignee?: string;
	    current_assignee_type?: string;
	    approval_event?: string;
	    workflow_skill_id?: string;
	    workflow_version?: string;
	    hub_workflow_id?: string;
	    hub_instance_id?: string;
	    hub_node_id?: string;
	    trigger_hub_workflow?: boolean;
	    current_node?: string;
	    current_node_ids?: string[];
	    workflow_node_ids?: string[];
	    business_status?: string;
	    result_status?: string;
	    from_status?: string;
	    to_status?: string;
	    business_entity?: string;
	    business_action?: string;
	    business_note?: string;
	    form_data?: Record<string, any>;
	    business_payload?: Record<string, any>;
	    result_payload?: Record<string, any>;
	    run_workflow_skill?: boolean;
	    workflow_run_args?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new MaclawAppApprovalWorkflowStartInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_id = source["app_id"];
	        this.app_name = source["app_name"];
	        this.dataset_id = source["dataset_id"];
	        this.object_role = source["object_role"];
	        this.blueprint_id = source["blueprint_id"];
	        this.record_id = source["record_id"];
	        this.approval_id = source["approval_id"];
	        this.instance_id = source["instance_id"];
	        this.continue_from_instance_id = source["continue_from_instance_id"];
	        this.title = source["title"];
	        this.applicant = source["applicant"];
	        this.owner = source["owner"];
	        this.approver = source["approver"];
	        this.current_assignee = source["current_assignee"];
	        this.current_assignee_type = source["current_assignee_type"];
	        this.approval_event = source["approval_event"];
	        this.workflow_skill_id = source["workflow_skill_id"];
	        this.workflow_version = source["workflow_version"];
	        this.hub_workflow_id = source["hub_workflow_id"];
	        this.hub_instance_id = source["hub_instance_id"];
	        this.hub_node_id = source["hub_node_id"];
	        this.trigger_hub_workflow = source["trigger_hub_workflow"];
	        this.current_node = source["current_node"];
	        this.current_node_ids = source["current_node_ids"];
	        this.workflow_node_ids = source["workflow_node_ids"];
	        this.business_status = source["business_status"];
	        this.result_status = source["result_status"];
	        this.from_status = source["from_status"];
	        this.to_status = source["to_status"];
	        this.business_entity = source["business_entity"];
	        this.business_action = source["business_action"];
	        this.business_note = source["business_note"];
	        this.form_data = source["form_data"];
	        this.business_payload = source["business_payload"];
	        this.result_payload = source["result_payload"];
	        this.run_workflow_skill = source["run_workflow_skill"];
	        this.workflow_run_args = source["workflow_run_args"];
	    }
	}
	export class MaclawAppBusinessOperationInput {
	    app_id: string;
	    app_name?: string;
	    dataset_id?: string;
	    object_role?: string;
	    blueprint_id?: string;
	    business_entity?: string;
	    business_action?: string;
	    business_note?: string;
	    preferred_action?: string;
	    preferred_view?: string;
	    preferred_report?: string;
	    preferred_dashboard?: string;
	    data?: Record<string, any>;
	    filter?: Record<string, any>;
	    limit?: number;
	    dry_run?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MaclawAppBusinessOperationInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_id = source["app_id"];
	        this.app_name = source["app_name"];
	        this.dataset_id = source["dataset_id"];
	        this.object_role = source["object_role"];
	        this.blueprint_id = source["blueprint_id"];
	        this.business_entity = source["business_entity"];
	        this.business_action = source["business_action"];
	        this.business_note = source["business_note"];
	        this.preferred_action = source["preferred_action"];
	        this.preferred_view = source["preferred_view"];
	        this.preferred_report = source["preferred_report"];
	        this.preferred_dashboard = source["preferred_dashboard"];
	        this.data = source["data"];
	        this.filter = source["filter"];
	        this.limit = source["limit"];
	        this.dry_run = source["dry_run"];
	    }
	}
	export class MaclawAppOpenWorkspaceInput {
	    app_id: string;
	    app_name?: string;
	    kind?: string;
	
	    static createFrom(source: any = {}) {
	        return new MaclawAppOpenWorkspaceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_id = source["app_id"];
	        this.app_name = source["app_name"];
	        this.kind = source["kind"];
	    }
	}
	export class MaclawAppSearchEvidence {
	    run_id?: string;
	    verified_at?: string;
	    definition_fingerprint?: string;
	    artifact_present?: boolean;
	    artifact_name?: string;
	    output_count?: number;
	    primary_result?: string;
	    result_payload?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new MaclawAppSearchEvidence(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.verified_at = source["verified_at"];
	        this.definition_fingerprint = source["definition_fingerprint"];
	        this.artifact_present = source["artifact_present"];
	        this.artifact_name = source["artifact_name"];
	        this.output_count = source["output_count"];
	        this.primary_result = source["primary_result"];
	        this.result_payload = source["result_payload"];
	    }
	}
	
	export class MaclawLLMStatus {
	    online: boolean;
	    configured: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new MaclawLLMStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.online = source["online"];
	        this.configured = source["configured"];
	        this.error = source["error"];
	    }
	}
	export class MemoryStatusCatRow {
	    category: string;
	    count: number;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new MemoryStatusCatRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.count = source["count"];
	        this.percent = source["percent"];
	    }
	}
	export class MemoryStatusData {
	    total_entries: number;
	    max_capacity: number;
	    capacity_percent: number;
	    archived_entries: number;
	    stale_entries: number;
	    pinned_entries: number;
	    embedder_active: boolean;
	    no_embedding: number;
	    oldest_entry?: string;
	    newest_entry?: string;
	    categories: MemoryStatusCatRow[];
	
	    static createFrom(source: any = {}) {
	        return new MemoryStatusData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_entries = source["total_entries"];
	        this.max_capacity = source["max_capacity"];
	        this.capacity_percent = source["capacity_percent"];
	        this.archived_entries = source["archived_entries"];
	        this.stale_entries = source["stale_entries"];
	        this.pinned_entries = source["pinned_entries"];
	        this.embedder_active = source["embedder_active"];
	        this.no_embedding = source["no_embedding"];
	        this.oldest_entry = source["oldest_entry"];
	        this.newest_entry = source["newest_entry"];
	        this.categories = this.convertValues(source["categories"], MemoryStatusCatRow);
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
	export class MixedSkillSearchResult {
	    id: string;
	    name: string;
	    description: string;
	    tags: string[];
	    source: string;
	    source_label: string;
	    install_ref?: string;
	    file_path?: string;
	    version?: string;
	    author?: string;
	    created_at?: string;
	    package_sha256?: string;
	    sha256?: string;
	    package_checksum?: string;
	    checksum?: string;
	    package_signature?: string;
	    signature?: string;
	    package_download_url?: string;
	    download_url?: string;
	    package_size?: number;
	    trust_level?: string;
	    avg_rating: number;
	    rating_count: number;
	    downloads: number;
	    score: number;
	    price: number;
	    repo_url?: string;
	    installed: boolean;
	    installed_name?: string;
	    can_update: boolean;
	    has_update: boolean;
	    product_kind?: string;
	    is_maclaw_app?: boolean;
	    maclaw_app_id?: string;
	    maclaw_app_name?: string;
	    maclaw_app_description?: string;
	    maclaw_app_kind?: string;
	    maclaw_app_category?: string;
	    maclaw_app_icon?: string;
	    maclaw_app_input_mode?: string;
	    maclaw_app_output_modes?: string[];
	    maclaw_app_definition_sha256?: string;
	    maclaw_app_test_evidence?: MaclawAppSearchEvidence;
	    artifact_contract_required?: boolean;
	    artifact_contract_output_modes?: string[];
	    artifact_contract_presentation?: string;
	
	    static createFrom(source: any = {}) {
	        return new MixedSkillSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.tags = source["tags"];
	        this.source = source["source"];
	        this.source_label = source["source_label"];
	        this.install_ref = source["install_ref"];
	        this.file_path = source["file_path"];
	        this.version = source["version"];
	        this.author = source["author"];
	        this.created_at = source["created_at"];
	        this.package_sha256 = source["package_sha256"];
	        this.sha256 = source["sha256"];
	        this.package_checksum = source["package_checksum"];
	        this.checksum = source["checksum"];
	        this.package_signature = source["package_signature"];
	        this.signature = source["signature"];
	        this.package_download_url = source["package_download_url"];
	        this.download_url = source["download_url"];
	        this.package_size = source["package_size"];
	        this.trust_level = source["trust_level"];
	        this.avg_rating = source["avg_rating"];
	        this.rating_count = source["rating_count"];
	        this.downloads = source["downloads"];
	        this.score = source["score"];
	        this.price = source["price"];
	        this.repo_url = source["repo_url"];
	        this.installed = source["installed"];
	        this.installed_name = source["installed_name"];
	        this.can_update = source["can_update"];
	        this.has_update = source["has_update"];
	        this.product_kind = source["product_kind"];
	        this.is_maclaw_app = source["is_maclaw_app"];
	        this.maclaw_app_id = source["maclaw_app_id"];
	        this.maclaw_app_name = source["maclaw_app_name"];
	        this.maclaw_app_description = source["maclaw_app_description"];
	        this.maclaw_app_kind = source["maclaw_app_kind"];
	        this.maclaw_app_category = source["maclaw_app_category"];
	        this.maclaw_app_icon = source["maclaw_app_icon"];
	        this.maclaw_app_input_mode = source["maclaw_app_input_mode"];
	        this.maclaw_app_output_modes = source["maclaw_app_output_modes"];
	        this.maclaw_app_definition_sha256 = source["maclaw_app_definition_sha256"];
	        this.maclaw_app_test_evidence = this.convertValues(source["maclaw_app_test_evidence"], MaclawAppSearchEvidence);
	        this.artifact_contract_required = source["artifact_contract_required"];
	        this.artifact_contract_output_modes = source["artifact_contract_output_modes"];
	        this.artifact_contract_presentation = source["artifact_contract_presentation"];
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
	export class MoAPresetSummary {
	    id: string;
	    display_name?: string;
	    ref_count: number;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MoAPresetSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.display_name = source["display_name"];
	        this.ref_count = source["ref_count"];
	        this.enabled = source["enabled"];
	    }
	}
	export class MoASessionState {
	    sticky: boolean;
	    one_shot: boolean;
	    preset?: string;
	    display_name?: string;
	    env?: string;
	    enabled: boolean;
	    available: boolean;
	    ref_count?: number;
	    presets?: MoAPresetSummary[];
	
	    static createFrom(source: any = {}) {
	        return new MoASessionState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sticky = source["sticky"];
	        this.one_shot = source["one_shot"];
	        this.preset = source["preset"];
	        this.display_name = source["display_name"];
	        this.env = source["env"];
	        this.enabled = source["enabled"];
	        this.available = source["available"];
	        this.ref_count = source["ref_count"];
	        this.presets = this.convertValues(source["presets"], MoAPresetSummary);
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
	export class MobileDocumentDraftImage {
	    id: string;
	    filename?: string;
	    content_type?: string;
	    size?: number;
	    url?: string;
	
	    static createFrom(source: any = {}) {
	        return new MobileDocumentDraftImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.filename = source["filename"];
	        this.content_type = source["content_type"];
	        this.size = source["size"];
	        this.url = source["url"];
	    }
	}
	export class MobileDocumentDraftImagePayload {
	    content_type: string;
	    filename?: string;
	    data_base64: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new MobileDocumentDraftImagePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content_type = source["content_type"];
	        this.filename = source["filename"];
	        this.data_base64 = source["data_base64"];
	        this.size = source["size"];
	    }
	}
	export class MobileDocumentQuota {
	    document_quota_bytes: number;
	    document_quota_used_bytes: number;
	    document_quota_remaining: number;

	    static createFrom(source: any = {}) {
	        return new MobileDocumentQuota(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.document_quota_bytes = source["document_quota_bytes"];
	        this.document_quota_used_bytes = source["document_quota_used_bytes"];
	        this.document_quota_remaining = source["document_quota_remaining"];
	    }
	}

	export class MobileDocumentDraftSummary {
	    id: string;
	    title: string;
	    template: string;
	    updated_at: string;
	    rune_count: number;
	    preview: string;
	    markdown?: string;
	    has_original?: boolean;
	    source_filename?: string;
	    source_content_type?: string;
	    source_size?: number;
	    source_download_url?: string;
	    images?: MobileDocumentDraftImage[];
	
	    static createFrom(source: any = {}) {
	        return new MobileDocumentDraftSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.template = source["template"];
	        this.updated_at = source["updated_at"];
	        this.rune_count = source["rune_count"];
	        this.preview = source["preview"];
	        this.markdown = source["markdown"];
	        this.has_original = source["has_original"];
	        this.source_filename = source["source_filename"];
	        this.source_content_type = source["source_content_type"];
	        this.source_size = source["source_size"];
	        this.source_download_url = source["source_download_url"];
	        this.images = this.convertValues(source["images"], MobileDocumentDraftImage);
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
	export class MobileLibraryAudio {
	    content_type?: string;
	    size_bytes?: number;
	    duration_sec?: number;
	    available: boolean;
	    download_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new MobileLibraryAudio(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content_type = source["content_type"];
	        this.size_bytes = source["size_bytes"];
	        this.duration_sec = source["duration_sec"];
	        this.available = source["available"];
	        this.download_url = source["download_url"];
	    }
	}
	export class MobileLibraryDerivedDocuments {
	    transcript_draft_id?: string;
	    minutes_draft_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new MobileLibraryDerivedDocuments(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.transcript_draft_id = source["transcript_draft_id"];
	        this.minutes_draft_id = source["minutes_draft_id"];
	    }
	}
	export class MobileLibraryProcessing {
	    status?: string;
	    mode?: string;
	    progress?: number;
	    message?: string;
	    failure_code?: string;
	
	    static createFrom(source: any = {}) {
	        return new MobileLibraryProcessing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.mode = source["mode"];
	        this.progress = source["progress"];
	        this.message = source["message"];
	        this.failure_code = source["failure_code"];
	    }
	}
	export class MobileLibraryItem {
	    id: string;
	    title: string;
	    template: string;
	    updated_at: string;
	    rune_count: number;
	    preview: string;
	    markdown?: string;
	    has_original?: boolean;
	    source_filename?: string;
	    source_content_type?: string;
	    source_size?: number;
	    source_download_url?: string;
	    images?: MobileDocumentDraftImage[];
	    type: string;
	    audio?: MobileLibraryAudio;
	    processing?: MobileLibraryProcessing;
	    derived_documents?: MobileLibraryDerivedDocuments;
	    managed_by_recording_id?: string;
	    retention_until?: string;
	
	    static createFrom(source: any = {}) {
	        return new MobileLibraryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.template = source["template"];
	        this.updated_at = source["updated_at"];
	        this.rune_count = source["rune_count"];
	        this.preview = source["preview"];
	        this.markdown = source["markdown"];
	        this.has_original = source["has_original"];
	        this.source_filename = source["source_filename"];
	        this.source_content_type = source["source_content_type"];
	        this.source_size = source["source_size"];
	        this.source_download_url = source["source_download_url"];
	        this.images = this.convertValues(source["images"], MobileDocumentDraftImage);
	        this.type = source["type"];
	        this.audio = this.convertValues(source["audio"], MobileLibraryAudio);
	        this.processing = this.convertValues(source["processing"], MobileLibraryProcessing);
	        this.derived_documents = this.convertValues(source["derived_documents"], MobileLibraryDerivedDocuments);
	        this.managed_by_recording_id = source["managed_by_recording_id"];
	        this.retention_until = source["retention_until"];
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
	
	export class MobileMeetingRecordingAudioPayload {
	    content_type: string;
	    filename: string;
	    data_base64: string;
	    size_bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new MobileMeetingRecordingAudioPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content_type = source["content_type"];
	        this.filename = source["filename"];
	        this.data_base64 = source["data_base64"];
	        this.size_bytes = source["size_bytes"];
	    }
	}
	export class MobilePWAShellRequest {
	    output_dir: string;
	    app_name: string;
	    application_id: string;
	    ios_bundle_id: string;
	    hubcenter_url: string;
	    start_url: string;
	    generate_android: boolean;
	    generate_ios: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MobilePWAShellRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.output_dir = source["output_dir"];
	        this.app_name = source["app_name"];
	        this.application_id = source["application_id"];
	        this.ios_bundle_id = source["ios_bundle_id"];
	        this.hubcenter_url = source["hubcenter_url"];
	        this.start_url = source["start_url"];
	        this.generate_android = source["generate_android"];
	        this.generate_ios = source["generate_ios"];
	    }
	}
	export class MobilePWAShellResult {
	    root_dir: string;
	    android?: AndroidPWAShellResult;
	    ios?: IOSPWAShellResult;
	
	    static createFrom(source: any = {}) {
	        return new MobilePWAShellResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root_dir = source["root_dir"];
	        this.android = this.convertValues(source["android"], AndroidPWAShellResult);
	        this.ios = this.convertValues(source["ios"], IOSPWAShellResult);
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
	export class NLSkillDefinition {
	    name: string;
	    dir_name?: string;
	    skill_id?: string;
	    description: string;
	    triggers: string[];
	    steps: corelib.NLSkillStep[];
	    status: string;
	    // Go type: time
	    created_at: any;
	    source: string;
	    source_project: string;
	    execution_class?: string;
	    hub_skill_id?: string;
	    hub_version?: string;
	    capability?: corelib.SkillCapabilityRef;
	    trust_level?: string;
	    type?: string;
	    content?: string;
	    publisher?: string;
	    mode?: string;
	    has_documentation: boolean;
	    params?: corelib.NLSkillParam[];
	    required_args?: string[];
	    requires_gui?: boolean;
	    capabilities?: string[];
	    requires_tools?: string[];
	    fallback_for_tools?: string[];
	    requires_toolsets?: string[];
	    fallback_for_toolsets?: string[];
	    usage_count: number;
	    success_count: number;
	    failure_count: number;
	    success_rate: number;
	    // Go type: time
	    last_used_at?: any;
	    last_error?: string;
	    review_reason?: string;
	    repair_attempt_count?: number;
	    last_repair_at?: string;
	    repair_history?: corelib.SkillRepairRecord[];
	    optimization_count?: number;
	    last_optimized_at?: string;
	    is_maclaw_app?: boolean;
	    maclaw_app_count?: number;
	    maclaw_app_entry?: string;
	
	    static createFrom(source: any = {}) {
	        return new NLSkillDefinition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.dir_name = source["dir_name"];
	        this.skill_id = source["skill_id"];
	        this.description = source["description"];
	        this.triggers = source["triggers"];
	        this.steps = this.convertValues(source["steps"], corelib.NLSkillStep);
	        this.status = source["status"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.source = source["source"];
	        this.source_project = source["source_project"];
	        this.execution_class = source["execution_class"];
	        this.hub_skill_id = source["hub_skill_id"];
	        this.hub_version = source["hub_version"];
	        this.capability = this.convertValues(source["capability"], corelib.SkillCapabilityRef);
	        this.trust_level = source["trust_level"];
	        this.type = source["type"];
	        this.content = source["content"];
	        this.publisher = source["publisher"];
	        this.mode = source["mode"];
	        this.has_documentation = source["has_documentation"];
	        this.params = this.convertValues(source["params"], corelib.NLSkillParam);
	        this.required_args = source["required_args"];
	        this.requires_gui = source["requires_gui"];
	        this.capabilities = source["capabilities"];
	        this.requires_tools = source["requires_tools"];
	        this.fallback_for_tools = source["fallback_for_tools"];
	        this.requires_toolsets = source["requires_toolsets"];
	        this.fallback_for_toolsets = source["fallback_for_toolsets"];
	        this.usage_count = source["usage_count"];
	        this.success_count = source["success_count"];
	        this.failure_count = source["failure_count"];
	        this.success_rate = source["success_rate"];
	        this.last_used_at = this.convertValues(source["last_used_at"], null);
	        this.last_error = source["last_error"];
	        this.review_reason = source["review_reason"];
	        this.repair_attempt_count = source["repair_attempt_count"];
	        this.last_repair_at = source["last_repair_at"];
	        this.repair_history = this.convertValues(source["repair_history"], corelib.SkillRepairRecord);
	        this.optimization_count = source["optimization_count"];
	        this.last_optimized_at = source["last_optimized_at"];
	        this.is_maclaw_app = source["is_maclaw_app"];
	        this.maclaw_app_count = source["maclaw_app_count"];
	        this.maclaw_app_entry = source["maclaw_app_entry"];
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
	export class NewsArticle {
	    id: string;
	    title: string;
	    content: string;
	    category: string;
	    pinned: boolean;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new NewsArticle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.pinned = source["pinned"];
	        this.created_at = source["created_at"];
	    }
	}
	export class Orchestrator {
	
	
	    static createFrom(source: any = {}) {
	        return new Orchestrator(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class PassthroughAuditEntry {
	    id: string;
	    kind: string;
	    command_name: string;
	    source?: string;
	    args?: string[];
	    status: string;
	    exit_code: number;
	    duration_ms: number;
	    started_at: string;
	    finished_at: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new PassthroughAuditEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.command_name = source["command_name"];
	        this.source = source["source"];
	        this.args = source["args"];
	        this.status = source["status"];
	        this.exit_code = source["exit_code"];
	        this.duration_ms = source["duration_ms"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	        this.error = source["error"];
	    }
	}
	export class PassthroughParam {
	    name: string;
	    type?: string;
	    required?: boolean;
	    default?: string;
	    example?: string;
	
	    static createFrom(source: any = {}) {
	        return new PassthroughParam(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.default = source["default"];
	        this.example = source["example"];
	    }
	}
	export class PassthroughCommand {
	    name: string;
	    title?: string;
	    description?: string;
	    script_path: string;
	    template_args?: string[];
	    runtime: string;
	    cwd?: string;
	    timeout_seconds: number;
	    confirm_required: boolean;
	    enabled: boolean;
	    params?: PassthroughParam[];
	    created_at?: string;
	    updated_at?: string;
	    last_run_at?: string;
	    last_exit_code?: number;
	    last_status?: string;
	
	    static createFrom(source: any = {}) {
	        return new PassthroughCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.script_path = source["script_path"];
	        this.template_args = source["template_args"];
	        this.runtime = source["runtime"];
	        this.cwd = source["cwd"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.confirm_required = source["confirm_required"];
	        this.enabled = source["enabled"];
	        this.params = this.convertValues(source["params"], PassthroughParam);
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.last_run_at = source["last_run_at"];
	        this.last_exit_code = source["last_exit_code"];
	        this.last_status = source["last_status"];
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
	
	export class PassthroughRunResult {
	    command_name: string;
	    status: string;
	    exit_code: number;
	    duration_ms: number;
	    output: string;
	    args?: string[];
	    started_at: string;
	    finished_at: string;
	
	    static createFrom(source: any = {}) {
	        return new PassthroughRunResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command_name = source["command_name"];
	        this.status = source["status"];
	        this.exit_code = source["exit_code"];
	        this.duration_ms = source["duration_ms"];
	        this.output = source["output"];
	        this.args = source["args"];
	        this.started_at = source["started_at"];
	        this.finished_at = source["finished_at"];
	    }
	}
	export class PassthroughSettings {
	    allow_exec: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PassthroughSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allow_exec = source["allow_exec"];
	    }
	}
	export class PendingQuestionOption {
	    label?: string;
	    description?: string;
	    preview?: string;
	
	    static createFrom(source: any = {}) {
	        return new PendingQuestionOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.description = source["description"];
	        this.preview = source["preview"];
	    }
	}
	export class PendingQuestionView {
	    tool_use_id?: string;
	    tool_name?: string;
	    header?: string;
	    question?: string;
	    hint?: string;
	    multi?: boolean;
	    options?: PendingQuestionOption[];
	
	    static createFrom(source: any = {}) {
	        return new PendingQuestionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_use_id = source["tool_use_id"];
	        this.tool_name = source["tool_name"];
	        this.header = source["header"];
	        this.question = source["question"];
	        this.hint = source["hint"];
	        this.multi = source["multi"];
	        this.options = this.convertValues(source["options"], PendingQuestionOption);
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
	export class PetPackRuntimeInfo {
	    pack_id: string;
	    variant: string;
	    declared_renderer: string;
	    effective_renderer: string;
	    degradation_reason: string;
	
	    static createFrom(source: any = {}) {
	        return new PetPackRuntimeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pack_id = source["pack_id"];
	        this.variant = source["variant"];
	        this.declared_renderer = source["declared_renderer"];
	        this.effective_renderer = source["effective_renderer"];
	        this.degradation_reason = source["degradation_reason"];
	    }
	}
	export class PolicyEngine {
	
	
	    static createFrom(source: any = {}) {
	        return new PolicyEngine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
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
	export class ProjectWorkflowState {
	    id?: string;
	    type?: string;
	    phase?: string;
	    status?: string;
	    project_path?: string;
	    pending_review?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProjectWorkflowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.phase = source["phase"];
	        this.status = source["status"];
	        this.project_path = source["project_path"];
	        this.pending_review = source["pending_review"];
	    }
	}
	export class ProjectContextSummary {
	    project_name: string;
	    recent_progress: string;
	    key_artifacts: string[];
	    recent_artifacts?: ProjectContextArtifact[];
	    active_workflow: string;
	    workflow_state?: ProjectWorkflowState;
	
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
	        this.workflow_state = this.convertValues(source["workflow_state"], ProjectWorkflowState);
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
	export class ProjectConversationHistoryItem {
	    role: string;
	    content: string;
	    reasoning_content?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectConversationHistoryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.reasoning_content = source["reasoning_content"];
	    }
	}
	export class ProjectScanner {
	
	
	    static createFrom(source: any = {}) {
	        return new ProjectScanner(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
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
	export class ProjectSceneDetail {
	    project_path: string;
	    name?: string;
	    active_workflow?: ProjectWorkflowState;
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
	        this.active_workflow = this.convertValues(source["active_workflow"], ProjectWorkflowState);
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
	
	export class ProjectSearchResult {
	    id: string;
	    name: string;
	    project_path: string;
	    working_dir?: string;
	    workflow_type: string;
	    active_workflow?: ProjectWorkflowState;
	    preview: string;
	    tags: string[];
	    last_activity: string;
	    entry_count: number;
	    has_output: boolean;
	    pinned: boolean;
	    archived: boolean;
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
	        this.active_workflow = this.convertValues(source["active_workflow"], ProjectWorkflowState);
	        this.preview = source["preview"];
	        this.tags = source["tags"];
	        this.last_activity = source["last_activity"];
	        this.entry_count = source["entry_count"];
	        this.has_output = source["has_output"];
	        this.pinned = source["pinned"];
	        this.archived = source["archived"];
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
	
	export class ProviderView {
	    name: string;
	    model_id: string;
	    is_default: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.model_id = source["model_id"];
	        this.is_default = source["is_default"];
	    }
	}
	export class ReclaimableVirtualEmployee {
	    id: string;
	    machine_id?: string;
	    name: string;
	    skill_description?: string;
	    status?: string;
	    online_status?: string;
	    twin_slot?: string;
	    registered_at?: string;
	    updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ReclaimableVirtualEmployee(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.machine_id = source["machine_id"];
	        this.name = source["name"];
	        this.skill_description = source["skill_description"];
	        this.status = source["status"];
	        this.online_status = source["online_status"];
	        this.twin_slot = source["twin_slot"];
	        this.registered_at = source["registered_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class RemoteActivationResult {
	    status: string;
	    hub_id?: string;
	    tenant_id?: string;
	    tenant_name?: string;
	    message?: string;
	    code?: string;
	    user_id?: string;
	    email?: string;
	    phone_number?: string;
	    sn?: string;
	    machine_id?: string;
	    machine_token?: string;
	    viewer_token?: string;
	    expires_at?: string;
	    vip_flag?: boolean;
	    rebound_existing_user?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteActivationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.hub_id = source["hub_id"];
	        this.tenant_id = source["tenant_id"];
	        this.tenant_name = source["tenant_name"];
	        this.message = source["message"];
	        this.code = source["code"];
	        this.user_id = source["user_id"];
	        this.email = source["email"];
	        this.phone_number = source["phone_number"];
	        this.sn = source["sn"];
	        this.machine_id = source["machine_id"];
	        this.machine_token = source["machine_token"];
	        this.viewer_token = source["viewer_token"];
	        this.expires_at = source["expires_at"];
	        this.vip_flag = source["vip_flag"];
	        this.rebound_existing_user = source["rebound_existing_user"];
	    }
	}
	export class RemoteActivationStatus {
	    activated: boolean;
	    hub_id?: string;
	    email: string;
	    sn: string;
	    tenant_id?: string;
	    tenant_name?: string;
	    machine_id: string;
	    hub_url: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteActivationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.activated = source["activated"];
	        this.hub_id = source["hub_id"];
	        this.email = source["email"];
	        this.sn = source["sn"];
	        this.tenant_id = source["tenant_id"];
	        this.tenant_name = source["tenant_name"];
	        this.machine_id = source["machine_id"];
	        this.hub_url = source["hub_url"];
	    }
	}
	export class RemoteCodingTaskMeta {
	    host: string;
	    user: string;
	    port: number;
	    work_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteCodingTaskMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.user = source["user"];
	        this.port = source["port"];
	        this.work_dir = source["work_dir"];
	    }
	}
	export class RemoteConnectionStatus {
	    enabled: boolean;
	    hub_url: string;
	    machine_id: string;
	    connected: boolean;
	    last_error: string;
	    session_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteConnectionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.hub_url = source["hub_url"];
	        this.machine_id = source["machine_id"];
	        this.connected = source["connected"];
	        this.last_error = source["last_error"];
	        this.session_count = source["session_count"];
	    }
	}
	export class RemoteHubCenterHub {
	    hub_id: string;
	    name: string;
	    base_url: string;
	    pwa_url: string;
	    visibility: string;
	    enrollment_mode: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteHubCenterHub(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_id = source["hub_id"];
	        this.name = source["name"];
	        this.base_url = source["base_url"];
	        this.pwa_url = source["pwa_url"];
	        this.visibility = source["visibility"];
	        this.enrollment_mode = source["enrollment_mode"];
	        this.status = source["status"];
	    }
	}
	export class RemoteHubVisibilityResult {
	    attempted: boolean;
	    verified: boolean;
	    hub_url: string;
	    user_id: string;
	    machine_id: string;
	    session_id: string;
	    machine_visible: boolean;
	    session_visible: boolean;
	    session_status?: string;
	    host_online: boolean;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteHubVisibilityResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attempted = source["attempted"];
	        this.verified = source["verified"];
	        this.hub_url = source["hub_url"];
	        this.user_id = source["user_id"];
	        this.machine_id = source["machine_id"];
	        this.session_id = source["session_id"];
	        this.machine_visible = source["machine_visible"];
	        this.session_visible = source["session_visible"];
	        this.session_status = source["session_status"];
	        this.host_online = source["host_online"];
	        this.message = source["message"];
	    }
	}
	export class RemoteLaunchProject {
	    id: string;
	    name: string;
	    path: string;
	    use_proxy: boolean;
	    yolo_mode: boolean;
	    admin_mode: boolean;
	    python_project: boolean;
	    python_env: string;
	    is_current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteLaunchProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.use_proxy = source["use_proxy"];
	        this.yolo_mode = source["yolo_mode"];
	        this.admin_mode = source["admin_mode"];
	        this.python_project = source["python_project"];
	        this.python_env = source["python_env"];
	        this.is_current = source["is_current"];
	    }
	}
	export class RemotePTYProbeResult {
	    supported: boolean;
	    ready: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RemotePTYProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.ready = source["ready"];
	        this.message = source["message"];
	    }
	}
	export class RemoteProbeResult {
	    invitation_code_required: boolean;
	    tenant_id?: string;
	    tenant_name?: string;
	    phone_number?: string;
	    status?: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.invitation_code_required = source["invitation_code_required"];
	        this.tenant_id = source["tenant_id"];
	        this.tenant_name = source["tenant_name"];
	        this.phone_number = source["phone_number"];
	        this.status = source["status"];
	        this.message = source["message"];
	    }
	}
	export class RemoteRegistrationAuthResult {
	    method: string;
	    tenant_id?: string;
	    code_ttl_minutes?: number;
	    code_length?: number;
	    provider?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteRegistrationAuthResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.tenant_id = source["tenant_id"];
	        this.code_ttl_minutes = source["code_ttl_minutes"];
	        this.code_length = source["code_length"];
	        this.provider = source["provider"];
	    }
	}
	export class RemoteRegistrationContactResult {
	    ok: boolean;
	    kind?: string;
	    tenant_id?: string;
	    email?: string;
	    phone_number?: string;
	    expires_min?: number;
	    code_length?: number;
	    purpose?: string;
	    daily_sms_remaining?: number;
	    resend_cooldown_seconds?: number;
	    code?: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteRegistrationContactResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.kind = source["kind"];
	        this.tenant_id = source["tenant_id"];
	        this.email = source["email"];
	        this.phone_number = source["phone_number"];
	        this.expires_min = source["expires_min"];
	        this.code_length = source["code_length"];
	        this.purpose = source["purpose"];
	        this.daily_sms_remaining = source["daily_sms_remaining"];
	        this.resend_cooldown_seconds = source["resend_cooldown_seconds"];
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class RemoteRegistrationProfileResult {
	    ok: boolean;
	    tenant_id?: string;
	    tenant_name?: string;
	    user_id?: string;
	    machine_id?: string;
	    email?: string;
	    phone_number?: string;
	    message?: string;
	    code?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteRegistrationProfileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.tenant_id = source["tenant_id"];
	        this.tenant_name = source["tenant_name"];
	        this.user_id = source["user_id"];
	        this.machine_id = source["machine_id"];
	        this.email = source["email"];
	        this.phone_number = source["phone_number"];
	        this.message = source["message"];
	        this.code = source["code"];
	    }
	}
	export class RemoteRegistrationTargetResult {
	    identity: string;
	    hub_url: string;
	    hub_id?: string;
	    tenant_id?: string;
	    method: string;
	    code_ttl_minutes?: number;
	    code_length?: number;
	    provider?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteRegistrationTargetResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.identity = source["identity"];
	        this.hub_url = source["hub_url"];
	        this.hub_id = source["hub_id"];
	        this.tenant_id = source["tenant_id"];
	        this.method = source["method"];
	        this.code_ttl_minutes = source["code_ttl_minutes"];
	        this.code_length = source["code_length"];
	        this.provider = source["provider"];
	    }
	}
	export class RemoteSMSSendResult {
	    ok: boolean;
	    code?: string;
	    tenant_id?: string;
	    expires_min?: number;
	    code_length?: number;
	    purpose?: string;
	    daily_sms_remaining?: number;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSMSSendResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.code = source["code"];
	        this.tenant_id = source["tenant_id"];
	        this.expires_min = source["expires_min"];
	        this.code_length = source["code_length"];
	        this.purpose = source["purpose"];
	        this.daily_sms_remaining = source["daily_sms_remaining"];
	        this.message = source["message"];
	    }
	}
	export class RemoteSessionManager {
	
	
	    static createFrom(source: any = {}) {
	        return new RemoteSessionManager(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class RemoteSessionTokenUsage {
	    input_tokens?: number;
	    output_tokens?: number;
	    cached_input_tokens?: number;
	    cache_write_tokens?: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSessionTokenUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.cached_input_tokens = source["cached_input_tokens"];
	        this.cache_write_tokens = source["cache_write_tokens"];
	    }
	}
	export class SessionOutputImage {
	    image_id: string;
	    media_type: string;
	    data: string;
	    after_line_idx: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionOutputImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.image_id = source["image_id"];
	        this.media_type = source["media_type"];
	        this.data = source["data"];
	        this.after_line_idx = source["after_line_idx"];
	    }
	}
	export class SessionPreview {
	    session_id: string;
	    output_seq: number;
	    preview_lines: string[];
	    updated_at: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.output_seq = source["output_seq"];
	        this.preview_lines = source["preview_lines"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class SessionSummary {
	    session_id: string;
	    machine_id: string;
	    tool: string;
	    title: string;
	    source?: string;
	    status: string;
	    severity: string;
	    waiting_for_user: boolean;
	    thinking: boolean;
	    thinking_since?: number;
	    current_task: string;
	    progress_summary: string;
	    step_progress?: string;
	    step_count?: number;
	    last_result: string;
	    suggested_action: string;
	    important_files: string[];
	    last_command: string;
	    pending_question?: PendingQuestionView;
	    token_usage?: RemoteSessionTokenUsage;
	    updated_at: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.machine_id = source["machine_id"];
	        this.tool = source["tool"];
	        this.title = source["title"];
	        this.source = source["source"];
	        this.status = source["status"];
	        this.severity = source["severity"];
	        this.waiting_for_user = source["waiting_for_user"];
	        this.thinking = source["thinking"];
	        this.thinking_since = source["thinking_since"];
	        this.current_task = source["current_task"];
	        this.progress_summary = source["progress_summary"];
	        this.step_progress = source["step_progress"];
	        this.step_count = source["step_count"];
	        this.last_result = source["last_result"];
	        this.suggested_action = source["suggested_action"];
	        this.important_files = source["important_files"];
	        this.last_command = source["last_command"];
	        this.pending_question = this.convertValues(source["pending_question"], PendingQuestionView);
	        this.token_usage = this.convertValues(source["token_usage"], RemoteSessionTokenUsage);
	        this.updated_at = source["updated_at"];
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
	export class RemoteSessionView {
	    id: string;
	    tool: string;
	    title: string;
	    launch_source?: string;
	    project_path: string;
	    workspace_path: string;
	    workspace_root: string;
	    workspace_mode: string;
	    workspace_is_git: boolean;
	    model_id: string;
	    provider?: string;
	    job_id?: string;
	    run_id?: string;
	    current_url?: string;
	    current_title?: string;
	    ready_state?: string;
	    last_snapshot_id?: string;
	    execution_mode: string;
	    status: string;
	    thinking: boolean;
	    thinking_since?: number;
	    pid: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    summary: SessionSummary;
	    preview: SessionPreview;
	    events: ImportantEvent[];
	    raw_output_lines: string[];
	    output_images?: SessionOutputImage[];
	    token_usage?: RemoteSessionTokenUsage;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSessionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tool = source["tool"];
	        this.title = source["title"];
	        this.launch_source = source["launch_source"];
	        this.project_path = source["project_path"];
	        this.workspace_path = source["workspace_path"];
	        this.workspace_root = source["workspace_root"];
	        this.workspace_mode = source["workspace_mode"];
	        this.workspace_is_git = source["workspace_is_git"];
	        this.model_id = source["model_id"];
	        this.provider = source["provider"];
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.current_url = source["current_url"];
	        this.current_title = source["current_title"];
	        this.ready_state = source["ready_state"];
	        this.last_snapshot_id = source["last_snapshot_id"];
	        this.execution_mode = source["execution_mode"];
	        this.status = source["status"];
	        this.thinking = source["thinking"];
	        this.thinking_since = source["thinking_since"];
	        this.pid = source["pid"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.summary = this.convertValues(source["summary"], SessionSummary);
	        this.preview = this.convertValues(source["preview"], SessionPreview);
	        this.events = this.convertValues(source["events"], ImportantEvent);
	        this.raw_output_lines = source["raw_output_lines"];
	        this.output_images = this.convertValues(source["output_images"], SessionOutputImage);
	        this.token_usage = this.convertValues(source["token_usage"], RemoteSessionTokenUsage);
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
	export class RemoteToolLaunchProbeResult {
	    tool: string;
	    supported: boolean;
	    ready: boolean;
	    message: string;
	    command_path: string;
	    project_path: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteToolLaunchProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.supported = source["supported"];
	        this.ready = source["ready"];
	        this.message = source["message"];
	        this.command_path = source["command_path"];
	        this.project_path = source["project_path"];
	    }
	}
	export class RemoteToolReadiness {
	    tool: string;
	    ready: boolean;
	    remote_enabled: boolean;
	    tool_installed: boolean;
	    model_configured: boolean;
	    project_path: string;
	    tool_path: string;
	    command_path: string;
	    hub_url: string;
	    pty_supported: boolean;
	    pty_message: string;
	    selected_model: string;
	    selected_model_id: string;
	    issues: string[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteToolReadiness(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.ready = source["ready"];
	        this.remote_enabled = source["remote_enabled"];
	        this.tool_installed = source["tool_installed"];
	        this.model_configured = source["model_configured"];
	        this.project_path = source["project_path"];
	        this.tool_path = source["tool_path"];
	        this.command_path = source["command_path"];
	        this.hub_url = source["hub_url"];
	        this.pty_supported = source["pty_supported"];
	        this.pty_message = source["pty_message"];
	        this.selected_model = source["selected_model"];
	        this.selected_model_id = source["selected_model_id"];
	        this.issues = source["issues"];
	        this.warnings = source["warnings"];
	    }
	}
	export class RemoteSmokeReport {
	    tool: string;
	    project_path: string;
	    use_proxy: boolean;
	    phase: string;
	    success: boolean;
	    last_updated: string;
	    recommended_next?: string;
	    connection: RemoteConnectionStatus;
	    readiness: RemoteToolReadiness;
	    pty_probe?: RemotePTYProbeResult;
	    launch_probe?: RemoteToolLaunchProbeResult;
	    activation?: RemoteActivationResult;
	    started_session?: RemoteSessionView;
	    hub_visibility?: RemoteHubVisibilityResult;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSmokeReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.project_path = source["project_path"];
	        this.use_proxy = source["use_proxy"];
	        this.phase = source["phase"];
	        this.success = source["success"];
	        this.last_updated = source["last_updated"];
	        this.recommended_next = source["recommended_next"];
	        this.connection = this.convertValues(source["connection"], RemoteConnectionStatus);
	        this.readiness = this.convertValues(source["readiness"], RemoteToolReadiness);
	        this.pty_probe = this.convertValues(source["pty_probe"], RemotePTYProbeResult);
	        this.launch_probe = this.convertValues(source["launch_probe"], RemoteToolLaunchProbeResult);
	        this.activation = this.convertValues(source["activation"], RemoteActivationResult);
	        this.started_session = this.convertValues(source["started_session"], RemoteSessionView);
	        this.hub_visibility = this.convertValues(source["hub_visibility"], RemoteHubVisibilityResult);
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
	export class RemoteSmokeSnapshot {
	    exists: boolean;
	    path: string;
	    report?: RemoteSmokeReport;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSmokeSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.path = source["path"];
	        this.report = this.convertValues(source["report"], RemoteSmokeReport);
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
	export class RemoteStartSessionRequest {
	    tool: string;
	    project_id?: string;
	    project_path?: string;
	    provider?: string;
	    use_proxy?: boolean;
	    yolo_mode?: boolean;
	    admin_mode?: boolean;
	    python_env?: string;
	    launch_source?: string;
	    resume_session_id?: string;
	    inject_resume_prompt?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteStartSessionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.project_id = source["project_id"];
	        this.project_path = source["project_path"];
	        this.provider = source["provider"];
	        this.use_proxy = source["use_proxy"];
	        this.yolo_mode = source["yolo_mode"];
	        this.admin_mode = source["admin_mode"];
	        this.python_env = source["python_env"];
	        this.launch_source = source["launch_source"];
	        this.resume_session_id = source["resume_session_id"];
	        this.inject_resume_prompt = source["inject_resume_prompt"];
	    }
	}
	
	export class RemoteToolMetadataView {
	    name: string;
	    display_name: string;
	    binary_name: string;
	    default_title: string;
	    uses_openai_compat: boolean;
	    requires_session_config: boolean;
	    supports_proxy: boolean;
	    supports_remote: boolean;
	    visible: boolean;
	    installed: boolean;
	    can_start: boolean;
	    tool_path: string;
	    unavailable_reason?: string;
	    readiness_hint: string;
	    smoke_hint: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteToolMetadataView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.display_name = source["display_name"];
	        this.binary_name = source["binary_name"];
	        this.default_title = source["default_title"];
	        this.uses_openai_compat = source["uses_openai_compat"];
	        this.requires_session_config = source["requires_session_config"];
	        this.supports_proxy = source["supports_proxy"];
	        this.supports_remote = source["supports_remote"];
	        this.visible = source["visible"];
	        this.installed = source["installed"];
	        this.can_start = source["can_start"];
	        this.tool_path = source["tool_path"];
	        this.unavailable_reason = source["unavailable_reason"];
	        this.readiness_hint = source["readiness_hint"];
	        this.smoke_hint = source["smoke_hint"];
	    }
	}
	
	export class RestoreReport {
	    restored: number;
	    skipped: number;
	    failed: number;
	    details: string[];
	
	    static createFrom(source: any = {}) {
	        return new RestoreReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.restored = source["restored"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.details = source["details"];
	    }
	}
	export class RiskAssessor {
	
	
	    static createFrom(source: any = {}) {
	        return new RiskAssessor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	
	export class SSHBackgroundTaskView {
	    task_id: string;
	    session_id: string;
	    task_role?: string;
	    status: string;
	    started_at: string;
	    mirror_file?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHBackgroundTaskView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.session_id = source["session_id"];
	        this.task_role = source["task_role"];
	        this.status = source["status"];
	        this.started_at = source["started_at"];
	        this.mirror_file = source["mirror_file"];
	    }
	}
	export class SaveWebSearchStrategyRequest {
	    version: number;
	    preset: string;
	    mode: string;
	    engines: corelib.WebSearchEngineConfig[];
	    clear_api_key_engine_ids?: string[];
	    browser_fallback_enabled: boolean;
	    browser_fallback_engine_id: string;
	    browser_human_assist_enabled: boolean;
	    hedging_delay_ms: number;
	    min_results_before_hedge: number;
	
	    static createFrom(source: any = {}) {
	        return new SaveWebSearchStrategyRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.preset = source["preset"];
	        this.mode = source["mode"];
	        this.engines = this.convertValues(source["engines"], corelib.WebSearchEngineConfig);
	        this.clear_api_key_engine_ids = source["clear_api_key_engine_ids"];
	        this.browser_fallback_enabled = source["browser_fallback_enabled"];
	        this.browser_fallback_engine_id = source["browser_fallback_engine_id"];
	        this.browser_human_assist_enabled = source["browser_human_assist_enabled"];
	        this.hedging_delay_ms = source["hedging_delay_ms"];
	        this.min_results_before_hedge = source["min_results_before_hedge"];
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
	export class SecurityEventItem {
	    time: string;
	    tool_name: string;
	    target: string;
	    remote_ip: string;
	    risk_level: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new SecurityEventItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.tool_name = source["tool_name"];
	        this.target = source["target"];
	        this.remote_ip = source["remote_ip"];
	        this.risk_level = source["risk_level"];
	        this.reason = source["reason"];
	    }
	}
	
	
	export class SessionProgressInfo {
	    session_status: string;
	    current_task?: string;
	    progress_summary?: string;
	    last_result?: string;
	    last_command?: string;
	    waiting_for_user?: boolean;
	    last_output_lines?: string[];
	    updated_at?: string;
	    poll_count?: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionProgressInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_status = source["session_status"];
	        this.current_task = source["current_task"];
	        this.progress_summary = source["progress_summary"];
	        this.last_result = source["last_result"];
	        this.last_command = source["last_command"];
	        this.waiting_for_user = source["waiting_for_user"];
	        this.last_output_lines = source["last_output_lines"];
	        this.updated_at = source["updated_at"];
	        this.poll_count = source["poll_count"];
	    }
	}
	
	export class modelRouteDecision {
	    task: string;
	    source: string;
	    model: string;
	    provider?: string;
	    reason: string;
	    baseline_model?: string;
	    escalated?: boolean;
	    cost_tier?: string;
	    cost_route_mode?: string;
	    cost_route_applied?: boolean;
	    thinking_policy?: string;
	
	    static createFrom(source: any = {}) {
	        return new modelRouteDecision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task = source["task"];
	        this.source = source["source"];
	        this.model = source["model"];
	        this.provider = source["provider"];
	        this.reason = source["reason"];
	        this.baseline_model = source["baseline_model"];
	        this.escalated = source["escalated"];
	        this.cost_tier = source["cost_tier"];
	        this.cost_route_mode = source["cost_route_mode"];
	        this.cost_route_applied = source["cost_route_applied"];
	        this.thinking_policy = source["thinking_policy"];
	    }
	}
	export class SharedAgentLoopStatus {
	    mode: string;
	    percent: number;
	    workflow_pilot: boolean;
	    config_enabled: boolean;
	    config_migrated: boolean;
	    default_enabled: boolean;
	    env_override?: string;
	    env_locks_mode: boolean;
	    shared_turns: number;
	    legacy_turns: number;
	    shared_success: number;
	    shared_error: number;
	    shared_cancelled: number;
	    skip_canary: number;
	    skip_ineligible: number;
	    shadow_eligible: number;
	    skip_by_reason?: Record<string, number>;
	    last_skip_reason?: string;
	    last_skip_at?: string;
	    last_shared_at?: string;
	    last_legacy_at?: string;
	    last_route?: modelRouteDecision;
	    last_usage?: agent.TurnUsage;
	    process_usage?: agent.TurnUsage;
	    prompt_light_turns: number;
	    prompt_full_turns: number;
	    prompt_light_percent: number;
	    prompt_est_tokens_saved: number;
	    last_prompt_profile?: string;
	    last_prompt_at?: string;
	    last_prompt_saved_tokens?: number;
	    last_prompt_task?: string;
	    last_prompt_reason?: string;
	    prompt_by_task?: Record<string, number>;
	    prompt_light_tool_denies: number;
	    prompt_last_denied_tool?: string;
	    prompt_by_denied_tool?: Record<string, number>;
	    prompt_light_upgrades: number;
	    prompt_last_upgrade_reason?: string;
	    prompt_ab_eligible_light?: number;
	    prompt_ab_sample_full?: number;
	    prompt_ab_sample_percent?: number;
	    prompt_upgrade_rate_percent?: number;
	    prompt_deny_rate_percent?: number;
	    prompt_profile_env?: string;
	    prompt_profile_forced?: string;
	    percent_from_env: boolean;
	    workflow_from_env: boolean;
	    config_canary_percent?: number;
	    config_workflow: boolean;
	    light_retry_enabled: boolean;
	    hub_connected: boolean;
	    hub_url?: string;
	    hub_adaptive_summary?: string;
	    hub_cost_ops_summary?: string;
	    export_dir?: string;
	    tool_compress_saved_bytes?: number;
	    tool_compress_spills?: number;
	    tool_compress_projects?: number;
	    tool_compress_by_tool?: Record<string, number>;
	    cost_session_usd?: number;
	    cost_daily_usd?: number;
	    cost_budget_usd?: number;
	    cost_over_budget?: boolean;
	    cost_session_line?: string;
	    cost_daily_line?: string;
	    cost_fleet_usd?: number;
	    cost_fleet_calls?: number;
	    cost_fleet_instances?: number;
	    cost_fleet_line?: string;
	    cost_route_decisions?: number;
	    cost_route_applied?: number;
	    cost_route_shadow?: number;
	    cost_route_last_tier?: string;
	    cost_route_last_mode?: string;
	    cost_route_by_tier?: Record<string, number>;
	    cost_route_line?: string;
	    denial_paused?: boolean;
	    denial_consecutive?: number;
	    denial_threshold?: number;
	    denial_last_tool?: string;
	    denial_pause_message?: string;
	    process_started_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new SharedAgentLoopStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.percent = source["percent"];
	        this.workflow_pilot = source["workflow_pilot"];
	        this.config_enabled = source["config_enabled"];
	        this.config_migrated = source["config_migrated"];
	        this.default_enabled = source["default_enabled"];
	        this.env_override = source["env_override"];
	        this.env_locks_mode = source["env_locks_mode"];
	        this.shared_turns = source["shared_turns"];
	        this.legacy_turns = source["legacy_turns"];
	        this.shared_success = source["shared_success"];
	        this.shared_error = source["shared_error"];
	        this.shared_cancelled = source["shared_cancelled"];
	        this.skip_canary = source["skip_canary"];
	        this.skip_ineligible = source["skip_ineligible"];
	        this.shadow_eligible = source["shadow_eligible"];
	        this.skip_by_reason = source["skip_by_reason"];
	        this.last_skip_reason = source["last_skip_reason"];
	        this.last_skip_at = source["last_skip_at"];
	        this.last_shared_at = source["last_shared_at"];
	        this.last_legacy_at = source["last_legacy_at"];
	        this.last_route = this.convertValues(source["last_route"], modelRouteDecision);
	        this.last_usage = this.convertValues(source["last_usage"], agent.TurnUsage);
	        this.process_usage = this.convertValues(source["process_usage"], agent.TurnUsage);
	        this.prompt_light_turns = source["prompt_light_turns"];
	        this.prompt_full_turns = source["prompt_full_turns"];
	        this.prompt_light_percent = source["prompt_light_percent"];
	        this.prompt_est_tokens_saved = source["prompt_est_tokens_saved"];
	        this.last_prompt_profile = source["last_prompt_profile"];
	        this.last_prompt_at = source["last_prompt_at"];
	        this.last_prompt_saved_tokens = source["last_prompt_saved_tokens"];
	        this.last_prompt_task = source["last_prompt_task"];
	        this.last_prompt_reason = source["last_prompt_reason"];
	        this.prompt_by_task = source["prompt_by_task"];
	        this.prompt_light_tool_denies = source["prompt_light_tool_denies"];
	        this.prompt_last_denied_tool = source["prompt_last_denied_tool"];
	        this.prompt_by_denied_tool = source["prompt_by_denied_tool"];
	        this.prompt_light_upgrades = source["prompt_light_upgrades"];
	        this.prompt_last_upgrade_reason = source["prompt_last_upgrade_reason"];
	        this.prompt_ab_eligible_light = source["prompt_ab_eligible_light"];
	        this.prompt_ab_sample_full = source["prompt_ab_sample_full"];
	        this.prompt_ab_sample_percent = source["prompt_ab_sample_percent"];
	        this.prompt_upgrade_rate_percent = source["prompt_upgrade_rate_percent"];
	        this.prompt_deny_rate_percent = source["prompt_deny_rate_percent"];
	        this.prompt_profile_env = source["prompt_profile_env"];
	        this.prompt_profile_forced = source["prompt_profile_forced"];
	        this.percent_from_env = source["percent_from_env"];
	        this.workflow_from_env = source["workflow_from_env"];
	        this.config_canary_percent = source["config_canary_percent"];
	        this.config_workflow = source["config_workflow"];
	        this.light_retry_enabled = source["light_retry_enabled"];
	        this.hub_connected = source["hub_connected"];
	        this.hub_url = source["hub_url"];
	        this.hub_adaptive_summary = source["hub_adaptive_summary"];
	        this.hub_cost_ops_summary = source["hub_cost_ops_summary"];
	        this.export_dir = source["export_dir"];
	        this.tool_compress_saved_bytes = source["tool_compress_saved_bytes"];
	        this.tool_compress_spills = source["tool_compress_spills"];
	        this.tool_compress_projects = source["tool_compress_projects"];
	        this.tool_compress_by_tool = source["tool_compress_by_tool"];
	        this.cost_session_usd = source["cost_session_usd"];
	        this.cost_daily_usd = source["cost_daily_usd"];
	        this.cost_budget_usd = source["cost_budget_usd"];
	        this.cost_over_budget = source["cost_over_budget"];
	        this.cost_session_line = source["cost_session_line"];
	        this.cost_daily_line = source["cost_daily_line"];
	        this.cost_fleet_usd = source["cost_fleet_usd"];
	        this.cost_fleet_calls = source["cost_fleet_calls"];
	        this.cost_fleet_instances = source["cost_fleet_instances"];
	        this.cost_fleet_line = source["cost_fleet_line"];
	        this.cost_route_decisions = source["cost_route_decisions"];
	        this.cost_route_applied = source["cost_route_applied"];
	        this.cost_route_shadow = source["cost_route_shadow"];
	        this.cost_route_last_tier = source["cost_route_last_tier"];
	        this.cost_route_last_mode = source["cost_route_last_mode"];
	        this.cost_route_by_tier = source["cost_route_by_tier"];
	        this.cost_route_line = source["cost_route_line"];
	        this.denial_paused = source["denial_paused"];
	        this.denial_consecutive = source["denial_consecutive"];
	        this.denial_threshold = source["denial_threshold"];
	        this.denial_last_tool = source["denial_last_tool"];
	        this.denial_pause_message = source["denial_pause_message"];
	        this.process_started_at = source["process_started_at"];
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
	export class SharedContextStore {
	
	
	    static createFrom(source: any = {}) {
	        return new SharedContextStore(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SkillAppInputFileRef {
	    name: string;
	    size: number;
	    type?: string;
	    last_modified?: number;
	    staged_path: string;
	    transfer: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillAppInputFileRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.type = source["type"];
	        this.last_modified = source["last_modified"];
	        this.staged_path = source["staged_path"];
	        this.transfer = source["transfer"];
	    }
	}
	export class SkillAppManifestField {
	    name: string;
	    label?: string;
	    type?: string;
	    required?: boolean;
	    default?: any;
	    options?: any[];
	
	    static createFrom(source: any = {}) {
	        return new SkillAppManifestField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.default = source["default"];
	        this.options = source["options"];
	    }
	}
	export class SkillAppManifestEntry {
	    id: string;
	    skill_id: string;
	    source?: string;
	    hub_skill_id?: string;
	    name: string;
	    description?: string;
	    category?: string;
	    kind?: string;
	    icon?: string;
	    custom_icon_data_url?: string;
	    input_mode?: string;
	    multiple_files?: boolean;
	    output_modes?: string[];
	    fields?: SkillAppManifestField[];
	    app_definition_file?: string;
	    app_definition?: Record<string, any>;
	    governance?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new SkillAppManifestEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.skill_id = source["skill_id"];
	        this.source = source["source"];
	        this.hub_skill_id = source["hub_skill_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.kind = source["kind"];
	        this.icon = source["icon"];
	        this.custom_icon_data_url = source["custom_icon_data_url"];
	        this.input_mode = source["input_mode"];
	        this.multiple_files = source["multiple_files"];
	        this.output_modes = source["output_modes"];
	        this.fields = this.convertValues(source["fields"], SkillAppManifestField);
	        this.app_definition_file = source["app_definition_file"];
	        this.app_definition = source["app_definition"];
	        this.governance = source["governance"];
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
	
	export class SkillArtifactRegistryEntry {
	    uri: string;
	    run_id: string;
	    owner_id?: string;
	    skill?: string;
	    artifact_id: string;
	    name?: string;
	    path?: string;
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
	        this.path = source["path"];
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
	export class SkillDiagEntry {
	    dir: string;
	    name: string;
	    ok: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillDiagEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.name = source["name"];
	        this.ok = source["ok"];
	        this.reason = source["reason"];
	    }
	}
	export class SkillEvolutionStatus {
	    pipeline_started: boolean;
	    pending_skills: number;
	    coalesced_notifications: number;
	    dropped_notifications: number;
	    processed_requests: number;
	    enable_repair: boolean;
	    enable_optimizer: boolean;
	    enable_promoter: boolean;
	    repair_cooldown: string;
	    has_repair_hook: boolean;
	    has_optimizer: boolean;
	    has_promoter: boolean;
	    repair_cooldown_hours: number;
	    env_disabled: boolean;
	    config_enabled: boolean;
	    config_disabled: boolean;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillEvolutionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pipeline_started = source["pipeline_started"];
	        this.pending_skills = source["pending_skills"];
	        this.coalesced_notifications = source["coalesced_notifications"];
	        this.dropped_notifications = source["dropped_notifications"];
	        this.processed_requests = source["processed_requests"];
	        this.enable_repair = source["enable_repair"];
	        this.enable_optimizer = source["enable_optimizer"];
	        this.enable_promoter = source["enable_promoter"];
	        this.repair_cooldown = source["repair_cooldown"];
	        this.has_repair_hook = source["has_repair_hook"];
	        this.has_optimizer = source["has_optimizer"];
	        this.has_promoter = source["has_promoter"];
	        this.repair_cooldown_hours = source["repair_cooldown_hours"];
	        this.env_disabled = source["env_disabled"];
	        this.config_enabled = source["config_enabled"];
	        this.config_disabled = source["config_disabled"];
	        this.disabled = source["disabled"];
	    }
	}
	export class SkillExecutor {
	
	
	    static createFrom(source: any = {}) {
	        return new SkillExecutor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SkillHubClient {
	
	
	    static createFrom(source: any = {}) {
	        return new SkillHubClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SkillMarketClient {
	
	
	    static createFrom(source: any = {}) {
	        return new SkillMarketClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SkillRunArtifact {
	    id: string;
	    uri?: string;
	    name?: string;
	    path?: string;
	    mime_type?: string;
	    size_bytes?: number;
	    remote_url?: string;
	    checksum?: string;
	    download_state?: string;
	    status?: string;
	    presentation?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillRunArtifact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.uri = source["uri"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.mime_type = source["mime_type"];
	        this.size_bytes = source["size_bytes"];
	        this.remote_url = source["remote_url"];
	        this.checksum = source["checksum"];
	        this.download_state = source["download_state"];
	        this.status = source["status"];
	        this.presentation = source["presentation"];
	    }
	}
	export class SkillRunOutputBlock {
	    id: string;
	    kind: string;
	    title?: string;
	    text?: string;
	    status?: string;
	    artifact_id?: string;
	    artifact?: SkillRunArtifact;
	
	    static createFrom(source: any = {}) {
	        return new SkillRunOutputBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.text = source["text"];
	        this.status = source["status"];
	        this.artifact_id = source["artifact_id"];
	        this.artifact = this.convertValues(source["artifact"], SkillRunArtifact);
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
	export class SkillRunSessionMeta {
	    session_id?: string;
	    tool?: string;
	    project_path?: string;
	    status?: string;
	    job_id?: string;
	    run_id?: string;
	    resume_session_id?: string;
	    launch_source?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillRunSessionMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.tool = source["tool"];
	        this.project_path = source["project_path"];
	        this.status = source["status"];
	        this.job_id = source["job_id"];
	        this.run_id = source["run_id"];
	        this.resume_session_id = source["resume_session_id"];
	        this.launch_source = source["launch_source"];
	    }
	}
	export class SkillRunSummary {
	    current_step_index?: number;
	    current_step?: string;
	    current_step_status?: string;
	    last_completed_step?: string;
	    last_completed_step_index?: number;
	    last_output_snippet?: string;
	    last_error_snippet?: string;
	    has_session_binding?: boolean;
	    needs_artifact_verification?: boolean;
	    artifact_path?: string;
	    artifact_status?: string;
	    artifacts?: SkillRunArtifact[];
	    output_blocks?: SkillRunOutputBlock[];
	
	    static createFrom(source: any = {}) {
	        return new SkillRunSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_step_index = source["current_step_index"];
	        this.current_step = source["current_step"];
	        this.current_step_status = source["current_step_status"];
	        this.last_completed_step = source["last_completed_step"];
	        this.last_completed_step_index = source["last_completed_step_index"];
	        this.last_output_snippet = source["last_output_snippet"];
	        this.last_error_snippet = source["last_error_snippet"];
	        this.has_session_binding = source["has_session_binding"];
	        this.needs_artifact_verification = source["needs_artifact_verification"];
	        this.artifact_path = source["artifact_path"];
	        this.artifact_status = source["artifact_status"];
	        this.artifacts = this.convertValues(source["artifacts"], SkillRunArtifact);
	        this.output_blocks = this.convertValues(source["output_blocks"], SkillRunOutputBlock);
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
	export class StepResult {
	    index: number;
	    name?: string;
	    action: string;
	    status: string;
	    output?: string;
	    error?: string;
	    exit_code?: number;
	    stdout_last_lines?: string[];
	    stderr_last_lines?: string[];
	    shell_path?: string;
	    command_resolved?: string;
	    duration_ms?: number;
	    timeout?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StepResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.action = source["action"];
	        this.status = source["status"];
	        this.output = source["output"];
	        this.error = source["error"];
	        this.exit_code = source["exit_code"];
	        this.stdout_last_lines = source["stdout_last_lines"];
	        this.stderr_last_lines = source["stderr_last_lines"];
	        this.shell_path = source["shell_path"];
	        this.command_resolved = source["command_resolved"];
	        this.duration_ms = source["duration_ms"];
	        this.timeout = source["timeout"];
	    }
	}
	export class SkillRunStatus {
	    run_id: string;
	    skill: string;
	    owner_id?: string;
	    status: string;
	    steps: StepResult[];
	    session?: SkillRunSessionMeta;
	    session_progress?: SessionProgressInfo;
	    summary?: SkillRunSummary;
	    outputs?: SkillRunOutputBlock[];
	    artifacts?: SkillRunArtifact[];
	    expected_output?: string;
	    expected_artifact?: boolean;
	    started_at: string;
	    ended_at?: string;
	    error?: string;
	    warnings?: string[];
	    duration_ms?: number;
	    total_steps?: number;
	    failed_steps?: number;
	    skipped_steps?: number;
	    self_repair_pending?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillRunStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.skill = source["skill"];
	        this.owner_id = source["owner_id"];
	        this.status = source["status"];
	        this.steps = this.convertValues(source["steps"], StepResult);
	        this.session = this.convertValues(source["session"], SkillRunSessionMeta);
	        this.session_progress = this.convertValues(source["session_progress"], SessionProgressInfo);
	        this.summary = this.convertValues(source["summary"], SkillRunSummary);
	        this.outputs = this.convertValues(source["outputs"], SkillRunOutputBlock);
	        this.artifacts = this.convertValues(source["artifacts"], SkillRunArtifact);
	        this.expected_output = source["expected_output"];
	        this.expected_artifact = source["expected_artifact"];
	        this.started_at = source["started_at"];
	        this.ended_at = source["ended_at"];
	        this.error = source["error"];
	        this.warnings = source["warnings"];
	        this.duration_ms = source["duration_ms"];
	        this.total_steps = source["total_steps"];
	        this.failed_steps = source["failed_steps"];
	        this.skipped_steps = source["skipped_steps"];
	        this.self_repair_pending = source["self_repair_pending"];
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
	
	export class SkillRunner {
	
	
	    static createFrom(source: any = {}) {
	        return new SkillRunner(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SkillUploadQueueItem {
	    id: string;
	    skill_name: string;
	    skill_dir?: string;
	    local_hash?: string;
	    reason?: string;
	    status: string;
	    attempts: number;
	    last_error?: string;
	    next_attempt_at?: string;
	    created_at: string;
	    updated_at: string;
	    submission_id?: string;
	    uploaded_targets?: Record<string, string>;
	    quality_score?: number;
	    require_runtime_proof: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillUploadQueueItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.skill_name = source["skill_name"];
	        this.skill_dir = source["skill_dir"];
	        this.local_hash = source["local_hash"];
	        this.reason = source["reason"];
	        this.status = source["status"];
	        this.attempts = source["attempts"];
	        this.last_error = source["last_error"];
	        this.next_attempt_at = source["next_attempt_at"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.submission_id = source["submission_id"];
	        this.uploaded_targets = source["uploaded_targets"];
	        this.quality_score = source["quality_score"];
	        this.require_runtime_proof = source["require_runtime_proof"];
	    }
	}
	export class SpeakerTranscript {
	    start: number;
	    end: number;
	    speaker: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeakerTranscript(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	        this.speaker = source["speaker"];
	        this.text = source["text"];
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
	export class TabIndexEntry {
	    id: string;
	    type: string;
	    title: string;
	    projectPath?: string;
	    agentMode?: string;
	    remoteHost?: string;
	    remoteSafety?: string;
	    lastActiveAt: number;
	    archived: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TabIndexEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.title = source["title"];
	        this.projectPath = source["projectPath"];
	        this.agentMode = source["agentMode"];
	        this.remoteHost = source["remoteHost"];
	        this.remoteSafety = source["remoteSafety"];
	        this.lastActiveAt = source["lastActiveAt"];
	        this.archived = source["archived"];
	    }
	}
	export class TestWebSearchEngineRequest {
	    engine: corelib.WebSearchEngineConfig;
	    use_saved_key: boolean;
	    human_assist_enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TestWebSearchEngineRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = this.convertValues(source["engine"], corelib.WebSearchEngineConfig);
	        this.use_saved_key = source["use_saved_key"];
	        this.human_assist_enabled = source["human_assist_enabled"];
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
	export class ToolCacheCleanupResult {
	    path: string;
	    exists: boolean;
	    before_bytes: number;
	    after_bytes: number;
	    freed_bytes: number;
	    skipped: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolCacheCleanupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.before_bytes = source["before_bytes"];
	        this.after_bytes = source["after_bytes"];
	        this.freed_bytes = source["freed_bytes"];
	        this.skipped = source["skipped"];
	        this.reason = source["reason"];
	    }
	}
	export class ToolCacheStatus {
	    path: string;
	    exists: boolean;
	    size_bytes: number;
	    size_approximate?: boolean;
	    auto_enabled: boolean;
	    max_bytes: number;
	    min_interval_hours: number;
	    clean_on_startup: boolean;
	    clean_on_exit: boolean;
	    last_cleanup_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolCacheStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.size_bytes = source["size_bytes"];
	        this.size_approximate = source["size_approximate"];
	        this.auto_enabled = source["auto_enabled"];
	        this.max_bytes = source["max_bytes"];
	        this.min_interval_hours = source["min_interval_hours"];
	        this.clean_on_startup = source["clean_on_startup"];
	        this.clean_on_exit = source["clean_on_exit"];
	        this.last_cleanup_at = source["last_cleanup_at"];
	    }
	}
	export class ToolDefinitionGenerator {
	
	
	    static createFrom(source: any = {}) {
	        return new ToolDefinitionGenerator(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ToolRouter {
	
	
	    static createFrom(source: any = {}) {
	        return new ToolRouter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ToolSelector {
	
	
	    static createFrom(source: any = {}) {
	        return new ToolSelector(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
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
	
	
	
	export class UIShellConfig {
	    language: string;
	    active_tool: string;
	    default_launch_mode: string;
	    remote_enabled: boolean;
	    pause_env_check: boolean;
	    current_project: string;
	    ui_zoom_factor?: number;
	    chat_font_size?: number;
	    show_app_entry?: boolean;
	    show_coding_tool_entry?: boolean;
	    show_utilities_entry?: boolean;
	    workflow_enabled?: boolean;
	    maclaw_llm_current_provider?: string;
	
	    static createFrom(source: any = {}) {
	        return new UIShellConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.active_tool = source["active_tool"];
	        this.default_launch_mode = source["default_launch_mode"];
	        this.remote_enabled = source["remote_enabled"];
	        this.pause_env_check = source["pause_env_check"];
	        this.current_project = source["current_project"];
	        this.ui_zoom_factor = source["ui_zoom_factor"];
	        this.chat_font_size = source["chat_font_size"];
	        this.show_app_entry = source["show_app_entry"];
	        this.show_coding_tool_entry = source["show_coding_tool_entry"];
	        this.show_utilities_entry = source["show_utilities_entry"];
	        this.workflow_enabled = source["workflow_enabled"];
	        this.maclaw_llm_current_provider = source["maclaw_llm_current_provider"];
	    }
	}
	export class UpdateResult {
	    has_update: boolean;
	    latest_version: string;
	    release_url: string;
	    tag_name: string;
	    download_url: string;
	    download_unavailable: boolean;
	    sha256?: string;
	    channel?: string;
	
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
	        this.download_unavailable = source["download_unavailable"];
	        this.sha256 = source["sha256"];
	        this.channel = source["channel"];
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
	export class VEApprovalConfig {
	    enabled: boolean;
	    acl: AccessControlList;
	    rules: ApprovalRules;
	    max_queue_size: number;
	    timeout_hours: number;
	    daily_quota: number;
	    fallback_approver?: string;
	
	    static createFrom(source: any = {}) {
	        return new VEApprovalConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.acl = this.convertValues(source["acl"], AccessControlList);
	        this.rules = this.convertValues(source["rules"], ApprovalRules);
	        this.max_queue_size = source["max_queue_size"];
	        this.timeout_hours = source["timeout_hours"];
	        this.daily_quota = source["daily_quota"];
	        this.fallback_approver = source["fallback_approver"];
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
	export class VESessionInfo {
	    session_id: string;
	    ve_id: string;
	    ve_name: string;
	
	    static createFrom(source: any = {}) {
	        return new VESessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.ve_id = source["ve_id"];
	        this.ve_name = source["ve_name"];
	    }
	}
	export class VirtualEmployeeEntry {
	    id: string;
	    machine_id?: string;
	    name: string;
	    skill_description: string;
	    avatar_data_url?: string;
	    access_policy: string;
	    status: string;
	    online_status: string;
	    resident?: boolean;
	    registered_at?: string;
	    whitelist?: string[];
	    blacklist?: string[];
	
	    static createFrom(source: any = {}) {
	        return new VirtualEmployeeEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.machine_id = source["machine_id"];
	        this.name = source["name"];
	        this.skill_description = source["skill_description"];
	        this.avatar_data_url = source["avatar_data_url"];
	        this.access_policy = source["access_policy"];
	        this.status = source["status"];
	        this.online_status = source["online_status"];
	        this.resident = source["resident"];
	        this.registered_at = source["registered_at"];
	        this.whitelist = source["whitelist"];
	        this.blacklist = source["blacklist"];
	    }
	}
	export class VEStatusResponse {
	    registered: boolean;
	    employee?: VirtualEmployeeEntry;
	
	    static createFrom(source: any = {}) {
	        return new VEStatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.registered = source["registered"];
	        this.employee = this.convertValues(source["employee"], VirtualEmployeeEntry);
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
	export class VSCodeACPLaunchResult {
	    ok: boolean;
	    message: string;
	    steps: string[];
	    warnings?: string[];
	    vscodePath?: string;
	    bridgePath?: string;
	    settingsPath?: string;
	    extensionId?: string;
	    gatewayReady: boolean;
	    agentName?: string;
	    needVSCodeInstall?: boolean;
	    vscodeDownloadURL?: string;
	
	    static createFrom(source: any = {}) {
	        return new VSCodeACPLaunchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.steps = source["steps"];
	        this.warnings = source["warnings"];
	        this.vscodePath = source["vscodePath"];
	        this.bridgePath = source["bridgePath"];
	        this.settingsPath = source["settingsPath"];
	        this.extensionId = source["extensionId"];
	        this.gatewayReady = source["gatewayReady"];
	        this.agentName = source["agentName"];
	        this.needVSCodeInstall = source["needVSCodeInstall"];
	        this.vscodeDownloadURL = source["vscodeDownloadURL"];
	    }
	}
	export class VectorSearchStatus {
	    enabled: boolean;
	    model_exists: boolean;
	    model_path: string;
	    model_size: number;
	    embedder_ok: boolean;
	    embedder_dim: number;
	    entry_count: number;
	    embedded_count: number;
	    hybrid_tool_retrieval_active: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VectorSearchStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.model_exists = source["model_exists"];
	        this.model_path = source["model_path"];
	        this.model_size = source["model_size"];
	        this.embedder_ok = source["embedder_ok"];
	        this.embedder_dim = source["embedder_dim"];
	        this.entry_count = source["entry_count"];
	        this.embedded_count = source["embedded_count"];
	        this.hybrid_tool_retrieval_active = source["hybrid_tool_retrieval_active"];
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
	export class WebSearchEngineTestResult {
	    engine_id: string;
	    transport: string;
	    duration_ms: number;
	    result_count: number;
	    retry_count?: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new WebSearchEngineTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine_id = source["engine_id"];
	        this.transport = source["transport"];
	        this.duration_ms = source["duration_ms"];
	        this.result_count = source["result_count"];
	        this.retry_count = source["retry_count"];
	        this.message = source["message"];
	    }
	}
	export class WebSearchEngineView {
	    id: string;
	    name: string;
	    enabled: boolean;
	    priority: number;
	    transport: string;
	    needs_api_key: boolean;
	    has_api_key: boolean;
	    api_key?: string;
	    base_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new WebSearchEngineView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.priority = source["priority"];
	        this.transport = source["transport"];
	        this.needs_api_key = source["needs_api_key"];
	        this.has_api_key = source["has_api_key"];
	        this.api_key = source["api_key"];
	        this.base_url = source["base_url"];
	    }
	}
	export class WebSearchStrategyView {
	    version: number;
	    preset: string;
	    mode: string;
	    engines: WebSearchEngineView[];
	    browser_fallback_enabled: boolean;
	    browser_fallback_engine_id: string;
	    browser_human_assist_enabled: boolean;
	    hedging_delay_ms: number;
	    min_results_before_hedge: number;
	
	    static createFrom(source: any = {}) {
	        return new WebSearchStrategyView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.preset = source["preset"];
	        this.mode = source["mode"];
	        this.engines = this.convertValues(source["engines"], WebSearchEngineView);
	        this.browser_fallback_enabled = source["browser_fallback_enabled"];
	        this.browser_fallback_engine_id = source["browser_fallback_engine_id"];
	        this.browser_human_assist_enabled = source["browser_human_assist_enabled"];
	        this.hedging_delay_ms = source["hedging_delay_ms"];
	        this.min_results_before_hedge = source["min_results_before_hedge"];
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
	export class WelcomeSyncPullResult {
	    owner_user_id?: string;
	    owner_user_email?: string;
	    tenant_id?: string;
	    has_document: boolean;
	    revision?: string;
	    stored_size_bytes?: number;
	    template_count?: number;
	    kind?: string;
	    exported_at?: string;
	    created_at?: string;
	    updated_at?: string;
	    limit_bytes?: number;
	    message?: string;
	    logged_in: boolean;
	    hub_url?: string;
	    error?: string;
	    payload_json?: string;
	
	    static createFrom(source: any = {}) {
	        return new WelcomeSyncPullResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner_user_id = source["owner_user_id"];
	        this.owner_user_email = source["owner_user_email"];
	        this.tenant_id = source["tenant_id"];
	        this.has_document = source["has_document"];
	        this.revision = source["revision"];
	        this.stored_size_bytes = source["stored_size_bytes"];
	        this.template_count = source["template_count"];
	        this.kind = source["kind"];
	        this.exported_at = source["exported_at"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.limit_bytes = source["limit_bytes"];
	        this.message = source["message"];
	        this.logged_in = source["logged_in"];
	        this.hub_url = source["hub_url"];
	        this.error = source["error"];
	        this.payload_json = source["payload_json"];
	    }
	}
	export class WelcomeSyncPushRequest {
	    hub_url?: string;
	    hub_token?: string;
	    tenant_id?: string;
	    email?: string;
	    payload_json: string;
	    if_match_revision?: string;
	
	    static createFrom(source: any = {}) {
	        return new WelcomeSyncPushRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.tenant_id = source["tenant_id"];
	        this.email = source["email"];
	        this.payload_json = source["payload_json"];
	        this.if_match_revision = source["if_match_revision"];
	    }
	}
	export class WelcomeSyncRequest {
	    hub_url?: string;
	    hub_token?: string;
	    tenant_id?: string;
	    email?: string;
	
	    static createFrom(source: any = {}) {
	        return new WelcomeSyncRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hub_url = source["hub_url"];
	        this.hub_token = source["hub_token"];
	        this.tenant_id = source["tenant_id"];
	        this.email = source["email"];
	    }
	}
	export class WelcomeSyncStatus {
	    owner_user_id?: string;
	    owner_user_email?: string;
	    tenant_id?: string;
	    has_document: boolean;
	    revision?: string;
	    stored_size_bytes?: number;
	    template_count?: number;
	    kind?: string;
	    exported_at?: string;
	    created_at?: string;
	    updated_at?: string;
	    limit_bytes?: number;
	    message?: string;
	    logged_in: boolean;
	    hub_url?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WelcomeSyncStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner_user_id = source["owner_user_id"];
	        this.owner_user_email = source["owner_user_email"];
	        this.tenant_id = source["tenant_id"];
	        this.has_document = source["has_document"];
	        this.revision = source["revision"];
	        this.stored_size_bytes = source["stored_size_bytes"];
	        this.template_count = source["template_count"];
	        this.kind = source["kind"];
	        this.exported_at = source["exported_at"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.limit_bytes = source["limit_bytes"];
	        this.message = source["message"];
	        this.logged_in = source["logged_in"];
	        this.hub_url = source["hub_url"];
	        this.error = source["error"];
	    }
	}
	
	export class codingCheckpointSidecarStats {
	    total_bytes: number;
	    max_bytes: number;
	    usage_ratio?: number;
	    dir_count: number;
	    user_bytes?: number;
	    user_dir_count?: number;
	    user_key?: string;
	    keep_label?: string;
	
	    static createFrom(source: any = {}) {
	        return new codingCheckpointSidecarStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_bytes = source["total_bytes"];
	        this.max_bytes = source["max_bytes"];
	        this.usage_ratio = source["usage_ratio"];
	        this.dir_count = source["dir_count"];
	        this.user_bytes = source["user_bytes"];
	        this.user_dir_count = source["user_dir_count"];
	        this.user_key = source["user_key"];
	        this.keep_label = source["keep_label"];
	    }
	}
	export class codingConflictFileDiff {
	    path: string;
	    status: string;
	    main_head?: string;
	    their_head?: string;
	    base_head?: string;
	    unified?: string;
	    three_way?: string;
	
	    static createFrom(source: any = {}) {
	        return new codingConflictFileDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.status = source["status"];
	        this.main_head = source["main_head"];
	        this.their_head = source["their_head"];
	        this.base_head = source["base_head"];
	        this.unified = source["unified"];
	        this.three_way = source["three_way"];
	    }
	}
	export class codingConflictFilePreview {
	    path: string;
	    side: string;
	    content?: string;
	    bytes?: number;
	    truncated?: boolean;
	    missing?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new codingConflictFilePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.side = source["side"];
	        this.content = source["content"];
	        this.bytes = source["bytes"];
	        this.truncated = source["truncated"];
	        this.missing = source["missing"];
	    }
	}
	export class codingConflictFileTriple {
	    path: string;
	    main: codingConflictFilePreview;
	    theirs: codingConflictFilePreview;
	    base: codingConflictFilePreview;
	
	    static createFrom(source: any = {}) {
	        return new codingConflictFileTriple(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.main = this.convertValues(source["main"], codingConflictFilePreview);
	        this.theirs = this.convertValues(source["theirs"], codingConflictFilePreview);
	        this.base = this.convertValues(source["base"], codingConflictFilePreview);
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
	
	
	export class codingWorkbenchPendingPlanDTO {
	    user_text?: string;
	    markdown?: string;
	    step_count?: number;
	    created_at?: number;
	
	    static createFrom(source: any = {}) {
	        return new codingWorkbenchPendingPlanDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user_text = source["user_text"];
	        this.markdown = source["markdown"];
	        this.step_count = source["step_count"];
	        this.created_at = source["created_at"];
	    }
	}
	
	
	
	export class maclawAppApprovalArtifact {
	    id?: string;
	    uri?: string;
	    name?: string;
	    path?: string;
	    mime_type?: string;
	    size_bytes?: number;
	    remote_url?: string;
	    checksum?: string;
	    download_state?: string;
	    status?: string;
	    presentation?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppApprovalArtifact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.uri = source["uri"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.mime_type = source["mime_type"];
	        this.size_bytes = source["size_bytes"];
	        this.remote_url = source["remote_url"];
	        this.checksum = source["checksum"];
	        this.download_state = source["download_state"];
	        this.status = source["status"];
	        this.presentation = source["presentation"];
	    }
	}
	export class maclawAppApprovalEvent {
	    at: string;
	    node?: string;
	    actor?: string;
	    decision?: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppApprovalEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.node = source["node"];
	        this.actor = source["actor"];
	        this.decision = source["decision"];
	        this.message = source["message"];
	    }
	}
	export class maclawAppApprovalOutput {
	    type?: string;
	    kind?: string;
	    title?: string;
	    text?: string;
	    status?: string;
	    artifact_id?: string;
	    artifact?: maclawAppApprovalArtifact;
	    data?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppApprovalOutput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.text = source["text"];
	        this.status = source["status"];
	        this.artifact_id = source["artifact_id"];
	        this.artifact = this.convertValues(source["artifact"], maclawAppApprovalArtifact);
	        this.data = source["data"];
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
	export class maclawAppApprovalInstance {
	    app_id: string;
	    app_name?: string;
	    blueprint_id?: string;
	    dataset_id?: string;
	    object_role?: string;
	    approval_object_role?: string;
	    approval_event?: string;
	    approval_workflow_id?: string;
	    approval_engine?: string;
	    hub_workflow_id?: string;
	    hub_instance_id?: string;
	    hub_node_id?: string;
	    hub_sync_error?: string;
	    instance_id: string;
	    title: string;
	    lane: string;
	    status: string;
	    current_node: string;
	    current_node_status?: string;
	    current_node_ids?: string[];
	    workflow_node_ids?: string[];
	    node_tasks?: any[];
	    owner: string;
	    applicant?: string;
	    approver: string;
	    current_assignee?: string;
	    current_assignee_type?: string;
	    created_at?: string;
	    updated_at: string;
	    result: string;
	    workflow_skill_id?: string;
	    workflow_version?: string;
	    business_status?: string;
	    result_status?: string;
	    from_status?: string;
	    to_status?: string;
	    workflow_decision_id?: string;
	    record_id?: string;
	    approval_id?: string;
	    record_approval_id?: string;
	    detail_url?: string;
	    business_entity?: string;
	    business_action?: string;
	    business_note?: string;
	    result_payload?: Record<string, any>;
	    outputs?: maclawAppApprovalOutput[];
	    artifacts?: maclawAppApprovalArtifact[];
	    events?: maclawAppApprovalEvent[];
	
	    static createFrom(source: any = {}) {
	        return new maclawAppApprovalInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_id = source["app_id"];
	        this.app_name = source["app_name"];
	        this.blueprint_id = source["blueprint_id"];
	        this.dataset_id = source["dataset_id"];
	        this.object_role = source["object_role"];
	        this.approval_object_role = source["approval_object_role"];
	        this.approval_event = source["approval_event"];
	        this.approval_workflow_id = source["approval_workflow_id"];
	        this.approval_engine = source["approval_engine"];
	        this.hub_workflow_id = source["hub_workflow_id"];
	        this.hub_instance_id = source["hub_instance_id"];
	        this.hub_node_id = source["hub_node_id"];
	        this.hub_sync_error = source["hub_sync_error"];
	        this.instance_id = source["instance_id"];
	        this.title = source["title"];
	        this.lane = source["lane"];
	        this.status = source["status"];
	        this.current_node = source["current_node"];
	        this.current_node_status = source["current_node_status"];
	        this.current_node_ids = source["current_node_ids"];
	        this.workflow_node_ids = source["workflow_node_ids"];
	        this.node_tasks = source["node_tasks"];
	        this.owner = source["owner"];
	        this.applicant = source["applicant"];
	        this.approver = source["approver"];
	        this.current_assignee = source["current_assignee"];
	        this.current_assignee_type = source["current_assignee_type"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.result = source["result"];
	        this.workflow_skill_id = source["workflow_skill_id"];
	        this.workflow_version = source["workflow_version"];
	        this.business_status = source["business_status"];
	        this.result_status = source["result_status"];
	        this.from_status = source["from_status"];
	        this.to_status = source["to_status"];
	        this.workflow_decision_id = source["workflow_decision_id"];
	        this.record_id = source["record_id"];
	        this.approval_id = source["approval_id"];
	        this.record_approval_id = source["record_approval_id"];
	        this.detail_url = source["detail_url"];
	        this.business_entity = source["business_entity"];
	        this.business_action = source["business_action"];
	        this.business_note = source["business_note"];
	        this.result_payload = source["result_payload"];
	        this.outputs = this.convertValues(source["outputs"], maclawAppApprovalOutput);
	        this.artifacts = this.convertValues(source["artifacts"], maclawAppApprovalArtifact);
	        this.events = this.convertValues(source["events"], maclawAppApprovalEvent);
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
	export class maclawAppApprovalDataSrvSyncInput {
	    dataset_id: string;
	    object_role?: string;
	    app_id?: string;
	    blueprint_id?: string;
	    record_id: string;
	    approval_id?: string;
	    instance: maclawAppApprovalInstance;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppApprovalDataSrvSyncInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dataset_id = source["dataset_id"];
	        this.object_role = source["object_role"];
	        this.app_id = source["app_id"];
	        this.blueprint_id = source["blueprint_id"];
	        this.record_id = source["record_id"];
	        this.approval_id = source["approval_id"];
	        this.instance = this.convertValues(source["instance"], maclawAppApprovalInstance);
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
	
	
	
	export class maclawAppInstallApprovalBindingSnapshot {
	    event?: string;
	    dataset_id?: string;
	    blueprint_id?: string;
	    object_role?: string;
	    workflow_skill_id?: string;
	    workflow_version?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallApprovalBindingSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.event = source["event"];
	        this.dataset_id = source["dataset_id"];
	        this.blueprint_id = source["blueprint_id"];
	        this.object_role = source["object_role"];
	        this.workflow_skill_id = source["workflow_skill_id"];
	        this.workflow_version = source["workflow_version"];
	    }
	}
	export class maclawAppReviewIssue {
	    path?: string;
	    severity?: string;
	    message: string;
	    suggestion?: string;
	    metadata?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppReviewIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.severity = source["severity"];
	        this.message = source["message"];
	        this.suggestion = source["suggestion"];
	        this.metadata = source["metadata"];
	    }
	}
	export class maclawAppInstallPlanDependency {
	    id: string;
	    skill_id?: string;
	    version?: string;
	    kind?: string;
	    required: boolean;
	    source?: string;
	    install_ref?: string;
	    canonical_id?: string;
	    aliases?: string[];
	    install_ref_kind?: string;
	    install_ref_target?: string;
	    install_ref_version?: string;
	    install_ref_status?: string;
	    install_ref_message?: string;
	    install_error_code?: string;
	    install_error_stage?: string;
	    install_error_detail?: string;
	    preflight_status?: string;
	    preflight_code?: string;
	    preflight_stage?: string;
	    preflight_message?: string;
	    package_sha256?: string;
	    package_checksum?: string;
	    package_signature?: string;
	    package_download_url?: string;
	    download_node?: string;
	    resolved_download_url?: string;
	    integrity_status?: string;
	    integrity_code?: string;
	    integrity_stage?: string;
	    integrity_message?: string;
	    app_ids?: string[];
	    installed: boolean;
	    installed_name?: string;
	    installed_version?: string;
	    required_version?: string;
	    version_status?: string;
	    runtime_skill_ref?: string;
	    installed_dir?: string;
	    installed_status?: string;
	    health?: string;
	    action: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallPlanDependency(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.skill_id = source["skill_id"];
	        this.version = source["version"];
	        this.kind = source["kind"];
	        this.required = source["required"];
	        this.source = source["source"];
	        this.install_ref = source["install_ref"];
	        this.canonical_id = source["canonical_id"];
	        this.aliases = source["aliases"];
	        this.install_ref_kind = source["install_ref_kind"];
	        this.install_ref_target = source["install_ref_target"];
	        this.install_ref_version = source["install_ref_version"];
	        this.install_ref_status = source["install_ref_status"];
	        this.install_ref_message = source["install_ref_message"];
	        this.install_error_code = source["install_error_code"];
	        this.install_error_stage = source["install_error_stage"];
	        this.install_error_detail = source["install_error_detail"];
	        this.preflight_status = source["preflight_status"];
	        this.preflight_code = source["preflight_code"];
	        this.preflight_stage = source["preflight_stage"];
	        this.preflight_message = source["preflight_message"];
	        this.package_sha256 = source["package_sha256"];
	        this.package_checksum = source["package_checksum"];
	        this.package_signature = source["package_signature"];
	        this.package_download_url = source["package_download_url"];
	        this.download_node = source["download_node"];
	        this.resolved_download_url = source["resolved_download_url"];
	        this.integrity_status = source["integrity_status"];
	        this.integrity_code = source["integrity_code"];
	        this.integrity_stage = source["integrity_stage"];
	        this.integrity_message = source["integrity_message"];
	        this.app_ids = source["app_ids"];
	        this.installed = source["installed"];
	        this.installed_name = source["installed_name"];
	        this.installed_version = source["installed_version"];
	        this.required_version = source["required_version"];
	        this.version_status = source["version_status"];
	        this.runtime_skill_ref = source["runtime_skill_ref"];
	        this.installed_dir = source["installed_dir"];
	        this.installed_status = source["installed_status"];
	        this.health = source["health"];
	        this.action = source["action"];
	        this.message = source["message"];
	    }
	}
	export class maclawAppInstallPlanApp {
	    id: string;
	    name?: string;
	    kind?: string;
	    schema?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallPlanApp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.schema = source["schema"];
	    }
	}
	export class maclawAppInstallPlan {
	    schema: string;
	    apps: maclawAppInstallPlanApp[];
	    dependencies: maclawAppInstallPlanDependency[];
	    workflow_contract_issues?: maclawAppReviewIssue[];
	    governance_review_issues?: maclawAppReviewIssue[];
	    has_missing_required: boolean;
	    has_blocking_dependency?: boolean;
	    has_workflow_contract_issue?: boolean;
	    has_governance_review_issue?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.apps = this.convertValues(source["apps"], maclawAppInstallPlanApp);
	        this.dependencies = this.convertValues(source["dependencies"], maclawAppInstallPlanDependency);
	        this.workflow_contract_issues = this.convertValues(source["workflow_contract_issues"], maclawAppReviewIssue);
	        this.governance_review_issues = this.convertValues(source["governance_review_issues"], maclawAppReviewIssue);
	        this.has_missing_required = source["has_missing_required"];
	        this.has_blocking_dependency = source["has_blocking_dependency"];
	        this.has_workflow_contract_issue = source["has_workflow_contract_issue"];
	        this.has_governance_review_issue = source["has_governance_review_issue"];
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
	
	
	export class maclawAppInstallSkillVersionSnapshot {
	    id: string;
	    version?: string;
	    kind?: string;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallSkillVersionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.version = source["version"];
	        this.kind = source["kind"];
	        this.source = source["source"];
	    }
	}
	export class maclawAppInstallVersionSnapshot {
	    app_entry_version?: string;
	    app_skill?: maclawAppInstallSkillVersionSnapshot;
	    workflow_skills?: maclawAppInstallSkillVersionSnapshot[];
	    approval_bindings?: maclawAppInstallApprovalBindingSnapshot[];
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallVersionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_entry_version = source["app_entry_version"];
	        this.app_skill = this.convertValues(source["app_skill"], maclawAppInstallSkillVersionSnapshot);
	        this.workflow_skills = this.convertValues(source["workflow_skills"], maclawAppInstallSkillVersionSnapshot);
	        this.approval_bindings = this.convertValues(source["approval_bindings"], maclawAppInstallApprovalBindingSnapshot);
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
	export class maclawAppInstallRecord {
	    app_id: string;
	    app_name?: string;
	    kind?: string;
	    source?: string;
	    installed_at: string;
	    package_sha256?: string;
	    package_bytes?: number;
	    package?: Record<string, any>;
	    dependencies?: maclawAppInstallPlanDependency[];
	    version_snapshot?: maclawAppInstallVersionSnapshot;
	    workflow_contract?: Record<string, any>;
	    workspace_layout?: Record<string, any>;
	    result_contract?: Record<string, any>;
	    review_evidence?: Record<string, any>;
	    submission?: Record<string, any>;
	    test_evidence?: Record<string, any>;
	    dependency_verification?: Record<string, any>;
	    datasrv_registration?: Record<string, any>;
	    has_missing_required: boolean;
	    has_blocking_dependency?: boolean;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppInstallRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app_id = source["app_id"];
	        this.app_name = source["app_name"];
	        this.kind = source["kind"];
	        this.source = source["source"];
	        this.installed_at = source["installed_at"];
	        this.package_sha256 = source["package_sha256"];
	        this.package_bytes = source["package_bytes"];
	        this.package = source["package"];
	        this.dependencies = this.convertValues(source["dependencies"], maclawAppInstallPlanDependency);
	        this.version_snapshot = this.convertValues(source["version_snapshot"], maclawAppInstallVersionSnapshot);
	        this.workflow_contract = source["workflow_contract"];
	        this.workspace_layout = source["workspace_layout"];
	        this.result_contract = source["result_contract"];
	        this.review_evidence = source["review_evidence"];
	        this.submission = source["submission"];
	        this.test_evidence = source["test_evidence"];
	        this.dependency_verification = source["dependency_verification"];
	        this.datasrv_registration = source["datasrv_registration"];
	        this.has_missing_required = source["has_missing_required"];
	        this.has_blocking_dependency = source["has_blocking_dependency"];
	        this.message = source["message"];
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
	
	
	
	export class maclawAppRunHistoryEntry {
	    runID: string;
	    appID: string;
	    status: string;
	    definitionHash?: string;
	    testProtocolFingerprint?: string;
	    workspaceLayoutFingerprint?: string;
	    outputMode?: string;
	    inputSummary?: string;
	    message?: string;
	    artifactID?: string;
	    artifactURI?: string;
	    artifactName?: string;
	    artifactPath?: string;
	    artifactDownloadState?: string;
	    artifacts?: any[];
	    resultPayload?: Record<string, any>;
	    outputs?: any[];
	    resultCoverage?: Record<string, any>;
	    dependencyVerification?: Record<string, any>;
	    approvalInstance?: Record<string, any>;
	    skillName?: string;
	    governanceRecorded?: boolean;
	    governanceError?: string;
	    source?: string;
	    at: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppRunHistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runID = source["runID"];
	        this.appID = source["appID"];
	        this.status = source["status"];
	        this.definitionHash = source["definitionHash"];
	        this.testProtocolFingerprint = source["testProtocolFingerprint"];
	        this.workspaceLayoutFingerprint = source["workspaceLayoutFingerprint"];
	        this.outputMode = source["outputMode"];
	        this.inputSummary = source["inputSummary"];
	        this.message = source["message"];
	        this.artifactID = source["artifactID"];
	        this.artifactURI = source["artifactURI"];
	        this.artifactName = source["artifactName"];
	        this.artifactPath = source["artifactPath"];
	        this.artifactDownloadState = source["artifactDownloadState"];
	        this.artifacts = source["artifacts"];
	        this.resultPayload = source["resultPayload"];
	        this.outputs = source["outputs"];
	        this.resultCoverage = source["resultCoverage"];
	        this.dependencyVerification = source["dependencyVerification"];
	        this.approvalInstance = source["approvalInstance"];
	        this.skillName = source["skillName"];
	        this.governanceRecorded = source["governanceRecorded"];
	        this.governanceError = source["governanceError"];
	        this.source = source["source"];
	        this.at = source["at"];
	    }
	}
	export class maclawAppSubmissionEvent {
	    at: string;
	    status: string;
	    channel: string;
	    submission_id: string;
	    message?: string;
	    reviewer?: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppSubmissionEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.status = source["status"];
	        this.channel = source["channel"];
	        this.submission_id = source["submission_id"];
	        this.message = source["message"];
	        this.reviewer = source["reviewer"];
	    }
	}
	export class maclawAppSubmissionRecord {
	    submission_id: string;
	    hub_capability_id?: string;
	    submitted_at: string;
	    status: string;
	    channel: string;
	    app_ids: string[];
	    app_names?: string[];
	    package_sha256?: string;
	    package_bytes?: number;
	    reviewed_at?: string;
	    published_at?: string;
	    reviewer?: string;
	    risk_level?: string;
	    approved_scopes?: string[];
	    review_issues?: maclawAppReviewIssue[];
	    review_evidence?: Record<string, any>;
	    dependencies?: maclawAppInstallPlanDependency[];
	    events?: maclawAppSubmissionEvent[];
	    package: Record<string, any>;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppSubmissionRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.submission_id = source["submission_id"];
	        this.hub_capability_id = source["hub_capability_id"];
	        this.submitted_at = source["submitted_at"];
	        this.status = source["status"];
	        this.channel = source["channel"];
	        this.app_ids = source["app_ids"];
	        this.app_names = source["app_names"];
	        this.package_sha256 = source["package_sha256"];
	        this.package_bytes = source["package_bytes"];
	        this.reviewed_at = source["reviewed_at"];
	        this.published_at = source["published_at"];
	        this.reviewer = source["reviewer"];
	        this.risk_level = source["risk_level"];
	        this.approved_scopes = source["approved_scopes"];
	        this.review_issues = this.convertValues(source["review_issues"], maclawAppReviewIssue);
	        this.review_evidence = source["review_evidence"];
	        this.dependencies = this.convertValues(source["dependencies"], maclawAppInstallPlanDependency);
	        this.events = this.convertValues(source["events"], maclawAppSubmissionEvent);
	        this.package = source["package"];
	        this.message = source["message"];
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
	export class maclawAppSubmissionStatusUpdate {
	    status: string;
	    hub_capability_id: string;
	    channel: string;
	    message: string;
	    submission_id: string;
	    reviewed_at: string;
	    published_at: string;
	    reviewer: string;
	    risk_level: string;
	    approved_scopes: string[];
	    review_issues: maclawAppReviewIssue[];
	    review_evidence: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppSubmissionStatusUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.hub_capability_id = source["hub_capability_id"];
	        this.channel = source["channel"];
	        this.message = source["message"];
	        this.submission_id = source["submission_id"];
	        this.reviewed_at = source["reviewed_at"];
	        this.published_at = source["published_at"];
	        this.reviewer = source["reviewer"];
	        this.risk_level = source["risk_level"];
	        this.approved_scopes = source["approved_scopes"];
	        this.review_issues = this.convertValues(source["review_issues"], maclawAppReviewIssue);
	        this.review_evidence = source["review_evidence"];
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
	export class maclawAppSubmissionSummary {
	    submission_id: string;
	    hub_capability_id?: string;
	    submitted_at: string;
	    status: string;
	    channel: string;
	    app_ids: string[];
	    app_names?: string[];
	    package_sha?: string;
	    package_sha256?: string;
	    package_bytes?: number;
	    reviewed_at?: string;
	    published_at?: string;
	    reviewer?: string;
	    risk_level?: string;
	    approved_scopes?: string[];
	    review_issues?: maclawAppReviewIssue[];
	    dependencies?: maclawAppInstallPlanDependency[];
	    submission_evidence?: Record<string, any>;
	    review_evidence?: Record<string, any>;
	    event_count?: number;
	    last_event_at?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new maclawAppSubmissionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.submission_id = source["submission_id"];
	        this.hub_capability_id = source["hub_capability_id"];
	        this.submitted_at = source["submitted_at"];
	        this.status = source["status"];
	        this.channel = source["channel"];
	        this.app_ids = source["app_ids"];
	        this.app_names = source["app_names"];
	        this.package_sha = source["package_sha"];
	        this.package_sha256 = source["package_sha256"];
	        this.package_bytes = source["package_bytes"];
	        this.reviewed_at = source["reviewed_at"];
	        this.published_at = source["published_at"];
	        this.reviewer = source["reviewer"];
	        this.risk_level = source["risk_level"];
	        this.approved_scopes = source["approved_scopes"];
	        this.review_issues = this.convertValues(source["review_issues"], maclawAppReviewIssue);
	        this.dependencies = this.convertValues(source["dependencies"], maclawAppInstallPlanDependency);
	        this.submission_evidence = source["submission_evidence"];
	        this.review_evidence = source["review_evidence"];
	        this.event_count = source["event_count"];
	        this.last_event_at = source["last_event_at"];
	        this.message = source["message"];
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
	export class mcpImportSummary {
	    Local: string[];
	    Remote: string[];
	
	    static createFrom(source: any = {}) {
	        return new mcpImportSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Local = source["Local"];
	        this.Remote = source["Remote"];
	    }
	}
	
	export class skillPackageQualitySummary {
	    files: number;
	    has_skill_yaml: boolean;
	    has_skill_definition: boolean;
	    has_skill_md: boolean;
	    referenced_missing?: string[];
	
	    static createFrom(source: any = {}) {
	        return new skillPackageQualitySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = source["files"];
	        this.has_skill_yaml = source["has_skill_yaml"];
	        this.has_skill_definition = source["has_skill_definition"];
	        this.has_skill_md = source["has_skill_md"];
	        this.referenced_missing = source["referenced_missing"];
	    }
	}
	export class persistedSkillQualityStatus {
	    skill_name: string;
	    stage: string;
	    score: number;
	    market_ready: boolean;
	    min_market_score: number;
	    reasons?: string[];
	    portability_summary: skill.IssueSummary;
	    package_summary: skillPackageQualitySummary;
	    require_runtime_proof: boolean;
	    usage_count: number;
	    success_count: number;
	    failure_count: number;
	    verification_status: string;
	    verification_summary?: string;
	    local_hash?: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new persistedSkillQualityStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skill_name = source["skill_name"];
	        this.stage = source["stage"];
	        this.score = source["score"];
	        this.market_ready = source["market_ready"];
	        this.min_market_score = source["min_market_score"];
	        this.reasons = source["reasons"];
	        this.portability_summary = this.convertValues(source["portability_summary"], skill.IssueSummary);
	        this.package_summary = this.convertValues(source["package_summary"], skillPackageQualitySummary);
	        this.require_runtime_proof = source["require_runtime_proof"];
	        this.usage_count = source["usage_count"];
	        this.success_count = source["success_count"];
	        this.failure_count = source["failure_count"];
	        this.verification_status = source["verification_status"];
	        this.verification_summary = source["verification_summary"];
	        this.local_hash = source["local_hash"];
	        this.updated_at = source["updated_at"];
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
	
	export class userDataMigrationJob {
	    id: string;
	    kind: string;
	    status: string;
	    progress: number;
	    progress_text?: string;
	    error?: string;
	    result?: Record<string, any>;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    // Go type: time
	    completed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new userDataMigrationJob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.progress_text = source["progress_text"];
	        this.error = source["error"];
	        this.result = source["result"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.completed_at = this.convertValues(source["completed_at"], null);
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
	export class userDataMigrationStatus {
	    configured: boolean;
	    hub_url?: string;
	    tenant_id?: string;
	    tenant_name?: string;
	    user_id?: string;
	    email?: string;
	    machine_id?: string;
	    machine_name?: string;
	    max_compressed_bytes?: number;
	    current_export?: any;
	    configuration_reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new userDataMigrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.hub_url = source["hub_url"];
	        this.tenant_id = source["tenant_id"];
	        this.tenant_name = source["tenant_name"];
	        this.user_id = source["user_id"];
	        this.email = source["email"];
	        this.machine_id = source["machine_id"];
	        this.machine_name = source["machine_name"];
	        this.max_compressed_bytes = source["max_compressed_bytes"];
	        this.current_export = source["current_export"];
	        this.configuration_reason = source["configuration_reason"];
	    }
	}

}

export namespace memory {
	
	export class BackupInfo {
	    name: string;
	    created_at: string;
	    size_bytes: number;
	    entry_count: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.created_at = source["created_at"];
	        this.size_bytes = source["size_bytes"];
	        this.entry_count = source["entry_count"];
	    }
	}
	export class CompressResult {
	    backup_name: string;
	    total_entries: number;
	    dedup_count: number;
	    merged_count: number;
	    compressed_count: number;
	    skipped_count: number;
	    error_count: number;
	    saved_chars: number;
	
	    static createFrom(source: any = {}) {
	        return new CompressResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backup_name = source["backup_name"];
	        this.total_entries = source["total_entries"];
	        this.dedup_count = source["dedup_count"];
	        this.merged_count = source["merged_count"];
	        this.compressed_count = source["compressed_count"];
	        this.skipped_count = source["skipped_count"];
	        this.error_count = source["error_count"];
	        this.saved_chars = source["saved_chars"];
	    }
	}
	export class CompressorStatus {
	    running: boolean;
	    last_run?: string;
	    last_result?: CompressResult;
	    last_error?: string;
	
	    static createFrom(source: any = {}) {
	        return new CompressorStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.last_run = source["last_run"];
	        this.last_result = this.convertValues(source["last_result"], CompressResult);
	        this.last_error = source["last_error"];
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
	export class StabilityMeta {
	    stability?: string;
	    confirm_count?: number;
	    contradict_count?: number;
	    // Go type: time
	    last_verified_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new StabilityMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stability = source["stability"];
	        this.confirm_count = source["confirm_count"];
	        this.contradict_count = source["contradict_count"];
	        this.last_verified_at = this.convertValues(source["last_verified_at"], null);
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
	export class VersionSnapshot {
	    content: string;
	    // Go type: time
	    timestamp: any;
	
	    static createFrom(source: any = {}) {
	        return new VersionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
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
	export class TimeInterval {
	    // Go type: time
	    start: any;
	    // Go type: time
	    end: any;
	
	    static createFrom(source: any = {}) {
	        return new TimeInterval(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = this.convertValues(source["start"], null);
	        this.end = this.convertValues(source["end"], null);
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
	export class MemoryBoundary {
	    project_path?: string;
	    owner_id?: string;
	    task_type?: string;
	    workflow?: string;
	    toolchain?: string;
	    source_scope?: string;
	    // Go type: time
	    since?: any;
	    // Go type: time
	    until?: any;
	
	    static createFrom(source: any = {}) {
	        return new MemoryBoundary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_path = source["project_path"];
	        this.owner_id = source["owner_id"];
	        this.task_type = source["task_type"];
	        this.workflow = source["workflow"];
	        this.toolchain = source["toolchain"];
	        this.source_scope = source["source_scope"];
	        this.since = this.convertValues(source["since"], null);
	        this.until = this.convertValues(source["until"], null);
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
	export class RelatedEdge {
	    id: string;
	    strength?: number;
	    link_type?: string;
	    // Go type: time
	    updated_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new RelatedEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.strength = source["strength"];
	        this.link_type = source["link_type"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class Entry {
	    id: string;
	    content: string;
	    title?: string;
	    category: string;
	    tags: string[];
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    access_count: number;
	    embedding?: number[];
	    related_ids?: string[];
	    related_edges?: RelatedEdge[];
	    strength?: number;
	    status?: string;
	    scope?: string;
	    pinned?: boolean;
	    compact_form?: string;
	    evidence_ids?: string[];
	    derived_kind?: string;
	    boundary?: MemoryBoundary;
	    level?: number;
	    interval?: TimeInterval;
	    parent_id?: string;
	    child_ids?: string[];
	    source_url?: string;
	    source_type?: string;
	    content_hash?: string;
	    versions?: VersionSnapshot[];
	    stale?: boolean;
	    // Go type: time
	    valid_at?: any;
	    // Go type: time
	    invalid_at?: any;
	    entities?: string[];
	    owner_id?: string;
	    version?: number;
	    stability_meta?: StabilityMeta;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.content = source["content"];
	        this.title = source["title"];
	        this.category = source["category"];
	        this.tags = source["tags"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.access_count = source["access_count"];
	        this.embedding = source["embedding"];
	        this.related_ids = source["related_ids"];
	        this.related_edges = this.convertValues(source["related_edges"], RelatedEdge);
	        this.strength = source["strength"];
	        this.status = source["status"];
	        this.scope = source["scope"];
	        this.pinned = source["pinned"];
	        this.compact_form = source["compact_form"];
	        this.evidence_ids = source["evidence_ids"];
	        this.derived_kind = source["derived_kind"];
	        this.boundary = this.convertValues(source["boundary"], MemoryBoundary);
	        this.level = source["level"];
	        this.interval = this.convertValues(source["interval"], TimeInterval);
	        this.parent_id = source["parent_id"];
	        this.child_ids = source["child_ids"];
	        this.source_url = source["source_url"];
	        this.source_type = source["source_type"];
	        this.content_hash = source["content_hash"];
	        this.versions = this.convertValues(source["versions"], VersionSnapshot);
	        this.stale = source["stale"];
	        this.valid_at = this.convertValues(source["valid_at"], null);
	        this.invalid_at = this.convertValues(source["invalid_at"], null);
	        this.entities = source["entities"];
	        this.owner_id = source["owner_id"];
	        this.version = source["version"];
	        this.stability_meta = this.convertValues(source["stability_meta"], StabilityMeta);
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
	export class ProtectedExperienceCandidate {
	    id: string;
	    title?: string;
	    summary?: string;
	    category?: string;
	    source?: string;
	    reason?: string;
	    tags?: string[];
	    strength?: number;
	    pinned?: boolean;
	    updated_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProtectedExperienceCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.category = source["category"];
	        this.source = source["source"];
	        this.reason = source["reason"];
	        this.tags = source["tags"];
	        this.strength = source["strength"];
	        this.pinned = source["pinned"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ExperienceDistillResult {
	    scanned_entries: number;
	    active_entries: number;
	    source_counts?: Record<string, number>;
	    protected_candidates: number;
	    protected_reason_counts?: Record<string, number>;
	    protected_source_counts?: Record<string, number>;
	    protected_samples?: ProtectedExperienceCandidate[];
	    layered_recommended: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExperienceDistillResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scanned_entries = source["scanned_entries"];
	        this.active_entries = source["active_entries"];
	        this.source_counts = source["source_counts"];
	        this.protected_candidates = source["protected_candidates"];
	        this.protected_reason_counts = source["protected_reason_counts"];
	        this.protected_source_counts = source["protected_source_counts"];
	        this.protected_samples = this.convertValues(source["protected_samples"], ProtectedExperienceCandidate);
	        this.layered_recommended = source["layered_recommended"];
	        this.reason = source["reason"];
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
	export class HealthReport {
	    active_entries: number;
	    max_capacity: number;
	    capacity_percent: number;
	    archived_entries: number;
	    stale_entries: number;
	    orphan_entries: number;
	    no_embedding: number;
	    no_hash: number;
	    pinned_entries: number;
	    embedder_active: boolean;
	    category_counts: Record<string, number>;
	    avg_access_count: number;
	    oldest_entry?: string;
	    newest_entry?: string;
	    versioned_entries: number;
	
	    static createFrom(source: any = {}) {
	        return new HealthReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active_entries = source["active_entries"];
	        this.max_capacity = source["max_capacity"];
	        this.capacity_percent = source["capacity_percent"];
	        this.archived_entries = source["archived_entries"];
	        this.stale_entries = source["stale_entries"];
	        this.orphan_entries = source["orphan_entries"];
	        this.no_embedding = source["no_embedding"];
	        this.no_hash = source["no_hash"];
	        this.pinned_entries = source["pinned_entries"];
	        this.embedder_active = source["embedder_active"];
	        this.category_counts = source["category_counts"];
	        this.avg_access_count = source["avg_access_count"];
	        this.oldest_entry = source["oldest_entry"];
	        this.newest_entry = source["newest_entry"];
	        this.versioned_entries = source["versioned_entries"];
	    }
	}
	export class InferenceDiagnosticsFact {
	    subject: string;
	    predicate: string;
	    object: string;
	    rule_name: string;
	    confidence: number;
	    explanation: string;
	    source_count: number;
	
	    static createFrom(source: any = {}) {
	        return new InferenceDiagnosticsFact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.subject = source["subject"];
	        this.predicate = source["predicate"];
	        this.object = source["object"];
	        this.rule_name = source["rule_name"];
	        this.confidence = source["confidence"];
	        this.explanation = source["explanation"];
	        this.source_count = source["source_count"];
	    }
	}
	export class InferenceDiagnosticsRule {
	    name: string;
	    type: string;
	    relation?: string;
	    input_relation1?: string;
	    input_relation2?: string;
	    output_relation?: string;
	    max_chain_length?: number;
	    confidence_decay: number;
	
	    static createFrom(source: any = {}) {
	        return new InferenceDiagnosticsRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.relation = source["relation"];
	        this.input_relation1 = source["input_relation1"];
	        this.input_relation2 = source["input_relation2"];
	        this.output_relation = source["output_relation"];
	        this.max_chain_length = source["max_chain_length"];
	        this.confidence_decay = source["confidence_decay"];
	    }
	}
	export class InferenceDiagnosticsData {
	    engine_active: boolean;
	    rule_count: number;
	    rules: InferenceDiagnosticsRule[];
	    last_derived: InferenceDiagnosticsFact[];
	    semantic_facts: number;
	    semantic_entities: number;
	
	    static createFrom(source: any = {}) {
	        return new InferenceDiagnosticsData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine_active = source["engine_active"];
	        this.rule_count = source["rule_count"];
	        this.rules = this.convertValues(source["rules"], InferenceDiagnosticsRule);
	        this.last_derived = this.convertValues(source["last_derived"], InferenceDiagnosticsFact);
	        this.semantic_facts = source["semantic_facts"];
	        this.semantic_entities = source["semantic_entities"];
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

export namespace moa {
	
	export class Stats {
	    fanouts: number;
	    ref_ok: number;
	    ref_fail: number;
	    total_ref_ms: number;
	    by_preset?: Record<string, number>;
	    last_preset?: string;
	    last_ms?: number;
	    last_ref_ok?: number;
	    last_ref_fail?: number;
	    updated_at?: string;
	    path?: string;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fanouts = source["fanouts"];
	        this.ref_ok = source["ref_ok"];
	        this.ref_fail = source["ref_fail"];
	        this.total_ref_ms = source["total_ref_ms"];
	        this.by_preset = source["by_preset"];
	        this.last_preset = source["last_preset"];
	        this.last_ms = source["last_ms"];
	        this.last_ref_ok = source["last_ref_ok"];
	        this.last_ref_fail = source["last_ref_fail"];
	        this.updated_at = source["updated_at"];
	        this.path = source["path"];
	    }
	}

}

export namespace oauth {
	
	export class UsageInfo {
	    total_granted: number;
	    total_used: number;
	    total_available: number;
	
	    static createFrom(source: any = {}) {
	        return new UsageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_granted = source["total_granted"];
	        this.total_used = source["total_used"];
	        this.total_available = source["total_available"];
	    }
	}

}

export namespace petpack {
	
	export class PackInfo {
	    id: string;
	    name: string;
	    version: string;
	    author: string;
	    scope: string;
	    status: string;
	    error?: string;
	    tier: string;
	    tone: string;
	    label?: Record<string, string>;
	    description?: string;
	    description_i18n?: Record<string, string>;
	    variants: string[];
	    default_size: number;
	    face_overlay: boolean;
	    preview_path?: string;
	    dir?: string;
	    can_uninstall: boolean;
	    source: string;
	    has_preview: boolean;
	    renderer?: string;
	
	    static createFrom(source: any = {}) {
	        return new PackInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.author = source["author"];
	        this.scope = source["scope"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.tier = source["tier"];
	        this.tone = source["tone"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.description_i18n = source["description_i18n"];
	        this.variants = source["variants"];
	        this.default_size = source["default_size"];
	        this.face_overlay = source["face_overlay"];
	        this.preview_path = source["preview_path"];
	        this.dir = source["dir"];
	        this.can_uninstall = source["can_uninstall"];
	        this.source = source["source"];
	        this.has_preview = source["has_preview"];
	        this.renderer = source["renderer"];
	    }
	}

}

export namespace progress {
	
	export class CorrectionOption {
	    label: string;
	    action: string;
	
	    static createFrom(source: any = {}) {
	        return new CorrectionOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.action = source["action"];
	    }
	}

}

export namespace pyenv {
	
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
	    stage?: string;
	    used_by?: string[];
	    created_at?: string;
	    updated_at?: string;
	    last_used_at?: string;
	    has_lock: boolean;
	    has_python: boolean;
	    has_pip: boolean;
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
	        this.stage = source["stage"];
	        this.used_by = source["used_by"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.last_used_at = source["last_used_at"];
	        this.has_lock = source["has_lock"];
	        this.has_python = source["has_python"];
	        this.has_pip = source["has_pip"];
	        this.error = source["error"];
	    }
	}

}

export namespace remote {
	
	export class SessionTemplate {
	    name: string;
	    tool: string;
	    project_path: string;
	    model_config: string;
	    yolo_mode: boolean;
	    env_vars?: Record<string, string>;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.tool = source["tool"];
	        this.project_path = source["project_path"];
	        this.model_config = source["model_config"];
	        this.yolo_mode = source["yolo_mode"];
	        this.env_vars = source["env_vars"];
	        this.created_at = source["created_at"];
	    }
	}

}

export namespace scheduler {
	
	export class DeliveryTarget {
	    kind: string;
	    group_id?: string;
	    group_name?: string;
	    user_id?: string;
	    mention_user_ids?: string[];
	    mention_all?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeliveryTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.group_id = source["group_id"];
	        this.group_name = source["group_name"];
	        this.user_id = source["user_id"];
	        this.mention_user_ids = source["mention_user_ids"];
	        this.mention_all = source["mention_all"];
	    }
	}
	export class TaskDelivery {
	    enabled: boolean;
	    channel?: string;
	    targets?: DeliveryTarget[];
	    on?: string;
	    prefix?: string;
	    fail_on_error?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TaskDelivery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.channel = source["channel"];
	        this.targets = this.convertValues(source["targets"], DeliveryTarget);
	        this.on = source["on"];
	        this.prefix = source["prefix"];
	        this.fail_on_error = source["fail_on_error"];
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
	export class ScheduledTask {
	    id: string;
	    name: string;
	    action: string;
	    hour: number;
	    minute: number;
	    day_of_week: number;
	    day_of_month: number;
	    interval_minutes?: number;
	    start_date?: string;
	    end_date?: string;
	    task_type?: string;
	    delivery?: TaskDelivery;
	    status: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    last_run_at?: any;
	    // Go type: time
	    next_run_at?: any;
	    run_count: number;
	    last_result?: string;
	    last_error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScheduledTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.action = source["action"];
	        this.hour = source["hour"];
	        this.minute = source["minute"];
	        this.day_of_week = source["day_of_week"];
	        this.day_of_month = source["day_of_month"];
	        this.interval_minutes = source["interval_minutes"];
	        this.start_date = source["start_date"];
	        this.end_date = source["end_date"];
	        this.task_type = source["task_type"];
	        this.delivery = this.convertValues(source["delivery"], TaskDelivery);
	        this.status = source["status"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.last_run_at = this.convertValues(source["last_run_at"], null);
	        this.next_run_at = this.convertValues(source["next_run_at"], null);
	        this.run_count = source["run_count"];
	        this.last_result = source["last_result"];
	        this.last_error = source["last_error"];
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

export namespace security {
	
	export class AuditEntry {
	    // Go type: time
	    timestamp: any;
	    user_id: string;
	    session_id: string;
	    action?: string;
	    tool_name: string;
	    arguments: Record<string, any>;
	    risk_level: string;
	    policy_action: string;
	    result: string;
	    source?: string;
	    sensitive_detected?: boolean;
	    sensitive_categories?: string[];
	    output_snippet?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuditEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.user_id = source["user_id"];
	        this.session_id = source["session_id"];
	        this.action = source["action"];
	        this.tool_name = source["tool_name"];
	        this.arguments = source["arguments"];
	        this.risk_level = source["risk_level"];
	        this.policy_action = source["policy_action"];
	        this.result = source["result"];
	        this.source = source["source"];
	        this.sensitive_detected = source["sensitive_detected"];
	        this.sensitive_categories = source["sensitive_categories"];
	        this.output_snippet = source["output_snippet"];
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
	export class AuditFilter {
	    // Go type: time
	    StartTime?: any;
	    // Go type: time
	    EndTime?: any;
	    Action: string;
	    ToolName: string;
	    RiskLevels: string[];
	
	    static createFrom(source: any = {}) {
	        return new AuditFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.StartTime = this.convertValues(source["StartTime"], null);
	        this.EndTime = this.convertValues(source["EndTime"], null);
	        this.Action = source["Action"];
	        this.ToolName = source["ToolName"];
	        this.RiskLevels = source["RiskLevels"];
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

export namespace session {
	
	export class SearchResult {
	    session_id: string;
	    timestamp: string;
	    platform: string;
	    topic: string;
	    snippet: string;
	    rank: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.timestamp = source["timestamp"];
	        this.platform = source["platform"];
	        this.topic = source["topic"];
	        this.snippet = source["snippet"];
	        this.rank = source["rank"];
	    }
	}
	export class SessionSummary {
	    session_id: string;
	    timestamp: string;
	    platform: string;
	    topic: string;
	    text_len: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.timestamp = source["timestamp"];
	        this.platform = source["platform"];
	        this.topic = source["topic"];
	        this.text_len = source["text_len"];
	    }
	}

}

export namespace skill {
	
	export class IssueSummary {
	    errors: number;
	    warnings: number;
	    infos: number;
	
	    static createFrom(source: any = {}) {
	        return new IssueSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.errors = source["errors"];
	        this.warnings = source["warnings"];
	        this.infos = source["infos"];
	    }
	}
	export class MaintenanceExperienceHint {
	    action: string;
	    skill: string;
	    related_skill?: string;
	    risk?: string;
	    reason?: string;
	    recommended_action?: string;
	    evidence?: string[];
	    high_value: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MaintenanceExperienceHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.skill = source["skill"];
	        this.related_skill = source["related_skill"];
	        this.risk = source["risk"];
	        this.reason = source["reason"];
	        this.recommended_action = source["recommended_action"];
	        this.evidence = source["evidence"];
	        this.high_value = source["high_value"];
	    }
	}

}

export namespace swarm {
	
	export class ProjectState {
	    had_git_repo: boolean;
	    had_commits: boolean;
	    stash_created: boolean;
	    original_branch: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.had_git_repo = source["had_git_repo"];
	        this.had_commits = source["had_commits"];
	        this.stash_created = source["stash_created"];
	        this.original_branch = source["original_branch"];
	    }
	}
	export class SubTask {
	    index: number;
	    description: string;
	    expected_files: string[];
	    dependencies: number[];
	    group_id: number;
	    acceptance_criteria?: string[];
	    test_file?: string;
	    test_code?: string;
	
	    static createFrom(source: any = {}) {
	        return new SubTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.description = source["description"];
	        this.expected_files = source["expected_files"];
	        this.dependencies = source["dependencies"];
	        this.group_id = source["group_id"];
	        this.acceptance_criteria = source["acceptance_criteria"];
	        this.test_file = source["test_file"];
	        this.test_code = source["test_code"];
	    }
	}
	export class SwarmAgent {
	    id: string;
	    role: string;
	    session_id: string;
	    task_index: number;
	    worktree_path: string;
	    branch_name: string;
	    status: string;
	    retry_count: number;
	    output?: string;
	    error?: string;
	    // Go type: time
	    started_at?: any;
	    // Go type: time
	    completed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new SwarmAgent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.session_id = source["session_id"];
	        this.task_index = source["task_index"];
	        this.worktree_path = source["worktree_path"];
	        this.branch_name = source["branch_name"];
	        this.status = source["status"];
	        this.retry_count = source["retry_count"];
	        this.output = source["output"];
	        this.error = source["error"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.completed_at = this.convertValues(source["completed_at"], null);
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
	export class SwarmRound {
	    number: number;
	    reason: string;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    ended_at?: any;
	    result: string;
	
	    static createFrom(source: any = {}) {
	        return new SwarmRound(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.reason = source["reason"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.ended_at = this.convertValues(source["ended_at"], null);
	        this.result = source["result"];
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
	export class TimelineEvent {
	    // Go type: time
	    timestamp: any;
	    type: string;
	    message: string;
	    agent_id?: string;
	    phase?: string;
	
	    static createFrom(source: any = {}) {
	        return new TimelineEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.type = source["type"];
	        this.message = source["message"];
	        this.agent_id = source["agent_id"];
	        this.phase = source["phase"];
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
	export class TaskGroup {
	    id: number;
	    task_indices: number[];
	    conflict_files: string[];
	
	    static createFrom(source: any = {}) {
	        return new TaskGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.task_indices = source["task_indices"];
	        this.conflict_files = source["conflict_files"];
	    }
	}
	export class SwarmRun {
	    run_id: string;
	    mode: string;
	    status: string;
	    phase: string;
	    project_path: string;
	    tech_stack?: string;
	    tool: string;
	    requirements?: string;
	    design_doc?: string;
	    tasks: SubTask[];
	    task_groups?: TaskGroup[];
	    agents: SwarmAgent[];
	    current_round: number;
	    max_rounds: number;
	    round_history: SwarmRound[];
	    project_state?: ProjectState;
	    timeline: TimelineEvent[];
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    // Go type: time
	    completed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new SwarmRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.mode = source["mode"];
	        this.status = source["status"];
	        this.phase = source["phase"];
	        this.project_path = source["project_path"];
	        this.tech_stack = source["tech_stack"];
	        this.tool = source["tool"];
	        this.requirements = source["requirements"];
	        this.design_doc = source["design_doc"];
	        this.tasks = this.convertValues(source["tasks"], SubTask);
	        this.task_groups = this.convertValues(source["task_groups"], TaskGroup);
	        this.agents = this.convertValues(source["agents"], SwarmAgent);
	        this.current_round = source["current_round"];
	        this.max_rounds = source["max_rounds"];
	        this.round_history = this.convertValues(source["round_history"], SwarmRound);
	        this.project_state = this.convertValues(source["project_state"], ProjectState);
	        this.timeline = this.convertValues(source["timeline"], TimelineEvent);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.completed_at = this.convertValues(source["completed_at"], null);
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
	export class TaskListInput {
	    source: string;
	    text?: string;
	    url?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskListInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.text = source["text"];
	        this.url = source["url"];
	    }
	}
	export class SwarmRunRequest {
	    mode: string;
	    project_path: string;
	    requirements?: string;
	    tech_stack?: string;
	    task_input?: TaskListInput;
	    max_agents?: number;
	    max_rounds?: number;
	    tool: string;
	
	    static createFrom(source: any = {}) {
	        return new SwarmRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.project_path = source["project_path"];
	        this.requirements = source["requirements"];
	        this.tech_stack = source["tech_stack"];
	        this.task_input = this.convertValues(source["task_input"], TaskListInput);
	        this.max_agents = source["max_agents"];
	        this.max_rounds = source["max_rounds"];
	        this.tool = source["tool"];
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
	export class SwarmRunSummary {
	    run_id: string;
	    mode: string;
	    status: string;
	    phase: string;
	    task_count: number;
	    current_round: number;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SwarmRunSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.mode = source["mode"];
	        this.status = source["status"];
	        this.phase = source["phase"];
	        this.task_count = source["task_count"];
	        this.current_round = source["current_round"];
	        this.created_at = this.convertValues(source["created_at"], null);
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

export namespace tool {
	
	export class RoutingHintAdjustmentExplanation {
	    tool_name: string;
	    query_tokens?: string[];
	    adjustment: number;
	    direction: string;
	    matching_records: number;
	    successes?: number;
	    failures?: number;
	    recovery_evidence?: number;
	    success_rate?: number;
	    failure_rate?: number;
	    reasons?: string[];
	
	    static createFrom(source: any = {}) {
	        return new RoutingHintAdjustmentExplanation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_name = source["tool_name"];
	        this.query_tokens = source["query_tokens"];
	        this.adjustment = source["adjustment"];
	        this.direction = source["direction"];
	        this.matching_records = source["matching_records"];
	        this.successes = source["successes"];
	        this.failures = source["failures"];
	        this.recovery_evidence = source["recovery_evidence"];
	        this.success_rate = source["success_rate"];
	        this.failure_rate = source["failure_rate"];
	        this.reasons = source["reasons"];
	    }
	}
	export class ToolRecoveryPattern {
	    context_key: string;
	    task_type?: string;
	    query_tokens?: string[];
	    failed_tool: string;
	    error_class?: string;
	    recovery_tool: string;
	    tool_sequence?: string[];
	    evidence: number;
	    success_rate: number;
	    confidence: number;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolRecoveryPattern(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context_key = source["context_key"];
	        this.task_type = source["task_type"];
	        this.query_tokens = source["query_tokens"];
	        this.failed_tool = source["failed_tool"];
	        this.error_class = source["error_class"];
	        this.recovery_tool = source["recovery_tool"];
	        this.tool_sequence = source["tool_sequence"];
	        this.evidence = source["evidence"];
	        this.success_rate = source["success_rate"];
	        this.confidence = source["confidence"];
	        this.description = source["description"];
	    }
	}
	export class ToolRoutingHint {
	    context_key: string;
	    task_type?: string;
	    query_tokens?: string[];
	    prefer_tools?: string[];
	    avoid_tools?: string[];
	    recovery_tools?: string[];
	    evidence: number;
	    confidence: number;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolRoutingHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context_key = source["context_key"];
	        this.task_type = source["task_type"];
	        this.query_tokens = source["query_tokens"];
	        this.prefer_tools = source["prefer_tools"];
	        this.avoid_tools = source["avoid_tools"];
	        this.recovery_tools = source["recovery_tools"];
	        this.evidence = source["evidence"];
	        this.confidence = source["confidence"];
	        this.description = source["description"];
	    }
	}
	export class ToolSkillNudgeCandidate {
	    context_key: string;
	    task_type?: string;
	    query_tokens?: string[];
	    tool_sequence: string[];
	    evidence: number;
	    success_rate: number;
	    confidence: number;
	    suggested_name?: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolSkillNudgeCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context_key = source["context_key"];
	        this.task_type = source["task_type"];
	        this.query_tokens = source["query_tokens"];
	        this.tool_sequence = source["tool_sequence"];
	        this.evidence = source["evidence"];
	        this.success_rate = source["success_rate"];
	        this.confidence = source["confidence"];
	        this.suggested_name = source["suggested_name"];
	        this.description = source["description"];
	    }
	}
	export class UsagePattern {
	    tool_name: string;
	    top_tokens: string[];
	    success_rate: number;
	    count: number;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new UsagePattern(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_name = source["tool_name"];
	        this.top_tokens = source["top_tokens"];
	        this.success_rate = source["success_rate"];
	        this.count = source["count"];
	        this.description = source["description"];
	    }
	}
	export class UsageTracker {
	    FingerprintProviders: any[];
	
	    static createFrom(source: any = {}) {
	        return new UsageTracker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FingerprintProviders = source["FingerprintProviders"];
	    }
	}

}
