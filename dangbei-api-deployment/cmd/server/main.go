package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/dangbei"
	"ds2api/internal/tools"
)

var (
	startTime      = time.Now()
	statsLock      sync.RWMutex
	totalRequests  int
	successCount   int
	errorCount     int
	responseTimes  []int64
	
	// 上游健康状态
	upstreamStatus     = "unknown"
	upstreamLastCheck  time.Time
	upstreamLastOK     time.Time
	upstreamError      string
	upstreamResponseMs int64
	
	// 全局配置
	cfg *config.Config
)

type OpenAIRequest struct {
	Model    string        `json:"model"`
	Messages []Message     `json:"messages"`
	Stream   bool          `json:"stream"`
	Tools    []RequestTool `json:"tools,omitempty"`
}

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 支持 string 或 array
}

type OpenAIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int          `json:"index"`
	Delta        *Delta       `json:"delta,omitempty"`
	Message      *Message     `json:"message,omitempty"`
	FinishReason *string      `json:"finish_reason"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
}

type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function,omitempty"`
}

type ToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type RequestTool struct {
	Type     string              `json:"type"`
	Function RequestToolFunction `json:"function"`
}

type RequestToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

func main() {
	// 配置日志格式
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	
	// 加载配置文件
	var err error
	cfg, err = config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Loaded %d accounts", len(cfg.Accounts))
	
	// 暂时禁用健康检查，避免阻塞
	// go func() {
	// 	time.Sleep(30 * time.Second)
	// 	upstreamHealthCheckLoop()
	// }()
	
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		
		// 统计请求
		statsLock.Lock()
		totalRequests++
		statsLock.Unlock()
		
		// 捕获 panic 防止服务崩溃
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC recovered: %v", err)
				statsLock.Lock()
				errorCount++
				statsLock.Unlock()
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			} else {
				// 记录响应时间
				elapsed := time.Since(startTime).Milliseconds()
				statsLock.Lock()
				successCount++
				responseTimes = append(responseTimes, elapsed)
				if len(responseTimes) > 100 {
					responseTimes = responseTimes[1:]
				}
				statsLock.Unlock()
			}
		}()
		
		// 记录请求
		log.Printf("Incoming request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		
		if r.Method != http.MethodPost {
			log.Printf("Method not allowed: %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse OpenAI request
		var req OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Failed to decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		log.Printf("Request model: %s, messages: %d, stream: %v, tools: %d", req.Model, len(req.Messages), req.Stream, len(req.Tools))

		// 智能构建上下文：保留最近对话历史
		contextMessages := buildContextMessages(req.Messages)
		
		// 注入系统提示（教 GLM-5 如何调用工具）
		contextMessages = injectSystemPrompt(contextMessages, req.Tools)
		
		question := extractLastUserMessage(contextMessages)
		
		if question == "" {
			log.Printf("No user message found in request")
			http.Error(w, "No user message found", http.StatusBadRequest)
			return
		}

		// Map model name to Dangbei model
		dangbeiModel := "deepseek"
		if req.Model == "glm-5" || req.Model == "glm5" {
			dangbeiModel = "glm-5"
		}
		
		log.Printf("Using %d messages as context, last question: %.50s..., model: %s", 
			len(contextMessages), question, dangbeiModel)

		// 获取下一个可用的 token
		token := cfg.GetNextToken()
		if token == "" {
			log.Printf("No available token")
			http.Error(w, "No available account", http.StatusServiceUnavailable)
			return
		}

		// Create Dangbei client
		client := dangbei.NewClient(token)
		
		// 构建带上下文的问题
		contextPrefix := formatContextForDangbei(contextMessages)
		fullQuestion := contextPrefix + question
		
		// 防止超长：如果问题大于 5000 字符，截断上下文只保留问题本身
		if len(fullQuestion) > 5000 {
			log.Printf("Question too long (%d chars), truncating context", len(fullQuestion))
			fullQuestion = question
			if len(fullQuestion) > 5000 {
				fullQuestion = fullQuestion[:5000]
			}
		}
		
		log.Printf("Final question length: %d chars", len(fullQuestion))
		
		// Create Dangbei request
		convID := fmt.Sprintf("conv_%d", time.Now().UnixNano())
		dangbeiReq := &dangbei.ChatRequest{
			Stream:         true,
			BotCode:        "AI_SEARCH",
			ConversationID: convID,
			Question:       fullQuestion,
			Model:          dangbeiModel,
			ChatOption: map[string]interface{}{
				"searchKnowledge":       true,  // 开启联网搜索
				"searchAllKnowledge":    false,
				"searchSharedKnowledge": false,
			},
			KnowledgeList: []interface{}{},
			AnonymousKey:  "",
			UUID:          fmt.Sprintf("uuid_%d", time.Now().UnixNano()),
			ChatID:        fmt.Sprintf("chat_%d", time.Now().UnixNano()),
			Files:         []interface{}{},
			Reference:     []interface{}{},
			Role:          "user",
			Status:        "local",
			Content:       fullQuestion,
			UserAction:    "deep,online",  // 添加 online 触发联网搜索
			AgentID:       "",
		}

		ctx := r.Context()
		
		// 添加超时保护
		timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		
		msgChan, errChan := client.Chat(timeoutCtx, dangbeiReq)

		if req.Stream {
			// SSE response
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming not supported", http.StatusInternalServerError)
				return
			}

			created := time.Now().Unix()
			firstChunk := true
			var accumulatedContent strings.Builder

			for {
				select {
				case msg, ok := <-msgChan:
					if !ok {
						// Stream ended - 检查是否有工具调用
						finalContent := accumulatedContent.String()
						log.Printf("Stream ended. Accumulated %d chars. Has tool_call block: %v", len(finalContent), strings.Contains(finalContent, "```tool_call"))
						if len(finalContent) < 500 {
							log.Printf("Full response: %s", finalContent)
						} else {
							log.Printf("Response preview: %s...", finalContent[:500])
						}
						parsedToolCalls := tools.ParseToolCalls(finalContent)
						
						if len(parsedToolCalls) > 0 {
							// 有工具调用 - 发送工具调用 chunk
							for _, tc := range parsedToolCalls {
								convertedTC := ToolCall{
									ID:   tc.ID,
									Type: tc.Type,
									Function: ToolFunction{
										Name:      tc.Function.Name,
										Arguments: tc.Function.Arguments,
									},
								}
								data := OpenAIResponse{
									ID:      convID,
									Object:  "chat.completion.chunk",
									Created: created,
									Model:   req.Model,
									Choices: []Choice{
										{
											Index: 0,
											Delta: &Delta{
												ToolCalls: []ToolCall{convertedTC},
											},
										},
									},
								}
								fmt.Fprintf(w, "data: %s\n\n", mustJSON(data))
								flusher.Flush()
							}
							
							// 发送 finish_reason: tool_calls
							data := OpenAIResponse{
								ID:      convID,
								Object:  "chat.completion.chunk",
								Created: created,
								Model:   req.Model,
								Choices: []Choice{
									{
										Index:        0,
										Delta:        &Delta{},
										FinishReason: stringPtr("tool_calls"),
									},
								},
							}
							fmt.Fprintf(w, "data: %s\n\n", mustJSON(data))
						} else {
							// 没有工具调用 - 正常结束
							data := OpenAIResponse{
								ID:      convID,
								Object:  "chat.completion.chunk",
								Created: created,
								Model:   req.Model,
								Choices: []Choice{
									{
										Index:        0,
										Delta:        &Delta{},
										FinishReason: stringPtr("stop"),
									},
								},
							}
							fmt.Fprintf(w, "data: %s\n\n", mustJSON(data))
						}
						
						fmt.Fprintf(w, "data: [DONE]\n\n")
						flusher.Flush()
						return
					}

					// Only send text content, skip thinking
					if msg.ContentType != "text" {
						continue
					}
					
					// 累积内容用于工具调用检测
					accumulatedContent.WriteString(msg.Content)
					
					// 防泄漏逻辑：如果包含工具调用的开头，立刻停止向前台输出，以防原文字符串泄漏
					if strings.Contains(accumulatedContent.String(), "```tool_call") {
						continue
					}

					delta := &Delta{
						Content: msg.Content,
					}
					if firstChunk {
						delta.Role = "assistant"
						firstChunk = false
					}

					data := OpenAIResponse{
						ID:      convID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   req.Model,
						Choices: []Choice{
							{
								Index: 0,
								Delta: delta,
							},
						},
					}

					fmt.Fprintf(w, "data: %s\n\n", mustJSON(data))
					flusher.Flush()

				case err := <-errChan:
					if err != nil {
						log.Printf("Stream error: %v", err)
						// 标记 token 失败
						if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
							cfg.MarkTokenFailed(token)
						}
						return
					}

				case <-timeoutCtx.Done():
					log.Printf("Request timeout or cancelled")
					return
				}
			}
		} else {
			// Non-streaming response
			var fullContent string
			
			for {
				select {
				case msg, ok := <-msgChan:
					if !ok {
						// 检查是否有工具调用
						parsedToolCalls := tools.ParseToolCalls(fullContent)
						
						var finishReason string
						var toolCallsForResponse []ToolCall
						var contentForResponse interface{}
						
						if len(parsedToolCalls) > 0 {
							// 有工具调用 - 转换类型
							finishReason = "tool_calls"
							for _, tc := range parsedToolCalls {
								toolCallsForResponse = append(toolCallsForResponse, ToolCall{
									ID:   tc.ID,
									Type: tc.Type,
									Function: ToolFunction{
										Name:      tc.Function.Name,
										Arguments: tc.Function.Arguments,
									},
								})
							}
							// 移除工具调用代码块，只保留文本
							contentForResponse = tools.RemoveToolCallBlocks(fullContent)
						} else {
							// 没有工具调用
							finishReason = "stop"
							contentForResponse = fullContent
						}
						
						// Send complete response
						resp := OpenAIResponse{
							ID:      convID,
							Object:  "chat.completion",
							Created: time.Now().Unix(),
							Model:   req.Model,
							Choices: []Choice{
								{
									Index: 0,
									Message: &Message{
										Role:    "assistant",
										Content: contentForResponse,
									},
									FinishReason: stringPtr(finishReason),
									ToolCalls:    toolCallsForResponse,
								},
							},
						}
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(resp)
						return
					}

					// Only accumulate text content
					if msg.ContentType == "text" {
						fullContent += msg.Content
					}

				case err := <-errChan:
					if err != nil {
						log.Printf("Non-stream error: %v", err)
						// 标记 token 失败
						if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
							cfg.MarkTokenFailed(token)
						}
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}

				case <-timeoutCtx.Done():
					log.Printf("Non-stream request timeout or cancelled")
					http.Error(w, "Request timeout", http.StatusGatewayTimeout)
					return
				}
			}
		}
	})
	
	// Web 管理面板
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "/root/clawd/ds2api/web/index.html")
	})
	
	// 健康检查 API
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		statsLock.RLock()
		defer statsLock.RUnlock()
		
		uptime := time.Since(startTime)
		avgResponse := int64(0)
		if len(responseTimes) > 0 {
			sum := int64(0)
			for _, t := range responseTimes {
				sum += t
			}
			avgResponse = sum / int64(len(responseTimes))
		}
		
		successRate := "100%"
		if totalRequests > 0 {
			rate := float64(successCount) / float64(totalRequests) * 100
			successRate = fmt.Sprintf("%.1f%%", rate)
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"uptime": fmt.Sprintf("%dd %dh %dm", 
				int(uptime.Hours())/24, 
				int(uptime.Hours())%24, 
				int(uptime.Minutes())%60),
			"stats": map[string]interface{}{
				"totalRequests": totalRequests,
				"successRate":   successRate,
				"avgResponse":   avgResponse,
			},
			"upstream": map[string]interface{}{
				"status":      upstreamStatus,
				"lastCheck":   upstreamLastCheck.Format("2006-01-02 15:04:05"),
				"lastOK":      upstreamLastOK.Format("2006-01-02 15:04:05"),
				"error":       upstreamError,
				"responseMs":  upstreamResponseMs,
			},
		})
	})
	
	// 日志 API
	http.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		lines := 50
		if l := r.URL.Query().Get("lines"); l != "" {
			fmt.Sscanf(l, "%d", &lines)
		}
		
		file, err := os.Open("/root/clawd/ds2api/dangbei.log")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "无法读取日志文件",
				"logs":  []string{},
			})
			return
		}
		defer file.Close()
		
		var logLines []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			logLines = append(logLines, scanner.Text())
		}
		
		// 只返回最后 N 行
		start := 0
		if len(logLines) > lines {
			start = len(logLines) - lines
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": logLines[start:],
		})
	})
	
	// 账号管理 API
	http.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		switch r.Method {
		case http.MethodGet:
			// 获取账号列表
			accounts := cfg.GetAccounts()
			accountsData := make([]map[string]interface{}, len(accounts))
			for i, acc := range accounts {
				accountsData[i] = map[string]interface{}{
					"name":        acc.Name,
					"token":       acc.Token,
					"status":      acc.Status,
					"lastUsed":    acc.LastUsed,
					"lastChecked": acc.LastChecked,
					"errorCount":  acc.ErrorCount,
				}
			}
			
			stats := cfg.GetAccountStats()
			
			json.NewEncoder(w).Encode(map[string]interface{}{
				"accounts": accountsData,
				"stats":    stats,
			})
			
		case http.MethodPost:
			// 添加账号
			var req struct {
				Name   string `json:"name"`
				Cookie string `json:"cookie"`
			}
			
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "无效的请求格式"})
				return
			}
			
			if err := cfg.AddAccount(req.Name, req.Cookie); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			
			log.Printf("Added new account: %s", req.Name)
			json.NewEncoder(w).Encode(map[string]string{"message": "账号添加成功"})
			
		case http.MethodDelete:
			// 删除账号
			var req struct {
				Token string `json:"token"`
			}
			
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "无效的请求格式"})
				return
			}
			
			if err := cfg.RemoveAccount(req.Token); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			
			log.Printf("Removed account with token: %s", req.Token[:8]+"...")
			json.NewEncoder(w).Encode(map[string]string{"message": "账号删除成功"})
			
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"error": "方法不允许"})
		}
	})
	
	// 测试账号 API
	http.HandleFunc("/api/accounts/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "方法不允许",
			})
			return
		}
		
		var req struct {
			Token string `json:"token"`
		}
		
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "无效的请求格式",
			})
			return
		}
		
		// 查找账号
		var account *config.Account
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Token == req.Token {
				account = &cfg.Accounts[i]
				break
			}
		}
		
		if account == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "账号不存在",
			})
			return
		}
		
		// 执行测试
		start := time.Now()
		client := dangbei.NewClient(account.Token)
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		testReq := &dangbei.ChatRequest{
			Stream:         true,
			BotCode:        "AI_SEARCH",
			ConversationID: fmt.Sprintf("test_%d", time.Now().Unix()),
			Question:       "hi",
			Model:          "deepseek",
			ChatOption: map[string]interface{}{
				"searchKnowledge":       false,
				"searchAllKnowledge":    false,
				"searchSharedKnowledge": false,
			},
			KnowledgeList: []interface{}{},
			UUID:          fmt.Sprintf("test_%d", time.Now().Unix()),
			ChatID:        fmt.Sprintf("test_%d", time.Now().Unix()),
			Files:         []interface{}{},
			Reference:     []interface{}{},
			Role:          "user",
			Status:        "local",
			Content:       "hi",
			UserAction:    "deep",
		}
		
		msgChan, errChan := client.Chat(ctx, testReq)
		
		// 等待第一条消息或错误
		select {
		case msg, ok := <-msgChan:
			elapsed := time.Since(start).Milliseconds()
			
			if ok && msg.Content != "" {
				// 测试成功
				cfg.MarkTokenActive(account.Token)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":      true,
					"responseTime": elapsed,
					"message":      "账号测试成功",
				})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "收到空响应",
				})
			}
			
		case err := <-errChan:
			if err != nil {
				errMsg := err.Error()
				
				if contains(errMsg, "401") || contains(errMsg, "403") || contains(errMsg, "UNAUTHORIZED") {
					cfg.MarkTokenFailed(account.Token)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"success": false,
						"error":   "认证失败，Token 无效或已过期",
					})
				} else {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"success": false,
						"error":   errMsg,
					})
				}
			}
			
		case <-ctx.Done():
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "测试超时",
			})
		}
		
		// 清理剩余消息
		go func() {
			for range msgChan {
			}
		}()
	})

	log.Println("Server starting on 0.0.0.0:8080")
	log.Printf("Dangbei API adapter ready - Models: deepseek-chat, glm-5")
	log.Printf("Web dashboard: http://192.168.1.31:8080/")
	if err := http.ListenAndServe("0.0.0.0:8080", nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func stringPtr(s string) *string {
	return &s
}

// upstreamHealthCheck 定期检查上游 API 健康状态（带智能退避）
// upstreamHealthCheckLoop 定期检查所有账号的健康状态
func upstreamHealthCheckLoop() {
	// 延迟首次检查，让服务器先启动
	time.Sleep(5 * time.Second)
	
	// 首次检查所有账号
	checkAllAccounts()
	
	// 每分钟检查一次
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		checkAllAccounts()
	}
}

func checkAllAccounts() {
	accounts := cfg.GetAccounts()
	if len(accounts) == 0 {
		log.Printf("No accounts to check")
		return
	}
	
	// 并发检查所有账号，避免阻塞
	var wg sync.WaitGroup
	for i := range accounts {
		wg.Add(1)
		go func(acc config.Account) {
			defer wg.Done()
			checkAccountSafe(&acc)
		}(accounts[i])
		time.Sleep(500 * time.Millisecond) // 稍微错开请求
	}
	wg.Wait()
}

func checkAccountSafe(acc *config.Account) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Account [%s] health check PANIC: %v", acc.Name, r)
		}
	}()
	checkAccount(acc)
}

func checkAccount(acc *config.Account) {
	start := time.Now()
	
	client := dangbei.NewClient(acc.Token)
	
	// 创建测试请求
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	testReq := &dangbei.ChatRequest{
		Stream:         true,
		BotCode:        "AI_SEARCH",
		ConversationID: fmt.Sprintf("health_check_%d", time.Now().Unix()),
		Question:       "hi",
		Model:          "deepseek",
		ChatOption: map[string]interface{}{
			"searchKnowledge":       false,
			"searchAllKnowledge":    false,
			"searchSharedKnowledge": false,
		},
		KnowledgeList: []interface{}{},
		UUID:          fmt.Sprintf("health_%d", time.Now().Unix()),
		ChatID:        fmt.Sprintf("health_%d", time.Now().Unix()),
		Files:         []interface{}{},
		Reference:     []interface{}{},
		Role:          "user",
		Status:        "local",
		Content:       "hi",
		UserAction:    "deep",
	}
	
	msgChan, errChan := client.Chat(ctx, testReq)
	
	// 等待第一条消息或错误
	select {
	case msg, ok := <-msgChan:
		elapsed := time.Since(start).Milliseconds()
		
		if ok && msg.Content != "" {
			// 成功收到响应
			cfg.MarkTokenActive(acc.Token)
			log.Printf("Account [%s] health check: OK (response time: %dms)", acc.Name, elapsed)
		} else {
			log.Printf("Account [%s] health check: DEGRADED (empty response)", acc.Name)
		}
		
	case err := <-errChan:
		if err != nil {
			errMsg := err.Error()
			
			if contains(errMsg, "401") || contains(errMsg, "403") || contains(errMsg, "invalid token") {
				cfg.MarkTokenFailed(acc.Token)
				log.Printf("Account [%s] health check: FAILED - Token invalid/expired", acc.Name)
			} else {
				log.Printf("Account [%s] health check: FAILED - %s", acc.Name, errMsg)
			}
		}
		
	case <-ctx.Done():
		log.Printf("Account [%s] health check: TIMEOUT", acc.Name)
	}
	
	// 清理剩余的消息
	go func() {
		for range msgChan {
		}
	}()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// buildContextMessages 智能构建上下文消息
// 策略：保留最近对话历史，严格控制长度以防止当贝 API 报 "\u7528\u6237\u63d0\u95ee\u8d85\u957f"
func buildContextMessages(messages []Message) []Message {
	const (
		maxRounds      = 3     // 最多保留 3 轮对话
		maxChars       = 3000  // 最多 3000 字符
		maxMessages    = 6     // 最多 6 条消息
	)
	
	if len(messages) == 0 {
		return messages
	}
	
	// 从后往前收集消息
	var result []Message
	totalChars := 0
	
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		
		// 计算消息长度
		msgLen := estimateMessageLength(msg)
		
		// 检查是否超过限制
		if len(result) >= maxMessages {
			break
		}
		if totalChars+msgLen > maxChars && len(result) > 0 {
			break
		}
		
		// 添加消息（插入到开头保持顺序）
		result = append([]Message{msg}, result...)
		totalChars += msgLen
	}
	
	// 确保至少有最后一条用户消息
	if len(result) == 0 && len(messages) > 0 {
		result = []Message{messages[len(messages)-1]}
	}
	
	return result
}

// estimateMessageLength 估算消息长度
func estimateMessageLength(msg Message) int {
	switch v := msg.Content.(type) {
	case string:
		return len(v)
	case []interface{}:
		total := 0
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					total += len(text)
				}
			}
		}
		return total
	default:
		return 100 // 默认估算
	}
}

// extractLastUserMessage 提取最后一条用户消息作为问题
// 跳过 OpenClaw 注入的内部元数据消息
func extractLastUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			var text string
			switch v := messages[i].Content.(type) {
			case string:
				text = v
			case []interface{}:
				for _, item := range v {
					if m, ok := item.(map[string]interface{}); ok {
						if m["type"] == "text" {
							if t, ok := m["text"].(string); ok {
								text = t
								break
							}
						}
					}
				}
			}
			// 跳过 OpenClaw 内部注入的元数据消息
			if text != "" &&
				!strings.HasPrefix(text, "Conversation info") &&
				!strings.HasPrefix(text, "Read HEARTBEAT") &&
				!strings.HasPrefix(text, "```json") &&
				!strings.Contains(text, "untrusted metadata") &&
				!strings.Contains(text, "workspace context") {
				return text
			}
		}
	}
	return ""
}

// formatContextForDangbei 将消息历史格式化为当贝 API 可理解的上下文
func formatContextForDangbei(messages []Message) string {
	if len(messages) <= 1 {
		return ""
	}
	
	var parts []string
	for i := 0; i < len(messages)-1; i++ { // 排除最后一条（当前问题）
		msg := messages[i]
		content := ""
		
		switch v := msg.Content.(type) {
		case string:
			content = v
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "text" {
						if text, ok := m["text"].(string); ok {
							content = text
							break
						}
					}
				}
			}
		}
		
		if content != "" {
			role := msg.Role
			if role == "system" {
				parts = append(parts, fmt.Sprintf("系统提示:\n%s\n---", content))
			} else if role == "assistant" {
				parts = append(parts, fmt.Sprintf("AI: %s", content))
			} else if role == "user" {
				parts = append(parts, fmt.Sprintf("用户: %s", content))
			}
		}
	}
	
	if len(parts) == 0 {
		return ""
	}
	
	return "对话历史：\n" + strings.Join(parts, "\n") + "\n\n当前问题："
}

// injectSystemPrompt 在消息列表开头注入系统提示
func injectSystemPrompt(messages []Message, reqTools []RequestTool) []Message {
	// 检查是否已有系统消息
	hasSystem := false
	for _, msg := range messages {
		if msg.Role == "system" {
			hasSystem = true
			break
		}
	}
	
	// 如果没有系统消息，添加工具调用提示
	if !hasSystem {
		var promptContent string
		if len(reqTools) > 0 {
			// 将 RequestTool 转换为 map 以复用 json 序列化逻辑
			var toolsInterface []interface{}
			for _, t := range reqTools {
				toolsInterface = append(toolsInterface, t)
			}
			promptContent = tools.GenerateDynamicSystemPrompt(toolsInterface)
		} else {
			promptContent = tools.SystemPrompt
		}
		
		systemMsg := Message{
			Role:    "system",
			Content: promptContent,
		}
		return append([]Message{systemMsg}, messages...)
	}
	
	return messages
}
