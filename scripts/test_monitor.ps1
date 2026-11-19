$mem = Get-CimInstance Win32_OperatingSystem
$freeGB = [math]::Round($mem.FreePhysicalMemory / 1MB, 1)
$cpu = (Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average
$tests = Get-CimInstance Win32_Process | Where-Object { $_.Name -match '\.test\.exe$' -or $_.Name -eq 'go.exe' }
$testProcs = $tests | Group-Object Name | ForEach-Object { "$($_.Name)x$($_.Count)" }
$relays = ($tests | Where-Object { $_.CommandLine -match 'open-file --quit' }).Count
$children = ($tests | Where-Object { $_.CommandLine -match 'GO_WANT_HELPER' }).Count
Write-Output "cpu=$cpu% free=${freeGB}GB relays=$relays helpers=$children procs: $($testProcs -join ' ')"
