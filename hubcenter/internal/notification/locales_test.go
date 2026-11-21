package notification

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type recordingTranslator struct {
	mu    sync.Mutex
	calls int
}

func (r *recordingTranslator) TranslateFields(context.Context, string, string, LocaleCopy) (LocaleCopy, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return LocaleCopy{}, errors.New("model down")
}

type fixedTranslator struct {
	reply LocaleCopy
}

func (f fixedTranslator) TranslateFields(context.Context, string, string, LocaleCopy) (LocaleCopy, error) {
	return f.reply, nil
}

func TestFillNotificationLocalesFillsDefaultButton(t *testing.T) {
	_, locales := fillNotificationLocales(context.Background(), "zh", []string{LangEn}, LocaleCopy{
		Title: "你好", Content: "正文", ButtonText: DefaultPinnedButtonText,
	}, fixedTranslator{reply: LocaleCopy{Title: "Hello", Content: "Body"}})
	if locales[LangZhHans].ButtonText != DefaultPinnedButtonText || locales[LangEn].ButtonText != "View details" {
		t.Fatalf("buttons = %#v", locales)
	}
}

func TestFillNotificationLocalesOmitsFailedLanguage(t *testing.T) {
	tr := &recordingTranslator{}
	lang, locales := fillNotificationLocales(context.Background(), "zh", []string{LangEn}, LocaleCopy{
		Title: "你好", Content: "正文", ButtonText: "查看详情",
	}, tr)
	if lang != LangZhHans {
		t.Fatalf("source lang = %q", lang)
	}
	if tr.calls != 1 {
		t.Fatalf("calls = %d", tr.calls)
	}
	if _, ok := locales[LangEn]; ok {
		t.Fatalf("failed language stored: %#v", locales)
	}
	if locales[LangZhHans].ButtonText != "查看详情" {
		t.Fatalf("source button = %#v", locales[LangZhHans])
	}
}

func TestEmptyTargetLangsDoesNotTranslate(t *testing.T) {
	tr := &recordingTranslator{}
	_, locales := fillNotificationLocales(context.Background(), LangEn, []string{}, LocaleCopy{
		Title: "Hello", Content: "Body",
	}, tr)
	if tr.calls != 0 || len(locales) != 1 || locales[LangEn].Title != "Hello" {
		t.Fatalf("calls=%d locales=%#v", tr.calls, locales)
	}
}

func TestTranslationUserPromptKeepsEmptyButton(t *testing.T) {
	prompt := TranslationUserPrompt(LangEn, LangMn, LocaleCopy{Title: "Hello", Content: "Body"})
	if !strings.Contains(prompt, `"button_text":""`) {
		t.Fatalf("prompt dropped the empty button: %s", prompt)
	}
}

func TestPrefilledLocalesSkipTranslator(t *testing.T) {
	tr := &recordingTranslator{}
	svc := &Service{translator: tr}
	lang, locales := svc.localesForCreate(context.Background(), CreateRequest{
		Title: "Hello", Content: "Body", SourceLang: LangEn,
		Locales: map[string]LocaleCopy{
			LangEn: {Title: "Hello", Content: "Body"},
			LangMn: {Title: "Сайн", Content: "бичвэр"},
		},
	})
	if tr.calls != 0 {
		t.Fatal("prefilled locales called the translator")
	}
	if lang != LangEn || locales[LangMn].Title != "Сайн" {
		t.Fatalf("stored %q %#v", lang, locales)
	}
}

func TestParseTranslatedLocaleStripsFence(t *testing.T) {
	got, err := ParseTranslatedLocale("```\n{\"title\":\"Hello\",\"content\":\"Body\",\"button_text\":\"View\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Hello" || got.ButtonText != "View" {
		t.Fatalf("parsed %#v", got)
	}
}
