package messenger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

func testAccount() Account {
	return Account{
		Enabled:         true,
		PageID:          "page-1",
		PageAccessToken: "page-token",
		VerifyToken:     "verify-token",
		AppSecret:       "app-secret",
		AllowFrom:       []string{"psid:1001"},
		DMPolicy:        DMPolicyAllowlist,
	}.Normalized()
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	body := []byte(`{"object":"page"}`)
	if VerifySignature(body, "app-secret", "sha256=abcd") {
		t.Fatal("short or non-hex signature must fail")
	}
	if VerifySignature(body, "app-secret", sign("other", body)) {
		t.Fatal("signature for another secret must fail")
	}
	if !VerifySignature(body, "app-secret", sign("app-secret", body)) {
		t.Fatal("valid signature must pass")
	}
	if VerifySignature(body, "", sign("app-secret", body)) {
		t.Fatal("empty secret must fail")
	}
}

func TestWebhookVerifyChallenge(t *testing.T) {
	gateway := NewGateway(testAccount(), nil)
	req := httptest.NewRequest(http.MethodGet, "/facebook-messenger/webhook?hub.mode=subscribe&hub.verify_token=verify-token&hub.challenge=challenge-42", nil)
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "challenge-42" {
		t.Fatalf("challenge = %q", rec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodGet, "/facebook-messenger/webhook?hub.mode=subscribe&hub.verify_token=nope&hub.challenge=challenge-42", nil)
	badRec := httptest.NewRecorder()
	gateway.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("bad token status %d", badRec.Code)
	}
}

func TestWebhookDispatchesTextAndIgnoresEcho(t *testing.T) {
	got := make(chan IncomingMessage, 2)
	gateway := NewGateway(testAccount(), func(msg IncomingMessage) { got <- msg })
	body := []byte(`{
		"object":"page",
		"entry":[{
			"id":"page-1",
			"messaging":[
				{"sender":{"id":"1001"},"message":{"mid":"m1","text":" hello ","is_echo":true}},
				{"sender":{"id":"1001"},"message":{"mid":"m2","text":" hello "}},
				{"sender":{"id":"9999"},"message":{"mid":"m3","text":"blocked"}},
				{"sender":{"id":"1001"},"delivery":{"mids":["m2"]}},
				{"sender":{"id":"1001"},"postback":{"title":"Yes","payload":"YES"}}
			]
		}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sign("app-secret", body))
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "EVENT_RECEIVED" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	gateway.WaitHandlers()
	close(got)
	var texts []string
	for msg := range got {
		if msg.PSID != "1001" {
			t.Fatalf("sender %s", msg.PSID)
		}
		texts = append(texts, msg.Text)
	}
	if len(texts) != 2 || texts[0] != "hello" || texts[1] != "Postback: Yes\nPayload: YES" {
		t.Fatalf("messages %#v", texts)
	}
}

func TestWebhookRejectsBadSignatureAndForeignObject(t *testing.T) {
	gateway := NewGateway(testAccount(), func(IncomingMessage) { t.Fatal("handler must not run") })
	body := []byte(`{"object":"page","entry":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sign("nope", body))
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}

	foreign := []byte(`{"object":"user","entry":[]}`)
	freq := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(string(foreign)))
	freq.Header.Set("X-Hub-Signature-256", sign("app-secret", foreign))
	frec := httptest.NewRecorder()
	gateway.ServeHTTP(frec, freq)
	if frec.Code != http.StatusNotFound {
		t.Fatalf("object status %d", frec.Code)
	}
}

func TestOpenPolicyAllowsAnyPSID(t *testing.T) {
	account := testAccount()
	account.DMPolicy = DMPolicyOpen
	account.AllowFrom = nil
	if !account.SenderAllowed("555") {
		t.Fatal("open policy must allow an unknown PSID")
	}
	account.DMPolicy = DMPolicyDisabled
	if account.SenderAllowed("1001") {
		t.Fatal("disabled policy must deny the allow list")
	}
	account = testAccount()
	if !account.SenderAllowed("messenger:1001") {
		t.Fatal("prefixed PSID must match the allow list")
	}
}

func TestSendTextAndMedia(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "page-token" {
			t.Fatalf("token query = %q", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.Path, "/v25.0/page-1/messages") {
			t.Fatalf("path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(map[string]string{"message_id": "mid-1", "recipient_id": "1001"})
	}))
	defer server.Close()

	gateway := NewGateway(testAccount(), nil)
	gateway.SetAPIBase(server.URL)
	id, err := gateway.Send(t.Context(), SendRequest{To: "psid:1001", Text: "hello", ReplyToID: "m2"})
	if err != nil || id != "mid-1" {
		t.Fatalf("send text id=%q err=%v", id, err)
	}
	if bodies[0]["messaging_type"] != "RESPONSE" {
		t.Fatalf("body %#v", bodies[0])
	}
	reply, _ := bodies[0]["reply_to"].(map[string]any)
	if reply["mid"] != "m2" {
		t.Fatalf("reply_to %#v", bodies[0]["reply_to"])
	}

	_, err = gateway.Send(t.Context(), SendRequest{To: "1001", MediaURL: "https://cdn.example.com/a.png", Text: "pic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 {
		t.Fatalf("graph calls = %d, want text then attachment", len(bodies))
	}
	textMessage, _ := bodies[1]["message"].(map[string]any)
	if textMessage["text"] != "pic" || textMessage["attachment"] != nil {
		t.Fatalf("caption %#v", textMessage)
	}
	fileMessage, _ := bodies[2]["message"].(map[string]any)
	attachment, _ := fileMessage["attachment"].(map[string]any)
	if attachment["type"] != "image" || fileMessage["text"] != nil {
		t.Fatalf("attachment %#v", fileMessage)
	}

	long := strings.Repeat("a", MessageTextLimit) + " " + strings.Repeat("b", MessageTextLimit)
	if _, err := gateway.Send(t.Context(), SendRequest{To: "1001", Text: long, MediaURL: "https://cdn.example.com/a.png"}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 6 {
		t.Fatalf("graph calls = %d, want caption chunks then attachment", len(bodies))
	}
	for _, body := range bodies[3:5] {
		message, _ := body["message"].(map[string]any)
		text, _ := message["text"].(string)
		if message["attachment"] != nil || len(utf16.Encode([]rune(text))) > MessageTextLimit {
			t.Fatalf("chunk %#v", message)
		}
	}
	last, _ := bodies[5]["message"].(map[string]any)
	if _, ok := last["attachment"]; !ok || last["text"] != nil {
		t.Fatalf("attachment after caption %#v", last)
	}
}

func TestSendErrorRedactsPageAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "bad access_token=SECRET", "code": 190},
		})
	}))
	defer server.Close()
	gateway := NewGateway(testAccount(), nil)
	gateway.SetAPIBase(server.URL)
	_, err := gateway.Send(t.Context(), SendRequest{To: "1001", Text: "hi"})
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "access_token=REDACTED") {
		t.Fatalf("redact %v", err)
	}
}

func TestSendSplitsLongText(t *testing.T) {
	parts := splitMessengerText(strings.Repeat("a", 10)+" "+strings.Repeat("b", 10), 12)
	if len(parts) < 2 {
		t.Fatalf("parts %#v", parts)
	}
	for _, part := range parts {
		if len(utf16.Encode([]rune(part))) > 12 {
			t.Fatalf("part too long: %q", part)
		}
	}
	emoji := string(rune(0x1F600))
	parts = splitMessengerText(strings.Repeat(emoji, 1001), 2000)
	if len(parts) < 2 {
		t.Fatalf("emoji parts %d", len(parts))
	}
	for _, part := range parts {
		if units := len(utf16.Encode([]rune(part))); units > 2000 {
			t.Fatalf("utf16 units %d", units)
		}
	}
	path, err := NormalizeWebhookPath("//")
	if err != nil || path != DefaultWebhookPath {
		t.Fatalf("slash path %q err=%v", path, err)
	}
}

func TestGatewayServesSubscribedChallenge(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount()
	account.ListenPort = ln.Addr().(*net.TCPAddr).Port
	account.ListenHost = "127.0.0.1"
	gateway := NewGateway(account, nil)
	if err := gateway.serve(ln); err != nil {
		t.Fatal(err)
	}
	defer gateway.Stop()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(account.LocalWebhookURL() + "?hub.mode=subscribe&hub.verify_token=verify-token&hub.challenge=ok")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(raw) != "ok" {
		t.Fatalf("status %d body %q", resp.StatusCode, raw)
	}
}

func TestCallbackURLPrefersPublicBase(t *testing.T) {
	account := testAccount()
	account.PublicBaseURL = "https://example.com/hook/"
	account = account.Normalized()
	if account.PublicBaseURL != "https://example.com/hook" {
		t.Fatalf("public base %s", account.PublicBaseURL)
	}
	if got := account.CallbackURL(); got != "https://example.com/hook"+DefaultWebhookPath {
		t.Fatalf("callback %s", got)
	}
	account.PublicBaseURL = "https://example.com"
	account = account.Normalized()
	if got := account.CallbackURL(); got != "https://example.com"+DefaultWebhookPath {
		t.Fatalf("callback %s", got)
	}
	account.PublicBaseURL = "https://example.com" + DefaultWebhookPath
	account = account.Normalized()
	if got := account.CallbackURL(); got != account.PublicBaseURL {
		t.Fatalf("doubled callback %s", got)
	}
	if _, err := NormalizePublicBaseURL("ftp://example.com"); err == nil {
		t.Fatal("ftp must be rejected")
	}
	if _, err := NormalizeDMPolicy("pairing"); err == nil {
		t.Fatal("pairing is not a desktop policy")
	}
}

func TestNormalizeAllowFromCapsAndDedupes(t *testing.T) {
	ids := []string{"psid:1001", "1001", " "}
	for i := 0; i < MaxAllowFrom+10; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	out := NormalizeAllowFrom(ids)
	if len(out) != MaxAllowFrom {
		t.Fatalf("len %d", len(out))
	}
	if out[0] != "1001" {
		t.Fatalf("first %s", out[0])
	}
}

func TestVerifyAcceptsSignedEmptyBody(t *testing.T) {
	if !VerifySignature(nil, "app-secret", sign("app-secret", nil)) {
		t.Fatal("signed empty body must verify")
	}
	if VerifySignature([]byte("x"), "  ", sign("app-secret", []byte("x"))) {
		t.Fatal("empty secret must fail")
	}
}

func TestWebhookIgnoresOtherPageAndSurvivesHandlerPanic(t *testing.T) {
	var mu sync.Mutex
	var got []string
	gateway := NewGateway(testAccount(), func(msg IncomingMessage) {
		if msg.Text == "boom" {
			panic("boom")
		}
		mu.Lock()
		got = append(got, msg.Text)
		mu.Unlock()
	})
	body := []byte(`{"object":"page","entry":[
		{"id":"other-page","messaging":[{"sender":{"id":"1001"},"message":{"mid":"m0","text":"foreign"}}]},
		{"id":"page-1","messaging":[
			{"sender":{"id":"1001"},"message":{"mid":"m1","text":"boom"}},
			{"sender":{"id":"1001"},"message":{"mid":"m2","text":"after"}}
		]}
	]}`)
	req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sign("app-secret", body))
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	gateway.WaitHandlers()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "after" {
		t.Fatalf("messages %#v", got)
	}
}

func TestWebhookRetriesMessageAfterHandlerPanic(t *testing.T) {
	var mu sync.Mutex
	panicked := false
	var got []string
	account := testAccount()
	account.DMPolicy = DMPolicyOpen
	gateway := NewGateway(account, func(msg IncomingMessage) {
		mu.Lock()
		shouldPanic := msg.Text == "boom" && !panicked
		if shouldPanic {
			panicked = true
		}
		mu.Unlock()
		if shouldPanic {
			panic("boom")
		}
		mu.Lock()
		got = append(got, msg.Text)
		mu.Unlock()
	})
	body := []byte(`{"object":"page","entry":[{"id":"page-1","messaging":[
		{"sender":{"id":"1001"},"message":{"mid":"m-boom","text":"boom"}}
	]}]}`)
	post := func() {
		req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", sign("app-secret", body))
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		gateway.WaitHandlers()
	}
	post()
	post()
	mu.Lock()
	defer mu.Unlock()
	if !panicked || len(got) != 1 || got[0] != "boom" {
		t.Fatalf("messages %#v panicked=%v", got, panicked)
	}
}

func TestWebhookDropsPageEchoAndDuplicateDelivery(t *testing.T) {
	var mu sync.Mutex
	var got []string
	account := testAccount()
	account.DMPolicy = DMPolicyOpen
	gateway := NewGateway(account, func(msg IncomingMessage) {
		mu.Lock()
		got = append(got, msg.Text)
		mu.Unlock()
	})
	body := []byte(`{"object":"page","entry":[{"id":"page-1","messaging":[
		{"sender":{"id":"page-1"},"message":{"mid":"echo","text":"from the page"}},
		{"sender":{"id":"1001"},"message":{"mid":"m-once","text":"once"}}
	]}]}`)
	post := func() {
		req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", sign("app-secret", body))
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	post()
	post()
	gateway.WaitHandlers()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "once" {
		t.Fatalf("messages %#v", got)
	}
}

func TestStopReleasesListenPort(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	account := testAccount()
	account.ListenHost = "127.0.0.1"
	account.ListenPort = port
	gateway := NewGateway(account, nil)
	if err := gateway.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Stop(); err != nil {
		t.Fatal(err)
	}
	again, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	_ = again.Close()
	base := testAccount()
	moved := base
	moved.WebhookPath = "/other"
	if base.ListenKey() != moved.ListenKey() {
		t.Fatalf("path change rebound %q %q", base.ListenKey(), moved.ListenKey())
	}
}

func TestRememberMessageKeepsRecentDuplicatesAtCap(t *testing.T) {
	gateway := NewGateway(testAccount(), nil)
	now := time.Now()
	gateway.seen = make(map[string]time.Time, 1024)
	for i := 0; i < 1024; i++ {
		gateway.seen[strconv.Itoa(i)] = now.Add(time.Duration(i) * time.Millisecond)
	}
	recent := IncomingMessage{PSID: "1001", MessageID: "1023", Text: "t"}
	if gateway.rememberMessage(recent) {
		t.Fatal("duplicate")
	}
	if !gateway.rememberMessage(IncomingMessage{PSID: "1001", MessageID: "new", Text: "t"}) {
		t.Fatal("new id")
	}
	if gateway.rememberMessage(recent) {
		t.Fatal("recent id was forgotten when the dedupe map filled")
	}
	if _, ok := gateway.seen["0"]; ok {
		t.Fatal("oldest id was kept")
	}
}

func TestStartListensAgainAfterTheServerExits(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	account := testAccount()
	account.ListenHost = "127.0.0.1"
	account.ListenPort = port
	gateway := NewGateway(account, nil)
	if err := gateway.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gateway.Stop() })
	gateway.mu.Lock()
	srv := gateway.server
	gateway.mu.Unlock()
	if srv == nil {
		t.Fatal("server")
	}
	_ = srv.Close()
	deadline := time.Now().Add(2 * time.Second)
	for gateway.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if gateway.Running() {
		t.Fatal("still running after the listener closed")
	}
	if err := gateway.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestParseIncomingKeepsCaptionAttachmentsEditsAndExactIDs(t *testing.T) {
	body := []byte(`{
		"object":"page",
		"entry":[{"id":"page-1","messaging":[
			{"sender":{"id":"1001"},"timestamp":10,"message":{
				"mid":"m1",
				"text":"see this",
				"quick_reply":{"payload":9007199254740993},
				"attachments":[
					{"type":"image","payload":{"url":"https://example.com/a.png"}},
					{"type":"sticker","payload":{"sticker_id":9007199254740993}},
					{"type":"location","payload":{"coordinates":{"lat":1.5,"long":2.25}}},
					{"type":"fallback","payload":{"title":"Tahiti","url":"https://example.com/v"}},
					{"type":"appointment_booking","payload":{"booking_id":"b1","status":"confirmed","start_time":100,"end_time":200,"timezone":"UTC"}}
				]
			}},
			{"sender":{"id":"1001"},"timestamp":11,"message_edit":{"mid":"m1","text":"see that","num_edit":2}}
		]}]
	}`)
	object, messages, err := ParseIncoming(body)
	if err != nil || object != "page" || len(messages) != 2 {
		t.Fatalf("parse object=%s messages=%d err=%v", object, len(messages), err)
	}
	want := strings.Join([]string{
		"see this",
		"[Messenger image attachment: https://example.com/a.png]",
		"[Messenger sticker attachment: 9007199254740993]",
		"[Messenger location attachment: 1.5,2.25]",
		"[Messenger fallback attachment: Tahiti https://example.com/v]",
		"[Messenger appointment_booking attachment: confirmed 100-200 UTC b1]",
		"Quick reply: 9007199254740993",
	}, "\n")
	if messages[0].Text != want || messages[0].MessageID != "m1" || messages[0].EditNum != 0 {
		t.Fatalf("caption message %#v", messages[0])
	}
	if messages[1].Text != "Edited: see that" || messages[1].MessageID != "m1" || messages[1].EditNum != 2 {
		t.Fatalf("edit %#v", messages[1])
	}

	gateway := NewGateway(testAccount(), nil)
	if !gateway.rememberMessage(messages[0]) || gateway.rememberMessage(messages[0]) {
		t.Fatal("original dedupe")
	}
	if !gateway.rememberMessage(messages[1]) {
		t.Fatal("edit was dropped as a duplicate of the original mid")
	}
	if gateway.rememberMessage(messages[1]) {
		t.Fatal("edit retry was dispatched twice")
	}
}

func TestParseIncomingKeepsStickerPostAndReelIdentity(t *testing.T) {
	body := []byte(`{
		"object":"page",
		"entry":[{"id":"page-1","messaging":[
			{"sender":{"id":"1001"},"timestamp":12,"message":{
				"mid":"m-ids",
				"attachments":[
					{"type":"image","payload":{"url":"https://example.com/old-sticker.png","sticker_id":369239263222822}},
					{"type":"sticker","payload":{"url":"https://example.com/like.png","sticker_id":9007199254740993}},
					{"type":"post","payload":{"title":"Hello","url":"https://example.com/post","id":9007199254740993}},
					{"type":"reel","payload":{"title":"Clip","url":"https://example.com/reel.mp4","reel_video_id":9007199254740993}},
					{"type":"ig_reel","payload":{"reel_video_id":42}}
				]
			}}
		]}]
	}`)
	object, messages, err := ParseIncoming(body)
	if err != nil || object != "page" || len(messages) != 1 {
		t.Fatalf("parse object=%s messages=%d err=%v", object, len(messages), err)
	}
	want := strings.Join([]string{
		"[Messenger image attachment: https://example.com/old-sticker.png 369239263222822]",
		"[Messenger sticker attachment: https://example.com/like.png 9007199254740993]",
		"[Messenger post attachment: Hello https://example.com/post 9007199254740993]",
		"[Messenger reel attachment: Clip https://example.com/reel.mp4 9007199254740993]",
		"[Messenger ig_reel attachment: 42]",
	}, "\n")
	if messages[0].Text != want || messages[0].MessageID != "m-ids" {
		t.Fatalf("identity message %#v", messages[0])
	}
}

func TestParseIncomingKeepsReferralsProductsAndLeads(t *testing.T) {
	body := []byte(`{
		"object":"page",
		"entry":[{"id":"page-1","messaging":[
			{"sender":{"id":"1001"},"timestamp":20,"message":{
				"mid":"m-ad",
				"text":"hello",
				"referral":{
					"ref":"spring",
					"ad_id":9007199254740993,
					"source":"ADS",
					"type":"OPEN_THREAD",
					"ads_context_data":{
						"ad_title":"Summer hats",
						"photo_url":"https://example.com/hat.png",
						"post_id":9007199254740997,
						"product_id":"sku-1"
					}
				},
				"attachments":[{
					"type":"template",
					"payload":{"product":{"elements":[{
						"id":9007199254740993,
						"retailer_id":"ext-1",
						"image_url":"https://example.com/p.png",
						"title":"Hat",
						"subtitle":"$40"
					}]}}
				}]
			}},
			{"sender":{"id":"1001"},"timestamp":21,"message":{
				"mid":"m-shop",
				"text":"this one",
				"referral":{"product":{"id":9007199254740993}}
			}},
			{"sender":{"id":"1001"},"timestamp":22,"postback":{
				"mid":"m-pb",
				"title":"Get Started",
				"payload":"START",
				"referral":{"ref":"mme1","source":"SHORTLINK","type":"OPEN_THREAD"}
			}},
			{"sender":{"id":"1001"},"timestamp":23,"referral":{
				"source":"ADS",
				"type":"LEAD_COMPLETE",
				"ad_id":"6302572858686"
			},"lead":{"data":[{"question":"City","answer":"Menlo Park"}]}},
			{"sender":{"id":"1001"},"timestamp":24,"message":{
				"mid":"m-post",
				"attachments":[{"type":"post","payload":{"id":9007199254740993}}]
			}}
		]}]
	}`)
	object, messages, err := ParseIncoming(body)
	if err != nil || object != "page" || len(messages) != 5 {
		t.Fatalf("parse object=%s messages=%d err=%v", object, len(messages), err)
	}
	wantAd := strings.Join([]string{
		"hello",
		"[Messenger template attachment: Hat $40 https://example.com/p.png 9007199254740993 ext-1]",
		"Referral: ADS OPEN_THREAD",
		"Ref: spring",
		"Ad id: 9007199254740993",
		"Ad: Summer hats",
		"Product: sku-1",
		"Photo: https://example.com/hat.png",
		"Post: 9007199254740997",
	}, "\n")
	if messages[0].Text != wantAd || messages[0].MessageID != "m-ad" {
		t.Fatalf("ad message %#v", messages[0])
	}
	if messages[1].Text != "this one\nProduct: 9007199254740993" || messages[1].MessageID != "m-shop" {
		t.Fatalf("shop message %#v", messages[1])
	}
	wantPostback := "Postback: Get Started\nPayload: START\nReferral: SHORTLINK OPEN_THREAD\nRef: mme1"
	if messages[2].Text != wantPostback || messages[2].MessageID != "m-pb" {
		t.Fatalf("postback %#v", messages[2])
	}
	wantLead := "Referral: ADS LEAD_COMPLETE\nAd id: 6302572858686\nLead: City Menlo Park"
	if messages[3].Text != wantLead || messages[3].MessageID != "" {
		t.Fatalf("lead %#v", messages[3])
	}
	if messages[4].Text != "[Messenger post attachment: 9007199254740993]" || messages[4].MessageID != "m-post" {
		t.Fatalf("post %#v", messages[4])
	}
}

func TestParseIncomingLeadSummaryIsNotQuoted(t *testing.T) {
	body := []byte(`{
		"object":"page",
		"entry":[{"id":"page-1","messaging":[{
			"sender":{"id":"1001"},
			"recipient":{"id":"page-1"},
			"timestamp":1661209328,
			"message":{
				"mid":"m_synthetic_summary",
				"text":"Lead summary:\nAre you interested in becoming a reseller?: Yes\nWhat city is your store located?: Menlo Park"
			},
			"hop_context":{"app_id":498721317747541,"metadata":"messenger_lead_gen_complete"}
		},{
			"sender":{"id":"1001"},
			"timestamp":1661209400,
			"message":{"mid":"m_synthetic_incomplete","text":"Lead ended early"},
			"hop_context":{"app_id":"498721317747541","metadata":"messenger_lead_gen_incomplete"}
		}]}]
	}`)
	object, messages, err := ParseIncoming(body)
	if err != nil || object != "page" || len(messages) != 2 {
		t.Fatalf("parse object=%s messages=%d err=%v", object, len(messages), err)
	}
	if messages[0].MessageID != "" || messages[1].MessageID != "" {
		t.Fatalf("synthetic mid was kept for reply_to: %#v / %#v", messages[0], messages[1])
	}
	if !strings.Contains(messages[0].Text, "Lead summary:") || !strings.Contains(messages[0].Text, "Menlo Park") {
		t.Fatalf("summary text %#v", messages[0])
	}
	if messages[1].Text != "Lead ended early" {
		t.Fatalf("incomplete summary %#v", messages[1])
	}
	if messages[0].PSID != "1001" || messages[0].PageID != "page-1" {
		t.Fatalf("identity %#v", messages[0])
	}
}

func TestNormalizeListenHostAndPublicURL(t *testing.T) {
	if _, err := NormalizeListenHost("127.0.0.1:8791"); err == nil {
		t.Fatal("host must not include a port")
	}
	host, err := NormalizeListenHost("[::1]")
	if err != nil || host != "::1" {
		t.Fatalf("ipv6 host %q err=%v", host, err)
	}
	base, err := NormalizePublicBaseURL("https://user:pass@example.com/hook/")
	if err != nil || base != "https://example.com/hook" {
		t.Fatalf("base %q err=%v", base, err)
	}
	if strings.Contains(base, "pass") {
		t.Fatal(base)
	}
	err = redactMessengerError(errors.New(`Post "https://graph.facebook.com/v25.0/1/messages?access_token=SECRET&x=1": dial`))
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "access_token=REDACTED") {
		t.Fatalf("redact %v", err)
	}
}

func TestParseIncomingKeepsReplyTarget(t *testing.T) {
	body := []byte(`{
		"object":"page",
		"entry":[{"id":"page-1","messaging":[
			{"sender":{"id":"1001"},"timestamp":1458692752478,"message":{
				"mid":"m_1457764197618:41d102a3e1ae206a38",
				"text":"hello, world!",
				"reply_to":{"mid":"m_1fTq8oLumEyIp3Q2MR-aY7IfLZDamVrALniheU","is_self_reply":false}
			}},
			{"sender":{"id":"1001"},"timestamp":1458692752479,"message":{
				"mid":"m-self",
				"text":"again",
				"reply_to":{"mid":"m-own","is_self_reply":true}
			}},
			{"sender":{"id":"1001"},"timestamp":1458692752480,"message":{"mid":"m-plain","text":"no quote"}}
		]}]
	}`)
	object, messages, err := ParseIncoming(body)
	if err != nil || object != "page" || len(messages) != 3 {
		t.Fatalf("parse object=%s messages=%d err=%v", object, len(messages), err)
	}
	if messages[0].Text != "hello, world!" || messages[0].MessageID != "m_1457764197618:41d102a3e1ae206a38" || messages[0].ReplyToMID != "m_1fTq8oLumEyIp3Q2MR-aY7IfLZDamVrALniheU" {
		t.Fatalf("reply %#v", messages[0])
	}
	if messages[0].ReplyQuote != "" {
		t.Fatalf("parse stored a quote body %#v", messages[0])
	}
	messages[0].ReplyQuote = "ship it"
	if messageDedupeKey(messages[0]) != messages[0].MessageID {
		t.Fatalf("dedupe key %q", messageDedupeKey(messages[0]))
	}
	if messages[1].Text != "again" || messages[1].ReplyToMID != "m-own" {
		t.Fatalf("self reply %#v", messages[1])
	}
	if messages[2].ReplyToMID != "" || messages[2].Text != "no quote" {
		t.Fatalf("plain %#v", messages[2])
	}
	if TextWithReplyQuote("thanks", "ship it") != "Reply to: ship it\nthanks" {
		t.Fatalf("prefix %q", TextWithReplyQuote("thanks", "ship it"))
	}
	if TextWithReplyQuote("ok", "line one\nline two") != "Reply to: line one line two\nok" {
		t.Fatalf("collapsed %q", TextWithReplyQuote("ok", "line one\nline two"))
	}
	if TextWithReplyQuote("/help", "") != "/help" || TextWithReplyQuote("thanks", " ") != "thanks" {
		t.Fatal("empty quote must leave the user text")
	}
}

func TestReplyQuoteResolvesSeenText(t *testing.T) {
	var mu sync.Mutex
	var got []IncomingMessage
	gateway := NewGateway(testAccount(), func(msg IncomingMessage) {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
	})
	post := func(body string) {
		t.Helper()
		raw := []byte(body)
		req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sign("app-secret", raw))
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"object":"page","entry":[{"id":"page-1","messaging":[
		{"sender":{"id":"1001"},"message":{"mid":"m-echo","text":"hidden","is_echo":true}},
		{"sender":{"id":"1001"},"message":{"mid":"m-user","text":"ship it"}}
	]}]}`)
	gateway.WaitHandlers()
	if gateway.QuotedText("m-echo") != "" {
		t.Fatalf("echo was stored %q", gateway.QuotedText("m-echo"))
	}
	if gateway.QuotedText("m-user") != "ship it" {
		t.Fatalf("inbound quote %q", gateway.QuotedText("m-user"))
	}

	var n int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "bad", "code": 100}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message_id": "m-sent-" + strconv.Itoa(n), "recipient_id": "1001"})
	}))
	defer server.Close()
	gateway.SetAPIBase(server.URL)
	if _, err := gateway.Send(t.Context(), SendRequest{To: "1001", Text: "nope"}); err == nil {
		t.Fatal("failed send must not succeed")
	}
	if gateway.QuotedText("m-sent-1") != "" {
		t.Fatal("failed send was stored")
	}
	id, err := gateway.Send(t.Context(), SendRequest{To: "1001", Text: "on it", MediaURL: "https://cdn.example.com/a.png"})
	if err != nil || id != "m-sent-3" {
		t.Fatalf("send id=%q err=%v", id, err)
	}
	if gateway.QuotedText("m-sent-2") != "on it" || gateway.QuotedText("m-sent-3") != "https://cdn.example.com/a.png" {
		t.Fatalf("sent quotes %q %q", gateway.QuotedText("m-sent-2"), gateway.QuotedText("m-sent-3"))
	}

	post(`{"object":"page","entry":[{"id":"page-1","messaging":[
		{"sender":{"id":"1001"},"message":{"mid":"m-reply","text":"thanks","reply_to":{"mid":"m-user","is_self_reply":false}}}
	]}]}`)
	gateway.WaitHandlers()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[1].Text != "thanks" || got[1].ReplyToMID != "m-user" || got[1].ReplyQuote != "ship it" {
		t.Fatalf("handler %#v", got)
	}
	agent := TextWithReplyQuote(got[1].Text, got[1].ReplyQuote)
	if agent != "Reply to: ship it\nthanks" {
		t.Fatalf("agent text %q", agent)
	}
	if TextWithReplyQuote(got[1].Text, gateway.QuotedText("missing")) != "thanks" {
		t.Fatal("missing quote changed the turn")
	}
	long := strings.Repeat("字", maxQuoteRunes+40)
	gateway.rememberQuote("m-long", long)
	stored := []rune(gateway.QuotedText("m-long"))
	if string(stored) != strings.Repeat("字", maxQuoteRunes)+"…" {
		t.Fatalf("truncated quote len %d", len(stored))
	}
}

func TestFullQuoteCacheKeepsTheRepliedMessage(t *testing.T) {
	var mu sync.Mutex
	var quote string
	gateway := NewGateway(testAccount(), func(msg IncomingMessage) {
		mu.Lock()
		quote = msg.ReplyQuote
		mu.Unlock()
	})
	gateway.rememberQuote("parent", "original")
	gateway.quoteMu.Lock()
	rec := gateway.quotes["parent"]
	rec.at = time.Now().Add(-time.Hour)
	gateway.quotes["parent"] = rec
	gateway.quoteMu.Unlock()
	for i := 0; i < maxQuoteEntries-1; i++ {
		gateway.rememberQuote("fill-"+strconv.Itoa(i), "x")
	}
	if len(gateway.quotes) != maxQuoteEntries || gateway.QuotedText("parent") != "original" {
		t.Fatalf("cache %d parent %q", len(gateway.quotes), gateway.QuotedText("parent"))
	}
	body := `{"object":"page","entry":[{"id":"page-1","messaging":[
		{"sender":{"id":"1001"},"message":{"mid":"m-reply","text":"thanks","reply_to":{"mid":"parent","is_self_reply":false}}}
	]}]}`
	raw := []byte(body)
	req := httptest.NewRequest(http.MethodPost, "/facebook-messenger/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign("app-secret", raw))
	recHTTP := httptest.NewRecorder()
	gateway.ServeHTTP(recHTTP, req)
	if recHTTP.Code != http.StatusOK {
		t.Fatalf("status %d", recHTTP.Code)
	}
	gateway.WaitHandlers()
	mu.Lock()
	defer mu.Unlock()
	if quote != "original" {
		t.Fatalf("reply quote %q", quote)
	}
	if gateway.QuotedText("parent") != "" {
		t.Fatal("storing the reply should evict the oldest parent after it was copied")
	}
	if gateway.QuotedText("m-reply") != "thanks" {
		t.Fatalf("reply stored %q", gateway.QuotedText("m-reply"))
	}
}
