# User-invoked isolated GUI launch. Windows PowerShell 5.1, no network.
[CmdletBinding()]
param([Parameter(Mandatory=$true)][string]$Directory,[Parameter(Mandatory=$true)][string]$Namespace)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2
function Safe([string]$Path) {
    $p = [IO.Path]::GetFullPath($Path)
    while ($p) {
        if (Test-Path -LiteralPath $p) {
            if ((Get-Item -Force -LiteralPath $p).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Linked/reparse path refused.' }
        }
        $p = [IO.Path]::GetDirectoryName($p)
    }
}
if ($env:OS -ne 'Windows_NT') { throw 'Windows only.' }
if ($Namespace.Length -gt 64 -or $Namespace -notmatch '^[A-Za-z0-9_-][A-Za-z0-9._-]*$' -or $Namespace.EndsWith('.') -or $Namespace.Contains('..') -or $Namespace -in @('prod','production','default')) { throw 'Invalid isolated namespace.' }
$Directory = [IO.Path]::GetFullPath($Directory)
Safe $Directory
$exe = Join-Path $Directory 'Sidravia.exe'; Safe $exe
if (!(Test-Path -LiteralPath $exe -PathType Leaf)) { throw 'Sidravia.exe missing.' }
$marker = Join-Path $Directory 'sidravia.portable'; Safe $marker
if (Test-Path -LiteralPath $marker) {
    if (!(Test-Path -LiteralPath $marker -PathType Leaf)) { throw 'Invalid portable marker.' }
    $root = Join-Path $Directory ('namespaces\' + $Namespace)
} else { $root = Join-Path $env:APPDATA ('Sidravia\namespaces\' + $Namespace) }
Safe $root
$buildFile = Join-Path $Directory 'BUILD-INFO.txt'; Safe $buildFile
$build = Get-Content -Raw -LiteralPath $buildFile
if ($build -notmatch '(?m)^BuildID:\s*([a-fA-F0-9]{40})\s*$') { throw 'Full candidate BuildID missing.' }
$owner = [ordered]@{Namespace=$Namespace;Directory=$Directory;BuildID=$Matches[1]}
$ownerFile = Join-Path $root '.offline-launch-owner.json'; Safe $ownerFile
$reuse = Test-Path -LiteralPath $root
if ($reuse) {
    if (!(Test-Path -LiteralPath $ownerFile -PathType Leaf)) { throw 'Unknown existing namespace refused.' }
    $existing = New-Object 'Collections.Generic.Stack[string]'; $existing.Push($root)
    $count = 0
    while ($existing.Count) {
        $item = $existing.Pop(); Safe $item; $count++
        if ($count -gt 10000) { throw 'Namespace exceeds 10000 items.' }
        if (Test-Path -LiteralPath $item -PathType Container) { foreach ($child in @(Get-ChildItem -Force -LiteralPath $item)) { $existing.Push($child.FullName) } }
    }
    $prior = Get-Content -Raw -LiteralPath $ownerFile | ConvertFrom-Json
    if ($prior.Namespace -cne $Namespace -or $prior.Directory -ine $Directory -or $prior.BuildID -cne $owner.BuildID) { throw 'Existing namespace owner mismatch.' }
}

$source = Join-Path $Directory 'institution-profiles'; Safe $source
if (!(Test-Path -LiteralPath $source -PathType Container)) { throw 'Packaged institution-profiles missing.' }
$files = New-Object 'Collections.Generic.List[string]'
$stack = New-Object 'Collections.Generic.Stack[string]'; $stack.Push($source)
while ($stack.Count) {
    $p = $stack.Pop(); Safe $p
    foreach ($i in @(Get-ChildItem -Force -LiteralPath $p)) {
        Safe $i.FullName
        if ($i.PSIsContainer) { $stack.Push($i.FullName) } else { $files.Add($i.FullName) }
        if ($files.Count -gt 1000) { throw 'Unexpected profile inventory size.' }
    }
}
if (!$reuse) {
New-Item -ItemType Directory -Path $root | Out-Null
$destination = Join-Path $root 'institution-profiles'
New-Item -ItemType Directory -Path $destination | Out-Null
foreach ($f in $files) {
    $relative = $f.Substring($source.Length).TrimStart('\')
    $to = Join-Path $destination $relative
    New-Item -ItemType Directory -Force -Path ([IO.Path]::GetDirectoryName($to)) | Out-Null
    Copy-Item -LiteralPath $f -Destination $to
}
$owner | ConvertTo-Json | Set-Content -LiteralPath $ownerFile -Encoding UTF8
}
$info = New-Object Diagnostics.ProcessStartInfo
$info.FileName = $exe; $info.WorkingDirectory = $Directory; $info.UseShellExecute = $false
$info.EnvironmentVariables['SIDRAVIA_NAMESPACE'] = $Namespace
[Diagnostics.Process]::Start($info) | Select-Object Id,ProcessName
Write-Host "Isolated namespace: $Namespace. Do not enter real credentials or authenticate. Only this helper owned namespace may be reused."
