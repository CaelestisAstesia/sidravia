# Sidravia full-CLI-surface smoke script (Windows).
#
# Runs every sidravia command against a real daemon and reports PASS/FAIL.
# Commands that need real campus credentials/network (auth start) are run with
# a placeholder account; if they cannot complete they are recorded as an
# expected outcome, not a failure. ASCII-only so Windows PowerShell 5.1 reads
# it without encoding issues.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\cli_smoke.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\cli_smoke.ps1 -Sidravia C:\path\sidravia.exe -SkipAuthStart
[CmdletBinding()]
param(
    [string]$Sidravia = '.\sidravia.exe',
    [switch]$SkipAuthStart
)
$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $Sidravia)) {
    throw "sidravia.exe not found: $Sidravia"
}
$Exe = (Resolve-Path -LiteralPath $Sidravia).Path

$script:checks = New-Object System.Collections.Generic.List[object]
$script:failed = New-Object System.Collections.Generic.List[string]

function Add-Check {
    param([string]$Name, [bool]$OK, [string]$Note = '')
    $script:checks.Add([pscustomobject]@{ Name = $Name; OK = $OK; Note = $Note })
    if (-not $OK) { $script:failed.Add("$Name : $Note") }
}

function Invoke-Sidravia {
    param(
        [string[]]$Args,
        [string]$Stdin,
        [string]$Expect,
        [string]$Match,
        [string]$Name
    )
    if ($null -ne $Stdin) {
        $out = ($Stdin | & $Exe @Args 2>&1 | Out-String)
    } else {
        $out = (& $Exe @Args 2>&1 | Out-String)
    }
    $code = $LASTEXITCODE
    $ok = $true
    $note = "exit=$code"
    switch ($Expect) {
        'success' { $ok = ($code -eq 0) }
        'failure' { $ok = ($code -ne 0) }
        'stopped' { $ok = ($code -eq 0 -and $out -match 'stopped') }
        'running' { $ok = ($code -eq 0 -and $out -match 'running') }
        'any'     { $ok = $true }
        default   { $ok = $true }
    }
    if ($ok -and $Match) { $ok = $out -match [regex]::Escape($Match) }
    if (-not $ok) {
        $flat = ($out -replace "\r?\n", ' ').Trim()
        if ($flat.Length -gt 160) { $flat = $flat.Substring(0, 160) }
        $note += "; out=$flat"
    }
    Add-Check -Name $Name -OK $ok -Note $note
    return $out
}

Write-Output "== Sidravia CLI smoke: $Exe =="

# Make the run idempotent: stop any daemon first.
Invoke-Sidravia -Args @('daemon', 'stop') -Expect any -Name 'daemon stop (initial cleanup)'

# 1. Help surface (identical entrypoints, never dispatch).
Invoke-Sidravia -Args @('--help') -Expect success -Name 'help --help'
Invoke-Sidravia -Args @('help', 'daemon') -Expect success -Name 'help daemon'
Invoke-Sidravia -Args @('help', 'auth', 'start') -Expect success -Name 'help auth start'
Invoke-Sidravia -Args @('auth', 'start', '--help') -Expect success -Name 'auth start --help'
Invoke-Sidravia -Args @('profile', 'list', '--help') -Expect success -Name 'profile list --help'

# 2. Command absence (retired/moved commands must fail).
Invoke-Sidravia -Args @('status') -Expect failure -Name 'retired status rejected'
Invoke-Sidravia -Args @('install') -Expect failure -Name 'install command absent'
Invoke-Sidravia -Args @('uninstall') -Expect failure -Name 'uninstall command absent'

# 3. Daemon lifecycle.
Invoke-Sidravia -Args @('daemon', 'status') -Expect stopped -Name 'daemon status initial'
Invoke-Sidravia -Args @('daemon', 'start', '--log-level', 'info') -Expect success -Name 'daemon start'
Invoke-Sidravia -Args @('daemon', 'status') -Expect running -Name 'daemon status running'
Invoke-Sidravia -Args @('daemon', 'restart') -Expect success -Name 'daemon restart'

# 4. Profile discovery at the program root.
Invoke-Sidravia -Args @('profile', 'list') -Expect success -Match 'jlu' -Name 'profile list shows jlu'

# 5. Configuration CRUD with a disposable id (always removed).
$id = 'smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
Invoke-Sidravia -Args @('config', 'create', '--id', $id, '--profile', 'jlu', '--username', 'smoke-user', '--password-stdin') -Stdin 'smoke-pass' -Expect success -Match $id -Name 'config create'
Invoke-Sidravia -Args @('config', 'list') -Expect success -Match $id -Name 'config list contains id'
Invoke-Sidravia -Args @('config', 'show', $id) -Expect success -Match $id -Name 'config show'
Invoke-Sidravia -Args @('config', 'create', '--id', $id, '--profile', 'jlu', '--username', 'smoke-user', '--password-stdin') -Stdin 'smoke-pass' -Expect failure -Name 'config create duplicate fails'
Invoke-Sidravia -Args @('config', 'show', 'smoke-missing') -Expect failure -Name 'config show missing fails'
Invoke-Sidravia -Args @('config', 'update', $id, '--name', 'SmokeUpdated') -Expect success -Name 'config update'
Invoke-Sidravia -Args @('config', 'set-password', $id, '--password-stdin') -Stdin 'smoke-pass-2' -Expect success -Name 'config set-password'
Invoke-Sidravia -Args @('config', 'remove', $id, '--yes') -Expect success -Name 'config remove'

# 6. Session surface.
Invoke-Sidravia -Args @('auth', 'list') -Expect success -Name 'auth list'
if ($SkipAuthStart) {
    Add-Check -Name 'auth start' -OK $true -Note 'skipped (-SkipAuthStart)'
} else {
    $startOut = Invoke-Sidravia -Args @('auth', 'start', '--profile', 'jlu', '--username', 'smoke-account', '--password-stdin') -Stdin 'smoke-pass' -Expect any -Name 'auth start'
    $m = [regex]::Match($startOut, 'session-\d+')
    if ($m.Success) {
        $sid = $m.Value
        Invoke-Sidravia -Args @('auth', 'status', $sid) -Expect success -Name 'auth status'
        Invoke-Sidravia -Args @('auth', 'restart', $sid) -Expect success -Name 'auth restart'
        Invoke-Sidravia -Args @('auth', 'stop', $sid) -Expect success -Name 'auth stop'
        Invoke-Sidravia -Args @('auth', 'remove', $sid) -Expect success -Name 'auth remove'
    } else {
        Add-Check -Name 'auth session-id parse' -OK $true -Note 'expected (needs real campus credentials/network)'
    }
}

# 7. Cleanup.
Invoke-Sidravia -Args @('daemon', 'stop') -Expect stopped -Name 'daemon stop (cleanup)'

# Summary.
Write-Output ''
Write-Output '== Summary (PASS/FAIL) =='
$passCount = 0
foreach ($c in $script:checks) {
    if ($c.OK) { $passCount++ }
    $mark = if ($c.OK) { 'PASS' } else { 'FAIL' }
    $note = if ($c.Note) { " [$($c.Note)]" } else { '' }
    Write-Output ("{0,-3} {1}{2}" -f $mark, $c.Name, $note)
}
$total = $script:checks.Count
Write-Output ''
Write-Output "Total: $total, Passed: $passCount, Failed: $($total - $passCount)"
if ($script:failed.Count -gt 0) {
    Write-Output 'Unexpected failures:'
    foreach ($f in $script:failed) { Write-Output "  - $f" }
    exit 1
}
exit 0
