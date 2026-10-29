package agent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

type commitHost struct {
	mockCallbacks
	root        string
	temp        string
	checklist   bool
	chatBound   bool
	fullControl bool
	fullNotes   int
	taskKey     string
	ledger      *CommitLedger
	prepared    string
	failStore   bool
	noPause     bool
	resolve     func(toolName, fingerprint, subject string) (CommitDecision, bool)
}

func (h *commitHost) CommitWorkspaceRoot() string { return h.root }
func (h *commitHost) CommitTaskTempDir() string   { return h.temp }
func (h *commitHost) CommitChecklistOpen() bool   { return h.checklist }
func (h *commitHost) CommitTaskKey() string       { return h.taskKey }
func (h *commitHost) CommitFullControlGranted() bool {
	return h != nil && h.fullControl
}
func (h *commitHost) CommitNoteFullControl(string, string) {
	if h != nil {
		h.fullNotes++
	}
}
func (h *commitHost) CommitLookup(fingerprint string) (CommitDecision, bool) {
	if h.ledger == nil {
		return "", false
	}
	return h.ledger.Lookup(fingerprint)
}
func (h *commitHost) CommitStore(fingerprint string, decision CommitDecision) error {
	if h.failStore {
		return errors.New("store failed")
	}
	if h.ledger == nil {
		h.ledger = NewCommitLedger()
	}
	h.ledger.Store(fingerprint, decision)
	return nil
}
func (h *commitHost) CommitPrepareAsk(fingerprint string) string {
	h.prepared = fingerprint
	return "nonce-1"
}

func (h *commitHost) CommitAskPauses() bool { return h == nil || !h.noPause }

func (h *commitHost) CommitCallBoundToChat(name, _ string) bool {
	return h != nil && h.chatBound && strings.EqualFold(strings.TrimSpace(name), "send_file")
}

func (h *commitHost) ResolveCommitAsk(toolName, fingerprint, subject string) (CommitDecision, bool) {
	if h == nil || h.resolve == nil {
		return "", false
	}
	return h.resolve(toolName, fingerprint, subject)
}

func newCommitHost(t *testing.T) *commitHost {
	t.Helper()
	root := t.TempDir()
	return &commitHost{
		mockCallbacks: mockCallbacks{toolResult: "ran"},
		root:          root,
		temp:          filepath.Join(root, ".maclaw-tmp"),
		taskKey:       "test\n" + t.Name(),
		ledger:        NewCommitLedger(),
	}
}

func TestClassifyCommitCallShapes(t *testing.T) {
	root := t.TempDir()
	temp := filepath.Join(root, ".maclaw-tmp")
	outside := filepath.Join(filepath.Dir(root), "commit-gate-outside.txt")

	cases := []struct {
		name  string
		args  string
		class CommitClass
	}{
		{"bash", `{"command":"git push"}`, CommitHuman},
		{"bash", `{"command":"git -C repo push"}`, CommitHuman},
		{"bash", `{"command":"git -Crepo push"}`, CommitHuman},
		{"bash", `{"command":"eval git push"}`, CommitHuman},
		{"bash", `{"command":"env MODE=prod git push"}`, CommitHuman},
		{"bash", `{"command":"env -i MODE=prod git push"}`, CommitHuman},
		{"bash", `{"command":"env -u MODE git push"}`, CommitHuman},
		{"bash", `{"command":"env --unset=MODE git push"}`, CommitHuman},
		{"bash", `{"command":"echo $(git push)"}`, CommitHuman},
		{"bash", "{\"command\":\"echo `git push`\"}", CommitHuman},
		{"bash", `{"command":"echo '$(git push)'"}`, CommitUnclassified},
		{"bash", `{"command":"xargs git push"}`, CommitHuman},
		{"bash", `{"command":"sudo -u root git push"}`, CommitHuman},
		{"bash", `{"command":"sudo bash -c 'git push'"}`, CommitHuman},
		{"bash", `{"command":"env bash -c 'git push'"}`, CommitHuman},
		{"bash", `{"command":"env -S 'git push'"}`, CommitHuman},
		{"bash", `{"command":"timeout 5 git push"}`, CommitHuman},
		{"bash", `{"command":"timeout 5 bash -c 'git push'"}`, CommitHuman},
		{"bash", `{"command":"sudo echo git push"}`, CommitUnclassified},
		{"bash", `{"command":"echo $(git status)"}`, CommitUnclassified},
		{"bash", `{"command":"# $(git push)"}`, CommitUnclassified},
		{"bash", `{"command":"git push # note"}`, CommitHuman},
		{"bash", `{"command":"echo <(git push)"}`, CommitHuman},
		{"bash", `{"command":"diff <(git status) file"}`, CommitUnclassified},
		{"bash", `{"command":"$(cd /tmp; git push)"}`, CommitHuman},
		{"bash", `{"command":"(cd /tmp; git push)"}`, CommitHuman},
		{"bash", `{"command":"{ git push; }"}`, CommitHuman},
		{"bash", `{"command":"(git status)"}`, CommitUnclassified},
		{"bash", `{"command":"{ git status; }"}`, CommitUnclassified},
		{"bash", `{"command":"echo {foo; git push"}`, CommitHuman},
		{"bash", `{"command":"echo ${HOME}; git status"}`, CommitUnclassified},
		{"bash", `{"command":"echo $((git push))"}`, CommitUnclassified},
		{"bash", `{"command":"((git push))"}`, CommitUnclassified},
		{"bash", `{"command":"echo $(( $(git push) ))"}`, CommitHuman},
		{"bash", `{"command":"gi\\t push"}`, CommitHuman},
		{"bash", `{"command":"command git push"}`, CommitHuman},
		{"bash", `{"command":"command -p git push"}`, CommitHuman},
		{"bash", `{"command":"command -v git"}`, CommitUnclassified},
		{"bash", `{"command":"exec git push"}`, CommitHuman},
		{"bash", `{"command":"nohup git push"}`, CommitHuman},
		{"bash", `{"command":"env -S git push"}`, CommitHuman},
		{"bash", `{"command":"{ git push ${HOME}; }"}`, CommitHuman},
		{"bash", `{"command":"{ echo ${HOME}; git status; }"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c git push"}`, CommitHuman},
		{"bash", `{"command":"cmd /c git status"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c echo git push"}`, CommitUnclassified},
		{"bash", `{"command":"powershell -NoProfile -Command git push"}`, CommitHuman},
		{"bash", `{"command":"powershell -Command git status"}`, CommitUnclassified},
		{"bash", `{"command":"powershell -Command:git push"}`, CommitHuman},
		{"bash", `{"command":"git --config-env REMOTE=ORIGIN push"}`, CommitHuman},
		{"bash", `{"command":"git --config-env=REMOTE=ORIGIN push"}`, CommitHuman},
		{"bash", `{"command":"git --config-env REMOTE=ORIGIN status"}`, CommitUnclassified},
		{"bash", `{"command":"git.cmd push"}`, CommitHuman},
		{"bash", `{"command":"git $cmd"}`, CommitHuman},
		{"bash", `{"command":"git pu$h"}`, CommitHuman},
		{"bash", `{"command":"git log $rev"}`, CommitUnclassified},
		{"bash", `{"command":"iex 'git push'"}`, CommitHuman},
		{"bash", `{"command":"Invoke-Expression 'git status'"}`, CommitUnclassified},
		{"bash", `{"command":"call git push"}`, CommitHuman},
		{"bash", `{"command":"call git status"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process git -ArgumentList push"}`, CommitHuman},
		{"bash", `{"command":"Start-Process git push"}`, CommitHuman},
		{"bash", `{"command":"Start-Process git -ArgumentList status"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process notepad"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process notepad -Wait"}`, CommitUnclassified},
		{"bash", `{"command":"iex -Command:git push"}`, CommitHuman},
		{"bash", `{"command":"iex -Command 'git status'"}`, CommitUnclassified},
		{"bash", `{"command":"iex -ErrorAction Stop 'git push'"}`, CommitHuman},
		{"bash", `{"command":"echo iex 'git push'"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process -ArgumentList push -FilePath git"}`, CommitHuman},
		{"bash", `{"command":"Start-Process git -WorkingDirectory repo -ArgumentList push"}`, CommitHuman},
		{"bash", `{"command":"Start-Process git -WorkingDirectory repo -ArgumentList status"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process git -ArgumentList push,origin"}`, CommitHuman},
		{"bash", `{"command":"Start-Process powershell -ArgumentList '-Command','git push'"}`, CommitHuman},
		{"bash", `{"command":"Start-Process powershell -ArgumentList '-Command','git status'"}`, CommitUnclassified},
		{"bash", `{"command":"saps -FilePath git -Args push"}`, CommitHuman},
		{"bash", `{"command":"start git push"}`, CommitHuman},
		{"bash", `{"command":"start notepad"}`, CommitUnclassified},
		{"bash", `{"command":"start /b git push"}`, CommitHuman},
		{"bash", `{"command":"start /D repo git push"}`, CommitHuman},
		{"bash", `{"command":"start /D repo git status"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process \"C:\\Program Files\\Git\\cmd\\git.exe\" push"}`, CommitHuman},
		{"bash", `{"command":"git @('push')"}`, CommitHuman},
		{"bash", `{"command":"git @('status')"}`, CommitUnclassified},
		{"bash", `{"command":"git @('push','origin')"}`, CommitHuman},
		{"bash", `{"command":"git @('push', 'origin')"}`, CommitHuman},
		{"bash", `{"command":"git @('status', 'origin')"}`, CommitUnclassified},
		{"bash", `{"command":"git @args"}`, CommitHuman},
		{"bash", `{"command":"git log @revs"}`, CommitUnclassified},
		{"bash", `{"command":"echo @('git push')"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process git -ArgumentList @('push')"}`, CommitHuman},
		{"bash", `{"command":"Start-Process git -ArgumentList @('status')"}`, CommitUnclassified},
		{"bash", `{"command":"Start-Process git -ArgumentList @('push','origin')"}`, CommitHuman},
		{"bash", `{"command":"Start-Process powershell -ArgumentList @('-Command','git push')"}`, CommitHuman},
		{"bash", `{"command":"Start-Process powershell -ArgumentList @('-Command','git status')"}`, CommitUnclassified},
		{"bash", `{"command":"wsl git push"}`, CommitHuman},
		{"bash", `{"command":"wsl git status"}`, CommitUnclassified},
		{"bash", `{"command":"wsl -d Ubuntu git push"}`, CommitHuman},
		{"bash", `{"command":"wsl -d Ubuntu git status"}`, CommitUnclassified},
		{"bash", `{"command":"wsl -e git push"}`, CommitHuman},
		{"bash", `{"command":"wsl echo git push"}`, CommitUnclassified},
		{"bash", `{"command":"wsl --status"}`, CommitUnclassified},
		{"bash", `{"command":"wsl --terminate Ubuntu"}`, CommitUnclassified},
		{"bash", `{"command":"echo wsl git push"}`, CommitUnclassified},
		{"bash", `{"command":"& git push"}`, CommitHuman},
		{"bash", `{"command":"& git status"}`, CommitUnclassified},
		{"bash", `{"command":"&{git push}"}`, CommitHuman},
		{"bash", `{"command":"& {git status}"}`, CommitUnclassified},
		{"bash", `{"command":".{git push}"}`, CommitHuman},
		{"bash", `{"command":"{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"function f { git push }"}`, CommitUnclassified},
		{"bash", `{"command":"Invoke-Command -ScriptBlock {git push}"}`, CommitHuman},
		{"bash", `{"command":"Invoke-Command -ScriptBlock {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"1 | % {git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | % {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"if($true){git push}"}`, CommitHuman},
		{"bash", `{"command":"if($true){git status}"}`, CommitUnclassified},
		{"bash", `{"command":"try {git push} catch { }"}`, CommitHuman},
		{"bash", `{"command":"function f(){git push}"}`, CommitUnclassified},
		{"bash", `{"command":"powershell -Command {git push}"}`, CommitHuman},
		{"bash", `{"command":"powershell -Command {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"$env:ComSpec /c git push"}`, CommitHuman},
		{"bash", `{"command":"$env:ComSpec /c git status"}`, CommitUnclassified},
		{"bash", `{"command":"%COMSPEC% /c git push"}`, CommitHuman},
		{"bash", `{"command":"%COMSPEC% /c git status"}`, CommitUnclassified},
		{"bash", `{"command":"$git push"}`, CommitHuman},
		{"bash", `{"command":"$git status"}`, CommitUnclassified},
		{"bash", `{"command":"echo $env:ComSpec"}`, CommitUnclassified},
		{"bash", `{"command":"switch (1) { 1 {git push} }"}`, CommitHuman},
		{"bash", `{"command":"switch (1) { default {git status} }"}`, CommitUnclassified},
		{"bash", `{"command":"switch (1) { default {git push} }"}`, CommitHuman},
		{"bash", `{"command":"foreach ($i in 1) { git push }"}`, CommitHuman},
		{"bash", `{"command":"foreach ($i in 1) { git status }"}`, CommitUnclassified},
		// A regex end-anchor inside ForEach-Object is not git push. The open
		// checklist used to refuse this verification pipeline.
		{"bash", `{"command":"cd E:\\gametest; .\\build\\verify-rel\\bin\\Release\\gametest.exe --self-test 2>&1 | Select-String -Pattern \"^PASS \" | ForEach-Object { ($_.Line -replace '^PASS ','') -replace '[:.].*$','' } | Group-Object | Select-Object Count,Name"}`, CommitUnclassified},
		{"bash", `{"command":"ForEach-Object { ($_.Line -replace '^PASS ','') -replace '[:.].*$','' }"}`, CommitUnclassified},
		{"bash", `{"command":"ForEach-Object { $_.Line.Split(' ') }"}`, CommitUnclassified},
		{"bash", `{"command":"$_.Line -replace 'git push'"}`, CommitUnclassified},
		{"bash", `{"command":"$x -replace 'git push'"}`, CommitUnclassified},
		{"bash", `{"command":"$x -replace 'a' git push"}`, CommitHuman},
		{"bash", `{"command":"$x -replace 'a', 'b' git push"}`, CommitHuman},
		{"bash", `{"command":"$x -replace $a, $b git push"}`, CommitHuman},
		{"bash", `{"command":"$x -replace $a $b git push"}`, CommitHuman},
		{"bash", `{"command":"$x -replace 'a' , 'b' git push"}`, CommitHuman},
		{"bash", `{"command":"$x -replace 'a' , 'b'"}`, CommitUnclassified},
		{"bash", `{"command":"$fmt -f $arg"}`, CommitUnclassified},
		{"bash", `{"command":"$fmt -f 'push'"}`, CommitUnclassified},
		{"bash", `{"command":"$fmt -f '[:.].*$'"}`, CommitUnclassified},
		{"bash", `{"command":"$git -f push"}`, CommitUnclassified},
		{"bash", `{"command":"& $git -f push"}`, CommitHuman},
		{"bash", `{"command":"git push -f"}`, CommitHuman},
		{"bash", `{"command":"$fmt -f 'a' git push"}`, CommitHuman},
		// -split runs a delimiter block. {git push} has no space, so the bash
		// brace rule does not see it. Unary -split and command mode do not run it.
		{"bash", `{"command":"$x -split {git push}"}`, CommitHuman},
		{"bash", `{"command":"$x -split {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"$x -split{git push}"}`, CommitHuman},
		{"bash", `{"command":"$x -isplit {git push}"}`, CommitHuman},
		{"bash", `{"command":"$x -csplit {git push}"}`, CommitHuman},
		{"bash", `{"command":"'ab' -split {git push}"}`, CommitHuman},
		{"bash", `{"command":"-split {git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo hi -split {git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo ($x -split {git push})"}`, CommitHuman},
		{"bash", `{"command":"$x.ForEach{git push}"}`, CommitHuman},
		{"bash", `{"command":"$x.ForEach{git status}"}`, CommitUnclassified},
		{"bash", `{"command":"$x.ForEach({git push})"}`, CommitHuman},
		{"bash", `{"command":"$x.Where{git push}"}`, CommitHuman},
		{"bash", `{"command":"hi.ForEach{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"$x.ForEach{ $_.Line -replace '[:.].*$','' }"}`, CommitUnclassified},
		{"bash", `{"command":"${git} push"}`, CommitHuman},
		{"bash", `{"command":"{git push}.Invoke()"}`, CommitHuman},
		{"bash", `{"command":"{git status}.Invoke()"}`, CommitUnclassified},
		{"bash", `{"command":"{git push}.InvokeReturnAsIs()"}`, CommitHuman},
		{"bash", `{"command":"$x={git push}.Invoke()"}`, CommitHuman},
		{"bash", `{"command":"1 | Sort-Object {git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | Sort-Object {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"1 | sort -Property {git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | Group-Object {git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | Select-Object {git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | Select-Object @{n='a';e={git push}}"}`, CommitHuman},
		{"bash", `{"command":"1 | Select-Object @{e={git status}}"}`, CommitUnclassified},
		{"bash", `{"command":"echo group {git push}"}`, CommitUnclassified},
		{"bash", `{"command":"Get-ChildItem -Property {git push}"}`, CommitUnclassified},
		{"bash", `{"command":"1 | Select-Object @{n='a|b';e={git push}}"}`, CommitHuman},
		{"bash", `{"command":"Measure-Command{git push}"}`, CommitHuman},
		{"bash", `{"command":"Measure-Command{git status}"}`, CommitUnclassified},
		{"bash", `{"command":"Measure-Command -Expression{git push}"}`, CommitHuman},
		{"bash", `{"command":"Start-Job{git push}"}`, CommitHuman},
		{"bash", `{"command":"Start-Job{git status}"}`, CommitUnclassified},
		{"bash", `{"command":"sajb{git push}"}`, CommitHuman},
		{"bash", `{"command":"echo Measure-Command{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo foreach{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo %{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo ?{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo icm{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"1 | %{git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | ?{git push}"}`, CommitHuman},
		{"bash", `{"command":"icm{git push}"}`, CommitHuman},
		{"bash", `{"command":"Microsoft.PowerShell.Utility\\Measure-Command{git push}"}`, CommitHuman},
		{"bash", `{"command":"Microsoft.PowerShell.Utility\\Measure-Command{git status}"}`, CommitUnclassified},
		{"bash", `{"command":"Microsoft.PowerShell.Utility\\Measure-Command -Expression{git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | Microsoft.PowerShell.Utility\\Select-Object @{n='a';e={git push}}"}`, CommitHuman},
		{"bash", `{"command":"Microsoft.PowerShell.Core\\ForEach-Object{git push}"}`, CommitHuman},
		{"bash", `{"command":"Microsoft.PowerShell.Core\\ForEach-Object{git status}"}`, CommitUnclassified},
		{"bash", `{"command":"& Measure-Command{git push}"}`, CommitHuman},
		{"bash", `{"command":"&Measure-Command{git push}"}`, CommitHuman},
		{"bash", `{"command":". Measure-Command{git push}"}`, CommitHuman},
		{"bash", `{"command":"1 | & ForEach-Object{git push}"}`, CommitHuman},
		// -Expression alone errors before the block. -Name before or after runs it.
		// A value word is the positional -Expression. The block alone binds to -Name.
		{"bash", `{"command":"Trace-Command -Expression{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"Trace-Command -Name ParameterBinding -Expression{git push}"}`, CommitHuman},
		{"bash", `{"command":"Trace-Command -Name ParameterBinding -Expression{git status}"}`, CommitUnclassified},
		{"bash", `{"command":"Trace-Command -Expression{git push} -Name ParameterBinding"}`, CommitHuman},
		{"bash", `{"command":"Trace-Command ParameterBinding{git push}"}`, CommitHuman},
		{"bash", `{"command":"Trace-Command{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"& 'Measure-Command'{git push}"}`, CommitHuman},
		{"bash", `{"command":"& \"Measure-Command\"{git push}"}`, CommitHuman},
		{"bash", `{"command":"& 'Microsoft.PowerShell.Utility\\Measure-Command'{git push}"}`, CommitHuman},
		{"bash", `{"command":". 'Measure-Command'{git push}"}`, CommitHuman},
		{"bash", `{"command":"& ('Measure-Command'){git push}"}`, CommitHuman},
		{"bash", `{"command":"&('Measure-Command'){git push}"}`, CommitHuman},
		{"bash", `{"command":"echo (1){git push}"}`, CommitUnclassified},
		{"bash", `{"command":"for($i=0;$i-lt1;$i++){git push}"}`, CommitHuman},
		{"bash", `{"command":"echo try{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo do{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo catch{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo else{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo finally{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"echo trap{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"do{git push} while ($false)"}`, CommitHuman},
		{"bash", `{"command":"if ($false) {} else {git push}"}`, CommitHuman},
		{"bash", `{"command":"if ($false) {} else {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"try {} finally {git push}"}`, CommitHuman},
		{"bash", `{"command":"try { throw 'x' } catch {git push}"}`, CommitHuman},
		{"bash", `{"command":"trap [System.Exception] {git push}"}`, CommitHuman},
		{"bash", `{"command":"trap [System.Exception] {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"echo trap [System.Exception] {git push}"}`, CommitUnclassified},
		{"bash", `{"command":"try { throw 'x' } catch [System.Exception] {git push}"}`, CommitHuman},
		{"bash", `{"command":"try { [scriptblock]{git push} }"}`, CommitUnclassified},
		{"bash", `{"command":"1 | Sort-Object @{e={git push}}"}`, CommitHuman},
		{"bash", `{"command":"1 | Sort-Object @{n={git push}}"}`, CommitUnclassified},
		{"bash", `{"command":"1 | Group-Object @{expression={git push}}"}`, CommitHuman},
		{"bash", `{"command":"1 | Group-Object @{n={git push}}"}`, CommitUnclassified},
		{"bash", `{"command":"[scriptblock]::Create('git push').Invoke()"}`, CommitHuman},
		{"bash", `{"command":"[scriptblock]::Create('git status').Invoke()"}`, CommitUnclassified},
		{"bash", `{"command":"[scriptblock]::Create('git push')"}`, CommitUnclassified},
		{"bash", `{"command":"[scriptblock]::Create($x).Invoke()"}`, CommitHuman},
		{"bash", `{"command":"& ([scriptblock]::Create('git push'))"}`, CommitHuman},
		{"bash", `{"command":"& ([scriptblock]::Create('git status'))"}`, CommitUnclassified},
		{"bash", `{"command":"([scriptblock]::Create('git push')).Invoke()"}`, CommitHuman},
		{"bash", `{"command":"echo ([scriptblock]::Create('git push'))"}`, CommitUnclassified},
		{"bash", `{"command":"$x -replace 'a',{git push}"}`, CommitUnclassified},
		{"bash", `{"command":"$_.Line -replace 'a' git push"}`, CommitHuman},
		{"bash", `{"command":"$_.Line git push"}`, CommitHuman},
		{"bash", `{"command":"& $_.Line -replace 'git push'"}`, CommitHuman},
		{"bash", `{"command":"& $_git -replace 'git push'"}`, CommitHuman},
		{"bash", `{"command":"git -replace push"}`, CommitUnclassified},
		{"bash", `{"command":"bash -replace 'git push'"}`, CommitHuman},
		{"bash", `{"command":"$git pu$h"}`, CommitHuman},
		{"bash", `{"command":"gametest.exe --self-test; echo EXIT=$LASTEXITCODE"}`, CommitUnclassified},
		{"bash", `{"command":"for ($i=0; $i -lt 1; $i++) { git push }"}`, CommitHuman},
		{"bash", `{"command":"for ($i=0; $i -lt 1; $i++) { git status }"}`, CommitUnclassified},
		{"bash", `{"command":"for /f %i in ('git push') do @echo %i"}`, CommitHuman},
		{"bash", `{"command":"for /f %i in ('git status') do @echo %i"}`, CommitUnclassified},
		{"bash", `{"command":"for /f \"tokens=*\" %i in ('git push') do @echo %i"}`, CommitHuman},
		{"bash", `{"command":"for %i in (1) do git push"}`, CommitHuman},
		{"bash", `{"command":"for %i in (1) do git status"}`, CommitUnclassified},
		{"bash", `{"command":"for %i in (1) do echo git push"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c \"for /f %i in ('git push') do @echo %i\""}`, CommitHuman},
		{"bash", `{"command":"echo for %i in (1) do git push"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c git pu^sh"}`, CommitHuman},
		{"bash", `{"command":"cmd /c git rev-par^se --is-inside-work-tree"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c echo git pu^sh"}`, CommitUnclassified},
		{"bash", `{"command":"git pu^sh"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c git pu^^sh"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c \"echo ok ^& git push\""}`, CommitHuman},
		{"bash", `{"command":"cmd /c \"echo ok ^& git status\""}`, CommitUnclassified},
		{"bash", `{"command":"cmd /v:on /c \"set s=push& git !s!\""}`, CommitHuman},
		{"bash", `{"command":"cmd /v:on /c git status"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c git !s!"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /v:on /c echo git !s!"}`, CommitUnclassified},
		{"bash", `{"command":"'git push' | iex"}`, CommitHuman},
		{"bash", `{"command":"'git status' | iex"}`, CommitUnclassified},
		{"bash", `{"command":"'git push' | Invoke-Expression"}`, CommitHuman},
		{"bash", `{"command":"iex $c"}`, CommitHuman},
		{"bash", `{"command":"iex (Get-Content script.ps1)"}`, CommitHuman},
		{"bash", `{"command":"echo iex $c"}`, CommitUnclassified},
		{"bash", `{"command":"Get-Date | iex"}`, CommitHuman},
		{"bash", `{"command":"@'\ngit push\n'@ | iex"}`, CommitHuman},
		{"bash", `{"command":"@'\ngit status\n'@ | iex"}`, CommitUnclassified},
		{"bash", `{"command":"do { git push } while ($false)"}`, CommitHuman},
		{"bash", `{"command":"do { git status } while ($false)"}`, CommitUnclassified},
		{"bash", `{"command":"for /f \"usebackq\" %i in (` + "`" + `git push` + "`" + `) do @echo %i"}`, CommitHuman},
		{"bash", `{"command":"for /f \"usebackq\" %i in (` + "`" + `git status` + "`" + `) do @echo %i"}`, CommitUnclassified},
		{"bash", `{"command":"Microsoft.PowerShell.Management\\Start-Process git -ArgumentList push"}`, CommitHuman},
		{"bash", `{"command":"Microsoft.PowerShell.Management\\Start-Process git -ArgumentList status"}`, CommitUnclassified},
		{"bash", `{"command":"switch (1) { { $true } { git push } }"}`, CommitHuman},
		{"bash", `{"command":"switch (1) { { $true } { git status } }"}`, CommitUnclassified},
		{"bash", `{"command":"function f { git push }; f"}`, CommitHuman},
		{"bash", `{"command":"function f { git status }; f"}`, CommitUnclassified},
		{"bash", `{"command":"function f { git push }; & f"}`, CommitHuman},
		{"bash", `{"command":"function f { git push }; echo f"}`, CommitUnclassified},
		{"bash", `{"command":"function f { git push }; function g { f }; g"}`, CommitHuman},
		{"bash", `{"command":"sal g git; g push"}`, CommitHuman},
		{"bash", `{"command":"sal g git; g status"}`, CommitUnclassified},
		{"bash", `{"command":"Set-Alias -Name g -Value git; g push"}`, CommitHuman},
		{"bash", `{"command":"New-Alias -Name g -Value git -Force; g status"}`, CommitUnclassified},
		{"bash", `{"command":"function f { git push }; sal g f; g"}`, CommitHuman},
		{"bash", `{"command":"cmd /c git pu%PATH:~0,0%sh"}`, CommitHuman},
		{"bash", `{"command":"cmd /c git status%PATH:~0,0%"}`, CommitUnclassified},
		{"bash", `{"command":"git pu%PATH:~0,0%sh"}`, CommitUnclassified},
		{"bash", `{"command":"cmd /c git %NOT_A_REAL_PUSH_VAR%"}`, CommitHuman},
		{"bash", `{"command":"git -c alias.p=push p"}`, CommitHuman},
		{"bash", `{"command":"git -c alias.p=status p"}`, CommitUnclassified},
		{"bash", `{"command":"git -c alias.p=push status"}`, CommitUnclassified},
		{"bash", `{"command":"git -c alias.p=push -- p"}`, CommitHuman},
		{"bash", `{"command":"git -c alias.p=status -- p"}`, CommitUnclassified},
		{"bash", `{"command":"git -c \"alias.p=!git push\" p"}`, CommitHuman},
		{"bash", `{"command":"git -c \"alias.p=!git status\" p"}`, CommitUnclassified},
		{"bash", `{"command":"git -c user.name=ada push"}`, CommitHuman},
		{"bash", `{"command":"git -c user.name=ada status"}`, CommitUnclassified},
		{"bash", `{"command":"git ('push')"}`, CommitHuman},
		{"bash", `{"command":"git ('status')"}`, CommitUnclassified},
		{"bash", `{"command":"git ('pu'+'sh')"}`, CommitHuman},
		{"bash", `{"command":"git ('sta'+'tus')"}`, CommitUnclassified},
		{"bash", `{"command":"git ('pu' + 'sh')"}`, CommitHuman},
		{"bash", `{"command":"git (\"{0}\" -f 'push')"}`, CommitHuman},
		{"bash", `{"command":"git (\"{0}\" -f 'status')"}`, CommitUnclassified},
		{"bash", `{"command":"git (\"{0}{1}\" -f 'pu','sh')"}`, CommitHuman},
		{"bash", `{"command":"git (\"{0}{1}\" -f 'sta','tus')"}`, CommitUnclassified},
		{"bash", `{"command":"git @('pu'+'sh')"}`, CommitHuman},
		{"bash", `{"command":"git @('sta'+'tus')"}`, CommitUnclassified},
		{"bash", `{"command":"& ('git') push"}`, CommitHuman},
		{"bash", `{"command":"& ('git') status"}`, CommitUnclassified},
		{"bash", `{"command":"& ('gi'+'t') push"}`, CommitHuman},
		{"bash", `{"command":"& ('gi'+'t') status"}`, CommitUnclassified},
		{"bash", `{"command":"& (Get-Command git) push"}`, CommitHuman},
		{"bash", `{"command":"& (Get-Command git) status"}`, CommitUnclassified},
		{"bash", `{"command":"& ('notepad') push"}`, CommitUnclassified},
		{"bash", `{"command":"git (Get-Date)"}`, CommitHuman},
		{"bash", `{"command":"git status (Get-Date)"}`, CommitUnclassified},
		{"bash", `{"command":"echo ('git push')"}`, CommitUnclassified},
		{"bash", `{"command":"&\"git\" push"}`, CommitHuman},
		{"bash", `{"command":"&'git' push"}`, CommitHuman},
		{"bash", `{"command":"&\"git\" status"}`, CommitUnclassified},
		{"bash", `{"command":"&'notepad' push"}`, CommitUnclassified},
		{"bash", `{"command":"git 'puXsh'.Replace('X','')"}`, CommitHuman},
		{"bash", `{"command":"git 'staXtus'.Replace('X','')"}`, CommitUnclassified},
		{"bash", `{"command":"git 'Xpush'.Substring(1)"}`, CommitHuman},
		{"bash", `{"command":"git 'Xstatus'.Substring(1)"}`, CommitUnclassified},
		{"bash", `{"command":"git 'Xpush'.substring(1)"}`, CommitHuman},
		{"bash", `{"command":"git \"Xpush\".Substring(1)"}`, CommitHuman},
		{"bash", `{"command":"git 'XXpushYY'.Substring(2,4)"}`, CommitHuman},
		{"bash", `{"command":"git 'pushXX'.Remove(4,2)"}`, CommitHuman},
		{"bash", `{"command":"git 'pushXX'.Remove(4)"}`, CommitHuman},
		{"bash", `{"command":"git 'statusXX'.Remove(6,2)"}`, CommitUnclassified},
		{"bash", `{"command":"git 'puh'.Insert(2,'s')"}`, CommitHuman},
		{"bash", `{"command":"git 'saus'.Insert(1,'t')"}`, CommitUnclassified},
		{"bash", `{"command":"git 'PUSH'.ToLower()"}`, CommitHuman},
		{"bash", `{"command":"git 'STATUS'.ToLower()"}`, CommitUnclassified},
		{"bash", `{"command":"git 'push'.ToUpper()"}`, CommitUnclassified},
		{"bash", `{"command":"git ' push '.Trim()"}`, CommitHuman},
		{"bash", `{"command":"git ' status '.Trim()"}`, CommitUnclassified},
		{"bash", `{"command":"git 'XpuXsh'.Replace('X','').Substring(0)"}`, CommitHuman},
		{"bash", `{"command":"git ' Xpush '.Replace('X','').Trim()"}`, CommitHuman},
		{"bash", `{"command":"git ('pu'+'sh'.Substring(0))"}`, CommitHuman},
		{"bash", `{"command":"git ('sta'+'tus'.Substring(0))"}`, CommitUnclassified},
		{"bash", `{"command":"git Xpush.Substring(1)"}`, CommitUnclassified},
		{"bash", `{"command":"echo 'puXsh'.Replace('X','')"}`, CommitUnclassified},
		{"bash", `{"command":"Invoke-Command {git push}"}`, CommitHuman},
		{"bash", `{"command":"Invoke-Command {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"icm {git push}"}`, CommitHuman},
		{"bash", `{"command":"icm {git status}"}`, CommitUnclassified},
		{"bash", `{"command":"echo {git push}"}`, CommitUnclassified},
		{"bash", `{"command":"git @'\npush\n'@"}`, CommitHuman},
		{"bash", `{"command":"git @'\nstatus\n'@"}`, CommitUnclassified},
		{"bash", `{"command":"git @\"\npush\n\"@"}`, CommitHuman},
		{"bash", `{"command":"git @\"\nstatus\n\"@"}`, CommitUnclassified},
		{"bash", `{"command":"git @\"\n$x\n\"@"}`, CommitHuman},
		{"bash", `{"command":"& @'\ngit\n'@ push"}`, CommitHuman},
		{"bash", `{"command":"& @'\ngit\n'@ status"}`, CommitUnclassified},
		{"bash", `{"command":"& @\"\ngit\n\"@ push"}`, CommitHuman},
		{"bash", `{"command":"& @'\nnotepad\n'@ push"}`, CommitUnclassified},
		{"bash", `{"command":"echo @'\ngit push\n'@"}`, CommitUnclassified},
		{"bash", `{"command":"git '  push'.TrimStart()"}`, CommitHuman},
		{"bash", `{"command":"git '  status'.TrimStart()"}`, CommitUnclassified},
		{"bash", `{"command":"git 'push  '.TrimEnd()"}`, CommitHuman},
		{"bash", `{"command":"git 'status  '.TrimEnd()"}`, CommitUnclassified},
		{"bash", `{"command":"git 'XXXpush'.Substring('XXX'.Length)"}`, CommitHuman},
		{"bash", `{"command":"git 'XXXstatus'.Substring('XXX'.Length)"}`, CommitUnclassified},
		{"bash", `{"command":"git -- push"}`, CommitHuman},
		{"bash", `{"command":"git -C repo -- push"}`, CommitHuman},
		{"bash", `{"command":"git status -- push"}`, CommitUnclassified},
		{"bash", `{"command":"git -- status"}`, CommitUnclassified},
		{"bash", `{"command":"git $'push'"}`, CommitHuman},
		{"bash", `{"command":"git $'status'"}`, CommitUnclassified},
		{"bash", `{"command":"git $'pu\\x73h'"}`, CommitHuman},
		{"bash", `{"command":"bash -c 'git push'"}`, CommitHuman},
		{"bash", `{"command":"bash -lc 'git push'"}`, CommitHuman},
		{"bash", `{"command":"npm test && git push"}`, CommitHuman},
		{"bash", `{"command":"npm test"}`, CommitUnclassified},
		{"bash", `{"command":"echo git push"}`, CommitUnclassified},
		{"bash", `{"command":"git status"}`, CommitUnclassified},
		{"bash", `{"command":"rm -rf /tmp/x"}`, CommitUnclassified},
		{"mailbox_send", `{"to":"a@b.c","body":"hello"}`, CommitHuman},
		{"im_message", `{}`, CommitHuman},
		{"send_file", `{"path":"notes.txt"}`, CommitHuman},
		{"send_file", `{}`, CommitUnclassified},
		{"send_file", `{"destination":"desktop"}`, CommitUnclassified},
		{"send_file", `{"destination":"当前对话"}`, CommitUnclassified},
		{"send_file", `{"destination":"wechat"}`, CommitHuman},
		{"send_file", `{"destination":"somewhere"}`, CommitHuman},
		{"send_file", `{"path":"report.pdf","destination":"desktop"}`, CommitHuman},
		{"send_to_im", `{}`, CommitHuman},
		{"delete_file", `{"path":` + jsonString(outside) + `}`, CommitHuman},
		{"delete_file", `{"path":"notes.txt"}`, CommitUnclassified},
		{"remove_file", `{}`, CommitHuman},
		{"move_file", `{"path":"a.txt","destination":` + jsonString(outside) + `}`, CommitHuman},
		{"edit_file", `{"path":"a.go"}`, CommitUnclassified},
		{"write_file", `{"path":".maclaw-tmp/a.txt"}`, CommitEngineSafe},
	}
	for _, tc := range cases {
		class, fp := classifyCommitCall(tc.name, tc.args, root, temp)
		if class != tc.class {
			t.Errorf("%s %s class=%v want %v fp=%q", tc.name, tc.args, class, tc.class, fp)
		}
		if strings.Contains(fp, "hello") || strings.Contains(fp, "git push") {
			t.Errorf("fingerprint stored a body: %q", fp)
		}
	}

	_, push := classifyCommitCall("Bash", `{"Command":"git push origin main"}`, root, temp)
	_, pushAgain := classifyCommitCall("bash", `{"command":"git push"}`, root, temp)
	if push != pushAgain {
		t.Fatalf("same shape fingerprints differ:\n%s\n%s", push, pushAgain)
	}
	class, changed := classifyCommitCall("bash", `{"command":"git push","model_safe":true}`, root, temp)
	if class != CommitHuman || changed == push {
		t.Fatalf("model_safe class=%v fp=%q original=%q", class, changed, push)
	}
}

func TestChatBoundSendFileDoesNotInheritAnExternalCommit(t *testing.T) {
	host := newCommitHost(t)
	aimed := `{"path":"report.pdf","destination":"wechat","forward_to_im":true}`
	class, fingerprint := classifyCommitCall("send_file", aimed, host.root, host.temp)
	if class != CommitHuman || fingerprint == "" {
		t.Fatalf("raw class=%v fp=%q", class, fingerprint)
	}
	host.ledger.Store(fingerprint, CommitDeny)
	host.checklist = true
	host.chatBound = true
	if got, blocked := ApplyCommitGate(host, "send_file", aimed); blocked || got.Result != "" {
		t.Fatalf("chat-bound send_file blocked=%v result=%q", blocked, got.Result)
	}
	if _, blocked := ApplyCommitGate(host, "send_to_im", `{}`); !blocked {
		t.Fatal("send_to_im stays an external send")
	}
	host.chatBound = false
	if _, blocked := ApplyCommitGate(host, "send_file", aimed); !blocked {
		t.Fatal("a send_file the host can aim stays human")
	}
}

func TestPowerShellEncodedGitPushIsHuman(t *testing.T) {
	root := t.TempDir()
	encoded := utf16LEBase64("git push")
	class, _ := classifyCommitCall("powershell", `{"command":`+jsonString("powershell -EncodedCommand "+encoded)+`}`, root, "")
	if class != CommitHuman {
		t.Fatalf("encoded push class=%v", class)
	}
	class, _ = classifyCommitCall("powershell", `{"command":`+jsonString("powershell -EncodedCommand:"+encoded)+`}`, root, "")
	if class != CommitHuman {
		t.Fatalf("attached encoded push class=%v", class)
	}
	class, _ = classifyCommitCall("powershell", `{"command":`+jsonString("powershell -EncodedCommand "+utf16LEBase64("\uFEFFgit push"))+`}`, root, "")
	if class != CommitHuman {
		t.Fatalf("encoded push with BOM class=%v", class)
	}
	status := utf16LEBase64("git status")
	class, _ = classifyCommitCall("powershell", `{"command":`+jsonString("powershell -EncodedCommand "+status)+`}`, root, "")
	if class != CommitUnclassified {
		t.Fatalf("encoded status class=%v", class)
	}
	class, _ = classifyCommitCall("powershell", `{"command":"powershell -EncodedCommand !!!"}`, root, "")
	if class != CommitHuman {
		t.Fatalf("unreadable encoded command class=%v", class)
	}
}

func utf16LEBase64(text string) string {
	units := utf16.Encode([]rune(text))
	raw := make([]byte, len(units)*2)
	for i, unit := range units {
		raw[i*2] = byte(unit)
		raw[i*2+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestDeepCommandSubstitutionAsksInsteadOfRecursingForever(t *testing.T) {
	command := "git status"
	for i := 0; i < 40; i++ {
		command = "$(" + command + ")"
	}
	class, _ := classifyCommitCall("bash", `{"command":`+jsonString("echo "+command)+`}`, t.TempDir(), "")
	if class != CommitHuman {
		t.Fatalf("deep substitution class=%v", class)
	}
}

func TestClassifyCreateIfAbsentNeedsARealWorkspace(t *testing.T) {
	root := t.TempDir()
	temp := filepath.Join(root, ".maclaw-tmp")
	class, _ := classifyCommitCall("write_file", `{"path":"new.txt"}`, root, temp)
	if class != CommitEngineSafe {
		t.Fatalf("missing workspace file class=%v", class)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	class, _ = classifyCommitCall("write_file", `{"path":"new.txt"}`, root, temp)
	if class != CommitUnclassified {
		t.Fatalf("existing file class=%v, want unclassified", class)
	}
	missing := filepath.Join(root, "missing-remote")
	class, _ = classifyCommitCall("write_file", `{"path":"new.txt"}`, missing, "")
	if class != CommitUnclassified {
		t.Fatalf("missing workspace class=%v, want unclassified", class)
	}
	fileAsRoot := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(fileAsRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	class, _ = classifyCommitCall("create_file", `{"path":"child.txt"}`, fileAsRoot, "")
	if class != CommitUnclassified {
		t.Fatalf("file workspace class=%v, want unclassified", class)
	}
}

func TestCommitPathClassificationResolvesSymlinksAndPlatformCase(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got := classifyCommitPath(filepath.Join(link, "new.txt"), root, ""); got != CommitPathOutside {
		t.Fatalf("symlink target class=%v, want outside", got)
	}
	if runtime.GOOS != "windows" {
		mixed := filepath.Join(root, "MiXeD")
		if err := os.MkdirAll(mixed, 0o755); err != nil {
			t.Fatal(err)
		}
		if pathWithin(mixed, filepath.Join(root, "mixed", "file.txt")) {
			t.Fatal("case-sensitive platform folded distinct paths")
		}
	}
}

func TestApplyCommitGateBlocksBeforeExecute(t *testing.T) {
	host := newCommitHost(t)
	got := executeAuthorizedLoopToolCall(host, "mailbox_send", `{"to":"a@b.c","body":"secret"}`, "call-1")
	if len(host.toolCalls) != 0 {
		t.Fatalf("blocked send executed: %#v", host.toolCalls)
	}
	if !IsAskUserResult(got.Result) || got.Outcome != ToolExecutionOutcomeError {
		t.Fatalf("result=%+v", got)
	}
	if strings.Contains(got.Result, "secret") || !strings.Contains(got.Result, "a@b.c") {
		t.Fatalf("ask hid the recipient or leaked the body: %s", got.Result)
	}
	ask, ok := ParseAskUserResult(got.Result)
	if !ok {
		t.Fatal("ask did not parse")
	}
	task, fp, nonce, ok := ParseCommitApprovalContext(ask.Context)
	if !ok || task != host.taskKey || nonce != "nonce-1" || fp != host.prepared {
		t.Fatalf("ticket task=%q fp=%q nonce=%q prepared=%q ok=%v", task, fp, nonce, host.prepared, ok)
	}
	shown := FormatAskUserForDisplay(ask)
	if strings.Contains(shown, CommitApprovalContextPrefix) || strings.Contains(shown, nonce) || !strings.Contains(shown, "仅这一次") {
		t.Fatalf("display leaked the ticket: %s", shown)
	}
	if history := FormatAskedUserHistoryResult(ask); strings.Contains(history, nonce) {
		t.Fatalf("history leaked the nonce: %s", history)
	}

	host.ledger.Store(fp, CommitDeny)
	denied := executeAuthorizedLoopToolCall(host, "mailbox_send", `{"body":"other","to":"c@d.e"}`, "call-2")
	if IsAskUserResult(denied.Result) || len(host.toolCalls) != 0 || !strings.Contains(denied.Result, "已拒绝") || !strings.Contains(denied.Result, "缺少人的批准") {
		t.Fatalf("deny result=%+v calls=%#v", denied, host.toolCalls)
	}
	again := executeAuthorizedLoopToolCall(host, "mailbox_send", `{"to":"z@y.x","body":"no"}`, "call-3")
	if IsAskUserResult(again.Result) || !strings.Contains(again.Result, "已拒绝") {
		t.Fatalf("deny asked again: %+v", again)
	}
}

func TestApplyCommitGateStoreFailureDoesNotExecute(t *testing.T) {
	host := newCommitHost(t)
	host.failStore = true
	host.resolve = func(string, string, string) (CommitDecision, bool) {
		return CommitAllowTask, true
	}
	args := `{"to":"a@b.c"}`
	got := executeAuthorizedLoopToolCall(host, "mailbox_send", args, "call-1")
	if len(host.toolCalls) != 0 || IsAskUserResult(got.Result) || !strings.Contains(got.Result, "无法可靠保存") {
		t.Fatalf("allow store failure result=%+v calls=%#v", got, host.toolCalls)
	}
	_, fp := classifyCommitCall("mailbox_send", args, host.root, host.temp)
	if _, ok := host.ledger.Lookup(fp); ok {
		t.Fatal("failed store left a decision")
	}

	once := newCommitHost(t)
	_, onceFP := classifyCommitCall("bash", `{"command":"git push"}`, once.root, once.temp)
	once.ledger.Store(onceFP, CommitAllowOnce)
	once.failStore = true
	consumed := executeAuthorizedLoopToolCall(once, "bash", `{"command":"git push"}`, "call-1")
	if len(once.toolCalls) != 0 || IsAskUserResult(consumed.Result) || !strings.Contains(consumed.Result, "无法可靠保存") {
		t.Fatalf("once store failure result=%+v calls=%#v", consumed, once.toolCalls)
	}
	if decision, ok := once.ledger.Lookup(onceFP); !ok || decision != CommitAllowOnce {
		t.Fatalf("once record after failed consume: %q ok=%v", decision, ok)
	}
}

func TestApplyCommitGateHostThatCannotPauseDoesNotAsk(t *testing.T) {
	host := newCommitHost(t)
	host.noPause = true
	got := executeAuthorizedLoopToolCall(host, "send_file", `{"path":"report.pdf"}`, "call-1")
	if len(host.toolCalls) != 0 || IsAskUserResult(got.Result) || !strings.Contains(got.Result, "先不执行") || host.prepared != "" {
		t.Fatalf("result=%+v prepared=%q calls=%#v", got, host.prepared, host.toolCalls)
	}
}

func TestApplyCommitGateFullControlSkipsHumanCall(t *testing.T) {
	host := newCommitHost(t)
	host.fullControl = true
	asks := 0
	host.resolve = func(string, string, string) (CommitDecision, bool) {
		asks++
		return "", false
	}
	_, fp := classifyCommitCall("mailbox_send", `{"to":"a@b.c","body":"secret"}`, host.root, host.temp)
	got := executeAuthorizedLoopToolCall(host, "mailbox_send", `{"to":"a@b.c","body":"secret"}`, "call-1")
	if got.Result != "ran" || len(host.toolCalls) != 1 || asks != 0 || host.prepared != "" {
		t.Fatalf("full control result=%+v asks=%d prepared=%q calls=%#v", got, asks, host.prepared, host.toolCalls)
	}
	if host.fullNotes != 1 {
		t.Fatalf("full control audit notes=%d, want 1", host.fullNotes)
	}
	if _, ok := host.ledger.Lookup(fp); ok {
		t.Fatal("full control stored a task decision")
	}
	// Leaving 完全控制 puts the card back.
	host.fullControl = false
	asked := executeAuthorizedLoopToolCall(host, "mailbox_send", `{"to":"z@y.x","body":"again"}`, "call-2")
	if !IsAskUserResult(asked.Result) || len(host.toolCalls) != 1 || asks != 1 || host.prepared != fp {
		t.Fatalf("request tier result=%+v asks=%d prepared=%q calls=%#v", asked, asks, host.prepared, host.toolCalls)
	}
	if host.fullNotes != 1 {
		t.Fatalf("request tier wrote a full-control note: %d", host.fullNotes)
	}

	// A one-call grant is not consumed while 完全控制 is on; it stays for the
	// next call after the tier drops back to a prompting mode.
	onceHost := newCommitHost(t)
	_, onceFP := classifyCommitCall("im_message", `{"text":"hi"}`, onceHost.root, onceHost.temp)
	onceHost.ledger.Store(onceFP, CommitAllowOnce)
	onceHost.fullControl = true
	if got := executeAuthorizedLoopToolCall(onceHost, "im_message", `{"text":"hi"}`, "call-1"); got.Result != "ran" || len(onceHost.toolCalls) != 1 {
		t.Fatalf("full control once result=%+v calls=%#v", got, onceHost.toolCalls)
	}
	if onceHost.fullNotes != 1 {
		t.Fatalf("full control once notes=%d, want 1", onceHost.fullNotes)
	}
	if decision, ok := onceHost.ledger.Lookup(onceFP); !ok || decision != CommitAllowOnce {
		t.Fatalf("full control consumed the once grant: %q ok=%v", decision, ok)
	}
	onceHost.fullControl = false
	if got := executeAuthorizedLoopToolCall(onceHost, "im_message", `{"text":"hi"}`, "call-2"); got.Result != "ran" || len(onceHost.toolCalls) != 2 {
		t.Fatalf("leftover once result=%+v calls=%#v", got, onceHost.toolCalls)
	}
	if onceHost.fullNotes != 1 {
		t.Fatalf("leftover once wrote another full-control note: %d", onceHost.fullNotes)
	}

	if _, ok := onceHost.ledger.Lookup(onceFP); ok {
		t.Fatal("leftover once grant stayed after use")
	}

	// A stored deny still refuses the call under full control.
	deniedHost := newCommitHost(t)
	deniedHost.fullControl = true
	_, deniedFP := classifyCommitCall("bash", `{"command":"git push"}`, deniedHost.root, deniedHost.temp)
	deniedHost.ledger.Store(deniedFP, CommitDeny)
	denied := executeAuthorizedLoopToolCall(deniedHost, "bash", `{"command":"git push"}`, "call-1")
	if IsAskUserResult(denied.Result) || len(deniedHost.toolCalls) != 0 || !strings.Contains(denied.Result, "已拒绝") {
		t.Fatalf("full control deny result=%+v calls=%#v", denied, deniedHost.toolCalls)
	}
	if deniedHost.fullNotes != 0 {
		t.Fatalf("deny wrote a full-control note: %d", deniedHost.fullNotes)
	}

	// An open checklist stays a hard refusal; it is not an approval card.
	checklistHost := newCommitHost(t)
	checklistHost.fullControl = true
	checklistHost.checklist = true
	blocked := executeAuthorizedLoopToolCall(checklistHost, "mailbox_send", `{"to":"a@b.c"}`, "call-1")
	if IsAskUserResult(blocked.Result) || len(checklistHost.toolCalls) != 0 || !strings.Contains(blocked.Result, "步骤清单") {
		t.Fatalf("full control checklist result=%+v calls=%#v", blocked, checklistHost.toolCalls)
	}
	if strings.Contains(blocked.Result, "人的批准") {
		t.Fatalf("full control checklist asked for a person: %s", blocked.Result)
	}
	if checklistHost.fullNotes != 0 {
		t.Fatalf("checklist wrote a full-control note: %d", checklistHost.fullNotes)
	}
}

func TestApplyCommitGateOnceIsConsumed(t *testing.T) {
	host := newCommitHost(t)
	_, fp := classifyCommitCall("bash", `{"command":"git push"}`, host.root, host.temp)
	host.ledger.Store(fp, CommitAllowOnce)
	got := executeAuthorizedLoopToolCall(host, "bash", `{"command":"git push origin"}`, "call-1")
	if got.Result != "ran" || len(host.toolCalls) != 1 {
		t.Fatalf("once result=%+v calls=%#v", got, host.toolCalls)
	}
	if _, ok := host.ledger.Lookup(fp); ok {
		t.Fatal("once approval was kept")
	}
	next := executeAuthorizedLoopToolCall(host, "bash", `{"command":"git push"}`, "call-2")
	if !IsAskUserResult(next.Result) || len(host.toolCalls) != 1 {
		t.Fatalf("second push result=%+v calls=%#v", next, host.toolCalls)
	}
}

func TestApplyCommitGateAllowsTaskAndOrdinaryEdits(t *testing.T) {
	host := newCommitHost(t)
	_, fp := classifyCommitCall("send_to_im", `{"text":"hi"}`, host.root, host.temp)
	host.ledger.Store(fp, CommitAllowTask)
	if got := executeAuthorizedLoopToolCall(host, "send_to_im", `{"text":"later"}`, "call-1"); got.Result != "ran" {
		t.Fatalf("task allow result=%+v", got)
	}
	if decision, ok := host.ledger.Lookup(fp); !ok || decision != CommitAllowTask {
		t.Fatalf("task allow was not kept: %q %v", decision, ok)
	}
	if got := executeAuthorizedLoopToolCall(host, "edit_file", `{"path":"a.go"}`, "call-2"); got.Result != "ran" || len(host.toolCalls) != 2 {
		t.Fatalf("edit result=%+v calls=%#v", got, host.toolCalls)
	}
	if got := executeAuthorizedLoopToolCall(host, "write_file", `{"path":".maclaw-tmp/a.txt","content":"draft"}`, "call-3"); got.Result != "ran" || IsAskUserResult(got.Result) {
		t.Fatalf("temp write result=%+v", got)
	}
	if len(host.toolCalls) != 3 {
		t.Fatalf("calls=%#v", host.toolCalls)
	}
}

func TestApplyCommitGateChecklistRefusesWithoutAsking(t *testing.T) {
	host := newCommitHost(t)
	host.checklist = true
	for _, call := range []struct{ name, args string }{
		{"mailbox_send", `{"to":"a@b.c"}`},
		{"bash", `{"command":"git push"}`},
	} {
		got := executeAuthorizedLoopToolCall(host, call.name, call.args, "call")
		if IsAskUserResult(got.Result) || !strings.Contains(got.Result, "步骤清单") || !strings.Contains(got.Result, "缺少人的批准") {
			t.Fatalf("%s checklist result=%+v", call.name, got)
		}
	}
	if host.prepared != "" || len(host.toolCalls) != 0 {
		t.Fatalf("checklist prepared=%q calls=%#v", host.prepared, host.toolCalls)
	}
	if got := executeAuthorizedLoopToolCall(host, "bash", `{"command":"npm test"}`, "call"); got.Result != "ran" || len(host.toolCalls) != 1 {
		t.Fatalf("npm test result=%+v calls=%#v", got, host.toolCalls)
	}
}

func TestApplyCommitGateResolverRunsTheSameCall(t *testing.T) {
	host := newCommitHost(t)
	var asks int
	host.resolve = func(toolName, fingerprint, subject string) (CommitDecision, bool) {
		asks++
		if toolName != "bash" || strings.Contains(fingerprint, "git push") || strings.Contains(fingerprint, "secret") || strings.Contains(subject, "secret") {
			t.Errorf("resolver saw tool=%q fingerprint=%q subject=%q", toolName, fingerprint, subject)
		}
		if asks == 1 && subject != "git push origin main" {
			t.Errorf("subject=%q", subject)
		}
		if asks == 1 {
			return CommitAllowOnce, true
		}
		return CommitDeny, true
	}
	first := executeAuthorizedLoopToolCall(host, "bash", `{"command":"git push origin main"}`, "call-1")
	if first.Result != "ran" || asks != 1 || len(host.toolCalls) != 1 || host.prepared != "" {
		t.Fatalf("once result=%+v asks=%d prepared=%q calls=%#v", first, asks, host.prepared, host.toolCalls)
	}
	second := executeAuthorizedLoopToolCall(host, "bash", `{"command":"git push origin other"}`, "call-2")
	if !strings.Contains(second.Result, "已拒绝") || IsAskUserResult(second.Result) || asks != 2 || len(host.toolCalls) != 1 {
		t.Fatalf("deny result=%+v asks=%d calls=%#v", second, asks, host.toolCalls)
	}
	third := executeAuthorizedLoopToolCall(host, "bash", `{"command":"git push"}`, "call-3")
	if !strings.Contains(third.Result, "已拒绝") || asks != 2 || len(host.toolCalls) != 1 {
		t.Fatalf("stored deny result=%+v asks=%d calls=%#v", third, asks, host.toolCalls)
	}

	taskHost := newCommitHost(t)
	taskAsks := 0
	taskHost.resolve = func(_, _, subject string) (CommitDecision, bool) {
		taskAsks++
		if subject != "a@b.c" || strings.Contains(subject, "secret") {
			t.Errorf("mailbox subject=%q", subject)
		}
		return CommitAllowTask, true
	}
	if got := executeAuthorizedLoopToolCall(taskHost, "mailbox_send", `{"to":"a@b.c","body":"secret"}`, "call-1"); got.Result != "ran" || taskAsks != 1 {
		t.Fatalf("task allow result=%+v asks=%d", got, taskAsks)
	}
	if got := executeAuthorizedLoopToolCall(taskHost, "mailbox_send", `{"body":"other","to":"z@y.x"}`, "call-2"); got.Result != "ran" || taskAsks != 1 || len(taskHost.toolCalls) != 2 {
		t.Fatalf("later task allow result=%+v asks=%d calls=%#v", got, taskAsks, taskHost.toolCalls)
	}

	stopped := newCommitHost(t)
	stopped.stopped = true
	stopped.resolve = func(string, string, string) (CommitDecision, bool) { return "", false }
	got := executeAuthorizedLoopToolCall(stopped, "im_message", `{"text":"hi"}`, "call")
	if IsAskUserResult(got.Result) || !strings.Contains(got.Result, "先不执行") || len(stopped.toolCalls) != 0 || stopped.prepared != "" {
		t.Fatalf("stopped result=%+v prepared=%q calls=%#v", got, stopped.prepared, stopped.toolCalls)
	}

	checklist := newCommitHost(t)
	checklist.checklist = true
	checklist.resolve = func(string, string, string) (CommitDecision, bool) {
		t.Fatal("open checklist asked the resolver")
		return CommitAllowTask, true
	}
	got = executeAuthorizedLoopToolCall(checklist, "bash", `{"command":"git push"}`, "call")
	if IsAskUserResult(got.Result) || !strings.Contains(got.Result, "步骤清单") || len(checklist.toolCalls) != 0 {
		t.Fatalf("checklist result=%+v calls=%#v", got, checklist.toolCalls)
	}
}

func TestApplyCommitGateLeavesUngatedHostsAlone(t *testing.T) {
	cb := &mockCallbacks{toolResult: "ok"}
	got := executeAuthorizedLoopToolCall(cb, "mailbox_send", `{"to":"a@b.c"}`, "call-1")
	if got.Result != "ok" || len(cb.toolCalls) != 1 {
		t.Fatalf("ungated result=%+v calls=%#v", got, cb.toolCalls)
	}
}

func TestIterationContractReminderDoesNotMutateHistory(t *testing.T) {
	system := map[string]string{"role": "system", "content": "base prompt"}
	conversation := []interface{}{system, map[string]string{"role": "user", "content": "hi"}}
	plain := &mockCallbacks{}
	if got := withIterationContractReminder(plain, conversation); system["content"] != "base prompt" || got[0].(map[string]string)["content"] != "base prompt" {
		t.Fatalf("plain reminder changed the prompt: %#v", got[0])
	}

	source := &contractCallbacks{goal: "写一份很长的报告\n第二行" + strings.Repeat("啊", 200), missing: []string{"document.generate.file", "document.generate.file", "report_path", "shell.execute.remote_host"}, locked: []string{"mailbox_send", "git push"}}
	out := withIterationContractReminder(source, conversation)
	if system["content"] != "base prompt" {
		t.Fatalf("stored system prompt changed: %q", system["content"])
	}
	content := out[0].(map[string]string)["content"]
	if !strings.Contains(content, "[本轮契约]") || !strings.Contains(content, "生成的文件") || !strings.Contains(content, "report_path") || !strings.Contains(content, "git push") || !strings.Contains(content, "ssh 命令") || strings.Contains(content, "shell.execute.remote_host") || strings.Contains(content, "ssh 的 host") {
		t.Fatalf("reminder=%s", content)
	}
	if strings.Contains(content, "\n第二行") || strings.Count(content, "[本轮契约]") != 1 || strings.Count(content, "啊") >= 200 {
		t.Fatalf("reminder stacked, kept a newline, or did not truncate: %s", content)
	}
	again := withIterationContractReminder(source, conversation)
	if strings.Count(again[0].(map[string]string)["content"], "[本轮契约]") != 1 || system["content"] != "base prompt" {
		t.Fatal("rebuilding the reminder stacked it onto stored history")
	}

	iface := map[string]interface{}{"role": "system", "content": "iface"}
	ifaceConv := []interface{}{iface}
	out = withIterationContractReminder(source, ifaceConv)
	if iface["content"] != "iface" || !strings.Contains(out[0].(map[string]interface{})["content"].(string), "[本轮契约]") {
		t.Fatalf("interface system map was not cloned: %#v", iface)
	}

	empty := &contractCallbacks{}
	if got := withIterationContractReminder(empty, conversation); got[0].(map[string]string)["content"] != "base prompt" {
		t.Fatalf("empty contract appended: %#v", got[0])
	}
}

type contractCallbacks struct {
	mockCallbacks
	goal    string
	missing []string
	locked  []string
}

func (c *contractCallbacks) IterationContract() (string, []string, []string) {
	return c.goal, c.missing, c.locked
}

func TestCommitAskSubjectShowsTheCallAndHidesBodies(t *testing.T) {
	if got := CommitAskSubject("bash", `{"command":"git push origin main"}`); got != "git push origin main" {
		t.Fatalf("push subject=%q", got)
	}
	if got := CommitAskSubject("mailbox_send", `{"to":"a@b.c","body":"secret"}`); got != "a@b.c" {
		t.Fatalf("mailbox subject=%q", got)
	}
	if got := CommitAskSubject("im_message", `{"text":"hi there"}`); got != "" {
		t.Fatalf("im subject leaked the text: %q", got)
	}
	if got := CommitAskSubject("im_message", `{"action":"send","text":"你好","message":"秘密","group_name":"项目群"}`); got != "项目群" || strings.Contains(got, "你好") || strings.Contains(got, "秘密") {
		t.Fatalf("im target subject=%q", got)
	}
	if got := CommitAskSubject("im_message", `{"action":"list_targets","channel":"lansenger","query":"项目","text":"秘密"}`); got != "list_targets lansenger 项目" || strings.Contains(got, "秘密") {
		t.Fatalf("im lookup subject=%q", got)
	}
	mail := `{"action":"send","to":["a@ex.com","b@ex.com"],"cc":["c@ex.com"],"subject":"Hello","text":"secret body","html":"<b>secret</b>"}`
	if got := CommitAskSubject("mailbox_send", mail); got != "a@ex.com, b@ex.com 抄送 c@ex.com · Hello" || strings.Contains(got, "secret") {
		t.Fatalf("mailbox list subject=%q", got)
	}
	if got := CommitAskSubject("mailbox_send", `{"action":"reply","uid":7,"text":"secret"}`); got != "reply 7" || strings.Contains(got, "secret") {
		t.Fatalf("reply subject=%q", got)
	}
	if got := CommitAskSubject("bash", `{"command":"git push","working_dir":"D:\\repo"}`); got != `git push · D:\repo` {
		t.Fatalf("cwd subject=%q", got)
	}
	if got := CommitAskSubject("send_file", `{"path":"report.pdf","group_name":"项目群","text":"secret"}`); got != "report.pdf → 项目群" || strings.Contains(got, "secret") {
		t.Fatalf("send_file subject=%q", got)
	}
	if got := CommitAskSubject("send_file", `{"path":"report.pdf","destination":"wechat","message":"secret","caption":"hidden"}`); got != "report.pdf → wechat" || strings.Contains(got, "secret") || strings.Contains(got, "hidden") {
		t.Fatalf("send destination subject=%q", got)
	}
	if got := CommitAskSubject("send_to_im", `{"path":"report.pdf","forward_to_im":true,"caption":"secret"}`); got != "report.pdf → im" || strings.Contains(got, "secret") {
		t.Fatalf("send forward subject=%q", got)
	}
	if got := CommitAskSubject("send_file", `{"path":"report.pdf","destination":"desktop","forward_to_im":true}`); got != "report.pdf → desktop" {
		t.Fatalf("desktop destination subject=%q", got)
	}
	if got := CommitAskSubject("send_file", `{"path":"report.pdf","destination":"somewhere"}`); got != "report.pdf" {
		t.Fatalf("unknown destination subject=%q", got)
	}
	if got := CommitAskSubject("send_file", `{"path":"report.pdf","destination":"somewhere","forward_to_im":true}`); got != "report.pdf → im" {
		t.Fatalf("unknown destination with flag subject=%q", got)
	}
	if got := CommitAskSubject("send_to_im", `{"path":"report.pdf","destination":"desktop"}`); got != "report.pdf → im" {
		t.Fatalf("send_to_im desktop subject=%q", got)
	}
	if got := CommitAskSubject("send_file", `{"path":"report.pdf","destination":"im","channel":"weixin","forward_to_im":true,"message":"secret"}`); got != "report.pdf → weixin" || strings.Contains(got, "secret") {
		t.Fatalf("channel subject=%q", got)
	}
	if got := CommitAskSubject("delete_file", `{"path":"C:\\outside\\note.txt"}`); got != `C:\outside\note.txt` {
		t.Fatalf("delete subject=%q", got)
	}
	if got := CommitAskSubject("move_file", `{"path":"a.txt","destination":"b.txt"}`); got != "a.txt → b.txt" {
		t.Fatalf("move subject=%q", got)
	}
	long := strings.Repeat("a", 400)
	raw, err := json.Marshal(map[string]string{"command": "git push " + long})
	if err != nil {
		t.Fatal(err)
	}
	got := CommitAskSubject("bash", string(raw))
	if strings.Contains(got, "\n") || len([]rune(got)) != 160 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated subject len=%d value=%q", len([]rune(got)), got)
	}
}

func TestForceSendFileToIMOverridesDesktop(t *testing.T) {
	args := map[string]interface{}{"path": "a.pdf", "destination": "desktop", "forward_to_im": true}
	ForceSendFileToIM(args)
	if !SendFileForwardsToIM(args) {
		t.Fatal("desktop destination stayed in the chat")
	}
	if dest, ok := SendFileDestinationArg(args); !ok || dest != "im" {
		t.Fatalf("destination=%q ok=%v", dest, ok)
	}
	kept := map[string]interface{}{"destination": "wechat"}
	ForceSendFileToIM(kept)
	if dest, ok := SendFileDestinationArg(kept); !ok || dest != "wechat" || !SendFileForwardsToIM(kept) {
		t.Fatalf("wechat destination=%q ok=%v", dest, ok)
	}
}

func jsonString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(raw)
}
