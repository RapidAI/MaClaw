param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^COM\d+$')]
    [string]$Port
)

$ErrorActionPreference = 'Stop'

if (-not ([System.IO.Ports.SerialPort]::GetPortNames() -contains $Port)) {
    throw "Serial port not found: $Port"
}

$serial = [System.IO.Ports.SerialPort]::new($Port, 115200, 'None', 8, 'One')
$serial.DtrEnable = $false
$serial.RtsEnable = $false
$serial.Open()

try {
    Start-Sleep -Milliseconds 100
    $serial.RtsEnable = $true
    Start-Sleep -Milliseconds 120
    $serial.RtsEnable = $false
    Start-Sleep -Milliseconds 120
} finally {
    if ($serial.IsOpen) {
        $serial.Close()
    }
    $serial.Dispose()
}
