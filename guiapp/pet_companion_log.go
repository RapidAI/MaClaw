package guiapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The pet companion keeps its own log so a wake, turn, or document question
// can be read without the rest of maclaw.log. Lines are written even when
// detailed logging is off.
const petCompanionLogName = "pet-companion.log"

var petLogState struct {
	mu   sync.Mutex
	file *os.File
}

func openPetCompanionLog(dir string) {
	petLogState.mu.Lock()
	defer petLogState.mu.Unlock()
	closePetCompanionLogLocked()
	if err := prepareLogDir(dir); err != nil {
		fmt.Fprintf(os.Stderr, "[pet-companion] refusing log dir %s: %v\n", dir, err)
		return
	}
	path := filepath.Join(dir, petCompanionLogName)
	if err := rejectSymlinkFile(path); err != nil {
		fmt.Fprintf(os.Stderr, "[pet-companion] refusing %s: %v\n", path, err)
		return
	}
	rotateLogIfLarge(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[pet-companion] cannot open %s: %v\n", path, err)
		return
	}
	petLogState.file = file
	fmt.Fprintf(file, "%s [pet-companion] log opened\n", time.Now().Format("2006/01/02 15:04:05"))
	fmt.Fprintf(os.Stderr, "[pet-companion] logging to %s\n", path)
}

func closePetCompanionLog() {
	petLogState.mu.Lock()
	defer petLogState.mu.Unlock()
	closePetCompanionLogLocked()
}

func closePetCompanionLogLocked() {
	if petLogState.file != nil {
		_ = petLogState.file.Close()
		petLogState.file = nil
	}
}

func petLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, file, line, ok := runtime.Caller(1)
	where := "pet"
	if ok {
		where = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}
	log.Output(2, "[pet-companion] "+msg)
	petLogState.mu.Lock()
	defer petLogState.mu.Unlock()
	if petLogState.file == nil {
		return
	}
	fmt.Fprintf(petLogState.file, "%s %s [pet-companion] %s\n", time.Now().Format("2006/01/02 15:04:05"), where, msg)
}

func petLogText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return text
}
