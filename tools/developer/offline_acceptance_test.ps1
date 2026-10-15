# Isolated helper-contract checks; never inspects installation state or launches GUI.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2
$scriptPath = Join-Path $PSScriptRoot 'offline_acceptance.ps1'
$tokens=$null; $errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($scriptPath,[ref]$tokens,[ref]$errors)
if ($errors.Count) { throw 'Parse failed.' }
foreach ($name in @('Assert-SafePath','Inventory','Equal-Json','Same-Inventory','Payload-Mismatches','Existing-FilesMatch')) {
    $node=$ast.Find({param($a) $a -is [Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $name},$true)
    . ([scriptblock]::Create($node.Extent.Text))
}
# Evaluate the production Results expression on PS5.1: @($genericList)
# can throw before evidence is saved, even when all inventory checks succeeded.
$results=New-Object 'Collections.Generic.List[object]'
$results.Add([pscustomobject]@{Check='fixture';Status='BLOCK';Detail='fixture'})
$snapshotNode=$ast.Find({param($a) $a -is [Management.Automation.Language.HashtableAst] -and $a.Extent.Text.StartsWith('@{Schema=1;ExpectedBuildID=')},$true)
$resultEntry=@($snapshotNode.KeyValuePairs | Where-Object { $_.Item1.Value -eq 'Results' })[0]
$savedResults=. ([scriptblock]::Create($resultEntry.Item2.Extent.Text))
if (@($savedResults).Count -ne 1 -or $savedResults.Status -ne 'BLOCK') { throw 'Evidence result serialization failed.' }
$deleteGuard=$ast.Find({param($a) $a -is [Management.Automation.Language.IfStatementAst] -and $a.Extent.Text.Contains("throw 'This baseline permits keep-data acceptance only;")},$true)
$keepOnly=$true; $Stage='AfterDelete'; $refused=$false
try { . ([scriptblock]::Create($deleteGuard.Extent.Text)) } catch { $refused=$true }
if (!$refused) { throw 'Keep-only baseline accepted deletion stage.' }
$Stage='AfterKeep'; . ([scriptblock]::Create($deleteGuard.Extent.Text))
$root=Join-Path ([IO.Path]::GetTempPath()) ('sidravia-offline-test-'+[guid]::NewGuid().ToString('N'))
try {
    New-Item -ItemType Directory -Path $root | Out-Null
    $file=Join-Path $root 'configurations.json'
    Set-Content -LiteralPath $file -Value '{"fake":"test-only"}' -Encoding UTF8
    $first=@(Inventory $root)
    if ($first.Count -ne 2 -or @($first | Where-Object Kind -eq 'File')[0].Hash.Length -ne 64) { throw 'Inventory mismatch.' }
    if (!(Same-Inventory $first @(Inventory $root))) { throw 'Stable inventory differs.' }
    $manifest=@([pscustomobject]@{Path=$file;SHA256=($first | Where-Object Kind -eq 'File').Hash})
    if (@(Payload-Mismatches $manifest $first).Count) { throw 'Valid payload rejected.' }
    if (@(Payload-Mismatches $manifest @()).Count -ne 1) { throw 'Missing payload accepted.' }
    # Exercise the real namespace filtering statements with Windows paths,
    # including a user name that itself contains a namespaces component.
    $config='C:\Users\namespaces\AppData\Roaming\Sidravia'
    $cache='C:\Users\namespaces\AppData\Local\Sidravia'
    $data=@([pscustomobject]@{Path=($config+'\gui-settings.json')},[pscustomobject]@{Path=($config+'\namespaces\old\gui-settings.json')})
    foreach ($variable in @('$namespaceParents','$default')) {
        $node=$ast.Find({param($a) $a -is [Management.Automation.Language.AssignmentStatementAst] -and $a.Left.Extent.Text -eq $variable},$true)
        . ([scriptblock]::Create($node.Extent.Text))
    }
    if ($default.Count -ne 1 -or $default[0].Path -cne ($config+'\gui-settings.json')) { throw 'Default data/namespace boundary mismatch.' }
    $other=Join-Path $root 'unknown.txt'; Set-Content -LiteralPath $other -Value 'keep'
    if (@(Payload-Mismatches $manifest @(Inventory $root)).Count) { throw 'Unknown file rejected.' }
    $unknownBefore=@(Inventory $other)
    if (!(Existing-FilesMatch $unknownBefore @(Inventory $root))) { throw 'New test data incorrectly changes existing-file baseline.' }
    Remove-Item -LiteralPath $other
    if (Existing-FilesMatch $unknownBefore @(Inventory $root)) { throw 'Preexisting file deletion accepted.' }
    if (Same-Inventory $unknownBefore @(Inventory $other)) { throw 'Unknown deletion undetected.' }
    Set-Content -LiteralPath $file -Value '{"fake":"changed"}' -Encoding UTF8
    if (Existing-FilesMatch $first @(Inventory $root)) { throw 'Preexisting user data modification accepted.' }
    if (Same-Inventory $first @(Inventory $root)) { throw 'Changed file went undetected.' }
    if (@(Inventory (Join-Path $root 'absent')).Count -ne 0) { throw 'Absent path mismatch.' }
    $link=Join-Path $root 'junction'
    New-Item -ItemType Junction -Path $link -Target $root | Out-Null
    $refused=$false
    try { @(Inventory $link) | Out-Null } catch { $refused=$true }
    if (!$refused) { throw 'Junction not refused.' }
    if (@(Inventory (Join-Path $link 'absent-shortcut.lnk')).Count) { throw 'Absent linked-parent path has content.' }
    # Remove only the exact junction created by this test; never recursively follow it.
    [IO.Directory]::Delete($link)
    Write-Output 'PASS: PS5.1 inventory stability/change/absent/reparse, missing payload and unknown-file contracts.'
} finally {
    $link=Join-Path $root 'junction'
    if (Test-Path -LiteralPath $link) { [IO.Directory]::Delete($link) }
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
