param(
    [Parameter(Mandatory)][string]$FrameworkSource,
    [Parameter(Mandatory)][string]$Qt,
    [Parameter(Mandatory)][string]$Output,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-fA-F]{64}$')][string]$PublisherPublicKey,
    [Parameter(Mandatory)][string]$CompressionAndSodiumHeaders,
    [Parameter(Mandatory)][string]$CompressionAndSodiumLibraries
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$source = (Resolve-Path -LiteralPath $FrameworkSource).Path
$qtPrefix = (Resolve-Path -LiteralPath $Qt).Path
$out = [IO.Path]::GetFullPath($Output)
$headers = ($CompressionAndSodiumHeaders.Split(';') | ForEach-Object { (Resolve-Path -LiteralPath $_).Path }) -join ';'
$libraries = ($CompressionAndSodiumLibraries.Split(';') | ForEach-Object { (Resolve-Path -LiteralPath $_).Path }) -join ';'
if (Test-Path -LiteralPath $out) { throw 'Output must be a new build directory.' }
$null = New-Item -ItemType Directory -Path $out
$gate = Join-Path $out 'gate'
$null = New-Item -ItemType Directory -Path $gate
Copy-Item -LiteralPath "$PSScriptRoot\publisher-gate.pri","$PSScriptRoot\productkeycheck.cpp" -Destination $gate
Set-Content -LiteralPath (Join-Path $gate 'publisherkey.h') -Encoding ASCII -Value (
    '#define SIDRAVIA_PUBLISHER_PUBLIC_KEY "' + $PublisherPublicKey.ToLowerInvariant() + '"'
)
foreach ($name in 'use-pinned-archives.patch', 'use-pinned-metadata.patch') {
    & cmd.exe /D /C "git -C `"$source`" apply --reverse --check `"$(Join-Path $PSScriptRoot $name)`" 2>nul"
    if ($LASTEXITCODE -eq 0) { continue }
    & git -C $source apply (Join-Path $PSScriptRoot $name)
    if ($LASTEXITCODE -ne 0) { throw "Unable to apply $name to the pinned IFW source." }
}
$env:INCLUDE = "$qtPrefix\include\QtZlib;$headers;$env:INCLUDE"
$env:LIB = "$qtPrefix\lib;$libraries;$env:LIB"
Push-Location -LiteralPath $out
try {
    & "$qtPrefix\bin\qmake.exe" -r "$source\installerfw.pro" "PRODUCTKEYCHECK_PRI_FILE=$gate\publisher-gate.pri" "IFW_ZLIB_LIBRARY=-lQt6BundledZLIB" "IFW_BZIP2_LIBRARY=-llibbz2" "IFW_LZMA_LIBRARY=-llzma" "LIBS+=-llibsodium -ladvapi32"
    if ($LASTEXITCODE -ne 0) { throw 'Configuring the IFW build failed.' }
    & nmake.exe /NOLOGO sub-src
    if ($LASTEXITCODE -ne 0) { throw 'Building the IFW runtime failed.' }
    & dumpbin.exe /DEPENDENTS bin\installerbase.exe
    if ($LASTEXITCODE -ne 0) { throw 'Inspecting the IFW runtime dependencies failed.' }
}
finally {
    Pop-Location
}
