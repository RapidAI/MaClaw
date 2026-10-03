package guiapp

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// refuseUnpublishedFileDestination stops a turn whose whole utterance names a
// file destination this origin cannot publish. The refusal is semantic-owned,
// so the turn does not generate a document and does not offer im_message.
func refuseUnpublishedFileDestination(msg IMUserMessage) (*IMAgentResponse, bool) {
	dest, ok := agentruntime.ResolveHostFileDestination(msg.Text)
	if !ok {
		return nil, false
	}
	origin := normalizeIMMessagePlatformKind(msg.Platform)
	if agentruntime.SemanticCrossChannelFileDeliveryPublished(origin.String(), dest.String()) {
		return nil, false
	}
	scope := dest.ChannelScope()
	text := unfinishedSlotText(msg.Lang,
		fmt.Sprintf("This channel cannot deliver a file to %s. This turn will not generate a new document and will not hand a path to im_message.", scope),
		fmt.Sprintf("当前通道不能把文件发到 %s。这一轮不会生成新文档，也不会把文件路径交给 im_message。", scope),
		fmt.Sprintf("目前通道不能把檔案發到 %s。這一輪不會生成新文件，也不會把路徑交給 im_message。", scope),
	)
	return &IMAgentResponse{
		Text:           text,
		Error:          "semantic_delivery_channel_unsupported",
		ResponseSource: "semantic_host_reject",
	}, true
}
