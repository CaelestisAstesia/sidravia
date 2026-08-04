# Sidravia 引导式一次性 Windows 现场验证。
#
# 本脚本在带随机标记的便携沙箱中运行公开 CLI 和随包集成脚本。
# 原始命令输出和凭据绝不会写入保留报告。本脚本仅支持 PowerShell 7，并使用 UTF-8 编码。
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
$script:ReleasePowerShell = $null
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

# Resolve-ReleasePowerShell is the only release-script host resolver. It accepts
# only the system Windows PowerShell Desktop 5.1 and returns its path and version
# after proving PSEdition=Desktop, Major=5 and Minor=1 in a read-only probe.
function Resolve-ReleasePowerShell {
    $candidate = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
        throw 'release_powershell_not_found'
    }
    $probeCommand = 'if ($PSVersionTable.PSEdition -ne ''Desktop'' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) { exit 1 }; $PSVersionTable.PSVersion.ToString()'
    $probe = Invoke-CapturedProcess -FilePath $candidate -Arguments @('-NoProfile', '-NonInteractive', '-Command', $probeCommand) -StdinValue $null
    if ($probe.ExitCode -ne 0) {
        throw 'unsupported_release_script_host'
    }
    $versionText = ($probe.Output -split '\r?\n' | Where-Object { $_.Trim() } | Select-Object -First 1).Trim()
    if (-not $versionText) {
        throw 'unsupported_release_script_host'
    }
    return [pscustomobject]@{
        Path = [IO.Path]::GetFullPath($candidate)
        Version = $versionText
    }
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
    $match = [regex]::Match($Output, '(?:\(|\uFF08)(suspended|waiting_for_network|authenticating|authenticated|waiting_before_retry|blocked_by_error|stopping)(?:\)|\uFF09)')
    if ($match.Success) { return $match.Groups[1].Value }
    return $null
}

function Get-AllowlistedSessionDiagnostics {
    param([string]$Output)
    $stateReason = $null
    foreach ($code in @(
        'network_unavailable',
        'runtime_definition_unavailable',
        'protocol_run_creation_failed',
        'protocol_run_failed',
        'protocol_contract_violated',
        'automatic_reconnect_disabled'
    )) {
        $pattern = '(?m)^\u539F\u56E0\uFF1A[^\r\n]*\uFF08' + [regex]::Escape($code) + '\uFF09\s*$'
        if ([regex]::IsMatch($Output, $pattern)) { $stateReason = $code; break }
    }

    $failure = $null
    foreach ($code in @(
        'network_timeout',
        'network_io_failed',
        'server_busy',
        'session_in_use',
        'credential_invalid',
        'insufficient_funds',
        'account_frozen',
        'binding_ip_mismatch',
        'binding_mac_mismatch',
        'too_many_sessions',
        'incompatible_version',
        'binding_pair_mismatch',
        'dhcp_required',
        'authentication_rejected',
        'protocol_response_invalid',
        'protocol_contract_violated',
        'logout_cleanup_failed'
    )) {
        $pattern = '(?m)^\u6700\u8FD1\u5931\u8D25\uFF1A[^\r\n]*\uFF08' + [regex]::Escape($code) + '\uFF09\s*$'
        if ([regex]::IsMatch($Output, $pattern)) { $failure = $code; break }
    }

    $recommendation = $null
    foreach ($code in @(
        'retry_after_standard_delay',
        'retry_after_extended_delay',
        'block_until_explicit_restart_or_relevant_input_change'
    )) {
        $pattern = '(?m)^\u5904\u7406\u5EFA\u8BAE\uFF1A[^\r\n]*\uFF08' + [regex]::Escape($code) + '\uFF09\s*$'
        if ([regex]::IsMatch($Output, $pattern)) { $recommendation = $code; break }
    }

    return [pscustomobject]@{
        State = Get-SessionState -Output $Output
        StateReason = $stateReason
        Failure = $failure
        Recommendation = $recommendation
    }
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

function Test-ParenthesizedMachineToken {
    param([string]$Output, [string]$Token)
    if ([string]::IsNullOrEmpty($Output) -or [string]::IsNullOrEmpty($Token)) { return $false }
    $pattern = '(?:\(|\uFF08)' + [regex]::Escape($Token) + '(?:\)|\uFF09)'
    return [regex]::IsMatch($Output, $pattern)
}

function Wait-DaemonRunning {
    param([int]$TimeoutSeconds)
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-Sidravia -Arguments @('daemon', 'status')
        if ($result.ExitCode -eq 0 -and
            (Test-ParenthesizedMachineToken -Output $result.Output -Token 'running')) { return $true }
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

function Get-SandboxTask {
    $task = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
    if ($null -eq $task) { return $null }
    if ($task.Actions.Count -ne 1) { return $null }
    if ($task.Triggers.Count -ne 1) { return $null }
    $currentUserSID = $null
    try {
        $currentIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
        if ($null -ne $currentIdentity.User) {
            $currentUserSID = $currentIdentity.User.Value
        }
    } catch {
    }
    if (-not $currentUserSID) { return $null }
    $principalSID = ConvertTo-SIDValue -Identity ([string]$task.Principal.UserId)
    $triggerSID = ConvertTo-SIDValue -Identity ([string]$task.Triggers[0].UserId)
    if ($principalSID -ne $currentUserSID -or $triggerSID -ne $currentUserSID) { return $null }
    if ([string]$task.Principal.LogonType -ne 'Interactive' -or
        [string]$task.Principal.RunLevel -ne 'Limited') { return $null }
    $expected = [IO.Path]::GetFullPath((Join-Path $script:Sandbox 'sidravia.exe'))
    $actual = [IO.Path]::GetFullPath($task.Actions[0].Execute)
    if (-not $actual.Equals($expected, [StringComparison]::OrdinalIgnoreCase)) { return $null }
    if ($task.Actions[0].Arguments -notin @('daemon start --log-level info', 'daemon start --log-level debug', 'daemon start --log-level trace')) { return $null }
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
    if ($timed.Value.ExitCode -eq 0 -and $timed.Value.Output -match '失败：\s*0') {
        Add-Check -ID 'local.cli_surface' -Status PASS -ReasonCode 'all_checks_passed' -Category automatic -DurationMs $timed.DurationMs
        return $true
    }
    Write-Host ''
    Write-Host '== 本地 CLI 冒烟测试诊断（不含现场凭据） =='
    if ($timed.Value.Output) {
        Write-Host $timed.Value.Output.TrimEnd()
    } else {
        Write-Host '[无输出]'
    }
    Add-Check -ID 'local.cli_surface' -Status FAIL -ReasonCode 'cli_smoke_failed' -Category automatic -DurationMs $timed.DurationMs
    return $false
}

function Add-IntegrationProcessCheck {
    param(
        [string]$ID,
        [object]$Result
    )
    if ($Result.ExitCode -eq 0) {
        Add-Check -ID $ID -Status PASS -ReasonCode 'command_succeeded' -Category integration
        return $true
    }
    Write-Host ''
    Write-Host ('== 用户态集成诊断：' + $ID + '（不含现场凭据） ==')
    if ($Result.Output) { Write-Host $Result.Output.TrimEnd() }
    else { Write-Host '[无输出]' }
    $reason = 'command_failed'
    if ($Result.Output -match 'scheduled_task_registration_failed') {
        $reason = 'scheduled_task_registration_failed'
    }
    Add-Check -ID $ID -Status FAIL -ReasonCode $reason -Category integration
    return $false
}

function Invoke-IntegrationSuite {
    if ($SkipIntegration) {
        Add-Check -ID 'integration.user_mode' -Status SKIPPED -ReasonCode 'requested_skip' -Category integration
        return
    }
    $approval = Read-Host '临时测试当前用户 PATH 和 SidraviaDaemon 计划任务？请输入 YES'
    if ($approval -cne 'YES') {
        Add-Check -ID 'integration.user_mode' -Status SKIPPED -ReasonCode 'consent_declined' -Category integration
        return
    }

    $beforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $beforeTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
    if ((Test-PathEntry -PathValue $beforePath -Entry $script:Sandbox) -or $null -ne $beforeTask) {
        Add-Check -ID 'integration.user_mode' -Status BLOCKED -ReasonCode 'preexisting_integration_state' -Category integration
        return
    }
    $script:IntegrationOwned = $true

    $watch = [Diagnostics.Stopwatch]::StartNew()
    $install = Join-Path $script:Sandbox (Join-Path 'scripts' 'install.ps1')
    $uninstall = Join-Path $script:Sandbox (Join-Path 'scripts' 'uninstall.ps1')
    $first = Invoke-CapturedProcess -FilePath $script:ReleasePowerShell.Path -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $install, '-LogLevel', 'info') -StdinValue $null
    $second = Invoke-CapturedProcess -FilePath $script:ReleasePowerShell.Path -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $install, '-LogLevel', 'info') -StdinValue $null
    $firstOK = Add-IntegrationProcessCheck -ID 'integration.install_first' -Result $first
    $secondOK = Add-IntegrationProcessCheck -ID 'integration.install_second' -Result $second

    $task = Get-SandboxTask
    $pathPresent = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
    if ($pathPresent) {
        Add-Check -ID 'integration.path_registered' -Status PASS -ReasonCode 'path_present' -Category integration
    } else {
        Add-Check -ID 'integration.path_registered' -Status FAIL -ReasonCode 'path_missing' -Category integration
    }
    if ($null -ne $task) {
        Add-Check -ID 'integration.task_registered' -Status PASS -ReasonCode 'owned_task_present' -Category integration
    } else {
        Add-Check -ID 'integration.task_registered' -Status FAIL -ReasonCode 'owned_task_missing_or_mismatched' -Category integration
    }
    $taskStarted = $false
    $taskStartCommandFailed = $false
    if ($null -ne $task -and $pathPresent) {
        try {
            Start-ScheduledTask -TaskName $TaskName -TaskPath '\'
            $taskStarted = Wait-DaemonRunning -TimeoutSeconds 15
        } catch {
            $taskStartCommandFailed = $true
        }
        if ($taskStartCommandFailed) {
            Add-Check -ID 'integration.task_start' -Status FAIL -ReasonCode 'task_start_command_failed' -Category integration
        } elseif ($taskStarted) {
            Add-Check -ID 'integration.task_start' -Status PASS -ReasonCode 'daemon_running' -Category integration
        } else {
            Add-Check -ID 'integration.task_start' -Status FAIL -ReasonCode 'daemon_readiness_timeout' -Category integration
        }
        $null = Invoke-Sidravia -Arguments @('daemon', 'stop')
    } else {
        Add-Check -ID 'integration.task_start' -Status SKIPPED -ReasonCode 'registration_failed' -Category integration
    }

    $removeFirst = Invoke-CapturedProcess -FilePath $script:ReleasePowerShell.Path -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $uninstall) -StdinValue $null
    $removeSecond = Invoke-CapturedProcess -FilePath $script:ReleasePowerShell.Path -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $uninstall) -StdinValue $null
    $removeFirstOK = Add-IntegrationProcessCheck -ID 'integration.uninstall_first' -Result $removeFirst
    $removeSecondOK = Add-IntegrationProcessCheck -ID 'integration.uninstall_second' -Result $removeSecond
    $finalTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
    $finalPath = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
    if (-not $finalPath) {
        Add-Check -ID 'integration.path_revoked' -Status PASS -ReasonCode 'path_absent' -Category integration
    } else {
        Add-Check -ID 'integration.path_revoked' -Status FAIL -ReasonCode 'path_still_present' -Category integration
    }
    if ($null -eq $finalTask) {
        Add-Check -ID 'integration.task_revoked' -Status PASS -ReasonCode 'task_absent' -Category integration
    } else {
        Add-Check -ID 'integration.task_revoked' -Status FAIL -ReasonCode 'task_still_present' -Category integration
    }
    if ($null -eq $finalTask -and -not $finalPath) { $script:IntegrationOwned = $false }
    $watch.Stop()

    $ok = $firstOK -and $secondOK -and $pathPresent -and $null -ne $task -and $taskStarted -and
        $removeFirstOK -and $removeSecondOK -and
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

function Invoke-HotspotContinuity {
    param([string]$SessionID)
    if ($SkipNetworkTransition) {
        Add-Check -ID 'campus.hotspot_continuity' -Status SKIPPED -ReasonCode 'requested_skip' -Category campus
        return $true
    }
    $approval = Read-Host '测试切换 Windows 移动热点时认证是否保持连续？请输入 YES'
    if ($approval -cne 'YES') {
        Add-Check -ID 'campus.hotspot_continuity' -Status SKIPPED -ReasonCode 'consent_declined' -Category campus
        return $true
    }
    $ready = Read-Host '输入 READY 开始观察热点切换期间的认证连续性'
    if ($ready -cne 'READY') {
        Add-Check -ID 'campus.hotspot_continuity' -Status SKIPPED -ReasonCode 'transition_declined' -Category campus
        return $true
    }
    Write-Host '请在 10 秒内切换一次 Windows 移动热点。保持校园网链路连接，并保持现有 TUN 校园网路由排除设置不变。'
    $deadline = [DateTime]::UtcNow.AddSeconds((Get-HeartbeatWaitSeconds))
    do {
        $status = Invoke-Sidravia -Arguments @('auth', 'status', $SessionID)
        if ($status.ExitCode -ne 0) {
            Add-Check -ID 'campus.hotspot_continuity' -Status FAIL -ReasonCode 'continuity_status_unavailable' -Category campus
            return $false
        }
        $state = Get-SessionState -Output $status.Output
        if (-not $state) {
            Add-Check -ID 'campus.hotspot_continuity' -Status FAIL -ReasonCode 'continuity_status_unavailable' -Category campus
            return $false
        }
        if ($state -ne 'authenticated') {
            Add-Check -ID 'campus.hotspot_continuity' -Status FAIL -ReasonCode 'unexpected_authentication_loss' -Category campus
            return $false
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    Add-Check -ID 'campus.hotspot_continuity' -Status PASS -ReasonCode 'authentication_continuity_observed' -Category campus
    return $true
}

function Invoke-NetworkRecovery {
    param([string]$SessionID)
    if ($SkipNetworkTransition) {
        Add-Check -ID 'campus.network_recovery' -Status SKIPPED -ReasonCode 'requested_skip' -Category campus
        return $true
    }
    $approval = Read-Host '测试校园网链路断开后的认证恢复？请输入 YES'
    if ($approval -cne 'YES') {
        Add-Check -ID 'campus.network_recovery' -Status SKIPPED -ReasonCode 'consent_declined' -Category campus
        return $true
    }
    $ready = Read-Host '输入 READY 后，请在 10 秒内断开校园网链路，并保持断开直至观察到网络丢失'
    if ($ready -cne 'READY') {
        Add-Check -ID 'campus.network_recovery' -Status SKIPPED -ReasonCode 'transition_declined' -Category campus
        return $true
    }
    Write-Host '正在观察 Session 状态。现在请断开校园网链路。'
    $lossDeadline = [DateTime]::UtcNow.AddSeconds(60)
    $observedLoss = $false
    do {
        $status = Invoke-Sidravia -Arguments @('auth', 'status', $SessionID)
        if ($status.ExitCode -eq 0) {
            $state = Get-SessionState -Output $status.Output
            if ($state -and $state -ne 'authenticated') { $observedLoss = $true; break }
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $lossDeadline)
    if (-not $observedLoss) {
        Add-Check -ID 'campus.network_recovery' -Status FAIL -ReasonCode 'network_loss_not_observed' -Category campus
        return $false
    }

    Write-Host '已观察到网络断开。现在请重新连接同一校园网链路。'
    $recoveryDeadline = [DateTime]::UtcNow.AddSeconds(120)
    do {
        $status = Invoke-Sidravia -Arguments @('auth', 'status', $SessionID)
        if ($status.ExitCode -eq 0 -and (Get-SessionState -Output $status.Output) -eq 'authenticated') {
            Add-Check -ID 'campus.network_recovery' -Status PASS -ReasonCode 'loss_and_recovery_observed' -Category campus
            return $true
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $recoveryDeadline)
    Add-Check -ID 'campus.network_recovery' -Status FAIL -ReasonCode 'authentication_recovery_not_observed' -Category campus
    return $false
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
    $autoLoginReady = $false

    $authenticated = Wait-SessionState -SessionID $sessionID -States @('authenticated') -TimeoutSeconds 60
    if ($null -eq $authenticated) {
        $status = Invoke-Sidravia -Arguments @('auth', 'status', $sessionID)
        $diagnostics = Get-AllowlistedSessionDiagnostics -Output $(if ($status.ExitCode -eq 0) { $status.Output } else { '' })
        $safeParts = New-Object System.Collections.Generic.List[string]
        if ($diagnostics.State) { $safeParts.Add('state=' + $diagnostics.State) }
        if ($diagnostics.StateReason) { $safeParts.Add('state_reason=' + $diagnostics.StateReason) }
        if ($diagnostics.Failure) { $safeParts.Add('failure=' + $diagnostics.Failure) }
        if ($diagnostics.Recommendation) { $safeParts.Add('recommendation=' + $diagnostics.Recommendation) }
        if ($safeParts.Count -gt 0) {
            Write-Host ('认证诊断（仅安全代码）：' + ($safeParts.ToArray() -join ', '))
        } else {
            Write-Host '认证诊断（仅安全代码）：不可用'
        }
        $reason = 'not_authenticated'
        if ($diagnostics.State) { $reason = $diagnostics.State }
        if ($diagnostics.StateReason) { $reason = $diagnostics.StateReason }
        if ($diagnostics.Failure) { $reason = $diagnostics.Failure }
        Add-Check -ID 'campus.authentication' -Status FAIL -ReasonCode $reason -Category campus
        return $false
    }
    Add-Check -ID 'campus.authentication' -Status PASS -ReasonCode 'authenticated' -Category campus
    $autoLoginReady = $true
    $bindingPresent = $authenticated.Output -match '(?m)^\u7f51\u7edc\uff1a[^\r\n]*(?:\d{1,3}\.){3}\d{1,3}[^\r\n]*$'
    if ($bindingPresent) {
        Add-Check -ID 'campus.selected_binding' -Status PASS -ReasonCode 'binding_present' -Category campus
    } else {
        Add-Check -ID 'campus.selected_binding' -Status FAIL -ReasonCode 'binding_missing' -Category campus
    }

    if (Test-FixedPortOwnership) {
        Add-Check -ID 'campus.fixed_port' -Status PASS -ReasonCode 'owned_61440' -Category campus
    } else {
        Add-Check -ID 'campus.fixed_port' -Status FAIL -ReasonCode 'fixed_port_owner_mismatch' -Category campus
    }

    $heartbeatSeconds = Get-HeartbeatWaitSeconds
    $ka1 = Wait-ProtocolPhase -Phase 'keepalive_ka1' -TimeoutSeconds $heartbeatSeconds
    $ka2 = Wait-ProtocolPhase -Phase 'keepalive_ka2' -TimeoutSeconds 15
    if ($ka1 -and $ka2) {
        Add-Check -ID 'campus.heartbeat' -Status PASS -ReasonCode 'keepalive_phases_completed' -Category campus
    } else {
        Add-Check -ID 'campus.heartbeat' -Status FAIL -ReasonCode 'keepalive_phase_missing' -Category campus
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
    }

    $null = Invoke-HotspotContinuity -SessionID $sessionID
    $null = Invoke-NetworkRecovery -SessionID $sessionID

    $stop = Invoke-Sidravia -Arguments @('auth', 'stop', $sessionID)
    $suspended = Wait-SessionState -SessionID $sessionID -States @('suspended') -TimeoutSeconds 30
    $logout = Wait-ProtocolPhase -Phase 'logout' -TimeoutSeconds 5
    if ($stop.ExitCode -eq 0 -and $null -ne $suspended -and $logout) {
        Add-Check -ID 'campus.logout_stop' -Status PASS -ReasonCode 'logout_and_suspended' -Category campus
    } else {
        Add-Check -ID 'campus.logout_stop' -Status FAIL -ReasonCode 'logout_or_suspend_missing' -Category campus
        $autoLoginReady = $false
    }
    if (Remove-TestSession -SessionID $sessionID) {
        Add-Check -ID 'campus.session_remove' -Status PASS -ReasonCode 'session_absent' -Category campus
    } else {
        Add-Check -ID 'campus.session_remove' -Status FAIL -ReasonCode 'session_remove_failed' -Category campus
        $autoLoginReady = $false
    }
    return $autoLoginReady
}

function Invoke-TemporaryConfigurationSuite {
    $approval = Read-Host '在受保护沙箱中临时保存本次输入的凭据以测试自动登录？请输入 YES'
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
    if (-not (Test-ParenthesizedMachineToken -Output $create.Output -Token 'protected')) {
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
    Write-Output '请停止其他 Dr.COM 客户端。停止 TUN，或为 RFC1918 校园网路由设置排除。'
    $ready = Read-Host '确认校园网链路已就绪，然后输入 YES'
    if ($ready -cne 'YES') {
        Add-Check -ID 'campus.full_chain' -Status BLOCKED -ReasonCode 'campus_precondition_declined' -Category campus
        return
    }
    $script:Username = Read-Host '校园网账号'
    if ([string]::IsNullOrWhiteSpace($script:Username)) {
        Add-Check -ID 'campus.full_chain' -Status BLOCKED -ReasonCode 'username_missing' -Category campus
        return
    }
    $script:SecurePassword = Read-Host -AsSecureString -Prompt '校园网密码'
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
    $autoLoginReady = Invoke-OneShotCampusSuite
    if ($autoLoginReady) {
        Invoke-TemporaryConfigurationSuite
    } else {
        Add-Check -ID 'campus.configuration_autologin' -Status SKIPPED -ReasonCode 'one_shot_prerequisite_failed' -Category campus
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
            $task = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
            $ownedTask = Get-SandboxTask
            $ownedPath = Test-PathEntry -PathValue ([Environment]::GetEnvironmentVariable('Path', 'User')) -Entry $script:Sandbox
            if ($null -ne $task -and $null -eq $ownedTask) {
                $ok = $false
                $sandboxRemovalSafe = $false
            } elseif ($null -ne $ownedTask -or $ownedPath) {
                if ($null -eq $script:ReleasePowerShell) {
                    $ok = $false
                    $sandboxRemovalSafe = $false
                } else {
                    $uninstall = Join-Path $script:Sandbox (Join-Path 'scripts' 'uninstall.ps1')
                    $result = Invoke-CapturedProcess -FilePath $script:ReleasePowerShell.Path -Arguments @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $uninstall) -StdinValue $null
                    if ($result.ExitCode -ne 0) {
                        $ok = $false
                        $sandboxRemovalSafe = $false
                    }
                }
            }

            $remainingTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
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
            releaseScriptPowerShell = $(if ($script:ReleasePowerShell) { $script:ReleasePowerShell.Version } else { $null })
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

    try {
        $script:ReleasePowerShell = Resolve-ReleasePowerShell
        Add-Check -ID 'preflight.release_host' -Status PASS -ReasonCode 'release_powershell_resolved' -Category automatic
    } catch {
        $reason = 'release_powershell_unavailable'
        if ($_.Exception.Message -eq 'release_powershell_not_found') { $reason = 'release_powershell_not_found' }
        if ($_.Exception.Message -eq 'unsupported_release_script_host') { $reason = 'unsupported_release_script_host' }
        Add-Check -ID 'preflight.release_host' -Status BLOCKED -ReasonCode $reason -Category automatic
        throw $_.Exception.Message
    }

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
        Write-Host ('现场验证器执行失败 [' + $errorDetails.ReasonCode + ':' + $errorDetails.ExceptionType + ']')
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
        Write-Host ('脱敏报告写入失败 [report_write_failed:' + $reportErrorType + ']')
    }
}

$script:StatusLabels = @{
    PASS = '通过'
    FAIL = '失败'
    BLOCKED = '受阻'
    SKIPPED = '已跳过'
}
$script:CheckLabels = @{
    'preflight.package' = '验证测试包'
    'preflight.release_host' = '解析正式发行脚本宿主'
    'preflight.stale_cleanup' = '检查并清理测试残留'
    'preflight.daemon' = '检查既有后台服务'
    'preflight.sandbox' = '创建一次性便携沙箱'
    'local.cli_surface' = '完整 CLI 命令面'
    'integration.install_first' = '首次运行安装脚本'
    'integration.install_second' = '再次运行安装脚本'
    'integration.path_registered' = '注册当前用户 PATH'
    'integration.task_registered' = '注册用户登录计划任务'
    'integration.task_start' = '通过计划任务启动后台服务'
    'integration.uninstall_first' = '首次运行卸载脚本'
    'integration.uninstall_second' = '再次运行卸载脚本'
    'integration.path_revoked' = '撤销当前用户 PATH'
    'integration.task_revoked' = '撤销用户登录计划任务'
    'integration.user_mode' = '用户态安装集成完整链路'
    'campus.full_chain' = '校园网完整链路'
    'campus.daemon' = '启动调试级后台服务'
    'campus.oneshot_start' = '创建一次性认证 Session'
    'campus.authentication' = '校园网认证'
    'campus.selected_binding' = '选择校园网绑定'
    'campus.fixed_port' = '固定本地端口所有权'
    'campus.heartbeat' = '认证心跳'
    'campus.retained_lifecycle' = '保留 Session 生命周期操作'
    'campus.hotspot_continuity' = '热点切换期间认证连续性'
    'campus.network_recovery' = '校园网断线与认证恢复'
    'campus.logout_stop' = '注销并暂停 Session'
    'campus.session_remove' = '移除 Session'
    'campus.configuration_autologin' = '受保护配置自动登录'
    'cleanup.owned_state' = '清理测试器拥有的状态'
    'harness.execution' = '现场验证器执行'
    'report.sanitized' = '写入脱敏报告'
}
$script:ReasonLabels = @{
    'package_verified' = '测试包内容与校验和已验证'
    'release_powershell_resolved' = '已解析并验证系统 Windows PowerShell 5.1'
    'release_powershell_not_found' = '未找到系统 Windows PowerShell 5.1'
    'release_powershell_unavailable' = '无法解析正式发行脚本宿主'
    'no_owned_stale_state' = '不存在测试器拥有的残留状态'
    'stale_cleanup_required' = '残留状态无法安全清理，需要人工检查'
    'no_preexisting_daemon' = '运行前不存在 Sidravia 后台服务'
    'preexisting_daemon' = '检测到运行前已存在的 Sidravia 后台服务'
    'owned_portable_sandbox' = '已创建并标记测试器拥有的便携沙箱'
    'all_checks_passed' = '全部命令面检查均已通过'
    'cli_smoke_failed' = 'CLI 冒烟测试存在失败项'
    'command_succeeded' = '命令执行成功'
    'command_failed' = '命令执行失败'
    'scheduled_task_registration_failed' = '计划任务注册失败'
    'requested_skip' = '已按启动参数要求跳过'
    'consent_declined' = '测试者未同意执行该项'
    'preexisting_integration_state' = '检测到运行前已存在的同名集成状态'
    'path_present' = '当前用户 PATH 已包含安装目录'
    'path_missing' = '当前用户 PATH 缺少安装目录'
    'owned_task_present' = '存在测试器拥有且定义匹配的计划任务'
    'owned_task_missing_or_mismatched' = '计划任务不存在或定义不匹配'
    'task_start_command_failed' = '启动计划任务的命令失败'
    'daemon_running' = '后台服务已进入运行状态'
    'daemon_readiness_timeout' = '等待后台服务就绪超时'
    'registration_failed' = '注册未成功，无法测试启动'
    'path_absent' = '当前用户 PATH 已不包含安装目录'
    'path_still_present' = '当前用户 PATH 仍包含安装目录'
    'task_absent' = '计划任务已不存在'
    'task_still_present' = '计划任务仍然存在'
    'idempotent_install_action_uninstall' = '安装、重复安装、启动、卸载和重复卸载均符合契约'
    'integration_contract_failed' = '用户态集成链路存在失败项'
    'debug_daemon_running' = '调试级后台服务已运行'
    'daemon_start_failed' = '后台服务启动失败'
    'session_created' = '已创建认证 Session'
    'session_start_failed' = '创建认证 Session 失败'
    'authenticated' = '认证成功'
    'binding_present' = '已显示所选网络绑定'
    'binding_missing' = '未显示所选网络绑定'
    'owned_61440' = '固定端口 61440 由测试后台服务拥有'
    'fixed_port_owner_mismatch' = '固定端口 61440 的所有者不匹配'
    'keepalive_phases_completed' = 'KA1 和 KA2 心跳阶段均已完成'
    'keepalive_phase_missing' = '缺少预期的心跳阶段'
    'list_status_restart' = '列出、查看状态和重启操作均成功'
    'retained_operation_failed' = '保留 Session 生命周期操作失败'
    'transition_declined' = '测试者未确认开始网络操作'
    'authentication_continuity_observed' = '观察窗口内认证始终保持成功'
    'continuity_status_unavailable' = '观察期间无法读取可靠的 Session 状态'
    'unexpected_authentication_loss' = '热点切换期间认证意外丢失'
    'loss_and_recovery_observed' = '已观察到断线并在重连后恢复认证'
    'network_loss_not_observed' = '未在等待时间内观察到网络丢失'
    'authentication_recovery_not_observed' = '重连后未在等待时间内恢复认证'
    'logout_and_suspended' = '已完成注销且 Session 进入暂停状态'
    'logout_or_suspend_missing' = '注销阶段或暂停状态不完整'
    'session_absent' = 'Session 已移除'
    'session_remove_failed' = 'Session 移除失败'
    'protected_autologin_authenticated' = '受保护配置已自动登录并认证成功'
    'protected_result_missing' = '配置创建成功但未识别到受保护结果'
    'protected_storage_unavailable' = '当前环境无法提供受保护存储'
    'temporary_configuration_failed' = '临时配置创建失败'
    'autologin_not_authenticated' = '自动登录未在等待时间内完成认证'
    'one_shot_prerequisite_failed' = '一次性认证、注销或移除前置条件失败'
    'campus_precondition_declined' = '测试者未确认校园网前置条件'
    'username_missing' = '未输入校园网账号'
    'password_missing' = '未输入校园网密码'
    'local_suite_failed' = '本地命令面测试失败，后续链路未运行'
    'all_owned_state_removed' = '测试器拥有的状态已全部移除'
    'cleanup_incomplete' = '未能确认测试器拥有的状态已全部移除'
    'report_write_failed' = '脱敏报告写入失败'
    'sanitized_internal_error' = '发生未公开内部细节的验证器错误'
    'report_directory_outside_package' = '报告目录超出测试包边界'
    'unsupported_windows_or_powershell' = '需要受支持的 Windows 和 PowerShell 7'
    'powershell_host_not_found' = '未找到当前 PowerShell 7 子进程宿主'
    'package_input_missing' = '测试包缺少必需文件'
    'package_checksum_missing' = '测试包缺少二进制校验和'
    'package_checksum_mismatch' = '测试包二进制校验和不匹配'
    'temporary_parent_is_reparse_point' = '临时目录父路径是重解析点，已拒绝使用'
    'sandbox_path_escape' = '沙箱路径超出拥有边界'
}

function Get-LocalizedLabel {
    param([hashtable]$Labels, [string]$Key, [string]$Fallback)
    if ($Labels.ContainsKey($Key)) { return $Labels[$Key] }
    return $Fallback
}

Write-Output ''
Write-Output '== Sidravia 现场验证结果 =='
foreach ($check in $script:Checks) {
    $statusLabel = Get-LocalizedLabel -Labels $script:StatusLabels -Key $check.status -Fallback $check.status
    $checkLabel = Get-LocalizedLabel -Labels $script:CheckLabels -Key $check.id -Fallback '未提供中文检查名称'
    $reasonLabel = Get-LocalizedLabel -Labels $script:ReasonLabels -Key $check.reasonCode -Fallback '未提供中文原因说明'
    Write-Output ('{0,-6} {1} [{2}]（{3}：{4}）' -f $statusLabel, $checkLabel, $check.id, $check.reasonCode, $reasonLabel)
}
if ($script:ReportPath) { Write-Output ('脱敏报告：' + $script:ReportPath) }

if ($script:CleanupFailed) { exit 3 }
if ($script:RequestedFailure) { exit 1 }
if ($script:PreconditionBlocked) { exit 2 }
exit 0
