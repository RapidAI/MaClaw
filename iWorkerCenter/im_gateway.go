package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/feishu"
	"github.com/RapidAI/CodeClaw/corelib/dingtalk"
	"github.com/RapidAI/CodeClaw/corelib/wecom"
)

// IMGatewayConfig holds configuration for all IM gateways.
type IMGatewayConfig struct {
	Feishu   *feishu.Config   `json:"feishu,omitempty"`
	DingTalk *dingtalk.Config `json:"dingtalk,omitempty"`
	WeCom    *wecom.Config    `json:"wecom,omitempty"`
}

// imRouter is the global IM router for iWorkerCenter.
var imRouter *cim.Router

// setupIMGateways initializes IM gateways from config and registers webhook handlers.
func setupIMGateways(mux *http.ServeMux) *cim.Router {
	router := cim.NewRouter()
	imRouter = router

	config := loadIMConfig()

	// Register Feishu gateway
	if config.Feishu != nil && config.Feishu.AppID != "" {
		gw := feishu.NewGateway(*config.Feishu)
		if err := router.Register(gw); err != nil {
			log.Printf("[iWorkerCenter] feishu register: %v", err)
		} else {
			mux.HandleFunc("/webhook/feishu", gw.WebhookHandler())
			log.Printf("[iWorkerCenter] feishu gateway registered")
		}
	}

	// Register DingTalk gateway
	if config.DingTalk != nil && config.DingTalk.AppKey != "" {
		gw := dingtalk.NewGateway(*config.DingTalk)
		if err := router.Register(gw); err != nil {
			log.Printf("[iWorkerCenter] dingtalk register: %v", err)
		} else {
			mux.HandleFunc("/webhook/dingtalk", gw.WebhookHandler())
			log.Printf("[iWorkerCenter] dingtalk gateway registered")
		}
	}

	// Register WeCom gateway
	if config.WeCom != nil && config.WeCom.CorpID != "" {
		gw := wecom.NewGateway(*config.WeCom)
		if err := router.Register(gw); err != nil {
			log.Printf("[iWorkerCenter] wecom register: %v", err)
		} else {
			mux.HandleFunc("/webhook/wecom", gw.WebhookHandler())
			log.Printf("[iWorkerCenter] wecom gateway registered")
		}
	}

	// Set up unified message handler — converts IM messages to collaboration tasks
	router.OnMessage(func(msg cim.IncomingMessage) {
		log.Printf("[iWorkerCenter] IM message from %s/%s: %s",
			msg.Platform, msg.PlatformUID, truncate(msg.Text, 100))
		handleIMMessage(msg)
	})

	router.StartAll(context.Background())
	return router
}

// handleIMMessage processes an incoming IM message by creating a collaboration task
// or submitting it as a DiWorker task through the LLM proxy.
func handleIMMessage(msg cim.IncomingMessage) {
	if strings.TrimSpace(msg.Text) == "" {
		return
	}

	// For now, reply with a simple acknowledgment via the same platform.
	// In a full implementation, this would:
	// 1. Match the IM user to a colleague
	// 2. Use the recommend engine to find the best colleague
	// 3. Create a collaboration task
	// 4. Submit to LLM and return the result
	if imRouter != nil {
		reply := fmt.Sprintf("收到您的消息：%s\n\n正在处理中，请稍候...", truncate(msg.Text, 200))
		_ = imRouter.SendText(context.Background(), msg.Platform,
			cim.UserTarget{PlatformUID: msg.PlatformUID}, reply)
	}
}

func loadIMConfig() IMGatewayConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return IMGatewayConfig{}
	}
	path := filepath.Join(home, ".iworkercenter", "im_config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return IMGatewayConfig{}
	}
	var config IMGatewayConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("[iWorkerCenter] parse im_config.json: %v", err)
		return IMGatewayConfig{}
	}
	return config
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
