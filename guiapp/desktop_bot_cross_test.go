package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotUnderstandCatalogBindsAsOneString(t *testing.T) {
	method, ok := reflect.TypeOf((*App)(nil)).MethodByName("UnderstandDesktopBotTask")
	if !ok {
		t.Fatal("missing UnderstandDesktopBotTask")
	}
	catalogType := method.Type.In(method.Type.NumIn() - 1)
	if catalogType.Kind() != reflect.String {
		t.Fatalf("catalog type = %s", catalogType)
	}
	page := `{"self":"bot_1","bots":[{"id":"bot_6","name":"Bot 6","primary":false},{"id":"bot_1","name":"值班","primary":true}]}`
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	dest := reflect.New(catalogType).Interface()
	if err := json.Unmarshal(encoded, dest); err != nil {
		t.Fatal(err)
	}
	roster := parseDesktopBotRoster(reflect.ValueOf(dest).Elem().String())
	if roster == nil || roster.Self != "bot_1" || len(roster.Bots) != 2 {
		t.Fatalf("roster = %+v", roster)
	}
}

func TestDesktopBotPromptNamesBotsOnlyForThePrimary(t *testing.T) {
	roster := &desktopBotRoster{Self: "bot_1", Bots: []desktopBotRosterEntry{
		{ID: "bot_1", Name: "值班", Primary: true},
		{ID: "bot_6", Name: "Bot 6", Primary: false},
	}}
	system, user := desktopBotPromptsFor(roster)
	if !strings.Contains(user, "id=bot_6") || !strings.Contains(user, "name=Bot 6") || !strings.Contains(user, "primary=no") || !strings.Contains(user, "primary=yes") {
		t.Fatalf("user prompt = %s", user)
	}
	if !strings.Contains(system, "object field cross") {
		t.Fatalf("system prompt = %s", system)
	}
	other := &desktopBotRoster{Self: "bot_6", Bots: roster.Bots}
	system, user = desktopBotPromptsFor(other)
	if strings.Contains(user, "id=bot_6") || strings.Contains(system, "object field cross") {
		t.Fatalf("non-primary prompt leaked the roster:\n%s\n%s", system, user)
	}
}

func desktopBotPromptsFor(roster *desktopBotRoster) (string, string) {
	user := desktopBotUnderstandUser("跟 Bot 6 说一声", "plan", "zh-Hans", nil, "")
	system := desktopBotUnderstandSystem
	if rosterPrimary(roster) {
		system += "\n\n" + desktopBotCrossSystem
		user += desktopBotRosterPrompt(roster)
	}
	return system, user
}

func TestResolveDesktopBotTarget(t *testing.T) {
	bots := []desktopBotRosterEntry{
		{ID: "bot_1", Name: "值班", Primary: true},
		{ID: "bot_6", Name: "Bot 6"},
		{ID: "bot_7", Name: "Bot 6"},
	}
	ids, ok := resolveDesktopBotTargets("bot_6", "bot_1", bots)
	if !ok || len(ids) != 1 || ids[0] != "bot_6" {
		t.Fatalf("id match = %#v ok=%v", ids, ok)
	}
	if _, ok := resolveDesktopBotTargets("  Bot 6  ", "bot_1", bots); ok {
		t.Fatal("two bots with the same name were treated as one")
	}
	if _, ok := resolveDesktopBotTargets("bot_1", "bot_1", bots); ok {
		t.Fatal("the primary was allowed to target itself")
	}
	bots[2].Name = "记录"
	ids, ok = resolveDesktopBotTargets("记录", "bot_1", bots)
	if !ok || len(ids) != 1 || ids[0] != "bot_7" {
		t.Fatalf("unique name = %#v ok=%v", ids, ok)
	}
	ids, ok = resolveDesktopBotTargets("all", "bot_1", bots)
	if !ok || len(ids) != 2 {
		t.Fatalf("all = %#v ok=%v", ids, ok)
	}
	understood := &DesktopBotUnderstanding{
		Told: "好", Instruction: "打开页面", Pace: "direct",
		Cross: &DesktopBotCross{Kind: "notify", Target: "Bot 6", Text: "先别装。"},
	}
	finishDesktopBotCross(understood, &desktopBotRoster{Self: "bot_1", Bots: bots})
	if understood.Cross == nil || understood.Pace != "reply" || understood.Instruction != "" {
		t.Fatalf("resolved reading = %+v cross=%+v", understood, understood.Cross)
	}
	miss := &DesktopBotUnderstanding{Told: "好", Pace: "direct", Cross: &DesktopBotCross{Kind: "delegate", Target: "不存在", Text: "装这个", Order: "安装这个"}}
	finishDesktopBotCross(miss, &desktopBotRoster{Self: "bot_1", Bots: bots})
	if miss.Cross != nil || miss.Pace != "ask" || !strings.Contains(miss.Told, "没有这个 Bot") {
		t.Fatalf("ambiguous reading = %+v", miss)
	}
	other := &DesktopBotUnderstanding{Told: "好", Instruction: "去叫", Pace: "direct", Cross: &DesktopBotCross{Kind: "notify", Target: "bot_1", Text: "看一下"}}
	finishDesktopBotCross(other, &desktopBotRoster{Self: "bot_6", Bots: bots})
	if other.Cross != nil || other.Told != desktopBotNotPrimarySentence || other.Instruction != "" {
		t.Fatalf("non-primary reading = %+v", other)
	}
}

func TestDesktopBotInfoCopiesPrimaryFromTheList(t *testing.T) {
	var listed struct {
		Items []hubBot `json:"items"`
	}
	raw := []byte(`{"items":[{"id":"bot_1","name":"值班","created_at":"2024-01-15T00:30:00Z"},{"id":"bot_6","name":"Bot 6","created_at":"2024-02-01T00:00:00Z","primary":true}]}`)
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	first := desktopBotInfo(listed.Items[0])
	second := desktopBotInfo(listed.Items[1])
	if first.Primary || !second.Primary || second.Title != "Bot 6" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	wire, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"primary"`) {
		t.Fatalf("false primary was written onto the row: %s", wire)
	}
}

func TestPostDesktopBotCrossRefusesANonPrimaryAndFansOut(t *testing.T) {
	var posts []string
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		switch {
		case method == http.MethodGet && path == "/api/v1/bots":
			fillDesktopBotDest(t, dest, map[string]any{"items": []map[string]any{
				{"id": "bot_1", "name": "值班", "primary": true},
				{"id": "bot_6", "name": "Bot 6"},
				{"id": "bot_7", "name": "记录"},
			}})
			return http.StatusOK, nil
		case method == http.MethodPost && strings.HasSuffix(path, "/cross"):
			posts = append(posts, path+" "+string(crossMustJSON(t, body)))
			var sent struct {
				Target string `json:"target"`
			}
			raw := crossMustJSON(t, body)
			_ = json.Unmarshal(raw, &sent)
			fillDesktopBotDest(t, dest, map[string]any{"id": "note-" + sent.Target, "from": "bot_1", "target": sent.Target, "kind": "notify", "text": "先别装。"})
			return http.StatusOK, nil
		default:
			t.Fatalf("unexpected %s %s", method, path)
			return 0, nil
		}
	}
	t.Cleanup(func() { desktopBotExchangeOverride = nil })
	app := &App{}
	if _, err := app.PostDesktopBotCross("bot_6", "notify", "bot_1", "看一下", "", ""); err == nil || !strings.Contains(err.Error(), desktopBotNotPrimarySentence) || len(posts) != 0 {
		t.Fatalf("non-primary err=%v posts=%#v", err, posts)
	}
	asked, err := app.PostDesktopBotCross("bot_1", "notify", "没有的", "看一下", "", "")
	if err != nil || asked.Posted || !strings.Contains(asked.Ask, "没有这个 Bot") || len(posts) != 0 {
		t.Fatalf("ask = %+v err=%v posts=%d", asked, err, len(posts))
	}
	sent, err := app.PostDesktopBotCross("bot_1", "notify", "all", "先别装。", "", "")
	if err != nil || !sent.Posted || sent.PrimaryLine != "已发消息给 2 个 Bot" || len(sent.Notes) != 2 || len(posts) != 2 {
		t.Fatalf("fan-out = %+v err=%v posts=%#v", sent, err, posts)
	}
	for _, post := range posts {
		if strings.Contains(post, "/messages") {
			t.Fatalf("cross talk admitted a run: %s", post)
		}
	}
}

func TestClaimDesktopBotCrossStartDoesNotAdmitTwice(t *testing.T) {
	claims := 0
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodPost && strings.Contains(path, "/messages") {
			t.Fatalf("claim admitted a run: %s", path)
		}
		if method == http.MethodPost && strings.HasSuffix(path, "/start") {
			claims++
			claimed := claims == 1
			fillDesktopBotDest(t, dest, map[string]any{"id": "note-1", "claimed": claimed, "kind": "delegate", "target": "bot_6", "started_at": "2026-10-14T00:00:00Z"})
			return http.StatusOK, nil
		}
		if method == http.MethodPost && strings.HasSuffix(path, "/run") {
			if body == nil {
				t.Fatal("run id was empty")
			}
			return http.StatusOK, nil
		}
		t.Fatalf("unexpected %s %s", method, path)
		return 0, nil
	}
	t.Cleanup(func() {
		desktopBotExchangeOverride = nil
		resetDesktopBotCrossArm()
	})
	app := &App{}
	first, err := app.ClaimDesktopBotCrossStart("bot_6", "note-1")
	if err != nil || first == nil || !first.Claimed {
		t.Fatalf("first = %+v err=%v", first, err)
	}
	second, err := app.ClaimDesktopBotCrossStart("bot_6", "note-1")
	if err != nil || second == nil || second.Claimed {
		t.Fatalf("second = %+v err=%v", second, err)
	}
	if err := app.ArmDesktopBotCrossRun("bot_6", "note-1"); err != nil {
		t.Fatal(err)
	}
	app.noteArmedDesktopBotCross("bot_6", "run-1")
	if takeDesktopBotCrossArm("bot_6") != "" {
		t.Fatal("the arm was kept after the run id was stored")
	}
}

func TestSetDesktopBotPrimaryReplacesTheListMark(t *testing.T) {
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method != http.MethodPost || path != "/api/v1/bots/bot_6/primary" {
			t.Fatalf("unexpected %s %s", method, path)
		}
		fillDesktopBotDest(t, dest, map[string]any{"items": []map[string]any{
			{"id": "bot_1", "name": "值班", "created_at": "2024-01-15T00:30:00Z"},
			{"id": "bot_6", "name": "Bot 6", "created_at": "2024-02-01T00:00:00Z", "primary": true},
		}})
		return http.StatusOK, nil
	}
	t.Cleanup(func() { desktopBotExchangeOverride = nil })
	items, err := (&App{}).SetDesktopBotPrimary("bot_6")
	if err != nil || len(items) != 2 || items[0].Primary || !items[1].Primary {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func crossMustJSON(t *testing.T, body any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
