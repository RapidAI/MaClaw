<#
.SYNOPSIS
    Run a bounded six-format OfficeRead fixture regression locally.

.DESCRIPTION
    OfficeRead keeps its large upstream corpus outside this repository. This
    script selects a fixed, representative 30-file subset from a local
    OfficeRead checkout, runs MaClaw's privacy-preserving dual-read reporter,
    and optionally runs the upstream content/image/negative tests first.

    The generated report always declares "fixture" provenance. It is useful
    implementation regression evidence. Its explicit automated acceptance
    profile is intentionally separate from the default internal_authorized
    production-evidence audit.

.EXAMPLE
    .\scripts\test-officeread-fixtures.ps1

.EXAMPLE
    .\scripts\test-officeread-fixtures.ps1 -OfficeReadRoot D:\workprj\OfficeRead -RunUpstreamTests
#>
[CmdletBinding()]
param(
    [string] $OfficeReadRoot = "D:\workprj\OfficeRead",
    [string] $ReportPath = "",
    [switch] $RunUpstreamTests,
    [Alias("KeepExistingReport")]
    [switch] $OverwriteReport
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$officeReadRoot = (Resolve-Path -LiteralPath $OfficeReadRoot -ErrorAction Stop).Path
$sampleRoot = Join-Path $officeReadRoot "testdata\samples"
if (-not (Test-Path -LiteralPath $sampleRoot -PathType Container)) {
    throw "OfficeRead samples directory was not found: $sampleRoot"
}
function Assert-ExactGoTestSelection([string] $Package, [string[]] $RequiredNames) {
    # Reject invalid definitions before calling Go. Apart from preventing a
    # zero-test false green, this avoids compiling a package for a malformed
    # empty/duplicate acceptance list.
    if ($RequiredNames.Count -eq 0) {
        throw "OfficeRead upstream fixture selector is empty for $Package"
    }
    $normalizedNames = @($RequiredNames | ForEach-Object { if ($null -eq $_) { "" } else { $_.Trim() } })
    $invalid = @($normalizedNames | Where-Object { [string]::IsNullOrWhiteSpace($_) })
    if ($invalid.Count -gt 0) {
        throw "OfficeRead upstream fixture selector is invalid for ${Package}; blank: $($invalid.Count)"
    }
    if (@(Compare-Object -CaseSensitive $RequiredNames $normalizedNames).Count -gt 0) {
        throw "OfficeRead upstream fixture selector is invalid for ${Package}; test names must not have leading or trailing whitespace"
    }
    $duplicates = @($normalizedNames | Group-Object | Where-Object Count -gt 1 | ForEach-Object Name)
    if ($duplicates.Count -gt 0) {
        throw "OfficeRead upstream fixture selector is invalid for ${Package}; duplicate: $($duplicates -join ', ')"
    }
    # Test names are passed through regex escaping so exact matches stay exact
    # even if Go ever allows metacharacters in test names.
    $alternation = (($RequiredNames | ForEach-Object { [regex]::Escape($_) }) -join '|')
    $exactPattern = "^(?:$alternation)`$"
    $listed = & go test $Package -list $exactPattern
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to list selected tests for $Package (exit code $LASTEXITCODE)"
    }
    $actual = @($listed | Where-Object { $_ -match '^Test' })
    $unexpected = @($actual | Where-Object { $_ -notin $RequiredNames })
    $missing = @($RequiredNames | Where-Object { $_ -notin $actual })
    if ($missing.Count -gt 0 -or $unexpected.Count -gt 0) {
        throw "OfficeRead upstream fixture selector drifted; missing: $($missing -join ', '); unexpected: $($unexpected -join ', ')"
    }
    return $exactPattern
}

# Keep the selection deliberately small and stable. Every extension exercises
# a mix of rich content, legacy formats, and common Office constructs.
$fixtureNames = @(
    "testPictures.doc", "word95err.doc", "header_image.doc", "table-merges.doc", "Word6.doc",
    "VariousPictures.docx", "FieldCodes.docx", "generated-docx-rich-parts.docx", "generated-altchunk-html.docx", "generated-docx-embedded-ooxml.docx",
    "WithHyperlink.xls", "SimpleWithImages.xls", "TestUnicode.xls", "chinese-provinces.xls", "BOOK_in_capitals.xls",
    "picture.xlsx", "WithDrawing.xlsx", "comments.xlsx", "ExcelTables.xlsx", "HeaderFooterComplexFormats.xlsx",
    "pictures.ppt", "54880_chinese.ppt", "WithComments.ppt", "with_textbox.ppt", "Single_Coloured_Page.ppt",
    "chart-picture-bg.pptx", "table_test2.pptx", "smartart-rotated-text.pptx", "generated-pptx-embedded-ooxml.pptx", "generated-pptx-thumbnail.pptx"
)

$inputs = foreach ($name in $fixtureNames) {
    $path = Join-Path $sampleRoot $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required OfficeRead fixture was not found: $path"
    }
    (Resolve-Path -LiteralPath $path).Path
}

$formatCounts = $inputs | Group-Object { [IO.Path]::GetExtension($_).ToLowerInvariant() }
if ($formatCounts.Count -ne 6 -or @($formatCounts | Where-Object Count -ne 5).Count -ne 0) {
    throw "Fixture selection must contain exactly five files for each of six Office formats"
}

if ($RunUpstreamTests) {
    Push-Location $officeReadRoot
    try {
        $upstreamRequiredTests = @(
            'TestExtractDownloadedSamples',
            'TestSampleImagesAreValidAndReferenced',
            'TestSampleMarkdownQuality',
            'TestComplexSampleExpectations',
            'TestNegativeSamplesDoNotPanic'
        )
        $upstreamTestPattern = Assert-ExactGoTestSelection '.' $upstreamRequiredTests
        go test . -run $upstreamTestPattern -count=1 -timeout 20m
        if ($LASTEXITCODE -ne 0) {
            throw "OfficeRead upstream fixture tests failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        Pop-Location
    }
}

if ([string]::IsNullOrWhiteSpace($ReportPath)) {
    $reportPath = Join-Path $repoRoot "office-read-samples-six-format-dual-report.json"
}
elseif ([IO.Path]::IsPathRooted($ReportPath)) {
    $reportPath = [IO.Path]::GetFullPath($ReportPath)
}
else {
    $reportPath = [IO.Path]::GetFullPath((Join-Path $repoRoot $ReportPath))
}
if ((Test-Path -LiteralPath $reportPath) -and -not $OverwriteReport) {
    throw "Refusing to overwrite existing report: $reportPath. Pass -OverwriteReport to replace it."
}

$hadEngine = Test-Path Env:MACLAW_OFFICE_READ_ENGINE
$hadFormats = Test-Path Env:MACLAW_OFFICE_READ_FORMATS
$previousEngine = $env:MACLAW_OFFICE_READ_ENGINE
$previousFormats = $env:MACLAW_OFFICE_READ_FORMATS
try {
    $env:MACLAW_OFFICE_READ_ENGINE = "dual"
    $env:MACLAW_OFFICE_READ_FORMATS = ".doc,.docx,.xls,.xlsx,.ppt,.pptx"
    Push-Location $repoRoot
    try {
        $arguments = @("run", "./cmd/office-read-dual-report")
        foreach ($input in $inputs) {
            $arguments += @("-input", $input)
        }
        $arguments += @(
            "-provenance", "fixture",
            "-min-samples", "1",
            # The fixture suite uses a deliberately explicit, reproducible
            # floor. It is lower than the production default because some
            # legacy readers omit content OfficeRead can recover differently;
            # the auditor still rejects any format below it.
            "-min-token-hit", "0.85",
            "-out", $reportPath
        )
        & go @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "MaClaw OfficeRead dual report failed with exit code $LASTEXITCODE"
        }

        # The explicit automated-fixture profile must accept this report only
        # after recomputing every format's counters. Parsing errors, missing
        # formats, and regressions must not be mistaken for a valid suite.
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            # Capture the auditor's JSON as data, then assert its complete
            # automated acceptance result below.
            $ErrorActionPreference = "Continue"
            $auditOutput = & go run ./cmd/office-read-dual-report -audit $reportPath -required-formats "doc,docx,ppt,pptx,xls,xlsx" -min-samples 1 -min-token-hit 0.85 -allow-fixture-automation -enforce-audit 2>&1
            $auditExitCode = $LASTEXITCODE
            $auditJSON = (($auditOutput | ForEach-Object { $_.ToString() } | Where-Object { $_ -notmatch '^exit status 1$' }) | Out-String).Trim()
            try {
                $audit = $auditJSON | ConvertFrom-Json -ErrorAction Stop
            }
            catch {
                throw "Fixture audit did not return valid JSON: $auditJSON"
            }
            $auditReasons = @($audit.reasons | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
            # Fixture success is intentionally a separate outcome from the
            # internal_authorized production gate. The latter must stay false
            # even though this selected test profile exits successfully.
            if ($auditExitCode -ne 0 -or $audit.quantitative_ready -ne $false -or $audit.fixture_automation_accepted -ne $true -or $audit.fixture_automation_ready -ne $true -or $auditReasons.Count -ne 0) {
                throw "Fixture report did not satisfy the automated acceptance profile: $auditJSON"
            }
        }
        finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    if ($hadEngine) {
        $env:MACLAW_OFFICE_READ_ENGINE = $previousEngine
    }
    else {
        Remove-Item Env:MACLAW_OFFICE_READ_ENGINE -ErrorAction SilentlyContinue
    }
    if ($hadFormats) {
        $env:MACLAW_OFFICE_READ_FORMATS = $previousFormats
    }
    else {
        Remove-Item Env:MACLAW_OFFICE_READ_FORMATS -ErrorAction SilentlyContinue
    }
}

Write-Host "Fixture dual report created: $reportPath" -ForegroundColor Green
Write-Host "Automated fixture acceptance passed; this is test evidence, not a human attestation." -ForegroundColor Yellow
