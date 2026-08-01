# Sidravia full-CLI-surface smoke script (Windows).
#
# Runs every sidravia command against a real daemon and reports PASS/FAIL.
# Commands that need real campus credentials/network (auth start) are run with
# a placeholder account; if they cannot complete they are recorded as an
# expected outcome, not a failure. PowerShell 7 and ASCII-only.
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
$script:NativeUTF8Encoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = $script:NativeUTF8Encoding
[Console]::OutputEncoding = $script:NativeUTF8Encoding

if ($PSVersionTable.PSEdition -ne 'Core' -or $PSVersionTable.PSVersion.Major -lt 7) {
    throw 'PowerShell 7 or later is required.'
}

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

function Invoke-NativeWithInput {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$InputValue
    )
    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $FilePath
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardInput = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.StandardOutputEncoding = $script:NativeUTF8Encoding
    $startInfo.StandardErrorEncoding = $script:NativeUTF8Encoding
    foreach ($argument in $Arguments) {
        $null = $startInfo.ArgumentList.Add($argument)
    }

    $process = $null
    $inputStream = $null
    $inputBytes = $null
    try {
        $process = New-Object System.Diagnostics.Process
        $process.StartInfo = $startInfo
        if (-not $process.Start()) { throw 'native_process_start_failed' }
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()

        $inputBytes = $script:NativeUTF8Encoding.GetBytes($InputValue + "`n")
        $inputStream = $process.StandardInput.BaseStream
        $inputStream.Write($inputBytes, 0, $inputBytes.Length)
        $inputStream.Flush()
        $inputStream.Close()
        $inputStream = $null

        $process.WaitForExit()
        $stdout = $stdoutTask.Result
        $stderr = $stderrTask.Result
        return [pscustomobject]@{
            ExitCode = $process.ExitCode
            Output = $stdout + $stderr
        }
    } finally {
        if ($null -ne $inputStream) {
            try { $inputStream.Close() } catch {}
        }
        if ($null -ne $inputBytes) {
            [Array]::Clear($inputBytes, 0, $inputBytes.Length)
        }
        if ($null -ne $process) { $process.Dispose() }
    }
}

function Invoke-Sidravia {
    param(
        [string[]]$CommandArguments,
        [string]$Stdin,
        [string]$Expect,
        [string]$Match,
        [string]$Name
    )
    if ($null -ne $Stdin) {
        $result = Invoke-NativeWithInput -FilePath $Exe -Arguments $CommandArguments -InputValue $Stdin
        $out = $result.Output
        $code = $result.ExitCode
    } else {
        $out = (& $Exe @CommandArguments 2>&1 | Out-String)
        $code = $LASTEXITCODE
    }
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
Invoke-Sidravia -CommandArguments @('daemon', 'stop') -Expect any -Name 'daemon stop (initial cleanup)'

# 1. Help surface (identical entrypoints, never dispatch).
Invoke-Sidravia -CommandArguments @('--help') -Expect success -Name 'help --help'
Invoke-Sidravia -CommandArguments @('help', 'daemon') -Expect success -Name 'help daemon'
Invoke-Sidravia -CommandArguments @('help', 'auth', 'start') -Expect success -Name 'help auth start'
Invoke-Sidravia -CommandArguments @('auth', 'start', '--help') -Expect success -Name 'auth start --help'
Invoke-Sidravia -CommandArguments @('profile', 'list', '--help') -Expect success -Name 'profile list --help'

# 2. Command absence (retired/moved commands must fail).
Invoke-Sidravia -CommandArguments @('status') -Expect failure -Name 'retired status rejected'
Invoke-Sidravia -CommandArguments @('install') -Expect failure -Name 'install command absent'
Invoke-Sidravia -CommandArguments @('uninstall') -Expect failure -Name 'uninstall command absent'

# 3. Daemon lifecycle.
Invoke-Sidravia -CommandArguments @('daemon', 'status') -Expect stopped -Name 'daemon status initial'
Invoke-Sidravia -CommandArguments @('daemon', 'start', '--log-level', 'info') -Expect success -Name 'daemon start'
Invoke-Sidravia -CommandArguments @('daemon', 'status') -Expect running -Name 'daemon status running'
Invoke-Sidravia -CommandArguments @('daemon', 'restart') -Expect success -Name 'daemon restart'

# 4. Profile discovery at the program root.
Invoke-Sidravia -CommandArguments @('profile', 'list') -Expect success -Match 'jlu' -Name 'profile list shows jlu'

# 5. Configuration CRUD with a disposable id (always removed).
$id = 'smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
Invoke-Sidravia -CommandArguments @('config', 'create', '--id', $id, '--profile', 'jlu', '--username', 'smoke-user', '--password-stdin') -Stdin 'smoke-pass' -Expect success -Match $id -Name 'config create'
Invoke-Sidravia -CommandArguments @('config', 'list') -Expect success -Match $id -Name 'config list contains id'
Invoke-Sidravia -CommandArguments @('config', 'show', $id) -Expect success -Match $id -Name 'config show'
Invoke-Sidravia -CommandArguments @('config', 'create', '--id', $id, '--profile', 'jlu', '--username', 'smoke-user', '--password-stdin') -Stdin 'smoke-pass' -Expect failure -Name 'config create duplicate fails'
Invoke-Sidravia -CommandArguments @('config', 'show', 'smoke-missing') -Expect failure -Name 'config show missing fails'
Invoke-Sidravia -CommandArguments @('config', 'update', $id, '--name', 'SmokeUpdated') -Expect success -Name 'config update'
Invoke-Sidravia -CommandArguments @('config', 'set-password', $id, '--password-stdin') -Stdin 'smoke-pass-2' -Expect success -Name 'config set-password'
Invoke-Sidravia -CommandArguments @('config', 'remove', $id, '--yes') -Expect success -Name 'config remove'

# 6. Session surface.
Invoke-Sidravia -CommandArguments @('auth', 'list') -Expect success -Name 'auth list'
if ($SkipAuthStart) {
    Add-Check -Name 'auth start' -OK $true -Note 'skipped (-SkipAuthStart)'
} else {
    $startOut = Invoke-Sidravia -CommandArguments @('auth', 'start', '--profile', 'jlu', '--username', 'smoke-account', '--password-stdin') -Stdin 'smoke-pass' -Expect any -Name 'auth start'
    $m = [regex]::Match($startOut, 'session-\d+')
    if ($m.Success) {
        $sid = $m.Value
        Invoke-Sidravia -CommandArguments @('auth', 'status', $sid) -Expect success -Name 'auth status'
        Invoke-Sidravia -CommandArguments @('auth', 'restart', $sid) -Expect success -Name 'auth restart'
        Invoke-Sidravia -CommandArguments @('auth', 'stop', $sid) -Expect success -Name 'auth stop'
        Invoke-Sidravia -CommandArguments @('auth', 'remove', $sid) -Expect success -Name 'auth remove'
    } else {
        Add-Check -Name 'auth session-id parse' -OK $true -Note 'expected (needs real campus credentials/network)'
    }
}

# 7. Cleanup.
Invoke-Sidravia -CommandArguments @('daemon', 'stop') -Expect stopped -Name 'daemon stop (cleanup)'

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
