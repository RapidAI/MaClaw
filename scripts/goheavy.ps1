# goheavy.ps1 - cross-process throttle for heavy Go commands (go test / go build).
#
# Several dev/agent threads running `go test ./guiapp` (or `go test ./...`)
# concurrently can push this 32 GB machine into an unresponsive overload:
# each run compiles/links a ~1.6 GB / 2300-file package (multi-GB peak) and the
# test binary itself may use multiple GB. This wrapper admits at most
# $Slots heavy commands at once across ALL processes in this user session,
# and caps the child's Go runtime footprint via GOMEMLIMIT / GOMAXPROCS.
#
# Usage (from cmd/git-bash):  scripts\goheavy.cmd go test ./guiapp -run Foo
# Slots default 2; override with MACLAW_HEAVY_SLOTS (e.g. 1 to serialize fully).
# Caps default GOMEMLIMIT=8GiB / GOMAXPROCS=8; override with
# MACLAW_HEAVY_GOMEMLIMIT / MACLAW_HEAVY_GOMAXPROCS ("off" disables).
# The gated command also runs at BelowNormal process priority (inherited by
# the go toolchain, compiler, and test binaries) so a full test run cannot
# starve the desktop/IDE. Override with MACLAW_HEAVY_PRIORITY (e.g. Idle,
# Normal; "off" keeps the caller's priority).

param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Cmd
)

$ErrorActionPreference = 'Stop'

if (-not $Cmd -or $Cmd.Count -eq 0) {
    Write-Host 'usage: goheavy <command> [args...]   (e.g. goheavy go test ./guiapp)'
    exit 2
}

$slots = 2
if ($env:MACLAW_HEAVY_SLOTS -match '^\d+$') { $slots = [int]$env:MACLAW_HEAVY_SLOTS }
if ($slots -lt 1) { $slots = 1 }

$waitTimeoutSec = 7200
if ($env:MACLAW_HEAVY_WAIT_TIMEOUT_SEC -match '^\d+$') { $waitTimeoutSec = [int]$env:MACLAW_HEAVY_WAIT_TIMEOUT_SEC }

$lockDir = Join-Path $env:LOCALAPPDATA 'MaclawHeavyLocks'
New-Item -ItemType Directory -Force -Path $lockDir | Out-Null

function Test-OwnerAlive([string]$slotFile) {
    try {
        $owner = Get-Content $slotFile -Raw | ConvertFrom-Json
        $null = Get-Process -Id ([int]$owner.pid) -ErrorAction Stop
        return $true
    } catch {
        return $false
    }
}

$mySlotFile = $null
$deadline = (Get-Date).AddSeconds($waitTimeoutSec)
$lastNotice = [DateTime]::MinValue

while ($true) {
    for ($i = 1; $i -le $slots -and -not $mySlotFile; $i++) {
        $f = Join-Path $lockDir "slot-$i.lock"
        if ((Test-Path $f) -and -not (Test-OwnerAlive $f)) {
            # Owner process died without cleanup; reclaim the stale slot.
            Remove-Item $f -Force -ErrorAction SilentlyContinue
        }
        try {
            $fs = [System.IO.File]::Open($f, 'CreateNew', 'Write', 'None')
            $json = (@{ pid = $PID; started = (Get-Date).ToString('o'); cmd = ($Cmd -join ' ') } | ConvertTo-Json -Compress)
            $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
            $fs.Write($bytes, 0, $bytes.Length)
            $fs.Close()
            $mySlotFile = $f
        } catch [System.IO.IOException] {
            # Slot taken; try next.
        }
    }
    if ($mySlotFile) { break }
    if ((Get-Date) -gt $deadline) {
        Write-Host "[goheavy] timed out after ${waitTimeoutSec}s waiting for a heavy-command slot"
        exit 3
    }
    if ((Get-Date) - $lastNotice -gt [TimeSpan]::FromSeconds(30)) {
        $lastNotice = Get-Date
        Write-Host "[goheavy] waiting for a heavy-command slot ($slots max): $($Cmd -join ' ')"
    }
    Start-Sleep -Milliseconds 500
}

Write-Host "[goheavy] acquired slot $(Split-Path $mySlotFile -Leaf): $($Cmd -join ' ')"

# Cap the child's Go runtime footprint (Go toolchain and test binaries honor both).
if (-not $env:GOMEMLIMIT) {
    $cap = if ($env:MACLAW_HEAVY_GOMEMLIMIT) { $env:MACLAW_HEAVY_GOMEMLIMIT } else { '8GiB' }
    if ($cap -ne 'off') { $env:GOMEMLIMIT = $cap }
}
if (-not $env:GOMAXPROCS) {
    $procs = if ($env:MACLAW_HEAVY_GOMAXPROCS) { $env:MACLAW_HEAVY_GOMAXPROCS } else { '8' }
    if ($procs -ne 'off') { $env:GOMAXPROCS = $procs }
}

# Keep the host responsive: on Windows, child processes inherit the parent's
# priority class, so lowering this process drops the go toolchain, compiler,
# linker, and every spawned test binary (including GO_WANT_HELPER_PROCESS
# re-execs) below interactive work. A saturating test run then competes only
# for leftover CPU instead of freezing the desktop.
$priority = if ($env:MACLAW_HEAVY_PRIORITY) { $env:MACLAW_HEAVY_PRIORITY } else { 'BelowNormal' }
$prevPriority = $null
$self = $null
if ($priority -ne 'off') {
    try {
        $self = [System.Diagnostics.Process]::GetCurrentProcess()
        $prevPriority = $self.PriorityClass
        $self.PriorityClass = [System.Diagnostics.ProcessPriorityClass]::$priority
    } catch {
        Write-Host "[goheavy] could not set process priority to '$priority': $($_.Exception.Message)"
        $self = $null
    }
}

$exitCode = 0
try {
    $exe = $Cmd[0]
    $exeArgs = @(if ($Cmd.Count -gt 1) { $Cmd[1..($Cmd.Count - 1)] })
    & $exe @exeArgs
    $exitCode = $LASTEXITCODE
    if ($null -eq $exitCode) { $exitCode = 0 }
} finally {
    if ($self -and $prevPriority) {
        try { $self.PriorityClass = $prevPriority } catch {}
    }
    Remove-Item $mySlotFile -Force -ErrorAction SilentlyContinue
}
exit $exitCode
