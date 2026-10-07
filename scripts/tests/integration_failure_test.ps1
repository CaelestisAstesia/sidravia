# Development-only isolated tests for install/uninstall recovery. No real PATH or
# Task Scheduler mutation is permitted; production copies receive asserted hooks.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSEdition -ne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) {
    throw 'failure_tests_require_windows_powershell_5_1'
}

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw "assertion_failed: $Message" }
}
function Assert-Equal {
    param([AllowNull()][object]$Expected, [AllowNull()][object]$Actual, [string]$Message)
    if ($Expected -cne $Actual) { throw "assertion_failed: $Message; expected=[$Expected] actual=[$Actual]" }
}
function New-HarnessTask {
    param([string]$InstallDirectory, [string]$UserSID, [string]$Level = 'info')
    return [pscustomobject]@{
        TaskPath = '\'; TaskName = 'SidraviaDaemon'
        Actions = @([pscustomobject]@{ Execute = (Join-Path $InstallDirectory 'sidraviactl.exe'); Arguments = "daemon start --log-level $Level" })
        Triggers = @([pscustomobject]@{ UserId = $UserSID; CimClass = [pscustomobject]@{ CimClassName = 'MSFT_TaskLogonTrigger' } })
        Principal = [pscustomobject]@{ UserId = $UserSID; LogonType = 'Interactive'; RunLevel = 'Limited' }
    }
}
function New-HarnessXml {
    param([object]$Definition, [string]$Marker = '')
    $exe = [Security.SecurityElement]::Escape([string]$Definition.Actions[0].Execute)
    $args = [Security.SecurityElement]::Escape([string]$Definition.Actions[0].Arguments)
    $sid = [Security.SecurityElement]::Escape([string]$Definition.Principal.UserId)
    $markerNode = if ($Marker) { "<Settings><Description>$Marker</Description></Settings>" } else { '<Settings />' }
    return "<Task><Principals><UserId>$sid</UserId><LogonType>Interactive</LogonType><RunLevel>Limited</RunLevel></Principals><Triggers><Logon UserId=`"$sid`" /></Triggers><Actions><Exec><Command>$exe</Command><Arguments>$args</Arguments></Exec></Actions>$markerNode</Task>"
}
function Reset-Harness {
    param([AllowNull()][object]$Path, [string]$InstallDirectory, [string]$Mode = '', [object]$InitialTask = $null, [AllowNull()][object]$InitialXml = $null)
    $global:Harness = [ordered]@{
        Path = $Path; InstallDirectory = $InstallDirectory; SID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
        Task = $InitialTask; TaskXml = $InitialXml; XmlTasks = @{}; Mode = $Mode
        PathWrites = 0; LastPathSetNull = $false; RegisterCalls = 0; UnregisterCalls = 0; QueryCalls = 0; ExportCalls = 0
        RegisterBehavior = ''; UnregisterBehavior = ''; PathBehavior = ''; QueryFailAt = @()
        ExternalPath = $null; ExternalTask = $null; ExternalTaskXml = $null; DefinitionFailure = $false
        DefinitionExportBehavior = ''; NullPathWrites = 0
    }
    if ($null -ne $InitialTask -and $InitialXml) { $global:Harness.XmlTasks[$InitialXml] = $InitialTask }
}
function Get-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath, [Parameter(ValueFromRemainingArguments=$true)][object[]]$Rest)
    $global:Harness.QueryCalls++
    if (@($global:Harness.QueryFailAt) -contains $global:Harness.QueryCalls) { throw 'simulated_task_query_access_denied' }
    if ($null -eq $global:Harness.Task) { return @() }
    return ,$global:Harness.Task
}
function Export-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath, [object]$InputObject, [Parameter(ValueFromRemainingArguments=$true)][object[]]$Rest)
    $global:Harness.ExportCalls++
    if ($null -ne $InputObject) {
        if ($null -ne $global:Harness.Task -and [object]::ReferenceEquals($InputObject, $global:Harness.Task)) { return $global:Harness.TaskXml }
        if ($global:Harness.DefinitionExportBehavior -eq 'throw') { throw 'simulated_definition_export_failure' }
        if ($global:Harness.DefinitionExportBehavior -eq 'empty') { return '' }
        return (New-HarnessXml -Definition $InputObject)
    }
    if ($null -eq $global:Harness.TaskXml) { throw 'simulated_export_without_task' }
    return $global:Harness.TaskXml
}
function New-ScheduledTaskAction {
    param([string]$Execute, [string]$Argument)
    return [pscustomobject]@{ Execute = $Execute; Arguments = $Argument }
}
function New-ScheduledTaskTrigger {
    param([switch]$AtLogOn, [string]$User)
    return [pscustomobject]@{ UserId = $User; CimClass = [pscustomobject]@{ CimClassName = 'MSFT_TaskLogonTrigger' } }
}
function New-ScheduledTaskPrincipal {
    param([string]$UserId, [string]$LogonType, [string]$RunLevel)
    return [pscustomobject]@{ UserId = $UserId; LogonType = $LogonType; RunLevel = $RunLevel }
}
function New-ScheduledTask {
    param([object]$Action, [object]$Trigger, [object]$Principal, [string]$Description)
    if ($global:Harness.DefinitionFailure) { throw 'simulated_definition_preflight_failure' }
    return [pscustomobject]@{ Actions = @($Action); Triggers = @($Trigger); Principal = $Principal; Description = $Description }
}
function Register-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath, [object]$InputObject, [string]$Xml, [switch]$Force, [Parameter(ValueFromRemainingArguments=$true)][object[]]$Rest)
    $global:Harness.RegisterCalls++
    if ($global:Harness.RegisterBehavior -eq 'before') { throw 'simulated_register_before_mutation' }
    if ($global:Harness.RegisterBehavior -eq 'restore_failure' -and $null -ne $Xml) { throw 'simulated_restore_failure' }
    if ($null -ne $InputObject) {
        $definition = $InputObject
        $task = New-HarnessTask -InstallDirectory $global:Harness.InstallDirectory -UserSID $global:Harness.SID -Level ([string]$definition.Actions[0].Arguments -replace '^.*--log-level ', '')
        $task.Actions[0].Execute = $definition.Actions[0].Execute
        $task.Actions[0].Arguments = $definition.Actions[0].Arguments
        $task.Triggers[0].UserId = $definition.Triggers[0].UserId
        $task.Principal = $definition.Principal
        $global:Harness.TaskXml = New-HarnessXml -Definition $definition
    } else {
        if (-not $global:Harness.XmlTasks.Contains($Xml)) { throw 'simulated_unknown_xml_restore' }
        $task = $global:Harness.XmlTasks[$Xml]
        $global:Harness.TaskXml = $Xml
    }
    $global:Harness.Task = $task
    if ($global:Harness.RegisterBehavior -eq 'external_after') {
        $global:Harness.Task = $global:Harness.ExternalTask
        $global:Harness.TaskXml = '<Task><Settings><Description>external-full-settings</Description></Settings><Principals><UserId>' + $global:Harness.SID + '</UserId><LogonType>Interactive</LogonType><RunLevel>Limited</RunLevel></Principals><Triggers><Logon UserId="' + $global:Harness.SID + '" /></Triggers><Actions><Exec><Command>' + [Security.SecurityElement]::Escape((Join-Path $global:Harness.InstallDirectory 'sidraviactl.exe')) + '</Command><Arguments>daemon start --log-level info</Arguments></Exec></Actions></Task>'
        $global:Harness.Path = $global:Harness.ExternalPath
        throw 'simulated_register_external_interference'
    }
    if ($global:Harness.RegisterBehavior -eq 'external_settings_after') {
        $global:Harness.TaskXml = New-HarnessXml -Definition $global:Harness.Task -Marker 'external-full-settings'
        $global:Harness.ExternalTaskXml = $global:Harness.TaskXml
    }
    if ($global:Harness.RegisterBehavior -eq 'after') { throw 'simulated_register_after_mutation' }
    if ($global:Harness.RegisterBehavior -eq 'after_restore_fail') { $global:Harness.RegisterBehavior = 'restore_failure'; throw 'simulated_register_after_mutation' }
    if ($global:Harness.RegisterBehavior -eq 'restore_failure' -and $null -ne $Xml) { throw 'simulated_restore_failure' }
    return $task
}
function Unregister-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath, [switch]$Confirm, [Parameter(ValueFromRemainingArguments=$true)][object[]]$Rest)
    $global:Harness.UnregisterCalls++
    if ($global:Harness.UnregisterBehavior -eq 'before') { throw 'simulated_unregister_before_mutation' }
    $global:Harness.Task = $null; $global:Harness.TaskXml = $null
    if ($global:Harness.UnregisterBehavior -eq 'external_after') {
        $global:Harness.Task = $global:Harness.ExternalTask
        $global:Harness.TaskXml = '<Task><Settings><Description>foreign-replacement</Description></Settings></Task>'
        throw 'simulated_unregister_external_interference'
    }
    if ($global:Harness.UnregisterBehavior -eq 'after') { throw 'simulated_unregister_after_mutation' }
}

$root = Split-Path -Parent $PSScriptRoot
$sourceInstall = Join-Path $root 'install.ps1'
$sourceUninstall = Join-Path $root 'uninstall.ps1'
$sourceBytesInstall = [IO.File]::ReadAllBytes($sourceInstall)
$sourceBytesUninstall = [IO.File]::ReadAllBytes($sourceUninstall)
Assert-True ($sourceBytesInstall.Length -ge 3 -and $sourceBytesInstall[0] -eq 239 -and $sourceBytesInstall[1] -eq 187 -and $sourceBytesInstall[2] -eq 191) 'install source BOM present'
Assert-True ($sourceBytesUninstall.Length -ge 3 -and $sourceBytesUninstall[0] -eq 239 -and $sourceBytesUninstall[1] -eq 187 -and $sourceBytesUninstall[2] -eq 191) 'uninstall source BOM present'
$utf8 = [System.Text.UTF8Encoding]::new($false, $true)
$installSource = $utf8.GetString($sourceBytesInstall, 3, $sourceBytesInstall.Length - 3)
$uninstallSource = $utf8.GetString($sourceBytesUninstall, 3, $sourceBytesUninstall.Length - 3)
# Guard every production User PATH .NET access: each script's getter and both
# explicit null/string setter forms stay inside replaceable wrappers. Task cmdlets go through
# the harness functions resolved in this test scope.
Assert-Equal 1 ([regex]::Matches($installSource, "\[Environment\]::GetEnvironmentVariable\('Path', 'User'\)").Count) 'install PATH getter surface'
Assert-Equal 1 ([regex]::Matches($installSource, '\[Environment\]::SetEnvironmentVariable\(''Path'', \$null, ''User''\)').Count) 'install PATH null setter surface'
Assert-Equal 1 ([regex]::Matches($installSource, '\[Environment\]::SetEnvironmentVariable\(''Path'', \[string\]\$Value, ''User''\)').Count) 'install PATH string setter surface'
Assert-Equal 1 ([regex]::Matches($uninstallSource, "\[Environment\]::GetEnvironmentVariable\('Path', 'User'\)").Count) 'uninstall PATH getter surface'
Assert-Equal 1 ([regex]::Matches($uninstallSource, '\[Environment\]::SetEnvironmentVariable\(''Path'', \$null, ''User''\)').Count) 'uninstall PATH null setter surface'
Assert-Equal 1 ([regex]::Matches($uninstallSource, '\[Environment\]::SetEnvironmentVariable\(''Path'', \[string\]\$Value, ''User''\)').Count) 'uninstall PATH string setter surface'
foreach ($name in @('Get-ScheduledTask', 'Export-ScheduledTask', 'Register-ScheduledTask', 'Unregister-ScheduledTask')) {
    Assert-True ($installSource -match "\b$name\b" -and $uninstallSource -match "\b$name\b") "mock interception exists for $name"
}
$taskCommands = @('Get-ScheduledTask', 'Export-ScheduledTask', 'Register-ScheduledTask', 'Unregister-ScheduledTask', 'New-ScheduledTaskAction', 'New-ScheduledTaskTrigger', 'New-ScheduledTaskPrincipal', 'New-ScheduledTask')
foreach ($source in @($installSource, $uninstallSource)) {
    $tokens = $null; $parseErrors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$parseErrors)
    Assert-Equal 0 $parseErrors.Count 'production source parses before isolated evaluation'
    $commands = @($ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] }, $true) | ForEach-Object { $_.GetCommandName() })
    foreach ($command in $commands) {
        if ($command -like '*ScheduledTask*') { Assert-True ($taskCommands -ccontains $command) "only intercepted Task Scheduler command is used: $command" }
    }
}
foreach ($name in $taskCommands) { Assert-True ($null -ne (Get-Command $name -CommandType Function -ErrorAction SilentlyContinue)) "test harness intercepts $name" }

$caseRoot = Join-Path $env:TEMP ('sidravia-ext-a11-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $caseRoot | Out-Null
try {
    $productRoot = Join-Path $caseRoot 'product'
    $scriptsDir = Join-Path $productRoot 'scripts'
    New-Item -ItemType Directory -Path $scriptsDir -Force | Out-Null
    foreach ($exe in @('sidraviactl.exe', 'sidraviad.exe')) { [IO.File]::WriteAllBytes((Join-Path $productRoot $exe), [byte[]]@()) }
    $installCopy = $installSource -replace "(?ms)function Get-SidraviaUserPath \{.*?\n\}", "function Get-SidraviaUserPath { return `$global:Harness.Path }"
    $installCopy = $installCopy -replace "(?ms)function Set-SidraviaUserPath \{.*?\n\}", "function Set-SidraviaUserPath { param([AllowNull()][object]`$Value) `$global:Harness.PathWrites++; `$global:Harness.LastPathSetNull = (`$null -eq `$Value); if (`$null -eq `$Value) { `$global:Harness.NullPathWrites++ }; if (`$global:Harness.PathBehavior -eq 'before') { throw 'simulated_path_before_mutation' }; `$global:Harness.Path = `$Value; if (`$global:Harness.PathBehavior -eq 'after') { throw 'simulated_path_after_mutation' }; if (`$global:Harness.PathBehavior -eq 'after_then_block_restore') { `$global:Harness.PathBehavior = 'before'; throw 'simulated_path_after_mutation' }; if (`$global:Harness.PathBehavior -eq 'succeed_then_block_restore') { `$global:Harness.PathBehavior = 'before'; return }; if (`$global:Harness.PathBehavior -eq 'external_after') { `$global:Harness.Path = `$global:Harness.ExternalPath; throw 'simulated_path_external_interference' } }"
    $uninstallCopy = $uninstallSource -replace "(?ms)function Get-SidraviaUserPath \{.*?\n\}", "function Get-SidraviaUserPath { return `$global:Harness.Path }"
    $uninstallCopy = $uninstallCopy -replace "(?ms)function Set-SidraviaUserPath \{.*?\n\}", "function Set-SidraviaUserPath { param([AllowNull()][object]`$Value) `$global:Harness.PathWrites++; `$global:Harness.LastPathSetNull = (`$null -eq `$Value); if (`$null -eq `$Value) { `$global:Harness.NullPathWrites++ }; if (`$global:Harness.PathBehavior -eq 'before') { throw 'simulated_path_before_mutation' }; `$global:Harness.Path = `$Value; if (`$global:Harness.PathBehavior -eq 'after') { throw 'simulated_path_after_mutation' }; if (`$global:Harness.PathBehavior -eq 'after_then_block_restore') { `$global:Harness.PathBehavior = 'before'; throw 'simulated_path_after_mutation' }; if (`$global:Harness.PathBehavior -eq 'succeed_then_block_restore') { `$global:Harness.PathBehavior = 'before'; return }; if (`$global:Harness.PathBehavior -eq 'external_after') { `$global:Harness.Path = `$global:Harness.ExternalPath; throw 'simulated_path_external_interference' } }"
    Assert-True ($installCopy -ne $installSource -and $uninstallCopy -ne $uninstallSource) 'both PATH wrappers replaced in isolated copies'
    foreach ($copy in @($installCopy, $uninstallCopy)) {
        Assert-Equal 0 ([regex]::Matches($copy, "\[Environment\]::GetEnvironmentVariable\('Path', 'User'\)").Count) 'no real User PATH getter survives substitution'
        Assert-Equal 0 ([regex]::Matches($copy, '\[Environment\]::SetEnvironmentVariable\(''Path'',\s*(\$null|\[string\]\$Value),\s*''User''\)').Count) 'no real User PATH setter survives substitution'
    }
    $installPath = Join-Path $scriptsDir 'install.ps1'; $uninstallPath = Join-Path $scriptsDir 'uninstall.ps1'
    [IO.File]::WriteAllText($installPath, $installCopy, ([System.Text.UTF8Encoding]::new($true)))
    [IO.File]::WriteAllText($uninstallPath, $uninstallCopy, ([System.Text.UTF8Encoding]::new($true)))
    $installDir = $productRoot.TrimEnd('\')

    function Invoke-Product {
        param([string]$Which)
        try {
            if ($Which -eq 'install') { & $installPath -LogLevel info | Out-Null }
            else { & $uninstallPath | Out-Null }
            return $null
        } catch { return $_.Exception }
    }
    function Assert-FailedResult {
        param([object]$Error, [string]$StatePath, [string]$StateTask, [string]$CauseFragment, [string]$OperationFragment)
        Assert-True ($null -ne $Error) 'expected operation failure'
        Assert-True ($Error -is [InvalidOperationException]) 'stable wrapper exception type'
        Assert-True ($Error.Message.Contains("path=$StatePath")) 'path recovery state reported'
        Assert-True ($Error.Message.Contains("task=$StateTask")) 'task recovery state reported'
        Assert-True ($Error.Message.Contains($OperationFragment)) 'operation/stage code reported'
        Assert-True ($null -ne $Error.InnerException -and $Error.InnerException.Message.Contains($CauseFragment)) 'original cause retained'
    }
    function New-InitialOwned {
        param([string]$Marker = '')
        $definition = [pscustomobject]@{
            Actions = @([pscustomobject]@{ Execute = (Join-Path $installDir 'sidraviactl.exe'); Arguments = 'daemon start --log-level debug' })
            Triggers = @([pscustomobject]@{ UserId = $global:Harness.SID })
            Principal = [pscustomobject]@{ UserId = $global:Harness.SID; LogonType = 'Interactive'; RunLevel = 'Limited' }
        }
        $task = New-HarnessTask -InstallDirectory $installDir -UserSID $global:Harness.SID -Level debug
        $xml = New-HarnessXml -Definition $definition -Marker $Marker
        $global:Harness.Task = $task; $global:Harness.TaskXml = $xml; $global:Harness.XmlTasks[$xml] = $task
        return $xml
    }
    function Reset-Case {
        param([AllowNull()][object]$Path = 'C:\existing;C:\tools', [string]$Mode = '', [bool]$OwnedTask = $false, [string]$Marker = '')
        Reset-Harness -Path $Path -InstallDirectory $installDir -Mode $Mode
        if ($OwnedTask) { [void](New-InitialOwned -Marker $Marker) }
    }

    # Normal and idempotent paths preserve existing behavior and summaries.
    Reset-Case
    Assert-True ($null -eq (Invoke-Product install)) 'normal install succeeds'
    Assert-True ($global:Harness.Path.Contains($installDir)) 'install adds exact directory'
    Assert-True ($null -ne $global:Harness.Task) 'install creates task'
    Assert-True ($null -eq (Invoke-Product install)) 'repeat install succeeds'
    Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true
    Assert-True ($null -eq (Invoke-Product uninstall)) 'normal uninstall succeeds'
    Assert-Equal 'C:\existing' $global:Harness.Path 'uninstall removes only owned path item'
    Assert-True ($null -eq $global:Harness.Task) 'uninstall removes owned task'
    Assert-True ($null -eq (Invoke-Product uninstall)) 'repeat uninstall succeeds'

    # Foreign task conflicts before either resource is changed.
    Reset-Case
    $global:Harness.Task = [pscustomobject]@{ TaskPath='\'; TaskName='SidraviaDaemon'; Actions=@(); Triggers=@(); Principal=[pscustomobject]@{} }
    $global:Harness.TaskXml = '<Task><Foreign /></Task>'
    $err = Invoke-Product install
    Assert-True ($null -ne $err -and $err.Message.Contains('scheduled_task_conflict')) 'foreign install task rejected'
    Assert-Equal 0 $global:Harness.PathWrites 'foreign task conflict has no path side effect'
    Assert-Equal 0 $global:Harness.RegisterCalls 'foreign task conflict has no task side effect'
    Reset-Case -Path ('C:\existing;' + $installDir)
    $global:Harness.Task = [pscustomobject]@{ TaskPath='\'; TaskName='SidraviaDaemon'; Actions=@(); Triggers=@(); Principal=[pscustomobject]@{} }
    $global:Harness.TaskXml = '<Task><Foreign /></Task>'
    $err = Invoke-Product uninstall
    Assert-True ($null -ne $err -and $err.Message.Contains('scheduled_task_conflict')) 'foreign uninstall task rejected'
    Assert-Equal 0 $global:Harness.PathWrites 'foreign uninstall conflict has no path side effect'

    # Definition/preflight errors occur before any writes.
    Reset-Case
    $global:Harness.DefinitionFailure = $true
    $err = Invoke-Product install
    Assert-True ($null -ne $err -and $err.Message.Contains('simulated_definition_preflight_failure')) 'definition error propagated'
    Assert-Equal 0 $global:Harness.PathWrites 'preflight has no path side effect'
    Assert-Equal 0 $global:Harness.RegisterCalls 'preflight has no task side effect'
    foreach ($behavior in @('empty', 'throw')) {
        Reset-Case
        $global:Harness.DefinitionExportBehavior = $behavior
        $err = Invoke-Product install
        $cause = if ($behavior -eq 'empty') { 'scheduled_task_export_empty' } else { 'simulated_definition_export_failure' }
        Assert-True ($null -ne $err -and $err.Message.Contains($cause)) 'definition export is validated before writes'
        Assert-Equal 0 $global:Harness.PathWrites 'definition export failure has no PATH side effect'
        Assert-Equal 0 $global:Harness.RegisterCalls 'definition export failure has no task side effect'
    }

    # PATH setter failures before/after mutation, with exact original raw string restored.
    foreach ($behavior in @('before', 'after')) {
        Reset-Case -Path 'C:\existing;;C:\tail'
        $global:Harness.PathBehavior = $behavior
        $err = Invoke-Product install
        $expected = if ($behavior -eq 'before') { 'unchanged' } else { 'restored' }
        Assert-FailedResult $err $expected 'unchanged' "simulated_path_${behavior}_mutation" 'install_path_write_failed'
        Assert-Equal 'C:\existing;;C:\tail' $global:Harness.Path 'exact raw PATH restored'
        Assert-Equal 0 $global:Harness.RegisterCalls 'path failure prevents task registration'
    }
    foreach ($behavior in @('before', 'after')) {
        Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true
        $oldXml = $global:Harness.TaskXml
        $global:Harness.PathBehavior = $behavior
        $err = Invoke-Product uninstall
        $expected = if ($behavior -eq 'before') { 'unchanged' } else { 'restored' }
        Assert-FailedResult $err $expected 'unchanged' "simulated_path_${behavior}_mutation" 'uninstall_path_write_failed'
        Assert-Equal ('C:\existing;' + $installDir) $global:Harness.Path 'uninstall restores exact PATH'
        Assert-Equal $oldXml $global:Harness.TaskXml 'PATH failure leaves original task untouched'
        Assert-Equal 0 $global:Harness.UnregisterCalls 'PATH failure prevents task removal'
    }

    # Removing the only PATH entry writes native null; later failure restores its exact old value.
    Reset-Case -Path $installDir -OwnedTask $true -Marker 'sole-path-entry'
    $solePathBefore = $global:Harness.Path
    $soleTaskBefore = $global:Harness.TaskXml
    $global:Harness.UnregisterBehavior = 'after'
    $err = Invoke-Product uninstall
    Assert-FailedResult $err 'restored' 'restored' 'simulated_unregister_after_mutation' 'uninstall_task_unregistration_failed'
    Assert-Equal $solePathBefore $global:Harness.Path 'sole PATH entry restored exactly'
    Assert-Equal 1 $global:Harness.NullPathWrites 'removing the final PATH entry passed actual null'
    Assert-Equal $soleTaskBefore $global:Harness.TaskXml 'sole-entry failure restores full task definition'

    # Registration throws before/after its own mutation; eligible PATH/task recovery is independent.
    foreach ($behavior in @('before', 'after')) {
        Reset-Case -Path 'C:\existing'
        $global:Harness.RegisterBehavior = $behavior
        $err = Invoke-Product install
        $taskExpected = if ($behavior -eq 'before') { 'restored' } else { 'restored' }
        Assert-FailedResult $err 'restored' $taskExpected "simulated_register_${behavior}_mutation" 'scheduled_task_registration_failed'
        Assert-Equal 'C:\existing' $global:Harness.Path 'registration failure restores PATH'
        Assert-True ($null -eq $global:Harness.Task) 'new task removed after failed registration'
    }

    # True null PATH and absent task survive register-before, register-after and validation failures.
    foreach ($behavior in @('before', 'after')) {
        Reset-Case -Path $null
        $global:Harness.RegisterBehavior = $behavior
        $err = Invoke-Product install
        Assert-FailedResult $err 'restored' 'restored' "simulated_register_${behavior}_mutation" 'scheduled_task_registration_failed'
        Assert-True ($null -eq $global:Harness.Path) 'initially null PATH is restored as null'
        Assert-True $global:Harness.LastPathSetNull 'PATH mock received actual null on restoration'
        Assert-True ($null -eq $global:Harness.Task) 'absent task is restored to absent'
    }
    Reset-Case -Path $null
    $global:Harness.QueryFailAt = @(2)
    $err = Invoke-Product install
    Assert-FailedResult $err 'restored' 'restored' 'simulated_task_query_access_denied' 'install_post_validation_failed'
    Assert-True ($null -eq $global:Harness.Path -and $global:Harness.LastPathSetNull) 'validation recovery preserves true null PATH'
    Assert-True ($null -eq $global:Harness.Task) 'validation recovery removes only its newly created task'

    # An owned task with custom full XML settings is restored after post-validation failure.
    Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true -Marker 'custom-retained-setting'
    $oldXml = $global:Harness.TaskXml
    $global:Harness.QueryFailAt = 2
    $err = Invoke-Product install
    Assert-FailedResult $err 'unchanged' 'restored' 'simulated_task_query_access_denied' 'install_post_validation_failed'
    Assert-Equal $oldXml $global:Harness.TaskXml 'full custom task XML restored'

    # A newly created task is removed after a later failure; pre-existing PATH entry stays exact.
    Reset-Case -Path ('C:\existing;' + $installDir)
    $global:Harness.RegisterBehavior = 'after'
    $err = Invoke-Product install
    Assert-FailedResult $err 'unchanged' 'restored' 'simulated_register_after_mutation' 'scheduled_task_registration_failed'
    Assert-Equal ('C:\existing;' + $installDir) $global:Harness.Path 'pre-existing PATH entry is unchanged'

    # Unregister failures before/after deletion restore the previous task and raw PATH.
    foreach ($behavior in @('before', 'after')) {
        Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true -Marker 'custom-on-uninstall'
        $oldXml = $global:Harness.TaskXml
        $global:Harness.UnregisterBehavior = $behavior
        $err = Invoke-Product uninstall
        $taskExpected = if ($behavior -eq 'before') { 'unchanged' } else { 'restored' }
        Assert-FailedResult $err 'restored' $taskExpected "simulated_unregister_${behavior}_mutation" 'uninstall_task_unregistration_failed'
        Assert-Equal ('C:\existing;' + $installDir) $global:Harness.Path 'unregister failure restores raw PATH'
        Assert-Equal $oldXml $global:Harness.TaskXml 'unregister failure restores full task XML'
    }

    # Uninstall post-validation failure restores both resources.
    Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true -Marker 'custom-postcheck'
    $oldXml = $global:Harness.TaskXml
    $global:Harness.QueryFailAt = 2
    $err = Invoke-Product uninstall
    Assert-FailedResult $err 'restored' 'restored' 'simulated_task_query_access_denied' 'uninstall_post_validation_failed'
    Assert-Equal ('C:\existing;' + $installDir) $global:Harness.Path 'post-validation restores raw PATH'
    Assert-Equal $oldXml $global:Harness.TaskXml 'post-validation restores full task XML'

    # Initial task-query errors propagate and are never interpreted as absence.
    Reset-Case
    $global:Harness.QueryFailAt = 1
    $err = Invoke-Product install
    Assert-True ($null -ne $err -and $err.Message.Contains('simulated_task_query_access_denied')) 'query error propagated'
    Assert-Equal 0 $global:Harness.PathWrites 'query error does not mutate PATH'
    Assert-Equal 0 $global:Harness.RegisterCalls 'query error does not register task'

    # A recovery-time query failure is unknown and never permits task overwrite/delete.
    Reset-Case -Path $null
    $global:Harness.RegisterBehavior = 'after'
    $global:Harness.QueryFailAt = @(2, 3)
    $err = Invoke-Product install
    Assert-FailedResult $err 'restored' 'unknown' 'simulated_register_after_mutation' 'scheduled_task_registration_failed'
    Assert-True ($null -ne $global:Harness.Task) 'task is preserved when recovery query is unknown'
    Assert-True ($null -eq $global:Harness.Path) 'independent PATH recovery still restores null'

    # External state that differs from our expected complete write is preserved.
    Reset-Case -Path 'C:\existing'
    $global:Harness.ExternalPath = 'C:\external;C:\existing'
    $global:Harness.ExternalTask = [pscustomobject]@{ TaskPath='\'; TaskName='SidraviaDaemon'; Actions=@(); Triggers=@(); Principal=[pscustomobject]@{} }
    $global:Harness.RegisterBehavior = 'external_after'
    $err = Invoke-Product install
    Assert-FailedResult $err 'changed_externally' 'changed_externally' 'simulated_register_external_interference' 'scheduled_task_registration_failed'
    Assert-Equal $global:Harness.ExternalTask $global:Harness.Task 'foreign replacement task preserved'
    Assert-Equal $global:Harness.ExternalPath $global:Harness.Path 'external PATH replacement preserved'

    # A PATH-stage failure preserves an external replacement even when the old PATH was null.
    Reset-Case -Path $null
    $global:Harness.ExternalPath = 'C:\external-path'
    $global:Harness.PathBehavior = 'external_after'
    $err = Invoke-Product install
    Assert-FailedResult $err 'changed_externally' 'unchanged' 'simulated_path_external_interference' 'install_path_write_failed'
    Assert-Equal $global:Harness.ExternalPath $global:Harness.Path 'external PATH replacement survives PATH-stage failure'

    # A foreign task that appears after our own uninstall is preserved.
    Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true -Marker 'uninstall-external'
    $global:Harness.ExternalTask = [pscustomobject]@{ TaskPath='\'; TaskName='SidraviaDaemon'; Actions=@(); Triggers=@(); Principal=[pscustomobject]@{} }
    $global:Harness.UnregisterBehavior = 'external_after'
    $err = Invoke-Product uninstall
    Assert-FailedResult $err 'restored' 'changed_externally' 'simulated_unregister_external_interference' 'uninstall_task_unregistration_failed'
    Assert-Equal $global:Harness.ExternalTask $global:Harness.Task 'foreign task appearing after deletion is preserved'
    Assert-True ($global:Harness.TaskXml.Contains('foreign-replacement')) 'foreign task XML remains intact'

    # A PATH rollback failure cannot prevent eligible restoration of the deleted task.
    Reset-Case -Path ('C:\existing;' + $installDir) -OwnedTask $true -Marker 'independent-task-restore'
    $taskBeforeBlockedPathRestore = $global:Harness.TaskXml
    $global:Harness.PathBehavior = 'succeed_then_block_restore'
    $global:Harness.UnregisterBehavior = 'after'
    $err = Invoke-Product uninstall
    Assert-FailedResult $err 'rollback_failed' 'restored' 'simulated_unregister_after_mutation' 'uninstall_task_unregistration_failed'
    Assert-Equal $taskBeforeBlockedPathRestore $global:Harness.TaskXml 'eligible task restore runs after PATH rollback failure'

    # An owned-looking task with different full settings appearing after Register is external.
    Reset-Case -Path 'C:\existing'
    $global:Harness.RegisterBehavior = 'external_settings_after'
    $global:Harness.QueryFailAt = 2
    $err = Invoke-Product install
    Assert-FailedResult $err 'restored' 'changed_externally' 'simulated_task_query_access_denied' 'install_post_validation_failed'
    Assert-Equal $global:Harness.ExternalTaskXml $global:Harness.TaskXml 'complete external task settings preserved'

    # Task rollback failure is reported while the independently eligible PATH rollback succeeds.
    Reset-Case -Path 'C:\existing' -OwnedTask $true -Marker 'rollback-failure'
    $oldXml = $global:Harness.TaskXml
    $global:Harness.RegisterBehavior = 'after_restore_fail'
    $err = Invoke-Product install
    Assert-FailedResult $err 'restored' 'rollback_failed' 'simulated_register_after_mutation' 'scheduled_task_registration_failed'
    Assert-Equal 'C:\existing' $global:Harness.Path 'PATH restored despite task rollback failure'
    Assert-True ($global:Harness.TaskXml -cne $oldXml) 'failed task restoration is accurately reported'

    Reset-Case -Path 'C:\existing'
    $global:Harness.PathBehavior = 'after_then_block_restore'
    $err = Invoke-Product install
    Assert-FailedResult $err 'rollback_failed' 'unchanged' 'simulated_path_after_mutation' 'install_path_write_failed'
    Assert-Equal 0 $global:Harness.RegisterCalls 'task remains untouched when PATH rollback fails'

    Write-Output 'EXT-A11 isolated install/uninstall failure matrix PASS'
} finally {
    if ((Split-Path -Leaf $caseRoot) -like 'sidravia-ext-a11-test-*' -and (Test-Path -LiteralPath $caseRoot)) {
        Remove-Item -LiteralPath $caseRoot -Recurse -Force
    }
}
