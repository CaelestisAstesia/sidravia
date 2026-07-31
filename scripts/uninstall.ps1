# Sidravia user-mode integration uninstaller (Windows).
#
# Idempotent: safe to re-run. Revokes only the per-user PATH entry and the
# user-logon task registered by install.ps1. It verifies the revoked state
# before and after, and never deletes any Configuration, credential, Profile
# or log.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
[CmdletBinding()]
param()
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

function Remove-PathEntry {
    param([string]$PathValue, [string]$Entry)
    $norm = $Entry.TrimEnd('\', '/')
    $removed = $false
    $kept = @()
    foreach ($part in ($PathValue -split ';')) {
        if (-not $removed -and $part -and ($part.TrimEnd('\', '/') -ieq $norm)) {
            $removed = $true
            continue
        }
        $kept += $part
    }
    if ($removed) { return ($kept -join ';').Trim(';') }
    return $PathValue
}

# BEFORE: record the current state.
$BeforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
$PathPresent = Test-PathEntry -PathValue $BeforePath -Entry $InstallDir
$BeforeTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
$TaskPresent = ($null -ne $BeforeTask)

# 1) Remove the exact per-user PATH entry.
if ($PathPresent) {
    $NewPath = Remove-PathEntry -PathValue $BeforePath -Entry $InstallDir
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}

# 2) Remove the user-logon task.
if ($TaskPresent) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

# AFTER: verify both are gone.
$AfterPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (Test-PathEntry -PathValue $AfterPath -Entry $InstallDir) {
    throw 'verify failed: install directory is still on the user PATH'
}
$AfterTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -ne $AfterTask) {
    throw "verify failed: task $TaskName still exists"
}

# Summary (before -> after).
if ($PathPresent) { Write-Output "PATH: removed $InstallDir" }
else { Write-Output "PATH: absent (idempotent)" }
if ($TaskPresent) { Write-Output "Task: removed $TaskName" }
else { Write-Output "Task: absent (idempotent)" }
