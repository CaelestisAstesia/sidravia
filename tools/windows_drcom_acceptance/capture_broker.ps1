param(
    [Parameter(Mandatory = $true)]
    [string]$ManifestPath,
    [switch]$Elevated
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function ConvertTo-QuotedProcessArgument {
    param([Parameter(Mandatory = $true)][string]$Value)
    if ($Value.Contains('"') -or $Value.Contains("`r") -or $Value.Contains("`n")) {
        throw "Unsafe process argument."
    }
    return '"' + $Value + '"'
}

function Write-AtomicJson {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][hashtable]$Value
    )
    $temporaryPath = $Path + ".tmp"
    $json = $Value | ConvertTo-Json -Compress
    [System.IO.File]::WriteAllText(
        $temporaryPath,
        $json,
        [System.Text.UTF8Encoding]::new($false)
    )
    Move-Item -LiteralPath $temporaryPath -Destination $Path -Force
}

function Get-CanonicalPath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $pathRoot = [System.IO.Path]::GetPathRoot($Path)
    if (-not [System.IO.Path]::IsPathRooted($Path) -or
        [string]::IsNullOrWhiteSpace($pathRoot) -or
        -not $pathRoot.EndsWith([System.IO.Path]::DirectorySeparatorChar)) {
        throw "A broker path is not absolute."
    }
    return [System.IO.Path]::GetFullPath($Path)
}

function Assert-ChildPath {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Candidate
    )
    $prefix = $Root.TrimEnd([System.IO.Path]::DirectorySeparatorChar) +
        [System.IO.Path]::DirectorySeparatorChar
    if (-not $Candidate.StartsWith(
            $prefix,
            [System.StringComparison]::OrdinalIgnoreCase
        )) {
        throw "A broker artifact path escapes the run root."
    }
}

function Assert-NotReparsePoint {
    param([Parameter(Mandatory = $true)][string]$Path)
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "A broker path is a reparse point."
    }
}

$canonicalManifest = Get-CanonicalPath -Path $ManifestPath

if (-not $Elevated) {
    $scriptPath = Get-CanonicalPath -Path $PSCommandPath
    $argumentValues = @(
        "-NoProfile",
        "-NonInteractive",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        (ConvertTo-QuotedProcessArgument -Value $scriptPath),
        "-Elevated",
        "-ManifestPath",
        (ConvertTo-QuotedProcessArgument -Value $canonicalManifest)
    )
    $powerShellPath = Join-Path -Path $PSHOME -ChildPath "powershell.exe"
    $elevated = Start-Process -FilePath $powerShellPath -Verb RunAs -Wait -PassThru `
        -ArgumentList $argumentValues
    exit $elevated.ExitCode
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "The elevated capture broker does not have an administrator token."
}

$manifest = Get-Content -LiteralPath $canonicalManifest -Raw -Encoding UTF8 |
    ConvertFrom-Json
$expectedFields = @(
    "schema_version", "run_root", "dumpcap_path", "interface_id", "bpf",
    "pcap_path", "ready_path", "stop_path", "duration_seconds"
)
$actualFields = @($manifest.PSObject.Properties.Name)
if (@($actualFields | Where-Object { $_ -notin $expectedFields }).Count -ne 0 -or
    @($expectedFields | Where-Object { $_ -notin $actualFields }).Count -ne 0) {
    throw "The capture manifest schema is not exact."
}
if ($manifest.schema_version -ne 1) {
    throw "Unsupported capture manifest version."
}
if ($manifest.bpf -cne "udp port 61440") {
    throw "The capture filter is not allowed."
}
$durationText = [string]$manifest.duration_seconds
$duration = 0
if ($durationText -notmatch '^\d+$' -or
    -not [int]::TryParse($durationText, [ref]$duration) -or
    $duration -lt 1 -or
    $duration -gt 900) {
    throw "The capture duration is outside the allowed range."
}
if ([string]::IsNullOrWhiteSpace($manifest.interface_id) -or
    $manifest.interface_id.Length -gt 512 -or
    $manifest.interface_id.IndexOfAny(@([char]0, [char]10, [char]13)) -ge 0) {
    throw "The capture interface identifier is unsafe."
}

$runRoot = Get-CanonicalPath -Path $manifest.run_root
$dumpcapPath = Get-CanonicalPath -Path $manifest.dumpcap_path
$pcapPath = Get-CanonicalPath -Path $manifest.pcap_path
$readyPath = Get-CanonicalPath -Path $manifest.ready_path
$stopPath = Get-CanonicalPath -Path $manifest.stop_path
if ([System.IO.Path]::GetFileName($dumpcapPath) -cne "dumpcap.exe") {
    throw "Only dumpcap.exe may inherit the elevated token."
}
if (-not (Test-Path -LiteralPath $dumpcapPath -PathType Leaf)) {
    throw "dumpcap.exe was not found."
}

$allowedDumpcapPaths = [System.Collections.Generic.List[string]]::new()
if (-not [string]::IsNullOrWhiteSpace($env:ProgramFiles)) {
    $allowedDumpcapPaths.Add((Get-CanonicalPath -Path (
        Join-Path $env:ProgramFiles "Wireshark\dumpcap.exe"
    )))
}
if (-not [string]::IsNullOrWhiteSpace(${env:ProgramFiles(x86)})) {
    $allowedDumpcapPaths.Add((Get-CanonicalPath -Path (
        Join-Path ${env:ProgramFiles(x86)} "Wireshark\dumpcap.exe"
    )))
}
foreach ($registryPath in @(
        "HKLM:\SOFTWARE\Wireshark",
        "HKLM:\SOFTWARE\WOW6432Node\Wireshark"
    )) {
    if (Test-Path -LiteralPath $registryPath) {
        $properties = Get-ItemProperty -LiteralPath $registryPath
        foreach ($propertyName in @("InstallDir", "InstallLocation")) {
            $property = $properties.PSObject.Properties[$propertyName]
            if ($null -eq $property) {
                continue
            }
            $installRoot = $property.Value
            if (-not [string]::IsNullOrWhiteSpace($installRoot)) {
                $allowedDumpcapPaths.Add((Get-CanonicalPath -Path (
                    Join-Path $installRoot "dumpcap.exe"
                )))
            }
        }
    }
}
if (-not ($allowedDumpcapPaths | Where-Object {
            $_.Equals($dumpcapPath, [System.StringComparison]::OrdinalIgnoreCase)
        })) {
    throw "dumpcap.exe is not from a machine-wide Wireshark installation."
}
Assert-ChildPath -Root $runRoot -Candidate $canonicalManifest
Assert-ChildPath -Root $runRoot -Candidate $pcapPath
Assert-ChildPath -Root $runRoot -Candidate $readyPath
Assert-ChildPath -Root $runRoot -Candidate $stopPath
foreach ($artifactPath in @($canonicalManifest, $pcapPath, $readyPath, $stopPath)) {
    if (-not [System.IO.Path]::GetDirectoryName($artifactPath).Equals(
            $runRoot,
            [System.StringComparison]::OrdinalIgnoreCase
        )) {
        throw "Broker artifacts must be direct children of the run root."
    }
}
Assert-NotReparsePoint -Path $runRoot
Assert-NotReparsePoint -Path $canonicalManifest
Assert-NotReparsePoint -Path $dumpcapPath
foreach ($outputPath in @($pcapPath, $readyPath, $stopPath)) {
    if (Test-Path -LiteralPath $outputPath) {
        throw "A broker output path already exists."
    }
}

$dumpcap = $null
$brokerFailure = $null
try {
    $dumpcapArguments = @(
        "-i", (ConvertTo-QuotedProcessArgument -Value $manifest.interface_id),
        "-f", (ConvertTo-QuotedProcessArgument -Value "udp port 61440"),
        "-w", (ConvertTo-QuotedProcessArgument -Value $pcapPath),
        "-q"
    )
    $dumpcap = Start-Process -FilePath $dumpcapPath -ArgumentList $dumpcapArguments `
        -PassThru -NoNewWindow
    Write-AtomicJson -Path $readyPath -Value @{
        schema_version = 1
        status = "ready"
        dumpcap_pid = $dumpcap.Id
    }

    $deadline = [DateTime]::UtcNow.AddSeconds($duration)
    while ([DateTime]::UtcNow -lt $deadline) {
        if (Test-Path -LiteralPath $stopPath -PathType Leaf) {
            break
        }
        if ($dumpcap.HasExited) {
            throw "dumpcap exited before a stop signal was received."
        }
        Start-Sleep -Milliseconds 100
        $dumpcap.Refresh()
    }
}
catch {
    $brokerFailure = $_.Exception.GetType().Name
}
finally {
    if ($null -ne $dumpcap) {
        try {
            if (-not $dumpcap.HasExited) {
                Stop-Process -Id $dumpcap.Id -Force -ErrorAction Stop
            }
            $dumpcap.WaitForExit(10000) | Out-Null
        }
        catch {
            if ($null -eq $brokerFailure) {
                $brokerFailure = "DumpcapCleanupFailure"
            }
        }
    }
    Write-AtomicJson -Path $readyPath -Value @{
        schema_version = 1
        status = "stopped"
        outcome = $(if ($null -eq $brokerFailure) { "clean" } else { "failed" })
    }
}

if ($null -ne $brokerFailure) {
    throw "The capture broker failed safely: $brokerFailure"
}
