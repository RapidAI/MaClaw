package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

type srvIMFileSendFunc func(context.Context, *agentservice.Service, agentservice.Principal, string, scheduler.DeliveryTarget, []byte, string, string, string) error

// newSrvIMFileHandler materializes a workspace file. Explicit IM targets are
// delivered by MaClawSrv itself; untargeted/current-client files retain the
// structured artifact envelope for the caller to consume.
func newSrvIMFileHandler(svc *agentservice.Service) func(args map[string]interface{}) string {
	h := newSrvIMFileHandlerContext(svc)
	return func(args map[string]interface{}) string {
		return h(context.Background(), agentservice.Principal{TenantID: "system", UserID: "scheduler"}, args)
	}
}

func newSrvIMFileHandlerWithSender(svc *agentservice.Service, send srvIMFileSendFunc) func(args map[string]interface{}) string {
	h := newSrvIMFileHandlerContextWithSender(svc, send)
	return func(args map[string]interface{}) string {
		return h(context.Background(), agentservice.Principal{TenantID: "system", UserID: "scheduler"}, args)
	}
}

func newSrvIMFileHandlerContext(svc *agentservice.Service) func(context.Context, agentservice.Principal, map[string]interface{}) string {
	return newSrvIMFileHandlerContextWithSender(svc, deliverSrvIMFileToTarget)
}

func newSrvIMFileHandlerContextWithSender(svc *agentservice.Service, send srvIMFileSendFunc) func(context.Context, agentservice.Principal, map[string]interface{}) string {
	return func(parent context.Context, principal agentservice.Principal, args map[string]interface{}) string {
		path := strings.TrimSpace(agent.StringArg(args, "path"))
		if path == "" {
			return "Error: missing path parameter"
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Sprintf("Error: file not found or inaccessible: %v", err)
		}
		if info.IsDir() {
			return "Error: path is a directory"
		}
		if info.Size() > agent.SendFileMaxSize {
			return fmt.Sprintf("Error: file too large (%d bytes)", info.Size())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("Error: read failed: %v", err)
		}
		name := strings.TrimSpace(agent.StringArg(args, "file_name"))
		if name == "" {
			name = filepath.Base(path)
		}
		mimeType := mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		target := agent.IMFileDeliveryTargetFromArgs(args)
		forward := false
		if v, ok := args["forward_to_im"].(bool); ok {
			forward = v
		}
		dest := strings.ToLower(strings.TrimSpace(agent.StringArg(args, "destination")))
		if dest == "im" || dest == "wechat" || dest == "weixin" || dest == "feishu" || dest == "qq" || dest == "telegram" || dest == "lansenger" {
			forward = true
		}
		if target.Active() {
			forward = true
		}
		if !forward {
			return fmt.Sprintf("[file_base64|%s|%s]%s", name, mimeType, b64)
		}
		if target.Active() {
			if send == nil {
				return "Error: targeted IM file delivery is not initialized"
			}
			if strings.TrimSpace(target.Channel) == "" {
				return "Error: exact IM file delivery requires channel"
			}
			delivery, err := parseSrvScheduleDelivery(args)
			if err != nil {
				return "Error: " + err.Error()
			}
			if err := resolveSrvScheduleDeliveryForPrincipal(parent, svc, principal, delivery); err != nil {
				return "Error: " + err.Error()
			}
			if delivery == nil || len(delivery.Targets) != 1 {
				return "Error: exact IM file delivery requires one resolved group or user target"
			}
			message := firstNonEmptyString(agent.StringArg(args, "message"), agent.StringArg(args, "caption"))
			ctx, cancel := scheduler.WithDeliveryTimeout(parent, scheduler.DefaultIMDeliveryTimeout)
			defer cancel()
			if err := send(ctx, svc, principal, delivery.Channel, delivery.Targets[0], data, name, mimeType, message); err != nil {
				return "Error: targeted IM file delivery failed: " + err.Error()
			}
			return fmt.Sprintf("File %s sent to %s.", name, scheduler.SummarizeDelivery(delivery))
		}
		return fmt.Sprintf("[file_base64|%s|%s|im]%s", name, mimeType, b64)
	}
}
