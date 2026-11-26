"""Debian 13 binary build from a verified, exact-source portable payload."""
from __future__ import annotations

import contextlib
from datetime import datetime, timezone
from email.utils import format_datetime
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import tarfile
import tempfile

from linux_development import producer_directory, read_portable_receipt
from verify_distribution import native_arch, read_archive, verify as verify_portable


BASELINE = {'ID': 'debian', 'VERSION_ID': '13', 'VERSION_CODENAME': 'trixie'}
APP = Path('usr/lib/sidravia')
DESKTOP = 'io.github.CaelestisAstesia.Sidravia'
COMMANDS = ('sidraviad', 'sidraviactl')
SCRIPTS = ('preinst', 'postinst', 'prerm', 'postrm')


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def debian_version(version, revision):
    if not isinstance(version, str) or not re.fullmatch(
            r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-zA-Z0-9.-]+)?', version):
        raise ValueError('DEB requires a canonical product version without build metadata')
    if not re.fullmatch(r'[1-9][0-9]{0,19}', revision) or int(revision) >= 1 << 64:
        raise ValueError('DEB requires a positive numeric Debian revision')
    return version.replace('-', '~', 1) + '-' + revision


def baseline_matches(info):
    return all(info.get(key) == value for key, value in BASELINE.items())


def portable_payload(root, archive, architecture, source, *, core, version, go):
    if source['dirty']:
        raise ValueError('DEB candidates require clean source')
    target = 'linux-' + architecture
    gui = None
    if not core:
        receipt = read_portable_receipt(archive)
        if (any(receipt['source'][key] != source[key] for key in ('commit', 'status', 'source_fingerprint'))
                or receipt['buildId'] != source['commit']
                or not baseline_matches(receipt.get('linuxBuildBaseline', {}))):
            raise ValueError('GUI DEB requires the exact clean-source Debian 13 GUI producer receipt')
        gui = producer_directory(archive.parent) / 'gui'
        if version and version != receipt['version']:
            raise ValueError('DEB version differs from its GUI producer receipt')
        version = receipt['version']
        if digest(archive) != receipt['artifactHashes'].get(archive.name):
            raise ValueError('GUI portable differs from its Debian producer receipt')
    verified = verify_portable(archive.parent, root, target, version, source['commit'], gui, go)
    expected = verified['artifactHashes'].get(archive.name)
    if not expected:
        raise ValueError('archive is not named in the exact portable verification')
    base = f'sidravia-v{version}-{target}'
    # Recheck the verified archive after reading its content to catch changes
    # between portable verification and staging.
    files = read_archive(archive, target, base)
    if digest(archive) != expected:
        raise ValueError('portable input changed during Debian staging')
    for excluded in ('sidravia.portable', 'sidravia-files.json', 'sidraviaupdater'):
        del files[excluded]
    if not core:
        del files['gui-runtime/data/linux-portable.md']
    for suffix in ('', '-lifecycle') + (('-core',) if core else ()):
        files['sidravia.deb' + suffix] = f'sidravia-deb{suffix}-v1\n'.encode()
    return version, expected, files


def executable(name):
    return name in (*COMMANDS, 'sidravia_gui', 'gui-runtime/sidravia_gui')


def stage_payload(root, stage, payload, *, core):
    app = stage / APP
    for name, data in payload.items():
        output = app / name
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_bytes(data)
        output.chmod(0o755 if executable(name) else 0o644)
    bindir = stage / 'usr/bin'
    bindir.mkdir()
    for name in (COMMANDS if core else (*COMMANDS, 'sidravia_gui')):
        (bindir / name).symlink_to('../lib/sidravia/' + name)
    if not core:
        for relative, source in {
                f'share/applications/{DESKTOP}.desktop': root / 'packaging/linux/sidravia.desktop',
                f'share/icons/hicolor/256x256/apps/{DESKTOP}.png': app / 'gui-runtime/data/icons/sidravia.png',
        }.items():
            target = stage / 'usr' / relative
            target.parent.mkdir(parents=True)
            shutil.copyfile(source, target)
            target.chmod(0o644)


def maintainer_scripts(source, package, manifest):
    common = (source / 'packaging/linux/rpm_maintenance.py').read_text(encoding='utf-8')
    callbacks = (source / 'debian/maintenance.py').read_text(encoding='utf-8')
    setup = f'DEB_PACKAGE = {package!r}\nDEB_FILES = {manifest!r}\n'
    wrapper = """#!/bin/sh
set -eu
if [ ! -x /usr/bin/python3 ] && [ "${DPKG_MAINTSCRIPT_NAME:-}" = postrm ] &&
    [ ! -e /var/lib/sidravia/deb-maintenance/transaction.json ]; then
    exit 0
fi
exec /usr/bin/python3 - "$@" <<'SIDRAVIA_PYTHON'
"""
    return {name: wrapper + common + '\n' + callbacks + '\n' + setup
            + f'raise SystemExit(deb_main({name!r}, sys.argv[1:]))\nSIDRAVIA_PYTHON\n'
            for name in SCRIPTS}


def file_inventory(stage):
    inventory = {}
    for path in sorted(stage.rglob('*')):
        name = path.relative_to(stage).as_posix()
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode):
            inventory[name] = {'mode': stat.S_IMODE(mode), 'link': os.readlink(path)}
        elif stat.S_ISREG(mode):
            inventory[name] = {'mode': stat.S_IMODE(mode), 'sha256': digest(path)}
    return inventory


def deb_field(package, field):
    return subprocess.check_output(['dpkg-deb', '--field', package, field], text=True).strip()


def deb_tar(package, option):
    # Materialize the dpkg stream into a bounded temporary file before it is
    # consumed, so truncation or failed decompression cannot appear successful.
    with tempfile.TemporaryFile() as output:
        subprocess.run(['dpkg-deb', option, package], stdout=output, check=True)
        output.seek(0)
        with tarfile.open(fileobj=output, mode='r:') as tree:
            for member in tree:
                if member.uid != 0 or member.gid != 0:
                    raise ValueError('DEB member is not owned by root')
                if member.isdir():
                    continue
                path = member.name.removeprefix('./')
                if path.startswith('/') or '..' in Path(path).parts or not (member.isreg() or member.issym()):
                    raise ValueError('unsafe member in Debian package')
                data = None
                if member.isreg():
                    with tree.extractfile(member) as stream:
                        data = stream.read()
                yield path, member, data


def verify(package, receipt, scripts, changelog):
    for field, expected in (('Package', receipt['name']), ('Version', receipt['debianVersion']),
                            ('Architecture', receipt['architecture']), ('Conflicts', receipt['conflicts'])):
        if deb_field(package, field) != expected:
            raise ValueError(f'DEB {field} disagrees with the verified stage')
    payload = {}
    documentation = {
        'copyright': receipt['files'][(APP / 'LICENSE').as_posix()]['sha256'],
        'changelog.Debian.gz': hashlib.sha256(changelog.encode()).hexdigest(),
    }
    for path, member, data in deb_tar(package, '--fsys-tarfile'):
        if path in payload:
            raise ValueError('duplicate Debian payload member')
        doc = path.removeprefix(f"usr/share/doc/{receipt['name']}/")
        if doc in documentation and doc != path:
            expected = documentation.pop(doc)
            if member.mode != 0o644 or data is None:
                raise ValueError('invalid Debian documentation permissions or type')
            content = gzip.decompress(data) if doc.endswith('.gz') else data
            if hashlib.sha256(content).hexdigest() != expected:
                raise ValueError('Debian documentation differs from the packaging source')
            continue
        payload[path] = {'mode': member.mode, **({'link': member.linkname} if member.issym()
                                                 else {'sha256': hashlib.sha256(data).hexdigest()})}
    if documentation:
        raise ValueError('DEB documentation is missing: ' + ', '.join(documentation))
    if payload != receipt['files']:
        paths = sorted(name for name in payload.keys() | receipt['files'].keys()
                       if payload.get(name) != receipt['files'].get(name))
        raise ValueError('DEB payload paths, permissions or bytes differ for: ' + ', '.join(paths[:10]))
    actual_scripts = {path: data for path, member, data in deb_tar(package, '--ctrl-tarfile')
                      if path in SCRIPTS and member.mode == 0o755}
    if actual_scripts != {name: contents.encode() for name, contents in scripts.items()}:
        raise ValueError('DEB maintenance scripts differ from reviewed sources')
    predepends = deb_field(package, 'Pre-Depends')
    depends = deb_field(package, 'Depends')
    if predepends != 'python3 (>= 3.11)' or 'ca-certificates' not in depends:
        raise ValueError('DEB maintenance or TLS trust dependency is missing')
    desktop_requirements = ('hicolor-icon-theme', 'xdg-utils', 'libgtk-3-0t64',
                            'libwebkit2gtk-4.1-0', 'libayatana-appindicator3-1')
    if any((name in depends) == (receipt['kind'] == 'core') for name in desktop_requirements):
        raise ValueError('DEB desktop dependencies disagree with the package kind')
    if any(name in depends for name in ('libflutter_linux_gtk', 'libdartjni', 'libjvm')):
        raise ValueError('DEB incorrectly depends on a private or unused native library')
    return depends


def publish(output, package, receipt, check_source):
    with tempfile.TemporaryDirectory(dir=output.parent, prefix='.sidravia-deb-') as temp:
        complete = Path(temp) / 'complete'
        complete.mkdir()
        published = complete / package.name
        shutil.copyfile(package, published)
        checksum = digest(published)
        if checksum != receipt['sha256']:
            raise ValueError('DEB changed before publication')
        (complete / (package.name + '.json')).write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
        (complete / 'SHA256SUMS').write_text(f'{checksum}  {package.name}\n', encoding='ascii')
        check_source()
        output.mkdir()
        reserved = output.lstat()
        try:
            complete.replace(output)
        except OSError:
            with contextlib.suppress(OSError):
                if os.path.samestat(output.lstat(), reserved):
                    output.rmdir()
            raise


def build(root, archive, output, revision, env, source, current_source, *, core=False, version=None, go='go'):
    if not output:
        raise ValueError('DEB requires --output')
    output = Path(output).resolve()
    if output.exists() or not output.parent.is_dir():
        raise ValueError('DEB output must be new and its parent must exist')
    if not baseline_matches(platform.freedesktop_os_release()):
        raise ValueError('DEB builds require Debian 13 (trixie)')
    architecture = subprocess.check_output(['dpkg', '--print-architecture'], text=True, env=env).strip()
    if architecture not in ('amd64', 'arm64') or architecture != native_arch():
        raise ValueError('DEB requires a native Debian amd64 or arm64 build environment')
    package_name = 'sidravia-core' if core else 'sidravia'
    version, portable_hash, payload = portable_payload(root, archive, architecture, source, core=core,
                                                       version=version, go=go)
    package_version = debian_version(version, revision)
    stage_root = root / '.local'
    stage_root.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='debian-build-', dir=stage_root) as temporary:
        workspace = Path(temporary)
        staged_source = workspace / 'sidravia'
        recipe = staged_source / 'debian'
        shutil.copytree(root / 'debian', recipe, ignore=shutil.ignore_patterns('__pycache__'))
        stage = staged_source / 'payload'
        stage_payload(root, stage, payload, core=core)
        files = file_inventory(stage)
        manifest = {str(Path(name).relative_to(APP)): (item['mode'], item['sha256'])
                    for name, item in files.items() if Path(name).is_relative_to(APP)}
        scripts = maintainer_scripts(root, package_name, manifest)
        for name, text in scripts.items():
            script = recipe / f'{package_name}.{name}'
            script.write_text(text, encoding='utf-8')
            script.chmod(0o755)
        shutil.copyfile(root / 'LICENSE', recipe / 'copyright')
        authored = subprocess.check_output(['git', 'show', '-s', '--format=%ct', source['commit']],
                                           cwd=root, env=env, text=True).strip()
        date = format_datetime(datetime.fromtimestamp(int(authored), tz=timezone.utc))
        changelog = (f'sidravia ({package_version}) trixie; urgency=medium\n\n'
                     '  * Assemble the verified Sidravia payload for Debian 13.\n\n'
                     f' -- Sidravia maintainers <CaelestisAstesia@users.noreply.github.com>  {date}\n')
        (recipe / 'changelog').write_text(changelog, encoding='utf-8')
        build_env = dict(env, DEB_BUILD_PROFILES='pkg.sidravia.core-only' if core else 'pkg.sidravia.gui-only',
                         SOURCE_DATE_EPOCH=authored)
        subprocess.run(['dpkg-buildpackage', '-B', '-us', '-uc'], cwd=staged_source, env=build_env, check=True)
        package = workspace / f'{package_name}_{package_version}_{architecture}.deb'
        receipt = {'name': package_name, 'package': package.name, 'version': version,
                   'debianVersion': package_version, 'architecture': architecture, 'revision': revision,
                   'kind': 'core' if core else 'gui', 'conflicts': 'sidravia' if core else 'sidravia-core',
                   'baseline': BASELINE, 'source': source, 'source_unchanged': True,
                   'portableSHA256': portable_hash, 'sha256': digest(package), 'files': files,
                   'unsigned': True}
        receipt['depends'] = verify(package, receipt, scripts, changelog)

        def check_source():
            if current_source() != source:
                raise ValueError('source changed during DEB build')

        publish(output, package, receipt, check_source)
    print('Verified unsigned DEB: ' + str(output / receipt['package']))
    return 0
