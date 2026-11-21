package notification

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingTranslator struct {
	mu    sync.Mutex
	calls int
	err   error
	reply LocaleCopy
}

func (r *recordingTranslator) TranslateFields(context.Context, string, string, LocaleCopy) (LocaleCopy, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	if r.err != nil {
		return LocaleCopy{}, r.err
	}
	if r.reply.Title == "" && r.reply.Content == "" {
		return LocaleCopy{Title: "translated", Content: "translated body", ButtonText: "Open"}, nil
	}
	return r.reply, nil
}

func TestFillNotificationLocalesFillsDefaultButton(t *testing.T) {
	tr := &recordingTranslator{reply: LocaleCopy{Title: "Hello", Content: "Body", ButtonText: "查看详情"}}
	_, locales := fillNotificationLocales(context.Background(), LangZhHans, []string{LangEn, LangZhHant, LangMn}, LocaleCopy{
		Title: "你好", Content: "正文", ButtonText: DefaultPinnedButtonText,
	}, tr)
	if locales[LangEn].ButtonText != "View details" || locales[LangZhHant].ButtonText != "查看詳情" || locales[LangMn].ButtonText != "Дэлгэрэнгүй харах" {
		t.Fatalf("default buttons = en %q hant %q mn %q", locales[LangEn].ButtonText, locales[LangZhHant].ButtonText, locales[LangMn].ButtonText)
	}
	if locales[LangEn].Title != "Hello" {
		t.Fatalf("title = %q", locales[LangEn].Title)
	}
}

func TestFillNotificationLocalesKeepsCustomButton(t *testing.T) {
	tr := &recordingTranslator{reply: LocaleCopy{Title: "Hello", Content: "Body"}}
	_, locales := fillNotificationLocales(context.Background(), LangZhHans, []string{LangEn}, LocaleCopy{
		Title: "你好", Content: "正文", ButtonText: "打开",
	}, tr)
	if locales[LangEn].ButtonText != "" {
		t.Fatalf("custom button = %q, want the source button left for display fallback", locales[LangEn].ButtonText)
	}
	tr.reply.ButtonText = "Open"
	_, locales = fillNotificationLocales(context.Background(), LangZhHans, []string{LangEn}, LocaleCopy{
		Title: "你好", Content: "正文", ButtonText: "打开",
	}, tr)
	if locales[LangEn].ButtonText != "Open" {
		t.Fatalf("translated button = %q", locales[LangEn].ButtonText)
	}
}

func TestFillNotificationLocalesOmitsFailedLanguage(t *testing.T) {
	tr := &recordingTranslator{err: errors.New("model down")}
	lang, locales := fillNotificationLocales(context.Background(), "en", []string{LangZhHans, LangEn}, LocaleCopy{
		Title: "Hello", Content: "Body", ButtonText: "View",
	}, tr)
	if lang != LangEn {
		t.Fatalf("source lang = %q", lang)
	}
	if tr.calls != 1 {
		t.Fatalf("translate calls = %d, want 1", tr.calls)
	}
	if _, ok := locales[LangZhHans]; ok {
		t.Fatalf("failed language was stored: %#v", locales)
	}
	if locales[LangEn].Title != "Hello" {
		t.Fatalf("source copy = %#v", locales[LangEn])
	}
}

func TestEmptyTargetLangsDoesNotTranslate(t *testing.T) {
	tr := &recordingTranslator{}
	lang, locales := fillNotificationLocales(context.Background(), LangEn, []string{}, LocaleCopy{
		Title: "Hello", Content: "Body",
	}, tr)
	if lang != LangEn || tr.calls != 0 || len(locales) != 1 {
		t.Fatalf("lang=%s calls=%d locales=%#v", lang, tr.calls, locales)
	}
}

func TestOmittedTargetLangsTranslateTheOtherThree(t *testing.T) {
	tr := &recordingTranslator{}
	_, locales := fillNotificationLocales(context.Background(), LangEn, nil, LocaleCopy{
		Title: "Hello", Content: "Body",
	}, tr)
	if tr.calls != 3 || len(locales) != 4 {
		t.Fatalf("calls=%d locales=%#v", tr.calls, locales)
	}
}

func TestTranslationUserPromptKeepsEmptyButton(t *testing.T) {
	prompt := TranslationUserPrompt(LangZhHans, LangEn, LocaleCopy{Title: "你好", Content: "正文"})
	if !strings.Contains(prompt, `"button_text":""`) {
		t.Fatalf("prompt dropped the empty button: %s", prompt)
	}
}

func TestFillNotificationLocalesStopsWhenCanceled(t *testing.T) {
	started := make(chan struct{}, 1)
	tr := cancelTranslator{started: started}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		fillNotificationLocales(ctx, LangEn, nil, LocaleCopy{Title: "Hi", Content: "Body"}, tr)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("translator did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fill did not return after cancel")
	}
}

type cancelTranslator struct {
	started chan struct{}
}

func (c cancelTranslator) TranslateFields(ctx context.Context, _, _ string, _ LocaleCopy) (LocaleCopy, error) {
	select {
	case c.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return LocaleCopy{}, ctx.Err()
}

func TestFillNotificationLocalesNilTranslatorKeepsSource(t *testing.T) {
	lang, locales := fillNotificationLocales(context.Background(), "", nil, LocaleCopy{Title: "你好", Content: "正文"}, nil)
	if lang != LangZhHans {
		t.Fatalf("default source = %q", lang)
	}
	if len(locales) != 1 || locales[LangZhHans].Title != "你好" {
		t.Fatalf("locales = %#v", locales)
	}
}

func TestSubsystemCreateSkipsLocales(t *testing.T) {
	svc := &Service{translator: &recordingTranslator{}}
	lang, locales := svc.localesForCreate(context.Background(), CreateRequest{
		Title: "invite", Content: "join", Source: SourceCompanionGroupInvite, SourceLang: LangEn, TargetLangs: NotificationLangs,
	})
	if lang != "" || locales != nil {
		t.Fatalf("subsystem locales = %q %#v", lang, locales)
	}
	if svc.translator.(*recordingTranslator).calls != 0 {
		t.Fatal("subsystem create called the translator")
	}
}

func TestPrefilledLocalesSkipTranslator(t *testing.T) {
	svc := &Service{translator: &recordingTranslator{}}
	lang, locales := svc.localesForCreate(context.Background(), CreateRequest{
		Title: "Hello", Content: "Body", SourceLang: LangEn,
		Locales: map[string]LocaleCopy{
			LangEn:     {Title: "Hello", Content: "Body"},
			LangZhHans: {Title: "你好", Content: "正文"},
		},
	})
	if lang != LangEn || locales[LangZhHans].Title != "你好" {
		t.Fatalf("stored %q %#v", lang, locales)
	}
	if svc.translator.(*recordingTranslator).calls != 0 {
		t.Fatal("prefilled locales called the translator")
	}
}

func TestParseTranslatedLocaleStripsFence(t *testing.T) {
	got, err := ParseTranslatedLocale("```json\n{\"title\":\"你好\",\"content\":\"正文\",\"button_text\":\"查看\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "你好" || got.Content != "正文" || got.ButtonText != "查看" {
		t.Fatalf("parsed %#v", got)
	}
}

func TestParseTranslatedLocaleRejectsEmpty(t *testing.T) {
	if _, err := ParseTranslatedLocale(`{"title":"","content":""}`); err == nil {
		t.Fatal("expected empty translation to fail")
	}
}

func TestCreateFromCascadeKeepsLocalesWithoutTranslator(t *testing.T) {
	svc, store, _ := setupTestService(t)
	tr := &recordingTranslator{}
	svc.SetTranslator(tr)
	ctx := context.Background()
	incoming := &Notification{
		ID:           "hc-locale-1",
		Title:        "Hello",
		Content:      "Body",
		Category:     CategorySecurityAlert,
		Priority:     PriorityNormal,
		AudienceType: AudienceAll,
		SourceLang:   LangEn,
		Locales: map[string]LocaleCopy{
			LangEn:     {Title: "Hello", Content: "Body", ButtonText: "View"},
			LangZhHans: {Title: "你好", Content: "正文", ButtonText: "查看"},
		},
		ButtonText: "View",
	}
	if err := svc.CreateFromCascade(ctx, CascadeRequest{Notification: incoming}); err != nil {
		t.Fatalf("CreateFromCascade: %v", err)
	}
	if tr.calls != 0 {
		t.Fatal("cascade called the translator")
	}
	found, err := store.FindBySource(ctx, "hubcenter", "hc-locale-1")
	if err != nil || found == nil {
		t.Fatalf("find cascaded notice: %v", err)
	}
	if found.SourceLang != LangEn || found.Locales[LangZhHans].Title != "你好" || found.Locales[LangZhHans].ButtonText != "查看" {
		t.Fatalf("stored locales = %q %#v", found.SourceLang, found.Locales)
	}
	if strings.TrimSpace(found.Title) != "Hello" {
		t.Fatalf("canonical title changed: %q", found.Title)
	}

	incoming.Title = "Hello again"
	incoming.SourceLang = "zh"
	incoming.Locales = map[string]LocaleCopy{
		"zh": {Title: "再次", Content: "正文", ButtonText: "这是一个超过二十个字的按钮名称不会被原样保存"},
	}
	if err := svc.CreateFromCascade(ctx, CascadeRequest{Notification: incoming}); err != nil {
		t.Fatalf("update cascade: %v", err)
	}
	if tr.calls != 0 {
		t.Fatal("cascade update called the translator")
	}
	found, err = store.FindBySource(ctx, "hubcenter", "hc-locale-1")
	if err != nil || found == nil {
		t.Fatalf("find updated notice: %v", err)
	}
	if found.SourceLang != LangZhHans || found.Locales[LangZhHans].Title != "再次" {
		t.Fatalf("updated locales = %q %#v", found.SourceLang, found.Locales)
	}
	if got := found.Locales[LangZhHans].ButtonText; len([]rune(got)) != 20 {
		t.Fatalf("button was not clamped: %q", got)
	}
}
