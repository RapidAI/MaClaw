<#
.SYNOPSIS
    Send wake-up commands to multiple ESP32 devices on Windows serial ports.

.DESCRIPTION
    The devices on COM3 through COM6 may use different firmware protocols, so
    each device has an independent wake-up command.  Edit $Devices below to
    match the command expected by each device.  By default the script only
    writes a serial command; it does not toggle DTR/RTS, which can reset some
    ESP32 development boards.

.EXAMPLE
    .\scripts\wake-esp32-devices.ps1

.EXAMPLE
    .\scripts\wake-esp32-devices.ps1 -Port COM3,COM5 -ReadResponse
#>
[CmdletBinding()]
param(
    [string[]] $Port = @("COM3", "COM4", "COM5", "COM6"),
    [switch] $ReadResponse,
    [int] $ResponseTimeoutMs = 800
)

$ErrorActionPreference = "Stop"

# Configure these values for the firmware running on each ESP32.
# WakeCommand accepts plain text or escaped sequences such as "wake\r\n".
$Devices = @(
    [pscustomobject]@{ Port = "COM3"; Name = "ESP32-1"; BaudRate = 115200; WakeCommand = "wake\r\n" },
    [pscustomobject]@{ Port = "COM4"; Name = "ESP32-2"; BaudRate = 115200; WakeCommand = "wake\r\n" },
    [pscustomobject]@{ Port = "COM5"; Name = "ESP32-3"; BaudRate = 115200; WakeCommand = "wake\r\n" },
    [pscustomobject]@{ Port = "COM6"; Name = "ESP32-4"; BaudRate = 115200; WakeCommand = "wake\r\n" }
)

function Convert-WakeCommand([string] $Value) {
    return $Value.Replace("\r", "`r").Replace("\n", "`n").Replace("\t", "`t")
}

$selectedPorts = $Port | ForEach-Object { $_.Trim().ToUpperInvariant() }
$targets = $Devices | Where-Object { $selectedPorts -contains $_.Port.ToUpperInvariant() }
$unknownPorts = $selectedPorts | Where-Object { $_ -notin ($Devices.Port | ForEach-Object { $_.ToUpperInvariant() }) }

foreach ($unknownPort in $unknownPorts) {
    Write-Warning "Unconfigured port: $unknownPort"
}

foreach ($device in $targets) {
    $serialPort = [System.IO.Ports.SerialPort]::new(
        $device.Port,
        $device.BaudRate,
        [System.IO.Ports.Parity]::None,
        8,
        [System.IO.Ports.StopBits]::One
    )
    $serialPort.Handshake = [System.IO.Ports.Handshake]::None
    $serialPort.NewLine = "`n"
    $serialPort.ReadTimeout = $ResponseTimeoutMs
    $serialPort.WriteTimeout = 1000

    try {
        $serialPort.Open()
        Start-Sleep -Milliseconds 100
        $command = Convert-WakeCommand $device.WakeCommand
        $serialPort.Write($command)
        Write-Host "[$($device.Port) / $($device.Name)] Wake command sent: $($device.WakeCommand)" -ForegroundColor Green

        if ($ReadResponse) {
            try {
                $response = $serialPort.ReadExisting()
                if ([string]::IsNullOrWhiteSpace($response)) {
                    Start-Sleep -Milliseconds $ResponseTimeoutMs
                    $response = $serialPort.ReadExisting()
                }
                if ([string]::IsNullOrWhiteSpace($response)) {
                    Write-Host "[$($device.Port) / $($device.Name)] No response received" -ForegroundColor Yellow
                } else {
                    Write-Host "[$($device.Port) / $($device.Name)] Response: $($response.Trim())" -ForegroundColor Cyan
                }
            } catch [System.TimeoutException] {
                Write-Host "[$($device.Port) / $($device.Name)] No response received" -ForegroundColor Yellow
            }
        }
    } catch {
        Write-Warning "[$($device.Port) / $($device.Name)] Wake failed: $($_.Exception.Message)"
    } finally {
        if ($serialPort.IsOpen) {
            $serialPort.Close()
        }
        $serialPort.Dispose()
    }
}
