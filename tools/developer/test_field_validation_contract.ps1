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
