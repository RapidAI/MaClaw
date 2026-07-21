package main

import "testing"

// Regression from ~/.maclaw workbench: T3 ran successfully
//   mkdir && cmake && make && /home/.../build/sysinfo12
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
