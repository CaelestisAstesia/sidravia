# Isolated helper safety checks. Fake compilers fail before product compilation.
param([string]$Helper = (Join-Path $PSScriptRoot 'build_windows.ps1'))
$ErrorActionPreference = 'Stop'
$root = Join-Path ([IO.Path]::GetTempPath()) ('sidravia-helper-test-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($root) | Out-Null
$source = Join-Path $root 'input'
$workspace = Join-Path $root 'workspace'
$manifestPath = Join-Path $root 'manifest.json'
$fake = Join-Path $root 'compiler.cmd'
[IO.File]::WriteAllText($fake, "@echo off`r`nexit /b 7`r`n")
$required = @('go.mod','gui/pubspec.yaml','gui/pubspec.lock','gui/lib/main.dart','tools/build/main.go','tools/developer/verify_distribution.py')
$items = @()
foreach ($name in ($required + @('stale.go','zz-invalid.go'))) {
    $path = Join-Path $source $name
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($path)) | Out-Null
    [IO.File]::WriteAllText($path,'fixture')
    $items += @{path=$name;sha256=(Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()}
}
function Save-Manifest($Files) {
    @{schema=1;source_commit=('a'*40);dirty=$true;files=@($Files)} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestPath -Encoding UTF8
}
function Invoke-Helper([string]$Destination, [int]$TimeoutSeconds = 900) {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = Join-Path $PSHOME 'powershell.exe'
    $info.Arguments = '-NoProfile -ExecutionPolicy Bypass -File "' + $Helper + '" -SourceRoot "' + $source + '" -ManifestPath "' + $manifestPath + '" -Workspace "' + $Destination + '" -Flutter "' + $fake + '" -Go "' + $fake + '" -StageTimeoutSeconds ' + $TimeoutSeconds
    $info.UseShellExecute=$false
    $info.CreateNoWindow=$true
    $info.RedirectStandardOutput=$true
    $info.RedirectStandardError=$true
    $p=[Diagnostics.Process]::Start($info)
    try {
        $out=$p.StandardOutput.ReadToEndAsync();$err=$p.StandardError.ReadToEndAsync()
        if (!$p.WaitForExit(30000)) { $p.Kill();throw 'Fixture helper timeout' }
        if ($p.ExitCode -ne 1) { throw ('Expected fixture failure: ' + $out.Result + $err.Result) }
    } finally { $p.Dispose() }
}
try {
    $items[-1].sha256='0'*64
    Save-Manifest $items
    Invoke-Helper $workspace
    if (!(Test-Path -LiteralPath (Join-Path $workspace 'source/stale.go'))) { throw 'Fixture did not reach partial source copy' }
    Save-Manifest ($items | Where-Object { $_.path -in $required })
    Invoke-Helper $workspace
    if (Test-Path -LiteralPath (Join-Path $workspace 'source/stale.go')) { throw 'Failed sync leaked stale source into next run' }
    Write-Output 'PASS failed-sync stale source recovery'

    foreach ($fixture in @('timeout','inherited-pipe')) {
        $childPid = Join-Path $root ($fixture + '.pid')
        $sleeper = Join-Path $root ($fixture + '.ps1')
        [IO.File]::WriteAllText($sleeper, ("[IO.File]::WriteAllText('{0}', [string]`$PID); Start-Sleep -Seconds 60" -f $childPid))
        $launch = 'powershell.exe -NoProfile -ExecutionPolicy Bypass -File "' + $sleeper + '"'
        if ($fixture -eq 'inherited-pipe') { $launch = 'start "" /b ' + $launch }
        [IO.File]::WriteAllText($fake, ("@echo off`r`necho preserved-before-wait`r`n" + $launch + "`r`nexit /b 0`r`n"))
        $timer = [Diagnostics.Stopwatch]::StartNew()
        Invoke-Helper $workspace 2
        if ($timer.Elapsed.TotalSeconds -gt 20) { throw 'Build timeout did not return within bound' }
        $latest = Get-ChildItem -LiteralPath (Join-Path $workspace 'runs') -Directory | Sort-Object Name | Select-Object -Last 1
        $result = Get-Content -LiteralPath (Join-Path $latest.FullName 'report.json') -Raw | ConvertFrom-Json
        $expected = if ($fixture -eq 'timeout') { 'timed out' } else { 'output did not close' }
        if ($result.status -ne 'failed' -or $result.error -notmatch $expected) { throw ('Missing bounded failure report: ' + $result.error) }
        $stage = @($result.stages)[-1]
        if ($stage.exit_code -eq 0 -or (Get-Content -LiteralPath $stage.log -Raw) -notmatch 'preserved-before-wait') { throw 'Timeout lost log or failure status' }
        if (!(Test-Path -LiteralPath $childPid)) { throw 'Descendant fixture did not start' }
        $descendant = Get-Process -Id ([int][IO.File]::ReadAllText($childPid)) -ErrorAction SilentlyContinue
        if ($descendant) { throw 'Owned descendant survived helper exit' }
        Write-Output ('PASS bounded ' + $fixture + ', retained log and owned descendant cleanup')
    }
    [IO.File]::WriteAllText($fake, "@echo off`r`nexit /b 7`r`n")

    $foreign = Join-Path $root 'foreign'
    [IO.Directory]::CreateDirectory($foreign) | Out-Null
    $sentinel = Join-Path $foreign 'keep.txt'
    [IO.File]::WriteAllText($sentinel,'keep')
    Invoke-Helper $foreign
    if ([IO.File]::ReadAllText($sentinel) -cne 'keep' -or (Test-Path (Join-Path $foreign '.sidravia-build-owner'))) { throw 'Unowned workspace was modified' }
    Write-Output 'PASS unowned workspace refusal'

    $junction = Join-Path $workspace 'cache'
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = Join-Path $env:SystemRoot 'System32/cmd.exe'
    $info.Arguments = '/d /c mklink /J "' + $junction + '" "' + $foreign + '"'
    $info.UseShellExecute=$false;$info.CreateNoWindow=$true
    $p=[Diagnostics.Process]::Start($info);$p.WaitForExit();$code=$p.ExitCode;$p.Dispose()
    if ($code -ne 0) { throw 'Could not prepare isolated junction fixture' }
    try {
        Invoke-Helper $workspace
        if ([IO.File]::ReadAllText($sentinel) -cne 'keep') { throw 'Junction target modified' }
    } finally { [IO.Directory]::Delete($junction) }
    Write-Output 'PASS linked build directory refusal'
} finally {
    # Only this invocation's GUID directory; junction is removed separately.
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
