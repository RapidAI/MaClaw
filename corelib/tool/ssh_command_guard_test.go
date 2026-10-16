package tool

import "testing"

func TestRejectRawSSHCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "ssh direct", command: "ssh root@example.com", want: true},
		{name: "ssh after separator", command: "echo ok; ssh root@example.com", want: true},
		{name: "ssh via command builtin", command: "command ssh root@example.com", want: true},
		{name: "ssh via exec", command: "exec ssh root@example.com", want: true},
		{name: "ssh via env", command: "env TERM=xterm ssh root@example.com", want: true},
		{name: "ssh via env option", command: "env -i TERM=xterm ssh root@example.com", want: true},
		{name: "ssh via timeout", command: "timeout 5 ssh root@example.com", want: true},
		{name: "ssh via nohup", command: "nohup ssh root@example.com", want: true},
		{name: "scp direct", command: "scp app root@example.com:/tmp/app", want: true},
		{name: "sftp direct", command: "sftp root@example.com", want: true},
		{name: "rsync remote destination", command: "rsync -az build/ root@example.com:/srv/app", want: true},
		{name: "rsync remote source", command: "rsync root@example.com:/srv/app ./backup", want: true},
		{name: "rsync host relative destination", command: "rsync app.tar example.com:app.tar", want: true},
		{name: "rsync url remote", command: "rsync rsync://example.com/module ./module", want: true},
		{name: "rsync daemon remote", command: "rsync rsync.example.com::module ./module", want: true},
		{name: "rsync via env", command: "env RSYNC_RSH=ssh rsync -az build/ root@example.com:/srv/app", want: true},
		{name: "ssh exe", command: "ssh.exe root@example.com", want: true},
		{name: "nested bash quoted", command: "bash -lc 'ssh root@example.com uptime'", want: true},
		{name: "nested bash unquoted", command: "bash -lc ssh root@example.com uptime", want: true},
		{name: "nested powershell quoted", command: "powershell -Command \"ssh root@example.com\"", want: true},
		{name: "nested powershell unquoted", command: "powershell -Command ssh root@example.com", want: true},
		{name: "nested cmd", command: "cmd /c ssh root@example.com", want: true},
		{name: "nested wrapper", command: "bash -lc 'env TERM=xterm ssh root@example.com uptime'", want: true},
		{name: "nested rsync remote", command: "bash -lc 'rsync -az build/ root@example.com:/srv/app'", want: true},
		{name: "nested full windows powershell path", command: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -Command ssh root@example.com`, want: true},
		{name: "nested full unix bash path", command: "/bin/bash -lc ssh root@example.com", want: true},
		{name: "ssh keygen allowed", command: "ssh-keygen -t ed25519", want: false},
		{name: "local rsync allowed", command: "rsync -az ./src/ ./dst/", want: false},
		{name: "local relative colon rsync allowed", command: "rsync ./foo:bar ./dst/", want: false},
		{name: "local parent colon rsync allowed", command: "rsync ../foo:bar ./dst/", want: false},
		{name: "local absolute colon rsync allowed", command: "rsync /tmp/foo:bar ./dst/", want: false},
		{name: "windows local rsync allowed", command: `rsync C:\tmp\src\ C:\tmp\dst\`, want: false},
		{name: "plain echo allowed", command: "echo ssh root@example.com", want: false},
		{name: "plain echo rsync allowed", command: "echo rsync root@example.com:/srv/app", want: false},
		{name: "env var named ssh allowed", command: "env ssh=disabled echo ok", want: false},
		{name: "nested echo allowed", command: "bash -lc 'echo ssh root@example.com'", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := RejectRawSSHCommand(tt.command)
			if got != tt.want {
				t.Fatalf("RejectRawSSHCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestRejectBroadBrowserKillCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "taskkill chrome", command: "taskkill /f /im chrome.exe", want: true},
		{name: "taskkill chromium chained", command: "echo ok; taskkill /F /IM chromium.exe", want: true},
		{name: "wmic chrome delete", command: `wmic process where name='chrome.exe' delete`, want: true},
		{name: "nested cmd wmic chrome delete with redirection", command: `cmd /c "wmic process where name='chrome.exe' delete 2>nul & sc query type=process state=all | findstr /i chrome"`, want: true},
		{name: "powershell get-process stop", command: "Get-Process chrome -ErrorAction SilentlyContinue | Stop-Process -Force", want: true},
		{name: "powershell get-process stop with error action", command: "Get-Process chrome -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue", want: true},
		{name: "powershell stop-process name", command: "Stop-Process -Name msedge -Force", want: true},
		{name: "nested cmd", command: `cmd /c "taskkill /f /im chrome.exe"`, want: true},
		{name: "multi taskkill from log", command: `taskkill /f /im chrome.exe 2>nul; taskkill /f /im chromium.exe 2>nul; echo "killed"`, want: true},
		{name: "inspect chrome allowed", command: "tasklist | findstr chrome", want: false},
		{name: "kill scoped pid allowed", command: "taskkill /F /T /PID 1234", want: false},
		{name: "stop scoped pid allowed", command: "Stop-Process -Id 1234 -Force", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := RejectBroadBrowserKillCommand(tt.command)
			if got != tt.want {
				t.Fatalf("RejectBroadBrowserKillCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestRejectBrowserSideEffectHTTPCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "zhihu publish curl", command: `curl.exe -X POST "https://zhuanlan.zhihu.com/api/articles/123/publish" -H "x-xsrftoken: token" -b "_xsrf=token; z_c0=abc" -d "{}"`, want: true},
		{name: "zhihu pins curl", command: `curl -X POST https://www.zhihu.com/api/v4/pins -H "x-csrftoken: token" --data-raw "{}"`, want: true},
		{name: "nested powershell", command: `powershell -Command "Invoke-RestMethod -Method POST https://www.zhihu.com/api/v4/pins -Headers @{ 'x-csrftoken'='t' }"`, want: true},
		{name: "generic cookie post", command: `curl -X POST https://social.example/api/publish -H "Cookie: SESSIONID=abc" --data-raw "{}"`, want: true},
		{name: "generic cookie header post", command: `curl -X POST https://social.example/api/publish -H "Cookie: sid=abc" --data-raw "{}"`, want: true},
		{name: "generic cookie long header post", command: `curl --request POST https://social.example/api/publish --header "Cookie: sid=abc" --data "{}"`, want: true},
		{name: "api authorization post allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Basic abc" --data-raw "{}"`, want: false},
		{name: "api bearer patch allowed", command: `Invoke-RestMethod -Method PATCH https://api.example/v1/item -Headers @{ Authorization='Bearer token' } -Body '{}'`, want: false},
		{name: "powershell cookie header post", command: `Invoke-WebRequest -Uri https://social.example/api/publish -Method 'POST' -Headers @{ Cookie='sid=abc' } -Body '{}'`, want: true},
		{name: "powershell spaced hashtable cookie", command: `Invoke-WebRequest -Uri https://social.example/api/publish -Method POST -Headers @{ Accept = 'json'; Cookie = 'sid=abc' } -Body '{}'`, want: true},
		{name: "powershell spaced csrf header", command: `Invoke-RestMethod -Method POST https://www.zhihu.com/api/v4/pins -Headers @{ "x-csrftoken" = "t" }`, want: true},
		{name: "powershell spaced bearer allowed", command: `Invoke-RestMethod -Method POST https://api.example/v1/videos -Headers @{ Authorization = "Bearer token"; "Content-Type" = "application/json" } -Body '{}'`, want: false},
		{name: "bash brace group keeps ffmpeg apart", command: `{ curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{}"; ffmpeg -i in.mp4 -b 1M out.mp4; }`, want: false},
		{name: "api bearer post allowed", command: `curl -XPOST https://api.agnes-ai.cn/v1/videos --header "Authorization: Bearer token" --data '{}'`, want: false},
		{name: "bearer plus cookie still rejected", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -b "sid=abc" --data '{}'`, want: true},
		{name: "api post body mentions csrf allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{\"prompt\":\"csrf review\"}"`, want: false},
		{name: "api post body mentions cookie colon allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{\"prompt\":\"cookie: chocolate\"}"`, want: false},
		{name: "cookie jar save allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" --cookie-jar cookies.txt -d "{}"`, want: false},
		{name: "wget load cookies rejected", command: `wget --load-cookies cookies.txt --post-data "{}" https://social.example/api/publish`, want: true},
		{name: "curl ascii flag is not a cookie", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -B -d "{}"`, want: false},
		{name: "json mentions document cookie allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d '{"script":"document.cookie=1"}'`, want: false},
		{name: "json mentions dash b allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d '{"prompt":"use -b flag"}'`, want: false},
		{name: "glued cookie short flag", command: `curl -bcookies.txt -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "glued cookie short flag quotes", command: `curl -b"sid=abc" -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "glued header equals", command: `curl --header=Cookie:sid=abc -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "glued capital h", command: `curl -H"Cookie: sid" -X POST https://social.example/api/publish --data "{}"`, want: true},
		{name: "ffmpeg bitrate beside api post allowed", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{}" && ffmpeg -i in.mp4 -b:v 1M -b 1M out.mp4`, want: false},
		{name: "cookie get does not poison api post", command: `curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{}" && curl -b cookies.txt https://example.com/home`, want: false},
		{name: "clustered curl cookie short flag", command: `curl -fsSLb cookies.txt -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "clustered curl header", command: `curl -sHCookie:sid=abc -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "glued output and body are not cookies", command: `curl -obanner.mp4 -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -dbody.json`, want: false},
		{name: "wget background is not a cookie", command: `wget -b --header="Authorization: Bearer token" --post-data "{}" https://api.example/v1/videos`, want: false},
		{name: "env wrapper still sees curl cookie", command: `env -i TERM=xterm curl -b cookies.txt -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "clustered data flag still posts", command: `curl -sd "{}" -b cookies.txt https://social.example/api/publish`, want: true},
		{name: "get with query data stays idempotent", command: `curl -Gd "q=1" -b cookies.txt https://social.example/api/search`, want: false},
		{name: "curl form cookie post", command: `curl -F "text=hi" -b cookies.txt https://social.example/api/publish`, want: true},
		{name: "curl form bearer upload allowed", command: `curl -F "file=@clip.mp4" -H "Authorization: Bearer token" https://api.example/v1/videos`, want: false},
		{name: "clustered form cookie post", command: `curl -sFtext=hi -b cookies.txt https://social.example/api/publish`, want: true},
		{name: "curl upload cookie put", command: `curl -T clip.mp4 -b cookies.txt https://social.example/api/upload`, want: true},
		{name: "wget timeout is not an upload", command: `wget -T 10 --load-cookies cookies.txt https://example.com/file`, want: false},
		{name: "sudo user still sees curl cookie", command: `sudo -u root curl -b cookies.txt -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "sudo user bearer api post allowed", command: `sudo -u root curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{}"`, want: false},
		{name: "stdbuf mode still sees curl cookie", command: `stdbuf -o L curl -b cookies.txt -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "timeout wrapper still sees curl cookie", command: `timeout 5 curl -b cookies.txt -X POST https://social.example/api/publish -d "{}"`, want: true},
		{name: "sudo bearer api post allowed", command: `sudo curl -X POST https://api.example/v1/videos -H "Authorization: Bearer token" -d "{}"`, want: false},
		{name: "zhihu read allowed", command: `curl.exe "https://www.zhihu.com/api/v4/me"`, want: false},
		{name: "public post elsewhere allowed", command: `curl -X POST https://example.com/api -d "{}"`, want: false},
		{name: "authed get allowed", command: `curl https://example.com/api/me -H "Authorization: Bearer token"`, want: false},
		{name: "public method string allowed without url", command: `echo "-Method POST Cookie=sid"`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := RejectBrowserSideEffectHTTPCommand(tt.command)
			if got != tt.want {
				t.Fatalf("RejectBrowserSideEffectHTTPCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestRejectShellBrowserAutomationCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "playwright cli", command: "npx playwright test", want: true},
		{name: "python playwright module", command: "python -m playwright install chromium", want: true},
		{name: "connect over cdp script", command: `python -c "from playwright.sync_api import sync_playwright; p.chromium.connect_over_cdp('http://127.0.0.1:3888')"`, want: true},
		{name: "puppeteer require", command: `node -e "require('puppeteer').launch()"`, want: true},
		{name: "selenium import", command: `python -c "from selenium import webdriver; webdriver.Chrome()"`, want: true},
		{name: "screenshot flag", command: "node run-playwright.js --screenshot", want: true},
		{name: "nested powershell", command: `powershell -Command "npx playwright test"`, want: true},
		{name: "remote debugging launch", command: `chrome.exe --remote-debugging-port=3888`, want: true},
		{name: "search allowed", command: "rg -n playwright gui corelib", want: false},
		{name: "marker search allowed", command: "rg -n connect_over_cdp gui corelib", want: false},
		{name: "marker grep allowed", command: "grep -R --line-number --screenshot gui", want: false},
		{name: "marker select-string allowed", command: "Get-Content post.py | Select-String connect_over_cdp", want: false},
		{name: "install dependency allowed", command: "npm install playwright", want: false},
		{name: "marker echo allowed", command: "echo connect_over_cdp", want: false},
		{name: "script with marker rejected", command: "python post.py --screenshot", want: true},
		{name: "script path rejected", command: "./run-playwright.js", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := RejectShellBrowserAutomationCommand(tt.command)
			if got != tt.want {
				t.Fatalf("RejectShellBrowserAutomationCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
