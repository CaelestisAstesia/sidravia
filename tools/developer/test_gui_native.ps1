[CmdletBinding()]
param([string]$VisualStudioDirectory)

$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'native_policy_tests_require_windows' }
if (-not $VisualStudioDirectory) {
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
    $VisualStudioDirectory = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if ($LASTEXITCODE -ne 0 -or -not $VisualStudioDirectory) { throw 'msvc_toolchain_not_found' }
}
$cmakeRoot = Join-Path $VisualStudioDirectory 'Common7/IDE/CommonExtensions/Microsoft/CMake'
$cmake = Join-Path $cmakeRoot 'CMake/bin/cmake.exe'
$ctest = Join-Path $cmakeRoot 'CMake/bin/ctest.exe'
$ninja = Join-Path $cmakeRoot 'Ninja/ninja.exe'
$developer = Join-Path $VisualStudioDirectory 'Common7/Tools/VsDevCmd.bat'
foreach ($tool in @($cmake, $ctest, $ninja, $developer)) {
    if (-not (Test-Path -LiteralPath $tool -PathType Leaf)) { throw 'native_policy_tool_missing' }
}
$sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits/10'
$sdkVersion = Get-ChildItem -LiteralPath (Join-Path $sdk 'bin') -Directory |
    Where-Object { $_.Name -match '^\d+\.\d+\.\d+\.\d+$' -and
        (Test-Path -LiteralPath (Join-Path $_.FullName 'x64/rc.exe')) -and
        (Test-Path -LiteralPath (Join-Path $_.FullName 'x64/mt.exe')) -and
        (Test-Path -LiteralPath (Join-Path $sdk ('Include/' + $_.Name + '/um/windows.h'))) -and
        (Test-Path -LiteralPath (Join-Path $sdk ('Lib/' + $_.Name + '/um/x64/kernel32.lib'))) } |
    Sort-Object { [version]$_.Name } -Descending | Select-Object -First 1
if (-not $sdkVersion) { throw 'windows_sdk_not_found' }
$sdkBin = Join-Path $sdkVersion.FullName 'x64'
$cmakeSDKBin = $sdkBin.Replace('\', '/')
$sdkInclude = Join-Path $sdk ('Include/' + $sdkVersion.Name)
$sdkLib = Join-Path $sdk ('Lib/' + $sdkVersion.Name)
$source = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../gui/test/native'))
$owned = Join-Path ([IO.Path]::GetTempPath()) ('sidravia-native-policies-' + [guid]::NewGuid().ToString('N'))
try {
    $null = New-Item -ItemType Directory -Path $owned
    $build = Join-Path $owned 'build'
    $batch = Join-Path $owned 'run.cmd'
    $commands = @(
        '@echo off',
        ('cd /d "' + $owned + '"'),
        ('call "' + $developer + '" -no_logo -arch=x64 -host_arch=x64'),
        'if errorlevel 1 exit /b %errorlevel%',
        ('set "PATH=' + $sdkBin + ';%PATH%"'),
        ('set "INCLUDE=' + $sdkInclude + '\ucrt;' + $sdkInclude + '\shared;' + $sdkInclude + '\um;' + $sdkInclude + '\winrt;%INCLUDE%"'),
        ('set "LIB=' + $sdkLib + '\ucrt\x64;' + $sdkLib + '\um\x64;%LIB%"'),
        ('"' + $cmake + '" -S "' + $source + '" -B "' + $build + '" -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_MAKE_PROGRAM="' + $ninja + '" -DCMAKE_RC_COMPILER:FILEPATH="' + $cmakeSDKBin + '/rc.exe" -DCMAKE_MT:FILEPATH="' + $cmakeSDKBin + '/mt.exe"'),
        'if errorlevel 1 exit /b %errorlevel%',
        ('"' + $cmake + '" --build "' + $build + '" --parallel 2'),
        'if errorlevel 1 exit /b %errorlevel%',
        ('"' + $ctest + '" --test-dir "' + $build + '" --output-on-failure'),
        'exit /b %errorlevel%'
    )
    [IO.File]::WriteAllLines($batch, $commands, [Text.Encoding]::Default)
    # Use native machine tools even when launched from a WSL-inherited PATH.
    $info = New-Object Diagnostics.ProcessStartInfo
    $info.FileName = Join-Path $env:SystemRoot 'System32/cmd.exe'
    $info.Arguments = '/d /c "' + $batch + '"'
    $info.WorkingDirectory = $owned
    $info.UseShellExecute = $false
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.EnvironmentVariables['Path'] = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $info
    try {
        if (-not $process.Start()) { throw 'native_policy_process_start_failed' }
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(300000)) {
            & (Join-Path $env:SystemRoot 'System32/taskkill.exe') /PID $process.Id /T /F | Out-Null
            throw 'native_policy_process_timeout'
        }
        [Console]::Out.Write($stdout.Result)
        [Console]::Error.Write($stderr.Result)
        if ($process.ExitCode -ne 0) { throw ('native_policy_tests_failed: ' + $process.ExitCode) }
    } finally { $process.Dispose() }
} finally {
    if (Test-Path -LiteralPath $owned) { Remove-Item -LiteralPath $owned -Recurse -Force }
}
