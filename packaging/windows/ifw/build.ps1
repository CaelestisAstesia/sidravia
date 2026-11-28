param(
    [Parameter(Mandatory)][string]$FrameworkArchive,
    [Parameter(Mandatory)][string]$Qt,
    [Parameter(Mandatory)][ValidateSet("windows-amd64","windows-arm64")][string]$PayloadTarget,
    [Parameter(Mandatory)][string]$Output,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-fA-F]{64}$')][string]$PublisherPublicKey,
    [Parameter(Mandatory)][string]$CompressionAndSodiumHeaders,
    [Parameter(Mandatory)][string]$CompressionAndSodiumLibraries
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$archive = (Resolve-Path -LiteralPath $FrameworkArchive).Path
$qtPrefix = (Resolve-Path -LiteralPath $Qt).Path
$out = [IO.Path]::GetFullPath($Output)
$headers = ($CompressionAndSodiumHeaders.Split(';') | ForEach-Object { (Resolve-Path -LiteralPath $_).Path }) -join ';'
$libraries = ($CompressionAndSodiumLibraries.Split(';') | ForEach-Object { (Resolve-Path -LiteralPath $_).Path }) -join ';'
if (Test-Path -LiteralPath $out) { throw 'Output must be a new build directory.' }
# Source archives from the verified upstream 4.11.0 Git tree.
$sourceLock = Get-Content -Raw -LiteralPath "$PSScriptRoot/source-lock.json" | ConvertFrom-Json
$expected = if ([IO.Path]::GetExtension($archive) -ieq '.tar') {
    $sourceLock.tarSHA256
} else {
    $sourceLock.gzipSHA256
}
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -cne $expected) {
    throw 'Unexpected IFW source archive digest.'
}
$null = New-Item -ItemType Directory -Path $out
& tar.exe -xf $archive -C $out
if ($LASTEXITCODE -ne 0) { throw 'Unable to extract the verified IFW source.' }
$source = Join-Path $out 'ifw'
$gate = Join-Path $out 'gate'
$null = New-Item -ItemType Directory -Path $gate
Copy-Item -LiteralPath "$PSScriptRoot\publisher-gate.pri","$PSScriptRoot\productkeycheck.cpp" -Destination $gate
Set-Content -LiteralPath (Join-Path $gate 'publisherkey.h') -Encoding ASCII -Value @(
    ('#define SIDRAVIA_PUBLISHER_PUBLIC_KEY "' + $PublisherPublicKey.ToLowerInvariant() + '"'),
    ('#define SIDRAVIA_IFW_TARGET "' + $PayloadTarget + '"')
)
foreach ($name in 'use-pinned-archives.patch', 'use-pinned-metadata.patch', 'preserve-maintenance-image.patch', 'report-maintenance-outcome.patch', 'remove-user-integration.patch') {
    & git -C $source apply (Join-Path $PSScriptRoot $name)
    if ($LASTEXITCODE -ne 0) { throw "Unable to apply $name to the pinned IFW source." }
}
$env:INCLUDE = "$qtPrefix\include\QtZlib;$headers;$env:INCLUDE"
$env:LIB = "$qtPrefix\lib;$libraries;$env:LIB"
$build = Join-Path $out 'build'
$null = New-Item -ItemType Directory -Path $build
Push-Location -LiteralPath $build
try {
    & "$qtPrefix\bin\qmake.exe" -r "$source\installerfw.pro" "PRODUCTKEYCHECK_PRI_FILE=$gate\publisher-gate.pri" "IFW_ZLIB_LIBRARY=-lQt6BundledZLIB" "IFW_BZIP2_LIBRARY=-llibbz2" "IFW_LZMA_LIBRARY=-llzma" "LIBS+=-llibsodium -ladvapi32 -lrstrtmgr"
    if ($LASTEXITCODE -ne 0) { throw 'Configuring the IFW build failed.' }
    & nmake.exe /NOLOGO sub-src
    if ($LASTEXITCODE -ne 0) { throw 'Building the IFW runtime failed.' }
    foreach ($tool in 'binarycreator', 'repogen') {
        Push-Location -LiteralPath (Join-Path $build "tools\$tool")
        try {
            & nmake.exe /NOLOGO
            if ($LASTEXITCODE -ne 0) { throw "Building $tool failed." }
        }
        finally {
            Pop-Location
        }
    }
    & dumpbin.exe /DEPENDENTS bin\installerbase.exe
    if ($LASTEXITCODE -ne 0) { throw 'Inspecting the IFW runtime dependencies failed.' }
}
finally {
    Pop-Location
}
