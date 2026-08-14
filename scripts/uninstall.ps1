# Sidravia Windows 用户态集成卸载脚本。
#
# 本脚本具备幂等性，可以安全地重复运行。它只撤销 install.ps1 注册的当前用户 PATH
# 条目和用户登录计划任务。脚本会在操作前后验证撤销状态，且绝不删除任何配置、
# 凭据、Profile 或日志。
#
# 用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
[CmdletBinding()]
param()
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

function ConvertTo-SIDValue {
    param([string]$Identity)
    if ([string]::IsNullOrWhiteSpace($Identity)) { return $null }
    try { return ([Security.Principal.SecurityIdentifier]::new($Identity)).Value } catch {}
    try { return ([Security.Principal.NTAccount]::new($Identity).Translate([Security.Principal.SecurityIdentifier])).Value } catch { return $null }
}

function Get-SidraviaTaskOwnership {
    param([object]$Task, [string]$InstallDirectory, [string]$UserSID)
    if ($null -eq $Task) { return 'absent' }
    if ($Task.TaskPath -ne '\' -or $Task.TaskName -ne $TaskName -or $Task.Actions.Count -ne 1 -or $Task.Triggers.Count -ne 1 -or [string]$Task.Triggers[0].CimClass.CimClassName -ne 'MSFT_TaskLogonTrigger') { return 'conflict' }
    $expectedExecutable = [IO.Path]::GetFullPath((Join-Path $InstallDirectory 'sidraviactl.exe'))
    try { $actualExecutable = [IO.Path]::GetFullPath([string]$Task.Actions[0].Execute) } catch { return 'conflict' }
    $principalSID = ConvertTo-SIDValue -Identity ([string]$Task.Principal.UserId)
    $triggerSID = ConvertTo-SIDValue -Identity ([string]$Task.Triggers[0].UserId)
    if (-not $actualExecutable.Equals($expectedExecutable, [StringComparison]::OrdinalIgnoreCase) -or
        [string]$Task.Actions[0].Arguments -notin @('daemon start --log-level info', 'daemon start --log-level debug', 'daemon start --log-level trace') -or
        $principalSID -ne $UserSID -or $triggerSID -ne $UserSID -or
        [string]$Task.Principal.LogonType -ne 'Interactive' -or [string]$Task.Principal.RunLevel -ne 'Limited') { return 'conflict' }
    return 'owned'
}

$CurrentUserSID = $null
try { $CurrentUserSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value } catch {}
if (-not $CurrentUserSID) { throw 'current_user_sid_unavailable' }

# 操作前：记录当前状态。
$BeforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
$PathPresent = Test-PathEntry -PathValue $BeforePath -Entry $InstallDir
$BeforeTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
$TaskOwnership = Get-SidraviaTaskOwnership -Task $BeforeTask -InstallDirectory $InstallDir -UserSID $CurrentUserSID
if ($TaskOwnership -eq 'conflict') { throw 'scheduled_task_conflict' }
$TaskPresent = ($TaskOwnership -eq 'owned')

# 1）移除精确匹配的当前用户 PATH 条目。
if ($PathPresent) {
    $NewPath = Remove-PathEntry -PathValue $BeforePath -Entry $InstallDir
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}

# 2）移除用户登录计划任务。
if ($TaskPresent) {
    Unregister-ScheduledTask -TaskName $TaskName -TaskPath '\' -Confirm:$false
}

# 操作后：验证两项状态均已撤销。
$AfterPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (Test-PathEntry -PathValue $AfterPath -Entry $InstallDir) {
    throw '验证失败：安装目录仍在当前用户 PATH 中'
}
$AfterTask = Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction SilentlyContinue
if ($null -ne $AfterTask) {
    throw "验证失败：计划任务 $TaskName 仍然存在"
}

# 结果摘要（操作前 -> 操作后）。
if ($PathPresent) { Write-Output "安装目录已从当前用户 PATH 移除：$InstallDir" }
else { Write-Output '当前用户 PATH 中原本没有该安装目录（幂等）' }
if ($TaskPresent) { Write-Output "计划任务已移除：$TaskName" }
else { Write-Output "计划任务原本不存在（幂等）：$TaskName" }
