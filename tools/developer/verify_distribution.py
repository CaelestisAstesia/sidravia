#!/usr/bin/env python3
"""Consume distribution artifacts without launching daemon, GUI or installer."""
from __future__ import annotations

import argparse
import hashlib
import json
import platform
import re
import shlex
import stat
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from pathlib import Path


class VerificationError(ValueError):
    pass


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def require(condition: bool, reason: str) -> None:
    if not condition:
        raise VerificationError(reason)


def read_archive(path: Path, target: str, base: str) -> dict[str, bytes]:
    contents: dict[str, bytes] = {}
    aliases: set[str] = set()
    def add(name: str, data: bytes) -> None:
        require(name.casefold() not in aliases, 'duplicate_archive_member')
        aliases.add(name.casefold())
        contents[name] = data
    if target == 'windows-amd64':
        with zipfile.ZipFile(path) as archive:
            for member in archive.infolist():
                require(not member.is_dir() and not stat.S_ISLNK(member.external_attr >> 16), 'non_file_archive_member')
                add(member.filename, archive.read(member))
    else:
        with tarfile.open(path, 'r:gz') as archive:
            for member in archive:
                require(member.isfile() and member.name.startswith(base + '/'), 'non_file_archive_member')
                stream = archive.extractfile(member)
                require(stream is not None, 'unreadable_archive_member')
                with stream:
                    add(member.name[len(base) + 1:], stream.read())
    return contents


def binary_identity(path: Path, role: str, target: str, version: str, build_id: str, go: str) -> bool:
    result = subprocess.run([go, 'version', '-m', '-json', str(path)], capture_output=True, text=True, check=True, timeout=30)
    info = json.loads(result.stdout)
    settings = {row['Key']: row['Value'] for row in info.get('Settings', [])}
    package = 'sidravia/cmd/' + ('sidravia' if role == 'sidraviactl' else role)
    require(info.get('Path') == package, 'binary_package_mismatch')
    require(settings.get('GOOS') == target.split('-')[0] and settings.get('GOARCH') == 'amd64', 'binary_target_mismatch')
    require(settings.get('CGO_ENABLED') == '0', 'binary_cgo_mismatch')
    # Go deliberately omits linker settings with -trimpath. Do not infer a
    # runtime identity from their absence; BUILD-INFO binds compiler output bytes.
    if '-ldflags' not in settings:
        return False
    flags = shlex.split(settings.get('-ldflags', ''))
    assignments = [flags[i + 1] for i, value in enumerate(flags[:-1]) if value == '-X']
    for key, value in (('main.ProductVersion', version), ('main.BuildID', build_id)):
        require([entry for entry in assignments if entry.startswith(key + '=')] == [key + '=' + value], 'binary_identity_mismatch')
    return True


def verify(directory: Path, repo: Path, target: str, version: str, build_id: str,
           gui: Path | None = None, go: str = 'go', smoke: bool = False) -> dict:
    require(re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?', version) is not None, 'invalid_version')
    require(re.fullmatch(r'[0-9a-f]{40}', build_id) is not None, 'invalid_build_id')
    require(target in ('windows-amd64', 'linux-amd64'), 'invalid_target')
    windows = target == 'windows-amd64'
    require(gui is None or windows, 'gui_target_mismatch')
    base = f'sidravia-v{version}-{target}'
    archive_name = base + ('-portable.zip' if windows else '-portable.tar.gz')
    outputs = {archive_name}
    if windows and gui is not None:
        outputs.add(base + '-setup.exe')
    require({p.name for p in directory.iterdir()} == outputs | {'SHA256SUMS.txt'}, 'output_file_set_mismatch')
    checksums = {}
    for line in (directory / 'SHA256SUMS.txt').read_text(encoding='utf-8').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([^/\\]+)', line)
        require(match is not None, 'invalid_checksum_row')
        hash_value, name = match.groups()
        require(name in outputs and name not in checksums, 'duplicate_or_unexpected_checksum')
        require((directory / name).is_file() and not (directory / name).is_symlink(), 'invalid_output_file')
        require(digest((directory / name).read_bytes()) == hash_value, 'output_checksum_mismatch')
        checksums[name] = hash_value
    require(set(checksums) == outputs, 'missing_checksum')
    if windows and gui is not None:
        require((directory / (base + '-setup.exe')).read_bytes().startswith(b'MZ'), 'installer_not_pe')
    contents = read_archive(directory / archive_name, target, base)
    expected = {'LICENSE': (repo / 'LICENSE').read_bytes(), 'sidravia.portable': b'',
                'BUILD-INFO.txt': (f'ProductVersion: {version}\nBuildID: {build_id}\nTarget: {target}\nArtifactKind: distribution\n').encode()}
    for profile in (repo / 'internal/daemon/configuration/profiles').glob('*.json'):
        expected['institution-profiles/' + profile.name] = profile.read_bytes()
    require(any(name.startswith('institution-profiles/') for name in expected), 'official_profiles_missing')
    roles = ['sidraviactl', 'sidraviad']
    if windows:
        roles.append('sidraviamgr')
        expected['app_icon.ico'] = (repo / 'gui/windows/runner/resources/app_icon.ico').read_bytes()
    if gui is not None:
        for path in gui.rglob('*'):
            require(not path.is_symlink(), 'gui_source_link')
            if path.is_file():
                name = path.relative_to(gui).as_posix()
                require(name.casefold() not in {key.casefold() for key in expected}, 'gui_source_collision')
                expected[name] = path.read_bytes()
        require('Sidravia.exe' in expected and 'flutter_windows.dll' in expected, 'gui_source_incomplete')
    suffix = '.exe' if windows else ''
    binaries = {role + suffix: role for role in roles}
    require(not any(name.casefold() in {key.casefold() for key in expected} for name in binaries), 'gui_binary_collision')
    require(set(contents) == set(expected) | set(binaries), 'archive_file_set_mismatch')
    expected['BUILD-INFO.txt'] += ''.join(
        f'BinarySHA256: {digest(contents[name])}  {name}\n' for name in binaries).encode()
    for name, data in expected.items():
        require(contents[name] == data, 'archive_source_mismatch: ' + name)
    if smoke:
        host = {'Windows': 'windows', 'Linux': 'linux'}.get(platform.system())
        require(host == target.split('-')[0] and platform.machine().lower() in ('amd64', 'x86_64'), 'smoke_requires_matching_native_host')
    embedded_identities = []
    with tempfile.TemporaryDirectory(prefix='sidravia-distribution-check-') as temporary:
        root = Path(temporary)
        for name, role in binaries.items():
            path = root / name
            path.write_bytes(contents[name])
            embedded_identities.append(binary_identity(path, role, target, version, build_id, go))
            if smoke and role != 'sidraviad':
                path.chmod(0o700)
                result = subprocess.run([str(path), '--help'], cwd=root, capture_output=True, check=True, timeout=30)
                require(bool(result.stdout.strip()), 'empty_help_smoke')
    return {'buildId': build_id, 'version': version, 'target': target, 'artifactHashes': checksums,
            'archiveFiles': len(contents), 'checksums': 'passed', 'sourceContents': 'passed',
            'goTargetAndPackage': 'passed', 'candidateIdentity': 'producer_manifest_passed',
            'embeddedGoIdentity': 'passed' if all(embedded_identities) else 'not_recorded',
            'helpSmoke': 'passed' if smoke else 'not_run',
            'daemonLaunch': 'not_run', 'guiInteraction': 'not_run', 'installation': 'not_run', 'campus': 'not_run'}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', required=True, type=Path)
    parser.add_argument('--repo', type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument('--target', required=True, choices=['windows-amd64', 'linux-amd64'])
    parser.add_argument('--version', required=True)
    parser.add_argument('--build-id', required=True)
    parser.add_argument('--gui-directory', type=Path)
    parser.add_argument('--go', default='go')
    parser.add_argument('--smoke', action='store_true')
    args = parser.parse_args()
    try:
        result = verify(args.directory, args.repo, args.target, args.version, args.build_id, args.gui_directory, args.go, args.smoke)
    except (VerificationError, OSError, ValueError, tarfile.TarError, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        print(f'distribution verification failed: {error}', file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
