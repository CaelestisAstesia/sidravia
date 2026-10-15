param(
    [Parameter(Mandatory=$true)][string]$SourceRoot,
    [Parameter(Mandatory=$true)][string]$ManifestPath,
    [Parameter(Mandatory=$true)][string]$Workspace,
    [Parameter(Mandatory=$true)][string]$Flutter,
    [Parameter(Mandatory=$true)][string]$Go,
    [string]$Python = '',
    [string]$ISCC = '',
    [string]$Version = '0.3.0-alpha.1',
    [switch]$Package,
    [switch]$CheckGUI,
    [ValidateRange(1,86400)][int]$StageTimeoutSeconds = 900
)

# Persistent, exclusively owned NTFS build workspace. Never starts the product.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$OutputEncoding = [Console]::OutputEncoding
$Workspace = [IO.Path]::GetFullPath($Workspace)
if ($Workspace -notmatch '^[A-Za-z]:\\.+') { throw 'Workspace must be a dedicated local drive directory' }
$ancestor = $Workspace
while ($ancestor) {
    if ((Test-Path -LiteralPath $ancestor) -and ((Get-Item -LiteralPath $ancestor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Workspace ancestry must not contain reparse points' }
    $ancestor = [IO.Path]::GetDirectoryName($ancestor)
}
if (Test-Path -LiteralPath $Workspace) {
    if ((Get-Item -LiteralPath $Workspace).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Workspace must not be a reparse point' }
} else { [IO.Directory]::CreateDirectory($Workspace) | Out-Null }
$marker = Join-Path $Workspace '.sidravia-build-owner'
if (!(Test-Path -LiteralPath $marker)) {
    if (@(Get-ChildItem -LiteralPath $Workspace -Force).Count -ne 0) { throw 'Refusing an existing unowned workspace' }
    [IO.File]::WriteAllText($marker, 'sidravia-build-v1')
}
if ([IO.File]::ReadAllText($marker) -cne 'sidravia-build-v1') { throw 'Workspace ownership marker mismatch' }
# Flutter owns plugin source links under this exact generated directory. All
# other linked objects could redirect our cache, logs or compiler writes.
$pendingDirectories = New-Object 'System.Collections.Generic.Stack[string]'
$pendingDirectories.Push($Workspace)
while ($pendingDirectories.Count) {
    $directory = $pendingDirectories.Pop()
    foreach ($entry in Get-ChildItem -LiteralPath $directory -Force) {
        if ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            $pluginRoot = Join-Path $Workspace 'source\gui\windows\flutter\ephemeral\.plugin_symlinks'
            if ($entry.PSIsContainer -and [IO.Path]::GetDirectoryName($entry.FullName) -eq $pluginRoot) { continue }
            throw ('Unexpected linked build object: ' + $entry.FullName)
        }
        if ($entry.PSIsContainer) { $pendingDirectories.Push($entry.FullName) }
    }
}
$lock = [IO.File]::Open((Join-Path $Workspace '.build.lock'), [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
$started = [DateTime]::UtcNow
$watch = [Diagnostics.Stopwatch]::StartNew()
$runName = $started.ToString('yyyyMMddTHHmmssfff') + '-' + $PID
$runRoot = Join-Path $Workspace ('runs\' + $runName)
$repo = Join-Path $Workspace 'source'
$report = [ordered]@{ schema=1; started_utc=$started.ToString('o'); status='failed'; workspace=$Workspace; source_commit=$null; dirty=$null; manifest_sha256=$null; stages=@(); total_seconds=0; error=$null; package=[bool]$Package; gui_checks=[bool]$CheckGUI }

function Get-OwnedPath([string]$Root, [string]$Relative) {
    if (!$Relative -or $Relative -match '(^/|\\|:|(^|/)\.\.?(/|$)|^\.git(/|$)|^\.project(/|$)|^\.codex(/|$)|^\.agents(/|$))') { throw ('Unsafe source path: ' + $Relative) }
    $full = [IO.Path]::GetFullPath((Join-Path $Root $Relative))
    $prefix = [IO.Path]::GetFullPath($Root).TrimEnd('\') + '\'
    if (!$full.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { throw 'Source path escaped root' }
    $part = $full
    while ($part.Length -ge $Root.Length) {
        if ((Test-Path -LiteralPath $part) -and ((Get-Item -LiteralPath $part -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw ('Refusing linked source path: ' + $Relative) }
        $parent = [IO.Path]::GetDirectoryName($part)
        if (!$parent -or $parent -eq $part) { break }
        $part = $parent
    }
    return $full
}

function Invoke-Stage([string]$Name, [string]$Command, [string[]]$Arguments) {
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $log = Join-Path $runRoot ($Name + '.log')
    Write-Host ('Running ' + $Name)
    $code = 1
    try {
        $info = [Diagnostics.ProcessStartInfo]::new()
        $info.FileName = $Command
        $quoted = @($Arguments | ForEach-Object {
            '"' + [regex]::Replace([regex]::Replace($_, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
        }) -join ' '
        $info.Arguments = $quoted
        if ([IO.Path]::GetExtension($Command) -in @('.bat','.cmd')) {
            $info.FileName = Join-Path $env:SystemRoot 'System32\cmd.exe'
            $info.Arguments = '/d /s /c ""' + $Command + '" ' + $quoted + '"'
        }
        $info.WorkingDirectory = $repo
        if ($Name -in @('dependencies','analyze','tests','gui-release','flutter-version')) { $info.WorkingDirectory = Join-Path $repo 'gui' }
        $info.UseShellExecute = $false
        $info.CreateNoWindow = $true
        $info.RedirectStandardOutput = $true
        $info.RedirectStandardError = $true
        # WSL can supply duplicate case-variant PATH and Linux Go settings.
        # Construct native build environment without changing user/system state.
        foreach ($key in @($info.EnvironmentVariables.Keys)) {
            if ($key -imatch '^(PATH|PATHEXT|GOROOT|GOCACHE|GOOS|GOARCH|GOFLAGS|GOWORK|GOENV|GOTOOLCHAIN|TMPDIR|PUB_CACHE)$') { $info.EnvironmentVariables.Remove($key) }
        }
        $info.EnvironmentVariables['PATH'] = (Split-Path -Parent $Go) + ';C:\Windows\System32;C:\Windows;' + [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
        $info.EnvironmentVariables['PATHEXT'] = '.COM;.EXE;.BAT;.CMD'
        $info.EnvironmentVariables['GOCACHE'] = $env:GOCACHE
        $info.EnvironmentVariables['PUB_CACHE'] = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Pub\Cache'
        $process = [Diagnostics.Process]::new()
        $process.StartInfo = $info
        $processStarted = $false
        try {
            $processStarted = $process.Start()
            if (!$processStarted) { throw 'Build process did not start' }
            # Stream immediately to separate files: do not retain build output in
            # memory or wait indefinitely for inherited pipe handles to close.
            $stdoutFile = [IO.FileStream]::new($log, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::Read, 1, $true)
            $stderrLog = Join-Path $runRoot ($Name + '.stderr.log')
            $stderrFile = [IO.FileStream]::new($stderrLog, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::Read, 1, $true)
            try {
                $stdout = $process.StandardOutput.BaseStream.CopyToAsync($stdoutFile)
                $stderr = $process.StandardError.BaseStream.CopyToAsync($stderrFile)
                if (!$process.WaitForExit($StageTimeoutSeconds * 1000)) { throw ('Build stage timed out: ' + $Name) }
                $code = $process.ExitCode
                if (![Threading.Tasks.Task]::WaitAll([Threading.Tasks.Task[]]@($stdout,$stderr), 5000)) {
                    $code = 1
                    throw ('Build stage output did not close: ' + $Name)
                }
            } finally {
                # Never await descendants here. The process-wide job kills the
                # owned tree at helper exit, after the failure report is saved.
                $stdoutFile.Dispose()
                $stderrFile.Dispose()
            }
        } finally {
            $process.Dispose()
        }
    } catch {
        $code = 1
        throw
    } finally {
        $report.stages += [ordered]@{ name=$Name; seconds=[Math]::Round($timer.Elapsed.TotalSeconds,3); exit_code=$code; log=$log; stderr_log=(Join-Path $runRoot ($Name + '.stderr.log')) }
    }
    if ($code -ne 0) { throw ($Name + ' failed (exit ' + $code + '); see ' + $log) }
    Write-Host ($Name + ': ' + [Math]::Round($timer.Elapsed.TotalSeconds,3) + 's')
}

try {
    [IO.Directory]::CreateDirectory($runRoot) | Out-Null
    [IO.Directory]::CreateDirectory($repo) | Out-Null
    # Non-inheritable job handle lives until this dedicated PowerShell process
    # exits. Assign ourselves BEFORE spawning tools: no child-assignment race.
    # Do not close it in finally: that would kill us before writing report.json.
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public static class SidraviaBuildJob {
    static IntPtr job;
    [StructLayout(LayoutKind.Sequential)] struct BasicLimits {
        public long PerProcessTime, PerJobTime;
        public uint Flags;
        public UIntPtr MinimumWorkingSet, MaximumWorkingSet;
        public uint ActiveProcesses;
        public UIntPtr Affinity;
        public uint Priority, Scheduling;
    }
    [StructLayout(LayoutKind.Sequential)] struct IoCounters {
        public ulong ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes;
    }
    [StructLayout(LayoutKind.Sequential)] struct Limits {
        public BasicLimits Basic;
        public IoCounters Io;
        public UIntPtr ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory;
    }
    [DllImport("kernel32.dll", SetLastError=true)] static extern IntPtr CreateJobObject(IntPtr attrs, string name);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool SetInformationJobObject(IntPtr job, int kind, ref Limits info, uint length);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);
    [DllImport("kernel32.dll")] static extern IntPtr GetCurrentProcess();
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    public static void Ensure() {
        if (job != IntPtr.Zero) return;
        IntPtr candidate = CreateJobObject(IntPtr.Zero, null);
        if (candidate == IntPtr.Zero) throw new Win32Exception();
        var info = new Limits(); info.Basic.Flags = 0x2000;
        if (!SetInformationJobObject(candidate, 9, ref info, (uint)Marshal.SizeOf(info)) ||
            !AssignProcessToJobObject(candidate, GetCurrentProcess())) {
            int error = Marshal.GetLastWin32Error(); CloseHandle(candidate);
            throw new Win32Exception(error);
        }
        job = candidate;
    }
}
'@
    [SidraviaBuildJob]::Ensure()
    $manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
    if ($manifest.schema -ne 1 -or $manifest.source_commit -notmatch '^[0-9a-f]{40}$' -or $manifest.dirty -isnot [bool]) { throw 'Invalid source manifest' }
    if ($Package -and $manifest.dirty) { throw 'Packaging requires a clean source commit' }
    $report.source_commit = $manifest.source_commit
    $report.dirty = $manifest.dirty
    $report.manifest_sha256 = (Get-FileHash -LiteralPath $ManifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
    foreach ($tool in @($Flutter, $Go)) { if (!(Test-Path -LiteralPath $tool -PathType Leaf)) { throw ('Tool not found: ' + $tool) } }
    if ($Package) { foreach ($tool in @($Python, $ISCC)) { if (!$tool -or !(Test-Path -LiteralPath $tool -PathType Leaf)) { throw ('Package tool not found: ' + $tool) } } }
    $sync = [Diagnostics.Stopwatch]::StartNew()
    $owned = Join-Path $Workspace 'source-manifest.json'
    $next = @{}
    foreach ($item in $manifest.files) {
        if ($item.sha256 -notmatch '^[0-9a-f]{64}$' -or $next.ContainsKey($item.path)) { throw 'Invalid or duplicate source entry' }
        $next[$item.path] = $item.sha256
        $null = Get-OwnedPath $repo $item.path
    }
    foreach ($required in @('go.mod', 'gui/pubspec.yaml', 'gui/pubspec.lock', 'gui/lib/main.dart', 'tools/build/main.go', 'tools/developer/verify_distribution.py')) {
        if (!$next.ContainsKey($required)) { throw ('Source manifest missing ' + $required) }
    }
    $previousFiles = @()
    if (Test-Path -LiteralPath $owned) {
        $previous = Get-Content -LiteralPath $owned -Raw | ConvertFrom-Json
        $previousFiles = @($previous.files)
    }
    # Write ownership BEFORE copies/removals. A failed sync may have introduced
    # new source files; the next run must know to remove those too.
    $union = @{}
    foreach ($item in $previousFiles) {
        $null = Get-OwnedPath $repo $item.path
        $union[$item.path] = $item
    }
    foreach ($item in $manifest.files) { $union[$item.path] = $item }
    $journal = Join-Path $Workspace 'source-manifest.pending.json'
    @{ files=@($union.Values) } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $journal -Encoding UTF8
    if (Test-Path -LiteralPath $owned) { [IO.File]::Replace($journal,$owned,($owned + '.previous')) }
    else { [IO.File]::Move($journal,$owned) }
    foreach ($item in $previousFiles) {
            $target = Get-OwnedPath $repo $item.path
            if (!$next.ContainsKey($item.path) -and (Test-Path -LiteralPath $target -PathType Leaf)) { Remove-Item -LiteralPath $target }
    }
    $copied = 0
    foreach ($item in $manifest.files) {
        $target = Get-OwnedPath $repo $item.path
        if ((Test-Path -LiteralPath $target -PathType Leaf) -and (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant() -eq $item.sha256) { continue }
        [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target)) | Out-Null
        Copy-Item -LiteralPath (Join-Path $SourceRoot $item.path) -Destination $target
        if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant() -cne $item.sha256) { throw ('Source changed during sync: ' + $item.path) }
        $copied++
    }
    Copy-Item -LiteralPath $ManifestPath -Destination $journal
    [IO.File]::Replace($journal,$owned,($owned + '.previous'))
    Copy-Item -LiteralPath $ManifestPath -Destination (Join-Path $runRoot 'source-manifest.json')
    $report.stages += [ordered]@{ name='source-sync'; seconds=[Math]::Round($sync.Elapsed.TotalSeconds,3); exit_code=0; copied_files=$copied; source_files=@($manifest.files).Count }
    $env:GOCACHE = Join-Path $Workspace 'cache\go'
    $env:PYTHONDONTWRITEBYTECODE = '1'
    $report.go_cache = $env:GOCACHE
    Invoke-Stage 'go-version' $Go @('version')
    Invoke-Stage 'flutter-version' $Flutter @('--version','--machine')
    $report.go_version = (Get-Content -LiteralPath (Join-Path $runRoot 'go-version.log') -Raw).Trim()
    $report.flutter_version = Get-Content -LiteralPath (Join-Path $runRoot 'flutter-version.log') -Raw | ConvertFrom-Json
    Push-Location (Join-Path $repo 'gui')
    try {
        Invoke-Stage 'dependencies' $Flutter @('pub','get','--enforce-lockfile')
        if ($CheckGUI) {
            Invoke-Stage 'analyze' $Flutter @('analyze','--no-pub')
            Invoke-Stage 'tests' $Flutter @('test','--no-pub')
        }
        Invoke-Stage 'gui-release' $Flutter @('build','windows','--release','--no-pub','--target','lib/main.dart','--build-name',$Version,'--build-number','1')
    } finally { Pop-Location }
    $gui = Join-Path $repo 'gui\build\windows\x64\runner\Release'
    $exe = Join-Path $gui 'Sidravia.exe'
    if (!(Test-Path -LiteralPath $exe)) { throw 'GUI executable missing after successful build' }
    if ((Get-Item -LiteralPath $exe).VersionInfo.ProductVersion -cne ($Version + '+1')) { throw 'GUI product version mismatch' }
    $report.gui_directory = $gui
    $report.gui_sha256 = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Package) {
        foreach ($item in $manifest.files) {
            $target = Get-OwnedPath $repo $item.path
            if (!(Test-Path -LiteralPath $target -PathType Leaf) -or (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant() -cne $item.sha256) { throw ('Staged source changed during build: ' + $item.path) }
        }
        $out = Join-Path $runRoot 'distribution'
        Push-Location $repo
        try {
            Invoke-Stage 'package' $Go @('run','-mod=readonly','./tools/build','--version',$Version,'--build-id',$manifest.source_commit,'--artifact','distribution','--target','windows-amd64','--output',$out,'--go',$Go,'--gui-directory',$gui,'--iscc',$ISCC)
            Invoke-Stage 'consume' $Python @('tools/developer/verify_distribution.py','--directory',$out,'--target','windows-amd64','--version',$Version,'--build-id',$manifest.source_commit,'--gui-directory',$gui,'--go',$Go,'--smoke')
        } finally { Pop-Location }
        $report.distribution = $out
    }
    $report.status = 'passed'
} catch {
    $report.error = $_.Exception.Message
    Write-Host $report.error
} finally {
    $report.total_seconds = [Math]::Round($watch.Elapsed.TotalSeconds,3)
    if (Test-Path -LiteralPath $runRoot) {
        $report | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $runRoot 'report.json') -Encoding UTF8
        Write-Host ('Report: ' + (Join-Path $runRoot 'report.json'))
    }
    $lock.Dispose()
}
if ($report.status -ne 'passed') { exit 1 }
