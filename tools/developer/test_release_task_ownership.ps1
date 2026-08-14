# Windows PowerShell 5.1 developer verifier for release scheduled-task ownership.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'

if ($env:OS -ne 'Windows_NT' -or $PSVersionTable.PSEdition -ne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) { throw 'unsupported_release_script_host' }
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$taskName = 'SidraviaDaemon'
$taskPath = '\'
$currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$originalPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('sidravia-release-task-' + [guid]::NewGuid().ToString('N'))
$fixtureTaskCreated = $false
$fixtureTaskXML = $null
$nestedTaskCreated = $false
$nested = '\SidraviaOwnershipFixture\'

function Assert-True { param([bool]$Value, [string]$Message) if (-not $Value) { throw $Message } }
function Get-RootTask { Get-ScheduledTask -TaskName $taskName -TaskPath $taskPath -ErrorAction SilentlyContinue }
function Capture-FixtureRootTask {
    $xml = Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath
    Assert-True ($null -ne $xml) 'fixture root task was not created'
    $script:fixtureTaskXML = $xml
    $script:fixtureTaskCreated = $true
}
function Remove-FixtureRootTask {
    if ($fixtureTaskCreated -and $null -ne (Get-RootTask)) {
        $currentXML = Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath
        if ($null -ne $fixtureTaskXML -and $currentXML -ceq $fixtureTaskXML) {
            Unregister-ScheduledTask -TaskName $taskName -TaskPath $taskPath -Confirm:$false
        }
    }
    $script:fixtureTaskCreated = $false
    $script:fixtureTaskXML = $null
}
function Invoke-Release { param([string]$Directory, [string]$Script, [string[]]$Arguments = @())
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $result = & (Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe') -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $Directory ('scripts\' + $Script)) @Arguments 2>&1
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    return [pscustomobject]@{ ExitCode = $exitCode; Output = ($result | Out-String) }
}
function New-Package { param([string]$Name)
    $directory = Join-Path $fixtureRoot $Name
    New-Item -ItemType Directory -Path (Join-Path $directory 'scripts') -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $repo 'scripts\install.ps1') -Destination (Join-Path $directory 'scripts\install.ps1')
    Copy-Item -LiteralPath (Join-Path $repo 'scripts\uninstall.ps1') -Destination (Join-Path $directory 'scripts\uninstall.ps1')
    New-Item -ItemType File -Path (Join-Path $directory 'sidraviactl.exe') -Force | Out-Null
    New-Item -ItemType File -Path (Join-Path $directory 'sidraviad.exe') -Force | Out-Null
    return $directory
}
function New-TaskDefinition { param([string]$Executable, [string]$Arguments, [int]$ActionCount = 1, [int]$TriggerCount = 1, [ValidateSet('Logon', 'Once')][string]$TriggerType = 'Logon')
    $actions = @()
    for ($index = 0; $index -lt $ActionCount; $index++) { $actions += New-ScheduledTaskAction -Execute $Executable -Argument $Arguments }
    $triggers = @()
    for ($index = 0; $index -lt $TriggerCount; $index++) {
        if ($TriggerType -eq 'Logon') { $triggers += New-ScheduledTaskTrigger -AtLogOn -User $currentSID }
        else { $triggers += New-ScheduledTaskTrigger -Once -At ([DateTime]::Now.AddMinutes(10 + $index)) }
    }
    $principal = New-ScheduledTaskPrincipal -UserId $currentSID -LogonType Interactive -RunLevel Limited
    return New-ScheduledTask -Action $actions -Trigger $triggers -Principal $principal
}
function Set-FixtureRootTask { param([object]$Definition)
    Register-ScheduledTask -TaskName $taskName -TaskPath $taskPath -InputObject $Definition -Force | Out-Null
    Capture-FixtureRootTask
}
function Assert-ConflictNoMutation { param([string]$Directory, [string]$Case)
    $beforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $beforeTask = Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath
    foreach ($scriptName in @('install.ps1', 'uninstall.ps1')) {
        $result = Invoke-Release -Directory $Directory -Script $scriptName
        Assert-True ($result.ExitCode -ne 0 -and $result.Output.Contains('scheduled_task_conflict')) "$Case $scriptName must report scheduled_task_conflict"
        Assert-True ([Environment]::GetEnvironmentVariable('Path', 'User') -ceq $beforePath) "$Case $scriptName changed PATH"
        Assert-True ((Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath) -ceq $beforeTask) "$Case $scriptName changed task XML"
    }
}

try {
    $installSource = [IO.File]::ReadAllText((Join-Path $repo 'scripts\install.ps1'))
    $uninstallSource = [IO.File]::ReadAllText((Join-Path $repo 'scripts\uninstall.ps1'))
    Assert-True ($installSource.Contains("Get-ScheduledTask -TaskName `$TaskName -TaskPath '\'")) 'install must select the root task explicitly'
    Assert-True ($installSource.Contains('scheduled_task_conflict')) 'install must gate conflicts before mutations'
    Assert-True ($uninstallSource.Contains("Unregister-ScheduledTask -TaskName `$TaskName -TaskPath '\'")) 'uninstall must remove the root task explicitly'
    Assert-True ($uninstallSource.Contains('scheduled_task_conflict')) 'uninstall must gate conflicts before mutations'
    Assert-True ($null -eq (Get-RootTask)) 'root production task must be absent'
    New-Item -ItemType Directory -Path $fixtureRoot -Force | Out-Null
    $a = New-Package -Name 'A'; $b = New-Package -Name 'B'

    Assert-True ((Invoke-Release -Directory $a -Script 'install.ps1').ExitCode -eq 0) 'A install failed'
    Capture-FixtureRootTask
    Assert-ConflictNoMutation -Directory $b -Case 'A/B directory conflict'
    Assert-True ((Invoke-Release -Directory $a -Script 'uninstall.ps1').ExitCode -eq 0) 'A uninstall failed'

    Assert-True ((Invoke-Release -Directory $b -Script 'install.ps1').ExitCode -eq 0) 'B install failed'
    Capture-FixtureRootTask
    $replacementDefinition = New-TaskDefinition -Executable (Join-Path $a 'sidraviactl.exe') -Arguments 'daemon start --log-level info'
    Register-ScheduledTask -TaskName $taskName -TaskPath $taskPath -InputObject $replacementDefinition -Force | Out-Null
    $replacementXML = Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath
    Remove-FixtureRootTask
    Assert-True ($null -ne (Get-RootTask)) 'foreign replacement was removed by fixture cleanup'
    Assert-True ((Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath) -ceq $replacementXML) 'foreign replacement XML changed by fixture cleanup'
    $currentReplacementXML = Export-ScheduledTask -TaskName $taskName -TaskPath $taskPath
    Assert-True ($currentReplacementXML -ceq $replacementXML) 'foreign replacement changed before exact cleanup'
    Unregister-ScheduledTask -TaskName $taskName -TaskPath $taskPath -Confirm:$false
    Assert-True ($null -eq (Get-RootTask)) 'foreign replacement cleanup failed'
    Assert-True ((Invoke-Release -Directory $b -Script 'install.ps1').ExitCode -eq 0) 'B reinstall after foreign replacement failed'
    Capture-FixtureRootTask
    Assert-ConflictNoMutation -Directory $a -Case 'old A uninstall against B ownership'
    $cases = @(
        [pscustomobject]@{ Name = 'wrong executable'; Definition = (New-TaskDefinition -Executable (Join-Path $a 'sidraviactl.exe') -Arguments 'daemon start --log-level info') },
        [pscustomobject]@{ Name = 'wrong arguments'; Definition = (New-TaskDefinition -Executable (Join-Path $b 'sidraviactl.exe') -Arguments 'daemon start --log-level invalid') },
        [pscustomobject]@{ Name = 'multiple actions'; Definition = (New-TaskDefinition -Executable (Join-Path $b 'sidraviactl.exe') -Arguments 'daemon start --log-level info' -ActionCount 2) },
        [pscustomobject]@{ Name = 'multiple triggers'; Definition = (New-TaskDefinition -Executable (Join-Path $b 'sidraviactl.exe') -Arguments 'daemon start --log-level info' -TriggerCount 2) },
        [pscustomobject]@{ Name = 'wrong trigger type'; Definition = (New-TaskDefinition -Executable (Join-Path $b 'sidraviactl.exe') -Arguments 'daemon start --log-level info' -TriggerType Once) }
    )
    foreach ($case in $cases) {
        Set-FixtureRootTask -Definition $case.Definition
        Assert-ConflictNoMutation -Directory $b -Case $case.Name
        Remove-FixtureRootTask
        Assert-True ((Invoke-Release -Directory $b -Script 'install.ps1').ExitCode -eq 0) "$($case.Name) cleanup did not restore B ownership"
        Capture-FixtureRootTask
    }

    Assert-True ((Invoke-Release -Directory $b -Script 'install.ps1' -Arguments @('-LogLevel', 'debug')).ExitCode -eq 0) 'same-directory debug reinstall failed'
    Capture-FixtureRootTask
    Assert-True ((Invoke-Release -Directory $b -Script 'uninstall.ps1').ExitCode -eq 0) 'owned uninstall failed'
    $fixtureTaskCreated = $false
    $fixtureTaskXML = $null
    Assert-True ((Invoke-Release -Directory $b -Script 'uninstall.ps1').ExitCode -eq 0) 'absent uninstall not idempotent'

    Register-ScheduledTask -TaskName $taskName -TaskPath $nested -Action (New-ScheduledTaskAction -Execute 'cmd.exe') -Trigger (New-ScheduledTaskTrigger -AtLogOn) -Force | Out-Null
    $nestedTaskCreated = $true
    Assert-True ((Invoke-Release -Directory $a -Script 'uninstall.ps1').ExitCode -eq 0) 'non-root task should be ignored'
    Assert-True ($null -ne (Get-ScheduledTask -TaskName $taskName -TaskPath $nested -ErrorAction SilentlyContinue)) 'non-root task was removed'
    Unregister-ScheduledTask -TaskName $taskName -TaskPath $nested -Confirm:$false
    $nestedTaskCreated = $false
} finally {
    if ($nestedTaskCreated -and $null -ne (Get-ScheduledTask -TaskName $taskName -TaskPath $nested -ErrorAction SilentlyContinue)) { Unregister-ScheduledTask -TaskName $taskName -TaskPath $nested -Confirm:$false }
    Remove-FixtureRootTask
    [Environment]::SetEnvironmentVariable('Path', $originalPath, 'User')
    if (Test-Path -LiteralPath $fixtureRoot) { Remove-Item -LiteralPath $fixtureRoot -Recurse -Force }
}
