package guiapp

import (
	"database/sql"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
	_ "modernc.org/sqlite"
)

const petRearingDBFile = "pet-rearing.db"

// petRearingState counts wall time while one pack is the live pet and applies
// the highest stage that time has reached. The user's saved size is not rewritten.
type petRearingState struct {
	mu        sync.Mutex
	db        *sql.DB
	dbPath    string
	pack      string
	loaded    int64
	announced string
	userSize  int
	running   bool
	observed  bool
	last      time.Time
	loopOn           bool
	stop             chan struct{}
	needsDutyHydrate bool
}

// rearedWindowSize is the window for this pack's current stage. Creating at
// the saved user size and resizing afterwards flashes the smaller pet.
func rearedWindowSize(app *App, pack string, userSize int) int {
	scale := 1.0
	if app != nil {
		pack = strings.TrimSpace(pack)
		if pack == "" {
			pack = petpack.DefaultPackID
		}
		_, scale = app.petRearingLook(pack)
	}
	return rearingWindowSize(userSize, scale)
}

func rearingWindowSize(userSize int, scale float64) int {
	if userSize <= 0 {
		userSize = defaultPetSize
	}
	if userSize < minPetSize {
		userSize = minPetSize
	}
	if userSize > maxPetSize {
		userSize = maxPetSize
	}
	if scale <= 0 {
		scale = 1
	}
	size := int(math.Round(float64(userSize) * scale))
	if size < minPetSize {
		size = minPetSize
	}
	if size > maxPetSize {
		size = maxPetSize
	}
	return size + 16
}

func petAppearanceVariant(userVariant, rearingVariant string) string {
	if strings.TrimSpace(rearingVariant) != "" {
		return petpack.ResolveVariantForRuntime(rearingVariant)
	}
	return petpack.ResolveVariantForRuntime(userVariant)
}

type rearingWindow struct {
	variant      string
	windowSize   int
	cue          string
	stageID      string
	adornments   []string
	refreshBuddy bool
}

func (a *App) syncPetRearing(packID string, visible bool, userSize int) {
	a.syncPetRearingAt(packID, visible, userSize, time.Now())
}

func (a *App) syncPetRearingAt(packID string, visible bool, userSize int, now time.Time) {
	cmd, live, _ := a.stagePetRearingAt(packID, visible, userSize, now)
	if !live {
		return
	}
	a.deliverPetRearing(cmd)
	a.hydratePetDutyFromRuntimeIfNeeded()
}

// stagePetRearingAt commits the live pack and opens the click gate before it
// does the slow save. Callers that are about to create a visible window must
// use this rather than waiting for deliver: Windows creates that window
// already visible, and a click in the gap has to land on this session.
// live is false when the pet is hidden. fresh is true when the clock just
// started, so the caller can drop the time spent before the window was shown.
func (a *App) stagePetRearingAt(packID string, visible bool, userSize int, now time.Time) (cmd rearingWindow, live bool, fresh bool) {
	if a == nil {
		return rearingWindow{}, false, false
	}
	a.petRearing.mu.Lock()
	wasRunning := a.petRearing.running
	nextPack := strings.TrimSpace(packID)
	hiding := !visible || nextPack == ""
	switching := !hiding && wasRunning && a.petRearing.pack != "" && a.petRearing.pack != nextPack
	if hiding || switching {
		// The open burst belongs to this pet session. Retire it before the
		// pack or the visibility bit changes, so the timer cannot credit the
		// next pet or open the main window after this one is gone.
		a.retirePetClickBurst()
	}
	if !hiding {
		// The window can already be on screen (a skin change) or is about to
		// be created visible. Open the gate before flush and plan, both of
		// which take the database. apply waits on this mutex, so a click
		// recorded now credits the pack committed below. Process exit keeps
		// the gate shut.
		a.allowDesktopPetClick()
	}
	a.flushPetRearingLocked(now)
	if hiding {
		// The clock stops while this pack is not the live pet. Leaving last
		// at the hide time made the next show count up to a minute of the gap.
		a.petRearing.running = false
		a.petRearing.last = time.Time{}
		a.petRearing.needsDutyHydrate = false
		a.closePetRearingDBLocked()
		a.petRearing.mu.Unlock()
		return rearingWindow{}, false, false
	}
	packID = nextPack
	if a.petRearing.pack != packID {
		a.petRearing.pack = packID
		a.petRearing.loaded, a.petRearing.announced = a.loadPetRearingLocked(packID)
		a.petRearing.observed = false
		a.petRearing.last = now
	}
	a.petRearing.userSize = userSize
	a.petRearing.running = true
	if !wasRunning || a.petRearing.last.IsZero() {
		a.petRearing.last = now
		fresh = !wasRunning
	}
	if !wasRunning {
		a.petRearing.needsDutyHydrate = true
	}
	cmd = a.planPetRearingLocked()
	a.ensurePetRearingLoopLocked()
	a.petRearing.mu.Unlock()
	return cmd, true, fresh
}

// rebasePetRearingClock forgets time that passed before the pet could be
// seen. The session starts early so a click during window creation still has
// a pack; that gap is not time on screen.
func (a *App) rebasePetRearingClock(now time.Time) {
	if a == nil {
		return
	}
	a.petRearing.mu.Lock()
	if a.petRearing.running {
		a.petRearing.last = now
	}
	a.petRearing.mu.Unlock()
}

func (a *App) hydratePetDutyFromRuntimeIfNeeded() {
	if a == nil {
		return
	}
	a.petRearing.mu.Lock()
	need := a.petRearing.needsDutyHydrate
	if need {
		a.petRearing.needsDutyHydrate = false
	}
	a.petRearing.mu.Unlock()
	if need {
		a.hydratePetDutyFromRuntime()
	}
}

// flushPetRearingForExit writes the open interval before the process is
// killed. The 30s loop would otherwise leave that tail only in memory.
func (a *App) flushPetRearingForExit() {
	if a == nil {
		return
	}
	a.flushPetRearingForExitAt(time.Now())
}

func (a *App) flushPetRearingForExitAt(now time.Time) {
	if a == nil {
		return
	}
	a.petRearing.mu.Lock()
	a.flushPetRearingLocked(now)
	a.closePetRearingDBLocked()
	a.petRearing.mu.Unlock()
}

func (a *App) flushPetRearingLocked(now time.Time) {
	if !a.petRearing.running || a.petRearing.pack == "" || a.petRearing.last.IsZero() {
		return
	}
	dt := now.Sub(a.petRearing.last)
	if dt < 0 {
		dt = 0
	}
	if dt > time.Minute {
		dt = time.Minute
	}
	a.petRearing.loaded += dt.Milliseconds()
	a.petRearing.last = now
	a.savePetRearingLocked()
}

func (a *App) planPetRearingLocked() rearingWindow {
	stageID := ""
	variant := ""
	scale := 1.0
	cue := ""
	var adornments []string
	reg := petpack.EnsureGlobal()
	if reg != nil {
		if manifest, ok := reg.Get(a.petRearing.pack); ok && manifest != nil {
			adornments = petpack.ActiveAdornmentSlots(manifest, a.petRearing.loaded)
			if stage, found := petpack.ActiveRearingStage(manifest.Rearing.Stages, a.petRearing.loaded); found {
				stageID = stage.ID
				variant = stage.Variant
				scale = petpack.StageScale(&stage)
				cue = stage.Cue
			}
		}
	}
	// The first observation only records the stage. Startup already sends the
	// reel for that stage. A later crossing resends it once.
	changed := stageID != a.petRearing.announced
	play := ""
	if changed && a.petRearing.observed && !a.petDutyCueRecent() {
		play = cue
	}
	refresh := changed && a.petRearing.observed
	a.petRearing.announced = stageID
	a.petRearing.observed = true
	a.savePetRearingLocked()
	return rearingWindow{
		variant:      variant,
		windowSize:   rearingWindowSize(a.petRearing.userSize, scale),
		cue:          play,
		stageID:      stageID,
		adornments:   adornments,
		refreshBuddy: refresh,
	}
}

func sameAdornmentSlots(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a *App) deliverPetRearing(cmd rearingWindow) {
	if a == nil {
		return
	}
	a.pushPetRearing(cmd)
	if cmd.refreshBuddy {
		go a.refreshBuddyPetFromRearing(cmd.stageID)
	}
}

func (a *App) pushPetRearing(cmd rearingWindow) {
	fa := a.existingFloatingAssistant()
	if fa == nil {
		return
	}
	fa.applyRearing(cmd)
}

func (a *App) ensurePetRearingLoopLocked() {
	if a.petRearing.loopOn || testing.Testing() {
		return
	}
	a.petRearing.loopOn = true
	stop := make(chan struct{})
	a.petRearing.stop = stop
	go a.petRearingLoop(stop)
}

func (a *App) petRearingLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			a.petRearing.mu.Lock()
			if !a.petRearing.running {
				a.petRearing.mu.Unlock()
				continue
			}
			a.flushPetRearingLocked(now)
			cmd := a.planPetRearingLocked()
			a.petRearing.mu.Unlock()
			a.deliverPetRearing(cmd)
		}
	}
}

// petRearingLook is the variant and scale the buddy bakes into its frames.
// A running pet uses the in-memory counter. Otherwise the saved loaded time
// is read and the database is closed again when this call opened it.
func (a *App) petRearingLook(packID string) (string, float64) {
	if a == nil {
		return "", 1
	}
	return petRearingLookFor(packID, a.petRearingLoadedMS(packID))
}

func (a *App) petRearingLoadedMS(packID string) int64 {
	if a == nil {
		return 0
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		return 0
	}
	a.petRearing.mu.Lock()
	defer a.petRearing.mu.Unlock()
	if a.petRearing.running && a.petRearing.pack == packID {
		return a.petRearing.loaded
	}
	opened := a.petRearing.db == nil
	loaded, _ := a.loadPetRearingLocked(packID)
	if opened {
		a.closePetRearingDBLocked()
	}
	return loaded
}

func petRearingLookFor(packID string, loaded int64) (string, float64) {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return "", 1
	}
	manifest, ok := reg.Get(packID)
	if !ok || manifest == nil {
		return "", 1
	}
	stage, found := petpack.ActiveRearingStage(manifest.Rearing.Stages, loaded)
	if !found {
		return "", 1
	}
	return stage.Variant, petpack.StageScale(&stage)
}

// petRearingAdornmentSlots is the prop set baked into the buddy reel.
// No stage, or a stage that lists nothing, wears nothing.
func (a *App) petRearingAdornmentSlots(packID string) []string {
	if a == nil {
		return nil
	}
	return petRearingAdornmentSlotsFor(packID, a.petRearingLoadedMS(packID))
}

// copyPetRearingAdornmentSlots is the prop list for the first paint. Create
// draws that frame before the deferred rearing apply, so the saved slots
// have to be copied up front or the props pop in afterwards.
func (a *App) copyPetRearingAdornmentSlots(packID string) []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.petRearingAdornmentSlots(packID)...)
}

func petRearingAdornmentSlotsFor(packID string, loaded int64) []string {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil
	}
	manifest, ok := reg.Get(strings.TrimSpace(packID))
	if !ok || manifest == nil {
		return nil
	}
	return petpack.ActiveAdornmentSlots(manifest, loaded)
}

func (a *App) refreshBuddyPetFromRearing(stageID string) {
	if a == nil {
		return
	}
	a.buddyMu.Lock()
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if runner == nil {
		return
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		logBuddyf("[buddy] pet rearing refresh skipped: %v", err)
		return
	}
	logBuddyf("[buddy] pet rearing stage %q; resending frames", stageID)
	a.refreshBuddyPetForConfig(cfg)
}

func (a *App) openPetRearingDBLocked() {
	if a == nil || a.petRearing.db != nil {
		return
	}
	// A test that never set a home would otherwise create the real profile db.
	if testing.Testing() && strings.TrimSpace(a.testHomeDir) == "" {
		return
	}
	dir := strings.TrimSpace(a.GetDataDir())
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[pet-rearing] mkdir: %v", err)
		return
	}
	path := filepath.Join(dir, petRearingDBFile)
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Printf("[pet-rearing] open: %v", err)
		return
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS pet_rearing (
		pack_id TEXT PRIMARY KEY,
		loaded_ms INTEGER NOT NULL,
		announced TEXT NOT NULL DEFAULT '',
		updated_unix INTEGER NOT NULL
	)`); err != nil {
		log.Printf("[pet-rearing] schema: %v", err)
		_ = db.Close()
		return
	}
	a.petRearing.db = db
	a.petRearing.dbPath = path
}

func (a *App) closePetRearingDBLocked() {
	if a.petRearing.db == nil {
		return
	}
	_ = a.petRearing.db.Close()
	a.petRearing.db = nil
}

func (a *App) loadPetRearingLocked(packID string) (int64, string) {
	a.openPetRearingDBLocked()
	if a.petRearing.db == nil {
		return 0, ""
	}
	var loaded int64
	var announced string
	err := a.petRearing.db.QueryRow(`SELECT loaded_ms, announced FROM pet_rearing WHERE pack_id = ?`, packID).Scan(&loaded, &announced)
	if err != nil {
		return 0, ""
	}
	if loaded < 0 {
		loaded = 0
	}
	return loaded, announced
}

func (a *App) savePetRearingLocked() {
	if a.petRearing.pack == "" {
		return
	}
	a.openPetRearingDBLocked()
	if a.petRearing.db == nil {
		return
	}
	_, err := a.petRearing.db.Exec(`INSERT INTO pet_rearing(pack_id, loaded_ms, announced, updated_unix)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(pack_id) DO UPDATE SET
			loaded_ms = excluded.loaded_ms,
			announced = excluded.announced,
			updated_unix = excluded.updated_unix`,
		a.petRearing.pack, a.petRearing.loaded, a.petRearing.announced, time.Now().Unix())
	if err != nil {
		log.Printf("[pet-rearing] save: %v", err)
	}
}
