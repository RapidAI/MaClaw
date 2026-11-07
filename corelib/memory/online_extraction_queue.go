package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

const onlineExtractionBatchSize = 20

type onlineExtractionRequest struct {
	OwnerID       string                `json:"owner_id"`
	Messages      []ConversationMessage `json:"messages"`
	Summary       string                `json:"summary,omitempty"`
	ReferenceTime time.Time             `json:"reference_time"`
	WriteContext  WriteContext          `json:"write_context"`
	NotBefore     time.Time             `json:"not_before"`
	Attempts      int                   `json:"attempts,omitempty"`
}

type onlineExtractionQueueDisk struct {
	Requests map[string]*onlineExtractionRequest `json:"requests"`
}

type onlineExtractionQueue struct {
	extractor *OnlineExtractor
	path      string

	mu            sync.Mutex
	pending       map[string]*onlineExtractionRequest
	pause         func(ownerID string) bool
	running       bool
	stopped       bool
	wake          chan struct{}
	stopCh        chan struct{}
	done          chan struct{}
	attemptCancel context.CancelFunc
	stopOnce      sync.Once
}

func (oe *OnlineExtractor) extractionQueue() *onlineExtractionQueue {
	oe.mu.Lock()
	defer oe.mu.Unlock()
	if oe.queue != nil {
		return oe.queue
	}
	path := ""
	if oe.store != nil && strings.TrimSpace(oe.store.Path()) != "" {
		path = filepath.Join(filepath.Dir(oe.store.Path()), "online-extraction-queue.json")
	}
	oe.queue = newOnlineExtractionQueue(oe, path)
	return oe.queue
}

func newOnlineExtractionQueue(extractor *OnlineExtractor, path string) *onlineExtractionQueue {
	queue := &onlineExtractionQueue{
		extractor: extractor,
		path:      path,
		pending:   make(map[string]*onlineExtractionRequest),
		wake:      make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}
	queue.load()
	return queue
}

func onlineExtractionRequestKey(ownerID, projectPath string) string {
	return strings.TrimSpace(ownerID) + "\x00" + semanticNormalizeProjectPath(projectPath)
}

func (oe *OnlineExtractor) EnqueueExtraction(
	ctx context.Context,
	messages []ConversationMessage,
	summary string,
	referenceTime time.Time,
	ownerID string,
	writeContexts ...WriteContext,
) error {
	if oe == nil || len(messages) == 0 {
		return nil
	}
	writeContext := WriteContext{OwnerID: strings.TrimSpace(ownerID), Source: "online_extraction"}
	if len(writeContexts) > 0 {
		writeContext = writeContexts[0]
		if writeContext.OwnerID == "" {
			writeContext.OwnerID = strings.TrimSpace(ownerID)
		}
		if writeContext.Source == "" {
			writeContext.Source = "online_extraction"
		}
	}
	request := &onlineExtractionRequest{
		OwnerID:       strings.TrimSpace(ownerID),
		Messages:      append([]ConversationMessage(nil), messages...),
		Summary:       summary,
		ReferenceTime: referenceTime,
		WriteContext:  writeContext,
		NotBefore:     time.Now(),
	}
	if request.OwnerID == "" {
		request.OwnerID = writeContext.OwnerID
	}
	request.WriteContext.OwnerID = request.OwnerID
	queue := oe.extractionQueue()
	if err := queue.enqueue(request); err != nil {
		return err
	}
	queue.start()
	return nil
}

func (oe *OnlineExtractor) SetQueuePauseChecker(checker func(ownerID string) bool) {
	if oe == nil {
		return
	}
	queue := oe.extractionQueue()
	queue.mu.Lock()
	queue.pause = checker
	queue.mu.Unlock()
	queue.start()
}

func (oe *OnlineExtractor) Stop() {
	if oe == nil {
		return
	}
	oe.mu.Lock()
	queue := oe.queue
	oe.mu.Unlock()
	if queue != nil {
		queue.stop()
	}
}

func (q *onlineExtractionQueue) enqueue(request *onlineExtractionRequest) error {
	if q == nil || request == nil || len(request.Messages) == 0 {
		return nil
	}
	key := onlineExtractionRequestKey(request.OwnerID, request.WriteContext.ProjectPath)
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return fmt.Errorf("online extraction queue is stopped")
	}
	current := q.pending[key]
	if current == nil {
		q.pending[key] = request
	} else {
		current.Messages = mergeExtractionMessages(current.Messages, request.Messages)
		if request.Summary != "" {
			current.Summary = request.Summary
		}
		if request.ReferenceTime.After(current.ReferenceTime) {
			current.ReferenceTime = request.ReferenceTime
		}
		current.WriteContext = request.WriteContext
		if request.NotBefore.Before(current.NotBefore) {
			current.NotBefore = request.NotBefore
		}
	}
	err := q.persistLocked()
	q.mu.Unlock()
	q.signal()
	return err
}

func mergeExtractionMessages(existing, incoming []ConversationMessage) []ConversationMessage {
	out := append([]ConversationMessage(nil), existing...)
	seen := make(map[string]struct{}, len(out))
	for i := range out {
		out[i].ID = extractionMessageID(out[i], i)
		seen[out[i].ID] = struct{}{}
	}
	for i, message := range incoming {
		message.ID = extractionMessageID(message, len(out)+i)
		if _, ok := seen[message.ID]; ok {
			continue
		}
		seen[message.ID] = struct{}{}
		out = append(out, message)
	}
	return out
}

func ConversationMessageID(existing string, timestamp int64, index int, role, content string) string {
	if id := strings.TrimSpace(existing); id != "" {
		return id
	}
	seed := strings.Join([]string{strconv.FormatInt(timestamp, 10), strconv.Itoa(index), role, content}, "\x00")
	digest := sha256.Sum256([]byte(seed))
	return "legacy-" + hex.EncodeToString(digest[:])
}

func extractionMessageID(message ConversationMessage, index int) string {
	if id := strings.TrimSpace(message.ID); id != "" {
		return id
	}
	return "content-" + computeContentHash(fmt.Sprintf("%d\x00%s\x00%s", index, message.Role, message.Content))
}

func (q *onlineExtractionQueue) start() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.running || q.stopped || len(q.pending) == 0 || !q.extractor.configured() {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.done = make(chan struct{})
	q.mu.Unlock()
	go q.run()
}

func (oe *OnlineExtractor) configured() bool {
	oe.mu.Lock()
	defer oe.mu.Unlock()
	return oe.llm != nil && oe.llm.IsConfigured()
}

func (oe *OnlineExtractor) cooldownDuration() time.Duration {
	oe.mu.Lock()
	defer oe.mu.Unlock()
	return oe.cooldown
}

func (q *onlineExtractionQueue) run() {
	defer q.finish()
	for {
		request, key := q.next()
		if request == nil {
			return
		}
		if q.shouldPause(request.OwnerID) {
			if !q.wait(time.Second) {
				return
			}
			continue
		}
		if wait := time.Until(request.NotBefore); wait > 0 {
			if !q.wait(wait) {
				return
			}
			continue
		}
		batch := request.Messages
		if len(batch) > onlineExtractionBatchSize {
			batch = batch[:onlineExtractionBatchSize]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		q.mu.Lock()
		q.attemptCancel = cancel
		q.mu.Unlock()
		result := q.extractor.extractAndIntegrate(
			ctx,
			batch,
			request.Summary,
			request.ReferenceTime,
			request.OwnerID,
			false,
			false,
			request.WriteContext,
		)
		succeeded := result != nil && result.Errors == 0 && ctx.Err() == nil
		cancel()
		q.mu.Lock()
		q.attemptCancel = nil
		q.mu.Unlock()
		q.complete(key, batch, succeeded)
	}
}

func (q *onlineExtractionQueue) finish() {
	q.mu.Lock()
	q.running = false
	done := q.done
	restart := len(q.pending) > 0 && !q.stopped && q.extractor.configured()
	q.mu.Unlock()
	if done != nil {
		close(done)
	}
	if restart {
		q.start()
	}
}

func (q *onlineExtractionQueue) next() (*onlineExtractionRequest, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped || len(q.pending) == 0 || !q.extractor.configured() {
		return nil, ""
	}
	keys := make([]string, 0, len(q.pending))
	for key := range q.pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	selected := keys[0]
	for _, key := range keys[1:] {
		if q.pending[key].NotBefore.Before(q.pending[selected].NotBefore) {
			selected = key
		}
	}
	request := q.pending[selected]
	clone := *request
	clone.Messages = append([]ConversationMessage(nil), request.Messages...)
	return &clone, selected
}

func (q *onlineExtractionQueue) complete(key string, processed []ConversationMessage, success bool) {
	q.mu.Lock()
	request := q.pending[key]
	if request == nil {
		q.mu.Unlock()
		return
	}
	if !success {
		request.Attempts++
		request.NotBefore = time.Now().Add(retryDelay(q.extractor.cooldownDuration()))
		ownerID, attempts := request.OwnerID, request.Attempts
		_ = q.persistLocked()
		q.mu.Unlock()
		log.Printf("[online_extraction_queue] retained failed request owner=%q attempts=%d", ownerID, attempts)
		return
	}
	processedIDs := make(map[string]struct{}, len(processed))
	for i, message := range processed {
		processedIDs[extractionMessageID(message, i)] = struct{}{}
	}
	remaining := request.Messages[:0]
	for i, message := range request.Messages {
		if _, ok := processedIDs[extractionMessageID(message, i)]; ok {
			continue
		}
		remaining = append(remaining, message)
	}
	request.Messages = remaining
	if len(request.Messages) == 0 {
		delete(q.pending, key)
	} else {
		request.NotBefore = time.Now().Add(q.extractor.cooldownDuration())
		request.Attempts = 0
	}
	_ = q.persistLocked()
	q.mu.Unlock()
}

func retryDelay(cooldown time.Duration) time.Duration {
	if cooldown < 5*time.Second {
		return 5 * time.Second
	}
	return cooldown
}

func (q *onlineExtractionQueue) shouldPause(ownerID string) bool {
	q.mu.Lock()
	pause := q.pause
	q.mu.Unlock()
	return pause != nil && pause(ownerID)
}

func (q *onlineExtractionQueue) wait(duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-q.wake:
		return true
	case <-q.stopCh:
		return false
	}
}

func (q *onlineExtractionQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *onlineExtractionQueue) stop() {
	if q == nil {
		return
	}
	q.stopOnce.Do(func() {
		q.mu.Lock()
		q.stopped = true
		done := q.done
		running := q.running
		cancel := q.attemptCancel
		close(q.stopCh)
		q.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		q.signal()
		if running && done != nil {
			<-done
		}
	})
}

func (q *onlineExtractionQueue) pendingCount() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func (q *onlineExtractionQueue) persistLocked() error {
	if q.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(onlineExtractionQueueDisk{Requests: q.pending})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(q.path, data, 0o600)
}

func (q *onlineExtractionQueue) load() {
	if q == nil || q.path == "" {
		return
	}
	data, err := os.ReadFile(q.path)
	if err != nil {
		return
	}
	var disk onlineExtractionQueueDisk
	if json.Unmarshal(data, &disk) != nil {
		return
	}
	for key, request := range disk.Requests {
		if request == nil || len(request.Messages) == 0 {
			continue
		}
		for i := range request.Messages {
			request.Messages[i].ID = extractionMessageID(request.Messages[i], i)
		}
		if request.NotBefore.After(time.Now()) {
			request.NotBefore = time.Now()
		}
		q.pending[key] = request
	}
}
