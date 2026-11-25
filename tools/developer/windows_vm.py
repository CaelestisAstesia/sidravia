"""Fedora/QGA transport for the existing Windows development commands.

No credentials, login automation, network server or VM reconfiguration.
"""
from __future__ import annotations

import base64
import contextlib
import hashlib
import io
import json
import os
import re
import sys
from pathlib import Path, PurePosixPath, PureWindowsPath
import subprocess
import time
import uuid
import zipfile

import validation_run as runner

POWERSHELL = r'C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe'
MARKER = 'sidravia-vm-development-v1'


def ps_quote(value):
    return "'" + value.replace("'", "''") + "'"


class Guest:
    def __init__(self, uri, domain):
        self.base = ['virsh', '-c', uri, 'qemu-agent-command', domain, '--timeout', '30']

    def call(self, name, arguments=None):
        message = {'execute': name}
        if arguments is not None:
            message['arguments'] = arguments
        result = subprocess.run(self.base + [json.dumps(message)], capture_output=True, encoding='utf-8', timeout=35)
        if result.returncode:
            raise RuntimeError('QGA transport failed: ' + result.stderr[-1000:])
        reply = json.loads(result.stdout)
        if 'error' in reply:
            raise RuntimeError('QGA ' + name + ': ' + str(reply['error']))
        return reply['return']

    def powershell(self, script, timeout=60):
        script = "$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);\n" + script
        pid = self.call('guest-exec', {'path': POWERSHELL, 'arg': ['-NoProfile', '-NonInteractive', '-EncodedCommand', base64.b64encode(script.encode('utf-16le')).decode()], 'capture-output': True})['pid']
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            status = self.call('guest-exec-status', {'pid': pid})
            if status.get('exited'):
                out = base64.b64decode(status.get('out-data', '')).decode('utf-8-sig', errors='replace')
                err = base64.b64decode(status.get('err-data', '')).decode('utf-8', errors='replace')
                if status.get('exitcode') != 0 or status.get('out-truncated') or status.get('err-truncated'):
                    raise RuntimeError('guest control command failed ' + str({k:v for k,v in status.items() if k not in ('out-data', 'err-data')}) + ': ' + (err or out)[-1500:])
                return out.strip()
            time.sleep(.2)
        raise RuntimeError('guest control command timed out; execution outcome unknown')

    def put(self, path, data):
        handle = self.call('guest-file-open', {'path': path, 'mode': 'wb'})
        try:
            for start in range(0, len(data), 49152):
                part = data[start:start + 49152]
                if self.call('guest-file-write', {'handle': handle, 'buf-b64': base64.b64encode(part).decode()})['count'] != len(part):
                    raise RuntimeError('short guest file write')
        finally:
            self.call('guest-file-close', {'handle': handle})

    def get(self, path, limit=64 * 1024 * 1024):
        handle = self.call('guest-file-open', {'path': path, 'mode': 'rb'})
        data = bytearray()
        try:
            while True:
                row = self.call('guest-file-read', {'handle': handle, 'count': 49152})
                data.extend(base64.b64decode(row.get('buf-b64', '')))
                if len(data) > limit:
                    raise RuntimeError('guest evidence exceeds transfer limit: ' + path)
                if row.get('eof') or not row.get('count'):
                    return bytes(data)
        finally:
            self.call('guest-file-close', {'handle': handle})


def snapshot(root, manifest):
    """Copy exact manifest bytes; refuse Windows aliases before sending anything."""
    seen = set()
    stream = io.BytesIO()
    with zipfile.ZipFile(stream, 'w', zipfile.ZIP_DEFLATED) as archive:
        for item in manifest['files']:
            name = item['path']
            parts = PurePosixPath(name).parts
            if not parts or any(p in ('.', '..') or ':' in p or '\\' in p or p.endswith((' ', '.')) or p.split('.')[0].upper() in {'CON', 'PRN', 'AUX', 'NUL', *('COM'+str(i) for i in range(1,10)), *('LPT'+str(i) for i in range(1,10))} for p in parts) or name.startswith('/') or parts[0] in ('.git', '.project', '.agents', '.local') or name.casefold() in seen:
                raise ValueError('unsafe or case-colliding Windows source path: ' + name)
            seen.add(name.casefold())
            path = root / name
            if any(p.is_symlink() for p in (path, *path.parents)):
                raise ValueError('linked source is unsupported: ' + name)
            data = path.read_bytes()
            if hashlib.sha256(data).hexdigest() != item['sha256']:
                raise ValueError('source changed while archiving: ' + name)
            archive.writestr(name, data)
    return stream.getvalue()


def expand_request(request, substitutions):
    request = runner.validate_request(request)
    def expand(value):
        for key, replacement in substitutions.items():
            value = value.replace('{' + key + '}', replacement)
        return value
    result = dict(request)
    result['environment'] = {k: expand(v) for k, v in request.get('environment', {}).items()}
    result['stages'] = []
    for original in request['stages']:
        stage = dict(original)
        stage['argv'] = [expand(a) for a in stage['argv']]
        relative = stage.get('cwd', '.')
        if PureWindowsPath(relative).root or PureWindowsPath(relative).is_absolute() or '..' in PureWindowsPath(relative).parts or ':' in relative:
            raise ValueError('VM cwd must be relative to the source snapshot')
        stage['cwd'] = str(PureWindowsPath(substitutions['source']) / relative)
        stage['environment'] = {k: expand(v) for k, v in stage.get('environment', {}).items()}
        result['stages'].append(stage)
    return result


@contextlib.contextmanager
def host_lock(directory, domain):
    import fcntl
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / ('vm-' + hashlib.sha256(domain.encode()).hexdigest()[:16] + '.lock')
    with path.open('a') as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError('another development runner owns this VM') from None
        yield


def cleanup_native_updates(guest, root, owner, sid):
    """Stop only exact-handle processes in this run's owned fixture tree.

    A normal pass must leave none. Stopping a leaked process is reported as a
    failed acceptance, not hidden behind task removal or VM shutdown.
    """
    script = r"""$ErrorActionPreference='Stop'
$r=@ROOT@;$owner=@OWNER@;$sid=@SID@
$part=$r
while($part){if((Test-Path -LiteralPath $part) -and ((Get-Item -LiteralPath $part -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'linked native fixture root'};$part=[IO.Path]::GetDirectoryName($part)}
$marker=Join-Path $r '.sidravia-native-owner'
if(!(Test-Path -LiteralPath $marker -PathType Leaf) -or ((Get-Item -LiteralPath $marker -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -or [IO.File]::ReadAllText($marker) -cne $owner){throw 'native fixture ownership unavailable'}
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SidraviaNativeToken {
 [DllImport("advapi32.dll", SetLastError=true)] public static extern bool OpenProcessToken(IntPtr process,uint access,out IntPtr token);
 [DllImport("kernel32.dll")] public static extern bool CloseHandle(IntPtr handle);
}
'@
function NativePath($path){
 if($path.StartsWith('\\?\')){$path=$path.Substring(4)}
 return [IO.Path]::GetFullPath($path)
}
function InNativeScope($path){return (NativePath $path).StartsWith(($r.TrimEnd('\')+'\'),[StringComparison]::OrdinalIgnoreCase)}
function AssertNoLinks($path){
 $part=$path
 while($part){if((Get-Item -LiteralPath $part -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'linked native executable path'};$part=[IO.Path]::GetDirectoryName($part)}
}
$stopped=@()
for($pass=0;$pass -lt 5;$pass++){
 $rows=@(Get-CimInstance Win32_Process | Where-Object {$_.ExecutablePath -and (InNativeScope $_.ExecutablePath)})
 if(!$rows.Count){break}
 foreach($row in $rows){
  $p=$null
  try{
   $p=[Diagnostics.Process]::GetProcessById([int]$row.ProcessId)
   $handle=$p.Handle
   $path=$p.MainModule.FileName
   if(!(InNativeScope $path)){throw 'native process identity changed'}
   AssertNoLinks (NativePath $path)
   $token=[IntPtr]::Zero
   if(![SidraviaNativeToken]::OpenProcessToken($handle,8,[ref]$token)){throw 'native process token unavailable'}
   try{$identity=[Security.Principal.WindowsIdentity]::new($token);try{if($identity.User.Value -cne $sid){throw 'native process owner mismatch'}}finally{$identity.Dispose()}}finally{[void][SidraviaNativeToken]::CloseHandle($token)}
   $created=$p.StartTime.ToUniversalTime().ToString('o')
   $id=$p.Id
   $p.Kill()
   if(!$p.WaitForExit(5000)){throw 'native process did not exit'}
   $stopped+=@{pid=$id;created=$created;path=$path}
  }catch{
   if($p -and $p.HasExited){continue}
   if(!$p -and $_.Exception.GetBaseException() -is [ArgumentException] -and !(Get-Process -Id ([int]$row.ProcessId) -ErrorAction SilentlyContinue)){continue}
   throw
  }finally{if($p){$p.Dispose()}}
 }
}
$left=@(Get-CimInstance Win32_Process | Where-Object {$_.ExecutablePath -and (InNativeScope $_.ExecutablePath)})
if($left.Count){throw 'native fixture processes still running'}
@{verified=$true;stopped=@($stopped);remaining=0} | ConvertTo-Json -Depth 5 -Compress
"""
    script = script.replace('@ROOT@', ps_quote(root)).replace('@OWNER@', ps_quote(owner)).replace('@SID@', ps_quote(sid))
    return json.loads(guest.powershell(script, timeout=60))


def run(root, args, manifest, source_after):
    guest = Guest(args.uri, args.domain)
    run_id = uuid.uuid4().hex
    task = 'Sidravia-Dev-' + run_id
    guest_root = str(PureWindowsPath(args.guest_root))
    if not PureWindowsPath(guest_root).is_absolute() or guest_root.startswith('\\\\') or len(PureWindowsPath(guest_root).parts) < 3 or '..' in PureWindowsPath(guest_root).parts:
        raise ValueError('guest root must be a dedicated absolute local directory')
    report_dir = Path(args.reports).resolve()
    report_dir.mkdir(parents=True, exist_ok=True)
    output = report_dir / ('vm-' + run_id)
    output.mkdir()
    remote = guest_root + '\\runs\\' + run_id
    source = remote + '\\source'
    request = expand_request(runner.read_json(args.request), {'source': source, 'run': remote, 'workspace': guest_root + '\\build'})
    native_updates = getattr(args, 'native_update_fixtures', False)
    native_root = guest_root + '\\fx-native-' + run_id[:12]
    native_owner = 'sidravia-native-update-v1:' + run_id
    if native_updates:
        request.setdefault('environment', {}).update(SIDRAVIA_INNO_TEST_ROOT=native_root, SIDRAVIA_WINDOWS_PORTABLE_TEST_ROOT=native_root, SIDRAVIA_NATIVE_JOB_PROBE_FIXTURE='1')
    identity = {'commit': manifest['source_commit'], 'dirty': manifest['dirty'], 'manifest_sha256': hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest(), 'file_count': len(manifest['files'])}
    request['source'] = identity
    payload = snapshot(root, manifest)
    receipt = {'schema': 1, 'kind': 'windows-vm', 'status': 'failed', 'source': identity, 'stages': [], 'guest_run': remote, 'task': task, 'cleanup': 'not_started'}
    started = time.monotonic()
    task_created = False
    native_created = False
    if native_updates:
        receipt.update(native_update_fixtures=True, native_fixture_root=native_root)
    common = subprocess.check_output(['git', 'rev-parse', '--git-common-dir'], cwd=root, text=True).strip()
    with host_lock((root / common).resolve() / 'development-locks', args.uri + ':' + args.domain):
        try:
            # Only this tool's dedicated directory is mutable. No account or
            # global policy changes, and no attempt to log in a missing user.
            setup = f"""
$r={ps_quote(guest_root)};$user={ps_quote(args.guest_user)}
$part=$r
while($part){{if((Test-Path -LiteralPath $part) -and ((Get-Item -LiteralPath $part -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){{throw 'linked VM root'}};$part=[IO.Path]::GetDirectoryName($part)}}
$sid=([Security.Principal.NTAccount]::new($user)).Translate([Security.Principal.SecurityIdentifier]).Value
$profile=Get-CimInstance Win32_UserProfile | Where-Object {{$_.SID -eq $sid -and $_.Loaded}}
if($profile -and !$r.StartsWith(($profile.LocalPath.TrimEnd('\\')+'\\'),[StringComparison]::OrdinalIgnoreCase)){{throw 'VM root must be below the requested user profile'}}
$session=@(Get-Process explorer -IncludeUserName -ErrorAction SilentlyContinue | Where-Object {{$_.UserName -ieq $user -or $_.UserName -ilike ('*\\'+$user)}})
if(!$profile -or !$session.Count){{throw 'requested ordinary-user desktop is not logged in'}}
if(!(Test-Path -LiteralPath $r)){{$null=New-Item -ItemType Directory -Path $r}}
$marker=Join-Path $r '.sidravia-vm-owner'
if(!(Test-Path -LiteralPath $marker)){{if(@(Get-ChildItem -LiteralPath $r -Force).Count){{throw 'unowned VM root'}};[IO.File]::WriteAllText($marker,{ps_quote(MARKER)})}}
if([IO.File]::ReadAllText($marker) -cne {ps_quote(MARKER)}){{throw 'VM root marker mismatch'}}
if(@(Get-ScheduledTask -TaskName 'Sidravia-Dev-*' -ErrorAction SilentlyContinue | Where-Object {{$_.State -eq 'Running'}}).Count){{throw 'another VM development run is active'}}
$runs=Join-Path $r 'runs'
if((Test-Path -LiteralPath $runs) -and ((Get-Item -LiteralPath $runs -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){{throw 'linked runs directory'}}
$remote={ps_quote(remote)}
if(Test-Path -LiteralPath $remote){{throw 'run directory already exists'}}
$null=New-Item -ItemType Directory -Path $remote
@{{user=$user;sid=$sid;session_ids=@($session.SessionId)}} | ConvertTo-Json -Compress
"""
            preflight = json.loads(guest.powershell(setup))
            receipt['preflight'] = preflight
            if native_updates:
                guest.powershell(f"""
$r={ps_quote(native_root)}
if(Test-Path -LiteralPath $r){{throw 'native fixture root already exists'}}
$null=New-Item -ItemType Directory -Path $r
[IO.File]::WriteAllText((Join-Path $r '.sidravia-native-owner'),{ps_quote(native_owner)})
""")
                native_created = True

            print('VM preflight passed; transferring source snapshot', file=sys.stderr, flush=True)
            guest.put(remote + '\\source.zip', payload)
            guest.put(remote + '\\manifest.json', (json.dumps(manifest) + '\n').encode())
            guest.put(remote + '\\request.json', (json.dumps(request, ensure_ascii=False) + '\n').encode())
            runner.write_json(output / 'request.json', request)
            runner.write_json(output / 'source-manifest.json', manifest)
            # Run in the existing InteractiveToken, never as QGA's SYSTEM token.
            launcher = f"""
$ErrorActionPreference='Stop'
{'$env:SIDRAVIA_NATIVE_OUTER_JOB='+ps_quote('Local\\Sidravia-Native-'+run_id) if native_updates else ''}
. {ps_quote(source + r'\tools\developer\process_job.ps1')}{' -AllowBreakaway' if native_updates else ''}
& git -C {ps_quote(source)} init -q
if($LASTEXITCODE){{exit $LASTEXITCODE}}
& git -C {ps_quote(source)} add --all
if($LASTEXITCODE){{exit $LASTEXITCODE}}
& {ps_quote(args.windows_python)} {ps_quote(source + r'\tools\developer\validation_run.py')} --request {ps_quote(remote + r'\request.json')} --output {ps_quote(remote)} --format json{' --native-update-fixtures' if native_updates else ''}
exit $LASTEXITCODE
"""
            # Extraction and verification happen before task registration.
            bootstrap = f"""
Expand-Archive -LiteralPath {ps_quote(remote + r'\source.zip')} -DestinationPath {ps_quote(source)}
$m=Get-Content -LiteralPath {ps_quote(remote + r'\manifest.json')} -Raw -Encoding UTF8 | ConvertFrom-Json
foreach($f in $m.files){{$p=Join-Path {ps_quote(source)} $f.path;if((Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash.ToLowerInvariant() -cne $f.sha256){{throw 'transferred source hash mismatch'}}}}
# The ordinary user owns the snapshot root; git creates its index in that
# user's task. No global safe.directory exception or elevated tests.
$acl=Get-Acl -LiteralPath {ps_quote(source)}
$acl.SetOwner([Security.Principal.NTAccount]::new({ps_quote(args.guest_user)}))
Set-Acl -LiteralPath {ps_quote(source)} -AclObject $acl
$null=New-Item -ItemType Directory -Path {ps_quote(remote + r'\fixtures')}
"""
            guest.powershell(bootstrap, timeout=120)
            guest.put(remote + '\\run.ps1', launcher.encode('utf-8-sig'))
            # Set this before registration: an interrupted response may still
            # mean registration succeeded. Finally always resolves that state.
            task_created = True
            guest.powershell(f"""
$a=New-ScheduledTaskAction -Execute {ps_quote(POWERSHELL)} -Argument {ps_quote('-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + remote + r'\run.ps1"')}
$p=New-ScheduledTaskPrincipal -UserId {ps_quote(args.guest_user)} -LogonType Interactive -RunLevel Limited
$s=New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds {int(args.timeout)})
Register-ScheduledTask -TaskName {ps_quote(task)} -Action $a -Principal $p -Settings $s -ErrorAction Stop | Out-Null
Start-ScheduledTask -TaskName {ps_quote(task)}
""")
            deadline = time.monotonic() + args.timeout
            last_stage = None
            while time.monotonic() < deadline:
                state = json.loads(guest.powershell(f"""
$t=Get-ScheduledTask -TaskName {ps_quote(task)};$i=Get-ScheduledTaskInfo -TaskName {ps_quote(task)}
@{{running=($t.State -eq 'Running');result=$i.LastTaskResult;report=(Test-Path -LiteralPath {ps_quote(remote + r'\report.json')})}} | ConvertTo-Json -Compress
"""))
                if state['report']:
                    try:
                        progress = json.loads(guest.get(remote + '\\report.json', 4 * 1024 * 1024).decode('utf-8-sig'))
                        stage = next((s['name'] for s in progress.get('stages', []) if s['status'] == 'running'), None)
                        if stage and stage != last_stage:
                            print('VM running ' + stage, file=sys.stderr, flush=True)
                            last_stage = stage
                    except (RuntimeError, json.JSONDecodeError):
                        pass  # The atomic report may be replacing; final collection is strict.
                if not state['running']:
                    receipt['task_result'] = state['result']
                    if state['report']:
                        break
                    if state['result'] == 267011:
                        time.sleep(1)
                        continue
                    raise RuntimeError('guest task ended without a report: ' + str(state['result']))
                time.sleep(1)
            else:
                raise TimeoutError('VM run deadline exceeded')
        except BaseException as error:
            receipt.update(status='interrupted' if not isinstance(error, Exception) else 'failed', error=type(error).__name__ + ': ' + str(error))
            if not isinstance(error, Exception):
                raise
        finally:
            if task_created:
                try:
                    guest.powershell(f"""
$t=Get-ScheduledTask -TaskName {ps_quote(task)} -ErrorAction SilentlyContinue
if($t){{if($t.State -eq 'Running'){{Stop-ScheduledTask -TaskName {ps_quote(task)}}};Start-Sleep -Milliseconds 300
if((Get-ScheduledTask -TaskName {ps_quote(task)}).State -eq 'Running'){{throw 'owned task still running'}}
Unregister-ScheduledTask -TaskName {ps_quote(task)} -Confirm:$false}}
""")
                    receipt['cleanup'] = 'task_removed'
                except Exception as error:
                    receipt.update(status='failed', cleanup='unknown', cleanup_error=str(error))
            if native_created:
                try:
                    native = cleanup_native_updates(guest, native_root, native_owner, preflight['sid'])
                    receipt['native_process_cleanup'] = native
                    if native['stopped']:
                        receipt.update(status='failed', cleanup_error='native fixture leaked processes; exact owned processes were stopped')
                except Exception as error:
                    receipt.update(status='failed', native_process_cleanup='unknown', cleanup_error=str(error))
            # Collect after the wrapper/job has exited, including the partial
            # report on timeout, cancellation or transport failures.
            if task_created and receipt['cleanup'] == 'task_removed':
                try:
                    collected = json.loads(guest.get(remote + '\\report.json', 4 * 1024 * 1024).decode('utf-8-sig'))
                    receipt.update(stages=collected['stages'], guest_duration_seconds=collected.get('duration_seconds'))
                    if not any(k in receipt for k in ('error', 'cleanup_error')):
                        receipt['status'] = 'interrupted' if collected['status'] == 'running' else collected['status']
                        if receipt['status'] == 'passed' and receipt.get('task_result') != 0:
                            receipt.update(status='failed', error='guest task exit code disagrees with the passed report')
                    total = 0
                    for row in receipt['stages']:
                        if row['status'] == 'running':
                            row['status'] = 'interrupted'
                        for stream in ('stdout', 'stderr'):
                            name = row.get(stream)
                            if name:
                                if not re.fullmatch(r'[A-Za-z0-9_-]+\.(stdout|stderr)\.log', name):
                                    raise ValueError('invalid guest log name')
                                data = guest.get(remote + '\\' + name)
                                total += len(data)
                                if total > 128 * 1024 * 1024:
                                    raise ValueError('total evidence exceeds 128 MiB')
                                (output / name).write_bytes(data)
                    receipt['evidence'] = 'collected'
                except Exception as error:
                    receipt.update(status='failed', evidence='incomplete', evidence_error=str(error))
            try:
                receipt['host_source_unchanged'] = source_after() == manifest
                if task_created and receipt['cleanup'] == 'task_removed':
                    receipt['guest_source_unchanged'] = guest.powershell(f"""
$m=Get-Content -LiteralPath {ps_quote(remote + r'\manifest.json')} -Raw -Encoding UTF8 | ConvertFrom-Json
$ok=$true
foreach($f in $m.files){{
$p=Join-Path {ps_quote(source)} $f.path
$part=$p
while($part.Length -ge {len(source)}){{if((Test-Path -LiteralPath $part) -and ((Get-Item -LiteralPath $part -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){{$ok=$false;break}};$part=[IO.Path]::GetDirectoryName($part)}}
if(!$ok -or !(Test-Path -LiteralPath $p -PathType Leaf) -or (Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash.ToLowerInvariant() -cne $f.sha256){{$ok=$false;break}}
}}
if($ok){{'true'}}else{{'false'}}
""") == 'true'
                receipt['source_unchanged'] = receipt['host_source_unchanged'] and receipt.get('guest_source_unchanged', False)
                if not receipt['source_unchanged']:
                    receipt.update(status='failed', source_error='source changed or could not be verified')
            except Exception as error:
                receipt.update(status='failed', source_error=str(error))
            receipt['duration_seconds'] = round(time.monotonic() - started, 3)
            runner.write_json(output / 'report.json', receipt)
    runner.present(receipt, output / 'report.json', args.format)
    return 0 if receipt['status'] == 'passed' else 1
