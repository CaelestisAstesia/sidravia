//go:build windows

package installationmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise only the classifier with in-memory fixture objects. No real tasks
// or registry settings are created, queried or removed by these native tests.
func TestNativeTaskOwnership(t *testing.T) {
	functions, _, ok := strings.Cut(taskScript, "# command handler")
	if !ok {
		t.Fatal("missing script functions")
	}
	fixture := `
$sid = 'S-1-5-21-111-222-333-1001'
$dir = 'C:\中文 folder\Sidravia'
function Fixture {
    return [pscustomobject]@{
        TaskName='SidraviaDaemon'; TaskPath='\';
        Actions=@([pscustomobject]@{ Execute=(Join-Path $dir 'sidraviactl.exe'); Arguments='daemon start --log-level info'; WorkingDirectory='' });
        Triggers=@([pscustomobject]@{ UserId=$sid; CimClass=[pscustomobject]@{CimClassName='MSFT_TaskLogonTrigger'} });
        Principal=[pscustomobject]@{ UserId=$sid; LogonType='Interactive'; RunLevel='Limited' }
    }
}

function Expect($task, $want) {
    $actual = Classify-Task $task $dir $sid
    if ($actual -ne $want) { throw "classification mismatch $actual $want" }
}
Expect $null 'disabled'
Expect (Fixture) 'daemon'
$x=Fixture; $x.Actions[0].Execute=Join-Path $dir 'Sidravia.exe'; $x.Actions[0].Arguments='--start-hidden'; Expect $x 'gui'
$x=Fixture; $x.Actions[0].Execute='C:\Other\sidraviactl.exe'; Expect $x 'conflict'
$x=Fixture; $x.Actions[0].Arguments='daemon start --log-level debug --other'; Expect $x 'conflict'
$x=Fixture; $x.Triggers[0].UserId=''; Expect $x 'conflict'
$x=Fixture; $x.Principal.RunLevel='Highest'; Expect $x 'conflict'
$x=Fixture; $x.Principal.LogonType='S4U'; Expect $x 'conflict'
$x=Fixture; $x.TaskPath='\Other\'; Expect $x 'conflict'
$x=Fixture; $x.Actions[0].WorkingDirectory='C:\Other'; Expect $x 'conflict'
$x=Fixture; $x.Actions[0].Execute='sidraviactl.exe'; Expect $x 'conflict'
Write-Output 'ownership-fixtures-pass'
`
	output, err := invokePowerShell(functions+fixture, nil)
	if err != nil || strings.TrimSpace(string(output)) != "ownership-fixtures-pass" {
		t.Fatalf("native classifier failed: %v", err)
	}
}

func TestNativeStartupStatusAndRepair(t *testing.T) {
	functions, handler, ok := strings.Cut(taskScript, "# command handler")
	if !ok {
		t.Fatal("missing handler boundary")
	}
	// Targets exist only to satisfy the production existence check. They are
	// never executed; every task read/write below is an in-memory replacement.
	dir := t.TempDir()
	for _, name := range []string{"Sidravia.exe", "sidraviactl.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not an executable"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixture := `
$ProgressPreference = 'SilentlyContinue'
$PSModuleAutoLoadingPreference = 'None'
# Load the in-memory constructors before installing test replacements. An
# implicit first import would replace our functions with the module exports.
Import-Module Microsoft.PowerShell.Utility,Microsoft.PowerShell.Management,CimCmdlets,ScheduledTasks -ErrorAction Stop
$scenario = '%SCENARIO%'
$fixtureDir = %DIRECTORY%
$fixtureSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$global:writes = 0
$global:current = [pscustomobject]@{
    TaskName='SidraviaDaemon'; TaskPath='\';
    Actions=@([pscustomobject]@{Execute=(Join-Path $fixtureDir 'sidraviactl.exe');Arguments='daemon start --log-level debug';WorkingDirectory=$fixtureDir});
    Triggers=@([pscustomobject]@{UserId=$fixtureSID;Enabled=$true;CimClass=[pscustomobject]@{CimClassName='MSFT_TaskLogonTrigger'}});
    Principal=[pscustomobject]@{UserId=$fixtureSID;LogonType='Interactive';RunLevel='Limited'};
    Settings=[pscustomobject]@{Enabled=$true;DisallowStartIfOnBatteries=$false;StopIfGoingOnBatteries=$false;ExecutionTimeLimit='PT0S'}
}
switch ($scenario) {
    'absent' { $global:current=$null }
    'disabled-task' { $global:current.Settings.Enabled=$false }
    'disabled-trigger' { $global:current.Triggers[0].Enabled=$false }
    'legacy-settings' { $global:current.Settings.DisallowStartIfOnBatteries=$true; $global:current.Settings.StopIfGoingOnBatteries=$true; $global:current.Settings.ExecutionTimeLimit='PT72H' }
    'conflict' { $global:current.Actions[0].Execute='C:\Other\sidraviactl.exe' }
    'verify-disabled' { $global:current.Triggers[0].Enabled=$false }
}
function Read-Task { return $global:current }
function Register-ScheduledTask {
    param($TaskName,$TaskPath,$InputObject,[switch]$Force,$ErrorAction)
    $global:writes++
    if($scenario -eq 'conflict'){throw 'foreign task was overwritten'}
    $global:current=[pscustomobject]@{
        TaskName=$TaskName;TaskPath=$TaskPath;
        Actions=$InputObject.Actions;Triggers=$InputObject.Triggers;
        Principal=$InputObject.Principal;Settings=$InputObject.Settings
    }
    if($scenario -eq 'verify-disabled'){$global:current.Triggers[0].Enabled=$false}
}
function Unregister-ScheduledTask { throw 'unexpected task removal' }
foreach($name in @('Register-ScheduledTask','Unregister-ScheduledTask','Read-Task')) {
    if((Get-Command $name).ModuleName -ne ''){throw 'task fixture is not isolated'}
}
`
	for _, tc := range []struct {
		scenario, operation, mode, code string
		changed, success                bool
	}{
		{"disabled-task", "status", "disabled", "ok", false, true},
		{"disabled-trigger", "status", "disabled", "ok", false, true},
		{"correct", "status", "daemon", "ok", false, true},
		{"conflict", "status", "conflict", "ok", false, true},
		{"absent", "autostart set daemon", "daemon", "ok", true, true},
		{"disabled-task", "autostart set daemon", "daemon", "ok", true, true},
		{"disabled-trigger", "autostart set daemon", "daemon", "ok", true, true},
		{"legacy-settings", "autostart set daemon", "daemon", "ok", true, true},
		{"correct", "autostart set daemon", "daemon", "ok", false, true},
		{"conflict", "autostart set daemon", "conflict", "scheduled_task_conflict", false, false},
		{"verify-disabled", "autostart set daemon", "daemon", "scheduled_task_verify_failed", true, false},
		{"absent", "autostart set gui", "gui", "ok", true, true},
	} {
		t.Run(tc.scenario+"/"+tc.operation, func(t *testing.T) {
			// Quote a literal PowerShell string, not command interpolation.
			setup := strings.ReplaceAll(fixture, "%DIRECTORY%", "'"+strings.ReplaceAll(dir, "'", "''")+"'")
			setup = strings.ReplaceAll(setup, "%SCENARIO%", tc.scenario)
			input, err := json.Marshal(map[string]string{"directory": dir, "operation": tc.operation})
			if err != nil {
				t.Fatal(err)
			}
			output, err := invokePowerShell(functions+setup+handler, input)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				OK, Changed bool
				Code, Mode  string
			}
			if err := json.Unmarshal(output, &result); err != nil || result.OK != tc.success || result.Changed != tc.changed || result.Code != tc.code || result.Mode != tc.mode {
				t.Fatalf("native result %s: %v; want %#v", output, err, tc)
			}
		})
	}
}
