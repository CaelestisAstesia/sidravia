# Sidravia guided disposable Windows field validation.
#
# Runs the public CLI and shipped integration scripts from a nonce-marked
# portable sandbox. Raw command output and credentials are never written to
# the retained report. PowerShell 7 and ASCII-only.
[CmdletBinding()]
param(
    [ValidatePattern('^[a-z0-9][a-z0-9-]{0,63}$')]
    [string]$Profile = 'jlu',
    [string]$ReportDirectory = 'field-results',
    [switch]$SkipCampus,
    [switch]$SkipIntegration,
    [switch]$SkipNetworkTransition
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$script:NativeUTF8Encoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = $script:NativeUTF8Encoding
[Console]::OutputEncoding = $script:NativeUTF8Encoding

$script:Checks = New-Object System.Collections.Generic.List[object]
$script:RequestedFailure = $false
$script:PreconditionBlocked = $false
$script:CleanupFailed = $false
$script:Sandbox = $null
$script:SandboxNonce = $null
$script:Cli = $null
$script:SessionIDs = New-Object System.Collections.Generic.List[string]
$script:ConfigurationID = $null
$script:IntegrationOwned = $false
$script:SecurePassword = $null
$script:Username = $null
$script:RunStarted = [DateTimeOffset]::Now
$script:ReportPath = $null
$script:PackageHashes = [ordered]@{}

$MarkerName = '.sidravia-field-validation'
$MarkerPrefix = 'sidravia-field-validation-v1:'
$TaskName = 'SidraviaDaemon'
$script:PowerShellExe = $null
$PackageRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$TempParent = Join-Path ([IO.Path]::GetTempPath()) 'SidraviaFieldValidation'

function Resolve-ChildPowerShell {
    $executableName = 'pwsh.exe'
    $candidate = Join-Path $PSHOME $executableName
    if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
        throw 'powershell_host_not_found'
    }
    return [IO.Path]::GetFullPath($candidate)
}

function Get-SanitizedHarnessError {
    param([System.Management.Automation.ErrorRecord]$ErrorRecord)
    $allowedReasonCodes = @(
        'report_directory_outside_package',
        'unsupported_windows_or_powershell',
        'powershell_host_not_found',
        'package_input_missing',
        'package_checksum_missing',
        'package_checksum_mismatch',
        'temporary_parent_is_reparse_point',
        'sandbox_path_escape',
        'stale_cleanup_required',
        'preexisting_daemon'
    )
    $reasonCode = 'sanitized_internal_error'
    if ($allowedReasonCodes -contains $ErrorRecord.Exception.Message) {
        $reasonCode = $ErrorRecord.Exception.Message
    }
    return [pscustomobject]@{
        ReasonCode = $reasonCode
        ExceptionType = $ErrorRecord.Exception.GetType().Name
    }
}

function Test-PathUnderRoot {
    param([string]$Path, [string]$Root)
    $fullPath = [IO.Path]::GetFullPath($Path)
    $fullRoot = [IO.Path]::GetFullPath($Root).TrimEnd('\', '/')
    $prefix = $fullRoot + [IO.Path]::DirectorySeparatorChar
    return $fullPath.Equals($fullRoot, [StringComparison]::OrdinalIgnoreCase) -or
        $fullPath.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
}

function Add-Check {
    param(
        [string]$ID,
        [ValidateSet('PASS', 'FAIL', 'BLOCKED', 'SKIPPED')]
        [string]$Status,
        [string]$ReasonCode,
        [string]$Category,
        [long]$DurationMs = 0
    )
    $script:Checks.Add([pscustomobject][ordered]@{
        id = $ID
        status = $Status
        reasonCode = $ReasonCode
        category = $Category
        durationMs = $DurationMs
    })
    if ($Status -eq 'FAIL') { $script:RequestedFailure = $true }
    if ($Status -eq 'BLOCKED') { $script:PreconditionBlocked = $true }
}

function Invoke-Timed {
    param([scriptblock]$Operation)
    $watch = [Diagnostics.Stopwatch]::StartNew()
    try {
        $value = & $Operation
        return [pscustomobject]@{ Value = $value; DurationMs = $watch.ElapsedMilliseconds }
    } finally {
        $watch.Stop()
    }
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

function Invoke-CapturedProcess {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [AllowNull()][string]$StdinValue
    )
    if ($null -ne $StdinValue) {
        return Invoke-NativeWithInput -FilePath $FilePath -Arguments $Arguments -InputValue $StdinValue
    }
    $output = (& $FilePath @Arguments 2>&1 | Out-String)
    return [pscustomobject]@{ ExitCode = $LASTEXITCODE; Output = $output }
}

function Invoke-Sidravia {
    param([string[]]$Arguments, [AllowNull()][string]$StdinValue = $null)
    if (-not $script:Cli) {
        return [pscustomobject]@{ ExitCode = 127; Output = '' }
    }
    return Invoke-CapturedProcess -FilePath $script:Cli -Arguments $Arguments -StdinValue $StdinValue
}

function Invoke-SidraviaWithSecret {
    param([string[]]$Arguments, [Security.SecureString]$Secret)
    $bstr = [IntPtr]::Zero
    $plain = $null
    try {
        $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Secret)
        $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
        return Invoke-Sidravia -Arguments $Arguments -StdinValue $plain
    } finally {
        $plain = $null
        if ($bstr -ne [IntPtr]::Zero) {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
        }
    }
}

function Get-SessionID {
    param([string]$Output)
    $match = [regex]::Match($Output, 'session-\d+')
    if ($match.Success) { return $match.Value }
    return $null
}

function Get-SessionState {
    param([string]$Output)
    $match = [regex]::Match($Output, '\((suspended|waiting_for_network|authenticating|authenticated|waiting_before_retry|blocked_by_error|stopping)\)')
    if ($match.Success) { return $match.Groups[1].Value }
    return $null
}

function Wait-SessionState {
    param(
        [string]$SessionID,
        [string[]]$States,
        [int]$TimeoutSeconds
    )
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-Sidravia -Arguments @('auth', 'status', $SessionID)
        if ($result.ExitCode -eq 0) {
            $state = Get-SessionState -Output $result.Output
            if ($States -contains $state) {
                return [pscustomobject]@{ State = $state; Output = $result.Output }
            }
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    return $null
}

function Wait-DaemonRunning {
    param([int]$TimeoutSeconds)
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-Sidravia -Arguments @('daemon', 'status')
        if ($result.ExitCode -eq 0 -and $result.Output -match '\(running\)') { return $true }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    return $false
}

function Test-PathEntry {
    param([string]$PathValue, [string]$Entry)
    if (-not $PathValue) { return $false }
    $normalized = $Entry.TrimEnd('\', '/')
    foreach ($part in ($PathValue -split ';')) {
        if ($part -and ($part.TrimEnd('\', '/') -ieq $normalized)) { return $true }
    }
    return $false
}

function Get-SandboxTask {
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -eq $task) { return $null }
    if ($task.Actions.Count -ne 1) { return $null }
    $expected = Join-Path $script:Sandbox 'sidravia.exe'
    $actual = [IO.Path]::GetFullPath($task.Actions[0].Execute)
    if (-not $actual.Equals($expected, [StringComparison]::OrdinalIgnoreCase)) { return $null }
    if ($task.Actions[0].Arguments -ne 'daemon start --log-level info') { return $null }
    return $task
}

function Test-OwnedSandbox {
    param([string]$Path)
    if (-not $Path -or -not (Test-PathUnderRoot -Path $Path -Root $TempParent)) { return $false }
    $leaf = Split-Path -Leaf $Path
    if ($leaf -notmatch '^SidraviaField-([0-9a-f]{32})$') { return $false }
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
    if ($null -eq $item -or -not $item.PSIsContainer) { return $false }
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { return $false }
    $marker = Join-Path $Path $MarkerName
    if (-not (Test-Path -LiteralPath $marker -PathType Leaf)) { return $false }
    $value = [IO.File]::ReadAllText($marker).Trim()
    return $value -eq ($MarkerPrefix + $Matches[1])
}

function Remove-OwnedSandbox {
    param([string]$Path)
    if (-not (Test-OwnedSandbox -Path $Path)) { return $false }
    for ($attempt = 0; $attempt -lt 5; $attempt++) {
        try {
            if (-not (Test-OwnedSandbox -Path $Path)) { return $false }
            $directories = New-Object System.Collections.Generic.List[string]
            $files = New-Object System.Collections.Generic.List[string]
            $pending = New-Object System.Collections.Generic.Stack[string]
            $pending.Push($Path)
            while ($pending.Count -gt 0) {
                $directory = $pending.Pop()
                foreach ($child in (Get-ChildItem -LiteralPath $directory -Force -ErrorAction Stop)) {
                    if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                        return $false
                    }
                    if ($child.PSIsContainer) {
                        $directories.Add($child.FullName)
                        $pending.Push($child.FullName)
                    } else {
                        $files.Add($child.FullName)
                    }
                }
            }

            $marker = Join-Path $Path $MarkerName
            foreach ($file in $files) {
                if ($file -ieq $marker) { continue }
                $item = Get-Item -LiteralPath $file -Force -ErrorAction Stop
                if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { return $false }
                Remove-Item -LiteralPath $file -Force -ErrorAction Stop
            }
            foreach ($directory in ($directories | Sort-Object { $_.Length } -Descending)) {
                $item = Get-Item -LiteralPath $directory -Force -ErrorAction Stop
                if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { return $false }
                Remove-Item -LiteralPath $directory -Force -ErrorAction Stop
            }
            Remove-Item -LiteralPath $marker -Force -ErrorAction Stop
            Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
        } catch {
        }
        if (-not (Test-Path -LiteralPath $Path)) { return $true }
        if (-not (Test-OwnedSandbox -Path $Path)) { return $false }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Remove-StaleSandboxes {
    if (-not (Test-Path -LiteralPath $TempParent)) { return $true }
    $ok = $true
    foreach ($directory in (Get-ChildItem -LiteralPath $TempParent -Directory -Force -ErrorAction SilentlyContinue)) {
        if (-not (Test-OwnedSandbox -Path $directory.FullName)) { continue }
        $staleCli = Join-Path $directory.FullName 'sidravia.exe'
        if (Test-Path -LiteralPath $staleCli -PathType Leaf) {
            try {
                $stop = Invoke-CapturedProcess -FilePath $staleCli -Arguments @('daemon', 'stop') -StdinValue $null
                if ($stop.ExitCode -ne 0) { $ok = $false; continue }
            } catch {
                $ok = $false
                continue
            }
        }
        if (-not (Remove-OwnedSandbox -Path $directory.FullName)) { $ok = $false }
    }
    return $ok
}

function Resolve-ReportDirectory {
    $candidate = $ReportDirectory
    if (-not [IO.Path]::IsPathRooted($candidate)) {
        $candidate = Join-Path $PackageRoot $candidate
    }
    $resolved = [IO.Path]::GetFullPath($candidate)
    if (-not (Test-PathUnderRoot -Path $resolved -Root $PackageRoot)) {
        throw 'report_directory_outside_package'
    }
    return $resolved
}

function Test-PackagePreflight {
    if ($env:OS -ne 'Windows_NT' -or
        $PSVersionTable.PSEdition -ne 'Core' -or
        $PSVersionTable.PSVersion.Major -lt 7) {
        throw 'unsupported_windows_or_powershell'
    }
    $script:PowerShellExe = Resolve-ChildPowerShell
    $required = @(
        'sidravia.exe',
        'sidraviad.exe',
        'BUILD-INFO.txt',
        'SHA256SUMS',
        (Join-Path 'institution-profiles' ($Profile + '.json')),
        (Join-Path 'scripts' 'cli-smoke.ps1'),
        (Join-Path 'scripts' 'install.ps1'),
        (Join-Path 'scripts' 'uninstall.ps1')
    )
    foreach ($relative in $required) {
        if (-not (Test-Path -LiteralPath (Join-Path $PackageRoot $relative) -PathType Leaf)) {
            throw 'package_input_missing'
        }
    }

    $expected = @{}
    foreach ($line in [IO.File]::ReadAllLines((Join-Path $PackageRoot 'SHA256SUMS'))) {
        if ($line -match '^([0-9a-fA-F]{64})  (sidravia(?:d)?\.exe)$') {
            $expected[$Matches[2]] = $Matches[1].ToLowerInvariant()
        }
    }
    foreach ($name in @('sidravia.exe', 'sidraviad.exe')) {
        if (-not $expected.ContainsKey($name)) { throw 'package_checksum_missing' }
        $actual = (Get-FileHash -LiteralPath (Join-Path $PackageRoot $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected[$name]) { throw 'package_checksum_mismatch' }
        $script:PackageHashes[$name] = $actual
    }
}

function New-Sandbox {
    if (-not (Test-Path -LiteralPath $TempParent)) {
        $null = New-Item -ItemType Directory -Path $TempParent
    }
    $parentItem = Get-Item -LiteralPath $TempParent -Force
    if (($parentItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'temporary_parent_is_reparse_point'
    }

    $script:SandboxNonce = [guid]::NewGuid().ToString('N')
    $script:Sandbox = Join-Path $TempParent ('SidraviaField-' + $script:SandboxNonce)
    $null = New-Item -ItemType Directory -Path $script:Sandbox
    [IO.File]::WriteAllText((Join-Path $script:Sandbox $MarkerName), $MarkerPrefix + $script:SandboxNonce)
    $null = New-Item -ItemType Directory -Path (Join-Path $script:Sandbox 'institution-profiles')
    $null = New-Item -ItemType Directory -Path (Join-Path $script:Sandbox 'scripts')

    foreach ($name in @('sidravia.exe', 'sidraviad.exe', 'BUILD-INFO.txt', 'SHA256SUMS', 'README.md', 'GETTING-STARTED.md', 'LICENSE')) {
        $source = Join-Path $PackageRoot $name
        if (Test-Path -LiteralPath $source -PathType Leaf) {
            Copy-Item -LiteralPath $source -Destination (Join-Path $script:Sandbox $name)
        }
    }
    Copy-Item -LiteralPath (Join-Path $PackageRoot (Join-Path 'institution-profiles' ($Profile + '.json'))) -Destination (Join-Path $script:Sandbox (Join-Path 'institution-profiles' ($Profile + '.json')))
    foreach ($name in @('cli-smoke.ps1', 'install.ps1', 'uninstall.ps1')) {
        Copy-Item -LiteralPath (Join-Path $PackageRoot (Join-Path 'scripts' $name)) -Destination (Join-Path $script:Sandbox (Join-Path 'scripts' $name))
    }
    [IO.File]::WriteAllText((Join-Path $script:Sandbox 'sidravia.portable'), '')
    $script:Cli = Join-Path $script:Sandbox 'sidravia.exe'
    if (-not (Test-PathUnderRoot -Path $script:Cli -Root $script:Sandbox)) { throw 'sandbox_path_escape' }
}

function Invoke-LocalSuite {
    $timed = Invoke-Timed {
        Invoke-CapturedProcess -FilePath $script:PowerShellExe -Arguments @(
            '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
            '-File', (Join-Path $script:Sandbox (Join-Path 'scripts' 'cli-smoke.ps1')),
            '-Sidravia', $script:Cli, '-SkipAuthStart'
        ) -StdinValue $null
    }
    if ($timed.Value.ExitCode -eq 0 -and $timed.Value.Output -match 'Failed:\s*0') {
        Add-Check -ID 'local.cli_surface' -Status PASS -ReasonCode 'all_checks_passed' -Category automatic -DurationMs $timed.DurationMs
        return $true
    }
    Write-Host ''
    Write-Host '== Local CLI smoke diagnostics (no field credentials) =='
    if ($timed.Value.Output) {
        Write-Host $timed.Value.Output.TrimEnd()
    } else {
        Write-Host '[no output]'
    }
    Add-Check -ID 'local.cli_surface' -Status FAIL -ReasonCode 'cli_smoke_failed' -Category automatic -DurationMs $timed.DurationMs
    return $false
}

function Invoke-IntegrationSuite {
    if ($SkipIntegration) {
        Add-Check -ID 'integration.user_mode' -Status SKIPPED -ReasonCode 'requested_skip' -Category integration
        return
    }
    $approval = Read-Host 'Temporarily test current-user PATH and SidraviaDaemon task? Type YES'
    if ($approval -cne 'YES') {
        Add-Check -ID 'integration.user_mode' -Status SKIPPED -ReasonCode 'consent_declined' -Category integration
        return
    }

    $beforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $beforeTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ((Test-PathEntry -PathValue $beforePath -Entry $script:Sandbox) -or $null -ne $beforeTask) {
        Add-Check -ID 'integration.user_mode' -Status BLOCKED -ReasonCode 'preexisting_integration_state' -Category integration
        return
    }
    $script:IntegrationOwned = $true

    $watch = [Diagnostics.Stopwatch]::StartNew()
    $install = Join-Path $script:Sandbox (Join-Path 'scripts' 'install.ps1')
    $uninstall = Join-Path $script:Sandbox (Join-Path 'scripts' 'uninstall.ps1')
    $first = Invoke-CapturedProcess -FilePath $script:PowerShellExe -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $install, '-LogLevel', 'info') -StdinValue $null
    $second = Invoke-CapturedProcess -FilePath $script:PowerShellExe -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $install, '-LogLevel', 'info') -StdinValue $null

    $task = Get-SandboxTask
    $pathPresent = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
    $taskStarted = $false
    if ($null -ne $task -and $pathPresent) {
        Start-ScheduledTask -TaskName $TaskName
        $taskStarted = Wait-DaemonRunning -TimeoutSeconds 15
        $null = Invoke-Sidravia -Arguments @('daemon', 'stop')
    }

    $removeFirst = Invoke-CapturedProcess -FilePath $script:PowerShellExe -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $uninstall) -StdinValue $null
    $removeSecond = Invoke-CapturedProcess -FilePath $script:PowerShellExe -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $uninstall) -StdinValue $null
    $finalTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    $finalPath = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
    if ($null -eq $finalTask -and -not $finalPath) { $script:IntegrationOwned = $false }
    $watch.Stop()

    $ok = $first.ExitCode -eq 0 -and $second.ExitCode -eq 0 -and $taskStarted -and
        $removeFirst.ExitCode -eq 0 -and $removeSecond.ExitCode -eq 0 -and
        $null -eq $finalTask -and -not $finalPath
    if ($ok) {
        Add-Check -ID 'integration.user_mode' -Status PASS -ReasonCode 'idempotent_install_action_uninstall' -Category integration -DurationMs $watch.ElapsedMilliseconds
    } else {
        Add-Check -ID 'integration.user_mode' -Status FAIL -ReasonCode 'integration_contract_failed' -Category integration -DurationMs $watch.ElapsedMilliseconds
    }
}

function Test-FixedPortOwnership {
    if ($Profile -ne 'jlu') { return $true }
    $daemonPids = @()
    foreach ($process in (Get-Process -Name 'sidraviad' -ErrorAction SilentlyContinue)) {
        try {
            if ($process.Path -and [IO.Path]::GetFullPath($process.Path).Equals((Join-Path $script:Sandbox 'sidraviad.exe'), [StringComparison]::OrdinalIgnoreCase)) {
                $daemonPids += $process.Id
            }
        } catch {
        }
    }
    if ($daemonPids.Count -eq 0) { return $false }
    foreach ($endpoint in (Get-NetUDPEndpoint -LocalPort 61440 -ErrorAction SilentlyContinue)) {
        if ($daemonPids -contains $endpoint.OwningProcess) { return $true }
    }
    return $false
}

function Wait-ProtocolPhase {
    param([string]$Phase, [int]$TimeoutSeconds)
    $log = Join-Path $script:Sandbox (Join-Path 'logs' 'sidraviad.log')
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    $pattern = 'event=protocol_phase.*phase=' + [regex]::Escape($Phase) + '.*boundary=end'
    do {
        if (Test-Path -LiteralPath $log -PathType Leaf) {
            if (Select-String -LiteralPath $log -Pattern $pattern -Quiet) { return $true }
        }
        Start-Sleep -Seconds 1
    } while ([DateTime]::UtcNow -lt $deadline)
    return $false
}

function Get-HeartbeatWaitSeconds {
    try {
        $profilePath = Join-Path $script:Sandbox (Join-Path 'institution-profiles' ($Profile + '.json'))
        $document = [IO.File]::ReadAllText($profilePath) | ConvertFrom-Json
        $value = [string]$document.institutionProtocolConfiguration.heartbeatInterval
        if ($value -match '^(\d+)s$') { return [Math]::Min(90, [int]$Matches[1] + 15) }
    } catch {
    }
    return 45
}

function Invoke-NetworkTransition {
    param([string]$SessionID)
    if ($SkipNetworkTransition) {
        Add-Check -ID 'campus.network_transition' -Status SKIPPED -ReasonCode 'requested_skip' -Category campus
        return $true
    }
    $ready = Read-Host 'Type READY, then perform the declared hotspot/network transition within 10 seconds'
    if ($ready -cne 'READY') {
        Add-Check -ID 'campus.network_transition' -Status SKIPPED -ReasonCode 'transition_declined' -Category campus
        return $true
    }
    Write-Host 'Observing Session state; perform the transition now.'
    $deadline = [DateTime]::UtcNow.AddSeconds(120)
    $observedLoss = $false
    $recovered = $false
    do {
        $status = Invoke-Sidravia -Arguments @('auth', 'status', $SessionID)
        if ($status.ExitCode -eq 0) {
            $state = Get-SessionState -Output $status.Output
            if ($state -and $state -ne 'authenticated') { $observedLoss = $true }
            if ($observedLoss -and $state -eq 'authenticated') { $recovered = $true; break }
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($recovered) {
        Add-Check -ID 'campus.network_transition' -Status PASS -ReasonCode 'loss_and_recovery_observed' -Category campus
        return $true
    } else {
        Add-Check -ID 'campus.network_transition' -Status FAIL -ReasonCode 'loss_or_recovery_not_observed' -Category campus
        return $false
    }
}

function Remove-TestSession {
    param([string]$SessionID)
    if (-not $SessionID) { return $true }
    $result = Invoke-Sidravia -Arguments @('auth', 'remove', $SessionID)
    if ($result.ExitCode -eq 0) {
        $null = $script:SessionIDs.Remove($SessionID)
        return $true
    }
    return $false
}

function Invoke-OneShotCampusSuite {
    $start = Invoke-SidraviaWithSecret -Arguments @('auth', 'start', '--profile', $Profile, '--username', $script:Username, '--password-stdin') -Secret $script:SecurePassword
    $sessionID = Get-SessionID -Output $start.Output
    if ($start.ExitCode -ne 0 -or -not $sessionID) {
        Add-Check -ID 'campus.oneshot_start' -Status FAIL -ReasonCode 'session_start_failed' -Category campus
        return $false
    }
    $script:SessionIDs.Add($sessionID)
    Add-Check -ID 'campus.oneshot_start' -Status PASS -ReasonCode 'session_created' -Category campus
    $ok = $true

    $authenticated = Wait-SessionState -SessionID $sessionID -States @('authenticated') -TimeoutSeconds 60
    if ($null -eq $authenticated) {
        Add-Check -ID 'campus.authentication' -Status FAIL -ReasonCode 'not_authenticated' -Category campus
        return $false
    }
    $bindingPresent = $authenticated.Output -match '(?m)^\u7f51\u7edc\uff1a[^\r\n]*(?:\d{1,3}\.){3}\d{1,3}[^\r\n]*$'
    if ($bindingPresent) {
        Add-Check -ID 'campus.selected_binding' -Status PASS -ReasonCode 'binding_present' -Category campus
    } else {
        Add-Check -ID 'campus.selected_binding' -Status FAIL -ReasonCode 'binding_missing' -Category campus
        $ok = $false
    }

    if (Test-FixedPortOwnership) {
        Add-Check -ID 'campus.fixed_port' -Status PASS -ReasonCode 'owned_61440' -Category campus
    } else {
        Add-Check -ID 'campus.fixed_port' -Status FAIL -ReasonCode 'fixed_port_owner_mismatch' -Category campus
        $ok = $false
    }

    $heartbeatSeconds = Get-HeartbeatWaitSeconds
    $ka1 = Wait-ProtocolPhase -Phase 'keepalive_ka1' -TimeoutSeconds $heartbeatSeconds
    $ka2 = Wait-ProtocolPhase -Phase 'keepalive_ka2' -TimeoutSeconds 15
    if ($ka1 -and $ka2) {
        Add-Check -ID 'campus.heartbeat' -Status PASS -ReasonCode 'keepalive_phases_completed' -Category campus
    } else {
        Add-Check -ID 'campus.heartbeat' -Status FAIL -ReasonCode 'keepalive_phase_missing' -Category campus
        $ok = $false
    }

    $list = Invoke-Sidravia -Arguments @('auth', 'list')
    $status = Invoke-Sidravia -Arguments @('auth', 'status', $sessionID)
    $restart = Invoke-Sidravia -Arguments @('auth', 'restart', $sessionID)
    $restarted = Wait-SessionState -SessionID $sessionID -States @('authenticated') -TimeoutSeconds 60
    if ($list.ExitCode -eq 0 -and $list.Output -match [regex]::Escape($sessionID) -and
        $status.ExitCode -eq 0 -and $restart.ExitCode -eq 0 -and $null -ne $restarted) {
        Add-Check -ID 'campus.retained_lifecycle' -Status PASS -ReasonCode 'list_status_restart' -Category campus
    } else {
        Add-Check -ID 'campus.retained_lifecycle' -Status FAIL -ReasonCode 'retained_operation_failed' -Category campus
        $ok = $false
    }

    if (-not (Invoke-NetworkTransition -SessionID $sessionID)) { $ok = $false }

    $stop = Invoke-Sidravia -Arguments @('auth', 'stop', $sessionID)
    $suspended = Wait-SessionState -SessionID $sessionID -States @('suspended') -TimeoutSeconds 30
    $logout = Wait-ProtocolPhase -Phase 'logout' -TimeoutSeconds 5
    if ($stop.ExitCode -eq 0 -and $null -ne $suspended -and $logout) {
        Add-Check -ID 'campus.logout_stop' -Status PASS -ReasonCode 'logout_and_suspended' -Category campus
    } else {
        Add-Check -ID 'campus.logout_stop' -Status FAIL -ReasonCode 'logout_or_suspend_missing' -Category campus
        $ok = $false
    }
    if (Remove-TestSession -SessionID $sessionID) {
        Add-Check -ID 'campus.session_remove' -Status PASS -ReasonCode 'session_absent' -Category campus
    } else {
        Add-Check -ID 'campus.session_remove' -Status FAIL -ReasonCode 'session_remove_failed' -Category campus
        $ok = $false
    }
    return $ok
}

function Invoke-TemporaryConfigurationSuite {
    $approval = Read-Host 'Temporarily store the entered credentials in the protected sandbox to test AutoLogin? Type YES'
    if ($approval -cne 'YES') {
        Add-Check -ID 'campus.configuration_autologin' -Status SKIPPED -ReasonCode 'consent_declined' -Category campus
        return
    }
    $script:ConfigurationID = 'field-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
    $create = Invoke-SidraviaWithSecret -Arguments @(
        'config', 'create', '--id', $script:ConfigurationID, '--profile', $Profile,
        '--username', $script:Username, '--password-stdin', '--auto-login=true', '--auto-reconnect=true'
    ) -Secret $script:SecurePassword
    if ($create.ExitCode -ne 0) {
        $reason = 'temporary_configuration_failed'
        if ($create.Output -match 'unprotected|insecure') { $reason = 'protected_storage_unavailable' }
        Add-Check -ID 'campus.configuration_autologin' -Status BLOCKED -ReasonCode $reason -Category campus
        $script:ConfigurationID = $null
        return
    }
    if ($create.Output -notmatch '\(protected\)') {
        Add-Check -ID 'campus.configuration_autologin' -Status FAIL -ReasonCode 'protected_result_missing' -Category campus
        return
    }

    $restart = Invoke-Sidravia -Arguments @('daemon', 'restart', '--log-level', 'debug')
    $sessionID = $null
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $list = Invoke-Sidravia -Arguments @('auth', 'list')
        if ($list.ExitCode -eq 0) { $sessionID = Get-SessionID -Output $list.Output }
        if (-not $sessionID) { Start-Sleep -Seconds 1 }
    } while (-not $sessionID -and [DateTime]::UtcNow -lt $deadline)
    if ($sessionID) { $script:SessionIDs.Add($sessionID) }
    $authenticated = $null
    if ($sessionID) {
        $authenticated = Wait-SessionState -SessionID $sessionID -States @('authenticated') -TimeoutSeconds 60
    }
    if ($restart.ExitCode -eq 0 -and $sessionID -and $null -ne $authenticated) {
        Add-Check -ID 'campus.configuration_autologin' -Status PASS -ReasonCode 'protected_autologin_authenticated' -Category campus
    } else {
        Add-Check -ID 'campus.configuration_autologin' -Status FAIL -ReasonCode 'autologin_not_authenticated' -Category campus
    }

    $remove = Invoke-Sidravia -Arguments @('config', 'remove', $script:ConfigurationID, '--yes')
    if ($remove.ExitCode -eq 0) {
        $script:ConfigurationID = $null
        if ($sessionID) { $null = $script:SessionIDs.Remove($sessionID) }
    }
}

function Invoke-CampusSuite {
    if ($SkipCampus) {
        Add-Check -ID 'campus.full_chain' -Status SKIPPED -ReasonCode 'requested_skip' -Category campus
        return
    }
    Write-Output 'Stop other Dr.COM clients. Stop TUN or exclude RFC1918 campus routes.'
    $ready = Read-Host 'Confirm the campus link is ready by typing YES'
    if ($ready -cne 'YES') {
        Add-Check -ID 'campus.full_chain' -Status BLOCKED -ReasonCode 'campus_precondition_declined' -Category campus
        return
    }
    $script:Username = Read-Host 'Campus username'
    if ([string]::IsNullOrWhiteSpace($script:Username)) {
        Add-Check -ID 'campus.full_chain' -Status BLOCKED -ReasonCode 'username_missing' -Category campus
        return
    }
    $script:SecurePassword = Read-Host -AsSecureString -Prompt 'Campus password'
    if ($null -eq $script:SecurePassword -or $script:SecurePassword.Length -eq 0) {
        Add-Check -ID 'campus.full_chain' -Status BLOCKED -ReasonCode 'password_missing' -Category campus
        return
    }

    $daemon = Invoke-Sidravia -Arguments @('daemon', 'start', '--log-level', 'debug')
    if ($daemon.ExitCode -ne 0) {
        Add-Check -ID 'campus.daemon' -Status FAIL -ReasonCode 'daemon_start_failed' -Category windows_native
        return
    }
    Add-Check -ID 'campus.daemon' -Status PASS -ReasonCode 'debug_daemon_running' -Category windows_native
    $oneShotOK = Invoke-OneShotCampusSuite
    if ($oneShotOK) {
        Invoke-TemporaryConfigurationSuite
    } else {
        Add-Check -ID 'campus.configuration_autologin' -Status SKIPPED -ReasonCode 'one_shot_failed' -Category campus
    }
}

function Invoke-OwnedCleanup {
    $ok = $true
    $sandboxRemovalSafe = $true
    if ($script:Cli -and (Test-Path -LiteralPath $script:Cli -PathType Leaf)) {
        foreach ($sessionID in $script:SessionIDs.ToArray()) {
            try {
                if (-not (Remove-TestSession -SessionID $sessionID)) {
                    $ok = $false
                    $sandboxRemovalSafe = $false
                }
            } catch {
                $ok = $false
                $sandboxRemovalSafe = $false
            }
        }
        if ($script:ConfigurationID) {
            try {
                $remove = Invoke-Sidravia -Arguments @('config', 'remove', $script:ConfigurationID, '--yes')
                if ($remove.ExitCode -eq 0) {
                    $script:ConfigurationID = $null
                } else {
                    $ok = $false
                    $sandboxRemovalSafe = $false
                }
            } catch {
                $ok = $false
                $sandboxRemovalSafe = $false
            }
        }
        try {
            $stop = Invoke-Sidravia -Arguments @('daemon', 'stop')
            if ($stop.ExitCode -ne 0) {
                $ok = $false
                $sandboxRemovalSafe = $false
            }
        } catch {
            $ok = $false
            $sandboxRemovalSafe = $false
        }
    }

    if ($script:IntegrationOwned -and $script:Sandbox) {
        try {
            $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
            $ownedTask = Get-SandboxTask
            $ownedPath = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
            if ($null -ne $task -and $null -eq $ownedTask) {
                $ok = $false
                $sandboxRemovalSafe = $false
            } elseif ($null -ne $ownedTask -or $ownedPath) {
                $uninstall = Join-Path $script:Sandbox (Join-Path 'scripts' 'uninstall.ps1')
                $result = Invoke-CapturedProcess -FilePath $script:PowerShellExe -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $uninstall) -StdinValue $null
                if ($result.ExitCode -ne 0) {
                    $ok = $false
                    $sandboxRemovalSafe = $false
                }
            }

            $remainingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
            $remainingPath = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
            if ($null -eq $remainingTask -and -not $remainingPath) {
                $script:IntegrationOwned = $false
            } else {
                $ok = $false
                $sandboxRemovalSafe = $false
            }
        } catch {
            $ok = $false
            $sandboxRemovalSafe = $false
        }
    }

    if ($script:SecurePassword) {
        try {
            $script:SecurePassword.Dispose()
        } catch {
            $ok = $false
        } finally {
            $script:SecurePassword = $null
        }
    }
    $script:Username = $null

    if ($script:Sandbox -and (Test-Path -LiteralPath $script:Sandbox)) {
        if ($sandboxRemovalSafe -and -not $script:IntegrationOwned) {
            try {
                if (-not (Remove-OwnedSandbox -Path $script:Sandbox)) { $ok = $false }
            } catch {
                $ok = $false
            }
        } else {
            $ok = $false
        }
    }
    return $ok
}

function Write-AllowlistReport {
    param([string]$Directory)
    if (-not (Test-Path -LiteralPath $Directory)) {
        $null = New-Item -ItemType Directory -Path $Directory
    }
    $timestamp = [DateTimeOffset]::Now.ToString('yyyyMMdd-HHmmss')
    $final = Join-Path $Directory ('sidravia-field-' + $timestamp + '.json')
    $temporary = $final + '.tmp'
    $overall = 'PASS'
    if ($script:CleanupFailed) { $overall = 'CLEANUP_REQUIRED' }
    elseif ($script:RequestedFailure) { $overall = 'FAIL' }
    elseif ($script:PreconditionBlocked) { $overall = 'BLOCKED' }
    $report = [ordered]@{
        schemaVersion = 1
        startedAt = $script:RunStarted.ToString('o')
        completedAt = [DateTimeOffset]::Now.ToString('o')
        overall = $overall
        productHashes = $script:PackageHashes
        platform = [ordered]@{
            os = [Environment]::OSVersion.VersionString
            powershell = $PSVersionTable.PSVersion.ToString()
        }
        evidence = [ordered]@{
            automatic = 'reported_per_check'
            windowsNative = 'reported_per_check'
            campusNetwork = 'reported_per_check'
            integration = 'reported_per_check'
            releaseReadiness = 'not_tested'
        }
        checks = $script:Checks.ToArray()
        cleanup = $(if ($script:CleanupFailed) { 'incomplete' } else { 'complete' })
    }
    try {
        [IO.File]::WriteAllText($temporary, ($report | ConvertTo-Json -Depth 8), (New-Object Text.UTF8Encoding($false)))
        Move-Item -LiteralPath $temporary -Destination $final
    } catch {
        if (Test-Path -LiteralPath $temporary -PathType Leaf) {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
        }
        throw
    }
    $script:ReportPath = $final
}

$reportRoot = $null
try {
    $reportRoot = Resolve-ReportDirectory
    $preflight = Invoke-Timed { Test-PackagePreflight }
    Add-Check -ID 'preflight.package' -Status PASS -ReasonCode 'package_verified' -Category automatic -DurationMs $preflight.DurationMs

    if (-not (Remove-StaleSandboxes)) {
        Add-Check -ID 'preflight.stale_cleanup' -Status BLOCKED -ReasonCode 'stale_cleanup_required' -Category cleanup
        throw 'stale_cleanup_required'
    }
    Add-Check -ID 'preflight.stale_cleanup' -Status PASS -ReasonCode 'no_owned_stale_state' -Category cleanup

    if (@(Get-Process -Name 'sidraviad' -ErrorAction SilentlyContinue).Count -gt 0) {
        Add-Check -ID 'preflight.daemon' -Status BLOCKED -ReasonCode 'preexisting_daemon' -Category windows_native
        throw 'preexisting_daemon'
    }
    Add-Check -ID 'preflight.daemon' -Status PASS -ReasonCode 'no_preexisting_daemon' -Category windows_native

    New-Sandbox
    Add-Check -ID 'preflight.sandbox' -Status PASS -ReasonCode 'owned_portable_sandbox' -Category cleanup
    $localOK = Invoke-LocalSuite
    if ($localOK) {
        Invoke-IntegrationSuite
        Invoke-CampusSuite
    } else {
        Add-Check -ID 'integration.user_mode' -Status SKIPPED -ReasonCode 'local_suite_failed' -Category integration
        Add-Check -ID 'campus.full_chain' -Status SKIPPED -ReasonCode 'local_suite_failed' -Category campus
    }
} catch {
    if (-not $script:PreconditionBlocked -and -not $script:RequestedFailure) {
        $errorDetails = Get-SanitizedHarnessError -ErrorRecord $_
        Add-Check -ID 'harness.execution' -Status FAIL -ReasonCode $errorDetails.ReasonCode -Category automatic
        Write-Host ('Harness execution failed [' + $errorDetails.ReasonCode + ':' + $errorDetails.ExceptionType + ']')
    }
} finally {
    $cleanup = $false
    try {
        $cleanup = Invoke-OwnedCleanup
    } catch {
        $cleanup = $false
    }
    if ($cleanup) {
        Add-Check -ID 'cleanup.owned_state' -Status PASS -ReasonCode 'all_owned_state_removed' -Category cleanup
    } else {
        $script:CleanupFailed = $true
        Add-Check -ID 'cleanup.owned_state' -Status FAIL -ReasonCode 'cleanup_incomplete' -Category cleanup
    }
    if (-not $reportRoot) {
        $reportRoot = Join-Path $PackageRoot 'field-results'
    }
    try {
        Write-AllowlistReport -Directory $reportRoot
    } catch {
        $reportErrorType = $_.Exception.GetType().Name
        $script:CleanupFailed = $true
        Add-Check -ID 'report.sanitized' -Status FAIL -ReasonCode 'report_write_failed' -Category cleanup
        Write-Host ('Sanitized report write failed [report_write_failed:' + $reportErrorType + ']')
    }
}

Write-Output ''
Write-Output '== Sidravia field validation =='
foreach ($check in $script:Checks) {
    Write-Output ('{0,-7} {1} [{2}]' -f $check.status, $check.id, $check.reasonCode)
}
if ($script:ReportPath) { Write-Output ('Sanitized report: ' + $script:ReportPath) }

if ($script:CleanupFailed) { exit 3 }
if ($script:RequestedFailure) { exit 1 }
if ($script:PreconditionBlocked) { exit 2 }
exit 0
