# Windows PowerShell 5.1. Read-only system inspection; writes only EvidenceRoot.
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][ValidateSet('Before','Installed','AfterKeep','AfterDelete')][string]$Stage,
    [Parameter(Mandatory=$true)][string]$Directory,
    [Parameter(Mandatory=$true)][string]$Namespace,
    [Parameter(Mandatory=$true)][string]$EvidenceRoot,
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-fA-F0-9]{40}$')][string]$ExpectedBuildID,
    [Parameter(Mandatory=$true)][string]$CandidateManifest,
    [Parameter(Mandatory=$true)][string]$CandidateInstaller,
    [switch]$KeepDataOnly
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2
function Assert-SafePath([string]$Path) {
    $p = [IO.Path]::GetFullPath($Path)
    while ($p) {
        if (Test-Path -LiteralPath $p) {
            $i = Get-Item -Force -LiteralPath $p
            if ($i.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Linked/reparse path refused.' }
        }
        $parent = [IO.Path]::GetDirectoryName($p)
        if ($parent -eq $p) { break }; $p = $parent
    }
}
function Inventory([string]$Root) {
    # An absent shortcut has no content to inventory. Its shell-owned parent
    # may be a OneDrive reparse directory; never enumerate that parent.
    if (!(Test-Path -LiteralPath $Root)) { return }
    Assert-SafePath $Root
    $stack = New-Object 'Collections.Generic.Stack[string]'
    $stack.Push($Root)
    $count = 0
    while ($stack.Count -gt 0) {
        $p = $stack.Pop(); Assert-SafePath $p
        $i = Get-Item -Force -LiteralPath $p
        $count++; if ($count -gt 10000) { throw 'Inventory exceeds 10000 items; stop and inspect manually.' }
        if ($i.PSIsContainer) {
            [pscustomobject]@{ Path=$i.FullName; Kind='Directory'; Hash=$null; Length=$null }
            foreach ($child in @(Get-ChildItem -Force -LiteralPath $p)) { $stack.Push($child.FullName) }
        } else {
            [pscustomobject]@{ Path=$i.FullName; Kind='File'; Hash=(Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash; Length=$i.Length }
        }
    }
}
function Result([string]$Name,[string]$Status,[string]$Detail) {
    $script:results.Add([pscustomobject]@{Check=$Name;Status=$Status;Detail=$Detail})
}
function Payload-Mismatches($Payload,$Inventory) {
    @($Payload | Where-Object {
        $wanted = $_
        $actual = @($Inventory | Where-Object { $_.Path -ieq $wanted.Path -and $_.Kind -eq 'File' })
        $actual.Count -ne 1 -or $actual[0].Hash -ine $wanted.SHA256
    })
}
function Equal-Json($A,$B) { (ConvertTo-Json -Depth 20 -Compress -InputObject $A) -ceq (ConvertTo-Json -Depth 20 -Compress -InputObject $B) }
function Same-Inventory($A,$B) {
    Equal-Json @($A | Sort-Object Path | Select-Object Path,Kind,Hash,Length) @($B | Sort-Object Path | Select-Object Path,Kind,Hash,Length)
}
function Managed-DataFile([string]$Path,[string]$Config,[string]$Cache) {
    $name = [IO.Path]::GetFileName($Path)
    $parent = [IO.Path]::GetDirectoryName($Path)
    $logRoot = Join-Path $Cache 'logs'
    if ($parent -ieq $Config) {
        if ($name -in @('configurations.json','gui-settings.json')) { return $true }
        if ($name -cmatch '^\.sidravia-(?:gui-)?migration-[a-f0-9]{32}\.json$|^\.sidravia-upgrade-[a-f0-9]{32}\.txt$') { return $true }
        if ($name -cmatch '^gui-settings\.json\.([1-9][0-9]{0,9})\.(0|[1-9][0-9]{0,9})\.tmp$') {
            return [uint64]$Matches[1] -le [uint32]::MaxValue -and [uint64]$Matches[2] -le [uint32]::MaxValue
        }
    }
    if ($parent -ieq $Cache -and $name -ieq 'runtime.json') { return $true }
    if ($parent -ieq $logRoot -and $name -cmatch '^sidraviad(?:\.log(?:\.1)?|-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{6}\.log)$') { return $true }
    return $parent -in @($Config,$Cache,$logRoot) -and $name -cmatch '^\.sidravia-[a-f0-9]{32}\.tmp$'
}
function Managed-Namespace([string]$Name) {
    $Name -match '^[a-z0-9._-]{1,64}$' -and !$Name.StartsWith('.') -and !$Name.EndsWith('.') -and
        !$Name.Contains('..') -and $Name -notin @('production','default','prod')
}
function Managed-RemovalFile([string]$Path,[string]$Config,[string]$Cache) {
    if (Managed-DataFile $Path $Config $Cache) { return $true }
    $nsPrefix = (Join-Path $Config 'namespaces') + '\'
    if ($Path.StartsWith($nsPrefix,[StringComparison]::OrdinalIgnoreCase)) {
        $name = $Path.Substring($nsPrefix.Length).Split('\')[0]
        if (Managed-Namespace $name) {
            $root = Join-Path $nsPrefix $name
            return (Managed-DataFile $Path $root $root)
        }
    }
    $cachePrefix = (Join-Path $Cache 'announcement-webview') + '\'
    if ($Path.StartsWith($cachePrefix,[StringComparison]::OrdinalIgnoreCase)) {
        $segments = $Path.Substring($cachePrefix.Length).Split('\')
        if ($segments.Count -ge 3 -and $segments[1] -ceq 'EBWebView') {
            $identity = $segments[0]
            return $identity -ceq 'production' -or ($identity.StartsWith('ns-',[StringComparison]::Ordinal) -and
                $identity -ceq $identity.ToLowerInvariant() -and (Managed-Namespace $identity.Substring(3)))
        }
    }
    return $false
}
function Existing-FilesMatch($Before,$After) {
    $priorFiles = @($Before | Where-Object Kind -eq 'File')
    $priorPaths = @($priorFiles | ForEach-Object Path)
    Same-Inventory $priorFiles @($After | Where-Object { $_.Kind -eq 'File' -and $_.Path -in $priorPaths })
}
if ($env:OS -ne 'Windows_NT') { throw 'Windows only.' }
if ($Namespace.Length -gt 64 -or $Namespace -notmatch '^[A-Za-z0-9_-][A-Za-z0-9._-]*$' -or $Namespace.EndsWith('.') -or $Namespace.Contains('..') -or $Namespace -in @('prod','production','default')) { throw 'Invalid isolated namespace.' }
$Directory = [IO.Path]::GetFullPath($Directory).TrimEnd('\')
$EvidenceRoot = [IO.Path]::GetFullPath($EvidenceRoot).TrimEnd('\')
Assert-SafePath $Directory; Assert-SafePath $EvidenceRoot
if ($EvidenceRoot.StartsWith($Directory + '\',[StringComparison]::OrdinalIgnoreCase) -or $EvidenceRoot -eq $Directory) { throw 'Evidence must be outside target directory.' }
$config = Join-Path $env:APPDATA 'Sidravia'
$cache = Join-Path $env:LOCALAPPDATA 'Sidravia'
$nsRoot = Join-Path $config ('namespaces\' + $Namespace)
foreach ($p in @($config,$cache)) {
    if ($EvidenceRoot -eq $p -or $EvidenceRoot.StartsWith($p+'\',[StringComparison]::OrdinalIgnoreCase)) { throw 'Evidence must be outside product data.' }
}
Assert-SafePath $CandidateManifest
Assert-SafePath $CandidateInstaller
if (![IO.Path]::GetExtension($CandidateInstaller).Equals('.exe',[StringComparison]::OrdinalIgnoreCase)) { throw 'Candidate installer must be an EXE.' }
$installerHash = (Get-FileHash -LiteralPath $CandidateInstaller -Algorithm SHA256).Hash
$manifestHash = (Get-FileHash -LiteralPath $CandidateManifest -Algorithm SHA256).Hash
$manifest = Get-Content -Raw -LiteralPath $CandidateManifest | ConvertFrom-Json
if ($manifest.Schema -ne 1 -or $manifest.BuildID -cne $ExpectedBuildID -or @($manifest.Payload).Count -eq 0) { throw 'Invalid candidate manifest.' }
$payload = @(); $seen = @{}
foreach ($entry in $manifest.Payload) {
    $relative = [string]$entry.Path
    if (!$relative -or $relative -match '(^[\\/]|:|(^|[\\/])\.\.?([\\/]|$))' -or $entry.SHA256 -notmatch '^[a-fA-F0-9]{64}$') { throw 'Unsafe manifest payload path/hash.' }
    $full = [IO.Path]::GetFullPath((Join-Path $Directory $relative))
    if (!$full.StartsWith($Directory+'\',[StringComparison]::OrdinalIgnoreCase) -or $seen.ContainsKey($full)) { throw 'Duplicate/escaped manifest payload.' }
    Assert-SafePath $full; $seen[$full]=$true
    $payload += [pscustomobject]@{Path=$full;SHA256=$entry.SHA256}
}
$baseline = $null
$keepOnly = [bool]$KeepDataOnly
if ($Stage -eq 'Before') {
    if (Test-Path -LiteralPath $EvidenceRoot) { throw 'Before requires a new evidence directory.' }
    New-Item -ItemType Directory -Path $EvidenceRoot | Out-Null
} else {
    $baseline = Get-Content -Raw -LiteralPath (Join-Path $EvidenceRoot 'Before.json') | ConvertFrom-Json
    if ($baseline.Directory -ine $Directory -or $baseline.Namespace -cne $Namespace -or $baseline.ExpectedBuildID -cne $ExpectedBuildID -or $baseline.ManifestHash -cne $manifestHash -or $baseline.InstallerSHA256 -cne $installerHash -or $baseline.User -ne [Security.Principal.WindowsIdentity]::GetCurrent().User.Value) { throw 'Baseline directory/namespace/user mismatch.' }
    if (@($baseline.Results | Where-Object Status -eq 'BLOCK').Count) { throw 'Baseline was blocked; use a clean Windows account and a new evidence root.' }
    $keepOnly = $null -ne $baseline.PSObject.Properties['KeepDataOnly'] -and [bool]$baseline.KeepDataOnly
    if ($KeepDataOnly -and !$keepOnly) { throw 'Keep-only mode must be selected in a new Before checkpoint.' }
}
if ($keepOnly -and $Stage -eq 'AfterDelete') { throw 'This baseline permits keep-data acceptance only; do not delete existing user data.' }
$out = Join-Path $EvidenceRoot ($Stage + '.json')
if (Test-Path -LiteralPath $out) { throw 'Stage evidence already exists; do not overwrite.' }
$results = New-Object 'Collections.Generic.List[object]'
# Registry snapshots use raw PATH text and value type, never expanded process PATH.
$envKey = Get-Item 'HKCU:\Environment'
$pathExists = $envKey.GetValueNames() -contains 'Path'
$path = if ($pathExists) { [string]$envKey.GetValue('Path',$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) } else { $null }
$pathKind = if ($pathExists) { [string]$envKey.GetValueKind('Path') } else { $null }
$registration = @()
$appIDs = @{ 'windows-amd64' = '{EB2663BD-8DEB-446A-958E-43DF637C1F53}'
             'windows-arm64' = '{63DB10A2-AC2C-4739-9DCA-2DF55C735150}' }
foreach ($targetArch in $appIDs.Keys) {
    foreach ($base in @('HKCU:\Software','HKLM:\Software','HKLM:\Software\WOW6432Node')) {
        $key = Join-Path $base ('Microsoft\Windows\CurrentVersion\Uninstall\' + $appIDs[$targetArch] + '_is1')
        if (Test-Path -LiteralPath $key) {
            $v = Get-ItemProperty -LiteralPath $key
            $registration += [pscustomobject]@{Target=$targetArch;Key=$key;InstallLocation=$v.InstallLocation;DisplayVersion=$v.DisplayVersion}
        }
    }
}
# Enumerate all folders: a same-name task outside root is also a shared-name conflict.
$tasks = @(Get-ScheduledTask -ErrorAction Stop | Where-Object TaskName -eq 'SidraviaDaemon' | Sort-Object TaskPath | ForEach-Object {
    [pscustomobject]@{TaskName=$_.TaskName;TaskPath=$_.TaskPath;Actions=@($_.Actions | Select-Object Execute,Arguments,WorkingDirectory)}
})
$shortcuts = @()
foreach ($p in @((Join-Path ([Environment]::GetFolderPath('Desktop')) 'Sidravia.lnk'),(Join-Path ([Environment]::GetFolderPath('CommonDesktopDirectory')) 'Sidravia.lnk'),(Join-Path ([Environment]::GetFolderPath('Programs')) 'Sidravia'),(Join-Path ([Environment]::GetFolderPath('CommonPrograms')) 'Sidravia'))) { $shortcuts += @(Inventory $p) }
$processes = @(Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object { $_.Name -in @('Sidravia.exe','sidraviad.exe','sidraviactl.exe','sidraviamgr.exe') } | ForEach-Object {
    # Command lines can contain sensitive material: record executable path only.
    [pscustomobject]@{Name=$_.Name;PID=$_.ProcessId;ExecutablePath=$_.ExecutablePath}
})
$target = @(Inventory $Directory)
$data = @(); foreach ($p in @($config,$cache) | Select-Object -Unique) { $data += @(Inventory $p) }
$ns = @($data | Where-Object { $_.Path -ieq $nsRoot -or $_.Path.StartsWith($nsRoot+'\',[StringComparison]::OrdinalIgnoreCase) })
if ($Stage -eq 'Before') {
    foreach ($pair in @(@('AppId',$registration.Count),@('SharedTask',$tasks.Count),@('Shortcuts',$shortcuts.Count),@('Target',$target.Count),@('ExistingProductData',@($data | Where-Object Kind -eq 'File').Count),@('Namespace',$ns.Count),@('ProductProcesses',$processes.Count))) {
        if ($pair[0] -eq 'ExistingProductData' -and $keepOnly) { Result 'DefaultData' 'PASS' 'Existing data recorded as protected baseline. Uninstall must keep data; deletion stage is prohibited.' }
        elseif ($pair[1] -gt 0) { Result $pair[0] 'BLOCK' 'Existing state: stop and resolve the conflict; never migrate or delete automatically.' } else { Result $pair[0] 'PASS' 'Absent.' }
    }
} elseif ($Stage -eq 'Installed') {
    $buildFile = Join-Path $Directory 'BUILD-INFO.txt'
    Assert-SafePath $buildFile
    $buildText = if (Test-Path -LiteralPath $buildFile -PathType Leaf) { Get-Content -Raw -LiteralPath $buildFile } else { '' }
    if ($buildText -notmatch ('(?m)^BuildID[:= ]+\s*' + [regex]::Escape($ExpectedBuildID) + '\s*$')) { Result 'BuildID' 'FAIL' 'Installed BUILD-INFO.txt does not match candidate BuildID.' } else { Result 'BuildID' 'PASS' 'Candidate build matches.' }

    if (!(Test-Path -LiteralPath (Join-Path $Directory 'sidraviamgr.exe')) -or $registration.Count -ne 1 -or $buildText -notmatch ('(?m)^Target: ' + $registration[0].Target + '\s*$') -or $registration[0].InstallLocation.TrimEnd('\') -ine $Directory) { Result 'Installation' 'FAIL' 'Expected manager and matching AppId installation location.' } else { Result 'Installation' 'PASS' 'Files and registration observed.' }
    $payloadBad = @(Payload-Mismatches $payload $target)
    if ($payloadBad.Count) { Result 'CandidatePayload' 'FAIL' 'Manifest payload missing or hash mismatch.' } else { Result 'CandidatePayload' 'PASS' 'All manifest payload files and hashes match.' }
    if ($keepOnly) {
        if (!(Existing-FilesMatch @($baseline.Data) $data)) { Result 'PreexistingDataPreserved' 'FAIL' 'Existing user data changed since Before; stop and preserve evidence.' }
        elseif (@($baseline.Data | Where-Object Kind -eq 'File').Count -eq 0) { Result 'PreexistingDataPreserved' 'SKIP' 'No existing user files to protect.' }
        else { Result 'PreexistingDataPreserved' 'PASS' 'All files observed before installation retained their bytes; new test namespace files are allowed.' }
    }
    Result 'IntegrationChoices' 'MANUAL' 'Compare task action, PATH delta and shortcuts with selected installer options. Actual login/startup remains manual.'
} else {
    foreach ($pair in @(@('AppId',$registration.Count),@('SharedTask',$tasks.Count),@('Shortcuts',$shortcuts.Count),@('ProductProcesses',$processes.Count))) {
        if ($pair[1] -gt 0) { Result $pair[0] 'FAIL' 'State remains; inspect evidence.' } else { Result $pair[0] 'PASS' 'Absent.' }
    }
    if ($pathExists -ne $baseline.PathExists -or $path -cne $baseline.UserPath -or $pathKind -cne $baseline.PathKind) { Result 'PATHRestored' 'FAIL' 'Raw user PATH/value type differs from Before.' } else { Result 'PATHRestored' 'PASS' 'Exact Before value and type restored.' }
    $installedPath = Join-Path $EvidenceRoot 'Installed.json'
    if (!(Test-Path -LiteralPath $installedPath)) { throw 'Installed evidence required for uninstall comparison.' }
    $installed = Get-Content -Raw -LiteralPath $installedPath | ConvertFrom-Json
    if ($installed.Schema -ne 1 -or $installed.Stage -cne 'Installed' -or $installed.Directory -ine $Directory -or $installed.Namespace -cne $Namespace -or $installed.ExpectedBuildID -cne $ExpectedBuildID -or $installed.ManifestHash -cne $manifestHash -or $installed.InstallerSHA256 -cne $installerHash -or $installed.User -cne $baseline.User) {
        throw 'Installed checkpoint identity/manifest mismatch.'
    }
    if ($keepOnly -and (!(Existing-FilesMatch @($baseline.Data) @($installed.Data)) -or !(Existing-FilesMatch @($baseline.Data) $data))) {
        Result 'PreexistingDataPreserved' 'FAIL' 'Existing user data changed since Before; cleanup success cannot override this failure.'
    }
    if (@($installed.Results | Where-Object { $_.Status -in @('FAIL','BLOCK') }).Count) {
        Result 'InstallationWasVerified' 'FAIL' 'Installed checkpoint failed; cleanup checks do not prove a complete installation cycle.'
    } else {
        Result 'InstallationWasVerified' 'PASS' 'Installed checkpoint had no failed automatic checks; manual choices remain separate.'
    }
    $owned = @($payload | ForEach-Object { [pscustomobject]@{Path=$_.Path} })
    $owned += @($installed.Target | Where-Object { $_.Kind -eq 'File' -and [IO.Path]::GetDirectoryName($_.Path) -ieq $Directory -and [IO.Path]::GetFileName($_.Path) -match '^unins[0-9]{3}\.(exe|dat|msg)$' })
    $remaining = @($owned | Where-Object { Test-Path -LiteralPath $_.Path })
    if ($remaining.Count) { Result 'ProgramResidue' 'FAIL' 'Installed payload files remain; inspect Target inventory.' } else { Result 'ProgramResidue' 'PASS' 'Manifest payload and observed installer uninstaller files absent; unknown user files may remain.' }

    $ownedPaths = @($owned | ForEach-Object Path)
    $unknownBefore = @($installed.Target | Where-Object { $_.Kind -eq 'File' -and $_.Path -notin $ownedPaths })
    $unknownNow = @($target | Where-Object { $_.Kind -eq 'File' -and $_.Path -notin $ownedPaths })
    if (!(Same-Inventory $unknownBefore $unknownNow)) { Result 'UnknownTargetFilesPreserved' 'FAIL' 'Non-payload target file inventory changed.' } elseif (!$unknownBefore.Count) { Result 'UnknownTargetFilesPreserved' 'SKIP' 'No unknown target files observed.' } else { Result 'UnknownTargetFilesPreserved' 'PASS' 'Observed unknown target files unchanged.' }
    if ($Stage -eq 'AfterKeep') {
        if (!(Same-Inventory @($installed.Data) $data)) { Result 'KeepData' 'FAIL' 'Data inventory changed since Installed.' } elseif (@($installed.Data | Where-Object Kind -eq 'File').Count -eq 0) { Result 'KeepData' 'SKIP' 'No data files observed before uninstall; retention not proved.' } else { Result 'KeepData' 'PASS' 'Observed data files unchanged.' }
    } else {
        $victims = @($installed.Data | Where-Object { $_.Kind -eq 'File' -and (Managed-RemovalFile $_.Path $config $cache) })
        $nowVictims = @($data | Where-Object { $_.Kind -eq 'File' -and (Managed-RemovalFile $_.Path $config $cache) })
        if ($nowVictims.Count) { Result 'DeleteManagedData' 'FAIL' 'Managed files remain.' }
        elseif (!$victims.Count) { Result 'DeleteManagedData' 'SKIP' 'No managed files observed before uninstall.' }
        else { Result 'DeleteManagedData' 'PASS' 'Observed managed files removed.' }
        $protected = @($installed.Data | Where-Object { $_.Kind -eq 'File' -and !(Managed-RemovalFile $_.Path $config $cache) })
        $nowProtected = @($data | Where-Object { $_.Kind -eq 'File' -and !(Managed-RemovalFile $_.Path $config $cache) })
        if (!(Same-Inventory $protected $nowProtected)) { Result 'NonWhitelistPreserved' 'FAIL' 'Unrelated file inventory changed.' }
        elseif (!$protected.Count) { Result 'NonWhitelistPreserved' 'SKIP' 'No unrelated files observed.' }
        else { Result 'NonWhitelistPreserved' 'PASS' 'Unrelated files unchanged.' }
    }
}
# Exact token sequence delta, retaining duplicates and empty PATH elements.
$beforeTokens = @(); if ($null -ne $baseline -and $baseline.PathExists) { $beforeTokens = @(([string]$baseline.UserPath).Split(';')) }
$tokens = @(); if ($pathExists) { $tokens = @($path.Split(';')) }
$snapshot = [ordered]@{Schema=1;ExpectedBuildID=$ExpectedBuildID;ManifestHash=$manifestHash;InstallerSHA256=$installerHash;Stage=$Stage;KeepDataOnly=$keepOnly;Time=(Get-Date).ToString('o');User=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value;Directory=$Directory;Namespace=$Namespace;PathExists=$pathExists;UserPath=$path;PathKind=$pathKind;BeforePathTokens=$beforeTokens;CurrentPathTokens=$tokens;Registration=$registration;Tasks=$tasks;Shortcuts=$shortcuts;Processes=$processes;Target=$target;Data=$data;NamespaceData=$ns;Results=$results.ToArray()}
$snapshot | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $out -Encoding UTF8
$results | Format-Table -AutoSize
Write-Host "Evidence: $out (contains file names/hashes and user PATH; review before sharing)"
if (@($results | Where-Object { $_.Status -in @('BLOCK','FAIL') }).Count) { exit 2 }
Write-Host 'Manual checks and SKIP items are not acceptance PASS.'
