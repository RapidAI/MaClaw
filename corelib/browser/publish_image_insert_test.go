package browser

import (
	"strings"
	"testing"
	"time"
)

func TestImageInsertWaitsUntilThePictureStays(t *testing.T) {
	rose := time.Now()
	ready, err := imageInsertReady(0, 1, rose, 0, 0, 0, 0, rose.Add(100*time.Millisecond))
	if err != nil || ready {
		t.Fatalf("a picture that just appeared is not inserted yet: ready=%v err=%v", ready, err)
	}
	ready, err = imageInsertReady(0, 0, time.Time{}, 0, 0, 0, 0, rose.Add(time.Second))
	if err != nil || ready {
		t.Fatalf("a picture that vanished is not inserted: ready=%v err=%v", ready, err)
	}
	ready, err = imageInsertReady(2, 3, rose, 0, 0, 0, 0, rose.Add(time.Second))
	if err != nil || !ready {
		t.Fatalf("a picture that stayed, with no upload request, is inserted: ready=%v err=%v", ready, err)
	}
	ready, err = imageInsertReady(0, 1, rose, 1, 0, 1, 0, rose.Add(time.Second))
	if err != nil || ready {
		t.Fatalf("an upload still open is not done: ready=%v err=%v", ready, err)
	}
	ready, err = imageInsertReady(0, 1, rose, 1, 1, 0, 0, rose.Add(time.Second))
	if err != nil || !ready {
		t.Fatalf("a finished upload is done: ready=%v err=%v", ready, err)
	}
	if _, err := imageInsertReady(0, 1, rose, 1, 0, 0, 1, rose.Add(time.Second)); err == nil {
		t.Fatal("a failed upload was treated as inserted")
	}
}

func TestImageDeliveryUsesADropTheEditorCanRead(t *testing.T) {
	if !strings.Contains(publishImageDeliverFn, "DragEvent") || !strings.Contains(publishImageDeliverFn, "'drop'") {
		t.Fatal("picture delivery does not drop the file onto the editor")
	}
	if strings.Contains(publishImageDeliverFn, "new ClipboardEvent('paste'") {
		t.Fatal("picture delivery still depends on a ClipboardEvent clipboardData the browser drops")
	}
}
