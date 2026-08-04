# Sidravia Windows 用户态集成安装脚本。
#
# 本脚本具备幂等性，可以安全地重复运行。它不区分安装模式，既适用于正式安装目录，
# 也适用于便携目录；注册目标始终是本脚本所在目录的上级目录。
# 脚本会把该目录加入当前用户 PATH，并创建名为 "SidraviaDaemon" 的用户登录计划任务，
# 该任务运行 "sidravia daemon start --log-level <level>"。
# 脚本会在操作前后验证注册状态，且绝不删除任何配置、凭据、Profile 或日志。
#
# 用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1 -LogLevel debug
[CmdletBinding()]
param(
    [ValidateSet('info', 'debug', 'trace')]
    [string]$LogLevel = 'info'
)
$ErrorActionPreference = 'Stop'

# 前置门：正式发行脚本只支持并验证 Windows PowerShell 5.1 Desktop。
# 其他宿主在任何 PATH、任务或文件状态变化前以稳定机器码失败。
if ($env:OS -ne 'Windows_NT' -or
    $PSVersionTable.PSEdition -ne 'Desktop' -or
    $PSVersionTable.PSVersion.Major -ne 5 -or
    $PSVersionTable.PSVersion.Minor -ne 1) {
    throw 'unsupported_release_script_host'
}

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

function Get-SidraviaTaskOwnership {
    param([object]$Task, [string]$InstallDirectory, [string]$UserSID)
    if ($null -eq $Task) { return 'absent' }
    if ($Task.TaskPath -ne '\' -or $Task.TaskName -ne $TaskName -or
        $Task.Actions.Count -ne 1 -or $Task.Triggers.Count -ne 1 -or
        [string]$Task.Triggers[0].CimClass.CimClassName -ne 'MSFT_TaskLogonTrigger') { return 'conflict' }
    $expectedExecutable = [IO.Path]::GetFullPath((Join-Path $InstallDirectory 'sidravia.exe'))
    try { $actualExecutable = [IO.Path]::GetFullPath([string]$Task.Actions[0].Execute) } catch { return 'conflict' }
    $principalSID = ConvertTo-SIDValue -Identity ([string]$Task.Principal.UserId)
    $triggerSID = ConvertTo-SIDValue -Identity ([string]$Task.Triggers[0].UserId)
    if (-not $actualExecutable.Equals($expectedExecutable, [StringComparison]::OrdinalIgnoreCase) -or
        [string]$Task.Actions[0].Arguments -notin @('daemon start --log-level info', 'daemon start --log-level debug', 'daemon start --log-level trace') -or
        $principalSID -ne $UserSID -or $triggerSID -ne $UserSID -or
        [string]$Task.Principal.LogonType -ne 'Interactive' -or [string]$Task.Principal.RunLevel -ne 'Limited') { return 'conflict' }
    return 'owned'
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
    return New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal -Description 'Sidravia 后台服务'
}

# 前置条件：产品二进制必须位于本脚本上级目录中。
if (-not (Test-Path -LiteralPath (Join-Path $InstallDir 'sidravia.exe'))) {
    throw "未找到产品程序：$InstallDir\sidravia.exe"
}
if (-not (Test-Path -LiteralPath (Join-Path $InstallDir 'sidraviad.exe'))) {
    throw "未找到后台服务程序：$InstallDir\sidraviad.exe"
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
    throw 'current_user_sid_unavailable：无法获取当前用户 SID'
}

# 操作前：记录当前状态。
$BeforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
$PathPresent = Test-PathEntry -PathValue $BeforePath -Entry $InstallDir
$BeforeTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
$TaskOwnership = Get-SidraviaTaskOwnership -Task $BeforeTask -InstallDirectory $InstallDir -UserSID $CurrentUserSID
if ($TaskOwnership -eq 'conflict') { throw 'scheduled_task_conflict' }
$TaskPresent = ($TaskOwnership -eq 'owned')

# 1）当前用户 PATH：精确匹配、不区分大小写、幂等。
if (-not $PathPresent) {
    $NewPath = Add-PathEntry -PathValue $BeforePath -Entry $InstallDir
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}

# 2）用户登录计划任务：幂等替换。
$Definition = New-SidraviaLogonTaskDefinition -InstallDirectory $InstallDir -LogLevel $LogLevel -UserSID $CurrentUserSID
try {
    Register-ScheduledTask -TaskName $TaskName -TaskPath '\' -InputObject $Definition -Force | Out-Null
} catch {
    throw [InvalidOperationException]::new('scheduled_task_registration_failed：注册 SidraviaDaemon 计划任务失败', $_.Exception)
}

# 操作后：验证注册状态。
$AfterPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (Test-PathEntry -PathValue $AfterPath -Entry $InstallDir)) {
    throw '验证失败：安装目录未加入当前用户 PATH'
}
$AfterTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
if ($null -eq $AfterTask) {
    throw "验证失败：计划任务 $TaskName 不存在"
}
if ($AfterTask.Actions.Count -ne 1) {
    throw '验证失败：计划任务动作不匹配'
}
$AfterOwnership = Get-SidraviaTaskOwnership -Task $AfterTask -InstallDirectory $InstallDir -UserSID $CurrentUserSID
if ($AfterOwnership -ne 'owned' -or $AfterTask.Actions[0].Arguments -ne "daemon start --log-level $LogLevel") {
    throw '验证失败：计划任务动作不匹配'
}
$AfterPrincipalSID = ConvertTo-SIDValue -Identity ([string]$AfterTask.Principal.UserId)
$AfterTriggerSID = $null
if ($AfterTask.Triggers.Count -eq 1) {
    $AfterTriggerSID = ConvertTo-SIDValue -Identity ([string]$AfterTask.Triggers[0].UserId)
}
if ($AfterPrincipalSID -ne $CurrentUserSID -or $AfterTriggerSID -ne $CurrentUserSID -or
    [string]$AfterTask.Principal.LogonType -ne 'Interactive' -or
    [string]$AfterTask.Principal.RunLevel -ne 'Limited') {
    throw '验证失败：计划任务用户身份不匹配'
}

# 结果摘要（操作前 -> 操作后）。
if ($PathPresent) { Write-Output "当前用户 PATH 已包含安装目录：$InstallDir" }
else { Write-Output "安装目录已加入当前用户 PATH：$InstallDir" }
if ($TaskPresent) { Write-Output "计划任务已更新：$TaskName（--log-level $LogLevel）" }
else { Write-Output "计划任务已创建：$TaskName（--log-level $LogLevel）" }
