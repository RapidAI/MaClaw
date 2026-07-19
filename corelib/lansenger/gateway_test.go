package lansenger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestReconnectBackoffDelayCapsAtMax(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "zero", attempt: 0, want: 0},
		{name: "first attempt", attempt: 1, want: 3 * time.Second},
		{name: "linear before cap", attempt: 3, want: 9 * time.Second},
		{name: "cap", attempt: 100, want: lansengerMaxBackoff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reconnectBackoffDelay(tt.attempt); got != tt.want {
				t.Fatalf("reconnectBackoffDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestLansengerConnectionAgeExceeded(t *testing.T) {
	now := time.Now()
	if lansengerConnectionAgeExceeded(time.Time{}, now) {
		t.Fatal("zero connectedAt should not exceed max age")
	}
	if lansengerConnectionAgeExceeded(now.Add(time.Minute), now) {
		t.Fatal("future connectedAt should not exceed max age")
	}
	if lansengerConnectionAgeExceeded(now.Add(-lansengerMaxConnAge+time.Second), now) {
		t.Fatal("connection younger than max age should not exceed")
	}
	if !lansengerConnectionAgeExceeded(now.Add(-lansengerMaxConnAge), now) {
		t.Fatal("connection at max age should exceed")
	}
	if !lansengerConnectionAgeExceeded(now.Add(-lansengerMaxConnAge-time.Second), now) {
		t.Fatal("connection older than max age should exceed")
	}
}

func TestProcessEventUsesConversationIDForGroupMessages(t *testing.T) {
	var got IncomingMessage
	gw := NewGateway(Config{}, func(msg IncomingMessage) { got = msg })
	gw.handleWSMessage([]byte(`{
		"events":[{
			"eventType":"bot_group_message",
			"id":"message-1",
			"data":{
				"from":"user-1",
				"conversationId":"group-1",
				"msgType":"text",
				"msgData":{"text":{"content":"hello group"}}
			}
		}]
	}`))
	if got.ChatType != "group" || got.GroupID != "group-1" {
		t.Fatalf("group routing = chatType %q groupID %q, want group/group-1", got.ChatType, got.GroupID)
	}
	if got.FromUserID != "user-1" || got.Text != "hello group" {
		t.Fatalf("message = %#v", got)
	}
}

func TestHandleWSMessageSupportsTopLevelAndDeeplyNestedEvents(t *testing.T) {
	var got []IncomingMessage
	gw := NewGateway(Config{}, func(msg IncomingMessage) { got = append(got, msg) })
	gw.handleWSMessage([]byte(`{"eventType":"bot_private_message","data":{"from":"user-1","msgType":"text","msgData":{"text":{"content":"top-level"}}}}`))
	gw.handleWSMessage([]byte(`{"events":[{"type":"bot_group_message","data":{"data":{"data":{"from":"user-2","conversationId":"group-1","msgType":"text","msgData":{"text":{"content":"nested"}}}}}}]}`))
	if len(got) != 2 {
		t.Fatalf("message count = %d, want 2", len(got))
	}
	if got[0].Text != "top-level" || got[0].ChatType != "p2p" {
		t.Fatalf("top-level message = %#v", got[0])
	}
	if got[1].Text != "nested" || got[1].ChatType != "group" || got[1].GroupID != "group-1" {
		t.Fatalf("nested message = %#v", got[1])
	}
}

func TestNestedEventInheritsOuterGroupMetadata(t *testing.T) {
	var got IncomingMessage
	gw := NewGateway(Config{}, func(msg IncomingMessage) { got = msg })
	gw.handleWSMessage([]byte(`{"events":[{"type":"bot_group_message","data":{"conversationId":"group-outer","groupName":"Team","reminder":{"staffs":[{"staffId":"u2","staffName":"Bob"}]},"data":{"from":"user-1","msgType":"text","msgData":{"text":{"content":"hello"}}}}}]}`))
	if got.GroupID != "group-outer" || got.GroupName != "Team" || len(got.MentionedStaffs) != 1 || got.Text != "hello" {
		t.Fatalf("nested metadata = %#v", got)
	}
}

func TestGroupEventPreservesExplicitBotMentionFlag(t *testing.T) {
	var got IncomingMessage
	gw := NewGateway(Config{}, func(msg IncomingMessage) { got = msg })
	gw.handleWSMessage([]byte(`{"events":[{"type":"bot_group_message","data":{"conversationId":"group-1","from":"user-1","reminder":{"isAtMe":true,"isAtAll":false,"bots":[{"botId":"runtime-bot","botName":"Bot"}]},"msgType":"text","msgData":{"text":{"content":"@Bot hello"}}}}]}`))
	if !got.IsAtMe || got.IsAtAll || len(got.MentionedBots) != 1 {
		t.Fatalf("mention metadata = %#v", got)
	}
}

func TestNestedGroupEventInheritsExplicitBotMentionFlag(t *testing.T) {
	var got IncomingMessage
	gw := NewGateway(Config{}, func(msg IncomingMessage) { got = msg })
	gw.handleWSMessage([]byte(`{"events":[{"type":"bot_group_message","data":{"conversationId":"group-1","reminder":{"isAtMe":true},"data":{"from":"user-1","msgType":"text","msgData":{"text":{"content":"@Bot hello"}}}}}]}`))
	if !got.IsAtMe {
		t.Fatalf("nested mention metadata = %#v", got)
	}
}

func TestHandlerPanicDoesNotStopSubsequentMessages(t *testing.T) {
	var calls atomic.Int32
	gw := NewGateway(Config{}, func(IncomingMessage) {
		if calls.Add(1) == 1 {
			panic("test handler panic")
		}
	})
	message := []byte(`{"events":[{"eventType":"bot_private_message","data":{"from":"user-1","msgType":"text","msgData":{"text":{"content":"hello"}}}}]}`)
	gw.handleWSMessage(message)
	gw.handleWSMessage(message)
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
}

func TestProcessEventDownloadsTextAttachmentAndPreservesContext(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"appToken": "token", "expiresIn": 3600}})
		case "/v1/medias/media-1/fetch":
			w.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
			_, _ = w.Write([]byte("report body"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()
	var got IncomingMessage
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, func(msg IncomingMessage) { got = msg })
	gw.handleWSMessage([]byte(`{"events":[{"eventType":"bot_group_message","data":{"from":"user-1","conversationId":"group-1","senderName":"Alice","groupName":"Team","msgType":"text","msgData":{"text":{"content":"see attachment","mediaType":3,"mediaIds":["media-1"]}},"reminder":{"staffs":[{"staffId":"u2","staffName":"Bob"}]},"referenceMsg":{"from":"u3","senderName":"Carol","msgType":"text","msgData":{"text":{"content":"quoted"}}}}}]}`))
	if got.MediaType != "file" || got.MediaName != "report.txt" || string(got.MediaData) != "report body" {
		t.Fatalf("media = type=%q name=%q data=%q", got.MediaType, got.MediaName, got.MediaData)
	}
	if got.SenderName != "Alice" || got.GroupName != "Team" || got.ReferenceText != "[引用 Carol] quoted" || got.Text != "see attachment\n\n[引用 Carol] quoted" || len(got.MentionedStaffs) != 1 {
		t.Fatalf("context = %#v", got)
	}
}

func TestExtractTextReturnsAttachmentLabelWithoutCaption(t *testing.T) {
	got := extractText(json.RawMessage(`{"text":{"mediaType":2,"mediaIds":["image-1"]}}`), "text")
	if got != "[image: image-1]" {
		t.Fatalf("extractText() = %q, want [image: image-1]", got)
	}
}

func TestExtractTextPreservesMediaIDAndCaption(t *testing.T) {
	got := extractText(json.RawMessage(`{"file":{"content":"Quarterly report","mediaIds":["file-1"]}}`), "file")
	if got != "[file: file-1] Quarterly report" {
		t.Fatalf("extractText() = %q", got)
	}
	got = extractText(json.RawMessage(`{"format":{"content":"rich content"}}`), "format")
	if got != "rich content" {
		t.Fatalf("format text = %q", got)
	}
}

func TestMediaFilenameSupportsRFC5987(t *testing.T) {
	got := mediaFilename("attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.txt")
	if got != "报告.txt" {
		t.Fatalf("mediaFilename() = %q, want 报告.txt", got)
	}
}

func TestMediaFilenamePrefersRFC5987AndSanitizesPath(t *testing.T) {
	got := mediaFilename("attachment; filename=old.txt; filename*=UTF-8''nested%2F%E6%8A%A5%E5%91%8A.txt")
	if got != "报告.txt" {
		t.Fatalf("mediaFilename() = %q, want 报告.txt", got)
	}
	got = mediaFilename(`attachment; filename="..\\unsafe.txt"`)
	if got != "unsafe.txt" {
		t.Fatalf("mediaFilename() = %q, want unsafe.txt", got)
	}
}

func TestDownloadMediaEscapesMediaIDAndToken(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"appToken": "token+/=", "expiresIn": 3600}})
		case "/v1/medias/media/id/fetch":
			if !strings.Contains(r.RequestURI, "/v1/medias/media%2Fid/fetch") {
				t.Fatalf("media ID was not escaped: %s", r.RequestURI)
			}
			if got := r.URL.Query().Get("app_token"); got != "token+/=" {
				t.Fatalf("app_token = %q", got)
			}
			_, _ = w.Write([]byte("data"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, nil)
	data, _, err := gw.downloadMedia(context.Background(), "media/id")
	if err != nil || string(data) != "data" {
		t.Fatalf("downloadMedia() = %q, %v", data, err)
	}
}

func TestTokenCacheKeepsNonPositiveExpiryUsable(t *testing.T) {
	var cache tokenCache
	cache.set("token", 0)
	if got, ok := cache.get(); !ok || got != "token" {
		t.Fatalf("cache.get() = %q, %v; want token, true", got, ok)
	}
}

func TestGetAppTokenEscapesCredentials(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("appid"); got != "app id+/" {
			t.Fatalf("appid = %q", got)
		}
		if got := r.URL.Query().Get("secret"); got != "secret+/=" {
			t.Fatalf("secret = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"appToken": "token", "expiresIn": 3600}})
	}))
	defer api.Close()
	gw := NewGateway(Config{AppID: "app id+/", AppSecret: "secret+/=", ApiGatewayURL: api.URL}, nil)
	if _, err := gw.getAppToken(context.Background()); err != nil {
		t.Fatalf("getAppToken: %v", err)
	}
}

func TestGetAppTokenCoalescesConcurrentRefreshes(t *testing.T) {
	var requests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(20 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"appToken": "token", "expiresIn": 3600}})
	}))
	defer api.Close()
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, nil)
	const callers = 12
	start := make(chan struct{})
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			_, err := gw.getAppToken(context.Background())
			errs <- err
		}()
	}
	close(start)
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("getAppToken: %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestGetAppTokenWaitRespectsCallerCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"appToken": "token", "expiresIn": 3600}})
	}))
	defer api.Close()
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, nil)
	firstDone := make(chan error, 1)
	go func() { _, err := gw.getAppToken(context.Background()); firstDone <- err }()
	<-requestStarted
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gw.getAppToken(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting getAppToken error = %v, want context canceled", err)
	}
	close(releaseRequest)
	if err := <-firstDone; err != nil {
		t.Fatalf("initial getAppToken: %v", err)
	}
}

func TestAPIClientDoesNotFollowRedirectsWithToken(t *testing.T) {
	redirectTargetHit := atomic.Bool{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetHit.Store(true)
	}))
	defer target.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/captured", http.StatusFound)
	}))
	defer api.Close()
	gw := NewGateway(Config{ApiGatewayURL: api.URL}, nil)
	if err := gw.sendPrivateMessage(context.Background(), "sensitive-token", "user", "text", map[string]any{}, nil); err == nil {
		t.Fatal("expected redirect to be returned as an API error")
	}
	if redirectTargetHit.Load() {
		t.Fatal("client followed a redirect carrying an app token")
	}
}

func TestMediaClientUsesLongerBoundedTimeoutAndDoesNotFollowRedirects(t *testing.T) {
	gw := NewGateway(Config{}, nil)
	if gw.mediaClient.Timeout != 5*time.Minute {
		t.Fatalf("media client timeout = %v, want 5m", gw.mediaClient.Timeout)
	}
	apiTransport, ok := gw.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("API transport type = %T, want *http.Transport", gw.client.Transport)
	}
	mediaTransport, ok := gw.mediaClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("media transport type = %T, want *http.Transport", gw.mediaClient.Transport)
	}
	if mediaTransport == apiTransport {
		t.Fatal("media client must not share the API transport's short header timeout")
	}
	if apiTransport.ResponseHeaderTimeout != 30*time.Second || mediaTransport.ResponseHeaderTimeout != 5*time.Minute {
		t.Fatalf("response header timeouts = api:%v media:%v, want 30s/5m", apiTransport.ResponseHeaderTimeout, mediaTransport.ResponseHeaderTimeout)
	}
	if mediaTransport.TLSHandshakeTimeout != apiTransport.TLSHandshakeTimeout || mediaTransport.Proxy == nil {
		t.Fatal("media transport did not preserve hardened TLS/proxy settings")
	}
	if err := gw.mediaClient.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("media redirect policy = %v, want ErrUseLastResponse", err)
	}
}

func TestStatusCallbackCanBeUpdatedWhileStatusesAreEmitted(t *testing.T) {
	gw := NewGateway(Config{}, nil)
	var callbacks atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			gw.emitStatus("connected")
		}
	}()
	for i := 0; i < 1000; i++ {
		gw.SetStatusCallback(func(string) { callbacks.Add(1) })
	}
	<-done
	if callbacks.Load() == 0 {
		t.Fatal("status callback was never invoked")
	}
}

func TestStopSerializesConcurrentStart(t *testing.T) {
	gw := NewGateway(Config{}, nil)
	gw.running = true
	gw.cancel = func() {}
	gw.wg.Add(1)
	loopDone := make(chan struct{})
	go func() {
		defer gw.wg.Done()
		<-loopDone
	}()

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		_ = gw.Stop()
	}()

	deadline := time.After(time.Second)
	for gw.IsRunning() {
		select {
		case <-deadline:
			t.Fatal("Stop did not mark the gateway stopped")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	started := make(chan error, 1)
	go func() { started <- gw.Start(context.Background()) }()
	select {
	case err := <-started:
		t.Fatalf("Start returned before Stop completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(loopDone)
	<-stopDone
	// Start reaches authentication after Stop releases lifecycleMu. It will fail
	// with the empty test config, which is sufficient to prove the serialization.
	if err := <-started; err == nil {
		t.Fatal("Start unexpectedly succeeded with empty credentials")
	}
}

func TestStopCancelsStartDuringAuthentication(t *testing.T) {
	requestStarted := make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, nil)
	startResult := make(chan error, 1)
	go func() { startResult <- gw.Start(context.Background()) }()
	<-requestStarted

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		_ = gw.Stop()
	}()
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel in-flight authentication")
	}
	if err := <-startResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context canceled", err)
	}
}

func TestStartWithCanceledContextDoesNotReportAuthFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: "http://127.0.0.1:1"}, nil)
	var statuses []string
	gw.SetStatusCallback(func(status string) { statuses = append(statuses, status) })
	err := gw.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context canceled", err)
	}
	for _, status := range statuses {
		if status == "error" {
			t.Fatalf("canceled Start emitted error status: %#v", statuses)
		}
	}
}

func TestConnectLoopClearsCancelWhenItExits(t *testing.T) {
	gw := NewGateway(Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	gw.cancel = cancel
	gw.running = true
	gw.wg.Add(1)
	cancel()
	gw.connectLoop(ctx, 0)
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.running || gw.cancel != nil {
		t.Fatalf("gateway state after loop exit: running=%v cancel_set=%v", gw.running, gw.cancel != nil)
	}
}

func TestConnectLoopRejectsConnectionAfterCancellation(t *testing.T) {
	gw := NewGateway(Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gw.running = true
	gw.wg.Add(1)
	gw.connectLoop(ctx, 0)
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.ws != nil {
		t.Fatal("canceled connection loop retained a WebSocket")
	}
}

func TestSendMediaUsesCurrentUploadEndpoint(t *testing.T) {
	var uploaded bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"appToken": "token", "expiresIn": 3600}})
		case "/v1/app/medias/create":
			uploaded = true
			if got := r.URL.Query().Get("type"); got != "file" {
				t.Fatalf("upload type = %q, want file", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"mediaId": "media-1"}})
		case "/v1/bot/messages/create":
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, nil)
	if err := gw.SendMedia(context.Background(), OutgoingMedia{ToUserID: "user", FileData: []byte("file"), FileName: "a.txt", MediaType: "file"}); err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if !uploaded {
		t.Fatal("current upload endpoint was not called")
	}
}

func TestSendRejectsMissingRecipientWithoutCallingAPI(t *testing.T) {
	requests := atomic.Int32{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		t.Fatal("API must not be called for an empty recipient")
	}))
	defer api.Close()

	gw := NewGateway(Config{ApiGatewayURL: api.URL}, nil)
	if err := gw.SendText(context.Background(), OutgoingText{Text: "hello"}); err == nil {
		t.Fatal("SendText accepted an empty recipient")
	}
	if err := gw.SendMedia(context.Background(), OutgoingMedia{FileData: []byte("data")}); err == nil {
		t.Fatal("SendMedia accepted an empty recipient")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("API requests = %d, want 0", got)
	}
}

func TestSendTextSkipsBlankContent(t *testing.T) {
	requests := atomic.Int32{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		t.Fatal("API must not be called for blank text")
	}))
	defer api.Close()

	gw := NewGateway(Config{ApiGatewayURL: api.URL}, nil)
	if err := gw.SendText(context.Background(), OutgoingText{ToUserID: "user", Text: " \t\n "}); err != nil {
		t.Fatalf("SendText blank content: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("API requests = %d, want 0", got)
	}
}


func TestSendTextDoesNotStripReminderOnNetworkError(t *testing.T) {
	// When the first attempt fails with a non-API error, we must not silently
	// retry without reminder (that would mask transport failures).
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/apptoken/create") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data":    map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
			return
		}
		// Close without response → client transport/read error, not APIError.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijack unsupported")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}))
	defer api.Close()

	gw := NewGateway(Config{ApiGatewayURL: api.URL, AppID: "a", AppSecret: "s"}, nil)
	err := gw.SendText(context.Background(), OutgoingText{
		ToUserID: "group-1",
		Text:     "answer",
		IsGroup:  true,
		Reminder: &OutgoingReminder{UserIDs: []string{"staff-1"}},
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("expected non-API error, got %v", err)
	}
}

func TestSendTextIncludesReminderAndRefMsgID(t *testing.T) {
	var body []byte
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v1/apptoken/create"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data":    map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
		case strings.Contains(r.URL.Path, "/v1/messages/group/create"):
			body, _ = io.ReadAll(r.Body)
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer api.Close()

	gw := NewGateway(Config{ApiGatewayURL: api.URL, AppID: "a", AppSecret: "s"}, nil)
	err := gw.SendText(context.Background(), OutgoingText{
		ToUserID: "group-1",
		Text:     "answer",
		IsGroup:  true,
		Reminder: &OutgoingReminder{UserIDs: []string{"staff-1"}},
		RefMsgID: "msg-99",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body: %v body=%s", err, body)
	}
	if payload["refMsgId"] != "msg-99" {
		t.Fatalf("refMsgId = %#v", payload["refMsgId"])
	}
	msgData, _ := payload["msgData"].(map[string]any)
	ft, _ := msgData["formatText"].(map[string]any)
	rem, _ := ft["reminder"].(map[string]any)
	ids, _ := rem["userIds"].([]any)
	if len(ids) != 1 || ids[0] != "staff-1" {
		t.Fatalf("reminder = %#v", rem)
	}
}

func TestSendTextRetriesWithoutRefMsgIDOnAPIError(t *testing.T) {
	// Invalid refMsgId is a common platform reject; keep @mention when possible.
	var attempt int
	var lastBody []byte
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v1/apptoken/create"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data":    map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
		case strings.Contains(r.URL.Path, "/v1/messages/group/create"):
			body, _ := io.ReadAll(r.Body)
			attempt++
			if attempt == 1 {
				// Reject full payload (with refMsgId).
				_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 40004, "errMsg": "invalid refMsgId"})
				return
			}
			lastBody = body
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer api.Close()

	gw := NewGateway(Config{ApiGatewayURL: api.URL, AppID: "a", AppSecret: "s"}, nil)
	err := gw.SendText(context.Background(), OutgoingText{
		ToUserID: "group-1",
		Text:     "answer",
		IsGroup:  true,
		Reminder: &OutgoingReminder{UserIDs: []string{"staff-1"}},
		RefMsgID: "bad-ref",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if attempt != 2 {
		t.Fatalf("attempts = %d, want 2", attempt)
	}
	var payload map[string]any
	if err := json.Unmarshal(lastBody, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, hasRef := payload["refMsgId"]; hasRef {
		t.Fatalf("retry must drop refMsgId, payload=%#v", payload)
	}
	msgData, _ := payload["msgData"].(map[string]any)
	ft, _ := msgData["formatText"].(map[string]any)
	rem, _ := ft["reminder"].(map[string]any)
	ids, _ := rem["userIds"].([]any)
	if len(ids) != 1 || ids[0] != "staff-1" {
		t.Fatalf("retry should keep reminder, rem=%#v", rem)
	}
}

func TestUploadMediaUsesCurrentDescriptiveTypes(t *testing.T) {
	for _, tt := range []struct {
		mediaType string
		wantType  string
	}{
		{mediaType: "image", wantType: "image"},
		{mediaType: "video", wantType: "video"},
		{mediaType: "audio", wantType: "audio"},
		{mediaType: "voice", wantType: "audio"},
		{mediaType: "file", wantType: "file"},
		{mediaType: "unknown", wantType: "file"},
	} {
		t.Run(tt.mediaType, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("type"); got != tt.wantType {
					t.Fatalf("upload type = %q, want %q", got, tt.wantType)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]any{"mediaId": "media-1"}})
			}))
			defer api.Close()

			gw := NewGateway(Config{ApiGatewayURL: api.URL}, nil)
			if _, err := gw.uploadMedia(context.Background(), "token", []byte("file"), "sample.bin", tt.mediaType); err != nil {
				t.Fatalf("uploadMedia: %v", err)
			}
		})
	}
}

func TestGetWebSocketURLUsesConfiguredWebSocketFallback(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ws/endpoint/create" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]string{"wsEndpoint": ""},
		})
	}))
	defer api.Close()

	gw := NewGateway(Config{
		AppID:            "app-1",
		AppSecret:        "secret-1",
		ApiGatewayURL:    api.URL,
		WebSocketBaseURL: "https://ws.example.test/base/",
	}, nil)

	got, err := gw.getWebSocketURL(context.Background())
	if err != nil {
		t.Fatalf("getWebSocketURL: %v", err)
	}
	want := "wss://ws.example.test/base/open-apis/im/v1/ws/app-1"
	if got != want {
		t.Fatalf("getWebSocketURL() = %q, want %q", got, want)
	}
}

func TestValidateWebSocketURLRejectsUnsafeOrMalformedEndpoints(t *testing.T) {
	for _, raw := range []string{
		"https://ws.example.test/live",
		"wss:///live",
		"wss://user:secret@ws.example.test/live",
	} {
		if _, err := validateWebSocketURL(raw); err == nil {
			t.Fatalf("validateWebSocketURL(%q) accepted an invalid endpoint", raw)
		}
	}
	if got, err := validateWebSocketURL("wss://ws.example.test/live?token=x"); err != nil || got != "wss://ws.example.test/live?token=x" {
		t.Fatalf("valid endpoint = %q, %v", got, err)
	}
}

func TestRedactWebSocketURLRemovesConnectionCredentials(t *testing.T) {
	got := redactWebSocketURL("wss://ws.example.test/live?token=secret&nonce=123#fragment")
	if got != "wss://ws.example.test/live" {
		t.Fatalf("redacted URL = %q", got)
	}
	if got := redactWebSocketURL("%not-a-url"); got != "[invalid websocket URL]" {
		t.Fatalf("invalid URL redaction = %q", got)
	}
}

func TestRedactHTTPErrorStripsSecretQuery(t *testing.T) {
	raw := &url.Error{
		Op:  "Get",
		URL: "https://apigw.example.test/v1/apptoken/create?grant_type=client_credential&appid=APP&secret=SUPERSECRET",
		Err: io.EOF,
	}
	got := redactHTTPError(raw)
	if got == nil {
		t.Fatal("expected redacted error")
	}
	msg := got.Error()
	if strings.Contains(msg, "SUPERSECRET") {
		t.Fatalf("secret leaked in error: %q", msg)
	}
	if !strings.Contains(msg, "secret=%2A%2A%2A") && !strings.Contains(msg, "secret=***") {
		t.Fatalf("expected redacted secret placeholder, got %q", msg)
	}

	// String-only errors (no *url.Error) must also be scrubbed.
	plain := errors.New(`Get "https://x/y?secret=ABC123": EOF`)
	got2 := redactHTTPError(plain)
	if strings.Contains(got2.Error(), "ABC123") {
		t.Fatalf("secret leaked in plain error: %q", got2.Error())
	}

	// Message-send URLs embed app_token in the query string.
	appTok := &url.Error{
		Op:  "Post",
		URL: "https://apigw.example.test/v1/messages/create?app_token=LIVE_APP_TOKEN_XYZ",
		Err: io.EOF,
	}
	got3 := redactHTTPError(appTok)
	if strings.Contains(got3.Error(), "LIVE_APP_TOKEN_XYZ") {
		t.Fatalf("app_token leaked in error: %q", got3.Error())
	}
}

func TestGetWebSocketURLRejectsUnsafeGatewayEndpoint(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0, "data": map[string]string{"wsEndpoint": "https://ws.example.test/live"}})
	}))
	defer api.Close()
	gw := NewGateway(Config{AppID: "app", AppSecret: "secret", ApiGatewayURL: api.URL}, nil)
	if _, err := gw.getWebSocketURL(context.Background()); err == nil {
		t.Fatal("unsafe endpoint was accepted")
	}
}

func TestDoPostRetriesTransientHTTPError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	}))
	defer api.Close()

	gw := NewGateway(Config{}, nil)
	if err := gw.doPost(context.Background(), api.URL, []byte(`{"hello":"world"}`)); err != nil {
		t.Fatalf("doPost: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestGetAppTokenRetriesTransientHTTPError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"appToken":  "tok-1",
				"expiresIn": 3600,
			},
		})
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	tok, err := gw.getAppToken(context.Background())
	if err != nil {
		t.Fatalf("getAppToken: %v", err)
	}
	if tok != "tok-1" {
		t.Fatalf("token = %q, want tok-1", tok)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestGetWebSocketURLRetriesTransientHTTPError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]string{"wsEndpoint": "wss://ws.example.test/live"},
		})
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	got, err := gw.getWebSocketURL(context.Background())
	if err != nil {
		t.Fatalf("getWebSocketURL: %v", err)
	}
	if got != "wss://ws.example.test/live" {
		t.Fatalf("getWebSocketURL() = %q", got)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDoPostDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer api.Close()

	gw := NewGateway(Config{}, nil)
	if err := gw.doPost(context.Background(), api.URL, []byte(`{"hello":"world"}`)); err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestSendTextRefreshesExpiredTokenAndRetriesOnce(t *testing.T) {
	tokenAttempts := 0
	sendAttempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			tokenAttempts++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data": map[string]any{
					"appToken":  "tok-1",
					"expiresIn": 3600,
				},
			})
		case "/v1/bot/messages/create":
			sendAttempts++
			if sendAttempts == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 42001, "errMsg": "token expired"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	if err := gw.SendText(context.Background(), OutgoingText{ToUserID: "user-1", Text: "hello"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if tokenAttempts != 2 {
		t.Fatalf("tokenAttempts = %d, want 2", tokenAttempts)
	}
	if sendAttempts != 2 {
		t.Fatalf("sendAttempts = %d, want 2", sendAttempts)
	}
}

func TestSendMediaRefreshesExpiredTokenDuringUploadAndRetriesOnce(t *testing.T) {
	tokenAttempts := 0
	uploadAttempts := 0
	sendAttempts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apptoken/create":
			tokenAttempts++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data": map[string]any{
					"appToken":  "tok-1",
					"expiresIn": 3600,
				},
			})
		case "/v1/app/medias/create":
			uploadAttempts++
			if uploadAttempts == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 42001, "errMsg": "token expired"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0,
				"data":    map[string]string{"mediaId": "media-1"},
			})
		case "/v1/bot/messages/create":
			sendAttempts++
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer api.Close()

	gw := NewGateway(Config{AppID: "app-1", AppSecret: "secret-1", ApiGatewayURL: api.URL}, nil)
	if err := gw.SendMedia(context.Background(), OutgoingMedia{ToUserID: "user-1", FileData: []byte("png"), FileName: "a.png", MediaType: "image"}); err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if tokenAttempts != 2 {
		t.Fatalf("tokenAttempts = %d, want 2", tokenAttempts)
	}
	if uploadAttempts != 2 {
		t.Fatalf("uploadAttempts = %d, want 2", uploadAttempts)
	}
	if sendAttempts != 1 {
		t.Fatalf("sendAttempts = %d, want 1", sendAttempts)
	}
}

func TestIsLansengerTokenExpiredError(t *testing.T) {
	if !isLansengerTokenExpiredError(&lansengerAPIError{Code: 42001, Msg: "expired"}) {
		t.Fatal("expected 42001 to be treated as token expired")
	}
	if !isLansengerTokenExpiredError(&lansengerAPIError{Code: 123, Msg: "app token invalid"}) {
		t.Fatal("expected token invalid message to be treated as token expired")
	}
	if isLansengerTokenExpiredError(&lansengerAPIError{Code: 123, Msg: "permission denied"}) {
		t.Fatal("permission denied should not be treated as token expired")
	}
}

func TestGetWebSocketURLHonorsCanceledContext(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not reach server")
	}))
	defer api.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	gw := NewGateway(Config{
		AppID:         "app-1",
		AppSecret:     "secret-1",
		ApiGatewayURL: api.URL,
	}, nil)

	if _, err := gw.getWebSocketURL(ctx); err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestReadLoopExitsPromptlyWhenContextCanceled(t *testing.T) {
	upgrader := websocket.Upgrader{}
	accepted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		close(accepted)
		<-r.Context().Done()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("server did not accept websocket")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	gw := NewGateway(Config{}, nil)
	go func() {
		gw.readLoop(ctx, conn)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit promptly after context cancellation")
	}
}

func TestConnectLoopClearsRunningOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	gw := NewGateway(Config{}, nil)
	gw.running = true
	gw.runID = 1
	gw.wg.Add(1)
	go gw.connectLoop(ctx, 1)
	gw.wg.Wait()

	if gw.IsRunning() {
		t.Fatal("gateway still reports running after connectLoop context cancellation")
	}
}
