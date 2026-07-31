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

# Precondition: the product binaries must sit next to this script's parent.
if (-not (Test-Path -LiteralPath (Join-Path $InstallDir 'sidravia.exe'))) {
    throw "missing $InstallDir\sidravia.exe"
}
if (-not (Test-Path -LiteralPath (Join-Path $InstallDir 'sidraviad.exe'))) {
    throw "missing $InstallDir\sidraviad.exe"
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
$Action = New-ScheduledTaskAction -Execute (Join-Path $InstallDir 'sidravia.exe') -Argument "daemon start --log-level $LogLevel"
$Trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Description 'Sidravia daemon' -Force | Out-Null

# AFTER: verify the registered state.
$AfterPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (Test-PathEntry -PathValue $AfterPath -Entry $InstallDir)) {
    throw 'verify failed: install directory is not on the user PATH'
}
$AfterTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -eq $AfterTask) {
    throw "verify failed: task $TaskName does not exist"
}
$AfterLeaf = Split-Path -Leaf $AfterTask.Actions[0].Execute
$AfterArgs = $AfterTask.Actions[0].Arguments
if ($AfterLeaf -ne 'sidravia.exe' -or $AfterArgs -ne "daemon start --log-level $LogLevel") {
    throw 'verify failed: task action does not match'
}

# Summary (before -> after).
if ($PathPresent) { Write-Output "PATH: already present $InstallDir" }
else { Write-Output "PATH: added $InstallDir" }
if ($TaskPresent) { Write-Output "Task: updated $TaskName (--log-level $LogLevel)" }
else { Write-Output "Task: created $TaskName (--log-level $LogLevel)" }
