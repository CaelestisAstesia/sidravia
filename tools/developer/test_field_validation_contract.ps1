[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Assert-True {
    param([bool]$Value, [string]$Message)
    if (-not $Value) { throw $Message }
}

function Assert-Equal {
    param([object]$Actual, [object]$Expected, [string]$Message)
    if ($Actual -cne $Expected) { throw "$Message (actual: $Actual)" }
}

$sourcePath = Join-Path $PSScriptRoot '../../scripts/field-test.ps1'
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($sourcePath, [ref]$tokens, [ref]$parseErrors)
Assert-True ($parseErrors.Count -eq 0) 'field-test parser errors'

$parameterNames = @($ast.ParamBlock.Parameters | ForEach-Object { $_.Name.VariablePath.UserPath })
Assert-True ($parameterNames -contains 'RunIntegration') 'RunIntegration parameter is required'
Assert-True ($parameterNames -contains 'SkipIntegration') 'SkipIntegration parameter is required'

function Get-ExtractedFunction {
    param([string]$Name)
    $functionAst = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $Name }, $true)
    Assert-True ($null -ne $functionAst) "$Name function is required"
    return $functionAst.Extent.Text
}

. ([scriptblock]::Create((Get-ExtractedFunction -Name 'Get-SanitizedHarnessError')))
. ([scriptblock]::Create((Get-ExtractedFunction -Name 'Resolve-IntegrationMode')))

$script:PromptCalls = 0
function script:Read-Host {
    param([string]$Prompt)
    $script:PromptCalls++
    throw 'prompt must not be called'
}

Assert-Equal (Resolve-IntegrationMode -RunIntegration $true -SkipIntegration $false) 'run' 'RunIntegration must run'
Assert-Equal $script:PromptCalls 0 'RunIntegration must not prompt'
Assert-Equal (Resolve-IntegrationMode -RunIntegration $false -SkipIntegration $true) 'skip' 'SkipIntegration must skip'
Assert-Equal $script:PromptCalls 0 'SkipIntegration must not prompt'

try {
    Resolve-IntegrationMode -RunIntegration $true -SkipIntegration $true
    throw 'conflicting modes must throw'
} catch {
    Assert-Equal $_.Exception.Message 'conflicting_integration_mode' 'conflicting mode reason'
    Assert-Equal (Get-SanitizedHarnessError -ErrorRecord $_).ReasonCode 'conflicting_integration_mode' 'conflicting mode sanitizer'
}

function script:Read-Host { param([string]$Prompt) 'YES' }
Assert-Equal (Resolve-IntegrationMode -RunIntegration $false -SkipIntegration $false) 'run' 'YES consent must run'
function script:Read-Host { param([string]$Prompt) 'no' }
Assert-Equal (Resolve-IntegrationMode -RunIntegration $false -SkipIntegration $false) 'declined' 'non-YES consent must decline'

$sensitivePromptText = 'fictional-sensitive-prompt-text'
function script:Read-Host { param([string]$Prompt) throw $script:sensitivePromptText }
try {
    Resolve-IntegrationMode -RunIntegration $false -SkipIntegration $false
    throw 'unavailable prompt must throw'
} catch {
    Assert-Equal $_.Exception.Message 'integration_consent_unavailable' 'unavailable prompt reason'
    $serialized = (Get-SanitizedHarnessError -ErrorRecord $_ | ConvertTo-Json -Compress)
    Assert-True (-not $serialized.Contains($sensitivePromptText)) 'sanitized prompt result leaked text'
    Assert-True ($serialized.Contains('integration_consent_unavailable')) 'unavailable prompt sanitizer'
}

$otherSensitiveText = 'fictional-sensitive-internal-text'
try {
    throw $otherSensitiveText
} catch {
    $serialized = (Get-SanitizedHarnessError -ErrorRecord $_ | ConvertTo-Json -Compress)
    Assert-Equal (Get-SanitizedHarnessError -ErrorRecord $_).ReasonCode 'sanitized_internal_error' 'unknown reason sanitizer'
    Assert-True (-not $serialized.Contains($otherSensitiveText)) 'sanitized internal result leaked text'
}

# Execute the actual consumer against inert files, never the field runner body.
. ([scriptblock]::Create((Get-ExtractedFunction -Name 'Resolve-ChildPowerShell')))
. ([scriptblock]::Create((Get-ExtractedFunction -Name 'Test-PackagePreflight')))
$PackageRoot = Join-Path ([IO.Path]::GetTempPath()) ('SidraviaChecksumContract-' + [guid]::NewGuid().ToString('N'))
$Profile = 'jlu'
try {
    $null = New-Item -ItemType Directory -Path $PackageRoot
    foreach ($relative in @('sidraviactl.exe', 'sidraviad.exe', 'BUILD-INFO.txt',
        'institution-profiles/jlu.json', 'scripts/cli-smoke.ps1', 'scripts/install.ps1', 'scripts/uninstall.ps1')) {
        $path = Join-Path $PackageRoot $relative
        $null = New-Item -ItemType Directory -Path (Split-Path -Parent $path) -Force
        [IO.File]::WriteAllBytes($path, [Text.Encoding]::UTF8.GetBytes('inert fixture: ' + $relative))
    }
    $ctlHash = (Get-FileHash -LiteralPath (Join-Path $PackageRoot 'sidraviactl.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
    $daemonHash = (Get-FileHash -LiteralPath (Join-Path $PackageRoot 'sidraviad.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
    # tools/build writeArtifacts and its Go tests protect this exact wire format.
    $ctlLine = $ctlHash + '  sidraviactl.exe'
    $daemonLine = $daemonHash + '  sidraviad.exe'
    function Invoke-ChecksumCase {
        param([string]$Text, [AllowNull()][object]$ExpectedReason)
        [IO.File]::WriteAllText((Join-Path $PackageRoot 'SHA256SUMS'), $Text)
        $script:PackageHashes = [ordered]@{}
        $reason = $null
        try { Test-PackagePreflight } catch { $reason = $_.Exception.Message }
        Assert-Equal $reason $ExpectedReason 'checksum consumer result'
        if ($ExpectedReason) {
            try { throw $ExpectedReason } catch {
                Assert-Equal (Get-SanitizedHarnessError -ErrorRecord $_).ReasonCode $ExpectedReason 'checksum sanitizer reason'
            }
        } else {
            Assert-Equal $script:PackageHashes.Count 2 'verified binary count'
            Assert-Equal $script:PackageHashes['sidraviactl.exe'] $ctlHash 'verified CTL hash'
            Assert-Equal $script:PackageHashes['sidraviad.exe'] $daemonHash 'verified daemon hash'
        }
    }
    Invoke-ChecksumCase ($ctlLine + "`n" + $daemonLine + "`n") $null
    Invoke-ChecksumCase ($daemonLine + "`r`n" + $ctlLine + "`r`n") $null
    Invoke-ChecksumCase ($ctlLine + "`n") 'package_checksum_missing'
    Invoke-ChecksumCase '' 'package_checksum_missing'
    Invoke-ChecksumCase ($ctlLine + "`n" + $daemonLine + "`n" + $ctlLine + "`n") 'package_checksum_duplicate'
    Invoke-ChecksumCase ($ctlHash + "  sidravia.exe`n" + $daemonLine + "`n") 'package_checksum_invalid'
    Invoke-ChecksumCase ($ctlHash + " sidraviactl.exe`n" + $daemonLine + "`n") 'package_checksum_invalid'
    Invoke-ChecksumCase ($ctlLine + "`n" + $daemonLine + "`ninvalid`n") 'package_checksum_invalid'
    Invoke-ChecksumCase ((('0' * 64) + '  sidraviactl.exe') + "`n" + $daemonLine + "`n") 'package_checksum_mismatch'
    Write-Output 'PASS field package checksum consumer contract'
} finally {
    if (Test-Path -LiteralPath $PackageRoot) { Remove-Item -LiteralPath $PackageRoot -Recurse -Force }
}
