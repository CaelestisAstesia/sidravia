//go:build windows

package installationmanager

import (
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
