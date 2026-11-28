param([Parameter(Mandatory)][string]$Work, [switch]$ProfileOnly)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = (Resolve-Path "$PSScriptRoot/../..").Path
$work = (Resolve-Path $Work).Path
$python = ''
if (!$ProfileOnly) {
    $tools = (New-Item -ItemType Directory -Path (Join-Path $work 'toolbin')).FullName
    foreach ($program in 'uv.exe','task.exe') {
        Copy-Item -LiteralPath (Get-Command $program).Source -Destination $tools
    }
    $python = (Get-Command python.exe).Source.Replace("'","''")
    $localDeveloper = (New-Item -ItemType Directory -Path "$work/tools/developer" -Force).FullName
    Copy-Item -LiteralPath "$repo/tools/developer/acceptance" -Destination $localDeveloper -Recurse
    Copy-Item -LiteralPath "$repo/tools/developer/verify_distribution.py" -Destination $localDeveloper
    Copy-Item -LiteralPath "$repo/Taskfile.yml" -Destination $work
}
$child = Join-Path $work 'run-acceptance.ps1'
@'
$ErrorActionPreference = 'Stop'
Start-Transcript -Path (Join-Path $PSScriptRoot 'acceptance.log')
try {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
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
    $profileKey = "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\$($identity.User.Value)"
    $userHome = [Environment]::ExpandEnvironmentVariables((Get-ItemProperty -LiteralPath $profileKey).ProfileImagePath)
    if (!(Test-Path -LiteralPath $userHome -PathType Container)) {
        throw 'The signed-in test account does not have a loaded profile directory.'
    }
    $roaming = Join-Path $userHome 'AppData\Roaming'
    $local = Join-Path $userHome 'AppData\Local'
    $temp = (New-Item -ItemType Directory -Path (Join-Path $local 'Temp') -Force).FullName
    $null = New-Item -ItemType Directory -Path $roaming -Force
    $probe = Join-Path $temp ('sidravia-acceptance-' + [Guid]::NewGuid().ToString('N'))
    [IO.File]::WriteAllText($probe, '')
    Remove-Item -LiteralPath $probe
    $env:USERPROFILE = $userHome
    $env:APPDATA = $roaming
    $env:LOCALAPPDATA = $local
    $env:TEMP = $temp
    $env:TMP = $temp
    Write-Host "Standard-user writable temp directory: $temp"
    if ('PROFILE_ONLY' -eq 'True') { exit 0 }
    $env:Path = "$PSScriptRoot\toolbin;$env:Path"
    $env:UV_PYTHON = 'PYTHON_PATH'
    $env:UV_NO_MANAGED_PYTHON = '1'
    $env:PYTHONUTF8 = '1'
    $env:SIDRAVIA_IFW_ISOLATED_WINDOWS = '1'
    $env:SIDRAVIA_QT_ACCEPTANCE_REQUEST = Join-Path $PSScriptRoot 'qt-acceptance.json'
    & task --exit-code accept:windows-qt -- --basetemp lifecycle --junitxml junit.xml
    $code = $LASTEXITCODE
} catch {
    $_ | Out-String | Set-Content -LiteralPath (Join-Path $PSScriptRoot 'child-error.log') -Encoding UTF8
    throw
} finally { Stop-Transcript }
exit $code
'@.Replace('PYTHON_PATH', $python).Replace('PROFILE_ONLY', [string]$ProfileOnly.IsPresent) |
    Set-Content -LiteralPath $child -Encoding utf8BOM
try {
    & "$PSScriptRoot/run-qt-as-standard-user.ps1" -Work $work -ChildScript $child -TimeoutSeconds 700
} finally {
    $evidence = (New-Item -ItemType Directory -Path "$repo/qt-evidence" -Force).FullName
    $names = 'runner-context.json','junit.xml','acceptance.log','child-error.log'
    foreach ($name in $names) {
        $path = Join-Path $work $name
        $destination = if ($ProfileOnly) { Join-Path $evidence "preflight-$name" } else { $evidence }
        if (Test-Path $path -PathType Leaf) { Copy-Item -LiteralPath $path -Destination $destination }
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
