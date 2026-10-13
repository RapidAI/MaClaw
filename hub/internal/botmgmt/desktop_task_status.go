package botmgmt

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// A finished desktop run stays available until the GUI has shown it. Six hours
// is enough for a result the person already received. A result nobody has
// seen yet waits out a weekend offline.
const (
	desktopTaskDeliveredKeep   = 6 * time.Hour
	desktopTaskUndeliveredKeep = 7 * 24 * time.Hour
	desktopTaskMediaRetry      = 2 * time.Minute
	desktopTaskTextKeep        = 4000
	desktopTaskLabelKeep       = 80
	desktopTaskStatusKeep      = 800
	desktopTaskListCap         = 8
)

// desktopTaskPin is the durable slice of one admitted run. The screenshot
// bytes stay in memory with the live reply; this record is the text, the
// status, and whether the GUI has already been given that result.
type desktopTaskPin struct {
	UserID     string `json:"user_id"`
	BotID      string `json:"bot_id"`
	RunID      string `json:"run_id"`
	InstanceID string `json:"instance_id,omitempty"`
	Task       string `json:"task"`
	Phase      string `json:"phase,omitempty"`
	Status     string `json:"status"`
	Text       string `json:"text,omitempty"`
	Error      string `json:"error,omitempty"`
	Handoff    bool   `json:"handoff,omitempty"`
	Attention  string `json:"attention,omitempty"`
	Delivered  bool   `json:"delivered,omitempty"`
	Started    string `json:"started,omitempty"`
	Finished   string `json:"finished,omitempty"`
}

// DesktopTaskStatus is one run as a status question would see it.
type DesktopTaskStatus struct {
	RunID   string    `json:"run_id"`
	BotID   string    `json:"bot_id"`
	Task    string    `json:"task"`
	Status  string    `json:"status"`
	Text    string    `json:"text,omitempty"`
	Error   string    `json:"error,omitempty"`
	Started time.Time `json:"-"`
}

// DesktopTaskDelivery is one finished run the GUI has not shown yet.
type DesktopTaskDelivery struct {
	BotID  string `json:"bot_id"`
	RunID  string `json:"run_id"`
	Task   string `json:"task"`
	Status string `json:"status"`
	Reply  Reply  `json:"reply"`
	Error  string `json:"error,omitempty"`
}

func newRunningDesktopTask(job admittedDesktopFollow) *desktopAdmitState {
	started := job.Started
	if started.IsZero() {
		started = time.Now()
	}
	return &desktopAdmitState{
		task:       clipDesktopRunes(job.Task, 500),
		status:     "running",
		userID:     strings.TrimSpace(job.UserID),
		botID:      strings.TrimSpace(job.BotID),
		runID:      strings.TrimSpace(job.RunID),
		instanceID: strings.TrimSpace(job.InstanceID),
		phase:      strings.TrimSpace(job.Phase),
		started:    started,
	}
}

func desktopTaskExpired(done, delivered bool, finished, now time.Time) bool {
	if !done || finished.IsZero() {
		return false
	}
	age := now.Sub(finished)
	if delivered {
		return age > desktopTaskDeliveredKeep
	}
	return age > desktopTaskUndeliveredKeep
}

func clipDesktopRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max])
}

func (st *desktopAdmitState) pinLocked() *desktopTaskPin {
	if st == nil {
		return nil
	}
	status := strings.TrimSpace(st.status)
	if status == "" {
		if st.done {
			status = "succeeded"
		} else {
			status = "running"
		}
	}
	errText := ""
	if st.err != nil {
		errText = clipDesktopRunes(st.err.Error(), 500)
	}
	pin := &desktopTaskPin{
		UserID:     st.userID,
		BotID:      st.botID,
		RunID:      st.runID,
		InstanceID: st.instanceID,
		Task:       st.task,
		Phase:      st.phase,
		Status:     status,
		Text:       clipDesktopRunes(st.reply.Text, desktopTaskTextKeep),
		Error:      errText,
		Handoff:    st.reply.Handoff,
		Attention:  strings.TrimSpace(st.reply.AttentionReason),
		Delivered:  st.delivered,
	}
	if !st.started.IsZero() {
		pin.Started = st.started.UTC().Format(time.RFC3339)
	}
	if !st.finished.IsZero() {
		pin.Finished = st.finished.UTC().Format(time.RFC3339)
	}
	return pin
}

// desktopTaskPins snapshots admitted runs for this tenant. Callers hold s.mu.
func (s *Service) desktopTaskPins(tenantID string) map[string]*desktopTaskPin {
	if s == nil || len(s.desktopAdmits) == 0 {
		return nil
	}
	prefix := desktopTenantPrefix(tenantID)
	now := s.now()
	out := map[string]*desktopTaskPin{}
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		if desktopTaskExpired(state.done, state.delivered, state.finished, now) {
			state.mu.Unlock()
			continue
		}
		pin := state.pinLocked()
		state.mu.Unlock()
		if pin == nil || pin.RunID == "" {
			continue
		}
		out[key] = pin
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// restoreDesktopTasks puts a saved run list back into the admit ledger and
// resumes a watcher for anything still marked running. Callers hold s.mu.
func (s *Service) restoreDesktopTasks(tenantID string, tasks map[string]*desktopTaskPin) {
	if s == nil || len(tasks) == 0 {
		return
	}
	if s.desktopAdmits == nil {
		s.desktopAdmits = map[string]*desktopAdmitState{}
	}
	prefix := desktopTenantPrefix(tenantID)
	for key, pin := range tasks {
		if pin == nil || !strings.HasPrefix(key, prefix) || strings.TrimSpace(pin.RunID) == "" {
			continue
		}
		if _, exists := s.desktopAdmits[key]; exists {
			continue
		}
		state := &desktopAdmitState{
			task:       pin.Task,
			status:     strings.TrimSpace(pin.Status),
			userID:     strings.TrimSpace(pin.UserID),
			botID:      strings.TrimSpace(pin.BotID),
			runID:      strings.TrimSpace(pin.RunID),
			instanceID: strings.TrimSpace(pin.InstanceID),
			phase:      pin.Phase,
			delivered:  pin.Delivered,
			reply: Reply{
				Text:            pin.Text,
				Handoff:         pin.Handoff,
				AttentionReason: pin.Attention,
			},
		}
		if state.status == "" {
			state.status = "running"
		}
		if stamp := strings.TrimSpace(pin.Started); stamp != "" {
			if parsed, err := time.Parse(time.RFC3339, stamp); err == nil {
				state.started = parsed
			}
		}
		if stamp := strings.TrimSpace(pin.Finished); stamp != "" {
			if parsed, err := time.Parse(time.RFC3339, stamp); err == nil {
				state.finished = parsed
			}
		}
		if pin.Error != "" {
			state.err = taskStatusError(pin.Error)
		}
		running := !desktopRunTerminal(state.status)
		state.done = !running
		s.desktopAdmits[key] = state
		if running && state.instanceID != "" && state.runID != "" {
			job := admittedDesktopFollow{
				TenantID:   tenantID,
				UserID:     state.userID,
				BotID:      state.botID,
				InstanceID: state.instanceID,
				RunID:      state.runID,
				Phase:      state.phase,
				Task:       state.task,
				Started:    state.started,
			}
			go s.watchAdmittedDesktopRun(job, state)
		}
	}
}

type taskStatusError string

func (e taskStatusError) Error() string { return string(e) }

func (s *Service) persistDesktopTasks(tenantID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	s.persistDesktopState(tenantID)
	s.mu.Unlock()
}

// noteDesktopTaskStatus records the terminal status of a run that was settled
// from MaClawSrv rather than by this process's watcher, and keeps the pin.
func (s *Service) noteDesktopTaskStatus(tenantID, userID, botID, instanceID, runID, status string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	if state == nil {
		return
	}
	state.mu.Lock()
	if strings.TrimSpace(status) != "" {
		state.status = strings.TrimSpace(status)
	}
	if state.userID == "" {
		state.userID = strings.TrimSpace(userID)
	}
	if state.botID == "" {
		state.botID = strings.TrimSpace(botID)
	}
	if state.runID == "" {
		state.runID = strings.TrimSpace(runID)
	}
	if state.instanceID == "" {
		state.instanceID = strings.TrimSpace(instanceID)
	}
	state.mu.Unlock()
	s.persistDesktopState(tenantID)
}

// DesktopTaskQuery lists this bot's recent runs and the sentence to show for
// a status question. hint is the person's words about which task they mean.
// An empty hint, or one that only asks "how is it going", lists the recent runs.
func (s *Service) DesktopTaskQuery(ctx context.Context, tenantID, userID, botID, hint, lang string) (string, []DesktopTaskStatus, error) {
	items, err := s.listDesktopTasks(ctx, tenantID, userID, botID)
	if err != nil {
		return "", nil, err
	}
	specific := desktopTaskHint(hint) != ""
	chosen := items
	if specific {
		matched := make([]DesktopTaskStatus, 0, len(items))
		for _, item := range items {
			if desktopTaskMatches(hint, item.Task) {
				matched = append(matched, item)
			}
		}
		if len(matched) == 0 {
			return formatDesktopTaskStatus(nil, items, lang, false), nil, nil
		}
		chosen = preferDesktopTaskMatches(hint, matched)
	}
	if len(chosen) > desktopTaskListCap {
		chosen = chosen[:desktopTaskListCap]
	}
	return formatDesktopTaskStatus(chosen, nil, lang, true), chosen, nil
}

// PendingDesktopDeliveries lists finished runs this user has not been shown.
func (s *Service) PendingDesktopDeliveries(ctx context.Context, tenantID, userID string) ([]DesktopTaskDelivery, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	if _, err := s.loadForUser(ctx, tenantID, userID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	prefix := desktopTenantPrefix(tenantID) + strings.TrimSpace(userID) + "\x00"
	now := s.now()
	out := make([]DesktopTaskDelivery, 0)
	needs := make([]pendingMedia, 0)
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		if !state.done || state.delivered || desktopTaskExpired(state.done, state.delivered, state.finished, now) {
			state.mu.Unlock()
			continue
		}
		item := deliveryFromState(state)
		needMedia := !state.mediaFetched && state.instanceID != "" && len(state.reply.Images) == 0 && len(state.reply.Files) == 0 && desktopMediaDue(state.mediaNext, now)
		instanceID := state.instanceID
		state.mu.Unlock()
		if item.RunID == "" || item.BotID == "" {
			continue
		}
		out = append(out, item)
		if needMedia {
			needs = append(needs, pendingMedia{state: state, instanceID: instanceID, runID: item.RunID, index: len(out) - 1})
		}
	}
	s.mu.Unlock()
	s.fillPendingMedia(ctx, tenantID, userID, needs, out)
	sortDesktopDeliveries(out)
	return out, nil
}

type pendingMedia struct {
	state      *desktopAdmitState
	instanceID string
	runID      string
	index      int
}

func desktopMediaDue(next, now time.Time) bool {
	return next.IsZero() || !now.Before(next)
}

// fillPendingMedia copies screenshots back onto a result that was restored
// from the text pin. The pin does not keep the image bytes, and MaClawSrv
// still has them until the run is reaped. A missing run is not asked again.
// A transport failure waits two minutes, so a down worker does not add a
// timeout to every status pull.
func (s *Service) fillPendingMedia(ctx context.Context, tenantID, userID string, needs []pendingMedia, out []DesktopTaskDelivery) {
	if len(needs) == 0 {
		return
	}
	budget, cancelBudget := context.WithTimeout(ctx, 3*time.Second)
	defer cancelBudget()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, need := range needs {
		wg.Add(1)
		go func(need pendingMedia) {
			defer wg.Done()
			select {
			case <-budget.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()
			refreshCtx, cancel := context.WithTimeout(budget, 3*time.Second)
			defer cancel()
			view, err := s.readDesktopRunFromSrv(refreshCtx, tenantID, userID, need.instanceID, need.runID)
			if err != nil || !desktopRunTerminal(view.Status) {
				need.state.mu.Lock()
				if err != nil && errors.Is(err, ErrSrvNotFound) {
					need.state.mediaFetched = true
				} else {
					need.state.mediaNext = s.now().Add(desktopTaskMediaRetry)
				}
				need.state.mu.Unlock()
				return
			}
			need.state.mu.Lock()
			need.state.mediaFetched = true
			if images := replyImages(view.Message.Attachments); len(images) > 0 && len(need.state.reply.Images) == 0 {
				need.state.reply.Images = images
			}
			if files := replyFiles(view.Message.Attachments); len(files) > 0 && len(need.state.reply.Files) == 0 {
				need.state.reply.Files = files
			}
			if strings.TrimSpace(need.state.reply.Text) == "" {
				if text := strings.TrimSpace(view.Message.Content); text != "" {
					need.state.reply.Text = text
				}
			}
			if need.index >= 0 && need.index < len(out) {
				out[need.index].Reply = need.state.reply
			}
			need.state.mu.Unlock()
		}(need)
	}
	wg.Wait()
}

// AckDesktopTask marks one run's result as shown. A second ack is a no-op,
// including a run this process no longer has: the GUI retries after a restart.
func (s *Service) AckDesktopTask(ctx context.Context, tenantID, userID, botID, runID string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	if _, err := s.loadForUser(ctx, tenantID, userID); err != nil {
		return err
	}
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	if botID == "" || runID == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	if state == nil {
		return nil
	}
	state.mu.Lock()
	state.delivered = true
	state.mu.Unlock()
	s.persistDesktopState(tenantID)
	return nil
}

func (s *Service) listDesktopTasks(ctx context.Context, tenantID, userID, botID string) ([]DesktopTaskStatus, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return nil, ErrNotFound
	}
	botID = rec.Bots[index].ID
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	now := s.now()
	items := make([]DesktopTaskStatus, 0)
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		if desktopTaskExpired(state.done, state.delivered, state.finished, now) {
			state.mu.Unlock()
			continue
		}
		items = append(items, statusFromState(state))
		state.mu.Unlock()
	}
	s.mu.Unlock()
	sortDesktopTaskStatus(items)
	return items, nil
}

func statusFromState(state *desktopAdmitState) DesktopTaskStatus {
	status := strings.TrimSpace(state.status)
	if status == "" {
		if state.done {
			status = "succeeded"
		} else {
			status = "running"
		}
	}
	errText := ""
	if state.err != nil {
		errText = state.err.Error()
	}
	return DesktopTaskStatus{
		RunID:   state.runID,
		BotID:   state.botID,
		Task:    state.task,
		Status:  status,
		Text:    state.reply.Text,
		Error:   errText,
		Started: state.started,
	}
}

func deliveryFromState(state *desktopAdmitState) DesktopTaskDelivery {
	view := statusFromState(state)
	reply := state.reply
	if strings.TrimSpace(reply.Text) == "" && view.Error != "" {
		reply.Text = view.Error
	}
	return DesktopTaskDelivery{
		BotID:  view.BotID,
		RunID:  view.RunID,
		Task:   view.Task,
		Status: view.Status,
		Reply:  reply,
		Error:  view.Error,
	}
}

func sortDesktopTaskStatus(items []DesktopTaskStatus) {
	sort.SliceStable(items, func(i, j int) bool {
		runningI := items[i].Status == "running"
		runningJ := items[j].Status == "running"
		if runningI != runningJ {
			return runningI
		}
		return items[i].Started.After(items[j].Started)
	})
}

func sortDesktopDeliveries(items []DesktopTaskDelivery) {
	// Nothing finer than run id is required for a stable push order.
	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i
		for j > 0 && items[j-1].RunID > item.RunID {
			items[j] = items[j-1]
			j--
		}
		items[j] = item
	}
}

// Question phrases are specific enough to remove wherever they sit. Short
// words such as 任务 stay in place: "任务分配" is a name, and deleting 任务
// from the middle would leave "分配", which then matches a different task.
var desktopTaskQuestionPhrases = []string{
	"查一下", "看一下", "怎么样", "怎样了", "如何了", "什么情况", "什么进度",
	"完成了吗", "好了吗", "好了没", "做完了吗", "状态如何", "进度如何",
	"what about", "how's", "how is", "status of", "progress of", "it going",
}

var desktopTaskEdgePrefixes = []string{"那个", "这个"}

var desktopTaskEdgeSuffixes = []string{"状态", "进度", "status", "progress"}

var desktopTaskGenericWords = map[string]struct{}{
	"任务": {}, "结果": {}, "情况": {}, "状态": {}, "进度": {}, "那个": {}, "这个": {},
	"现在": {}, "查询": {}, "看看": {}, "一下": {},
	"task": {}, "about": {}, "status": {}, "progress": {}, "the": {}, "a": {}, "an": {},
	"it": {}, "is": {}, "my": {}, "this": {}, "that": {}, "going": {},
	"of": {}, "and": {}, "or": {}, "to": {}, "for": {}, "in": {}, "on": {}, "at": {}, "by": {},
}

var desktopTaskParticles = map[rune]struct{}{
	'了': {}, '的': {}, '吗': {}, '呢': {}, '啊': {}, '呀': {}, '吧': {},
}

// desktopTaskHint is what remains of a status question after the words that
// only mean "tell me how it is going" are taken out. Empty means the person
// did not name a task.
func desktopTaskHint(hint string) string {
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" {
		return ""
	}
	for _, phrase := range desktopTaskQuestionPhrases {
		hint = strings.ReplaceAll(hint, phrase, " ")
	}
	var b strings.Builder
	for _, r := range hint {
		switch r {
		case '，', '。', '？', '！', '、', ',', '.', '?', '!', ':', '：', ';', '；', '"', '\'', '“', '”', '（', '）', '(', ')':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	fields := strings.Fields(b.String())
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		field = trimDesktopTaskEdges(field)
		if field == "" || desktopTaskGeneric(field) {
			continue
		}
		kept = append(kept, field)
	}
	return strings.Join(kept, " ")
}

func trimDesktopTaskEdges(field string) string {
	for {
		next := trimListedAffix(field, desktopTaskEdgePrefixes, true)
		next = trimStatusQuestionSuffix(next)
		next = dropDesktopTaskLinker(next)
		next = trimDesktopTaskParticles(next)
		if next == field {
			return field
		}
		field = next
	}
}

// trimStatusQuestionSuffix removes a trailing 进度 or 状态 only when a
// particle sits in front of it, as in 下载的进度. 项目进度 keeps its ending,
// because that ending is the name.
func trimStatusQuestionSuffix(field string) string {
	for _, word := range desktopTaskEdgeSuffixes {
		if len(field) <= len(word) || !strings.HasSuffix(field, word) {
			continue
		}
		head := []rune(field[:len(field)-len(word)])
		if len(head) == 0 {
			continue
		}
		if _, ok := desktopTaskParticles[head[len(head)-1]]; !ok {
			continue
		}
		return trimDesktopTaskParticles(string(head))
	}
	return field
}

var desktopTaskGenericOrder = func() []string {
	words := make([]string, 0, len(desktopTaskGenericWords))
	for word := range desktopTaskGenericWords {
		words = append(words, word)
	}
	sort.Slice(words, func(i, j int) bool { return len(words[i]) > len(words[j]) })
	return words
}()

func desktopTaskGeneric(field string) bool {
	if field == "" {
		return false
	}
	if _, ok := desktopTaskGenericWords[field]; ok {
		return true
	}
	rest := field
	for rest != "" {
		matched := false
		for _, word := range desktopTaskGenericOrder {
			if strings.HasPrefix(rest, word) {
				rest = rest[len(word):]
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func trimListedAffix(field string, words []string, prefix bool) string {
	for _, word := range words {
		if word == "" || len(field) <= len(word) {
			continue
		}
		if prefix && strings.HasPrefix(field, word) {
			return strings.TrimSpace(field[len(word):])
		}
		if !prefix && strings.HasSuffix(field, word) {
			return strings.TrimSpace(field[:len(field)-len(word)])
		}
	}
	return field
}

// dropDesktopTaskLinker removes a grammatical 的 between two real words,
// so 下载的报表 still finds 下载报表. 目的地 stays as it is: the 的 there
// has a one-character neighbor and is part of the name.
func dropDesktopTaskLinker(field string) string {
	if !strings.Contains(field, "的") {
		return field
	}
	parts := strings.Split(field, "的")
	for _, part := range parts {
		if utf8.RuneCountInString(part) < 2 {
			return field
		}
	}
	return strings.Join(parts, "")
}

func trimDesktopTaskParticles(field string) string {
	runes := []rune(field)
	for len(runes) > 0 {
		if _, ok := desktopTaskParticles[runes[len(runes)-1]]; !ok {
			break
		}
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func desktopTaskMatches(hint, task string) bool {
	needle := desktopTaskHint(hint)
	if needle == "" {
		return true
	}
	hay := strings.ToLower(strings.TrimSpace(task))
	if hay == "" {
		return false
	}
	if strings.Contains(hay, strings.ReplaceAll(needle, " ", "")) || strings.Contains(hay, needle) {
		return true
	}
	// Every remaining word has to be in the task. One shared word is not
	// enough: "download report" must not answer "annual report".
	saw := false
	for _, part := range strings.Fields(needle) {
		if utf8.RuneCountInString(part) < 2 {
			continue
		}
		saw = true
		if !strings.Contains(hay, part) {
			return false
		}
	}
	return saw
}

// preferDesktopTaskMatches keeps the tasks that also contain the progress
// word from the question, when any of them do. 项目的进度 then stays on
// 项目进度. 下载的进度 still answers 下载报表, because that task has no
// closer name.
func preferDesktopTaskMatches(hint string, matched []DesktopTaskStatus) []DesktopTaskStatus {
	if len(matched) < 2 {
		return matched
	}
	word := desktopTaskFocusWord(hint)
	if word == "" {
		return matched
	}
	tighter := make([]DesktopTaskStatus, 0, len(matched))
	for _, item := range matched {
		if strings.Contains(strings.ToLower(item.Task), word) {
			tighter = append(tighter, item)
		}
	}
	if len(tighter) == 0 {
		return matched
	}
	return tighter
}

func desktopTaskFocusWord(hint string) string {
	hint = strings.ToLower(hint)
	last := ""
	lastAt := -1
	for _, word := range []string{"进度", "状态", "progress", "status"} {
		if at := strings.LastIndex(hint, word); at > lastAt {
			lastAt = at
			last = word
		}
	}
	return last
}

func formatDesktopTaskStatus(chosen, recent []DesktopTaskStatus, lang string, matched bool) string {
	hant := strings.Contains(lang, "Hant")
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en")
	if !matched {
		lead := "没有找到和这句话对应的任务。"
		if hant {
			lead = "沒有找到和這句話對應的任務。"
		}
		if en {
			lead = "No task matches that."
		}
		if len(recent) == 0 {
			if en {
				return "No running or recently finished task."
			}
			if hant {
				return "沒有還在進行或剛完成的任務。"
			}
			return "没有还在进行或刚完成的任务。"
		}
		lines := make([]string, 0, len(recent)+1)
		lines = append(lines, lead)
		limit := len(recent)
		if limit > 3 {
			limit = 3
		}
		for _, item := range recent[:limit] {
			lines = append(lines, desktopTaskStatusLine(item, lang, false))
		}
		return strings.Join(lines, "\n")
	}
	if len(chosen) == 0 {
		if en {
			return "No running or recently finished task."
		}
		if hant {
			return "沒有還在進行或剛完成的任務。"
		}
		return "没有还在进行或刚完成的任务。"
	}
	lines := make([]string, 0, len(chosen))
	for _, item := range chosen {
		lines = append(lines, desktopTaskStatusLine(item, lang, true))
	}
	return strings.Join(lines, "\n\n")
}

func desktopTaskStatusLine(item DesktopTaskStatus, lang string, withResult bool) string {
	label := clipDesktopRunes(item.Task, desktopTaskLabelKeep)
	if label == "" {
		label = item.RunID
	}
	hant := strings.Contains(lang, "Hant")
	en := strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en")
	var line string
	switch item.Status {
	case "running":
		if en {
			line = `"` + label + `" is still running.`
		} else if hant {
			line = "「" + label + "」還在進行。"
		} else {
			line = "「" + label + "」还在进行。"
		}
	case "failed", "cancelled":
		if en {
			line = `"` + label + `" did not finish.`
		} else if hant {
			line = "「" + label + "」沒有完成。"
		} else {
			line = "「" + label + "」没有完成。"
		}
	default:
		if en {
			line = `"` + label + `" is finished.`
		} else if hant {
			line = "「" + label + "」已經完成。"
		} else {
			line = "「" + label + "」已经完成。"
		}
	}
	if !withResult || item.Status == "running" {
		return line
	}
	detail := strings.TrimSpace(item.Text)
	if detail == "" {
		detail = strings.TrimSpace(item.Error)
	}
	detail = clipDesktopRunes(detail, desktopTaskStatusKeep)
	if detail == "" || detail == label {
		return line
	}
	return line + "\n" + detail
}
