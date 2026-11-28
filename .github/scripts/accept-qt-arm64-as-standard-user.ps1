param([Parameter(Mandatory)][string]$Work)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = (Resolve-Path "$PSScriptRoot/../..").Path
$work = (Resolve-Path $Work).Path
$tools = (New-Item -ItemType Directory -Path (Join-Path $work 'toolbin')).FullName
foreach ($program in 'uv.exe','task.exe') {
    Copy-Item -LiteralPath (Get-Command $program).Source -Destination $tools
}
$python = (Get-Command python.exe).Source.Replace("'","''")
$localDeveloper = (New-Item -ItemType Directory -Path "$work/tools/developer" -Force).FullName
Copy-Item -LiteralPath "$repo/tools/developer/acceptance" -Destination $localDeveloper -Recurse
Copy-Item -LiteralPath "$repo/tools/developer/verify_distribution.py" -Destination $localDeveloper
Copy-Item -LiteralPath "$repo/Taskfile.yml" -Destination $work
$child = Join-Path $work 'run-acceptance.ps1'
@'
$ErrorActionPreference = 'Stop'
Start-Transcript -Path (Join-Path $PSScriptRoot 'acceptance.log')
try {
    $principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
    $admin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    $session = (Get-Process -Id $PID).SessionId
    [pscustomobject]@{
      administrator = $admin
      session = $session
      architecture = $env:PROCESSOR_ARCHITECTURE
    } | ConvertTo-Json | Set-Content runner-context.json -Encoding UTF8
    if ($admin -or $session -eq 0 -or $env:PROCESSOR_ARCHITECTURE -ne 'ARM64') {
        throw 'The Qt acceptance session is not a native ARM64 standard-user desktop.'
    }
    # Start-Process -Credential can inherit folders from the runner's environment.
    $profile = [Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)
    $roaming = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
    $local = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    if (!$profile -or !$roaming -or !$local) {
        throw 'The standard-user profile did not provide its application data folders.'
    }
    $temp = (New-Item -ItemType Directory -Path (Join-Path $local 'Temp') -Force).FullName
    $probe = Join-Path $temp ('sidravia-acceptance-' + [Guid]::NewGuid().ToString('N'))
    [IO.File]::WriteAllText($probe, '')
    Remove-Item -LiteralPath $probe
    $env:USERPROFILE = $profile
    $env:APPDATA = $roaming
    $env:LOCALAPPDATA = $local
    $env:TEMP = $temp
    $env:TMP = $temp
    Write-Host "Standard-user writable temp directory: $temp"
    $env:Path = "$PSScriptRoot\toolbin;$env:Path"
    $env:UV_PYTHON = 'PYTHON_PATH'
    $env:UV_NO_MANAGED_PYTHON = '1'
    $env:PYTHONUTF8 = '1'
    $env:SIDRAVIA_IFW_ISOLATED_WINDOWS = '1'
    $env:SIDRAVIA_QT_ACCEPTANCE_REQUEST = Join-Path $PSScriptRoot 'qt-acceptance.json'
    & task --exit-code accept:windows-qt -- --basetemp lifecycle --junitxml junit.xml
    $code = $LASTEXITCODE
} finally { Stop-Transcript }
exit $code
'@.Replace('PYTHON_PATH', $python) | Set-Content -LiteralPath $child -Encoding utf8BOM
try {
    & "$PSScriptRoot/run-qt-as-standard-user.ps1" -Work $work -ChildScript $child -TimeoutSeconds 700
} finally {
    $evidence = (New-Item -ItemType Directory -Path "$repo/qt-evidence" -Force).FullName
    foreach ($name in 'runner-context.json','junit.xml','acceptance.log') {
        $path = Join-Path $work $name
        if (Test-Path $path -PathType Leaf) { Copy-Item -LiteralPath $path -Destination $evidence }
    }
    $lifecycle = Join-Path $work 'lifecycle'
    if (Test-Path $lifecycle -PathType Container) {
        Get-ChildItem -LiteralPath $lifecycle -File -Recurse | Where-Object {
            $_.Extension -eq '.log' -or $_.Name -eq 'qt-outcome.json'
        } | ForEach-Object {
            $relative = [IO.Path]::GetRelativePath($work,$_.FullName)
            $destination = Join-Path $evidence $relative
            $null = New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force
            Copy-Item -LiteralPath $_.FullName -Destination $destination
        }
    }
}
