//go:build windows

package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The script is fixed, read-only and shipped inside the daemon. It reads its
// typed query from stdin only after the process has joined our kill-on-close job.
// No endpoint or OS error is interpolated into executable PowerShell text.
const networkTraceWindowsScript = `
[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false)
$ErrorActionPreference='Stop'
$q=ConvertFrom-Json ([Console]::In.ReadLine())
function Observe($body,$limit=0) {
 try { $value=@(& $body); $truncated=$limit -gt 0 -and $value.Count -gt $limit; if($truncated){$value=@($value | Select-Object -First $limit)}; return @{status='collected';systemCode=0;limit=$limit;truncated=$truncated;data=$value} }
 catch { $cause=$_.Exception; while($cause.InnerException) {$cause=$cause.InnerException}; $code=0; if($cause -is [System.ComponentModel.Win32Exception]) {$code=$cause.NativeErrorCode}; $status='unavailable'; $kind='other'; if($_.CategoryInfo.Category -eq 'ObjectNotFound'){$status='no_data';$kind='object_not_found'}; if($_.CategoryInfo.Category -in @('PermissionDenied','SecurityError')){$kind='permission_denied'}; if($_.CategoryInfo.Category -eq 'InvalidArgument'){$kind='invalid_argument'}; return @{status=$status;systemCode=$code;hresult=$cause.HResult;errorKind=$kind} }
}
$result=@{}
$result.udp=@{status='not_applicable';systemCode=0}
if($q.port -gt 0){ $result.udp=Observe {
 Get-NetUDPEndpoint -LocalPort $q.port -ErrorAction Stop | Select-Object LocalAddress,LocalPort,OwningProcess,@{Name='ProcessName';Expression={try{(Get-Process -Id $_.OwningProcess -ErrorAction Stop).ProcessName}catch{'unavailable'}}} | Select-Object -First 33
} 32 }
$result.excludedPorts=Observe {
 $process=[System.Diagnostics.Process]::new()
 try {
  $process.StartInfo.FileName=Join-Path $q.systemDirectory 'netsh.exe'
  $process.StartInfo.Arguments='interface ipv4 show excludedportrange protocol=udp'
  $process.StartInfo.UseShellExecute=$false
  $process.StartInfo.CreateNoWindow=$true
  $process.StartInfo.RedirectStandardOutput=$true
  $null=$process.Start()
  $buffer=New-Object char[] 2048; $text=[System.Text.StringBuilder]::new(); $truncated=$false
  while(($count=$process.StandardOutput.Read($buffer,0,$buffer.Length)) -gt 0) {
   $keep=[Math]::Min($count,32768-$text.Length); if($keep -gt 0){$null=$text.Append($buffer,0,$keep)}; if($keep -lt $count){$truncated=$true}
  }
  $process.WaitForExit()
  if($process.ExitCode -ne 0){throw [System.ComponentModel.Win32Exception]::new($process.ExitCode)}
  @{text=$text.ToString();truncated=$truncated}
 } finally {$process.Dispose()}
}
if($result.excludedPorts.status -eq 'collected' -and $result.excludedPorts.data[0].truncated){$result.excludedPorts.truncated=$true;$result.excludedPorts.status='truncated'}
$result.addresses=@{status='not_applicable';systemCode=0}
if($q.source){$result.addresses=Observe {
 Get-NetIPAddress -AddressFamily IPv4 -IPAddress $q.source -ErrorAction Stop | Select-Object IPAddress,InterfaceIndex,InterfaceAlias,PrefixLength,AddressState | Select-Object -First 17
} 16 }
$result.defaultRoute=@{status='not_applicable';systemCode=0}
if($q.destination){$result.defaultRoute=Observe {
 Find-NetRoute -RemoteIPAddress $q.destination -ErrorAction Stop | Select-Object IPAddress,InterfaceIndex,InterfaceAlias,DestinationPrefix,NextHop,RouteMetric | Select-Object -First 9
} 8 }
$result.route=@{status='not_applicable';systemCode=0}
if($q.destination){$result.route=Observe {
 $routeQuery=@{RemoteIPAddress=$q.destination;ErrorAction='Stop'}; if($q.source){$routeQuery.LocalIPAddress=$q.source}
 Find-NetRoute @routeQuery | Select-Object IPAddress,InterfaceIndex,InterfaceAlias,DestinationPrefix,NextHop,RouteMetric | Select-Object -First 9
} 8 }
$result.events=Observe {
 $start=[datetime]::Parse($q.triggeredAt).AddMinutes(-2)
 $end=Get-Date
 foreach($spec in @(
 @{log='Microsoft-Windows-NetworkProfile/Operational';ids=@(4004,10000,10001)},
 @{log='Microsoft-Windows-Windows Firewall With Advanced Security/Firewall';ids=@(2010,2097)},
 @{log='Security';ids=@(5152,5157)},
 @{log='Microsoft-Windows-WinNat/Oper';ids=@()}
 )) {
  $log=$spec.log
  try { $metadata=Get-WinEvent -ListLog $log -ErrorAction Stop; if(-not $metadata.IsEnabled){@{channel=$log;status='disabled'};continue} } catch {@{channel=$log;status='unavailable'};continue}
  $eventErrors=@()
  $filter=@{LogName=$log;StartTime=$start;EndTime=$end}; if($spec.ids.Count -gt 0){$filter.Id=$spec.ids}
  $pidPredicate="(Data[@Name='ProcessID']='"+$q.processId+"' or Data[@Name='ProcessId']='"+$q.processId+"' or Data[@Name='ProcessID']='"+('0x{0:x}' -f $q.processId)+"' or Data[@Name='ProcessId']='"+('0x{0:x}' -f $q.processId)+"')"
  $flowPredicate=$pidPredicate
  if($q.source -and $q.destination -and $q.port -gt 0 -and $q.destinationPort -gt 0) {
   $flowPredicate=$pidPredicate+" or (Data[@Name='SourceAddress']='"+$q.source+"' and Data[@Name='SourcePort']='"+$q.port+"' and Data[@Name='DestAddress']='"+$q.destination+"' and Data[@Name='DestPort']='"+$q.destinationPort+"')"
  }
  $securityXPath="*[System[(EventID=5152 or EventID=5157) and TimeCreated[@SystemTime >= '"+$start.ToUniversalTime().ToString('o')+"' and @SystemTime <= '"+$end.ToUniversalTime().ToString('o')+"']]] and *[EventData["+$flowPredicate+"]]"
  if($log -eq 'Security') {
   $events=@(Get-WinEvent -LogName $log -FilterXPath $securityXPath -MaxEvents 16 -ErrorAction SilentlyContinue -ErrorVariable eventErrors)
  } else {
   $events=@(Get-WinEvent -FilterHashtable $filter -MaxEvents 16 -ErrorAction SilentlyContinue -ErrorVariable eventErrors)
  }
  if($events.Count -eq 0) {
   $status='no_data'; if(@($eventErrors | Where-Object {$_.FullyQualifiedErrorId -notlike 'NoMatchingEventsFound*'}).Count -gt 0){$status='unavailable'; if(@($eventErrors | Where-Object {$_.CategoryInfo.Category -eq 'InvalidArgument'}).Count -gt 0){$status='query_failed'}}
   @{channel=$log;status=$status}; continue
  }
  @{channel=$log;status='collected';limit=16;limitReached=($events.Count -eq 16)}
  foreach($event in $events) {
   [xml]$xml=$event.ToXml(); $fields=@{}
   foreach($field in $xml.Event.EventData.Data) {
    if($field.Name -in @('TetheringOperationalStateChanged','TetheringClientCountChanged','NetworkConnectivityLevelChanged','InterfaceGuid','InterfaceName','NewProfile','OldProfile','RuleName','ServiceName','Action','Protocol','LocalPorts','ErrorCode','ProcessID','ProcessId','SourceAddress','SourcePort','DestAddress','DestPort','LayerName','FilterRTID')) {$fields[$field.Name]=$field.InnerText}
   }
   @{channel=$log;id=$event.Id;recordId=$event.RecordId;time=$event.TimeCreated.ToString('o');fields=$fields}
  }
 }
}
ConvertTo-Json -InputObject $result -Compress -Depth 7
`

func collectNetworkTrace(ctx context.Context, request networkTraceRequest) networkTraceResult {
	result := networkTraceResult{Status: "unavailable"}
	local, remote := request.Failure.LocalEndpoint, request.Failure.RemoteEndpoint
	source, destination := "", ""
	if local.IsValid() && local.Addr().Unmap().Is4() {
		source = local.Addr().Unmap().String()
	}
	if remote.IsValid() && remote.Addr().Unmap().Is4() {
		destination = remote.Addr().Unmap().String()
	}
	systemDirectory, err := windows.GetSystemDirectory()
	if err != nil {
		return result
	}
	query, err := json.Marshal(struct {
		ProcessID       int    `json:"processId"`
		Port            uint16 `json:"port"`
		DestinationPort uint16 `json:"destinationPort"`
		Source          string `json:"source"`
		Destination     string `json:"destination"`
		TriggeredAt     string `json:"triggeredAt"`
		SystemDirectory string `json:"systemDirectory"`
	}{os.Getpid(), local.Port(), remote.Port(), source, destination, request.TriggeredAt.Format(time.RFC3339Nano), systemDirectory})
	if err != nil {
		return result
	}
	output, status := runNetworkTracePowerShell(ctx, systemDirectory, query)
	if status != "collected" {
		result.Status = status
		return result
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.Facts); err != nil {
		result.Status = "invalid_output"
		return result
	}
	if decoder.Decode(new(any)) != io.EOF {
		result.Status = "invalid_output"
		return result
	}
	result.Status = "collected"
	return result
}

func runNetworkTracePowerShell(ctx context.Context, systemDirectory string, query []byte) ([]byte, string) {
	if ctx.Err() != nil {
		return nil, "cancelled_or_timed_out"
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, "job_unavailable"
	}
	defer windows.CloseHandle(job)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, "job_unavailable"
	}
	script := utf16.Encode([]rune(networkTraceWindowsScript))
	encoded := make([]byte, len(script)*2)
	for i, ch := range script {
		binary.LittleEndian.PutUint16(encoded[i*2:], ch)
	}
	cmd := exec.CommandContext(ctx, filepath.Join(systemDirectory, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Cancel = func() error { return windows.TerminateJobObject(job, 1) }
	output := &boundedTraceOutput{limit: networkTraceOutputLimit}
	stderr := &boundedTraceOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = output, stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, "start_failed"
	}
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		return nil, "start_failed"
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err != nil {
		_ = input.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return nil, "cancelled_or_timed_out"
		}
		return nil, "job_unavailable"
	}
	if ctx.Err() != nil {
		_ = windows.TerminateJobObject(job, 1)
	}
	_, writeErr := input.Write(append(query, '\n'))
	_ = input.Close()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, "cancelled_or_timed_out"
	}
	if output.overflow {
		return nil, "output_truncated"
	}
	if writeErr != nil || err != nil {
		return nil, "query_failed"
	}
	return output.data, "collected"
}
