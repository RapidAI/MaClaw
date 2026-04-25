param(
    [Parameter(Mandatory = $true)]
    [string[]]$CenterUrls,

    [string]$ClusterSecret = "",

    [int]$TimeoutSec = 5
)

$ErrorActionPreference = "Stop"

if ($CenterUrls.Count -lt 3) {
    throw "Please provide at least 3 CenterUrls for the 3-node HA check."
}

function Normalize-Url {
    param([string]$Value)
    return $Value.Trim().TrimEnd('/')
}

function Invoke-JsonGet {
    param(
        [string]$Url,
        [hashtable]$Headers = @{}
    )

    return Invoke-RestMethod -Uri $Url -Method Get -Headers $Headers -TimeoutSec $TimeoutSec
}

function Invoke-StatusGet {
    param(
        [string]$Url,
        [hashtable]$Headers = @{}
    )

    try {
        $response = Invoke-WebRequest -Uri $Url -Method Get -Headers $Headers -TimeoutSec $TimeoutSec -UseBasicParsing
        return [int]$response.StatusCode
    }
    catch {
        $resp = $null
        if ($null -ne $_.Exception) {
            $resp = $_.Exception.Response
        }
        if ($null -ne $resp) {
            try {
                return [int]$resp.StatusCode
            }
            catch {
                try {
                    return [int]$resp.StatusCode.value__
                }
                catch {
                }
            }
        }
        throw
    }
}

$centers = @($CenterUrls | ForEach-Object { Normalize-Url $_ } | Select-Object -Unique)

Write-Host "Checking Hub Center HA nodes..." -ForegroundColor Cyan

$results = @()
foreach ($center in $centers) {
    $row = [ordered]@{
        center          = $center
        healthz         = ""
        quality_status  = ""
        quality_score   = ""
        routable        = ""
        endpoints_count = ""
        ha_auth         = ""
        error           = ""
    }

    try {
        $health = Invoke-StatusGet -Url ($center + "/healthz")
        $row.healthz = $health

        $quality = Invoke-JsonGet -Url ($center + "/api/client/quality")
        $row.quality_status = $quality.service_status
        $row.quality_score = $quality.quality_score
        $row.routable = [bool]$quality.routable

        $endpoints = Invoke-JsonGet -Url ($center + "/api/client/endpoints")
        if ($null -ne $endpoints.nodes) {
            $row.endpoints_count = @($endpoints.nodes).Count
        }
        else {
            $row.endpoints_count = 0
        }

        $haUnauthorized = Invoke-StatusGet -Url ($center + "/api/internal/ha/ops?after_seq=0&limit=1")
        if ($ClusterSecret -ne "") {
            if ($haUnauthorized -ne 401) {
                $row.ha_auth = "unexpected-unauthorized:$haUnauthorized"
            }
            else {
                $haAuthorized = Invoke-StatusGet -Url ($center + "/api/internal/ha/ops?after_seq=0&limit=1") -Headers @{ Authorization = "Bearer $ClusterSecret" }
                $row.ha_auth = "unauthorized:$haUnauthorized;authorized:$haAuthorized"
            }
        }
        elseif ($haUnauthorized -eq 401) {
            $row.ha_auth = "unauthorized-ok"
        }
        else {
            $row.ha_auth = "unexpected:$haUnauthorized"
        }
    }
    catch {
        $row.error = (($_ | Out-String).Trim())
    }

    $results += [pscustomobject]$row
}

$results | Format-Table -AutoSize

$failed = @($results | Where-Object {
    $_.healthz -ne 200 -or
    $_.quality_status -eq "" -or
    $_.routable -ne $true -or
    [int]$_.endpoints_count -lt 3 -or
    ($_.ha_auth -ne "unauthorized-ok" -and $_.ha_auth -ne "unauthorized:401;authorized:200") -or
    $_.error -ne ""
})

if ($failed.Count -gt 0) {
    Write-Host "HA smoke check failed on $($failed.Count) node(s)." -ForegroundColor Red
    exit 1
}

Write-Host "HA smoke check passed for $($results.Count) node(s)." -ForegroundColor Green
