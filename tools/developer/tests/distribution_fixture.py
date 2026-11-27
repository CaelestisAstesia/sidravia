"""Inert distribution inputs for consumer and release-metadata tests."""

from __future__ import annotations

import hashlib
import io
import json
import tarfile
import zipfile
from pathlib import Path


VERSION = '0.3.0-alpha.1'
SHA = 'a' * 40
LINUX_EXECUTABLES = {
    'sidraviactl', 'sidraviad', 'sidraviaupdater',
    'sidravia_gui', 'gui-runtime/sidravia_gui',
}


def pe(arch, tail=b''):
    head = bytearray(90)
    head[:2] = b'MZ'
    head[60] = 64
    head[64:68] = b'PE\0\0'
    head[68:70] = {'amd64': 0x8664, 'arm64': 0xaa64}[arch].to_bytes(2, 'little')
    head[88:90] = b'\x0b\x02'
    return bytes(head) + tail


def elf(arch, tail=b''):
    return (b'\x7fELF\x02\x01' + b'\0' * 12
            + {'amd64': 62, 'arm64': 183}[arch].to_bytes(2, 'little') + tail)


class DistributionFixture:
    """Own fixture paths and construct archives independently of the verifier."""

    def __init__(self, root: Path):
        self.repo = root / 'repo'
        self.out = root / 'out'
        self.gui = root / 'gui'
        for path in (self.repo, self.out, self.gui):
            path.mkdir()

        repo_files = (
            'LICENSE',
            'THIRD-PARTY-NOTICES.txt',
            'internal/connection/profile/data/jlu.json',
            'gui/windows/runner/resources/app_icon.ico',
        )
        for name in repo_files:
            path = self.repo / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(name.encode())

        gui_files = (
            'Sidravia.exe',
            'flutter_windows.dll',
            'msvcp140.dll',
            'vcruntime140.dll',
            'vcruntime140_1.dll',
            'data/app.so',
            'data/icudtl.dat',
            'data/flutter_assets/AssetManifest.bin',
            'data/flutter_assets/FontManifest.json',
            'data/flutter_assets/NOTICES.Z',
        )
        for name in gui_files:
            path = self.gui / name
            path.parent.mkdir(parents=True, exist_ok=True)
            data = name.encode()
            if name.endswith(('.exe', '.dll')):
                data = pe('amd64', data)
            if name.endswith('.so'):
                data = elf('amd64', data)
            if name.endswith('FontManifest.json'):
                data = b'[]'
            path.write_bytes(data)

    def clear_output(self):
        for path in self.out.iterdir():
            path.unlink()

    def payload(self, target, gui=True):
        contents = {
            'LICENSE': b'LICENSE',
            'THIRD-PARTY-NOTICES.txt': b'THIRD-PARTY-NOTICES.txt',
            'sidravia.portable': b'',
            'institution-profiles/jlu.json': b'internal/connection/profile/data/jlu.json',
            'BUILD-INFO.txt': (
                f'ProductVersion: {VERSION}\nBuildID: {SHA}\n'
                f'Target: {target}\nArtifactKind: distribution\n'
            ).encode(),
        }
        programs = {'sidraviactl': b'ctl', 'sidraviad': b'daemon', 'sidraviaupdater': b'updater'}
        os_name, arch = target.split('-')
        windows = os_name == 'windows'
        gui = gui and windows
        if windows:
            programs = {name + '.exe': pe(arch, data) for name, data in programs.items()}
            programs['sidraviamgr.exe'] = pe(arch, b'manager')
        else:
            programs = {name: elf(arch, data) for name, data in programs.items()}
        contents.update(programs)
        if gui:
            contents.update({
                path.relative_to(self.gui).as_posix(): path.read_bytes()
                for path in self.gui.rglob('*') if path.is_file()
            })
        contents['BUILD-INFO.txt'] += ''.join(
            f'BinarySHA256: {hashlib.sha256(data).hexdigest()}  {name}\n'
            for name, data in programs.items()
        ).encode()
        contents['sidravia-files.json'] = self.ownership(target, contents, 'gui' if gui else 'core')
        return contents

    def ownership(self, target, contents, kind):
        os_name, arch = target.split('-')
        files = [
            {
                'path': name,
                'size': len(data),
                'sha256': hashlib.sha256(data).hexdigest(),
                'mode': 0o755 if os_name == 'linux' and name in LINUX_EXECUTABLES else 0o644,
            }
            for name, data in sorted(contents.items())
            if name not in ('sidravia.portable', 'sidravia-files.json')
        ]
        return json.dumps({
            'schemaVersion': 1,
            'version': VERSION,
            'buildId': SHA,
            'os': os_name,
            'arch': arch,
            'payloadKind': kind,
            'files': files,
        }, separators=(',', ':')).encode()

    def package(self, target, contents, gui=True):
        base = f'sidravia-v{VERSION}-{target}'
        kind = 'gui-portable' if 'Sidravia.exe' in contents or 'sidravia_gui' in contents else 'core-portable'
        if target.startswith('windows-'):
            with zipfile.ZipFile(self.out / f'{base}-{kind}.zip', 'w') as archive:
                for name, data in contents.items():
                    archive.writestr(name, data)
            if gui:
                (self.out / f'{base}-setup.exe').write_bytes(pe('amd64', b'inert installer'))
        else:
            with tarfile.open(self.out / f'{base}-{kind}.tar.gz', 'w:gz') as archive:
                for name, data in contents.items():
                    entry = tarfile.TarInfo(f'{base}/{name}')
                    entry.size = len(data)
                    entry.mode = 0o755 if name in LINUX_EXECUTABLES else 0o644
                    archive.addfile(entry, io.BytesIO(data))
        sums = ''.join(
            f'{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n'
            for path in sorted(self.out.iterdir()) if path.name != 'SHA256SUMS.txt'
        )
        (self.out / 'SHA256SUMS.txt').write_text(sums)
