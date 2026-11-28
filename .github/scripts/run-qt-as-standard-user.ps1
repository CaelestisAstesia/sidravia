param([Parameter(Mandatory)][string]$Work,
      [Parameter(Mandatory)][string]$ChildScript,
      [ValidateRange(20,1800)][int]$TimeoutSeconds = 90)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$workPath = (Resolve-Path -LiteralPath $Work).Path
$scriptPath = (Resolve-Path -LiteralPath $ChildScript).Path
if (!$scriptPath.StartsWith($workPath + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'The test script must be in its dedicated workspace.'
}
$id = [Guid]::NewGuid().ToString('N')
$name = 'sqt-' + $id.Substring(0,12)
$password = ConvertTo-SecureString ($id + 'sQ8!') -AsPlainText -Force
$account = New-LocalUser -Name $name -Password $password -Description 'Disposable Sidravia Qt acceptance user'
try {
    Add-LocalGroupMember -SID 'S-1-5-32-545' -Member $account
    $sid = $account.SID.Value
    icacls.exe $workPath /grant "*${sid}:(OI)(CI)M"
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant fixture workspace access.' }
    $credential = [PSCredential]::new("$env:COMPUTERNAME\$name",$password)
    $pwsh = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $child = Start-Process -FilePath $pwsh -ArgumentList @(
        '-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass',
        '-File',('"' + $scriptPath + '"')
    ) -WorkingDirectory $workPath -Credential $credential -LoadUserProfile -PassThru
    if (!$child.WaitForExit($TimeoutSeconds * 1000)) {
        # This is the sole child we started, and the enclosing runner is disposable.
        $child.Kill()
        $child.WaitForExit()
        throw 'The standard-user acceptance process timed out.'
    }
    if ($child.ExitCode -ne 0) { throw "The standard-user acceptance process exited $($child.ExitCode)." }
} finally {
    Remove-LocalUser -Name $name
}
