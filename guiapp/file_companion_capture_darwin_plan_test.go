package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDarwinProcessTapPlan(t *testing.T) {
	plan := darwinProcessTapPlan()
	if plan.InitSelector != "initMonoMixdownOfProcesses:" {
		t.Fatalf("selector = %q", plan.InitSelector)
	}
	if plan.Exclusive {
		t.Fatal("include-list tap is exclusive")
	}
	if !plan.RefuseWhenEmpty {
		t.Fatal("empty process-object list is accepted")
	}
	if plan.AllowUnixPID {
		t.Fatal("unix pid is a tap process element")
	}
	if plan.TranslatePID != 0x69643270 || plan.ProcessPID != 0x70706964 || plan.ProcessList != 0x70727323 {
		t.Fatalf("fourcc translate=%#x pid=%#x list=%#x", plan.TranslatePID, plan.ProcessPID, plan.ProcessList)
	}
	if plan.ForbiddenSelector != "initStereoGlobalTapButExcludeProcesses" {
		t.Fatalf("forbidden selector = %q", plan.ForbiddenSelector)
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test file path is unknown")
	}
	srcBytes, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "file_companion_capture_darwin.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	if strings.Contains(src, "initWithProcesses:") {
		t.Fatal("capture file still names initWithProcesses:")
	}
	if strings.Contains(src, plan.ForbiddenSelector) {
		t.Fatal("capture file names the machine-wide exclude tap")
	}
	needle := `NSSelectorFromString(@"` + plan.InitSelector + `")`
	if !strings.Contains(src, needle) {
		t.Fatalf("NSSelectorFromString is not given %s", plan.InitSelector)
	}
	for _, cc := range []uint32{plan.TranslatePID, plan.ProcessList, plan.ProcessPID} {
		lit := fmt.Sprintf("0x%08x", cc)
		if !strings.Contains(strings.ToLower(src), lit) {
			t.Fatalf("capture file missing %s", lit)
		}
	}

	body := darwinTapStartBody(src)
	if body == "" {
		t.Fatal("companionStartProcessTap is missing")
	}
	procAt := strings.Index(body, "companionProcessObjectIDs(")
	if procAt < 0 {
		t.Fatal("tap start does not resolve HAL process objects")
	}
	if i := strings.Index(body, "instancesRespondToSelector:"); i >= 0 && i < procAt {
		t.Fatal("instancesRespondToSelector returns before process objects are resolved")
	}
	if !strings.Contains(body, "cls == Nil") {
		t.Fatal("a missing CATapDescription class does not refuse")
	}
	if !strings.Contains(body, "processes == nil || processes.count == 0") {
		t.Fatal("empty process-object list does not refuse")
	}
	if strings.Contains(body, "@(pid)") {
		t.Fatal("tap start passes the unix pid")
	}
	if !strings.Contains(body, `setExclusive:"`) || !strings.Contains(body, "NO") {
		t.Fatal("include-list tap does not set exclusive off")
	}
	selAt := strings.Index(body, needle)
	if selAt < 0 || selAt < procAt {
		t.Fatal("initializer is chosen before process objects are resolved")
	}
}

func darwinTapStartBody(src string) string {
	const start = "void *companionStartProcessTap"
	const end = "void companionFinishProcessTap"
	i := strings.Index(src, start)
	j := strings.Index(src, end)
	if i < 0 || j < i {
		return ""
	}
	return src[i:j]
}
