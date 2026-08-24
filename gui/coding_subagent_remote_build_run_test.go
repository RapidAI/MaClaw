package main

import (
	"strings"
	"testing"
)

// Regression from ~/.maclaw workbench: T3 ran successfully
//
//	mkdir && cmake && make && /home/.../build/sysinfo12
//
// but quality gate said "failure-suppressing shell syntax" because the absolute
// build binary after && was not treated as verification/acceptance.
func TestRemoteBuildAndRunAbsoluteBinaryIsVerification(t *testing.T) {
	cmd := "cd /home/sysinfo12/build && cmake .. 2>&1 && make 2>&1 && /home/sysinfo12/build/sysinfo12"
	if suppressesVerificationFailure(cmd) {
		t.Fatalf("build+run absolute binary must not be failure-suppressing: %q", cmd)
	}
	if isUnsafeSubAgentVerificationCommand(cmd) {
		t.Fatalf("build+run absolute binary must not be unsafe verification: %q", cmd)
	}
	if !isSubAgentVerificationCommand(cmd) {
		t.Fatalf("build+run absolute binary must count as verification: %q", cmd)
	}
	if !isSubAgentVerificationCommand("/home/sysinfo12/build/sysinfo12") {
		t.Fatal("absolute project build binary alone must count as verification")
	}
	status, summary := summarizeSubAgentVerification(
		[]string{"/home/sysinfo12/src/sysinfo.cpp"},
		[]CodingSubAgentCommandResult{
			{Command: cmd, Succeeded: true, Summary: "系统信息\nCPU\n内存\nEXIT: 0", seq: 5},
		},
		3,
	)
	if status != codingSubAgentQualityPassed {
		t.Fatalf("T3 build+run should pass verification, got (%q, %q)", status, summary)
	}

	// System binaries still must not count.
	if isSubAgentVerificationCommand("/bin/rm") || isSubAgentVerificationCommand("/usr/bin/ls") {
		t.Fatal("system absolute binaries must not count as project verification")
	}
	if isSubAgentVerificationCommand("/tmp/evil") || isSubAgentVerificationCommand("/home/user/random") {
		t.Fatal("arbitrary absolute paths without a build/dist/out/bin tree must not count")
	}
	if isSubAgentVerificationCommand(`C:\Windows\System32\cmd.exe`) {
		t.Fatal("Windows system executables must not count as project verification")
	}
	// make ; run is still extra-command style; we only green-light && chains via
	// verification segments. Absolute build binary alone is fine.
	if !isSubAgentVerificationCommand("/opt/myapp/build/app") {
		t.Fatal("opt project build binary should count")
	}
}

// Live local hello-world (2026-08-20): g++ compiled and printed Hello, World!,
// but quality said "verification not run" + "2 command(s) failed" because the
// PATH-prefixed compile+run was treated as failure-suppressing and the
// where.exe inventory (clang++ missing) stayed unresolved.
func TestLocalMinGWHelloWorldCompileRunPassesVerification(t *testing.T) {
	probe := `where.exe g++ 2>$null; where.exe gcc 2>$null; where.exe clang++ 2>$null; $env:PATH -split ';' | Select-String -Pattern '(gcc|clang|g\+\+|bin)' | Select-Object -First 10`
	compile := `$env:PATH = 'C:\Users\ma139\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin;' + $env:PATH; g++ -std=c++17 -o hello.exe hello.cpp && ./hello.exe`
	if suppressesVerificationFailure(compile) {
		t.Fatalf("PATH-prefixed g++ && ./hello.exe must not be failure-suppressing: %q", compile)
	}
	if isUnsafeSubAgentVerificationCommand(compile) {
		t.Fatalf("PATH-prefixed g++ && ./hello.exe must not be unsafe: %q", compile)
	}
	if !isSubAgentVerificationCommand(compile) {
		t.Fatalf("PATH-prefixed g++ && ./hello.exe must count as verification: %q", compile)
	}

	commands := []CodingSubAgentCommandResult{
		{
			Command:   `cmd /c 'call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 /EHsc /Fe:hello.exe hello.cpp && hello.exe'`,
			Succeeded: false,
			Summary:   `'\"C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat\"' is not recognized as an internal or external command`,
			seq:       2,
		},
		{
			Command:   `Get-ChildItem "C:\Program Files\Microsoft Visual Studio" -Recurse -Filter "cl.exe" -ErrorAction SilentlyContinue | Select-Object -First 5 FullName`,
			Succeeded: true,
			seq:       3,
		},
		{
			Command:   probe,
			Succeeded: false,
			Summary:   "C:\\Users\\ma139\\AppData\\Local\\Microsoft\\WinGet\\Packages\\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\\mingw64\\bin\\g++.exe\r\nC:\\Users\\ma139\\AppData\\Local\\Microsoft\\WinGet\\Packages\\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\\mingw64\\bin\\gcc.exe\r\n[stderr] Last error: INFO: Could not find files for the given pattern(s).\ncommand exited with code 1",
			seq:       4,
		},
		{
			Command:   compile,
			Succeeded: true,
			Summary:   "Hello, World!\n",
			seq:       5,
		},
	}
	if unresolved := unresolvedFailedSubAgentCommands(commands); len(unresolved) != 0 {
		t.Fatalf("later successful g++ compile must resolve probe failures, got %#v", unresolved)
	}
	status, summary := summarizeSubAgentVerification([]string{"hello.cpp"}, commands, 1)
	if status != codingSubAgentQualityPassed {
		t.Fatalf("live MinGW hello-world should pass verification, got (%q, %q)", status, summary)
	}
	quality, qualitySummary, issues := summarizeSubAgentQuality(
		codingSubAgentQualityNotNeeded, status, true,
		[]string{"hello.cpp"}, []string{"hello.cpp"},
		commands, 1, nil, nil,
	)
	if quality != codingSubAgentQualityPassed {
		t.Fatalf("live MinGW hello-world quality should pass, got (%q, %q, %d)", quality, qualitySummary, issues)
	}
}

func TestCompileThenRunIsOneVerification(t *testing.T) {
	for _, cmd := range []string{
		`cl /utf-8 /EHsc /Fe:hello.exe hello.cpp && hello.exe`,
		`cl /utf-8 /EHsc /Fe:hello.exe hello.cpp ; .\hello.exe`,
		`g++ -std=c++17 -o hello.exe hello.cpp && ./hello.exe`,
		`g++ -std=c++17 -o hello.exe hello.cpp ; .\hello.exe`,
	} {
		if suppressesVerificationFailure(cmd) {
			t.Fatalf("compile+run must not be failure-suppressing: %q", cmd)
		}
		if !isSubAgentVerificationCommand(cmd) {
			t.Fatalf("compile+run must count as verification: %q", cmd)
		}
	}
	if !suppressesVerificationFailure(`go test ./... && echo done`) {
		t.Fatal("echo after go test must stay unsafe")
	}
	if !suppressesVerificationFailure(`go test ./... ; echo done`) {
		t.Fatal("echo after go test via semicolon must stay unsafe")
	}
	if out := cCompilerOutputBinary([]string{"g++", "-og", "-ofast", "-o", "hello.exe", "hello.cpp"}); out != "hello.exe" {
		t.Fatalf("optimizer flags must not be compile outputs, got %q", out)
	}
	if out := cCompilerOutputBinary([]string{"g++", "-ohello", "hello.cpp"}); out != "hello" {
		t.Fatalf("glued -ohello must be output hello, got %q", out)
	}
	if suppressesVerificationFailure(`g++ -og -o hello.exe hello.cpp && ./hello.exe`) {
		t.Fatal("g++ -og -o hello.exe && ./hello.exe must stay a compile+run")
	}
	if suppressesVerificationFailure(`g++ -ohello hello.cpp && hello`) {
		t.Fatal("g++ -ohello && hello must stay a compile+run")
	}
}

func TestRecoverableSubAgentVerificationCommandRemovesDisplayWrapper(t *testing.T) {
	wrapped := `g++ -std=c++11 -o snake.exe snake.cpp 2>&1; echo "Exit code: $?"`
	if !isUnsafeSubAgentVerificationCommand(wrapped) {
		t.Fatalf("fixture must be classified as unsafe before recovery: %q", wrapped)
	}
	if got := recoverSubAgentVerificationCommand(wrapped); got != `g++ -std=c++11 -o snake.exe snake.cpp` {
		t.Fatalf("recovered verifier = %q", got)
	}
	commands := []CodingSubAgentCommandResult{{Command: wrapped, WorkingDir: `C:\repo\cmd`, Succeeded: true, seq: 4}}
	if got := recoverableSubAgentVerificationCommand(commands, 3); got != `g++ -std=c++11 -o snake.exe snake.cpp` {
		t.Fatalf("fresh successful wrapper should produce a clean verifier, got %q", got)
	}
	if got, workingDir := recoverableSubAgentVerification(commands, 3); got != `g++ -std=c++11 -o snake.exe snake.cpp` || workingDir != `C:\repo\cmd` {
		t.Fatalf("recovered verifier must retain its original working directory, got (%q, %q)", got, workingDir)
	}
	if got := recoverableSubAgentVerificationCommand(commands, 5); got != "" {
		t.Fatalf("stale wrapper must not be recovered, got %q", got)
	}
	if got := recoverableSubAgentVerificationCommand([]CodingSubAgentCommandResult{{Command: wrapped, Succeeded: true}}, 1); got != "" {
		t.Fatalf("unsequenced wrapper must not be recovered after a tracked edit, got %q", got)
	}
	commands = append(commands, CodingSubAgentCommandResult{Command: `g++ -std=c++11 -o snake.exe snake.cpp`, Succeeded: true, Summary: "build succeeded", seq: 5})
	status, summary := summarizeSubAgentVerification([]string{"snake.cpp"}, commands, 3)
	if status != codingSubAgentQualityPassed {
		t.Fatalf("clean rerun must supersede the recovered display wrapper, got (%q, %q)", status, summary)
	}
	for _, command := range []string{
		`g++ -o snake.exe snake.cpp 2>&1 | tee build.log`,
		`g++ -o snake.exe snake.cpp || true`,
		`g++ -o snake.exe snake.cpp; Remove-Item snake.exe`,
		`g++ -o snake.exe snake.cpp; echo build complete`,
		`g++ -o snake.exe snake.cpp; echo "Exit code: $?"; Remove-Item snake.exe`,
		`g++ -o snake.exe snake.cpp && printf 'exit code: %s' "$?" && touch build-ran`,
		`g++ -o snake.exe snake.cpp; echo "Exit code: $(Remove-Item snake.exe)"`,
		`g++ -o snake.exe snake.cpp; echo "Exit code: $?" > build.log`,
	} {
		if got := recoverSubAgentVerificationCommand(command); got != "" {
			t.Fatalf("unsafe non-display wrapper must not be recovered: %q -> %q", command, got)
		}
	}
}

func TestRewriteWindowsCompileThenRunSemicolon(t *testing.T) {
	got := rewriteWindowsCompileThenRunSemicolon(`cl /utf-8 /EHsc /Fe:hello.exe hello.cpp ; .\hello.exe`)
	if !strings.Contains(got, "&&") || strings.Contains(got, ";") {
		t.Fatalf("compile then run semicolon must become &&, got %q", got)
	}
	got = rewriteWindowsCompileThenRunSemicolon(`g++ -std=c++17 -o hello.exe hello.cpp ; .\hello.exe`)
	if !strings.Contains(got, "&&") {
		t.Fatalf("g++ compile then run semicolon must become &&, got %q", got)
	}
	kept := rewriteWindowsCompileThenRunSemicolon(`where.exe g++ ; where.exe gcc ; where.exe clang++`)
	if strings.Contains(kept, "&&") {
		t.Fatalf("inventory probes must keep semicolons, got %q", kept)
	}
	kept = rewriteWindowsCompileThenRunSemicolon(`go test ./... ; echo done`)
	if strings.Contains(kept, "&&") {
		t.Fatalf("echo after go test must stay semicolon, got %q", kept)
	}
	got = rewriteWindowsCompileThenRunSemicolon(`Write-Output "a ; b" ; g++ -o hello.exe hello.cpp ; .\hello.exe`)
	if strings.Count(got, "&&") != 1 || !strings.Contains(got, `"a ; b"`) {
		t.Fatalf("quoted semicolon must stay, compile tail must convert, got %q", got)
	}
	got = rewriteWindowsCompileThenRunSemicolon(`cmd /c "cl /utf-8 /EHsc /Fe:hello.exe hello.cpp ; .\hello.exe"`)
	if !strings.Contains(got, `cmd /c "`) || !strings.Contains(got, "&&") || strings.Contains(got, ";") {
		t.Fatalf("cmd /c quoted compile+run must unwrap and convert, got %q", got)
	}
	got = rewriteWindowsCompileThenRunSemicolon(`powershell -NoProfile -Command "g++ -o hello.exe hello.cpp ; .\hello.exe"`)
	if !strings.Contains(got, "-Command") || !strings.Contains(got, "&&") || strings.Contains(got, ";") {
		t.Fatalf("powershell -Command quoted compile+run must convert, got %q", got)
	}
	kept = rewriteWindowsCompileThenRunSemicolon(`cmd /c "where.exe g++ ; where.exe gcc"`)
	if strings.Contains(kept, "&&") {
		t.Fatalf("cmd /c inventory must keep semicolons, got %q", kept)
	}
}

func TestRemoteDocumentationEditDoesNotInvalidatePriorBuildVerification(t *testing.T) {
	cb := &remoteCodingCallbacks{
		fileEdits: []remoteCodingFileAuditEvent{
			{Path: "/home/sysinfo17/README.md", Seq: 4},
		},
	}
	verificationFiles, verificationLastEditSeq := cb.remoteVerificationRelevantEdits([]string{"/home/sysinfo17/README.md"})
	if len(verificationFiles) != 0 {
		t.Fatalf("documentation-only files must not require build verification: %#v", verificationFiles)
	}
	if got := verificationLastEditSeq; got != 0 {
		t.Fatalf("documentation-only edit sequence = %d, want 0", got)
	}
	status, summary := summarizeSubAgentVerification(
		verificationFiles,
		[]CodingSubAgentCommandResult{
			{Command: "cd build && cmake .. && make", Succeeded: true, Summary: "[100%] Built target sysinfo", seq: 2},
			{Command: "/home/sysinfo17/build/sysinfo", Succeeded: true, Summary: "system information", seq: 3},
			{Command: `cd /home/sysinfo17 && git status --short; echo "=== EXIT: $? ==="`, Succeeded: true, seq: 5},
		},
		verificationLastEditSeq,
	)
	if status != codingSubAgentQualityNotNeeded {
		t.Fatalf("README wrap-up must not require a stale rebuild, got (%q, %q)", status, summary)
	}

	cb.fileEdits = append(cb.fileEdits, remoteCodingFileAuditEvent{Path: "/home/sysinfo17/CMakeLists.txt", Seq: 6})
	verificationFiles, verificationLastEditSeq = cb.remoteVerificationRelevantEdits([]string{"/home/sysinfo17/README.md", "/home/sysinfo17/CMakeLists.txt"})
	if len(verificationFiles) != 1 || verificationFiles[0] != "/home/sysinfo17/CMakeLists.txt" {
		t.Fatalf("build-config file must require verification: %#v", verificationFiles)
	}
	if got := verificationLastEditSeq; got != 6 {
		t.Fatalf("build-config edit sequence = %d, want 6", got)
	}
}
