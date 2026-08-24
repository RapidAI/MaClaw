package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdaptWindowsUnixInspectCommandRewritesFileAndLs(t *testing.T) {
	got, ok := adaptWindowsUnixInspectCommand(`ls -lh "C:\Users\me\app\build\snake.exe" && file "C:\Users\me\app\build\snake.exe"`)
	if !ok {
		t.Fatal("inspect-only ls/file must be rewritten on Windows")
	}
	if strings.Contains(got, "file ") || strings.Contains(got, "ls -lh") {
		t.Fatalf("adapted command still has Unix inspect tools: %q", got)
	}
	if !strings.Contains(got, "Get-Item") || !strings.Contains(got, `C:\Users\me\app\build\snake.exe`) {
		t.Fatalf("adapted command should Get-Item the exe, got %q", got)
	}
	if strings.Contains(got, "$ErrorActionPreference") {
		t.Fatalf("inspect rewrite must not change global ErrorActionPreference: %q", got)
	}
	if strings.Count(got, "Get-Item") != 1 {
		t.Fatalf("same path should be inspected once, got %q", got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`file "C:\repo\build\snake.exe" "C:\repo\README.md"`)
	if !ok || strings.Count(got, "Get-Item") != 2 || !strings.Contains(got, "README.md") {
		t.Fatalf("file of two paths should inspect both, got ok=%v %q", ok, got)
	}
}

func TestAdaptWindowsUnixInspectCommandLeavesBuildCommands(t *testing.T) {
	if _, ok := adaptWindowsUnixInspectCommand(`cmake --build build && file snake.exe`); ok {
		t.Fatal("mixed build+inspect must not be rewritten")
	}
	if _, ok := adaptWindowsUnixInspectCommand(`g++ --version; echo not-a-separator`); ok {
		t.Fatal("non-inspect compounds must not be rewritten")
	}
}

func TestAdaptWindowsUnixInspectCommandSkipsStatFormatAndDirFlags(t *testing.T) {
	got, ok := adaptWindowsUnixInspectCommand(`stat -c %s "C:\repo\build\snake.exe"`)
	if !ok {
		t.Fatal("stat format of an exe must be rewritten")
	}
	if strings.Contains(got, "'%s'") || strings.Contains(got, `"%s"`) {
		t.Fatalf("stat format token must not become the path: %q", got)
	}
	if !strings.Contains(got, `C:\repo\build\snake.exe`) || !strings.Contains(got, "Get-Item") {
		t.Fatalf("stat should Get-Item the exe, got %q", got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`dir /s "C:\repo\build\snake.exe" && file "C:\repo\build\snake.exe"`)
	if !ok {
		t.Fatal("dir /s && file must be rewritten")
	}
	if strings.Contains(got, `'/s'`) || strings.Contains(got, `"/s"`) {
		t.Fatalf("dir switch must not become the path: %q", got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`file "C:\repo\build\snake.exe" | head`)
	if !ok || !strings.Contains(got, "Get-Item") || !strings.Contains(got, `C:\repo\build\snake.exe`) {
		t.Fatalf("file|head must be rewritten, got ok=%v %q", ok, got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`pwd && ls -lh "C:\repo\build\snake.exe"`)
	if !ok {
		t.Fatal("pwd && ls -lh must be rewritten")
	}
	if !strings.Contains(got, "Get-Item") || !strings.Contains(got, `C:\repo\build\snake.exe`) {
		t.Fatalf("pwd+ls should Get-Item the exe, got %q", got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`ls /usr`)
	if !ok || !strings.Contains(got, "/usr") {
		t.Fatalf("Unix root path must stay a path, got ok=%v %q", ok, got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`ls build\*.exe`)
	if !ok || !strings.Contains(got, "-Path ") || strings.Contains(got, "-LiteralPath 'build\\*.exe'") {
		t.Fatalf("glob inspect should use -Path, got ok=%v %q", ok, got)
	}

	got, ok = adaptWindowsUnixInspectCommand(`cd missing-build && file snake.exe`)
	if !ok || !strings.Contains(got, "Set-Location") || strings.Count(got, "does not exist") < 2 {
		t.Fatalf("cd must guard the target before inspecting, got ok=%v %q", ok, got)
	}
	if !strings.Contains(got, "$null -eq $dest") {
		t.Fatalf("cd must reject a null Get-Item result, got %q", got)
	}
	if !strings.Contains(got, "not a directory") {
		t.Fatalf("cd must reject a file target, got %q", got)
	}
	if !strings.Contains(got, "PSIsContainer") {
		t.Fatalf("directory inspect must list children, got %q", got)
	}
}

func TestExecuteCodingBashWindowsRewritesUnixFileInspect(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell inspect rewrite")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "snake.exe")
	if err := os.WriteFile(target, []byte("mz"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := executeCodingBash(map[string]interface{}{
		"command": `ls -lh "` + target + `" && file "` + target + `"`,
	}, nil)
	if result.Kind != codingCommandResultOK {
		t.Fatalf("rewritten unix inspect should succeed, got %#v", result)
	}
	if strings.Contains(result.Text, "无法将") || strings.Contains(strings.ToLower(result.Text), "not recognized") {
		t.Fatalf("rewritten inspect leaked a PowerShell missing-command error: %q", result.Text)
	}
	if !strings.Contains(result.Text, "snake.exe") || !strings.Contains(result.Text, "bytes") {
		t.Fatalf("rewritten inspect should describe the file, got %q", result.Text)
	}
	shell, args := windowsCodingBashInvocation(`ls -lh "` + target + `" && file "` + target + `"`)
	if !strings.Contains(shell, "powershell") || len(args) < 4 || !strings.Contains(args[len(args)-1], "Get-Item") {
		t.Fatalf("windows invocation should run adapted Get-Item, shell=%q args=%q", shell, args)
	}

	listed := filepath.Join(dir, "outdir")
	if err := os.Mkdir(listed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(listed, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	listResult := executeCodingBash(map[string]interface{}{
		"command": `ls -lh "` + listed + `"`,
	}, nil)
	if listResult.Kind != codingCommandResultOK || !strings.Contains(listResult.Text, "hello.txt") {
		t.Fatalf("ls of a directory should list children, got %#v", listResult)
	}

	missing := filepath.Join(dir, "no-such-build")
	wrongCwd := executeCodingBash(map[string]interface{}{
		"command":     `cd "` + missing + `" && file snake.exe`,
		"working_dir": dir,
	}, nil)
	if wrongCwd.Kind == codingCommandResultOK {
		t.Fatalf("cd into a missing dir must not inspect cwd snake.exe, got %#v", wrongCwd)
	}
	if !strings.Contains(strings.ToLower(wrongCwd.Text), "does not exist") {
		t.Fatalf("missing cd target should report does not exist, got %q", wrongCwd.Text)
	}

	notDir := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	cdFile := executeCodingBash(map[string]interface{}{
		"command":     `cd "` + notDir + `" && ls`,
		"working_dir": dir,
	}, nil)
	if cdFile.Kind == codingCommandResultOK {
		t.Fatalf("cd onto a file must not list cwd, got %#v", cdFile)
	}
	if strings.Contains(cdFile.Text, "secret.txt") {
		t.Fatalf("cd onto a file leaked cwd listing: %q", cdFile.Text)
	}
	if !strings.Contains(strings.ToLower(cdFile.Text), "not a directory") {
		t.Fatalf("cd onto a file should report not a directory, got %q", cdFile.Text)
	}
}

func TestWindowsInspectParameterErrorIsSoft(t *testing.T) {
	failed := unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `ls -lh "C:\\repo\\build\\snake.exe" && file "C:\\repo\\build\\snake.exe"`,
		Succeeded: false,
		Summary:   "PowerShell exception: 找不到与参数名称“lh”匹配的参数。",
	}})
	if len(failed) != 0 {
		t.Fatalf("ls -lh parameter noise must be a soft inspect failure, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `dir "C:\\repo\\build" /s /b`,
		Succeeded: false,
		Summary:   "PowerShell exception: 找不到接受实际参数“/b”的位置形式参数。",
	}})
	if len(failed) != 0 {
		t.Fatalf("dir positional-parameter noise must be a soft inspect failure, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `cmake --build build /b`,
		Succeeded: false,
		Summary:   "PowerShell exception: 找不到接受实际参数“/b”的位置形式参数。",
	}})
	if len(failed) != 1 {
		t.Fatalf("cmake positional-parameter noise must stay hard, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `cd missing-build && file snake.exe`,
		Succeeded: false,
		Summary:   "Cannot find path 'missing-build' because it does not exist.",
	}})
	if len(failed) != 0 {
		t.Fatalf("cd+file missing-path inspect must be soft, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `dir missing-build`,
		Succeeded: false,
		Summary:   "Cannot find path 'missing-build' because it does not exist.",
	}})
	if len(failed) != 0 {
		t.Fatalf("dir missing-path inspect must be soft, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `ls missing-build`,
		Succeeded: false,
		Summary:   "Get-ChildItem : 找不到路径“C:\\repo\\missing-build”。",
	}})
	if len(failed) != 0 {
		t.Fatalf("Chinese missing-path ls must be soft, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `cd missing-build && cmake --build build`,
		Succeeded: false,
		Summary:   "Cannot find path 'missing-build' because it does not exist.",
	}})
	if len(failed) != 1 {
		t.Fatalf("cd+cmake must stay a hard failure, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `cmake --build missing-build`,
		Succeeded: false,
		Summary:   "Get-ChildItem : 找不到路径“C:\\repo\\missing-build”。",
	}})
	if len(failed) != 1 {
		t.Fatalf("cmake missing-path must stay hard, got %#v", failed)
	}
}

func TestAdaptWindowsPythonInspectCommandRewritesListDir(t *testing.T) {
	got, ok := adaptWindowsPythonInspectCommand(`python3 -c "import os; print(os.listdir(r'C:\\Users\\me\\prog-test'))"`)
	if !ok {
		t.Fatal("inspect-only python listdir must be rewritten on Windows")
	}
	if strings.Contains(got, "python3") || strings.Contains(got, "os.listdir") {
		t.Fatalf("adapted command still invokes python: %q", got)
	}
	if !strings.Contains(got, "Get-Item") || !strings.Contains(got, `C:\Users\me\prog-test`) {
		t.Fatalf("adapted command should Get-Item the listed path, got %q", got)
	}

	got, ok = adaptWindowsPythonInspectCommand(`python -c "import os; print(os.listdir())"`)
	if !ok || !strings.Contains(got, "Get-ChildItem") {
		t.Fatalf("listdir() should list cwd, got ok=%v %q", ok, got)
	}

	got, ok = adaptWindowsPythonInspectCommand(`py -3 -c "import os; print(os.listdir('.'))"`)
	if !ok || !strings.Contains(got, "Get-ChildItem") {
		t.Fatalf("py -3 listdir('.') should list cwd, got ok=%v %q", ok, got)
	}
}

func TestAdaptWindowsPythonInspectCommandLeavesMutatingPython(t *testing.T) {
	if _, ok := adaptWindowsPythonInspectCommand(`python3 -c "open('x.txt','w').write('hi')"`); ok {
		t.Fatal("python write script must not be rewritten")
	}
	if _, ok := adaptWindowsPythonInspectCommand(`python3 main.py`); ok {
		t.Fatal("python script run must not be rewritten")
	}
	if _, ok := adaptWindowsPythonInspectCommand(`python3 -m py_compile app.py`); ok {
		t.Fatal("py_compile must not be rewritten")
	}
	if _, ok := adaptWindowsPythonInspectCommand(`cmake --build build && python3 -c "import os; print(os.listdir())"`); ok {
		t.Fatal("mixed build+python inspect must not be rewritten")
	}
}

func TestWindowsPythonInspectArchitectureErrorIsSoft(t *testing.T) {
	cmd := `python3 -c "import os; print(os.listdir(r'C:\\Users\\ma139\\Desktop\\prog-test'))"`
	summary := `failed: [ERROR] Executable C:\Users\ma139\AppData\Local\Python\pythoncore-3.14-64\python.exe is for a different kind of processor architecture. (1 issue(s))`
	if !subAgentWindowsPythonInspectOnlyCommand(cmd) {
		t.Fatal("listdir probe must classify as python inspect-only")
	}
	failed := unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command: cmd, Succeeded: false, Summary: summary,
	}})
	if len(failed) != 0 {
		t.Fatalf("pythoncore architecture mismatch on listdir must be soft, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `python3 -m pytest tests`,
		Succeeded: false,
		Summary:   summary,
	}})
	if len(failed) != 1 {
		t.Fatalf("pytest architecture mismatch must stay hard, got %#v", failed)
	}

	failed = unresolvedFailedSubAgentCommands([]CodingSubAgentCommandResult{{
		Command:   `python3 main.py`,
		Succeeded: false,
		Summary:   summary,
	}})
	if len(failed) != 1 {
		t.Fatalf("python main.py architecture mismatch must stay hard, got %#v", failed)
	}
}

func TestExecuteCodingBashWindowsRewritesPythonListDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell python inspect rewrite")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := executeCodingBash(map[string]interface{}{
		"command": `python3 -c "import os; print(os.listdir(r'` + strings.ReplaceAll(dir, `\`, `\\`) + `'))"`,
	}, nil)
	if result.Kind != codingCommandResultOK {
		t.Fatalf("rewritten python listdir should succeed, got %#v", result)
	}
	if strings.Contains(result.Text, "processor architecture") || strings.Contains(result.Text, "pythoncore") {
		t.Fatalf("rewritten inspect leaked a python architecture error: %q", result.Text)
	}
	if !strings.Contains(result.Text, "hello.txt") {
		t.Fatalf("rewritten python listdir should list children, got %q", result.Text)
	}
	shell, args := windowsCodingBashInvocation(`python3 -c "import os; print(os.listdir(r'` + strings.ReplaceAll(dir, `\`, `\\`) + `'))"`)
	if !strings.Contains(shell, "powershell") || len(args) < 4 || !strings.Contains(args[len(args)-1], "Get-Item") {
		t.Fatalf("windows invocation should run adapted Get-Item, shell=%q args=%q", shell, args)
	}
}

func TestWindowsExecutableMatchesProcessArch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PE machine check")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !windowsExecutableMatchesProcessArch(exe) {
		t.Fatalf("current process image must match GOARCH, exe=%s", exe)
	}
	if windowsCodingPythonUsable(`C:\Users\me\AppData\Local\Microsoft\WindowsApps\python3.exe`) {
		t.Fatal("WindowsApps python3 stub must not count as usable")
	}
}

func TestReplaceWindowsPython3CommandTokens(t *testing.T) {
	got := replaceWindowsPython3Command(`python3 -m pytest tests`, "python")
	if got != `python -m pytest tests` {
		t.Fatalf("line-start python3: %q", got)
	}
	got = replaceWindowsPython3Command(`cmake --build build && python3 -m pytest`, "py")
	if got != `cmake --build build && py -m pytest` {
		t.Fatalf("operator-prefixed python3: %q", got)
	}
	got = replaceWindowsPython3Command(`g++ main.cpp ; python3.exe app.py`, "python")
	if got != `g++ main.cpp ; python app.py` {
		t.Fatalf("python3.exe token: %q", got)
	}
	kept := `dir C:\python3\bin && type notes.txt`
	if got = replaceWindowsPython3Command(kept, "python"); got != kept {
		t.Fatalf("path segment must stay, got %q", got)
	}
}

func TestAdaptWindowsPythonInspectCommandRewritesUtf8DashC(t *testing.T) {
	got, ok := adaptWindowsPythonInspectCommand(`python3 -X utf8 -c "import os; print(os.listdir(r'C:\\repo'))"`)
	if !ok || !strings.Contains(got, `C:\repo`) || strings.Contains(got, "python3") {
		t.Fatalf("python -X utf8 -c listdir must rewrite, got ok=%v %q", ok, got)
	}
	if _, ok := adaptWindowsPythonInspectCommand(`python3 -m py_compile -c "import os; print(os.listdir())"`); ok {
		t.Fatal("-m must not be treated as listdir inspect")
	}
}

func TestPythonInspectListPathsKeepsRawPrefixOnlyBeforeQuote(t *testing.T) {
	got := pythonInspectListPaths(`print(os.listdir(r'C:\\Users\\me\\app'))`)
	if len(got) != 1 || got[0] != `C:\Users\me\app` {
		t.Fatalf("raw quoted path: %#v", got)
	}
	if paths := pythonInspectListPaths(`print(os.listdir(root))`); len(paths) != 0 {
		t.Fatalf("listdir(root) must not strip leading r, got %#v", paths)
	}
}

func TestReplaceWindowsPython3CommandOperatorsAndQuotes(t *testing.T) {
	got := replaceWindowsPython3Command("build.bat || python3 app.py", "python")
	if got != "build.bat || python app.py" {
		t.Fatalf("|| must stay a unit, got %q", got)
	}
	got = replaceWindowsPython3Command("echo a\npython3 -m pytest", "python")
	if got != "echo a\npython -m pytest" {
		t.Fatalf("newline python3: %q", got)
	}
	got = replaceWindowsPython3Command(`cmd /c "python3 -m pytest"`, "python")
	if got != `cmd /c "python -m pytest"` {
		t.Fatalf("quoted python3: %q", got)
	}
}

func TestAdaptWindowsPythonInspectCommandRejectsUnknownListDirTarget(t *testing.T) {
	if _, ok := adaptWindowsPythonInspectCommand(`python3 -c "import os; print(os.listdir(some_dir))"`); ok {
		t.Fatal("listdir of an unresolved name must not rewrite to cwd")
	}
	got, ok := adaptWindowsPythonInspectCommand(`python3 -c "from pathlib import Path; print(list(Path(r'C:\\repo').iterdir()))"`)
	if !ok || !strings.Contains(got, `C:\repo`) || strings.Contains(got, "python3") {
		t.Fatalf("Path(...).iterdir must rewrite to that path, got ok=%v %q", ok, got)
	}
}

func TestPythonInspectListPathsIgnoresAbspathSuffix(t *testing.T) {
	got := pythonInspectListPaths(`print(os.listdir(some_dir)); print(os.path.abspath(r'C:\\other'))`)
	if len(got) != 0 {
		t.Fatalf("os.path.abspath must not be treated as an inspect target, got %#v", got)
	}
	if _, ok := adaptWindowsPythonInspectCommand(`python3 -c "import os; print(os.listdir(d)); print(os.path.abspath(r'C:\\other'))"`); ok {
		t.Fatal("listdir of an unresolved name plus abspath must not rewrite")
	}
}
