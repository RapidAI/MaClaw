package skill

import (
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveStepShellPowerShellCmdletIsNotAPATHCommand(t *testing.T) {
	command := `Get-ChildItem "$env:USERPROFILE\.maclaw" -ErrorAction SilentlyContinue | Select-Object FullName`
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	for _, name := range []string{"Get-ChildItem", "Select-Object"} {
		if commandIsPathDependency(name, command, "", "windows") {
			t.Fatalf("%s was treated as a PATH dependency", name)
		}
	}
}

func TestResolveStepShellCmdCopyAndDirAreNotPATHCommands(t *testing.T) {
	copyCmd := `copy "C:\a\report.pdf" "C:\b\"`
	shell, _ := ResolveStepShell(copyCmd, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("copy shell = %q, want cmd", shell)
	}
	if commandIsPathDependency("copy", copyCmd, "", "windows") {
		t.Fatal("copy is a cmd.exe internal command")
	}

	dirCmd := `cd "C:\work" && dir`
	shell, _ = ResolveStepShell(dirCmd, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("cd && dir shell = %q, want cmd", shell)
	}
	if commandIsPathDependency("dir", dirCmd, "", "windows") {
		t.Fatal("dir is a cmd.exe internal command")
	}
}

func TestResolveStepShellBareUnixToolStaysBash(t *testing.T) {
	for _, command := range []string{
		`ls -la F:\work\skills`,
		`ls file.py`,
	} {
		shell, reason := ResolveStepShell(command, "", "windows")
		if shell != StepShellBash {
			t.Fatalf("%q shell = %q (%s), want bash", command, shell, reason)
		}
	}
	if commandIsPathDependency("ls", `ls -la F:\work\skills`, "", "windows") {
		t.Fatal("ls was treated as a PATH dependency")
	}
}

func TestResolveStepShellCmdletAfterInterpreterIsPowerShell(t *testing.T) {
	command := "node build.js\nGet-Item \".\\output\\a.html\" | Select-Object Length"
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	for _, name := range []string{"Get-Item", "Select-Object"} {
		if commandIsPathDependency(name, command, "", "windows") {
			t.Fatalf("%s was treated as a PATH dependency", name)
		}
	}
}

func TestResolveStepShellPwdAndDirArePowerShell(t *testing.T) {
	command := `pwd && dir`
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	for _, name := range []string{"pwd", "dir"} {
		if commandIsPathDependency(name, command, "", "windows") {
			t.Fatalf("%s was treated as a PATH dependency", name)
		}
	}
}

func TestResolveStepShellInlineAtQuoteStaysCmd(t *testing.T) {
	shell, reason := ResolveStepShell("echo @\"C:\\temp\"@", "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
}

func TestResolveStepShellCommentDoesNotHidePowerShell(t *testing.T) {
	command := "# list skills\nGet-ChildItem .\ncd $env:USERPROFILE"
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	if commandIsPathDependency("Get-ChildItem", command, "", "windows") {
		t.Fatal("Get-ChildItem was treated as a PATH dependency")
	}
}

func TestResolveStepShellCommentBeforePythonStaysCmd(t *testing.T) {
	shell, reason := ResolveStepShell("# run\npython script.py", "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
}

func TestResolveStepShellExportDoesNotHideCmdlet(t *testing.T) {
	command := "export FOO=1\nGet-ChildItem ."
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	if commandIsPathDependency("Get-ChildItem", command, "", "windows") {
		t.Fatal("Get-ChildItem was treated as a PATH dependency")
	}
	shell, reason = ResolveStepShell("export FOO=1", "", "windows")
	if shell != StepShellBash {
		t.Fatalf("export shell = %q (%s), want bash", shell, reason)
	}
}

func TestResolveStepShellQuotedSubstitutionStaysCmd(t *testing.T) {
	command := `cd C:\work && python -c "print('$(missing-tool)')"`
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
	shell, _ = ResolveStepShell("echo $(date)", "", "windows")
	if shell != StepShellBash {
		t.Fatal("unquoted $(date) should stay bash")
	}
}

func TestResolveStepShellPowerShellContinuationIsNotBash(t *testing.T) {
	command := "copy a.txt `\r\n  b.txt"
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	if commandIsPathDependency("copy", command, "", "windows") {
		t.Fatal("copy was treated as a PATH dependency")
	}
	shell, reason = ResolveStepShell("echo hello`nworld", "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("escape shell = %q (%s), want powershell", shell, reason)
	}
	shell, reason = ResolveStepShell("echo `date`", "", "windows")
	if shell != StepShellBash {
		t.Fatalf("substitution shell = %q (%s), want bash", shell, reason)
	}
}

func TestResolveStepShellPowerShellSubexprIsNotBash(t *testing.T) {
	command := `copy $($src) $($dst)`
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	if commandIsPathDependency("copy", command, "", "windows") {
		t.Fatal("copy was treated as a PATH dependency")
	}
	shell, reason = ResolveStepShell("echo $(Get-Date)", "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("cmdlet subexpr shell = %q (%s), want powershell", shell, reason)
	}
	shell, reason = ResolveStepShell("echo $(date)", "", "windows")
	if shell != StepShellBash {
		t.Fatalf("unix subexpr shell = %q (%s), want bash", shell, reason)
	}
	shell, reason = ResolveStepShell(`python -c "print('$(Get-Date)')"`, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("quoted subexpr shell = %q (%s), want cmd", shell, reason)
	}
}

func TestResolveStepShellUnixPipelineStaysBash(t *testing.T) {
	command := `ls /tmp | head -20`
	shell, _ := ResolveStepShell(command, "", "windows")
	if shell != StepShellBash {
		t.Fatalf("shell = %q, want bash", shell)
	}
	if commandIsPathDependency("head", command, "", "windows") || commandIsPathDependency("ls", command, "", "windows") {
		t.Fatal("posix builtins should stay non-dependencies")
	}
}

func TestResolveStepShellPreferredShellWins(t *testing.T) {
	command := `Get-ChildItem .`
	shell, _ := ResolveStepShell(command, "bash", "windows")
	if shell != StepShellBash {
		t.Fatalf("preferred bash shell = %q", shell)
	}
	if !commandIsPathDependency("Get-ChildItem", command, "bash", "windows") {
		t.Fatal("explicit bash cannot resolve a PowerShell cmdlet, so it stays a PATH dependency")
	}
}

func TestResolveStepShellKeepsRealExecutables(t *testing.T) {
	command := `git clone https://example.com/repo.git dest`
	shell, _ := ResolveStepShell(command, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("git shell = %q, want cmd", shell)
	}
	if !commandIsPathDependency("git", command, "", "windows") {
		t.Fatal("git is an external command")
	}
	if !commandIsPathDependency("docker-compose", "docker-compose up", "", "windows") {
		t.Fatal("docker-compose must stay a PATH dependency")
	}
}

func TestResolveStepShellInterpreterSourceStaysOffPowerShell(t *testing.T) {
	command := `python -c "print('$env:USERPROFILE')"`
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
}

func TestResolveStepShellDoubleQuotedEnvDriveIsPowerShell(t *testing.T) {
	shell, _ := ResolveStepShell(`cd "$env:USERPROFILE"`, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q, want powershell", shell)
	}
}

func TestHereStringBodyIsNotACommandRequirement(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "here-string",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "@'\nimport os\nprint('missing-tool')\n'@ | Out-File out.py\ngit status",
			},
		}},
	}
	names := inferredCommandNames(skill)
	for _, unexpected := range []string{"import", "os", "print", "missing-tool", "Out-File"} {
		if names[unexpected] {
			t.Fatalf("inferred commands = %#v, %s should not be a PATH dependency", names, unexpected)
		}
	}
	if !names["git"] {
		t.Fatalf("inferred commands = %#v, want git after the here-string", names)
	}
}

func TestInlineAtQuoteDoesNotSwallowFollowingCommand(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "inline-at",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "echo @\"C:\\temp\"@\ngit status",
			},
		}},
	}
	names := inferredCommandNames(skill)
	if !names["git"] {
		t.Fatalf("inferred commands = %#v, inline @\" swallowed git", names)
	}
}

func TestResolveStepShellSortObjectIsNotAPATHCommand(t *testing.T) {
	command := `Get-ChildItem . | Sort-Object Name`
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
	if commandIsPathDependency("Sort-Object", command, "", "windows") {
		t.Fatal("Sort-Object was treated as a PATH dependency")
	}
}

func TestResolveStepShellVersionedPythonStaysCmd(t *testing.T) {
	for _, command := range []string{
		`python3.11 -c "print('$env:USERPROFILE')"`,
		`py -c print($env:USERPROFILE)`,
		`pythonw.exe script.py`,
	} {
		shell, reason := ResolveStepShell(command, "", "windows")
		if shell != StepShellCmd {
			t.Fatalf("%q shell = %q (%s), want cmd", command, shell, reason)
		}
	}
}

func TestResolveStepShellHereStringBeatsHashComment(t *testing.T) {
	command := "@'\n# comment\nprint(1)\n'@ | Out-File out.py"
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellPowerShell {
		t.Fatalf("shell = %q (%s), want powershell", shell, reason)
	}
}

func TestResolveStepShellPythonCommentInsideQuotesStaysCmd(t *testing.T) {
	command := "python -c \"\n# keep this comment\nprint(1)\n\""
	shell, reason := ResolveStepShell(command, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
	if !strings.Contains(StripBashCommentLines(command), "# keep this comment") {
		t.Fatal("cmd comment stripping removed a # line inside the python string")
	}
}

func TestResolveStepShellCommentDoesNotChooseTheShell(t *testing.T) {
	shell, reason := ResolveStepShell("# build\nmake all", "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("make shell = %q (%s), want cmd", shell, reason)
	}
	command := "# note\ncopy a.txt b.txt"
	shell, reason = ResolveStepShell(command, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("copy shell = %q (%s), want cmd", shell, reason)
	}
	if commandIsPathDependency("copy", command, "", "windows") {
		t.Fatal("copy was treated as a PATH dependency")
	}
	shell, reason = ResolveStepShell("# note\nexport FOO=1", "", "windows")
	if shell != StepShellBash {
		t.Fatalf("export shell = %q (%s), want bash", shell, reason)
	}
	shell, reason = ResolveStepShell("# note\nls -la", "", "windows")
	if shell != StepShellBash {
		t.Fatalf("ls shell = %q (%s), want bash", shell, reason)
	}
}

func TestResolveStepShellPyLauncherSourceStaysCmd(t *testing.T) {
	shell, reason := ResolveStepShell(`py -c "print('$env:USERPROFILE')"`, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
}

func TestResolveStepShellCmdChainWithPythonStaysCmd(t *testing.T) {
	shell, reason := ResolveStepShell(`cd C:\work && python script.py`, "", "windows")
	if shell != StepShellCmd {
		t.Fatalf("shell = %q (%s), want cmd", shell, reason)
	}
}

func TestResolveStepShellNonWindowsDefaultsToBash(t *testing.T) {
	shell, _ := ResolveStepShell(`Get-ChildItem .`, "", "linux")
	if shell != StepShellBash {
		t.Fatalf("shell = %q, want bash", shell)
	}
	if !commandIsPathDependency("Get-ChildItem", `Get-ChildItem .`, "", "linux") {
		t.Fatal("linux bash does not provide PowerShell cmdlets")
	}
	if !commandIsPathDependency("copy", `copy a b`, "", "linux") {
		t.Fatal("copy is not a bash builtin")
	}
}

func TestExtractRequirementsSkipsWindowsShellCommands(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("inferred requirements follow the host shell")
	}
	skill := &corelib.NLSkillEntry{
		Name: "weather-pdf",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": `Get-ChildItem "$env:USERPROFILE" | Select-Object FullName`}},
			{Action: "bash", Params: map[string]interface{}{"command": `copy "a.pdf" "b.pdf"`}},
			{Action: "bash", Params: map[string]interface{}{"command": `git status`}},
		},
	}
	names := inferredCommandNames(skill)
	for _, unexpected := range []string{"Get-ChildItem", "Select-Object", "copy"} {
		if names[unexpected] {
			t.Fatalf("inferred commands = %#v, %s is provided by the step shell", names, unexpected)
		}
	}
	if !names["git"] {
		t.Fatalf("inferred commands = %#v, want git", names)
	}
}

func TestCheckRunnerRequirementsWindowsShellNativesDoNotBlock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("runner precheck follows the host shell")
	}
	entry := &corelib.NLSkillEntry{
		Name: "craft_beijing_weather_pdf_report",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": `Get-ChildItem "$env:USERPROFILE\.maclaw" | Select-Object FullName`}},
			{Action: "bash", Params: map[string]interface{}{"command": `copy "a.pdf" "b.pdf"`}},
		},
	}
	if errs := FilterErrors(CheckRunnerRequirements(entry, nil, RunnerBackendGUI)); len(errs) > 0 {
		t.Fatalf("runner blocked shell-native commands:\n%s", FormatViolations(errs))
	}
}

func commandIsPathDependency(name, command, preferred, goos string) bool {
	shell, _ := ResolveStepShell(command, preferred, goos)
	normalized := normalizeInferredCommandName(name)
	lower := strings.ToLower(normalized)
	if shellBuiltins[lower] || shellProvidesCommand(normalized, shell) || skipInferredCommand(normalized) {
		return false
	}
	return true
}

func inferredCommandNames(skill *corelib.NLSkillEntry) map[string]bool {
	names := map[string]bool{}
	for _, req := range inferCommandRequirements(skill) {
		names[req.Name] = true
	}
	return names
}
