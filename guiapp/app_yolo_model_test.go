package guiapp

import "testing"

func TestYOLODownloadState(t *testing.T) {
	yoloStateMu.Lock()
	prevRun, prevFail, prevStop := yoloRunning, yoloFailedMsg, yoloUserStopped
	yoloRunning, yoloFailedMsg, yoloUserStopped = false, "", false
	yoloStateMu.Unlock()
	t.Cleanup(func() {
		yoloStateMu.Lock()
		yoloRunning, yoloFailedMsg, yoloUserStopped = prevRun, prevFail, prevStop
		yoloStateMu.Unlock()
	})

	running, failed := yoloDownloadStatus()
	if running || failed != "" {
		t.Fatalf("fresh state running=%v failed=%q", running, failed)
	}
	yoloNoteDownloadStarted()
	running, failed = yoloDownloadStatus()
	if !running || failed != "" {
		t.Fatalf("started running=%v failed=%q", running, failed)
	}
	yoloNoteDownloadFailed(errString("github timeout"))
	running, failed = yoloDownloadStatus()
	if running || failed != "github timeout" {
		t.Fatalf("failed running=%v failed=%q", running, failed)
	}
	yoloNoteDownloadStarted()
	_, failed = yoloDownloadStatus()
	if failed != "" {
		t.Fatalf("a new attempt must clear the previous failure, got %q", failed)
	}
	yoloNoteDownloadFinished()
	running, failed = yoloDownloadStatus()
	if running || failed != "" {
		t.Fatalf("finished running=%v failed=%q", running, failed)
	}
	if !yoloAllowEnableAfterDownload() {
		t.Fatal("enable stays allowed until the user turns parsing off during a download")
	}
	yoloStateMu.Lock()
	yoloRunning = true
	yoloStateMu.Unlock()
	yoloStateMu.Lock()
	if yoloRunning {
		yoloUserStopped = true
	}
	yoloRunning = false
	yoloStateMu.Unlock()
	if yoloAllowEnableAfterDownload() {
		t.Fatal("turning parsing off during the download must keep it off")
	}
}

func TestCheckYOLOModelKeepsLiveDownloadState(t *testing.T) {
	yoloStateMu.Lock()
	prevRun, prevFail, prevStop := yoloRunning, yoloFailedMsg, yoloUserStopped
	yoloRunning, yoloFailedMsg, yoloUserStopped = false, "", false
	yoloStateMu.Unlock()
	yoloModelStatusCache.Lock()
	prevValid := yoloModelStatusCache.valid
	prevPath := yoloModelStatusCache.path
	prevExists := yoloModelStatusCache.exists
	prevSize := yoloModelStatusCache.size
	yoloModelStatusCache.valid = false
	yoloModelStatusCache.Unlock()
	t.Cleanup(func() {
		yoloStateMu.Lock()
		yoloRunning, yoloFailedMsg, yoloUserStopped = prevRun, prevFail, prevStop
		yoloStateMu.Unlock()
		yoloModelStatusCache.Lock()
		yoloModelStatusCache.valid = prevValid
		yoloModelStatusCache.path = prevPath
		yoloModelStatusCache.exists = prevExists
		yoloModelStatusCache.size = prevSize
		yoloModelStatusCache.Unlock()
	})

	first := (&App{}).CheckYOLOModel()
	if first["path"] == "" {
		t.Fatal("model path is empty; the cache hit was not exercised")
	}
	yoloModelStatusCache.Lock()
	yoloModelStatusCache.valid = true
	yoloModelStatusCache.exists = false
	yoloModelStatusCache.size = 1
	yoloModelStatusCache.Unlock()
	yoloNoteDownloadStarted()
	second := (&App{}).CheckYOLOModel()
	if second["exists"] != false || second["size"] != int64(1) {
		t.Fatalf("expected the cached file snapshot, got %#v", second)
	}
	if second["downloading"] != true || second["error"] != "" {
		t.Fatalf("cached status dropped an in-progress download: %#v", second)
	}
	yoloNoteDownloadFailed(errString("github timeout"))
	third := (&App{}).CheckYOLOModel()
	if third["downloading"] != false || third["error"] != "github timeout" {
		t.Fatalf("cached status dropped the failure: %#v", third)
	}
}

func TestYOLOMissingFileIsNotCached(t *testing.T) {
	yoloModelStatusCache.Lock()
	prevValid := yoloModelStatusCache.valid
	prevPath := yoloModelStatusCache.path
	prevExists := yoloModelStatusCache.exists
	prevSize := yoloModelStatusCache.size
	yoloModelStatusCache.valid = true
	yoloModelStatusCache.path = "omniparser-v2.yolow"
	yoloModelStatusCache.exists = true
	yoloModelStatusCache.size = 10
	yoloModelStatusCache.Unlock()
	t.Cleanup(func() {
		yoloModelStatusCache.Lock()
		yoloModelStatusCache.valid = prevValid
		yoloModelStatusCache.path = prevPath
		yoloModelStatusCache.exists = prevExists
		yoloModelStatusCache.size = prevSize
		yoloModelStatusCache.Unlock()
	})

	yoloRememberFile("omniparser-v2.yolow", false, 0)
	yoloModelStatusCache.Lock()
	missingValid := yoloModelStatusCache.valid
	yoloModelStatusCache.Unlock()
	if missingValid {
		t.Fatal("a missing weight file must not stay cached")
	}
	yoloRememberFile("omniparser-v2.yolow", true, 42)
	yoloModelStatusCache.Lock()
	defer yoloModelStatusCache.Unlock()
	if !yoloModelStatusCache.valid || yoloModelStatusCache.size != 42 {
		t.Fatalf("present file cache = valid %v size %d", yoloModelStatusCache.valid, yoloModelStatusCache.size)
	}
}

func TestYOLOSettledProgressCarriesParsingState(t *testing.T) {
	on := yoloSettledProgress(true)
	if on["percent"] != 100 || on["screen_parsing_enabled"] != true || on["error"] != "" {
		t.Fatalf("enabled settle: %#v", on)
	}
	off := yoloSettledProgress(false)
	if off["screen_parsing_enabled"] != false {
		t.Fatalf("disabled settle: %#v", off)
	}
}

func TestYOLOAutoDownloadPolicy(t *testing.T) {
	download, enable := yoloAutoDownloadPolicy(false, true)
	if !download || enable {
		t.Fatalf("missing weights with parsing already on: download=%v enable=%v", download, enable)
	}
	download, enable = yoloAutoDownloadPolicy(false, false)
	if !download || !enable {
		t.Fatalf("missing weights must download and enable: download=%v enable=%v", download, enable)
	}
	download, enable = yoloAutoDownloadPolicy(true, false)
	if download || enable {
		t.Fatalf("existing weights left off must stay off: download=%v enable=%v", download, enable)
	}
	download, enable = yoloAutoDownloadPolicy(true, true)
	if download || enable {
		t.Fatalf("existing weights already on need nothing: download=%v enable=%v", download, enable)
	}
}
