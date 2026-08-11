<#
.SYNOPSIS
    Execute the complete automated OfficeRead acceptance suite.

.DESCRIPTION
    This is the non-interactive replacement for the migration plan's former
    manual acceptance gates. It combines upstream six-format fixtures, the
    privacy-preserving dual-read report, parser safety and rollback tests,
    knowledge image-search contracts, service integration, and browser-facing
    marker rendering checks. It makes no network calls and never requires an
    operator to inspect a document or UI manually.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\scripts\test-officeread-acceptance.ps1
#>
[CmdletBinding()]
param(
    [string] $OfficeReadRoot = "D:\workprj\OfficeRead",
    # Optional explicit Node executable for hermetic CI. When omitted, the
    # script uses the active PATH so it works outside a local Codex runtime.
    [string] $NodePath = "",
    # Runs the backend / fixture portion only. This is useful for local
    # iteration, but deliberately does not claim complete acceptance because
    # it omits browser marker tests, TypeScript and the production build.
    [switch] $SkipFrontendBuild
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

function Invoke-Checked([string] $Label, [scriptblock] $Command) {
    Write-Host "==> $Label" -ForegroundColor Cyan
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Label failed with exit code $LASTEXITCODE"
    }
}

function Assert-ExactGoTestSelection([string] $Package, [string[]] $RequiredNames) {
    # Reject invalid definitions before calling Go. Apart from preventing a
    # zero-test false green, this avoids compiling a package for a malformed
    # empty/duplicate acceptance list.
    if ($RequiredNames.Count -eq 0) {
        throw "Required OfficeRead test selector is empty for $Package"
    }
    $normalizedNames = @($RequiredNames | ForEach-Object { if ($null -eq $_) { "" } else { $_.Trim() } })
    $invalid = @($normalizedNames | Where-Object { [string]::IsNullOrWhiteSpace($_) })
    if ($invalid.Count -gt 0) {
        throw "Required OfficeRead test selector is invalid for ${Package}; blank: $($invalid.Count)"
    }
    if (@(Compare-Object -CaseSensitive $RequiredNames $normalizedNames).Count -gt 0) {
        throw "Required OfficeRead test selector is invalid for ${Package}; test names must not have leading or trailing whitespace"
    }
    $duplicates = @($normalizedNames | Group-Object | Where-Object Count -gt 1 | ForEach-Object Name)
    if ($duplicates.Count -gt 0) {
        throw "Required OfficeRead test selector is invalid for ${Package}; duplicate: $($duplicates -join ', ')"
    }
    # A required set proves the named tests exist, while this selector guard
    # proves the exact anchored regex selects every one and nothing else.
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
        throw "Required OfficeRead test selector drifted for ${Package}; missing: $($missing -join ', '); unexpected: $($unexpected -join ', ')"
    }
    return $exactPattern
}

function Invoke-RequiredGoTests([string] $Package, [string[]] $RequiredNames, [switch] $Race) {
    # The exact selector proves names exist, then this invokes the same
    # fully anchored set once to avoid a separate package compile/startup per test.
    $exactPattern = Assert-ExactGoTestSelection $Package $RequiredNames
    $args = @('test')
    if ($Race) {
        $args += '-race'
    }
    $args += @($Package, '-run', $exactPattern, '-count=1', '-timeout', '15m')
    & go @args
    if ($LASTEXITCODE -ne 0) {
        throw "Required OfficeRead contract tests failed for ${Package}"
    }
}

function Resolve-NodeExecutable([string] $RequestedPath) {
    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        $resolved = Resolve-Path -LiteralPath $RequestedPath -ErrorAction Stop
        if (-not (Test-Path -LiteralPath $resolved.Path -PathType Leaf)) {
            throw "Node executable is not a file: $RequestedPath"
        }
        return $resolved.Path
    }
    $nodeCommand = Get-Command node -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $nodeCommand -or [string]::IsNullOrWhiteSpace($nodeCommand.Source)) {
        throw "Node.js is required for frontend acceptance. Install Node.js or pass -NodePath <node.exe>."
    }
    return $nodeCommand.Source
}

function Resolve-CurrentPowerShellExecutable {
    $currentHost = (Get-Process -Id $PID -ErrorAction Stop).Path
    if ([string]::IsNullOrWhiteSpace($currentHost) -or -not (Test-Path -LiteralPath $currentHost -PathType Leaf)) {
        throw "Unable to resolve the current PowerShell executable for nested fixture validation."
    }
    return $currentHost
}

Push-Location $repoRoot
try {
    $powerShellExe = Resolve-CurrentPowerShellExecutable
    Invoke-Checked "OfficeRead upstream fixtures and six-format dual audit" {
        & $powerShellExe -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-officeread-fixtures.ps1 -OfficeReadRoot $OfficeReadRoot -RunUpstreamTests -OverwriteReport
    }
    Invoke-Checked "dual-report race and static analysis" {
        go test -race ./cmd/office-read-dual-report -count=1 -timeout 15m
        if ($LASTEXITCODE -eq 0) { go vet ./cmd/office-read-dual-report }
    }
    Invoke-Checked "OfficeRead parser, pagination, safety and rollback" {
        $agentRequiredTests = @(
            'TestOfficeReadSettings_DefaultsToAllSupportedFormats',
            'TestExtractOfficeTextWithEngine_RealOfficeReadOOXMLFamilies',
            'TestExtractOfficeTextWithEngine_RealOfficeReadBIFFXLS',
            'TestExtractOfficeTextWithEngine_RejectsEncryptedOOXMLBeforeBothParsers',
            'TestExtractOfficeTextWithEngine_RejectsEncryptedOLEBeforeBothParsers',
            'TestToolReadDocument_OffsetPaging',
            'TestToolReadDocument_FormatLevelOfficeReadRollbackInvalidatesCache',
            'TestExpandUserSelectedFilePaths_AllOfficeFormatsUseOfficeReadDefaultRoute'
        )
        Invoke-RequiredGoTests './corelib/agent' $agentRequiredTests -Race
        go test -race ./corelib/agent -run 'Test(OfficeRead|ExtractOfficeTextWithEngine|ToolReadDocument|FormatAutoExtracted|ExpandUserSelectedFilePaths|StripAutoExtractBodies)' -count=1 -timeout 15m
    }
    Invoke-Checked "OfficeRead persisted defaults and rollout migration" {
        # Defaults and the one-time historic PPT-only policy promotion live in
        # corelib, outside the GUI package. Exercise them explicitly so a
        # passing GUI rollback test cannot mask a future default regression.
        $coreRequiredTests = @('TestOfficeReadConfigDefaultsAndRoundTrip')
        Invoke-RequiredGoTests './corelib' $coreRequiredTests
    }
    Invoke-Checked "knowledge Markdown, managed images and lifecycle" {
        $knowledgeRequiredTests = @(
            'TestOfficeReadStructuredKnowledgeContentRequiresExplicitOptIn',
            'TestOfficeReadKnowledgeImagesUseManagedAssets',
            'TestOfficeReadKnowledgeImportPersistsStableEncryptedError',
            'TestOfficeReadKnowledgeImportSharesOneRichExtraction',
			'TestImageAssetLookupAndSearchRejectWhitespacePaddedAssetIDs',
			'TestSearchImagesDropsCrossSourceImageAssetID',
			'TestFindImageAssetSourceDoesNotUseLegacyAssetPath',
			'TestFindImageAssetSourceRejectsCrossSourceAndNonImageNodeClaims',
			'TestDoctorDoesNotNormalizeWhitespacePaddedImageAssetID',
            'TestDocumentNodePersistenceDropsInvalidImageAssetID',
            'TestDocumentNodeReadDropsInvalidImageAssetIDInsertedAfterOpen',
            'TestOpeningLegacyKnowledgeStoreScrubsInvalidImageAssetID',
            'TestDeleteSourceDoesNotNormalizeWhitespacePaddedEmbeddedAssetID',
			'TestDeleteSourceDoesNotReclaimForeignOrNonImageAssetClaims',
			'TestEmbedImageThumbForSearchResultDoesNotGuessAnAssetID'
        )
        Invoke-RequiredGoTests './corelib/knowledge' $knowledgeRequiredTests
		go test ./corelib/knowledge -run 'Test(OfficeRead|ParseLegacy|ParseDocumentNodesPPT|Capabilities|DeleteSource(ReclaimsStandaloneAndEmbeddedImageAssets|DoesNotNormalizeWhitespacePaddedEmbeddedAssetID|DoesNotReclaimForeignOrNonImageAssetClaims)|ImageAssetLookupAndSearchRejectWhitespacePaddedAssetIDs|SearchImagesDropsCrossSourceImageAssetID|FindImageAssetSource(DoesNotUseLegacyAssetPath|RejectsCrossSourceAndNonImageNodeClaims)|EmbedImageThumbForSearchResultDoesNotGuessAnAssetID|DoctorDoesNotNormalizeWhitespacePaddedImageAssetID|DocumentNodePersistenceDropsInvalidImageAssetID|DocumentNodeReadDropsInvalidImageAssetIDInsertedAfterOpen|OpeningLegacyKnowledgeStoreScrubsInvalidImageAssetID)' -count=1 -timeout 15m
    }
    Invoke-Checked "request-scoped Agent OfficeRead and image retrieval" {
        # A request policy has to reach both automatic attachment extraction and
        # the explicit read_document tool.  The latter is intentionally listed
        # here rather than inferred from the former: they use different tool
        # dispatch paths in multi-tenant server deployments.
        $agentServicePattern = 'Test(CoreAgent.*KnowledgeImage|.*KnowledgeImageSearch|.*KB_IMAGE|BuildConversationUsesRequestScopedOfficeReadPolicyForAttachments|CoreAgentReadDocument(UsesRequestOfficeReadPolicy|UsesRequestScopedOfficeReadConfig|ReportsSharedReaderFailures)|ReadDocumentToolResultPreservesTimeoutOutcome)'
        $agentServiceRequiredTests = @(
            'TestBuildConversationUsesRequestScopedOfficeReadPolicyForAttachments',
            'TestCoreAgentReadDocumentUsesRequestOfficeReadPolicy',
            'TestCoreAgentReadDocumentUsesRequestScopedOfficeReadConfig',
            'TestReadDocumentToolResultPreservesTimeoutOutcome'
        )
        Invoke-RequiredGoTests './corelib/agentservice' $agentServiceRequiredTests
        go test ./corelib/agentservice -run $agentServicePattern -count=1 -timeout 15m
    }
    Invoke-Checked "GUI, VE and mobile six-format Office contract" {
        # Cover desktop tools, VE attachment routing, and the independent
        # mobile-worker GET source -> extract -> PATCH result lifecycle. This
        # keeps the six-format claim from relying on desktop-only evidence.
		$guiPattern = 'Test(NewAppOfficeReadProviderAppliesFormatRollbackImmediately|OfficeReadFormatLevelRollbackDrill|ClassifyFileType|MimeTypeForFile|ToolOfficeReadDocumentFromTaskWorkspace|PrepareVEAttachmentMessageSupportsAllOfficeFormats|ProcessMessageAttachmentsKeepsAllOfficeFormatsOutOfInlineContext|ProcessMobileDocumentUploadTaskUsesPersistedOfficeReadPolicy|MobileDocument(RequiresOfficeExtractionRecognizesSixFormats|SourceLimitUsesOfficeReadBoundForSixFormats|SourceMarkdownUses(SharedOfficeRouteForDOCX|PersistedOfficeReadPolicy)|OfficeMarkdownRejectsEncryptedDocumentWithoutFallback)|Knowledge(ImageAsset|OpenImage|GetImage|ImagePresentation)|VEKnowledgePromptAdvertisesImageSearchAndDisplay|ToolKnowledgeImageSearch)'
        $guiRequiredTests = @(
            'TestOfficeReadFormatLevelRollbackDrill',
            'TestNewAppOfficeReadProviderAppliesFormatRollbackImmediately',
            'TestToolOfficeReadDocumentFromTaskWorkspace',
            'TestPrepareVEAttachmentMessageSupportsAllOfficeFormats',
            'TestProcessMessageAttachmentsKeepsAllOfficeFormatsOutOfInlineContext',
			'TestProcessMobileDocumentUploadTaskUsesPersistedOfficeReadPolicy',
			'TestMobileDocumentOfficeMarkdownRejectsEncryptedDocumentWithoutFallback',
			'TestKnowledgeImagePresentationRequiresRegisteredAsset'
        )
        Invoke-RequiredGoTests './gui' $guiRequiredTests
        go test ./gui -run $guiPattern -count=1 -timeout 15m
    }
    Invoke-Checked "server image and Office attachment authorization contract" {
        $serverPattern = 'Test.*Knowledge.*Image|Test.*Image.*Knowledge|TestThirdPartyGatewayStagesLargeOfficeMediaInInstanceWorkspace|TestPlatformAttachment(MaxBytesForUsesSharedDocumentLimit|TreatsMislabelledOfficeTextAttachmentAsFile|FileAttachmentLineTreatsMislabelledOfficeImageAsFile)'
        # This selector deliberately spans image-asset authorization and
        # Office attachment staging. Pin the specific boundary tests as well:
        # broad `Knowledge.*Image` matching alone would not detect an accidental
        # loss of a platform attachment or tenant-access contract.
		$serverRequiredTests = @(
			'TestKnowledgeImageAssetEndpointsEnforceReadAccess',
			'TestKnowledgeImageSearchRespectsReadScopesAndReturnsMedia',
			'TestSanitizeKnowledgeSearchResultsForAPIDoesNotGuessOrBorrowImageAssetID',
			'TestThirdPartyGatewayStagesLargeOfficeMediaInInstanceWorkspace',
            'TestPlatformAttachmentMaxBytesForUsesSharedDocumentLimit',
            'TestEnrichPlatformMessageTreatsMislabelledOfficeTextAttachmentAsFile',
            'TestPlatformFileAttachmentLineTreatsMislabelledOfficeImageAsFile'
        )
        Invoke-RequiredGoTests './MaClawSrv' $serverRequiredTests
		go test ./MaClawSrv -run $serverPattern -count=1 -timeout 15m
    }
    Invoke-Checked "Hub mobile document Office routing contract" {
        # The Hub decides whether an uploaded mobile document needs desktop
        # Office extraction before the GUI worker ever sees it. Include this
        # independently-tested API decision so all six formats are verified on
        # both sides of the worker boundary.
        $hubPattern = 'Test(MobileUploadedFileNeedsRemoteOfficeExtractionRecognizesSixFormats|MobileDraftSourceLooksTextLikeRejectsAllOfficeFormats|MobileDocumentUploadClaimHandlerDocumentKindSkipsOCRTasks)'
        $hubRequiredTests = @(
            'TestMobileUploadedFileNeedsRemoteOfficeExtractionRecognizesSixFormats',
            'TestMobileDraftSourceLooksTextLikeRejectsAllOfficeFormats',
            'TestMobileDocumentUploadClaimHandlerDocumentKindSkipsOCRTasks'
        )
        Invoke-RequiredGoTests './hub/internal/httpapi' $hubRequiredTests
        go test ./hub/internal/httpapi -run $hubPattern -count=1 -timeout 15m
    }
    if (-not $SkipFrontendBuild) {
        $node = Resolve-NodeExecutable $NodePath
        Invoke-Checked "browser image marker tests, TypeScript and production build" {
            Push-Location gui/frontend
            try {
                & $node 'node_modules\vitest\vitest.mjs' run 'src/components/ai/aiAssistantMarkdown.test.tsx' --reporter=dot
                if ($LASTEXITCODE -eq 0) { & $node 'node_modules\typescript\bin\tsc' }
                # Invoke Vite through the same resolved Node executable as
                # Vitest and tsc. Calling vite.cmd here would resolve a second
                # Node from PATH, defeating -NodePath and making CI behavior
                # depend on whichever unrelated Node installation appears first.
                if ($LASTEXITCODE -eq 0) { & $node 'node_modules\vite\bin\vite.js' build }
            }
            finally {
                Pop-Location
            }
        }
    }
}
finally {
    Pop-Location
}

if ($SkipFrontendBuild) {
    Write-Host "OfficeRead Go-only acceptance passed; frontend marker tests, TypeScript and production build were skipped." -ForegroundColor Yellow
}
else {
    Write-Host "OfficeRead complete automated acceptance passed." -ForegroundColor Green
}
