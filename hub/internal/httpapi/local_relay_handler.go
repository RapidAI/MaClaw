package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/gorilla/websocket"
)

// localRelay is this Hub's broker between bots (through MaClawSrv and the
// cloud desktop) and the person's own computer (through their MaClaw GUI).
var localRelay = &localrelay.Broker{}

var localRelayUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
}

func localRelayKey(tenantID, userID string) string {
	owner, _ := desktop.SplitDesktopUser(strings.TrimSpace(userID))
	return localrelay.UserKey(store.NormalizeTenantID(tenantID), owner)
}

// GetLocalRelayLinkHandler is the GUI's outbound link. The person's machine
// credential authenticates it; the link carries only this person's streams.
func GetLocalRelayLinkHandler(identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		ws, err := localRelayUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		key := localRelayKey(principal.TenantID, principal.UserID)
		machine := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
		log.Printf("[local-relay] link up user=%s machine=%s", strings.TrimSpace(principal.UserID), machine)
		_ = localRelay.Attach(context.Background(), key, machine, localrelay.NewWSConn(ws))
		log.Printf("[local-relay] link down user=%s machine=%s", strings.TrimSpace(principal.UserID), machine)
	}
}

// LocalRelayConnectHandler is where the cloud desktop's helper connects with
// a grant token. The token is the only credential: it names one person, one
// bot, and one file or target, and it is minted only through the desktop API.
func LocalRelayConnectHandler() http.HandlerFunc {
	return localRelay.ServeConnect
}

type localRelayRequest struct {
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	InstanceID string `json:"instance_id"`
	RunID      string `json:"run_id"`
	Op         string `json:"op"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Remote     string `json:"remote"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	ID         string `json:"id"`
	// Ref is a file in the bot file store (op fetch_to_local).
	Ref string `json:"ref"`
}

func writeRelayFailure(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": localrelay.CodeOf(err), "error": err.Error()})
}

// PostLocalRelayHandler is MaClawSrv's side: status, listing, and minting
// tokens after the GUI approved the file or target. Each answer is the
// GUI's own result, or the exact reason it could not be reached.
func PostLocalRelayHandler(bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in localRelayRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid relay request")
			return
		}
		owner, isolatedBot := desktop.SplitDesktopUser(strings.TrimSpace(in.UserID))
		if strings.TrimSpace(in.TenantID) == "" || owner == "" {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "tenant_id and user_id are required")
			return
		}
		key := localRelayKey(in.TenantID, owner)
		botID, botName := strings.TrimSpace(in.InstanceID), ""
		if bots != nil {
			bot, err := bots.BotByInstance(r.Context(), in.TenantID, owner, in.InstanceID)
			if err != nil && isolatedBot != "" {
				bot, err = bots.BotByInstance(r.Context(), in.TenantID, owner, isolatedBot)
			}
			if err == nil {
				botID, botName = bot.ID, bot.Name
			}
		}
		if botID == "" {
			botID = firstNonBlank(isolatedBot, "bot")
		}
		base := localrelay.Request{BotID: botID, BotName: botName, RunID: strings.TrimSpace(in.RunID), Remote: strings.TrimSpace(in.Remote)}
		ctx, cancel := context.WithTimeout(r.Context(), 11*time.Minute)
		defer cancel()
		ping := func() (*localrelay.Summary, error) {
			req := base
			req.Op = localrelay.OpPing
			pctx, pcancel := context.WithTimeout(ctx, 20*time.Second)
			defer pcancel()
			resp, err := localRelay.Call(pctx, key, req)
			if err != nil {
				return nil, err
			}
			if resp.Summary == nil {
				return &localrelay.Summary{}, nil
			}
			return resp.Summary, nil
		}
		switch strings.TrimSpace(in.Op) {
		case "status":
			online, machine, since := localRelay.Online(key)
			out := map[string]any{"ok": true, "online": online, "bot_id": botID, "bot_name": botName}
			if online {
				out["machine"] = machine
				out["since"] = since.Format(time.RFC3339)
				sum, err := ping()
				if err != nil {
					writeRelayFailure(w, err)
					return
				}
				out["summary"] = sum
			}
			out["grants"] = localRelay.List(key, botID)
			writeJSON(w, http.StatusOK, out)
		case "list":
			req := base
			req.Op, req.Path = localrelay.OpList, in.Path
			resp, err := localRelay.Call(ctx, key, req)
			if err != nil {
				writeRelayFailure(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": resp.Path, "entries": resp.Entries, "truncated": resp.Truncated, "summary": resp.Summary})
		case "grant_to_cloud":
			req := base
			req.Op, req.Path = localrelay.OpCheckRead, in.Path
			resp, err := localRelay.Call(ctx, key, req)
			if err != nil {
				writeRelayFailure(w, err)
				return
			}
			g := localRelay.Mint(localrelay.Grant{Key: key, BotID: botID, BotName: botName, RunID: base.RunID, Kind: localrelay.KindToCloud, LocalPath: resp.Path, Name: resp.Name, Size: resp.Size, Remote: base.Remote})
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": g.ID, "token": g.Token, "local_path": resp.Path, "name": resp.Name, "size": resp.Size})
		case "grant_to_local":
			sum, err := ping()
			if err != nil {
				writeRelayFailure(w, err)
				return
			}
			req := base
			req.Op, req.Path, req.Name = localrelay.OpCheckWrite, in.Path, in.Name
			resp, err := localRelay.Call(ctx, key, req)
			if err != nil {
				writeRelayFailure(w, err)
				return
			}
			max := int64(sum.MaxTransferMB) << 20
			g := localRelay.Mint(localrelay.Grant{Key: key, BotID: botID, BotName: botName, RunID: base.RunID, Kind: localrelay.KindToLocal, LocalPath: resp.Path, Name: resp.Name, MaxSize: max, Remote: base.Remote})
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": g.ID, "token": g.Token, "folder": resp.Path, "name": resp.Name, "max_bytes": max})
		case "fetch_to_local":
			// The cloud file is already in Hub's bot file store; the person's
			// computer pulls it over HTTP (Range, resume) into the folder.
			if botFiles == nil {
				writeRelayFailure(w, &localrelay.Err{Code: localrelay.CodeFailed, Message: "this server has no file store"})
				return
			}
			tenant, fileOwner := botFileOwner(in.TenantID, owner)
			m, err := botFiles.Get(tenant, fileOwner, strings.TrimSpace(in.Ref))
			if err != nil {
				writeRelayFailure(w, &localrelay.Err{Code: localrelay.CodeNotFound, Message: "the uploaded file is not on the server: " + err.Error()})
				return
			}
			link, _ := botFiles.SignedPath(m)
			req := base
			req.Path, req.Name, req.Size, req.URL, req.Ref, req.SHA256 = in.Path, firstNonBlank(in.Name, m.Name), m.Size, link, m.ID, m.SHA256
			resp, err := localRelay.Fetch(ctx, key, req, 9*time.Minute)
			if err != nil {
				writeRelayFailure(w, err)
				return
			}
			out := map[string]any{"ok": true, "state": firstNonBlank(resp.State, "done"), "path": resp.Path, "name": resp.Name, "size": m.Size}
			writeJSON(w, http.StatusOK, out)
		case "grant_forward", "grant_socks":
			sum, err := ping()
			if err != nil {
				writeRelayFailure(w, err)
				return
			}
			if !sum.NetworkEnabled {
				writeRelayFailure(w, &localrelay.Err{Code: localrelay.CodeDisabled, Message: "the person turned off network access for bots on their computer (Bot 管理 → 设置 → 本地访问)"})
				return
			}
			g := localrelay.Grant{Key: key, BotID: botID, BotName: botName, RunID: base.RunID, Kind: localrelay.KindSocks, Idle: time.Duration(sum.TunnelIdleMinutes) * time.Minute}
			addr := ""
			if in.Op == "grant_forward" {
				req := base
				req.Op, req.Host, req.Port = localrelay.OpCheckDial, strings.TrimSpace(in.Host), in.Port
				resp, err := localRelay.Call(ctx, key, req)
				if err != nil {
					writeRelayFailure(w, err)
					return
				}
				g.Kind, g.Host, g.Port, addr = localrelay.KindForward, req.Host, req.Port, resp.Addr
			}
			g = localRelay.Mint(g)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": g.ID, "token": g.Token, "kind": g.Kind, "addr": addr, "idle_minutes": g.IdleMin, "expires": g.Expires.Format(time.RFC3339)})
		case "close":
			found := localRelay.Revoke(key, strings.TrimSpace(in.ID))
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "closed": found})
		case "grants":
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grants": localRelay.List(key, botID)})
		default:
			writeRelayFailure(w, errors.New("unknown relay op "+in.Op))
		}
	}
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
