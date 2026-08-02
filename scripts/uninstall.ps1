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

# 操作前：记录当前状态。
$BeforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
$PathPresent = Test-PathEntry -PathValue $BeforePath -Entry $InstallDir
$BeforeTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
$TaskPresent = ($null -ne $BeforeTask)

# 1）移除精确匹配的当前用户 PATH 条目。
if ($PathPresent) {
    $NewPath = Remove-PathEntry -PathValue $BeforePath -Entry $InstallDir
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}

# 2）移除用户登录计划任务。
if ($TaskPresent) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

# 操作后：验证两项状态均已撤销。
$AfterPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (Test-PathEntry -PathValue $AfterPath -Entry $InstallDir) {
    throw '验证失败：安装目录仍在当前用户 PATH 中'
}
$AfterTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -ne $AfterTask) {
    throw "验证失败：计划任务 $TaskName 仍然存在"
}

# 结果摘要（操作前 -> 操作后）。
if ($PathPresent) { Write-Output "安装目录已从当前用户 PATH 移除：$InstallDir" }
else { Write-Output '当前用户 PATH 中原本没有该安装目录（幂等）' }
if ($TaskPresent) { Write-Output "计划任务已移除：$TaskName" }
else { Write-Output "计划任务原本不存在（幂等）：$TaskName" }
