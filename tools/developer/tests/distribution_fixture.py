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
            'internal/daemon/configuration/profiles/jlu.json',
            'gui/windows/runner/resources/app_icon.ico',
        )
        for name in repo_files:
            path = self.repo / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(name.encode())

        gui_files = (
            'Sidravia.exe',
            'flutter_windows.dll',
            'data/app.so',
            'data/icudtl.dat',
            'data/flutter_assets/AssetManifest.bin',
            'data/flutter_assets/FontManifest.json',
            'data/flutter_assets/NOTICES.Z',
        )
        for name in gui_files:
            path = self.gui / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'[]' if name.endswith('FontManifest.json') else name.encode())

    def clear_output(self):
        for path in self.out.iterdir():
            path.unlink()

    def payload(self, target, gui=True):
        contents = {
            'LICENSE': b'LICENSE',
            'sidravia.portable': b'',
            'institution-profiles/jlu.json': b'internal/daemon/configuration/profiles/jlu.json',
            'BUILD-INFO.txt': (
                f'ProductVersion: {VERSION}\nBuildID: {SHA}\n'
                f'Target: {target}\nArtifactKind: distribution\n'
            ).encode(),
        }
        programs = {'sidraviactl': b'ctl', 'sidraviad': b'daemon', 'sidraviaupdater': b'updater'}
        windows = target.startswith('windows-')
        gui = gui and target == 'windows-amd64'
        if windows:
            programs = {name + '.exe': data for name, data in programs.items()}
            programs['sidraviamgr.exe'] = b'manager'
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
            if gui and target == 'windows-amd64':
                (self.out / f'{base}-setup.exe').write_bytes(b'MZinert installer')
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
