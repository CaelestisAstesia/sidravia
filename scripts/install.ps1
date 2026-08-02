# Sidravia user-mode integration installer (Windows).
#
# Idempotent: safe to re-run. Mode-agnostic: works for both installed and
# portable directories; it registers the directory this script lives in.
# It adds that directory to the per-user PATH and creates the exact user-logon
# task "SidraviaDaemon" that runs "sidravia daemon start --log-level <level>".
# It verifies the registered state before and after, and never deletes any
# Configuration, credential, Profile or log.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1 -LogLevel debug
[CmdletBinding()]
param(
    [ValidateSet('info', 'debug', 'trace')]
    [string]$LogLevel = 'info'
)
$ErrorActionPreference = 'Stop'

$TaskName = 'SidraviaDaemon'
$InstallDir = Split-Path -Parent $PSScriptRoot

function Test-PathEntry {
    param([string]$PathValue, [string]$Entry)
    if (-not $PathValue) { return $false }
    $norm = $Entry.TrimEnd('\', '/')
    foreach ($part in ($PathValue -split ';')) {
        if ($part -and ($part.TrimEnd('\', '/') -ieq $norm)) { return $true }
    }
    return $false
}

function Add-PathEntry {
    param([string]$PathValue, [string]$Entry)
    $norm = $Entry.TrimEnd('\', '/')
    if (-not $PathValue) { return $norm }
    if ($PathValue.EndsWith(';')) { return $PathValue + $norm }
    return $PathValue + ';' + $norm
}

function ConvertTo-SIDValue {
    param([string]$Identity)
    if ([string]::IsNullOrWhiteSpace($Identity)) { return $null }
    try {
        return ([Security.Principal.SecurityIdentifier]::new($Identity)).Value
    } catch {
    }
    try {
        $account = [Security.Principal.NTAccount]::new($Identity)
        return ($account.Translate([Security.Principal.SecurityIdentifier])).Value
    } catch {
        return $null
    }
}

function New-SidraviaLogonTaskDefinition {
    param(
        [string]$InstallDirectory,
        [string]$LogLevel,
        [string]$UserSID
    )
    $action = New-ScheduledTaskAction -Execute (Join-Path $InstallDirectory 'sidravia.exe') -Argument "daemon start --log-level $LogLevel"
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $UserSID
    $principal = New-ScheduledTaskPrincipal -UserId $UserSID -LogonType Interactive -RunLevel Limited
    return New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal -Description 'Sidravia daemon'
}

# Precondition: the product binaries must sit next to this script's parent.
if (-not (Test-Path -LiteralPath (Join-Path $InstallDir 'sidravia.exe'))) {
    throw "missing $InstallDir\sidravia.exe"
}
if (-not (Test-Path -LiteralPath (Join-Path $InstallDir 'sidraviad.exe'))) {
    throw "missing $InstallDir\sidraviad.exe"
}
$CurrentUserSID = $null
try {
    $CurrentIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
    if ($null -ne $CurrentIdentity.User) {
        $CurrentUserSID = $CurrentIdentity.User.Value
    }
} catch {
}
if (-not $CurrentUserSID) {
    throw 'current_user_sid_unavailable'
}

# BEFORE: record the current state.
$BeforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
$PathPresent = Test-PathEntry -PathValue $BeforePath -Entry $InstallDir
$BeforeTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
$TaskPresent = ($null -ne $BeforeTask)

# 1) Per-user PATH: exact, case-insensitive, idempotent.
if (-not $PathPresent) {
    $NewPath = Add-PathEntry -PathValue $BeforePath -Entry $InstallDir
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}

# 2) User-logon task: idempotent replace.
$Definition = New-SidraviaLogonTaskDefinition -InstallDirectory $InstallDir -LogLevel $LogLevel -UserSID $CurrentUserSID
try {
    Register-ScheduledTask -TaskName $TaskName -InputObject $Definition -Force | Out-Null
} catch {
    throw [InvalidOperationException]::new('scheduled_task_registration_failed', $_.Exception)
}

# AFTER: verify the registered state.
$AfterPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (Test-PathEntry -PathValue $AfterPath -Entry $InstallDir)) {
    throw 'verify failed: install directory is not on the user PATH'
}
$AfterTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -eq $AfterTask) {
    throw "verify failed: task $TaskName does not exist"
}
if ($AfterTask.Actions.Count -ne 1) {
    throw 'verify failed: task action does not match'
}
$AfterLeaf = Split-Path -Leaf $AfterTask.Actions[0].Execute
$AfterArgs = $AfterTask.Actions[0].Arguments
if ($AfterLeaf -ne 'sidravia.exe' -or $AfterArgs -ne "daemon start --log-level $LogLevel") {
    throw 'verify failed: task action does not match'
}
$AfterPrincipalSID = ConvertTo-SIDValue -Identity ([string]$AfterTask.Principal.UserId)
$AfterTriggerSID = $null
if ($AfterTask.Triggers.Count -eq 1) {
    $AfterTriggerSID = ConvertTo-SIDValue -Identity ([string]$AfterTask.Triggers[0].UserId)
}
if ($AfterPrincipalSID -ne $CurrentUserSID -or $AfterTriggerSID -ne $CurrentUserSID -or
    [string]$AfterTask.Principal.LogonType -ne 'Interactive' -or
    [string]$AfterTask.Principal.RunLevel -ne 'Limited') {
    throw 'verify failed: task identity does not match'
}

# Summary (before -> after).
if ($PathPresent) { Write-Output "PATH: already present $InstallDir" }
else { Write-Output "PATH: added $InstallDir" }
if ($TaskPresent) { Write-Output "Task: updated $TaskName (--log-level $LogLevel)" }
else { Write-Output "Task: created $TaskName (--log-level $LogLevel)" }
