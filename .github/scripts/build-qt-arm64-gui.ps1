param([Parameter(Mandatory)][string]$Output)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$sdk = Join-Path $env:RUNNER_TEMP 'flutter-arm64'
git clone --depth 1 --branch 3.47.0 https://github.com/flutter/flutter.git $sdk
if ((git -C $sdk rev-parse HEAD) -cne '4cf24164269a5ebf0c16a028a00727d0e77bbb05') {
    throw 'Flutter framework commit mismatch.'
}
$flutter = Join-Path $sdk 'bin/flutter.bat'
$null = New-Item -ItemType Directory -Path $Output
$out = (Resolve-Path $Output).Path
Push-Location gui
try {
    & $flutter pub get --enforce-lockfile
    foreach ($version in '0.6.0-rc.1','0.6.0') {
        & $flutter build windows --release --no-pub --target lib/main.dart --build-name $version --build-number 1
        $runtime = (Resolve-Path 'build/windows/arm64/runner/Release').Path
        $app = Join-Path $runtime 'Sidravia.exe'
        if ((Get-Item $app).VersionInfo.ProductVersion -cne ($version + '+1')) { throw 'Flutter GUI build version mismatch.' }
        Copy-Item -LiteralPath $runtime -Destination (Join-Path $out $version) -Recurse
    }
} finally { Pop-Location }
Set-Content -LiteralPath (Join-Path $out 'source-commit.txt') -Value $env:GITHUB_SHA -Encoding Ascii
