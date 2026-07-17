package lansenger

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Bot command scopeType values for /v1/bot/commands/* APIs.
// OpenClaw Lansenger channel (production) uses 6 for private and 5 for groups.
// Docs also mention 4 for private in older notes; 6 is the corrected private scope.
const (
	// CommandScopeGroup registers slash commands for all group chats.
	CommandScopeGroup = 5
	// CommandScopePrivate registers slash commands for all private chats.
	CommandScopePrivate = 6
)

const (
	commandSyncRetryMax   = 3
	commandSyncRetryDelay = 30 * time.Second
)

// botCommandNameRe is the Lansenger client constraint: alphanumeric + underscore only.
var botCommandNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// BotCommandI18n is multi-language description for a slash command.
// Keys match the OpenClaw / 蓝信 channel field i18nDescription.
type BotCommandI18n struct {
	ZhHans   string `json:"zhHans,omitempty"`
	ZhHant   string `json:"zhHant,omitempty"`
	ZhHantHK string `json:"zhHantHK,omitempty"`
	En       string `json:"en,omitempty"`
	Fr       string `json:"fr,omitempty"`
}

// BotCommand is one entry for /v1/bot/commands/create.
// Command must NOT include a leading slash (the client adds it).
type BotCommand struct {
	Command         string          `json:"command"`
	Description     string          `json:"description"`
	I18nDescription *BotCommandI18n `json:"i18nDescription,omitempty"`
}

// SupportedBotCommands returns the slash commands this product exposes on Lansenger.
// Currently: /summary (group discussion summary; optional "start" arg sets the cursor).
func SupportedBotCommands() []BotCommand {
	return []BotCommand{
		{
			Command:     "summary",
			Description: "群讨论摘要（可加 start 设定起点）",
			I18nDescription: &BotCommandI18n{
				ZhHans:   "群讨论摘要（可加 start 设定起点）",
				ZhHant:   "群討論摘要（可加 start 設定起點）",
				ZhHantHK: "群討論摘要（可加 start 設定起點）",
				En:       "Summarize group discussion (add start to set cursor)",
			},
		},
	}
}

// NormalizeBotCommands validates and normalizes command names (strip leading /,
// drop invalid names). Empty result means nothing to register.
func NormalizeBotCommands(commands []BotCommand) []BotCommand {
	if len(commands) == 0 {
		return nil
	}
	out := make([]BotCommand, 0, len(commands))
	seen := make(map[string]struct{}, len(commands))
	for _, c := range commands {
		name := strings.TrimSpace(c.Command)
		name = strings.TrimPrefix(name, "/")
		name = strings.TrimSpace(name)
		if name == "" || !botCommandNameRe.MatchString(name) {
			log.Printf("[lansenger] skip invalid bot command name %q", c.Command)
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		c.Command = name
		c.Description = strings.TrimSpace(c.Description)
		if c.Description == "" {
			c.Description = name
		}
		out = append(out, c)
	}
	return out
}

// CreateCommands registers commands for one scope (delete is separate).
// API: POST /v1/bot/commands/create?app_token=…
func (g *Gateway) CreateCommands(ctx context.Context, scopeType int, commands []BotCommand) error {
	commands = NormalizeBotCommands(commands)
	if len(commands) == 0 {
		return fmt.Errorf("lansenger: no valid commands to create")
	}
	return g.postBotCommandAPI(ctx, "/v1/bot/commands/create", map[string]any{
		"scopeType": scopeType,
		"commands":  commands,
	})
}

// DeleteCommands clears all bot commands for one scope.
// API: POST /v1/bot/commands/delete?app_token=…
func (g *Gateway) DeleteCommands(ctx context.Context, scopeType int) error {
	return g.postBotCommandAPI(ctx, "/v1/bot/commands/delete", map[string]any{
		"scopeType": scopeType,
	})
}

func (g *Gateway) postBotCommandAPI(ctx context.Context, path string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	do := func() error {
		token, err := g.getAppToken(ctx)
		if err != nil {
			return err
		}
		endpoint := fmt.Sprintf("%s?app_token=%s", g.apiURL(path), url.QueryEscape(token))
		return g.doPost(ctx, endpoint, body)
	}
	err = do()
	if err != nil && isLansengerTokenExpiredError(err) {
		g.tokens.clear()
		return do()
	}
	return err
}

// SyncBotCommands replaces registered slash commands on the given scopes.
// Default scopes are private (6) + group (5). Flow: delete scope → create scope.
//
// Delete failures are logged but do not fail the sync when create succeeds
// (empty-scope delete may error on some deployments; create is authoritative).
func (g *Gateway) SyncBotCommands(ctx context.Context, commands []BotCommand, scopes ...int) error {
	commands = NormalizeBotCommands(commands)
	if len(commands) == 0 {
		return fmt.Errorf("lansenger: no valid commands to sync")
	}
	if len(scopes) == 0 {
		scopes = []int{CommandScopePrivate, CommandScopeGroup}
	}
	var createErr error
	created := 0
	for _, scope := range scopes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := g.DeleteCommands(ctx, scope); err != nil {
			log.Printf("[lansenger] deleteCommands scope=%d: %v (continuing with create)", scope, err)
		}
		if err := g.CreateCommands(ctx, scope, commands); err != nil {
			log.Printf("[lansenger] createCommands scope=%d: %v", scope, err)
			if createErr == nil {
				createErr = err
			}
			continue
		}
		created++
	}
	if createErr != nil {
		return createErr
	}
	if created == 0 {
		return fmt.Errorf("lansenger: bot command sync created nothing")
	}
	log.Printf("[lansenger] synced %d bot command(s) to %d scope(s)", len(commands), created)
	return nil
}

// SyncSupportedBotCommands registers SupportedBotCommands() with the server.
// Only the group scope is used: /summary is a group-discussion feature, and
// registering it in private chat would advertise a command the handler ignores.
func (g *Gateway) SyncSupportedBotCommands(ctx context.Context) error {
	return g.SyncBotCommands(ctx, SupportedBotCommands(), CommandScopeGroup)
}

// syncSupportedCommandsBackground registers slash commands after Start.
// Retries a few times with delay; stops when ctx is cancelled (gateway Stop).
// Uses SyncSupportedBotCommands (group scope only) — not the multi-scope default.
func (g *Gateway) syncSupportedCommandsBackground(ctx context.Context) {
	if len(NormalizeBotCommands(SupportedBotCommands())) == 0 {
		return
	}
	for attempt := 1; attempt <= commandSyncRetryMax; attempt++ {
		if ctx.Err() != nil {
			return
		}
		// Must call SyncSupportedBotCommands (not SyncBotCommands with defaults),
		// so we only register on the product scope (group).
		err := g.SyncSupportedBotCommands(ctx)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if attempt >= commandSyncRetryMax {
			log.Printf("[lansenger] bot command sync failed after %d attempts: %v", commandSyncRetryMax, err)
			return
		}
		log.Printf("[lansenger] bot command sync attempt %d/%d failed: %v; retry in %v",
			attempt, commandSyncRetryMax, err, commandSyncRetryDelay)
		timer := time.NewTimer(commandSyncRetryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}
