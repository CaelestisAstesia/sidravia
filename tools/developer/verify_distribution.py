#!/usr/bin/env python3
"""Consume distribution artifacts without launching daemon, GUI or installer."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
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


def validate_product_ownership(document: dict) -> None:
    require(isinstance(document, dict) and type(document.get('schemaVersion')) is int
            and isinstance(document.get('files'), list), 'ownership_field_type')
    files = document['files']
    require(0 < len(files) <= 20000, 'ownership_file_limit')
    paths = set()
    total = 0
    protected = {'config', 'runtime', 'logs', 'namespaces', 'sidravia.portable', 'sidravia.update', 'sidravia-files.json'}
    for item in files:
        require(isinstance(item, dict) and type(item.get('size')) is int
                and type(item.get('mode')) is int and isinstance(item.get('path'), str), 'ownership_field_type')
        name = item['path']
        require(0 < len(name) <= 1024 and all(32 <= ord(c) <= 126 and c not in '\\<>:"|?*' for c in name), 'ownership_unsafe_path')
        parts = name.split('/')
        require(all(part and part not in ('.', '..') and part == part.strip() and not part.endswith('.') for part in parts), 'ownership_unsafe_path')
        for part in parts:
            base = part.split('.')[0].rstrip(' ').upper()
            require(base not in ('CON', 'PRN', 'AUX', 'NUL', 'CONIN$', 'CONOUT$')
                    and not re.fullmatch(r'(?:COM|LPT)[1-9]', base), 'ownership_unsafe_path')
        require(parts[0].lower() not in protected, 'ownership_protected_path')
        key = name.lower()
        require(key not in paths, 'ownership_path_alias')
        paths.add(key)
        require(0 <= item['size'] <= 2 << 30 and item['mode'] in (0o644, 0o755), 'ownership_file_limit')
        require(isinstance(item.get('sha256'), str) and re.fullmatch('[0-9a-f]{64}', item['sha256']) is not None, 'ownership_hash')
        total += item['size']
        require(total <= 4 << 30, 'ownership_total_limit')
    for name in paths:
        parts = name.split('/')
        require(all('/'.join(parts[:i]) not in paths for i in range(1, len(parts))), 'ownership_parent_alias')


def matches_elf(data: bytes, arch: str) -> bool:
    return (len(data) >= 20 and data[:6] == b'\x7fELF\x02\x01'
            and int.from_bytes(data[18:20], 'little') == {'amd64': 62, 'arm64': 183}[arch])


def matches_pe(data: bytes, arch: str) -> bool:
    if len(data) < 64 or data[:2] != b'MZ':
        return False
    offset = int.from_bytes(data[60:64], 'little')
    return (64 <= offset <= len(data) - 26 and data[offset:offset + 4] == b'PE\x00\x00'
            and int.from_bytes(data[offset + 4:offset + 6], 'little') == {'amd64': 0x8664, 'arm64': 0xaa64}[arch]
            and data[offset + 24:offset + 26] == b'\x0b\x02')


def verify_gui_runtime(files: dict[str, bytes], windows: bool, arch: str) -> None:
    fonts = json.loads(files.get('data/flutter_assets/FontManifest.json', b'[]'))
    require(isinstance(fonts, list), 'invalid_gui_font_manifest')
    declared = set()
    for family in fonts:
        require(isinstance(family, dict) and isinstance(family.get('fonts'), list), 'invalid_gui_font_manifest')
        for font in family['fonts']:
            require(isinstance(font, dict) and isinstance(font.get('asset'), str), 'invalid_gui_font_manifest')
            declared.add('data/flutter_assets/' + font['asset'])
    require(declared <= files.keys(), 'gui_declared_font_missing')
    for name in files:
        lower = name.lower()
        path = Path(lower)
        require(not any(part.startswith('.') for part in path.parts), 'gui_hidden_build_content: ' + name)
        require(path.suffix not in ('.pdb', '.lib', '.exp', '.ilk', '.obj', '.o', '.a', '.log', '.d', '.map', '.tmp'), 'gui_build_artifact: ' + name)
        require(path.suffix not in ('.woff', '.woff2'), 'gui_web_font: ' + name)
        if path.suffix in ('.ttf', '.otf', '.ttc'):
            require(name in declared, 'gui_undeclared_font: ' + name)
        require(path.name not in ('libdartjni.so', 'dartjni.dll', 'libdartjni.dll', 'libjvm.so', 'jvm.dll'), 'gui_unsupported_java: ' + name)
        require(lower not in ('data/flutter_assets/notices', 'data/flutter_assets/assetmanifest.bin.json'), 'gui_web_only_asset: ' + name)
        allowed = lower.startswith(('data/', 'licenses/'))
        if windows:
            allowed |= lower in ('sidravia.exe', 'native_assets.json') or '/' not in lower and lower.endswith('.dll')
        else:
            allowed |= lower in ('sidravia_gui', 'flutter-license', 'lib/native_assets.json') or lower.startswith('lib/') and lower.endswith('.so')
        require(allowed, 'unexpected_gui_runtime_path: ' + name)
        if windows and path.suffix in ('.dll', '.exe'):
            require(matches_pe(files[name], arch), 'gui_pe_target_mismatch: ' + name)
        if path.suffix == '.so' or not windows and lower == 'sidravia_gui':
            require(matches_elf(files[name], arch), 'gui_elf_target_mismatch: ' + name)



def native_arch() -> str | None:
    return {'amd64': 'amd64', 'x86_64': 'amd64', 'arm64': 'arm64',
            'aarch64': 'arm64'}.get(platform.machine().lower())


def read_archive(path: Path, target: str, base: str) -> dict[str, bytes]:
    contents: dict[str, bytes] = {}
    aliases: set[str] = set()
    def add(name: str, data: bytes) -> None:
        require(name.casefold() not in aliases, 'duplicate_archive_member')
        aliases.add(name.casefold())
        contents[name] = data
    if target.startswith('windows-'):
        with zipfile.ZipFile(path) as archive:
            for member in archive.infolist():
                require(not member.is_dir() and not stat.S_ISLNK(member.external_attr >> 16), 'non_file_archive_member')
                add(member.filename, archive.read(member))
    else:
        with tarfile.open(path, 'r:xz' if path.name.endswith('.tar.xz') else 'r:gz') as archive:
            for member in archive:
                require(member.isfile() and member.name.startswith(base + '/'), 'non_file_archive_member')
                name = member.name[len(base) + 1:]
                require(name and not name.startswith('/') and all(part not in ('', '.', '..') for part in name.split('/')), 'unsafe_archive_path')
                require(member.mode == (0o755 if name in ('sidravia_gui', 'gui-runtime/sidravia_gui', 'sidraviactl', 'sidraviad', 'sidraviaupdater') else 0o644), 'archive_mode_mismatch')
                stream = archive.extractfile(member)
                require(stream is not None, 'unreadable_archive_member')
                with stream:
                    add(member.name[len(base) + 1:], stream.read())
    return contents


def binary_identity(path: Path, role: str, target: str, version: str, build_id: str, go: str) -> bool:
    result = subprocess.run([go, 'version', '-m', '-json', str(path)], capture_output=True, text=True, check=True, timeout=30)
    info = json.loads(result.stdout)
    settings = {row['Key']: row['Value'] for row in info.get('Settings', [])}
    package = 'sidravia/cmd/sidravia-gui-launcher' if role == 'sidravia_gui' else 'sidravia/cmd/' + role
    require(info.get('Path') == package, 'binary_package_mismatch')
    require(settings.get('GOOS') == target.split('-')[0] and settings.get('GOARCH') == target.split('-')[1], 'binary_target_mismatch')
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
    require(target in ('windows-amd64', 'linux-amd64', 'windows-arm64', 'linux-arm64'), 'invalid_target')
    os_name, arch = target.split('-')
    windows = os_name == 'windows'
    base = f'sidravia-v{version}-{target}'
    kind = 'gui-portable' if gui is not None else 'core-portable'
    archive_name = base + '-' + kind + ('.zip' if windows else '.tar.gz')
    if not windows:
        alternatives = [base + '-' + kind + suffix for suffix in ('.tar.gz', '.tar.xz')]
        present = [name for name in alternatives if (directory / name).exists()]
        require(len(present) == 1, 'missing_or_ambiguous_archive_encoding')
        archive_name = present[0]
    outputs = {archive_name}
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
    contents = read_archive(directory / archive_name, target, base)
    expected = {'LICENSE': (repo / 'LICENSE').read_bytes(),
                'THIRD-PARTY-NOTICES.txt': (repo / 'THIRD-PARTY-NOTICES.txt').read_bytes(), 'sidravia.portable': b'',
                'BUILD-INFO.txt': (f'ProductVersion: {version}\nBuildID: {build_id}\nTarget: {target}\nArtifactKind: distribution\n').encode()}
    for profile in (repo / 'internal/connection/profile/data').glob('*.json'):
        expected['institution-profiles/' + profile.name] = profile.read_bytes()
    require(any(name.startswith('institution-profiles/') for name in expected), 'official_profiles_missing')
    roles = ['sidraviactl', 'sidraviad']
    if windows:
        roles.append('sidraviamgr')
    if gui is not None:
        gui_files = {}
        for path in gui.rglob('*'):
            require(not path.is_symlink(), 'gui_source_link')
            if path.is_file():
                name = path.relative_to(gui).as_posix()
                require(name.casefold() not in {key.casefold() for key in expected}, 'gui_source_collision')
                data = path.read_bytes()
                expected[name if windows else 'gui-runtime/' + name] = data
                gui_files[name] = data
        verify_gui_runtime(gui_files, windows, arch)
        if 'data/flutter_assets/fonts/MaterialIcons-Regular.otf' in gui_files:
            license_asset = 'assets/fonts/MaterialIcons-LICENSE.txt'
            require(gui_files.get('data/flutter_assets/' + license_asset) == (repo / 'gui' / license_asset).read_bytes(),
                    'material_icons_notice_missing_or_changed')
        if windows:
            for filename in ('LICENSE.txt', 'NOTICE.txt'):
                require(gui_files.get('licenses/Microsoft.Web.WebView2/' + filename),
                        'webview2_notice_missing: ' + filename)
        runtime_required = ('Sidravia.exe', 'flutter_windows.dll', 'msvcp140.dll', 'vcruntime140.dll', 'data/app.so', 'data/icudtl.dat', 'data/flutter_assets/AssetManifest.bin', 'data/flutter_assets/FontManifest.json', 'data/flutter_assets/NOTICES.Z') if windows else ('sidravia_gui', 'lib/libflutter_linux_gtk.so', 'lib/libapp.so', 'data/icudtl.dat', 'data/flutter_assets/NOTICES.Z', 'data/flutter_assets/version.json', 'data/icons/sidravia.png', 'FLUTTER-LICENSE')
        if windows and arch == 'amd64':
            runtime_required += ('vcruntime140_1.dll',)
        required_gui = runtime_required if windows else tuple('gui-runtime/' + name for name in runtime_required)
        require(all(name in expected for name in required_gui), 'gui_source_incomplete')
        if not windows:
            gui_version = json.loads(expected['gui-runtime/data/flutter_assets/version.json'])
            require(gui_version.get('version') == version and gui_version.get('app_name') == 'sidravia_gui', 'gui_version_mismatch')
            require(all(p.relative_to(gui).as_posix() == 'sidravia_gui' or p.relative_to(gui).as_posix() == 'FLUTTER-LICENSE' or p.relative_to(gui).parts[0] in ('lib', 'data') for p in gui.rglob('*') if p.is_file()), 'unexpected_linux_gui_source')
    if gui is not None and not windows:
        roles.append('sidravia_gui')
    suffix = '.exe' if windows else ''
    binaries = {role + suffix: role for role in roles}
    require(not any(name.casefold() in {key.casefold() for key in expected} for name in binaries), 'gui_binary_collision')
    require(set(contents) == set(expected) | set(binaries) | {'sidravia-files.json'}, 'archive_file_set_mismatch')
    expected['BUILD-INFO.txt'] += ''.join(
        f'BinarySHA256: {digest(contents[name])}  {name}\n' for name in binaries).encode()
    for name, data in expected.items():
        require(contents[name] == data, 'archive_source_mismatch: ' + name)
    ownership_bytes = contents['sidravia-files.json']
    require(0 < len(ownership_bytes) <= 2 << 20, 'ownership_size_limit')
    def unique_fields(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate_ownership_field')
            result[key] = value
        return result
    ownership = json.loads(ownership_bytes.decode('utf-8'), object_pairs_hook=unique_fields)
    product = {name: data for name, data in expected.items() if name != 'sidravia.portable'}
    product.update({name: contents[name] for name in binaries})
    wanted_ownership = {'schemaVersion': 1, 'version': version, 'buildId': build_id,
                        'os': target.split('-')[0], 'arch': target.split('-')[1],
                        'payloadKind': 'gui' if gui is not None else 'core',
                        'files': [{'path': name, 'size': len(data), 'sha256': digest(data),
                                   'mode': 0o755 if not windows and name in (*binaries, 'gui-runtime/sidravia_gui') else 0o644}
                                  for name, data in sorted(product.items())]}
    validate_product_ownership(ownership)
    require(ownership == wanted_ownership, 'ownership_source_mismatch')
    if smoke:
        host = {'Windows': 'windows', 'Linux': 'linux'}.get(platform.system())
        require(host == os_name and native_arch() == arch, 'smoke_requires_matching_native_host')
    embedded_identities = []
    with tempfile.TemporaryDirectory(prefix='sidravia-distribution-check-') as temporary:
        scratch = Path(temporary)
        root = scratch
        smoke_env = None
        if smoke and windows:
            root, local, roaming = scratch / 'payload', scratch / 'local', scratch / 'roaming'
            for folder in (root, local, roaming):
                folder.mkdir()
            smoke_env = dict(os.environ, LOCALAPPDATA=str(local), APPDATA=str(roaming))
        for name, role in binaries.items():
            path = root / name
            require((matches_pe if windows else matches_elf)(contents[name], arch), 'binary_image_target_mismatch: ' + name)
            path.write_bytes(contents[name])
            embedded_identities.append(binary_identity(path, role, target, version, build_id, go))
            if smoke and role != 'sidraviad':
                path.chmod(0o700)
                try:
                    result = subprocess.run([str(path), '--help'], cwd=root, capture_output=True,
                                            check=True, timeout=30, env=smoke_env)
                except subprocess.CalledProcessError as exc:
                    detail = (exc.stderr or b'').decode('utf-8', errors='replace')[:400].strip()
                    raise VerificationError(f'help_smoke_failed: {name}: exit {exc.returncode}: {detail}') from exc
                require(bool(result.stdout.strip()), 'empty_help_smoke')
    return {'buildId': build_id, 'version': version, 'target': target, 'artifactHashes': checksums,
            'archiveFiles': len(contents), 'checksums': 'passed', 'sourceContents': 'passed',
            'productOwnershipSha256': digest(ownership_bytes), 'payloadKind': ownership['payloadKind'],
            'productOwnership': 'passed', 'goTargetAndPackage': 'passed', 'candidateIdentity': 'producer_manifest_passed',
            'embeddedGoIdentity': 'passed' if all(embedded_identities) else 'not_recorded',
            'helpSmoke': 'passed' if smoke else 'not_run',
            'daemonLaunch': 'not_run', 'guiInteraction': 'not_run', 'installation': 'not_run', 'campus': 'not_run'}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', required=True, type=Path)
    parser.add_argument('--repo', type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument('--target', required=True, choices=['windows-amd64', 'linux-amd64', 'windows-arm64', 'linux-arm64'])
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
