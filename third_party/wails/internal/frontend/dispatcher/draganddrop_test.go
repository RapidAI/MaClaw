package dispatcher

import (
	"testing"

	"github.com/wailsapp/wails/v2/internal/frontend"
)

type recordEvents struct {
	name string
	args []interface{}
}

func (r *recordEvents) Emit(name string, data ...interface{}) {
	r.name = name
	r.args = append([]interface{}(nil), data...)
}

func (r *recordEvents) On(string, func(...interface{})) func() { return func() {} }
func (r *recordEvents) OnMultiple(string, func(...interface{}), int) func() {
	return func() {}
}
func (r *recordEvents) Once(string, func(...interface{})) func()         { return func() {} }
func (r *recordEvents) Off(string)                                       {}
func (r *recordEvents) OffAll()                                          {}
func (r *recordEvents) Notify(frontend.Frontend, string, ...interface{}) {}

func TestProcessDragAndDropKeepsFractionalCSSPixels(t *testing.T) {
	events := &recordEvents{}
	d := &Dispatcher{events: events}
	// 12.5 rounds to 13. The drive-letter colon stays in the path.
	_, err := d.processDragAndDropMessage("DD:12.5:40.2:C:\\notes\\a.pdf\nD:\\b.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if events.name != "wails:file-drop" {
		t.Fatalf("event = %q", events.name)
	}
	if len(events.args) != 3 {
		t.Fatalf("args = %#v", events.args)
	}
	if events.args[0] != 13 || events.args[1] != 40 {
		t.Fatalf("point = %v,%v", events.args[0], events.args[1])
	}
	paths, ok := events.args[2].([]string)
	if !ok || len(paths) != 2 || paths[0] != `C:\notes\a.pdf` || paths[1] != `D:\b.pdf` {
		t.Fatalf("paths = %#v", events.args[2])
	}

	if _, err := d.processDragAndDropMessage("DD:1:2:/tmp/a.pdf"); err != nil {
		t.Fatal(err)
	}
	if events.args[0] != 1 || events.args[1] != 2 {
		t.Fatalf("integer point = %v,%v", events.args[0], events.args[1])
	}
	if _, err := d.processDragAndDropMessage("DD:nope:2:/tmp/a.pdf"); err == nil {
		t.Fatal("non-numeric x was accepted")
	}
}
