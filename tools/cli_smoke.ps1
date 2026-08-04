# Sidravia 命令行完整冒烟测试（Windows）。
#
# 本脚本针对本地真实后台服务运行公开 CLI 命令，并报告通过/失败结果。
# 真实认证由 field-test.ps1 在获授权的 Windows 现场环境中执行。本脚本仅支持
# PowerShell 7，并使用 UTF-8 编码。
#
# 用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\cli_smoke.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\cli_smoke.ps1 -Sidravia C:\path\sidravia.exe
[CmdletBinding()]
param(
    [string]$Sidravia = '.\sidravia.exe'
)
$ErrorActionPreference = 'Stop'
$script:NativeUTF8Encoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = $script:NativeUTF8Encoding
[Console]::OutputEncoding = $script:NativeUTF8Encoding

if ($PSVersionTable.PSEdition -ne 'Core' -or $PSVersionTable.PSVersion.Major -lt 7) {
    throw '需要 PowerShell 7 或更高版本。'
}

if (-not (Test-Path -LiteralPath $Sidravia)) {
    throw "未找到 sidravia.exe：$Sidravia"
}
$Exe = (Resolve-Path -LiteralPath $Sidravia).Path

$script:checks = New-Object System.Collections.Generic.List[object]
$script:failed = New-Object System.Collections.Generic.List[string]

function Add-Check {
    param([string]$Name, [bool]$OK, [string]$Note = '')
    $script:checks.Add([pscustomobject]@{ Name = $Name; OK = $OK; Skipped = $false; Note = $Note })
    if (-not $OK) { $script:failed.Add("$Name : $Note") }
}

function Add-SkippedCheck {
    param([string]$Name, [string]$Note)
    $script:checks.Add([pscustomobject]@{ Name = $Name; OK = $false; Skipped = $true; Note = $Note })
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
    $note = "退出码=$code"
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
        $note += "; 输出=$flat"
    }
    Add-Check -Name $Name -OK $ok -Note $note
    return $out
}

Write-Output "== Sidravia 命令行完整冒烟测试：$Exe =="

# 为保证重复运行安全，先停止可能存在的后台服务。
Invoke-Sidravia -CommandArguments @('daemon', 'stop') -Expect any -Name '后台服务：初始停止与清理'

# 1. 帮助界面（所有入口行为一致，且不会派发业务操作）。
Invoke-Sidravia -CommandArguments @('--help') -Expect success -Name '帮助：--help'
Invoke-Sidravia -CommandArguments @('help', 'daemon') -Expect success -Name '帮助：daemon'
Invoke-Sidravia -CommandArguments @('help', 'auth', 'start') -Expect success -Name '帮助：auth start'
Invoke-Sidravia -CommandArguments @('auth', 'start', '--help') -Expect success -Name '帮助：auth start --help'
Invoke-Sidravia -CommandArguments @('profile', 'list', '--help') -Expect success -Name '帮助：profile list --help'

# 2. 命令缺失（已退役或移动的命令必须失败）。
Invoke-Sidravia -CommandArguments @('status') -Expect failure -Name '拒绝已退役的 status 命令'
Invoke-Sidravia -CommandArguments @('install') -Expect failure -Name 'install 命令不存在'
Invoke-Sidravia -CommandArguments @('uninstall') -Expect failure -Name 'uninstall 命令不存在'

# 3. 后台服务生命周期。
Invoke-Sidravia -CommandArguments @('daemon', 'status') -Expect stopped -Name '后台服务：初始状态为已停止'
Invoke-Sidravia -CommandArguments @('daemon', 'start', '--log-level', 'info') -Expect success -Name '后台服务：启动'
Invoke-Sidravia -CommandArguments @('daemon', 'status') -Expect running -Name '后台服务：状态为运行中'
Invoke-Sidravia -CommandArguments @('daemon', 'restart') -Expect success -Name '后台服务：重启'

# 4. 在程序根目录发现 Profile。
Invoke-Sidravia -CommandArguments @('profile', 'list') -Expect success -Match 'jlu' -Name 'Profile 列表包含 jlu'

# 5. 使用一次性 ID 测试配置增删改查（始终移除）。
$id = 'smoke-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
Invoke-Sidravia -CommandArguments @('config', 'create', '--id', $id, '--profile', 'jlu', '--username', 'smoke-user', '--password-stdin') -Stdin 'smoke-pass' -Expect success -Match $id -Name '配置：创建'
Invoke-Sidravia -CommandArguments @('config', 'list') -Expect success -Match $id -Name '配置：列表包含一次性 ID'
Invoke-Sidravia -CommandArguments @('config', 'show', $id) -Expect success -Match $id -Name '配置：显示详情'
Invoke-Sidravia -CommandArguments @('config', 'create', '--id', $id, '--profile', 'jlu', '--username', 'smoke-user', '--password-stdin') -Stdin 'smoke-pass' -Expect failure -Name '配置：拒绝重复创建'
Invoke-Sidravia -CommandArguments @('config', 'show', 'smoke-missing') -Expect failure -Name '配置：拒绝显示不存在的配置'
Invoke-Sidravia -CommandArguments @('config', 'update', $id, '--name', 'SmokeUpdated') -Expect success -Name '配置：更新'
Invoke-Sidravia -CommandArguments @('config', 'set-password', $id, '--password-stdin') -Stdin 'smoke-pass-2' -Expect success -Name '配置：更新密码'
Invoke-Sidravia -CommandArguments @('config', 'remove', $id, '--yes') -Expect success -Name '配置：移除'

# 6. 认证 Session 操作面。
Invoke-Sidravia -CommandArguments @('auth', 'list') -Expect success -Name '认证：列出 Session'
Add-SkippedCheck -Name '认证：启动 Session' -Note '本地冒烟不执行真实认证；请使用 field-test.ps1。'

# 7. 清理。
Invoke-Sidravia -CommandArguments @('daemon', 'stop') -Expect stopped -Name '后台服务：停止并清理'

# 汇总。
Write-Output ''
Write-Output '== 汇总（通过/失败） =='
$passCount = 0
$failureCount = 0
$skippedCount = 0
foreach ($c in $script:checks) {
    if ($c.Skipped) {
        $skippedCount++
        $mark = '跳过'
    } elseif ($c.OK) {
        $passCount++
        $mark = '通过'
    } else {
        $failureCount++
        $mark = '失败'
    }
    $note = if ($c.Note) { " [$($c.Note)]" } else { '' }
    Write-Output ("{0,-3} {1}{2}" -f $mark, $c.Name, $note)
}
$total = $passCount + $failureCount
Write-Output ''
Write-Output "总计：$total，通过：$passCount，失败：$failureCount，跳过：$skippedCount"
if ($script:failed.Count -gt 0) {
    Write-Output '未预期的失败：'
    foreach ($f in $script:failed) { Write-Output "  - $f" }
    exit 1
}
exit 0
