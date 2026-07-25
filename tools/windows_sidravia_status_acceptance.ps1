# Sidravia Windows acceptance: sidravia status
# Requires: sidravia.exe and sidraviad.exe in the current directory.
# Usage: powershell -NoProfile -File tools/windows_sidravia_status_acceptance.ps1

$ErrorActionPreference = "Stop"

Write-Host "=== Sidravia Windows status acceptance ==="

# Runtime info path.
$cacheDir = [System.Environment]::GetFolderPath([System.Environment+SpecialFolder]::LocalApplicationData)
$runtimePath = Join-Path $cacheDir "Sidravia\runtime.json"

# Identity and outcome tracked at script scope so the finally block can clean up safely.
$script:DaemonPID = $null
$script:DaemonToken = $null
$script:SecondProcess = $null
$script:TestFailure = $null
$script:CleanupFailure = $null

# --- Helpers ---------------------------------------------------------------

# Parse a runtime info file without performing any business logic. Returns $null
# when the file is absent; an object with Parsed=$true and the decoded content on
# success; or Parsed=$false when the file exists but cannot be parsed. This helper
# never deletes anything and never makes daemon decisions.
function Get-RuntimeInfo {
    param([string]$Path)
    if (-not (Test-Path $Path)) { return $null }
    try {
        $obj = Get-Content -Raw $Path -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
        return [pscustomobject]@{ Parsed = $true; Object = $obj }
    } catch {
        return [pscustomobject]@{ Parsed = $false; Object = $null }
    }
}

function Test-PositiveInt32 {
    param($Value)
    $parsed = 0
    if (-not [int]::TryParse([string]$Value, [ref]$parsed)) { return $false }
    return $parsed -gt 0
}

function Test-DaemonToken {
    param($Value)
    return $Value -is [string] -and $Value -match '^[0-9a-f]{64}$'
}

# Returns the process name for a PID, or $null if no such process exists.
function Get-ProcessNameById {
    param([int]$ProcessId)
    $proc = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    if ($null -eq $proc) { return $null }
    return $proc.ProcessName
}

# --- Verify executables ----------------------------------------------------

if (-not (Test-Path .\sidravia.exe)) {
    Write-Error "sidravia.exe not found in current directory"
    exit 1
}
if (-not (Test-Path .\sidraviad.exe)) {
    Write-Error "sidraviad.exe not found in current directory"
    exit 1
}
Write-Host "Executables found."

# --- Initial runtime info: is a daemon already running? --------------------
# JSON parsing and the "is a sidraviad running" business decision are kept in
# separate control flows. Parse failures never delete the file.

if (Test-Path $runtimePath) {
    $initial = Get-RuntimeInfo $runtimePath
    if ($null -eq $initial -or -not $initial.Parsed) {
        Write-Error "Runtime info exists but cannot be safely identified; leaving it in place."
        exit 1
    }
    $initialInfo = $initial.Object
    if (-not (Test-PositiveInt32 $initialInfo.pid)) {
        Write-Error "Runtime info PID is not a positive integer; leaving file in place."
        exit 1
    }
    $initialPid = [int]$initialInfo.pid
    $initialName = Get-ProcessNameById -ProcessId $initialPid
    if ($initialName -eq "sidraviad") {
        Write-Error "A sidraviad instance is already running (PID $initialPid). Stop it first."
        exit 1
    }
    # PID is gone or already belongs to a different process: stale and safe to remove.
    try {
        Remove-Item $runtimePath -Force -ErrorAction Stop
        Write-Host "Cleaned up stale runtime info (PID $initialPid is not sidraviad)."
    } catch {
        Write-Host "Failed to remove stale runtime info: $($_.Exception.Message)"
        exit 1
    }
}

# --- Test 1: cold start ----------------------------------------------------

Write-Host "Test 1: sidravia status (cold start)"
$output = & .\sidravia.exe status 2>&1
$exitCode = $LASTEXITCODE
Write-Host "  exit code: $exitCode"
Write-Host "  output: $output"

if ($exitCode -ne 0) {
    Write-Error "Test 1 FAILED: expected exit code 0, got $exitCode"
    exit 1
}
if ($output -notmatch "sidraviad .+ pid=\d+ status=running") {
    Write-Error "Test 1 FAILED: output does not match expected pattern"
    exit 1
}
if (-not (Test-Path $runtimePath)) {
    Write-Error "Test 1 FAILED: runtime info file not created"
    exit 1
}
Write-Host "  runtime info file exists."
Write-Host "Test 1 PASSED"

# --- Establish this acceptance's daemon identity ---------------------------
# Re-read the runtime info written by Test 1 and confirm PID, token and process
# name before adopting them. If anything is wrong we cannot safely clean up, so
# we fail without guessing a PID, batch-stopping by name, or deleting the file.

$identity = Get-RuntimeInfo $runtimePath
if ($null -eq $identity -or -not $identity.Parsed) {
    Write-Error "Identity FAILED: cannot parse runtime info written by Test 1."
    exit 1
}
$identityInfo = $identity.Object
if (-not (Test-PositiveInt32 $identityInfo.pid)) {
    Write-Error "Identity FAILED: runtime info PID is not a positive integer."
    exit 1
}
if (-not (Test-DaemonToken $identityInfo.token)) {
    Write-Error "Identity FAILED: runtime info token is not 64 lowercase hex characters."
    exit 1
}
$script:DaemonPID = [int]$identityInfo.pid
$script:DaemonToken = [string]$identityInfo.token
if ((Get-ProcessNameById -ProcessId $script:DaemonPID) -ne "sidraviad") {
    Write-Error "Identity FAILED: PID $script:DaemonPID is not currently sidraviad."
    exit 1
}
Write-Host "  daemon identity: PID $script:DaemonPID, token recorded."

# --- Tests 2-5, protected so any failure still runs safe cleanup -----------

try {
    # Test 2: warm start, daemon already running.
    Write-Host "Test 2: sidravia status (warm, daemon already running)"
    $output = & .\sidravia.exe status 2>&1
    $exitCode = $LASTEXITCODE
    Write-Host "  exit code: $exitCode"
    Write-Host "  output: $output"
    if ($exitCode -ne 0) {
        $script:TestFailure = "Test 2 FAILED: expected exit code 0, got $exitCode"
        throw $script:TestFailure
    }
    if ($output -notmatch "sidraviad .+ pid=\d+ status=running") {
        $script:TestFailure = "Test 2 FAILED: output does not match expected pattern"
        throw $script:TestFailure
    }
    Write-Host "Test 2 PASSED"

    # Test 3: second daemon rejected by mutex.
    Write-Host "Test 3: second sidraviad rejected by mutex"
    $script:SecondProcess = Start-Process -FilePath .\sidraviad.exe -NoNewWindow -PassThru
    Start-Sleep -Seconds 2
    $script:SecondProcess.Refresh()
    if (-not $script:SecondProcess.HasExited) {
        $script:TestFailure = "Test 3 FAILED: second daemon did not exit"
        throw $script:TestFailure
    }
    $secondExitCode = $script:SecondProcess.ExitCode
    Write-Host "  second daemon exit code: $secondExitCode"
    if ($secondExitCode -eq 0) {
        $script:TestFailure = "Test 3 FAILED: second daemon should exit with non-zero code"
        throw $script:TestFailure
    }
    Write-Host "Test 3 PASSED"

    # Test 4: invalid CLI args.
    Write-Host "Test 4: invalid CLI args (bogus command)"
    $output = & .\sidravia.exe bogus 2>&1
    $exitCode = $LASTEXITCODE
    Write-Host "  exit code: $exitCode"
    if ($exitCode -eq 0) {
        $script:TestFailure = "Test 4 FAILED: expected non-zero exit for invalid args"
        throw $script:TestFailure
    }
    Write-Host "Test 4 PASSED"

    # Test 5: extra arguments rejected.
    Write-Host "Test 5: extra arguments rejected"
    $output = & .\sidravia.exe status extra 2>&1
    $exitCode = $LASTEXITCODE
    Write-Host "  exit code: $exitCode"
    if ($exitCode -eq 0) {
        $script:TestFailure = "Test 5 FAILED: expected non-zero exit for extra args"
        throw $script:TestFailure
    }
    Write-Host "Test 5 PASSED"
}
catch {
    if (-not $script:TestFailure) {
        $script:TestFailure = "Unexpected error during tests: $($_.Exception.Message)"
    }
}
finally {
    # --- Safe cleanup ---

    # Tracks whether this acceptance's main daemon has been confirmed to have
    # exited. Runtime info is deleted only when this is true and the file still
    # matches the recorded PID/token.
    $mainDaemonConfirmedExited = $false

    # 1. Second daemon: only the exact process object from Test 3.
    if ($null -ne $script:SecondProcess) {
        try {
            $script:SecondProcess.Refresh()
            if (-not $script:SecondProcess.HasExited) {
                $script:SecondProcess.Kill()
                $waitOk = $script:SecondProcess.WaitForExit(5000)
                if (-not $waitOk) {
                    if (-not $script:CleanupFailure) { $script:CleanupFailure = "Second daemon did not exit within five seconds." }
                }
                $script:SecondProcess.Refresh()
                if (-not $script:SecondProcess.HasExited) {
                    if (-not $script:CleanupFailure) { $script:CleanupFailure = "Second daemon still running after cleanup." }
                }
            }
        } catch {
            if (-not $script:CleanupFailure) { $script:CleanupFailure = "Second daemon cleanup failed: $($_.Exception.Message)" }
            try { $script:SecondProcess.Refresh() } catch { }
            try {
                if (-not $script:SecondProcess.HasExited) {
                    if (-not $script:CleanupFailure) { $script:CleanupFailure = "Second daemon still running after cleanup." }
                }
            } catch { }
        }
    }

    # 2. Main daemon: stop only if the current runtime info still proves it is ours.
    if (Test-Path $runtimePath) {
        $mainInfo = Get-RuntimeInfo $runtimePath
        if ($null -eq $mainInfo) {
            if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info missing; cannot confirm main daemon ownership." }
        }
        elseif (-not $mainInfo.Parsed) {
            if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info unparseable; cannot confirm main daemon ownership." }
        }
        else {
            $curPidValid = Test-PositiveInt32 $mainInfo.Object.pid
            $curTokenValid = Test-DaemonToken $mainInfo.Object.token
            if (-not $curPidValid -or -not $curTokenValid) {
                if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info PID/token is invalid; cannot confirm main daemon ownership." }
            }
            else {
                $curPid = [int]$mainInfo.Object.pid
                $curToken = [string]$mainInfo.Object.token
                if ($curPid -ne $script:DaemonPID -or $curToken -ne $script:DaemonToken) {
                    if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info PID/token does not match this acceptance; not stopping main daemon." }
                }
                else {
                    $curName = Get-ProcessNameById -ProcessId $curPid
                    if ($null -eq $curName) {
                        # Main daemon already exited; deletion may proceed in step 3.
                        $mainDaemonConfirmedExited = $true
                        Write-Host "Cleanup: main daemon already exited (PID $curPid)."
                    }
                    elseif ($curName -ne "sidraviad") {
                        if (-not $script:CleanupFailure) { $script:CleanupFailure = "PID $curPid no longer corresponds to sidraviad; not stopping." }
                    }
                    else {
                        Write-Host "Cleanup: stopping daemon PID $curPid"
                        try {
                            Stop-Process -Id $curPid -Force -ErrorAction Stop
                        } catch {
                            if (-not $script:CleanupFailure) { $script:CleanupFailure = "Main daemon stop failed: $($_.Exception.Message)" }
                        }
                        # Wait/poll for the precise PID to no longer be our sidraviad within a limited time.
                        for ($i = 0; $i -lt 10; $i++) {
                            $afterName = Get-ProcessNameById -ProcessId $curPid
                            if ($null -eq $afterName) {
                                $mainDaemonConfirmedExited = $true
                                Write-Host "  daemon confirmed exited (PID $curPid gone)."
                                break
                            }
                            if ($afterName -ne "sidraviad") {
                                if (-not $script:CleanupFailure) { $script:CleanupFailure = "PID $curPid reused by another process after stop; keeping runtime info." }
                                break
                            }
                            Start-Sleep -Milliseconds 500
                        }
                        if (-not $mainDaemonConfirmedExited -and -not $script:CleanupFailure) {
                            $script:CleanupFailure = "Main daemon (PID $curPid) still running after stop; keeping runtime info."
                        }
                    }
                }
            }
        }
    }
    else {
        if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info missing; cannot confirm main daemon ownership." }
    }

    # 3. Runtime info: delete only if the main daemon has been confirmed exited
    #    and the file still matches the recorded PID and token.
    if (Test-Path $runtimePath) {
        $finalInfo = Get-RuntimeInfo $runtimePath
        if ($null -eq $finalInfo) {
            # File vanished between checks; nothing to delete.
        }
        elseif (-not $finalInfo.Parsed) {
            if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info unparseable; not deleting file." }
        }
        else {
            $finalPidValid = Test-PositiveInt32 $finalInfo.Object.pid
            $finalTokenValid = Test-DaemonToken $finalInfo.Object.token
            if (-not $finalPidValid -or -not $finalTokenValid) {
                if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info PID/token is invalid; not deleting file." }
            }
            else {
                $finalPid = [int]$finalInfo.Object.pid
                $finalToken = [string]$finalInfo.Object.token
                if (-not $mainDaemonConfirmedExited) {
                    if (-not $script:CleanupFailure) { $script:CleanupFailure = "Main daemon exit not confirmed; not deleting runtime info." }
                }
                elseif ($finalPid -eq $script:DaemonPID -and $finalToken -eq $script:DaemonToken) {
                    try {
                        Remove-Item $runtimePath -Force -ErrorAction Stop
                        Write-Host "  runtime info cleaned up."
                    } catch {
                        if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info delete failed: $($_.Exception.Message)" }
                    }
                }
                else {
                    if (-not $script:CleanupFailure) { $script:CleanupFailure = "Runtime info does not match this acceptance; not deleting file." }
                }
            }
        }
    }
}

# --- Result ----------------------------------------------------------------

if ($script:TestFailure) {
    Write-Host "TEST FAILURE: $script:TestFailure"
}
if ($script:CleanupFailure) {
    Write-Host "CLEANUP FAILURE: $script:CleanupFailure"
}

if ($script:TestFailure -or $script:CleanupFailure) {
    exit 1
}

Write-Host "=== All tests PASSED ==="
exit 0
