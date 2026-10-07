package guiapp

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPetCompanionLogStaysSeparateWhenDetailIsOff(t *testing.T) {
	dir := t.TempDir()
	setLogDetailForTest(t, false)
	previous := log.Writer()
	previousFlags := log.Flags()
	var main bytes.Buffer
	log.SetOutput(&detailAwareLogWriter{file: &main, stderr: nil})
	log.SetFlags(0)
	t.Cleanup(func() {
		closePetCompanionLog()
		log.SetOutput(previous)
		log.SetFlags(previousFlags)
	})

	openPetCompanionLog(dir)
	petLogf("listening for wake word")

	mainText := main.String()
	if strings.Contains(mainText, "listening for wake word") {
		t.Fatalf("pet line landed in the main log: %q", mainText)
	}
	body, err := os.ReadFile(filepath.Join(dir, petCompanionLogName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "[pet-companion] listening for wake word") {
		t.Fatalf("pet log missing the line: %s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "maclaw.log")); !os.IsNotExist(err) {
		t.Fatal("pet log created maclaw.log")
	}
}

func TestPetCompanionLogRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, petCompanionLogName)
	if err := os.Symlink(filepath.Join(dir, "elsewhere.log"), path); err != nil {
		t.Skip("symlink not available")
	}
	openPetCompanionLog(dir)
	t.Cleanup(closePetCompanionLog)
	petLogf("should not follow the link")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced")
	}
	target, err := os.ReadFile(filepath.Join(dir, "elsewhere.log"))
	if err == nil && strings.Contains(string(target), "should not follow") {
		t.Fatal("pet log followed a symlink")
	}
}
