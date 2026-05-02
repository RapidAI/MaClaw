package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type handshakeRequest struct {
	ClientID        string         `json:"clientId"`
	ClientName      string         `json:"clientName"`
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
}

type incomingRequest struct {
	ClientID       string         `json:"clientId"`
	EventID        string         `json:"eventId"`
	MessageID      string         `json:"messageId"`
	ConversationID string         `json:"conversationId"`
	User           userRef        `json:"user"`
	Message        messagePayload `json:"message"`
	CreatedAt      int64          `json:"createdAt"`
}

type userRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type messagePayload struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type incomingResponse struct {
	OK              bool      `json:"ok"`
	Code            string    `json:"code,omitempty"`
	Message         string    `json:"message,omitempty"`
	RequestID       string    `json:"requestId,omitempty"`
	Accepted        bool      `json:"accepted"`
	Duplicate       bool      `json:"duplicate"`
	MaclawMessageID string    `json:"maclawMessageId"`
	Error           *apiError `json:"error,omitempty"`
}

type outgoingResponse struct {
	OK         bool              `json:"ok"`
	Code       string            `json:"code,omitempty"`
	Message    string            `json:"message,omitempty"`
	RequestID  string            `json:"requestId,omitempty"`
	Messages   []outgoingMessage `json:"messages"`
	NextCursor string            `json:"nextCursor"`
	HasMore    bool              `json:"hasMore"`
	Error      *apiError         `json:"error,omitempty"`
}

type outgoingMessage struct {
	ID               string         `json:"id"`
	Seq              int64          `json:"seq"`
	ConversationID   string         `json:"conversationId"`
	ReplyToMessageID string         `json:"replyToMessageId,omitempty"`
	Type             string         `json:"type"`
	Text             string         `json:"text,omitempty"`
	Caption          string         `json:"caption,omitempty"`
	FileName         string         `json:"fileName,omitempty"`
	ContentType      string         `json:"contentType,omitempty"`
	Data             string         `json:"data,omitempty"`
	Progress         bool           `json:"progress,omitempty"`
	Error            string         `json:"error,omitempty"`
	CreatedAt        int64          `json:"createdAt"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type ackRequest struct {
	ClientID   string   `json:"clientId"`
	MessageIDs []string `json:"messageIds"`
	Status     string   `json:"status"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func main() {
	var baseURL string
	var token string
	var clientID string
	var conversationID string
	var userID string
	var userName string
	var text string
	var cursor string
	var pollTimeout int
	var wait bool
	var ack bool
	var skipSend bool

	flag.StringVar(&baseURL, "base", "http://127.0.0.1:18777/api/im-gateway/v1", "MaClaw third-party gateway base URL")
	flag.StringVar(&token, "token", os.Getenv("MACLAW_GATEWAY_TOKEN"), "gateway bearer token, or MACLAW_GATEWAY_TOKEN")
	flag.StringVar(&clientID, "client", "connnectMaClaw", "third-party client id")
	flag.StringVar(&conversationID, "conversation", "demo", "conversation id")
	flag.StringVar(&userID, "user", "tester", "external user id")
	flag.StringVar(&userName, "name", "Test User", "external user display name")
	flag.StringVar(&text, "text", "你好，MaClaw", "text message to send")
	flag.StringVar(&cursor, "cursor", "0", "outgoing cursor")
	flag.IntVar(&pollTimeout, "timeout", 30, "outgoing long-poll timeout seconds")
	flag.BoolVar(&wait, "wait", true, "poll outgoing messages after sending")
	flag.BoolVar(&ack, "ack", true, "ack returned outgoing messages")
	flag.BoolVar(&skipSend, "poll-only", false, "only poll outgoing messages; do not send incoming text")
	flag.Parse()

	if strings.TrimSpace(token) == "" {
		fatal(errors.New("missing -token or MACLAW_GATEWAY_TOKEN"))
	}
	baseURL = strings.TrimRight(baseURL, "/")
	client := &http.Client{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(pollTimeout+20)*time.Second)
	defer cancel()

	fmt.Println("== handshake ==")
	var hs map[string]any
	mustDoJSON(ctx, client, http.MethodPost, baseURL+"/handshake", token, handshakeRequest{
		ClientID:        clientID,
		ClientName:      "connnectMaClaw test client",
		ProtocolVersion: "1.0",
		Capabilities: map[string]any{
			"text":        true,
			"longPolling": true,
		},
	}, &hs)
	printJSON(hs)

	messageID := fmt.Sprintf("connnect_msg_%d", time.Now().UnixMilli())
	if !skipSend {
		fmt.Println("== incoming ==")
		var in incomingResponse
		mustDoJSON(ctx, client, http.MethodPost, baseURL+"/incoming", token, incomingRequest{
			ClientID:       clientID,
			EventID:        "evt_" + messageID,
			MessageID:      messageID,
			ConversationID: conversationID,
			User:           userRef{ID: userID, Name: userName},
			Message:        messagePayload{Type: "text", Text: text},
			CreatedAt:      time.Now().UnixMilli(),
		}, &in)
		printJSON(in)
		if !in.OK {
			os.Exit(1)
		}
	}

	if wait {
		fmt.Println("== outgoing ==")
		var out outgoingResponse
		url := fmt.Sprintf("%s/outgoing?clientId=%s&cursor=%s&timeout=%d&limit=20", baseURL, queryEscape(clientID), queryEscape(cursor), pollTimeout)
		mustDoJSON(ctx, client, http.MethodGet, url, token, nil, &out)
		printJSON(out)
		if !out.OK {
			os.Exit(1)
		}
		if ack && len(out.Messages) > 0 {
			ids := make([]string, 0, len(out.Messages))
			for _, msg := range out.Messages {
				ids = append(ids, msg.ID)
			}
			fmt.Println("== ack ==")
			var ackResp map[string]any
			mustDoJSON(ctx, client, http.MethodPost, baseURL+"/ack", token, ackRequest{ClientID: clientID, MessageIDs: ids, Status: "delivered"}, &ackResp)
			printJSON(ackResp)
		}
		fmt.Printf("next cursor: %s\n", out.NextCursor)
	}
}

func mustDoJSON(ctx context.Context, client *http.Client, method, url, token string, body any, out any) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			fatal(err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		fmt.Printf("raw response (%d): %s\n", resp.StatusCode, string(data))
		fatal(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		printJSON(out)
		fatal(fmt.Errorf("HTTP %d", resp.StatusCode))
	}
}

func printJSON(v any) {
	buf, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(buf))
}

func queryEscape(s string) string {
	replacer := strings.NewReplacer("%", "%25", " ", "%20", "&", "%26", "=", "%3D", "?", "%3F", "#", "%23", "+", "%2B")
	return replacer.Replace(s)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
