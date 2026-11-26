"""Upstream binary RPM producer/consumer; no host install command is exposed."""
import contextlib
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid

from verification_commands import resolve_flutter_sdk
from verify_distribution import native_arch


APP = Path('/usr/lib64/sidravia')
COMMANDS = ('sidraviactl', 'sidraviad', 'sidravia_gui')
CORE_COMMANDS = ('sidraviactl', 'sidraviad')
DESKTOP = 'io.github.CaelestisAstesia.Sidravia'


def maintenance_scripts(root):
    code = 'import sys\n' + (root / 'packaging/linux/rpm_maintenance.py').read_text()
    return {'PREIN': code + '\nraise SystemExit(quiesce())\n',
            'PREUN': code + "\nraise SystemExit(quiesce(remove=True) if sys.argv[1] == '0' else 0)\n"}


def digest(path):
    with path.open('rb') as payload:
        return hashlib.file_digest(payload, 'sha256').hexdigest()


def publish_rpm(package, receipt, output, check_source):
    # Assemble beside the destination so moving the completed set is atomic.
    with tempfile.TemporaryDirectory(prefix='.sidravia-rpm-', dir=output.parent) as temporary:
        complete = Path(temporary) / 'complete'
        complete.mkdir()
        payload = complete / package.name
        shutil.copy2(package, payload)
        if digest(payload) != receipt['sha256']:
            raise ValueError('RPM changed before publication')
        (complete / (package.name + '.json')).write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
        (complete / 'SHA256SUMS').write_text(receipt['sha256'] + '  ' + package.name + '\n', encoding='ascii')
        check_source()
        # An exclusive reservation refuses even an existing empty directory.
        output.mkdir()
        reserved = output.lstat()
        try:
            complete.replace(output)
        except OSError:
            with contextlib.suppress(OSError):
                if os.path.samestat(output.lstat(), reserved):
                    output.rmdir()
            raise


def same_source(before, after):
    return all(before[k] == after[k] for k in ('commit', 'dirty', 'status', 'source_fingerprint'))


def rpm_version(version):
    if not re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-zA-Z0-9.-]+)?', version):
        raise ValueError('RPM requires a canonical product version without build metadata')
    return version.replace('-', '~', 1)


def query(package, *args):
    # Empty digest/link columns are meaningful in structured file inventories.
    return subprocess.check_output(['rpm', '-qp', *args, str(package)], text=True).rstrip('\n')


def tree_manifest(stage):
    result = {}
    for path in sorted(stage.rglob('*')):
        name = '/' + str(path.relative_to(stage))
        # Shared system directories are not owned by this package.
        if path.is_dir() and not any(name == p or name.startswith(p + '/') for p in (
            str(APP), '/usr/share/doc/sidravia', '/usr/share/licenses/sidravia')):
            continue
        mode = path.lstat().st_mode
        result[name] = {'mode': mode, 'digest': digest(path) if stat.S_ISREG(mode) else '',
                        'link': os.readlink(path) if stat.S_ISLNK(mode) else ''}
    return result


RPM_ARCH = {'amd64': 'x86_64', 'arm64': 'aarch64'}


def receipt_architecture(receipt):
    # Earlier RPM receipts were produced exclusively on x86_64.
    arch = receipt.get('architecture', 'x86_64')
    if arch not in RPM_ARCH.values():
        raise ValueError('unsupported RPM architecture')
    return arch


def verify(package, receipt):
    if digest(package) != receipt['sha256']:
        raise ValueError('RPM differs from producer hash')
    identity = query(package, '--qf', '%{NAME}|%{VERSION}|%{RELEASE}|%{ARCH}')
    core = receipt.get('kind') == 'core'
    name = 'sidravia-core' if core else 'sidravia'
    arch = receipt_architecture(receipt)
    if identity != f"{name}|{rpm_version(receipt['version'])}|{receipt['release']}|{arch}":
        raise ValueError('RPM identity mismatch')
    scripts = receipt['maintenanceScripts']
    for tag in ('PREIN', 'PREUN'):
        if query(package, '--qf', '%{' + tag + '}') != scripts[tag].rstrip('\n') or query(package, '--qf', '%{' + tag + 'PROG}') != '/usr/bin/python3':
            raise ValueError('RPM maintenance script differs from reviewed producer source')
    for tag in ('POSTIN', 'POSTUN', 'PRETRANS', 'POSTTRANS', 'TRIGGERSCRIPTS', 'FILETRIGGERSCRIPTS', 'TRANSFILETRIGGERSCRIPTS'):
        if query(package, '--qf', '%{' + tag + '}') not in ('(none)', ''):
            raise ValueError('unexpected RPM transaction script: ' + tag)
    rows = query(package, '--qf', '[%{FILENAMES}\t%{FILEMODES:octal}\t%{FILEDIGESTS}\t%{FILELINKTOS}\n]')
    actual = {}
    for row in rows.splitlines():
        name, mode, checksum, link = row.split('\t')
        actual[name] = {'mode': int(mode, 8), 'digest': checksum, 'link': link}
    if any(Path(name).name in ('sidraviaupdater','sidravia-files.json') for name in actual):
        raise ValueError('RPM must not carry portable updater metadata or executable')
    if actual != receipt['files']:
        raise ValueError('RPM payload paths/modes/digests/links differ from the verified stage')
    requirements = query(package, '--requires').splitlines()
    for required in (() if core else ('gtk3', 'webkit2gtk4.1', 'libayatana-appindicator-gtk3',
                     '/usr/bin/env', '/usr/bin/xdg-open', 'desktop-file-utils', 'hicolor-icon-theme')):
        if required not in requirements:
            raise ValueError('missing RPM runtime dependency: ' + required)
    if core:
        if any(Path(path).name == 'sidravia_gui' or '/gui-runtime/' in path or '/applications/' in path
               or '/icons/' in path for path in actual):
            raise ValueError('core RPM contains desktop payload')
        if any(any(word in requirement.lower() for word in ('gtk', 'webkit', 'ayatana', 'flutter', 'xdg-open', 'desktop-file-utils', 'hicolor'))
               for requirement in requirements):
            raise ValueError('core RPM requires desktop runtime')
        if query(package, '--conflicts').splitlines() != ['sidravia']:
            raise ValueError('core RPM must conflict with the full desktop package')
    if any('libflutter_linux_gtk.so' in x for x in requirements):
        raise ValueError('private Flutter library leaked into external requirements')
    if any('libjvm' in x or 'libdartjni' in x for x in requirements):
        raise ValueError('unused Android JNI/JDK dependency leaked into the Linux RPM')
    if any('.so' in x for x in query(package, '--provides').splitlines()):
        raise ValueError('private library leaked into global RPM provides')
    # RPM 6's default signature policy rejects unsigned local candidates. Check
    # every digest while explicitly declining signature acceptance for this mode.
    subprocess.run(['rpm', '-K', '--nosignature', str(package)], check=True)
    return requirements


def build(root, archive, output, release, env, source, current_source, *, core=False, version=None, go='go'):
    if not output or not re.fullmatch(r'[1-9][0-9]*', release):
        raise ValueError('RPM requires --output and a positive numeric --rpm-release')
    output = Path(output).resolve()
    if output.exists() or not output.parent.is_dir():
        raise ValueError('RPM output must not exist and its parent must exist')
    arch = native_arch()
    if arch not in RPM_ARCH:
        raise ValueError('RPM requires a native AMD64 or ARM64 build host')
    target = f'linux-{arch}'
    rpm_arch = RPM_ARCH[arch]
    if core:
        import verify_distribution
        portable = verify_distribution.verify(archive.parent, root, target, version, source['commit'], go=go)
        portable['source'] = source
    else:
        from linux_development import read_portable_receipt
        portable = read_portable_receipt(archive)
    if portable['target'] != target or portable['payloadKind'] != ('core' if core else 'gui'):
        raise ValueError('RPM input target or package kind mismatch')
    expected_archive_hash = portable['artifactHashes'].get(archive.name)
    if not same_source(portable['source'], source) or expected_archive_hash is None:
        raise ValueError('RPM input must be the exact clean-source verified portable')
    version = portable['version']
    rpmver = rpm_version(version)
    local = root / '.local'
    local.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='linux-rpm-build-', dir=local) as tmp:
        top = Path(tmp)
        for name in ('SOURCES', 'SPECS', 'BUILD', 'BUILDROOT', 'RPMS', 'SRPMS'):
            (top / name).mkdir()
        extracted = top / 'input'
        staged_archive = top / archive.name
        shutil.copyfile(archive, staged_archive)
        archive_hash = digest(staged_archive)
        if archive_hash != expected_archive_hash:
            raise ValueError('RPM input must be the exact clean-source verified portable')
        with tarfile.open(staged_archive) as tar:
            # The producer hash is checked first; still reject links and traversal.
            if any(not (m.isfile() or m.isdir()) or m.name.startswith('/') or '..' in Path(m.name).parts for m in tar):
                raise ValueError('unsafe portable RPM input')
            tar.extractall(extracted, filter='data')
        bundle, = extracted.iterdir()
        if not (bundle / 'sidravia.portable').is_file():
            raise ValueError('not a portable input')
        # RPM updates are served by CTL's package-manager boundary.
        (bundle / 'sidraviaupdater').unlink()
        # RPM owns its own file inventory; the portable ownership index also
        # describes the updater/docs deliberately removed from this payload.
        (bundle / 'sidravia-files.json').unlink()
        (bundle / 'sidravia.portable').unlink()
        if not core:
            (bundle / 'gui-runtime/data/linux-portable.md' if (bundle / 'gui-runtime').is_dir() else bundle / 'data/linux-portable.md').unlink()
        # Explicit ownership boundary consumed by GUI and updater. No runtime data.
        (bundle / 'sidravia.rpm').write_text('sidravia-rpm-v1\n')
        (bundle / 'sidravia.rpm-lifecycle').write_text('sidravia-rpm-lifecycle-v1\n')
        if core:
            (bundle / 'sidravia.rpm-core').write_text('sidravia-rpm-core-v1\n')
        stage = top / 'payload'
        installed = stage / APP.relative_to('/')
        installed.parent.mkdir(parents=True)
        shutil.move(bundle, installed)
        bindir = stage / 'usr/bin'
        bindir.mkdir()
        for name in (CORE_COMMANDS if core else COMMANDS):
            (bindir / name).symlink_to('../lib64/sidravia/' + name)
        if not core:
            desktop = stage / f'usr/share/applications/{DESKTOP}.desktop'
            desktop.parent.mkdir(parents=True)
            shutil.copy2(root / 'packaging/linux/sidravia.desktop', desktop)
            icon = stage / f'usr/share/icons/hicolor/256x256/apps/{DESKTOP}.png'
            icon.parent.mkdir(parents=True)
            shutil.copy2(installed / ('gui-runtime/data/icons/sidravia.png' if (installed / 'gui-runtime').is_dir() else 'data/icons/sidravia.png'), icon)
        documentation = [('usr/share/licenses/sidravia/LICENSE', root / 'LICENSE')]
        if not core:
            documentation.append(('usr/share/doc/sidravia/linux-rpm.md', root / 'docs/linux-rpm.md'))
        for name, source_path in documentation:
            target = stage / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source_path, target)
        for path in stage.rglob('*'):
            if not path.is_symlink():
                path.chmod(0o755 if path.is_dir() or path.name in COMMANDS else 0o644)
        files = tree_manifest(stage)
        (top / 'SOURCES/files.list').write_text(''.join(('%dir ' if stat.S_ISDIR(v['mode']) else '') + p + '\n' for p, v in files.items()))
        with tarfile.open(top / 'SOURCES/payload.tar.gz', 'w:gz') as tar:
            for path in sorted(stage.rglob('*')):
                tar.add(path, arcname=str(path.relative_to(stage)), recursive=False)
        scripts = maintenance_scripts(root)
        spec = (root / ('packaging/linux/sidravia-core.spec.in' if core else 'packaging/linux/sidravia.spec.in')).read_text().replace('@VERSION@', rpmver).replace('@RELEASE@', release).replace('@ARCH@', rpm_arch)
        spec = spec.replace('@PREIN@', scripts['PREIN'].replace('%', '%%')).replace('@PREUN@', scripts['PREUN'].replace('%', '%%'))
        specpath = top / 'SPECS/sidravia.spec'
        specpath.write_text(spec)
        subprocess.run(['rpmbuild', '-bb', '--define', '_topdir ' + str(top), str(specpath)], env=env, check=True)
        package, = (top / 'RPMS' / rpm_arch).glob('*.rpm')
        receipt = {'version': version, 'release': release, 'source': source, 'source_unchanged': True,
                   'portableSHA256': archive_hash, 'sha256': digest(package), 'files': files,
                   'package': package.name, 'architecture': rpm_arch, 'unsigned': True, 'maintenanceScripts': scripts}
        receipt['kind'] = 'core' if core else 'gui'
        receipt['requires'] = verify(package, receipt)
        def check_source():
            if not same_source(current_source(), source):
                raise ValueError('source changed during RPM build')
        publish_rpm(package, receipt, output, check_source)
    print('Verified unsigned RPM: ' + str(output / receipt['package']))
    return 0


def accept(root, packages, env, source, *, go, flutter):
    receipts = [json.loads(Path(str(p) + '.json').read_text()) for p in packages]
    if receipts[0].get('kind', 'gui') != receipts[1].get('kind', 'gui'):
        raise ValueError('RPM acceptance requires matching package kinds')
    if any(receipt_architecture(receipt) != RPM_ARCH.get(native_arch()) for receipt in receipts):
        raise ValueError('RPM acceptance requires a matching native host')
    for package, receipt in zip(packages, receipts):
        if not same_source(receipt['source'], source):
            raise ValueError('RPM acceptance requires its exact clean producer source')
    if receipts[0]['version'] != receipts[1]['version'] or int(receipts[0]['release']) >= int(receipts[1]['release']):
        raise ValueError('acceptance requires the same product version and increasing packaging revisions')
    core = receipts[0].get('kind') == 'core'
    dart = None
    if not core:
        _, sdk = resolve_flutter_sdk(flutter, root / 'gui/.flutter-version', cwd=root, env=env)
        dart = Path(sdk['flutterRoot']) / 'bin/dart'
        if not dart.is_file():
            raise ValueError('Flutter SDK is missing bin/dart')
    # Root maps only to the invoking user. A private PID namespace limits /proc
    # to our own descendants; the shared user namespace lets the transaction
    # root inspect them without acquiring any privilege over the real host.
    local = root / '.local'
    local.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='linux-rpm-session-', dir=local) as tmp:
        session = Path(tmp)
        verified_packages = []
        for index, (package, receipt) in enumerate(zip(packages, receipts, strict=True)):
            directory = session / f'package-{index}'
            directory.mkdir()
            staged = directory / package.name
            shutil.copyfile(package, staged)
            verify(staged, receipt)
            Path(str(staged) + '.json').write_text(json.dumps(receipt))
            verified_packages.append(staged)
        runtime = session / 'runtime'
        runtime.mkdir(mode=0o700)
        config = session / 'bus.conf'
        config.write_text('<busconfig><type>session</type><listen>unix:abstract=sidravia-rpm-' + uuid.uuid4().hex + '</listen>'
            '<auth>EXTERNAL</auth><policy context="default"><allow own="*"/>'
            '<allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>')
        child_env = dict(env, XDG_RUNTIME_DIR=str(runtime))
        if not core:
            wayland = Path(env['WAYLAND_DISPLAY'])
            if not wayland.is_absolute():
                wayland = Path(env['XDG_RUNTIME_DIR']) / wayland
            child_env['WAYLAND_DISPLAY'] = str(wayland)
        command = ['unshare', '--user', '--map-root-user', '--pid', '--fork', '--mount-proc',
            'dbus-run-session', '--config-file=' + str(config), '--', sys.executable,
            str(root / 'tools/developer/linux_rpm_fixture.py'), '--go', go]
        if dart is not None:
            command.extend(['--dart', str(dart)])
        command.extend([str(root), *map(str, verified_packages)])
        return subprocess.run(command, env=child_env, check=False).returncode
