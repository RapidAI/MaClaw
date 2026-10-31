//go:build windows

package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestTextFileWriteLockKeyFoldsShortNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "long directory name")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	short, ok := windowsShortPath(dir)
	if !ok || strings.EqualFold(short, dir) {
		t.Skip("volume has no distinct 8.3 name")
	}
	longFile := filepath.Join(dir, "file.go")
	shortFile := filepath.Join(short, "file.go")
	before := textFileWriteLockKey(longFile)
	if got := textFileWriteLockKey(shortFile); got != before {
		t.Fatalf("short and long parent keys differ before create: long %q short %q", before, got)
	}
	if err := os.WriteFile(longFile, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := textFileWriteLockKey(longFile); after != before {
		t.Fatalf("long path key changed after create: before %q after %q", before, after)
	}
	if got := textFileWriteLockKey(shortFile); got != before {
		t.Fatalf("short path key diverged after create: long %q short %q", before, got)
	}
	shortFileName, ok := windowsShortPath(longFile)
	if !ok || strings.EqualFold(shortFileName, longFile) {
		return
	}
	if got := textFileWriteLockKey(shortFileName); got != before {
		t.Fatalf("short file name key: long %q short %q", before, got)
	}
}

func TestTextFileWriteLockKeyFoldsJunctionAndPrefixAliases(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "long directory name")
	if err := os.Mkdir(long, 0o755); err != nil {
		t.Fatal(err)
	}
	short, shortOK := windowsShortPath(long)
	link := filepath.Join(dir, "link")
	target := long
	if shortOK && !strings.EqualFold(short, long) {
		target = short
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skip("junction unavailable: " + err.Error() + " " + string(out))
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	viaLink := filepath.Join(link, "child", "file.go")
	viaLong := filepath.Join(long, "child", "file.go")
	want := textFileWriteLockKey(viaLong)
	if got := textFileWriteLockKey(viaLink); got != want {
		t.Fatalf("junction to %s did not share the long path key before create: link %q long %q", target, got, want)
	}
	if got := textFileWriteLockKey(`\\?\` + viaLink); got != want {
		t.Fatalf("extended junction key before create: extended %q long %q", got, want)
	}
	if got := textFileWriteLockKey(`\\?\` + viaLong); got != want {
		t.Fatalf("extended long key before create: extended %q long %q", got, want)
	}
	if shortOK && !strings.EqualFold(short, long) {
		viaShort := filepath.Join(short, "child", "file.go")
		if got := textFileWriteLockKey(viaShort); got != want {
			t.Fatalf("8.3 target key before create: short %q long %q", got, want)
		}
	}
	if shortLink, ok := windowsShortPath(link); ok && !strings.EqualFold(shortLink, link) {
		viaShortLink := filepath.Join(shortLink, "child", "file.go")
		if got := textFileWriteLockKey(viaShortLink); got != want {
			t.Fatalf("8.3 junction name key before create: short %q long %q", got, want)
		}
	}
	if err := os.MkdirAll(filepath.Dir(viaLong), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(viaLong, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := textFileWriteLockKey(viaLink); got != want {
		t.Fatalf("junction key changed after create: before %q after %q", want, got)
	}
	if got := textFileWriteLockKey(viaLong); got != want {
		t.Fatalf("long key changed after create: before %q after %q", want, got)
	}

	if got := textFileWriteLockKey(`\\?\` + viaLong); got != want {
		t.Fatalf("extended prefix key: extended %q plain %q", got, want)
	}
	forwardExtended := `//?/` + strings.ReplaceAll(viaLong, `\`, "/")
	if got := textFileWriteLockKey(forwardExtended); got != want {
		t.Fatalf("forward extended key: extended %q plain %q", got, want)
	}
	slashed := strings.ReplaceAll(viaLink, `\`, "/")
	if got := textFileWriteLockKey(slashed); got != want {
		t.Fatalf("forward slash junction key: slash %q plain %q", got, want)
	}
	dotted := viaLong + "."
	if err := os.WriteFile(dotted, []byte("dotted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(viaLong)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "dotted\n" {
		if got := textFileWriteLockKey(dotted); got != want {
			t.Fatalf("win32 trailing dot is the same file but a different lock: dot %q plain %q", got, want)
		}
	} else if got := textFileWriteLockKey(dotted); got == want {
		t.Fatalf("trailing dot file is distinct but shares the lock: dot %q plain %q", got, want)
	}
	if got := textFileWriteLockKey(`\\?\` + dotted); got == want {
		t.Fatalf("extended trailing-dot path collapsed onto the win32 file: %q", got)
	}
	spaced := `\\?\` + viaLong + " "
	if got := textFileWriteLockKey(spaced); got == want {
		t.Fatalf("extended trailing-space path collapsed onto the win32 file: %q", got)
	}
	extLink := `\\?\` + viaLink + "."
	extReal := `\\?\` + viaLong + "."
	extLinkKey := textFileWriteLockKey(extLink)
	extRealKey := textFileWriteLockKey(extReal)
	if extLinkKey != extRealKey {
		t.Fatalf("extended trailing-dot junction keys differ: link %q real %q", extLinkKey, extRealKey)
	}
	if extLinkKey == want {
		t.Fatalf("extended trailing-dot junction path collapsed onto the win32 file: %q", extLinkKey)
	}
	if shortOK && !strings.EqualFold(short, long) {
		extShort := `\\?\` + filepath.Join(short, "child", "file.go.")
		if got := textFileWriteLockKey(extShort); got != extRealKey {
			t.Fatalf("extended trailing-dot 8.3 parent keys differ: short %q real %q", got, extRealKey)
		}
	}
	if got, ok := stripWindowsExtendedLockPrefix(`\\?\UNC\server\share\a.go`); !ok || got != `\\server\share\a.go` {
		t.Fatalf("UNC extended prefix = %q ok %v", got, ok)
	}
	if _, ok := stripWindowsExtendedLockPrefix(`\\?\Volume{01234567-89ab-cdef-0123-456789abcdef}\a.go`); ok {
		t.Fatal("volume guid prefix was treated as a drive path")
	}

	pairs := [][2]string{
		{`C:\Documents and Settings\Public\lock-probe.go`, `C:\Users\Public\lock-probe.go`},
		{`C:\Users\All Users\lock-probe.go`, `C:\ProgramData\lock-probe.go`},
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		pairs = append(pairs, [2]string{
			filepath.Join(home, "Application Data", "lock-probe.go"),
			filepath.Join(home, "AppData", "Roaming", "lock-probe.go"),
		})
	}
	for _, pair := range pairs {
		leftInfo, leftErr := os.Lstat(filepath.Dir(pair[0]))
		rightInfo, rightErr := os.Lstat(filepath.Dir(pair[1]))
		if leftErr != nil || rightErr != nil || leftInfo == nil || rightInfo == nil {
			continue
		}
		left := textFileWriteLockKey(pair[0])
		right := textFileWriteLockKey(pair[1])
		if left != right {
			t.Fatalf("system junction keys differ: %q -> %q and %q -> %q", pair[0], left, pair[1], right)
		}
	}
}

func TestTextFileWriteLockKeyExtendedSuffixAliases(t *testing.T) {
	base := t.TempDir()
	plainParent := textFileWriteLockKey(filepath.Join(base, "x.txt"))
	dotdot := `\\?\` + base + `\foo.\..\x.txt`
	if got := textFileWriteLockKey(dotdot); got == plainParent {
		t.Fatalf("extended .. collapsed onto the parent file: %q", got)
	}
	winDotdot := `\\?\` + base + `\foo\..\x.txt`
	if got := textFileWriteLockKey(winDotdot); got == plainParent {
		t.Fatalf("extended .. without a trailing dot collapsed onto the parent file: %q", got)
	}
	slashed := `\\?\` + base + `/foo./a.txt`
	backslashed := `\\?\` + base + `\foo.\a.txt`
	if textFileWriteLockKey(slashed) == textFileWriteLockKey(backslashed) {
		t.Fatal("forward slash in an extended trailing-dot path collapsed onto that file")
	}

	foo := `\\?\` + base + `\foo.`
	if err := os.Mkdir(foo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(foo) })
	long := foo + `\long directory name`
	if err := os.Mkdir(long, 0o755); err != nil {
		t.Fatal(err)
	}
	file := long + `\a.txt`
	longKey := textFileWriteLockKey(file)
	plain := textFileWriteLockKey(filepath.Join(base, "foo", "long directory name", "a.txt"))
	if longKey == plain {
		t.Fatalf("trailing-dot directory collapsed onto the win32 directory: %q", longKey)
	}
	if err := os.WriteFile(file, []byte("L"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := textFileWriteLockKey(file); again != longKey {
		t.Fatalf("extended key changed after create: before %q after %q", longKey, again)
	}
	if short, ok := windowsShortPath(long); ok && !strings.EqualFold(short, long) {
		viaShort := short + `\a.txt`
		body, err := os.ReadFile(viaShort)
		if err != nil || string(body) != "L" {
			t.Fatalf("8.3 suffix did not read the extended file: %q err=%v body=%q", viaShort, err, string(body))
		}
		if got := textFileWriteLockKey(viaShort); got != longKey {
			t.Fatalf("extended 8.3 suffix keys differ: long %q short %q", longKey, got)
		}
	}

	other := filepath.Join(base, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	link := foo + `\jlink`
	if err := makeTestJunction(link, other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if err := os.WriteFile(link+`\z.txt`, []byte("Z"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(other, "z.txt"))
	if err != nil || string(body) != "Z" {
		t.Fatalf("junction under trailing-dot dir did not write the target: err=%v body=%q", err, string(body))
	}
	want := textFileWriteLockKey(filepath.Join(other, "z.txt"))
	if got := textFileWriteLockKey(link + `\z.txt`); got != want {
		t.Fatalf("junction under trailing-dot dir keys differ: via %q target %q", got, want)
	}
}

func makeTestJunction(link, target string) error {
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	p, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	subst := syscall.StringToUTF16(`\??\` + target)
	printName := syscall.StringToUTF16(target)
	pathU16 := append(append([]uint16{}, subst...), printName...)
	substLen := uint16((len(subst) - 1) * 2)
	printLen := uint16((len(printName) - 1) * 2)
	dataLen := uint16(8) + substLen + 2 + printLen + 2
	raw := make([]byte, 8+int(dataLen))
	*(*uint32)(unsafe.Pointer(&raw[0])) = 0xA0000003
	*(*uint16)(unsafe.Pointer(&raw[4])) = dataLen
	*(*uint16)(unsafe.Pointer(&raw[8])) = 0
	*(*uint16)(unsafe.Pointer(&raw[10])) = substLen
	*(*uint16)(unsafe.Pointer(&raw[12])) = substLen + 2
	*(*uint16)(unsafe.Pointer(&raw[14])) = printLen
	for i, u := range pathU16 {
		*(*uint16)(unsafe.Pointer(&raw[16+i*2])) = u
	}
	var ret uint32
	return syscall.DeviceIoControl(h, 0x000900A4, &raw[0], uint32(len(raw)), nil, 0, &ret, nil)
}

func windowsShortPath(path string) (string, bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) >= len(buf) {
		return "", false
	}
	return syscall.UTF16ToString(buf[:n]), true
}
