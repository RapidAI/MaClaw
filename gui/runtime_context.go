package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

type RuntimeSourceRef struct {
	Channel  string `json:"channel,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type RuntimeActorRef struct {
	ActorID   string `json:"actor_id,omitempty"`
	ActorType string `json:"actor_type,omitempty"`
}

type RuntimeConversationRef struct {
	ConversationID string `json:"conversation_id,omitempty"`
	SessionKey     string `json:"session_key,omitempty"`
}

type RuntimeParentRef struct {
	ParentRequestID string `json:"parent_request_id,omitempty"`
	ParentTaskID    string `json:"parent_task_id,omitempty"`
	HandoffToken    string `json:"handoff_token,omitempty"`
}

type RuntimeMessagePayload struct {
	Text        string              `json:"text,omitempty"`
	MessageType string              `json:"message_type,omitempty"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

type RuntimeDebugOptions struct {
	Trace bool `json:"trace,omitempty"`
}

type RequestEnvelope struct {
	RequestID    string                 `json:"request_id"`
	Source       RuntimeSourceRef       `json:"source"`
	Actor        RuntimeActorRef        `json:"actor"`
	Conversation RuntimeConversationRef `json:"conversation"`
	Parent       RuntimeParentRef       `json:"parent,omitempty"`
	Payload      RuntimeMessagePayload  `json:"payload"`
	Debug        RuntimeDebugOptions    `json:"debug,omitempty"`
}

type ResponseEnvelope struct {
	RequestID string                 `json:"request_id,omitempty"`
	Source    RuntimeSourceRef       `json:"source,omitempty"`
	Actor     RuntimeActorRef        `json:"actor,omitempty"`
	Session   RuntimeConversationRef `json:"session,omitempty"`
	Text      string                 `json:"text,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

type RuntimeContext struct {
	RequestID       string
	Source          RuntimeSourceRef
	Actor           RuntimeActorRef
	Conversation    RuntimeConversationRef
	PolicyOwnerID   string
	LockKey         string
	WorkflowOwnerID string
	Execution       ExecutionProfile
	SemanticIntent  *intent.ClassificationResult `json:"-"`
	// VisionFallthrough is set when a host-staged image suppressed a lookup or
	// file_read grant. The image bytes stay in user content; the name router
	// must not reintroduce web_search, and the prompt must not describe a
	// closed grant surface whose list is empty.
	VisionFallthrough bool `json:"-"`
	// RoutingMissFallback is a leftover turn after a precise semantic surface
	// missed. Privilege-expanding core tools stay stripped for the whole loop,
	// including injection and skill-recover rebuilds.
	RoutingMissFallback bool `json:"-"`
	// HostAdapterLeftover pins generate_pdf on a published channel when the
	// missed family was a generate primary.
	HostAdapterLeftover bool `json:"-"`
	// Adaptive prompt dual-build estimates (set when light profile is chosen).
	PromptFullTokens  int `json:"-"`
	PromptLightTokens int `json:"-"`
	// PromptABSample is set when ResolvePromptProfile forced full via quality A/B.
	PromptABSample bool `json:"-"`
	// PromptSoftFull is set when SoftFullAgentIntent upgraded light→full.
	PromptSoftFull bool `json:"-"`
}

func runtimeContextFromIMMessage(msg IMUserMessage) RuntimeContext {
	source := runtimeSourceFromIMMessage(msg)
	actor := runtimeActorFromIMMessage(msg)
	conversationID := strings.TrimSpace(msg.UserID)
	if conversationID == "" {
		conversationID = "anonymous"
	}
	sessionKey := runtimeSessionKey(source, conversationID, actor.ActorID)
	requestID := strings.TrimSpace(msg.RequestID)
	if requestID == "" {
		requestID = "req-" + generateID()
	}
	return RuntimeContext{
		RequestID:       requestID,
		Source:          source,
		Actor:           actor,
		Conversation:    RuntimeConversationRef{ConversationID: conversationID, SessionKey: sessionKey},
		PolicyOwnerID:   strings.TrimSpace(msg.UserID),
		LockKey:         sessionKey + ":" + actor.ActorID,
		WorkflowOwnerID: strings.TrimSpace(msg.UserID),
		Execution:       fullExecutionProfile("unclassified"),
	}
}

func requestEnvelopeFromIMMessage(msg IMUserMessage, runtime RuntimeContext) RequestEnvelope {
	return RequestEnvelope{
		RequestID:    runtime.RequestID,
		Source:       runtime.Source,
		Actor:        runtime.Actor,
		Conversation: runtime.Conversation,
		Payload: RuntimeMessagePayload{
			Text:        msg.Text,
			MessageType: msg.MessageType,
			Attachments: msg.Attachments,
		},
	}
}

func runtimeSourceFromIMMessage(msg IMUserMessage) RuntimeSourceRef {
	provider := strings.ToLower(strings.TrimSpace(msg.Platform))
	if provider == "" {
		provider = "local"
	}
	channel := "im"
	switch {
	case msg.IsBackground || isScheduledTaskConversationOwner(msg.UserID):
		channel = "system"
		if provider == "local" || provider == "desktop" {
			provider = "scheduler"
		}
	case provider == "desktop" || strings.HasPrefix(strings.TrimSpace(msg.UserID), desktopUserID):
		channel = "desktop"
	case strings.HasPrefix(provider, "thirdparty") || strings.HasPrefix(strings.TrimSpace(msg.UserID), "thirdparty:"):
		channel = "third_party"
	case strings.HasPrefix(strings.TrimSpace(msg.UserID), "ve-group-executor:"):
		channel = "discussion"
	case provider == "tui":
		channel = "desktop"
	}
	return RuntimeSourceRef{Channel: channel, Provider: provider}
}

func runtimeActorFromIMMessage(msg IMUserMessage) RuntimeActorRef {
	if msg.IsBackground || isScheduledTaskConversationOwner(msg.UserID) {
		return RuntimeActorRef{ActorID: "system", ActorType: "system"}
	}
	if strings.HasPrefix(strings.TrimSpace(msg.UserID), "ve-group-executor:") {
		return RuntimeActorRef{ActorID: "digital-employee", ActorType: "digital_employee"}
	}
	return RuntimeActorRef{ActorID: "main-ai", ActorType: "main_ai"}
}

// isScheduledTaskConversationOwner accepts the historical desktop owner and
// the profile/task-scoped owner used by multi-bot scheduled execution. Runtime
// classification must stay aligned with scheduledTaskConversationOwner: a
// profile task is still a system/background request even though its isolated
// owner no longer equals the legacy literal.
func isScheduledTaskConversationOwner(ownerID string) bool {
	ownerID = strings.TrimSpace(ownerID)
	return ownerID == "scheduled_task" || strings.HasPrefix(ownerID, "lansenger-scheduled:")
}

func runtimeSessionKey(source RuntimeSourceRef, conversationID, actorID string) string {
	channel := strings.TrimSpace(source.Channel)
	if channel == "" {
		channel = "unknown"
	}
	provider := strings.TrimSpace(source.Provider)
	if provider == "" {
		provider = "unknown"
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		conversationID = "anonymous"
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		actorID = "actor"
	}
	return channel + ":" + provider + ":" + conversationID + ":" + actorID
}
