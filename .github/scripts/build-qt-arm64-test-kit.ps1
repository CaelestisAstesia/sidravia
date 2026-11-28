param([Parameter(Mandatory)][string]$Work,
      [Parameter(Mandatory)][string]$Output,
      [Parameter(Mandatory)][string]$FrameworkArchive)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-Native($Program, [object[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program exited $LASTEXITCODE" }
}

$repo = (Resolve-Path "$PSScriptRoot/../..").Path
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
$vs = & $vswhere -latest -products '*' -version '[17,18)' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if (!$vs) { throw 'Visual Studio 2022 x64 is required' }
$envLines = & cmd.exe /d /c ('call "' + (Join-Path $vs 'VC/Auxiliary/Build/vcvars64.bat') + '" && set')
if ($LASTEXITCODE -ne 0) { throw 'Unable to initialize MSVC' }
foreach ($line in $envLines) {
    if ($line -match '^([^=]+)=(.*)$') { [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2]) }
}

$src = New-Item -ItemType Directory -Path (Join-Path $Work 'src') -Force
function Fetch-Source($Url, $File, $SHA256) {
    $archive = Join-Path $Work $File
    Invoke-Native curl.exe @('--fail','--location','--silent','--show-error','--retry','2','--output',$archive,$Url)
    if ((Get-FileHash $archive -Algorithm SHA256).Hash -ine $SHA256) { throw "Source digest mismatch: $File" }
    Invoke-Native tar.exe @('-xf',$archive,'-C',$src.FullName)
}

foreach ($line in (Get-Content "$repo/packaging/windows/ifw/src.sha256")) {
    $hash,$name = $line -split '\s+'
    Fetch-Source "https://download.qt.io/archive/qt/6.9/6.9.3/submodules/$name" $name $hash
}
Fetch-Source 'https://sourceware.org/pub/bzip2/bzip2-1.0.8.tar.gz' 'bzip2-1.0.8.tar.gz' 'ab5a03176ee106d3f0fa90e381da478ddae405918153cca248e682cd0c4a2269'
Fetch-Source 'https://github.com/tukaani-project/xz/releases/download/v5.8.1/xz-5.8.1.tar.xz' 'xz-5.8.1.tar.xz' '0b54f79df85912504de0b14aec7971e3f964491af1812d83447005807513cd9e'
Fetch-Source 'https://download.libsodium.org/libsodium/releases/libsodium-1.0.22.tar.gz' 'libsodium-1.0.22.tar.gz' 'adbdd8f16149e81ac6078a03aca6fc03b592b89ef7b5ed83841c086191be3349'

$prefix = Join-Path $Work 'qt-prefix'
$base = New-Item -ItemType Directory -Path (Join-Path $Work 'qt-base')
Push-Location $base
try {
    Invoke-Native (Join-Path $src 'qtbase-everywhere-src-6.9.3/configure.bat') @('-prefix',$prefix,'-release','-static','-static-runtime','-accessibility','-no-icu','-no-sql-sqlite','-nomake','examples','-nomake','tests','-opensource','-confirm-license')
    Invoke-Native cmake @('--build','.','--parallel','4')
    Invoke-Native cmake @('--install','.')
} finally { Pop-Location }

foreach ($name in 'qtdeclarative','qt5compat','qttools','qttranslations') {
    $directory = New-Item -ItemType Directory -Path (Join-Path $Work "$name-build")
    Push-Location $directory
    try {
        Invoke-Native (Join-Path $prefix 'bin/qt-configure-module.bat') @((Join-Path $src "$name-everywhere-src-6.9.3"))
        Invoke-Native cmake @('--build','.','--parallel','4')
        Invoke-Native cmake @('--install','.')
    } finally { Pop-Location }
}

$bzip = Join-Path $src 'bzip2-1.0.8'
$bzipBuild = New-Item -ItemType Directory -Path (Join-Path $Work 'bzip2')
Push-Location $bzipBuild
try {
    $units = @('blocksort','huffman','crctable','randtable','compress','decompress','bzlib')
    Invoke-Native cl.exe (@('/nologo','/c','/O2','/MT') + @($units | ForEach-Object { Join-Path $bzip "$_.c" }))
    Invoke-Native lib.exe (@('/nologo','/out:libbz2.lib') + @($units | ForEach-Object { "$_.obj" }))
} finally { Pop-Location }

$xz = Join-Path $Work 'xz'
$xzBuild = Join-Path $Work 'xz-build'
Invoke-Native cmake @('-S',(Join-Path $src 'xz-5.8.1'),'-B',$xzBuild,'-G','Ninja',
    '-DCMAKE_BUILD_TYPE=Release','-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded','-DBUILD_SHARED_LIBS=OFF',
    '-DBUILD_TESTING=OFF',"-DCMAKE_INSTALL_PREFIX=$xz")
Invoke-Native cmake @('--build',$xzBuild,'--parallel','4','--target','install')

$sodium = Join-Path $src 'libsodium-1.0.22'
Invoke-Native msbuild.exe @((Join-Path $sodium 'builds/msvc/vs2022/libsodium.sln'),'/nologo','/m:4','/p:Configuration=StaticRelease','/p:Platform=x64')
$crypto = Join-Path $sodium 'bin/x64/Release/v143/static'

$ifw = Join-Path $Work 'ifw-build'
# Public Ed25519 fixture vector corresponding to the all-zero test seed.
& "$repo/packaging/windows/ifw/build.ps1" -FrameworkArchive $FrameworkArchive -Qt $prefix `
    -PayloadTarget windows-arm64 -Output $ifw `
    -PublisherPublicKey '3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29' `
    -CompressionAndSodiumHeaders "$bzip;$xz/include;$sodium/src/libsodium/include" `
    -CompressionAndSodiumLibraries "$($bzipBuild.FullName);$xz/lib;$crypto"
$null = New-Item -ItemType Directory -Path $Output
foreach ($name in 'installerbase.exe','binarycreator.exe','repogen.exe') {
    Copy-Item (Join-Path $ifw "build/bin/$name") $Output
}
