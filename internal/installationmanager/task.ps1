$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
$TaskName = 'SidraviaDaemon'

function Resolve-SID([string]$Identity) {
    if ([string]::IsNullOrWhiteSpace($Identity)) { return $null }
    try { return ([Security.Principal.SecurityIdentifier]::new($Identity)).Value } catch {}
    try { return ([Security.Principal.NTAccount]::new($Identity)).Translate([Security.Principal.SecurityIdentifier]).Value } catch { return $null }
}

function Classify-Task($Task, [string]$Directory, [string]$SID) {
    if ($null -eq $Task) { return 'disabled' }
    if ($Task.TaskPath -ne '\' -or $Task.TaskName -ne $TaskName -or
        $Task.Actions.Count -ne 1 -or $Task.Triggers.Count -ne 1 -or
        $Task.Triggers[0].CimClass.CimClassName -ne 'MSFT_TaskLogonTrigger' -or
        (Resolve-SID $Task.Principal.UserId) -ne $SID -or
        (Resolve-SID $Task.Triggers[0].UserId) -ne $SID -or
        [string]$Task.Principal.LogonType -ne 'Interactive' -or
        [string]$Task.Principal.RunLevel -ne 'Limited') { return 'conflict' }
    try {
        if (-not [IO.Path]::IsPathRooted([string]$Task.Actions[0].Execute)) { return 'conflict' }
        $actual = [IO.Path]::GetFullPath([string]$Task.Actions[0].Execute)
        $ctl = [IO.Path]::GetFullPath((Join-Path $Directory 'sidraviactl.exe'))
        $gui = [IO.Path]::GetFullPath((Join-Path $Directory 'Sidravia.exe'))
    } catch { return 'conflict' }
    $work = [string]$Task.Actions[0].WorkingDirectory
    if ($work -and -not $work.Equals($Directory, [StringComparison]::OrdinalIgnoreCase)) { return 'conflict' }
    if ($actual.Equals($ctl, [StringComparison]::OrdinalIgnoreCase) -and
        [string]$Task.Actions[0].Arguments -in @('daemon start --log-level info', 'daemon start --log-level debug', 'daemon start --log-level trace')) { return 'daemon' }
    if ($actual.Equals($gui, [StringComparison]::OrdinalIgnoreCase) -and
        [string]$Task.Actions[0].Arguments -eq '--start-hidden') { return 'gui' }
    return 'conflict'
}

function Read-Task {
    try {
        $items = @(Get-ScheduledTask -TaskName $TaskName -TaskPath '\' -ErrorAction Stop)
        if ($items.Count -eq 0) { return $null }
        if ($items.Count -ne 1) { throw 'scheduled_task_query_failed' }
        return $items[0]
    } catch {
        if ($_.FullyQualifiedErrorId -eq 'CmdletizationQuery_NotFound,Get-ScheduledTask' -and
            $_.CategoryInfo.Category -eq [Management.Automation.ErrorCategory]::ObjectNotFound) { return $null }
        throw 'scheduled_task_query_failed'
    }
}

function Task-Enabled($Task) {
    return $null -ne $Task -and $Task.Settings.Enabled -and $Task.Triggers[0].Enabled
}

function Startup-SettingsMatch($Task) {
    return (Task-Enabled $Task) -and
        -not $Task.Settings.DisallowStartIfOnBatteries -and
        -not $Task.Settings.StopIfGoingOnBatteries -and
        [string]$Task.Settings.ExecutionTimeLimit -eq 'PT0S'
}

# command handler
$response = @{ ok = $false; code = 'scheduled_task_operation_failed'; mode = ''; changed = $false }
try {
    if ($PSVersionTable.PSEdition -ne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) { throw 'unsupported_release_script_host' }
    $request = [Console]::In.ReadToEnd() | ConvertFrom-Json
    $dir = [string]$request.directory
    $op = [string]$request.operation
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $task = Read-Task
    $mode = Classify-Task $task $dir $sid
    $response.mode = $mode
    if ($op -eq 'status') {
        if ($mode -ne 'conflict' -and -not (Task-Enabled $task)) { $response.mode = 'disabled' }
    } else {
        if ($mode -eq 'conflict') { throw 'scheduled_task_conflict' }
        if ($op -eq 'autostart disable') {
            if ($null -ne $task) {
                Unregister-ScheduledTask -TaskName $TaskName -TaskPath '\' -Confirm:$false -ErrorAction Stop
                $response.changed = $true
            }
            if ($null -ne (Read-Task)) { throw 'scheduled_task_verify_failed' }
            $response.mode = 'disabled'
        } else {
            if ($op -eq 'autostart set gui') { $desired = 'gui'; $exe = Join-Path $dir 'Sidravia.exe'; $arguments = '--start-hidden' }
            elseif ($op -eq 'autostart set daemon') { $desired = 'daemon'; $exe = Join-Path $dir 'sidraviactl.exe'; $arguments = 'daemon start --log-level debug' }
            else { throw 'invalid_arguments' }
            if (-not [IO.File]::Exists($exe)) { throw 'autostart_target_missing' }
            if ($mode -ne $desired -or -not (Startup-SettingsMatch $task) -or [string]$task.Actions[0].Arguments -ne $arguments) {
                $action = New-ScheduledTaskAction -Execute $exe -Argument $arguments -WorkingDirectory $dir
                $trigger = New-ScheduledTaskTrigger -AtLogOn -User $sid
                $principal = New-ScheduledTaskPrincipal -UserId $sid -LogonType Interactive -RunLevel Limited
                $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero)
                $definition = New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Sidravia user startup'
                Register-ScheduledTask -TaskName $TaskName -TaskPath '\' -InputObject $definition -Force -ErrorAction Stop | Out-Null
                $response.changed = $true
            }
            $after = Read-Task
            if ((Classify-Task $after $dir $sid) -ne $desired -or -not (Startup-SettingsMatch $after) -or [string]$after.Actions[0].Arguments -ne $arguments) { throw 'scheduled_task_verify_failed' }
            $response.mode = $desired
        }
    }
    $response.ok = $true; $response.code = 'ok'
} catch {
    $safe = @('scheduled_task_query_failed','scheduled_task_conflict','scheduled_task_verify_failed','unsupported_release_script_host','autostart_target_missing','invalid_arguments')
    if ($_.Exception.Message -in $safe) { $response.code = $_.Exception.Message }
}
$response | ConvertTo-Json -Compress
