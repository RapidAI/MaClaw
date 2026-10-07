package guiapp

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	petCompanionUserID    = "desktop-pet"
	petCompanionPlatform  = "pet"
	petCompanionActorID   = "pet-companion"
	petCompanionActorType = "pet_companion"
	petCompanionChannel   = "pet"

	petToolModeChat = "chat"

	petCompanionPersona = "你是桌面宠物码卡龙，在用户旁边说话。用一两句口语，先给结果，不要复述指令，不要列表、Markdown 或工具名，不要叠道歉，不要追问还需要什么。闲聊时不要调用工具。删除、外发、执行命令之前，如果用户还没明确答应，先问一句再做。生成文稿后不要自己发到聊天，也不要自己放进移动文稿库，把文稿留在本机，等用户选择。"

	petDocumentQuestion  = "文稿好了。发给聊天，还是放进移动文稿库？"
	petWakeAck           = "主人，我在，有什么吩咐？"
	petWindowConfirmLine = "这步要在主窗口里点一下"
)

var (
	petCompanionToolMode   sync.Map
	petCompanionDeliveryOK sync.Map
)

func petVoiceCompanionArmed(cfg corelib.AppConfig) bool {
	if !cfg.PetEnabled || !cfg.PetVoiceInput {
		return false
	}
	switch strings.TrimSpace(cfg.PetConversationMode) {
	case "voice-turn", "continuous":
		return true
	default:
		return false
	}
}

func petCompanionContinuous(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.PetConversationMode) == "continuous"
}

func setPetCompanionToolMode(userID, mode string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if strings.TrimSpace(mode) == "" {
		petCompanionToolMode.Delete(userID)
		return
	}
	petCompanionToolMode.Store(userID, mode)
}

func petCompanionToolsDisabled(userID string) bool {
	mode, ok := petCompanionToolMode.Load(strings.TrimSpace(userID))
	return ok && mode == petToolModeChat
}

func isPetCompanionUser(userID string) bool {
	return strings.TrimSpace(userID) == petCompanionUserID
}

func setPetCompanionDeliveryArmed(userID string, armed bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if armed {
		petCompanionDeliveryOK.Store(userID, true)
		return
	}
	petCompanionDeliveryOK.Delete(userID)
}

func petCompanionOutboundBlocked(userID, toolName, argsJSON, userText string) bool {
	if !isPetCompanionUser(userID) {
		return false
	}
	if armed, ok := petCompanionDeliveryOK.Load(strings.TrimSpace(userID)); ok {
		if enabled, _ := armed.(bool); enabled {
			return false
		}
	}
	if !petFileDeliveryTool(toolName, argsJSON) {
		return false
	}
	if petCreatesDocument(userText) {
		return true
	}
	if petWantsIM(compactPetSpeech(userText)) {
		return false
	}
	return true
}

func petFileDeliveryTool(name, argsJSON string) bool {
	switch strings.TrimSpace(name) {
	case "send_file", "send_to_im":
		return true
	case "im_message":
		args := strings.ToLower(argsJSON)
		return strings.Contains(args, "send_file") || strings.Contains(args, "\"path\":")
	default:
		return false
	}
}
