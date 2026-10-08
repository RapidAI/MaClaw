package botmgmt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

const (
	ScopeGlobal     = "global"
	ScopeDepartment = "department"
	ScopeUser       = "user"

	// DisabledMessage is what the Maclaw GUI shows when the bot feature is off.
	DisabledMessage = "服务器没有开通bot功能"
)

// Grant turns the bot feature on for everyone, one department, or one user.
// No grants means the feature is off.
type Grant struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	TargetID string `json:"target_id,omitempty"`
}

// Directory resolves a user's department chain. Nil skips department grants.
type Directory interface {
	Email(ctx context.Context, userID string) (string, error)
	GroupID(ctx context.Context, email string) (string, error)
	ParentID(ctx context.Context, groupID string) (string, error)
}

// DesktopControl starts and stops the cloud desktop for one user.
type DesktopControl interface {
	Open(ctx context.Context, tenantID, userID string) (novncURL string, err error)
	Stop(ctx context.Context, tenantID, userID string) error
}

// Reply is the text from one MaClawSrv instance, plus a desktop handoff URL
// when that instance paused for a login or captcha.
type Reply struct {
	Text               string `json:"text"`
	NovncURL           string `json:"novnc_url,omitempty"`
	Handoff            bool   `json:"handoff,omitempty"`
	AttentionReason    string `json:"attention_reason,omitempty"`
	AskUserInputType   string `json:"ask_user_input_type,omitempty"`
	AskUserSecretName  string `json:"ask_user_secret_name,omitempty"`
	AskUserQuestion    string `json:"ask_user_question,omitempty"`
	AskUserOptionsJSON string `json:"ask_user_options_json,omitempty"`
}

func (s *Service) Enabled(ctx context.Context, tenantID, userID string) (bool, error) {
	_, err := s.loadForUser(ctx, tenantID, userID)
	if errors.Is(err, ErrDisabled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// loadForUser reads the settings once and reports whether that user may use
// bots. Callers that also need the record reuse it instead of reading the
// store a second time, and the department chain is resolved only once.
func (s *Service) loadForUser(ctx context.Context, tenantID, userID string) (record, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return record{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return record{}, err
	}
	// A user or global grant does not need the department walk. That walk is
	// a directory round trip on every bot message.
	if grantMatches(rec.Grants, userID, nil) {
		return rec, nil
	}
	if !grantMatches(rec.Grants, userID, s.departmentChain(directoryContext(ctx, tenantID), userID)) {
		return record{}, ErrDisabled
	}
	return rec, nil
}

func (s *Service) CreateGrant(ctx context.Context, tenantID string, in Grant) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return Grant{}, err
	}
	item, err := normalizeGrant(in, rec.Grants)
	if err != nil {
		return Grant{}, err
	}
	item.ID = newBotID()
	rec.Grants = append(rec.Grants, item)
	if err := s.save(ctx, tenantID, rec); err != nil {
		return Grant{}, err
	}
	return item, nil
}

func (s *Service) DeleteGrant(ctx context.Context, tenantID, grantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	grantID = strings.TrimSpace(grantID)
	next := rec.Grants[:0]
	found := false
	for _, item := range rec.Grants {
		if item.ID == grantID {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return ErrNotFound
	}
	if next == nil {
		next = []Grant{}
	}
	rec.Grants = next
	return s.save(ctx, tenantID, rec)
}

func (s *Service) BotsForUser(ctx context.Context, tenantID, userID string) ([]Bot, error) {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Bot, 0)
	for _, bot := range rec.Bots {
		if bot.OwnerUserID == userID {
			out = append(out, bot)
		}
	}
	return out, nil
}

func (s *Service) CreateBotForUser(ctx context.Context, tenantID, userID, name, description string) (Bot, error) {
	if _, err := s.loadForUser(ctx, tenantID, userID); err != nil {
		return Bot{}, err
	}
	return s.createBot(ctx, tenantID, userID, name, description, true)
}

func (s *Service) UpdateBotForUser(ctx context.Context, tenantID, userID, botID, name, description string) (Bot, error) {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return Bot{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return Bot{}, ErrNotFound
	}
	return s.UpdateBot(ctx, tenantID, botID, name, description)
}

func (s *Service) DeleteBotForUser(ctx context.Context, tenantID, userID, botID string) error {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return ErrNotFound
	}
	return s.DeleteBot(ctx, tenantID, botID)
}

// PostMessage sends one command to that user's MaClawSrv instance.
// It opens the user's cloud desktop first. MaClawSrv stops the desktop when
// this user has no run left. A transport failure here also stops it, because
// MaClawSrv never saw the command.
func (s *Service) PostMessage(ctx context.Context, tenantID, userID, botID, content string) (Reply, error) {
	return s.PostMessagePhase(ctx, tenantID, userID, botID, content, "")
}

// PostMessagePhase is PostMessage with the bot turn phase. An empty phase
// leaves the body as content plus the session key, so an older client keeps
// working. plan and execute travel as message metadata bot_phase.
func (s *Service) PostMessagePhase(ctx context.Context, tenantID, userID, botID, content, phase string) (Reply, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Reply{}, fmt.Errorf("%w: message is required", ErrInvalidInput)
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return Reply{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return Reply{}, ErrNotFound
	}
	if err := configured(rec); err != nil {
		return Reply{}, err
	}
	// The person finished logging in and sent the next command. The agent
	// uses the same browser now. The keyboard is returned only after this
	// command is counted, so a stop cannot shut the browser in between.
	instanceID := rec.Bots[index].InstanceID
	novnc, opened, err := s.openDesktop(ctx, tenantID, userID, instanceID, botID)
	if err != nil {
		// The desktop did not change hands. If this bot was already waiting
		// for a login, the person still needs the keyboard on that browser.
		s.restoreDesktopKeyboard(tenantID, userID, botID)
		return Reply{}, err
	}
	var payload struct {
		Message struct {
			Content  string            `json:"content"`
			Metadata map[string]string `json:"metadata"`
		} `json:"message"`
		DesktopHandoff  bool   `json:"desktop_handoff"`
		AttentionReason string `json:"attention_reason"`
		Error           string `json:"error"`
	}
	// The token exchange is a remote call: keep it off s.mu, so one slow
	// MaClawSrv cannot stall every other user's command.
	fresh, token, tokenErr := s.ownerToken(ctx, tenantID, userID)
	if tokenErr != nil {
		s.restoreDesktopKeyboard(tenantID, userID, botID)
		s.finishDesktopOpen(tenantID, userID, instanceID, opened, true)
		return Reply{}, tokenErr
	}
	instancePath := "/api/v1/instances/" + url.PathEscape(rec.Bots[index].InstanceID)
	// Keep the rest of this instance and rewrite only the desktop owner.
	// Replacing the whole metadata map would drop the settings this bot
	// already has, and the next command would not be the same instance.
	// If those settings cannot be read, skip the write. A partial map would
	// replace them and the next command would not be this same bot.
	metadata, metaErr := s.desktopIdentityMetadata(ctx, fresh, token, instancePath, userID, tenantID)
	if metaErr != nil {
		s.restoreDesktopKeyboard(tenantID, userID, botID)
		s.finishDesktopOpen(tenantID, userID, instanceID, opened, true)
		return Reply{}, metaErr
	}
	if err := s.doAuth(ctx, fresh, token, http.MethodPatch, instancePath, map[string]any{
		"metadata": metadata,
	}, nil); err != nil {
		s.restoreDesktopKeyboard(tenantID, userID, botID)
		s.finishDesktopOpen(tenantID, userID, instanceID, opened, true)
		return Reply{}, err
	}
	path := instancePath + "/messages"
	messageBody := map[string]any{
		"content":            content,
		"client_session_key": rec.Bots[index].ID,
	}
	switch strings.TrimSpace(phase) {
	case "plan", "execute":
		messageBody["metadata"] = map[string]string{"bot_phase": strings.TrimSpace(phase)}
	}
	callErr := s.doAuth(ctx, fresh, token, http.MethodPost, path, messageBody, &payload)
	if callErr != nil {
		// The instance may already be waiting for a login. Stopping here
		// would close that browser before the person can use it. A failed
		// continuation did not take the keyboard, so the person keeps it.
		if !desktopCallTimedOut(callErr) {
			s.restoreDesktopKeyboard(tenantID, userID, botID)
			s.finishDesktopOpen(tenantID, userID, instanceID, opened, true)
		} else {
			// The browser is still open. Keep it so the website login stays
			// in this desktop. The keyboard comes back only when this bot
			// had already handed the page to the person. A slow task must
			// not be treated as a login.
			if s.desktopViewURL(tenantID, userID) != "" {
				if s.desktopHeldByPerson(tenantID, userID) {
					s.restoreDesktopKeyboard(tenantID, userID, botID)
				} else {
					s.keepDesktopWithoutKeyboard(tenantID, userID, botID)
				}
			}
			s.finishDesktopOpen(tenantID, userID, instanceID, opened, false)
		}
		return Reply{}, callErr
	}
	text := strings.TrimSpace(payload.Message.Content)
	if text == "" && strings.TrimSpace(payload.Error) != "" {
		text = strings.TrimSpace(payload.Error)
	}
	if text == "" && payload.DesktopHandoff {
		text = "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。"
	}
	if text == "" {
		s.restoreDesktopKeyboard(tenantID, userID, botID)
		s.finishDesktopOpen(tenantID, userID, instanceID, opened, true)
		return Reply{}, fmt.Errorf("%w: instance returned no result", ErrSrv)
	}
	reply := Reply{Text: text}
	if meta := payload.Message.Metadata; meta != nil {
		reply.AskUserInputType = strings.TrimSpace(meta["ask_user_input_type"])
		reply.AskUserSecretName = strings.TrimSpace(meta["ask_user_secret_name"])
		reply.AskUserQuestion = strings.TrimSpace(meta["ask_user_question"])
		reply.AskUserOptionsJSON = strings.TrimSpace(meta["ask_user_options_json"])
	}
	reason := strings.TrimSpace(payload.AttentionReason)
	if payload.DesktopHandoff {
		// The login is already decided. This open may not have returned a
		// picture yet; a later session can still publish one. Clearing the
		// hold here would let the next failure close the browser before the
		// person can sign in, and the website login would be gone.
		s.noteDesktopHeld(tenantID, userID, botID)
		if reason != "" {
			s.noteDesktopAttention(tenantID, userID, botID, reason)
		}
		reply.Handoff = true
		reply.AttentionReason = reason
		reply.NovncURL = s.desktopViewURL(tenantID, userID)
		if reply.NovncURL == "" && novnc != "" {
			reply.NovncURL = s.gateDesktopHandoff(novnc)
		}
	} else {
		s.clearDesktopHeld(tenantID, userID, botID)
		if reason != "" {
			s.noteDesktopAttention(tenantID, userID, botID, reason)
			reply.AttentionReason = reason
		}
	}
	s.finishDesktopOpen(tenantID, userID, instanceID, opened, false)
	return reply, nil
}

// desktopIdentityMetadata is this Hub user on the instance, plus any
// metadata the instance already had. The desktop key is the Hub user.
// Other fields stay so this bot remains the same instance.
func (s *Service) desktopIdentityMetadata(ctx context.Context, rec record, token, instancePath, userID, tenantID string) (map[string]string, error) {
	var existing struct {
		ID       string            `json:"id"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := s.doAuth(ctx, rec, token, http.MethodGet, instancePath, nil, &existing); err != nil {
		return nil, err
	}
	// An empty reply is not an instance with no settings. Writing over it
	// would drop the configuration this bot already has.
	if strings.TrimSpace(existing.ID) == "" {
		return nil, fmt.Errorf("%w: instance settings were not read", ErrSrv)
	}
	metadata := map[string]string{}
	for key, value := range existing.Metadata {
		metadata[key] = value
	}
	metadata["hub_bot"] = "1"
	metadata["hub_user_id"] = userID
	metadata["hub_tenant_id"] = tenantID
	return metadata, nil
}

func (s *Service) beginDesktopUse(tenantID, userID, instanceID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopOpening == nil {
		s.desktopOpening = map[string]int{}
	}
	key := desktopViewKey(tenantID, userID)
	s.desktopOpening[key]++
	s.trackDesktopInstanceLocked(key, instanceID, 1)
}

func (s *Service) endDesktopUse(tenantID, userID, instanceID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dropDesktopUseLocked(desktopViewKey(tenantID, userID), instanceID)
	s.mu.Unlock()
}

func (s *Service) dropDesktopUseLocked(key, instanceID string) int {
	s.trackDesktopInstanceLocked(key, instanceID, -1)
	left := 0
	if s.desktopOpening != nil {
		left = s.desktopOpening[key]
		if left > 0 {
			left--
			if left == 0 {
				delete(s.desktopOpening, key)
			} else {
				s.desktopOpening[key] = left
			}
		}
	}
	return left
}

func (s *Service) trackDesktopInstanceLocked(key, instanceID string, delta int) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || delta == 0 {
		return
	}
	if s.desktopOpenInstance == nil {
		s.desktopOpenInstance = map[string]map[string]int{}
	}
	if s.desktopOpenInstance[key] == nil {
		s.desktopOpenInstance[key] = map[string]int{}
	}
	next := s.desktopOpenInstance[key][instanceID] + delta
	if next <= 0 {
		delete(s.desktopOpenInstance[key], instanceID)
		if len(s.desktopOpenInstance[key]) == 0 {
			delete(s.desktopOpenInstance, key)
		}
		return
	}
	s.desktopOpenInstance[key][instanceID] = next
}

func (s *Service) otherInstanceHasDesktop(tenantID, userID, instanceID string) bool {
	instanceID = strings.TrimSpace(instanceID)
	if s == nil || instanceID == "" {
		return false
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	defer s.mu.Unlock()
	open := s.desktopOpenInstance[desktopViewKey(tenantID, userID)]
	for id, count := range open {
		if count > 0 && id != instanceID {
			return true
		}
	}
	return false
}

func (s *Service) desktopUserGate(tenantID, userID string) *sync.Mutex {
	key := desktopViewKey(tenantID, userID)
	gate, _ := s.desktopGates.LoadOrStore(key, &sync.Mutex{})
	return gate.(*sync.Mutex)
}

func (s *Service) finishDesktopOpen(tenantID, userID, instanceID string, opened, stop bool) {
	if s == nil || !opened {
		return
	}
	s.mu.Lock()
	// The admin hold is persisted, so it has to be restored before it can keep
	// this desktop up. The counters above are in-memory only and need no load.
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	left := s.dropDesktopUseLocked(key, instanceID)
	_, held := s.desktopHeld[key]
	adminView := s.adminDesktopViewActiveLocked(key)
	userView := s.userDesktopViewActiveLocked(key)
	s.mu.Unlock()
	if !stop || left > 0 || held || adminView || userView {
		return
	}
	if s.beforeDesktopStop != nil {
		s.beforeDesktopStop()
	}
	// Open raises the count before it returns and holds this gate until then.
	// Recheck under the gate so this stop cannot land on the desktop the next
	// command of the same user has already started.
	gate := s.desktopUserGate(tenantID, userID)
	gate.Lock()
	s.mu.Lock()
	if s.desktopOpening != nil {
		left = s.desktopOpening[key]
	} else {
		left = 0
	}
	_, held = s.desktopHeld[key]
	adminView = s.adminDesktopViewActiveLocked(key)
	userView = s.userDesktopViewActiveLocked(key)
	s.mu.Unlock()
	if left == 0 && !held && !adminView && !userView {
		_ = s.stopDesktop(context.Background(), tenantID, userID)
	}
	gate.Unlock()
}

func (s *Service) desktopHeldByPerson(tenantID, userID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	defer s.mu.Unlock()
	_, ok := s.desktopHeld[desktopViewKey(tenantID, userID)]
	return ok
}

func (s *Service) desktopKeyboardForBot(tenantID, userID, botID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	defer s.mu.Unlock()
	key := desktopViewKey(tenantID, userID)
	holder, ok := s.desktopHeld[key]
	return ok && holder == strings.TrimSpace(botID) && s.desktopAwaiting[key]
}

// HoldDesktop marks this user's desktop as waiting for a person as soon as
// MaClawSrv decides to hand it over. The message reply can still be on the
// way. Another bot's failure must not stop the browser before that reply
// arrives, or the website login is lost.
func (s *Service) HoldDesktop(ctx context.Context, tenantID, userID, instanceID string) error {
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	if userID == "" || instanceID == "" {
		return fmt.Errorf("%w: instance is required", ErrInvalidInput)
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, bot := range rec.Bots {
		if bot.InstanceID == instanceID && bot.OwnerUserID == userID {
			s.noteDesktopHeld(tenantID, userID, bot.ID)
			return nil
		}
	}
	return ErrNotFound
}

// keepDesktopWithoutKeyboard stops a later failure from closing this
// user's browser. The person does not get the keyboard; that only happens
// for a login handoff.
func (s *Service) keepDesktopWithoutKeyboard(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	if holder, ok := s.desktopHeld[key]; ok && holder != "" && holder != botID {
		return // another bot owns this pin
	}
	if s.desktopHeld[key] == botID && !s.desktopAwaiting[key] {
		return // already pinned without the keyboard — skip the disk write
	}
	if s.desktopHeld == nil {
		s.desktopHeld = map[string]string{}
	}
	if s.desktopAwaiting == nil {
		s.desktopAwaiting = map[string]bool{}
	}
	s.desktopHeld[key] = botID
	s.desktopAwaiting[key] = false
	s.persistDesktopState(tenantID)
}

// AdminDesktopViewHold is how long an admin's desktop check keeps that
// desktop alive. A check ends with a stop decision the admin makes, but a bot
// command that happens to finish in the middle must not pull the desktop out
// from under the open noVNC page. It is a hold, not a lease: the admin never
// has to release it, and an explicit stop always wins.
const AdminDesktopViewHold = 30 * time.Minute

// NoteDesktopAdminView records that an admin opened this desktop just now, so
// the idle checks leave it alone while the check is still plausibly running.
func (s *Service) NoteDesktopAdminView(tenantID, userID string) {
	if s == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	now := s.now()
	if s.desktopAdminView == nil {
		s.desktopAdminView = map[string]time.Time{}
	}
	if last, ok := s.desktopAdminView[key]; ok && now.Sub(last) < time.Minute {
		return // a check already in progress; skip the disk write
	}
	s.desktopAdminView[key] = now
	s.persistDesktopState(tenantID)
}

// adminDesktopViewActiveLocked reports whether an admin check of this desktop
// started inside the hold window. Callers must hold s.mu.
func (s *Service) adminDesktopViewActiveLocked(key string) bool {
	last, ok := s.desktopAdminView[key]
	if !ok {
		return false
	}
	age := s.now().Sub(last)
	if age < 0 || age >= AdminDesktopViewHold {
		delete(s.desktopAdminView, key)
		return false
	}
	return true
}

// ReleaseDesktopIfIdle drops a timeout pin so the desktop can stop.
// It returns false while the person still has the keyboard, while another
// command of this user already has the desktop open, while an admin check
// of that desktop is still inside its hold, or while this user's own Bot
// view is still inside its hold. The caller must leave that browser up so
// the website login stays there.
func (s *Service) ReleaseDesktopIfIdle(tenantID, userID string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	defer s.mu.Unlock()
	key := desktopViewKey(tenantID, userID)
	if s.desktopAwaiting[key] {
		return false
	}
	if s.adminDesktopViewActiveLocked(key) {
		return false
	}
	if s.userDesktopViewActiveLocked(key) {
		return false
	}
	// The command that is stopping still counts as one open. A second open
	// means the next command is already on this browser.
	if s.desktopOpening != nil && s.desktopOpening[key] > 1 {
		return false
	}
	if _, held := s.desktopHeld[key]; held {
		delete(s.desktopHeld, key)
		delete(s.desktopAwaiting, key)
		delete(s.desktopKeyboardTaken, key)
		s.persistDesktopState(tenantID)
	}
	return true
}

// StopDesktopIfIdle stops this user's desktop after the last command.
// The stop holds the same gate as open, so it cannot land on a browser the
// next command of this user has just started. The website login stays in
// that browser.
func (s *Service) StopDesktopIfIdle(ctx context.Context, tenantID, userID, instanceID string) (bool, error) {
	if s == nil {
		return false, nil
	}
	gate := s.desktopUserGate(tenantID, userID)
	gate.Lock()
	defer gate.Unlock()
	// This stop belongs to one instance. Another instance still has the
	// desktop open, so the website login stays in that browser.
	if s.otherInstanceHasDesktop(tenantID, userID, instanceID) {
		return false, nil
	}
	if !s.ReleaseDesktopIfIdle(tenantID, userID) {
		return false, nil
	}
	if err := s.stopDesktop(ctx, tenantID, userID); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) noteDesktopHeld(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	if s.desktopHeld == nil {
		s.desktopHeld = map[string]string{}
	}
	if s.desktopAwaiting == nil {
		s.desktopAwaiting = map[string]bool{}
	}
	key := desktopViewKey(tenantID, userID)
	if s.desktopHeld[key] == botID && s.desktopAwaiting[key] && s.desktopKeyboardTaken[key] == "" {
		return // already pinned exactly like this — skip the disk write
	}
	s.desktopHeld[key] = botID
	s.desktopAwaiting[key] = true
	delete(s.desktopKeyboardTaken, key)
	s.persistDesktopState(tenantID)
}

func (s *Service) releaseDesktopKeyboard(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	holder, ok := s.desktopHeld[key]
	botID = strings.TrimSpace(botID)
	if !ok || holder != botID || !s.desktopAwaiting[key] {
		s.mu.Unlock()
		return
	}
	s.desktopAwaiting[key] = false
	if s.desktopKeyboardTaken == nil {
		s.desktopKeyboardTaken = map[string]string{}
	}
	s.desktopKeyboardTaken[key] = botID
	s.persistDesktopState(tenantID)
	s.mu.Unlock()
}

func (s *Service) restoreDesktopKeyboard(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	botID = strings.TrimSpace(botID)
	taken := s.desktopKeyboardTaken[key]
	delete(s.desktopKeyboardTaken, key)
	if taken != botID {
		// The delete above may have cleared a stale pin; keep disk in step.
		if taken != "" {
			s.persistDesktopState(tenantID)
		}
		s.mu.Unlock()
		return
	}
	holder, ok := s.desktopHeld[key]
	if !ok || holder != botID {
		if taken != "" {
			s.persistDesktopState(tenantID)
		}
		s.mu.Unlock()
		return
	}
	if s.desktopAwaiting == nil {
		s.desktopAwaiting = map[string]bool{}
	}
	s.desktopAwaiting[key] = true
	s.persistDesktopState(tenantID)
	s.mu.Unlock()
}

func (s *Service) clearDesktopHeld(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	holder, hadHolder := s.desktopHeld[key]
	_, hadAwaiting := s.desktopAwaiting[key]
	_, hadKeyboard := s.desktopKeyboardTaken[key]
	if !hadHolder || holder == strings.TrimSpace(botID) {
		delete(s.desktopHeld, key)
		delete(s.desktopAwaiting, key)
		delete(s.desktopKeyboardTaken, key)
		if hadHolder || hadAwaiting || hadKeyboard {
			s.persistDesktopState(tenantID)
		}
	}
	s.forgetDesktopAttentionLocked(tenantID, userID, botID)
	s.mu.Unlock()
}

func (s *Service) createBot(ctx context.Context, tenantID, ownerUserID, name, description string, requireOwner bool) (Bot, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	ownerUserID = strings.TrimSpace(ownerUserID)
	if name == "" || len([]rune(name)) > 80 {
		return Bot{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len([]rune(description)) > 200 {
		return Bot{}, fmt.Errorf("%w: description is too long", ErrInvalidInput)
	}
	if requireOwner && !desktop.ValidUserID(ownerUserID) {
		return Bot{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return Bot{}, err
	}
	if err := configured(rec); err != nil {
		return Bot{}, err
	}
	token, err := s.ensureOwnerToken(ctx, tenantID, &rec, ownerUserID)
	if err != nil {
		return Bot{}, err
	}
	meta := map[string]string{"hub_bot": "1"}
	if ownerUserID != "" {
		meta["hub_user_id"] = ownerUserID
		meta["hub_tenant_id"] = tenantID
	}
	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"name":                 name,
		"description":          description,
		"allow_invalid_config": true,
		"metadata":             meta,
	}
	if err := s.doAuth(ctx, rec, token, http.MethodPost, "/api/v1/instances", body, &created); err != nil {
		return Bot{}, err
	}
	if strings.TrimSpace(created.ID) == "" {
		return Bot{}, fmt.Errorf("%w: instance id missing", ErrSrv)
	}
	bot := Bot{
		ID:          newBotID(),
		Name:        name,
		Description: description,
		InstanceID:  strings.TrimSpace(created.ID),
		OwnerUserID: ownerUserID,
		CreatedAt:   s.now().UTC().Format(time.RFC3339),
	}
	rec.Bots = append(rec.Bots, bot)
	if err := s.save(ctx, tenantID, rec); err != nil {
		return Bot{}, err
	}
	return bot, nil
}

func (s *Service) openDesktop(ctx context.Context, tenantID, userID, instanceID, botID string) (string, bool, error) {
	if s == nil || s.Desktop == nil {
		return "", false, nil
	}
	// Count this open before Open returns, and hold the user gate across the
	// call. A command that already decided to stop waits here, then sees the
	// count and leaves the logged-in browser up. The keyboard goes back to
	// the agent only after that count, or the stop sees nobody using the
	// desktop and closes the browser the person just signed into.
	gate := s.desktopUserGate(tenantID, userID)
	gate.Lock()
	s.beginDesktopUse(tenantID, userID, instanceID)
	s.releaseDesktopKeyboard(tenantID, userID, botID)
	novnc, err := s.Desktop.Open(ctx, tenantID, userID)
	if err != nil {
		s.endDesktopUse(tenantID, userID, instanceID)
		s.restoreDesktopKeyboard(tenantID, userID, botID)
		gate.Unlock()
		return "", false, fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	novnc = strings.TrimSpace(novnc)
	if novnc != "" {
		s.rememberDesktopView(tenantID, userID, novnc)
	}
	gate.Unlock()
	return novnc, true, nil
}

func (s *Service) stopDesktop(ctx context.Context, tenantID, userID string) error {
	if s == nil {
		return nil
	}
	s.ForgetDesktopView(tenantID, userID)
	if s.Desktop == nil {
		return nil
	}
	return s.Desktop.Stop(ctx, tenantID, userID)
}

// NoteDesktopView points the chat at the desktop MaClawSrv just opened.
// A later session can publish a different port; the picture has to follow it.
func (s *Service) NoteDesktopView(tenantID, userID, raw string) {
	s.rememberDesktopView(tenantID, userID, raw)
}

// ForgetDesktopView drops the chat picture after MaClawSrv stops the desktop.
// The stop call does not go through the bot message path, so the picture has
// to be cleared here or the next task shows a desktop that is already gone.
// An admin check hold ends with the desktop it was watching; a user view
// hold ends too, so the next view opens a fresh desktop.
func (s *Service) ForgetDesktopView(tenantID, userID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	_, hadView := s.desktopView[key]
	_, hadAdminView := s.desktopAdminView[key]
	_, hadUserView := s.desktopUserView[key]
	if hadView {
		delete(s.desktopView, key)
	}
	if hadAdminView {
		delete(s.desktopAdminView, key)
	}
	if hadUserView {
		delete(s.desktopUserView, key)
	}
	if hadView || hadAdminView || hadUserView {
		s.persistDesktopState(tenantID)
	}
	s.mu.Unlock()
}

// DesktopWatch is the Hub noVNC path for this user's desktop while a bot is
// working. It is empty when that desktop is not running. The keyboard is
// returned only to the bot that handed the desktop over for login.
func (s *Service) DesktopWatch(ctx context.Context, tenantID, userID, botID string) (string, bool, error) {
	if ok, err := s.Enabled(ctx, tenantID, userID); err != nil || !ok {
		if err != nil {
			return "", false, err
		}
		return "", false, ErrDisabled
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return "", false, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return "", false, ErrNotFound
	}
	novnc := s.desktopViewURL(tenantID, userID)
	return novnc, novnc != "" && s.desktopKeyboardForBot(tenantID, userID, botID), nil
}

func desktopViewKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
}

func (s *Service) rememberDesktopView(tenantID, userID, raw string) {
	if s == nil {
		return
	}
	raw = strings.TrimSpace(raw)
	key := desktopViewKey(tenantID, userID)
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	current := s.desktopView[key]
	s.mu.Unlock()
	if current.raw == raw && current.gated != "" {
		return
	}
	gated := s.gateDesktopHandoff(raw)
	if gated == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopView == nil {
		s.desktopView = map[string]desktopWatch{}
	}
	if existing := s.desktopView[key]; existing.raw == raw && existing.gated != "" {
		return
	}
	s.desktopView[key] = desktopWatch{raw: raw, gated: gated}
	s.persistDesktopState(tenantID)
}

// DesktopViewURL is the Hub noVNC path recorded for this user's desktop,
// or "" when no view was published yet. Admin desktop checks use it to
// open the same gated picture the chat hands out.
func (s *Service) DesktopViewURL(tenantID, userID string) string {
	return s.desktopViewURL(tenantID, userID)
}

func (s *Service) desktopViewURL(tenantID, userID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	defer s.mu.Unlock()
	return s.desktopView[desktopViewKey(tenantID, userID)].gated
}

func (s *Service) departmentChain(ctx context.Context, userID string) []string {
	return s.departmentChainCached(ctx, userID, "", nil)
}

// directoryContext carries the settings tenant into department lookups.
// Group and parent rows are filtered by the security tenant on the context.
// A request that never set one would read the default tenant and miss every
// department grant stored for the real tenant.
func directoryContext(ctx context.Context, tenantID string) context.Context {
	return security.WithTenant(ctx, tenantID)
}

// parentMemo caches one request's parent lookups. The chain itself is not
// cached: a walk that stops on a cycle or the depth cap is not some other
// group's ancestor list. A failed lookup is not stored, so the next user
// retries it. A nil memo keeps the single-user path allocation-free.
type parentMemo struct {
	parent map[string]string
}

func (s *Service) departmentChainCached(ctx context.Context, userID, knownEmail string, memo *parentMemo) []string {
	if s == nil || s.Directory == nil {
		return nil
	}
	email := strings.TrimSpace(knownEmail)
	if email == "" {
		resolved, err := s.Directory.Email(ctx, userID)
		if err != nil || strings.TrimSpace(resolved) == "" {
			return nil
		}
		email = strings.TrimSpace(resolved)
	}
	groupID, err := s.Directory.GroupID(ctx, email)
	if err != nil {
		return nil
	}
	groupID = strings.TrimSpace(groupID)
	var chain []string
	seen := map[string]bool{}
	for groupID != "" && len(chain) < 32 && !seen[groupID] {
		seen[groupID] = true
		chain = append(chain, groupID)
		parent, err := memoParent(ctx, s, groupID, memo)
		if err != nil {
			break
		}
		groupID = parent
	}
	return chain
}

func memoParent(ctx context.Context, s *Service, groupID string, memo *parentMemo) (string, error) {
	if memo != nil {
		if parent, ok := memo.parent[groupID]; ok {
			return parent, nil
		}
	}
	parent, err := s.Directory.ParentID(ctx, groupID)
	if err != nil {
		return "", err
	}
	parent = strings.TrimSpace(parent)
	if memo != nil {
		if memo.parent == nil {
			memo.parent = map[string]string{}
		}
		memo.parent[groupID] = parent
	}
	return parent, nil
}

// GrantSubject is one user tested against 开通范围. Email, when already
// known, skips the directory read that would load it again.
type GrantSubject struct {
	ID    string
	Email string
}

// FilterGranted keeps the subjects 开通范围 covers, in the same order.
// A global grant keeps every id. With no grants the feature is off, so the
// result is empty. A nil service or a failed settings read also returns nil:
// the check-desktop list must not offer a user the Bot entry would hide.
// Blank ids are dropped, and a repeated id is kept once.
func (s *Service) FilterGranted(ctx context.Context, tenantID string, subjects []GrantSubject) []string {
	if s == nil || len(subjects) == 0 {
		return nil
	}
	ctx = directoryContext(ctx, tenantID)
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return nil
	}
	global, users, depts := indexGrants(rec.Grants)
	if !global && len(users) == 0 && len(depts) == 0 {
		return nil
	}
	out := make([]string, 0, len(subjects))
	seen := map[string]bool{}
	var memo *parentMemo
	if !global && len(depts) > 0 {
		memo = &parentMemo{}
	}
	for _, subject := range subjects {
		if err := ctx.Err(); err != nil {
			return nil
		}
		id := strings.TrimSpace(subject.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if global || users[id] {
			out = append(out, id)
			continue
		}
		if len(depts) == 0 {
			continue
		}
		for _, groupID := range s.departmentChainCached(ctx, id, subject.Email, memo) {
			if depts[groupID] {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// FilterGrantedIDs is FilterGranted for callers that only have user ids.
func (s *Service) FilterGrantedIDs(ctx context.Context, tenantID string, userIDs []string) []string {
	if len(userIDs) == 0 {
		return nil
	}
	subjects := make([]GrantSubject, len(userIDs))
	for i, id := range userIDs {
		subjects[i] = GrantSubject{ID: id}
	}
	return s.FilterGranted(ctx, tenantID, subjects)
}

// indexGrants is grantMatches keyed for a whole user list. Targets are
// trimmed so a stored id matches the trimmed id the list compares with.
func indexGrants(grants []Grant) (global bool, users, depts map[string]bool) {
	for _, item := range grants {
		switch item.Scope {
		case ScopeGlobal:
			global = true
		case ScopeUser:
			if id := strings.TrimSpace(item.TargetID); id != "" {
				if users == nil {
					users = map[string]bool{}
				}
				users[id] = true
			}
		case ScopeDepartment:
			if id := strings.TrimSpace(item.TargetID); id != "" {
				if depts == nil {
					depts = map[string]bool{}
				}
				depts[id] = true
			}
		}
	}
	return global, users, depts
}

// grantMatches is the single-user form of indexGrants. It does not build
// maps: bot access checks run on every message, and a tenant has few grants.
func grantMatches(grants []Grant, userID string, departments []string) bool {
	userID = strings.TrimSpace(userID)
	if userID != "" {
		for _, item := range grants {
			if item.Scope == ScopeUser && strings.TrimSpace(item.TargetID) == userID {
				return true
			}
		}
	}
	for _, groupID := range departments {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		for _, item := range grants {
			if item.Scope == ScopeDepartment && strings.TrimSpace(item.TargetID) == groupID {
				return true
			}
		}
	}
	for _, item := range grants {
		if item.Scope == ScopeGlobal {
			return true
		}
	}
	return false
}

func normalizeGrant(in Grant, existing []Grant) (Grant, error) {
	in.Scope = strings.TrimSpace(in.Scope)
	in.TargetID = strings.TrimSpace(in.TargetID)
	switch in.Scope {
	case ScopeGlobal:
		in.TargetID = ""
		for _, item := range existing {
			if item.Scope == ScopeGlobal {
				return Grant{}, fmt.Errorf("%w: global grant already exists", ErrInvalidInput)
			}
		}
	case ScopeDepartment, ScopeUser:
		if !desktop.ValidUserID(in.TargetID) {
			return Grant{}, fmt.Errorf("%w: target is required", ErrInvalidInput)
		}
		for _, item := range existing {
			if item.Scope == in.Scope && item.TargetID == in.TargetID {
				return Grant{}, fmt.Errorf("%w: grant already exists", ErrInvalidInput)
			}
		}
	default:
		return Grant{}, fmt.Errorf("%w: scope must be global, department, or user", ErrInvalidInput)
	}
	return in, nil
}
