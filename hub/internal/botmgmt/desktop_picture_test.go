package botmgmt

import "testing"

func TestLiveDesktopPictureDropsAStoppedFrame(t *testing.T) {
	url, state := liveDesktopPicture("http://hub/stale/vnc.html", DesktopStateDisplayStopped)
	if url != "" || state != DesktopStateDisplayStopped {
		t.Fatalf("display stopped url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("http://hub/stale/vnc.html", DesktopStateAsleep)
	if url != "" || state != DesktopStateAsleep {
		t.Fatalf("asleep url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("http://hub/live/vnc.html", DesktopStateRunning)
	if url != "http://hub/live/vnc.html" || state != DesktopStateRunning {
		t.Fatalf("running url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("", DesktopStateRunning)
	if url != "" || state != DesktopStateAsleep {
		t.Fatalf("running without a page url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("", "")
	if url != "" || state != DesktopStateAsleep {
		t.Fatalf("unknown empty url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture(" http://hub/live/vnc.html ", "")
	if url != "http://hub/live/vnc.html" || state != DesktopStateRunning {
		t.Fatalf("older watch url=%q state=%q", url, state)
	}
}
