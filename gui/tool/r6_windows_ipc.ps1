param(
  [Parameter(Mandatory=$true)][string]$OwnedRoot,
  [string]$Flutter = 'D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat',
  [ValidateSet('All','Smoke','Exit','Integration')][string]$Stage = 'All'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$OwnedRoot = [IO.Path]::GetFullPath($OwnedRoot)
if (!(Test-Path (Join-Path $OwnedRoot 'R6-OWNED.txt'))) { throw 'Missing owned sandbox marker' }
if ((Get-Content (Join-Path $OwnedRoot 'R6-OWNED.txt') -Raw).Trim() -ne 'sidravia-r6-20261006') { throw 'Wrong sandbox owner' }
$Gui = Join-Path $OwnedRoot 'gui'
$Evidence = Join-Path $OwnedRoot 'evidence'
New-Item -ItemType Directory -Force $Evidence | Out-Null
$previousNamespace = $env:SIDRAVIA_NAMESPACE
$previousPathExt = $env:PATHEXT
$env:PATHEXT = '.COM;.EXE;.BAT;.CMD'

function Invoke-Flutter([string[]]$Arguments, [string]$Name) {
  Push-Location $Gui
  try {
    & 'C:\Windows\System32\cmd.exe' /d /c $Flutter @Arguments 2>&1 | Tee-Object -FilePath (Join-Path $Evidence "$Name.log") | Out-Host
    $code = $LASTEXITCODE
    if ($code -ne 0) { throw "Flutter $Name exit $code" }
  } finally { Pop-Location }
}
function Prepare-Bundle([string]$Bundle, [string]$Namespace) {
  Copy-Item (Join-Path $OwnedRoot 'sidraviactl.exe') $Bundle
  Copy-Item (Join-Path $OwnedRoot 'sidraviad.exe') $Bundle
  [IO.File]::WriteAllText((Join-Path $Bundle 'sidravia.portable'), '')
  $state = Join-Path $Bundle "namespaces\$Namespace"
  if (Test-Path $state) { throw 'Refusing pre-existing namespace' }
  New-Item -ItemType Directory (Join-Path $state 'institution-profiles') -Force | Out-Null
  Copy-Item (Join-Path $OwnedRoot 'r6-loopback.json') (Join-Path $state 'institution-profiles\r6-loopback.json')
  foreach ($path in @('config\configurations.json','config\gui-settings.json','runtime\runtime.json')) {
    $sentinel = Join-Path $Bundle $path
    if (!(Test-Path $sentinel)) {
      New-Item -ItemType Directory -Force ([IO.Path]::GetDirectoryName($sentinel)) | Out-Null
      [IO.File]::WriteAllText($sentinel, 'r6-owned-production-sentinel')
    }
    if ([IO.File]::ReadAllText($sentinel) -ne 'r6-owned-production-sentinel') { throw 'Production-shaped sentinel changed' }
  }
}
function Wait-Condition([scriptblock]$Condition, [string]$Label) {
  $deadline = [DateTime]::UtcNow.AddSeconds(20)
  while (!(& $Condition)) {
    if ([DateTime]::UtcNow -gt $deadline) { throw "Condition timed out: $Label" }
    # A bounded condition wait, not a fixed startup sleep.
    [Threading.Thread]::Sleep(25)
  }
}
function Invoke-Ipc($Info, [string]$Method) {
  $socket = New-Object Net.WebSockets.ClientWebSocket
  $socket.Options.SetRequestHeader('Authorization', "Bearer $($Info.token)")
  $socket.Options.SetRequestHeader('Sidravia-Build-ID', $Info.buildId)
  $cancel = New-Object Threading.CancellationTokenSource
  $cancel.CancelAfter(5000)
  try {
    $socket.ConnectAsync([Uri]$Info.endpoint, $cancel.Token).GetAwaiter().GetResult() | Out-Null
    $bytes = [Text.Encoding]::UTF8.GetBytes((@{kind='request';id='r6-smoke';method=$Method;payload=@{}} | ConvertTo-Json -Compress))
    $socket.SendAsync([ArraySegment[byte]]::new($bytes), [Net.WebSockets.WebSocketMessageType]::Text, $true, $cancel.Token).GetAwaiter().GetResult() | Out-Null
    $buffer = New-Object byte[] 65536
    $stream = New-Object IO.MemoryStream
    do {
      $reply = $socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), $cancel.Token).GetAwaiter().GetResult()
      if ($reply.MessageType -ne [Net.WebSockets.WebSocketMessageType]::Text) { throw 'Non-text IPC response' }
      $stream.Write($buffer, 0, $reply.Count)
      if ($stream.Length -gt 65536) { throw 'IPC response too large' }
    } while (!$reply.EndOfMessage)
    $value = [Text.Encoding]::UTF8.GetString($stream.ToArray()) | ConvertFrom-Json
    if ($value.id -ne 'r6-smoke' -or !$value.ok) { throw 'IPC request rejected' }
    return $value.result
  } finally {
    $socket.Abort(); $socket.Dispose(); $cancel.Dispose()
  }
}
function Run-ReleaseSmoke([string]$Bundle, [string]$Namespace) {
  $env:SIDRAVIA_NAMESPACE = $Namespace
  Prepare-Bundle $Bundle $Namespace
  $state = Join-Path $Bundle "namespaces\$Namespace"
  $runtime = Join-Path $state 'runtime.json'
  $program = Join-Path $Bundle 'Sidravia.exe'
  $passes = @()
  $daemon = $null
  $owner = $null
  try {
    for ($generation=1; $generation -le 2; $generation++) {
      $owner = Start-Process -FilePath $program -PassThru
      Wait-Condition { Test-Path $runtime } 'release daemon runtime ready'
      $info = Get-Content $runtime -Raw | ConvertFrom-Json
      $status = Invoke-Ipc $info 'daemon.status'
      if ($status.mode -ne 'desktop' -or $status.desktopOwnerPid -ne $owner.Id -or $status.pid -ne $info.pid) { throw 'Wrong desktop owner or daemon identity' }
      $daemon = Get-Process -Id $status.pid
      if ([IO.Path]::GetFullPath($daemon.Path) -ne [IO.Path]::GetFullPath((Join-Path $Bundle 'sidraviad.exe'))) { throw 'Unowned daemon process' }
      $profiles = Invoke-Ipc $info 'profile.list'
      $configurations = Invoke-Ipc $info 'configuration.list'
      $sessions = Invoke-Ipc $info 'session.list'
      if ($profiles.profiles.Count -ne 1 -or $profiles.profiles[0].institutionProfileId -ne 'r6-loopback' -or $configurations.configurations.Count -ne 0 -or $sessions.sessions.Count -ne 0) { throw 'Release snapshot namespace mismatch' }
      $second = Start-Process -FilePath $program -PassThru
      if (!$second.WaitForExit(10000) -or $second.ExitCode -ne 0) { throw 'Second GUI failed to activate existing instance' }
      $again = Invoke-Ipc $info 'daemon.status'
      if ($again.pid -ne $daemon.Id -or $again.desktopOwnerPid -ne $owner.Id) { throw 'Second GUI changed daemon lifetime' }
      $passes += @{generation=$generation;guiPid=$owner.Id;daemonPid=$daemon.Id;version=$status.productVersion;build=$status.buildId;mode=$status.mode;realIPC=$true;secondInstance=$true}
      # Own exact process object only; exercises daemon's GUI crash watcher.
      Stop-Process -InputObject $owner
      if (!$owner.WaitForExit(10000) -or !$daemon.WaitForExit(20000)) { throw 'Owned process exit failed' }
      Wait-Condition { !(Test-Path $runtime) } 'owner crash runtime cleanup'
      $owner=$null; $daemon=$null
    }
    $passes | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $Evidence 'release-smoke.json') -Encoding UTF8
    Write-Output 'R6 PASS release GUI real IPC, second instance, owner crash, GUI restart'
  } catch {
    Write-Output ('R6 smoke failed at line ' + $_.InvocationInfo.ScriptLineNumber + ': ' + $_.Exception.Message)
    throw
  } finally {
    if ($null -ne $owner -and !$owner.HasExited) { Stop-Process -InputObject $owner; $owner.WaitForExit(10000) | Out-Null }
    if ($null -ne $daemon -and !$daemon.HasExited) {
      if (!$daemon.WaitForExit(20000)) { Stop-Process -InputObject $daemon; $daemon.WaitForExit(10000) | Out-Null }
    }
    Wait-Condition { !(Test-Path $runtime) } 'finally owner watcher cleanup'
    if (Test-Path $runtime) { throw 'Runtime residue: preserving namespace for diagnosis' }
    Remove-Item $state -Recurse -Force
  }
}
function Assert-Sentinels([string]$Bundle) {
  foreach ($path in @('config\configurations.json','config\gui-settings.json','runtime\runtime.json')) {
    if ([IO.File]::ReadAllText((Join-Path $Bundle $path)) -ne 'r6-owned-production-sentinel') { throw 'Namespace touched production-shaped sentinel' }
  }
}
function Run-ExitSmoke {
  $release = Join-Path $Gui 'build\windows\x64\runner\Release'
  $preserved = Join-Path $OwnedRoot 'production-release'
  if (!(Test-Path $preserved)) { Copy-Item $release $preserved -Recurse }
  Invoke-Flutter @('build','windows','--release','-t','tool/r6_exit_smoke.dart') 'exit-harness-build'
  $env:SIDRAVIA_NAMESPACE = 'r6-exit-20261006'
  Prepare-Bundle $release $env:SIDRAVIA_NAMESPACE
  $state = Join-Path $release 'namespaces\r6-exit-20261006'
  $runtime = Join-Path $state 'runtime.json'
  $owner = $null
  try {
    $owner = Start-Process -FilePath (Join-Path $release 'Sidravia.exe') -PassThru
    if (!$owner.WaitForExit(60000)) { throw 'Explicit exit GUI deadline' }
    if ($owner.ExitCode -ne 0) { throw 'Explicit exit GUI nonzero' }
    Wait-Condition { !(Test-Path $runtime) } 'explicit exit daemon runtime cleanup'
    $marker = Get-Content (Join-Path $state 'r6-exit-marker.json') -Raw | ConvertFrom-Json
    if ($marker.guiPid -ne $owner.Id -or !$marker.pollAfterClose -or !$marker.sameDaemonAfterRestore -or $marker.secondInstanceExit -ne 0) { throw 'Lifecycle marker mismatch' }
    Wait-Condition { $null -eq (Get-Process -Id $marker.daemonPid -ErrorAction SilentlyContinue) } 'explicit exit daemon process cleanup'
    Assert-Sentinels $release
    $marker | Add-Member -NotePropertyName guiExitCode -NotePropertyValue $owner.ExitCode
    $marker | Add-Member -NotePropertyName daemonExited -NotePropertyValue $true
    $marker | Add-Member -NotePropertyName namespaceSentinelsUnchanged -NotePropertyValue $true
    $marker | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $Evidence 'native-exit.json') -Encoding UTF8
    Write-Output 'R6 PASS native close-to-tray path, IPC continuity, restore, actual explicit exit callback and native destruction'
  } finally {
    if ($null -ne $owner -and !$owner.HasExited) { Stop-Process -InputObject $owner; $owner.WaitForExit(10000) | Out-Null }
    Wait-Condition { !(Test-Path $runtime) } 'exit harness finally cleanup'
    Remove-Item $state -Recurse -Force
  }
}

try {
  if ($Stage -eq 'All' -or $Stage -eq 'Integration') {
  Invoke-Flutter @('pub','get') 'pub-get'
  Invoke-Flutter @('build','windows','--debug','-t','lib/main.dart') 'debug-build'
  $debug = Join-Path $Gui 'build\windows\x64\runner\Debug'
  $env:SIDRAVIA_NAMESPACE = 'r6-local-20261006'
  Prepare-Bundle $debug $env:SIDRAVIA_NAMESPACE
  Invoke-Flutter @('test','integration_test/r6_production_test.dart','-d','windows') 'production-integration'
  if (Test-Path (Join-Path $debug 'namespaces\r6-local-20261006')) { throw 'Integration namespace residue' }
  Assert-Sentinels $debug
  if ($Stage -eq 'All') { Invoke-Flutter @('build','windows','--release','-t','lib/main.dart') 'release-build' }
  }
  if ($Stage -eq 'All' -or $Stage -eq 'Smoke') {
    $bundle = Join-Path $Gui 'build\windows\x64\runner\Release'
    if ($Stage -eq 'Smoke' -and (Test-Path (Join-Path $OwnedRoot 'production-release'))) {
      $bundle = Join-Path $OwnedRoot 'production-release'
    }
    Run-ReleaseSmoke $bundle 'r6-release-20261006-r3'
    Assert-Sentinels $bundle
  }
  if ($Stage -eq 'All' -or $Stage -eq 'Exit') { Run-ExitSmoke }
} finally { $env:SIDRAVIA_NAMESPACE = $previousNamespace; $env:PATHEXT = $previousPathExt }
