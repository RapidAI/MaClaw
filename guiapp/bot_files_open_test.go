package guiapp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeliveredFileOpensByItsName(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "weather-report.pdf")
	newerDir := filepath.Join(dir, "later")
	if err := os.MkdirAll(newerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	newer := filepath.Join(newerDir, "weather-report.pdf")
	if err := os.WriteFile(older, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := map[string]string{
		"bf_old":                        older,
		"name:weather-report.pdf":       newer,
		"name:bot_4:weather-report.pdf": older,
		"name:bot_3:weather-report.pdf": newer,
		"name:other.txt":                filepath.Join(dir, "missing.txt"),
	}
	cloud := "/home/desktop/work/main/run/weather-report.pdf"
	if got := deliveredFileNamed("", cloud, saved); got != newer {
		t.Fatalf("opened %s", got)
	}
	if deliveredFileNamed("", "/home/desktop/work/main/run/", saved) != "" {
		t.Fatal("a directory path named a file")
	}
	if deliveredFileNamed("", "notes.txt", saved) != "" {
		t.Fatal("an unsaved name resolved")
	}
	if deliveredFileNamed("bot_4", cloud, saved) != older {
		t.Fatal("bot 4 opened another bot's file")
	}
	if deliveredFileNamed("bot_3", cloud, saved) != newer {
		t.Fatal("bot 3 opened the wrong copy")
	}
	if deliveredFileNamed("bot_9", cloud, saved) != "" {
		t.Fatal("a bot with no copy opened someone else's file")
	}
}

func TestDeliveryReplacesKeepsTheNewerFile(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "older.pdf")
	newer := filepath.Join(dir, "newer.pdf")
	if err := os.WriteFile(older, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !deliveryReplaces(older, newer, false) {
		t.Fatal("a newer file did not replace the old one")
	}
	if deliveryReplaces(newer, older, false) {
		t.Fatal("an older file replaced the newer one")
	}
	if !deliveryReplaces(newer, older, true) {
		t.Fatal("the file just saved lost to a touched copy")
	}
	if deliveryReplaces(newer, newer, true) {
		t.Fatal("the same file was written again")
	}
	if !deliveryReplaces("", newer, false) {
		t.Fatal("the first copy was not recorded")
	}
	if deliveryReplaces(newer, filepath.Join(dir, "missing.pdf"), true) {
		t.Fatal("a missing file replaced a real one")
	}
	same := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, same, same); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, same, same); err != nil {
		t.Fatal(err)
	}
	if deliveryReplaces(older, newer, false) {
		t.Fatal("a refresh replaced a file saved at the same time")
	}
	if !deliveryReplaces(older, newer, true) {
		t.Fatal("the file just saved did not become the one that opens")
	}
}

func TestCloudDesktopOpenPath(t *testing.T) {
	if !cloudDesktopOpenPath("/home/desktop/work/main/run/weather-report.pdf") {
		t.Fatal("container path")
	}
	win := filepath.Join(`C:\Users\ma139\MaClaw`, "Bot 4", "weather-report.pdf")
	if cloudDesktopOpenPath(win) {
		t.Fatal("a drive path is this computer")
	}
	if cloudDesktopOpenPath(`D:\notes.txt`) {
		t.Fatal("another drive")
	}
	if cloudDesktopOpenPath("/home/ma139/notes.txt") {
		t.Fatal("another home directory")
	}
	if !cloudDesktopOpenPath("/tmp/weather-report.pdf") {
		t.Fatal("tmp")
	}
	if !cloudDesktopOpenPath("~/work/weather-report.pdf") {
		t.Fatal("home shorthand")
	}
}
