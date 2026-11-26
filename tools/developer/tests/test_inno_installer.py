"""Native Pascal Script contracts, isolated from real product/account data.

Set SIDRAVIA_ISCC and SIDRAVIA_INNO_TEST_ROOT to use the pinned Windows compiler
and a native Windows scratch directory. The generated installer exits from
InitializeSetup: it never installs files, starts MGR or changes PATH/startup.
Only synthetic data and a fresh random test AUMID are affected.
"""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
import uuid

REPO = Path(__file__).resolve().parents[3]


def windows_path(path):
    if os.name == 'nt':
        return str(path)
    return subprocess.check_output(['wslpath', '-w', str(path)], text=True).strip()


@unittest.skipUnless(os.environ.get('SIDRAVIA_ISCC') and os.environ.get('SIDRAVIA_INNO_TEST_ROOT'),
                     'native Inno compiler/scratch root not configured')
class InnoContracts(unittest.TestCase):
    def test_native_amd64_payload_script_contracts(self):
        self.check_native_script_contracts('windows-amd64')

    def test_native_arm64_payload_script_contracts(self):
        self.check_native_script_contracts('windows-arm64')

    def check_native_script_contracts(self, payload_target):
        compiler = os.environ['SIDRAVIA_ISCC']
        with tempfile.TemporaryDirectory(prefix='inno-contract-', dir=os.environ['SIDRAVIA_INNO_TEST_ROOT']) as directory:
            root = Path(directory)
            state = root / 'state'
            state.mkdir()
            native = windows_path(state)
            # Junction boundary: both link and target are this fixture's data.
            target = state / 'outside'
            target.mkdir()
            (target / 'sentinel.txt').write_text('keep')
            link = state / 'roaming/Sidravia/namespaces/linked'
            link.parent.mkdir(parents=True)
            subprocess.run(['/mnt/c/Windows/System32/cmd.exe' if os.name != 'nt' else 'cmd.exe',
                            '/c', 'mklink', '/J', windows_path(link), windows_path(target)],
                           check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            powershell = '/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe' if os.name != 'nt' else 'powershell.exe'
            acl_script = root / 'acl.ps1'
            acl_script.write_text(r'''param([string]$Root, [string]$Mode)
$ErrorActionPreference = 'Stop'
$sourceFile = Join-Path $Root 'protected-original.json'
$backupFile = Join-Path $Root 'protected-copy.json'
if ($Mode -eq 'create') {
 [IO.File]::WriteAllText($sourceFile, '{"synthetic":"fixture"}')
 $ownerSid = [Security.Principal.WindowsIdentity]::GetCurrent().User
 $acl = New-Object Security.AccessControl.FileSecurity
 $acl.SetOwner($ownerSid)
 $acl.SetAccessRuleProtection($true, $false)
 $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($ownerSid,'FullControl','Allow')))
 $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule((New-Object Security.Principal.SecurityIdentifier('S-1-5-18')),'FullControl','Allow')))
 Set-Acl -LiteralPath $sourceFile -AclObject $acl
} else {
 $sourceAcl = Get-Acl -LiteralPath $sourceFile
 $backupAcl = Get-Acl -LiteralPath $backupFile
 function Rules($acl) {
   @($acl.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier]) | ForEach-Object {
     $_.IdentityReference.Value + ':' + $_.AccessControlType + ':' + $_.FileSystemRights + ':' + $_.InheritanceFlags + ':' + $_.PropagationFlags
   } | Sort-Object) -join ';'
 }
 if (-not $backupAcl.AreAccessRulesProtected -or (Rules $sourceAcl) -ne (Rules $backupAcl)) { throw 'backup access rules differ' }

}
''')
            subprocess.run([powershell, '-NoProfile', '-File', windows_path(acl_script), native, 'create'],
                           check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)

            template = (REPO / 'packaging/windows/sidravia.iss').read_text()
            preamble = template.split('[Setup]', 1)[0]
            source = template.split('[Code]\n', 1)[1]
            # Rename entry points; no production wizard/uninstall event executes.
            events = ('InitializeSetup', 'DeinitializeSetup', 'DeinitializeUninstall', 'InitializeWizard', 'RegisterPreviousData', 'ShouldSkipPage', 'CurPageChanged',
                      'UpdateReadyMemo', 'NextButtonClick', 'PrepareToInstall', 'CurStepChanged',
                      'GetCustomSetupExitCode', 'InitializeUninstall', 'CurUninstallStepChanged')
            for event in events:
                source = re.sub(r'\b' + event + r'\b', 'Unused' + event, source)
            for constant, leaf in (('{userappdata}', 'roaming'), ('{localappdata}', 'local'), ('{app}', 'app')):
                source = source.replace(constant, native + '\\' + leaf)
            # Use the real OppositeSetupRegistered implementation against a
            # unique test key. A deployed AppId never reaches the registry.
            registry_prefix = 'Software\\SidraviaInnoTest-' + uuid.uuid4().hex
            source = source.replace('Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall', registry_prefix)
            # Fixed real AUMIDs/process names never reach test calls.
            identity = 'io.github.astesia.Sidravia.InnoTest.' + uuid.uuid4().hex
            source = source.replace('io.github.astesia.Sidravia.Announcements', identity)
            for name in ('Sidravia.exe', 'sidraviad.exe', 'sidraviactl.exe', 'sidraviaupdater.exe'):
                source = source.replace('Name="' + name + '"', 'Name="inno-fixture-unused-' + name + '"')
            cases = (REPO / 'packaging/windows/testdata/contracts.iss').read_text().replace('@ROOT@', native).replace('io.github.astesia.Sidravia.InnoTest.FIXTURE', identity)
            script = root / 'contracts.iss'
            cases = cases.replace('@OPPOSITE_KEY@', registry_prefix + '\\{#OppositeAppId}_is1')
            script.write_text('#define ProductVersion "0.3.0-alpha.2"\n'
                              f'#define PayloadTarget "{payload_target}"\n' + preamble +
                              '[Setup]\nAppName=Sidravia isolated contracts\nAppVersion=1\n'
                              'DefaultDirName={tmp}\\never-installed\nPrivilegesRequired=lowest\n'
                              'SetupArchitecture=x64\nUninstallable=no\nCreateAppDir=no\n'
                              'OutputBaseFilename=contracts\n[Code]\n' + source + cases, encoding='utf-8-sig')
            result = subprocess.run([compiler, '/Q', '/O' + windows_path(root), windows_path(script)],
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout.decode(errors='replace'))
            result = subprocess.run([str(root / 'contracts.exe'), '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART'],
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
            receipt = state / 'result.txt'
            self.assertTrue(receipt.exists(), f'no fixture receipt, exit {result.returncode}')
            self.assertEqual(receipt.read_text(), 'passed')
            checked = subprocess.run([powershell, '-NoProfile', '-File', windows_path(acl_script), native, 'compare'],
                                     stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)
            self.assertEqual(checked.returncode, 0, checked.stdout.decode(errors='replace'))
