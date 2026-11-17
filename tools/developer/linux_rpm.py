"""Upstream binary RPM producer/consumer; no host install command is exposed."""
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


APP = Path('/usr/lib64/sidravia')
COMMANDS = ('sidraviactl', 'sidraviad', 'sidravia_gui')
DESKTOP = 'io.github.CaelestisAstesia.Sidravia'


def maintenance_scripts(root):
    code = 'import sys\n' + (root / 'packaging/linux/rpm_maintenance.py').read_text()
    return {'PREIN': code + '\nraise SystemExit(quiesce())\n',
            'PREUN': code + "\nraise SystemExit(quiesce() if sys.argv[1] == '0' else 0)\n"}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


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


def verify(package, receipt):
    if digest(package) != receipt['sha256']:
        raise ValueError('RPM differs from producer hash')
    identity = query(package, '--qf', '%{NAME}|%{VERSION}|%{RELEASE}|%{ARCH}')
    if identity != f"sidravia|{rpm_version(receipt['version'])}|{receipt['release']}|x86_64":
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
    if any(Path(name).name == 'sidraviaupdater' for name in actual):
        raise ValueError('RPM must not carry the portable updater')
    if actual != receipt['files']:
        raise ValueError('RPM payload paths/modes/digests/links differ from the verified stage')
    requirements = query(package, '--requires').splitlines()
    for required in ('gtk3', 'webkit2gtk4.1', 'libayatana-appindicator-gtk3',
                     '/usr/bin/env', '/usr/bin/xdg-open', 'desktop-file-utils', 'hicolor-icon-theme'):
        if required not in requirements:
            raise ValueError('missing RPM runtime dependency: ' + required)
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


def build(root, archive, output, release, env, source, current_source):
    if not output or not re.fullmatch(r'[1-9][0-9]*', release):
        raise ValueError('RPM requires --output and a positive numeric --rpm-release')
    output = Path(output).resolve()
    if output.exists() or not output.parent.is_dir():
        raise ValueError('RPM output must not exist and its parent must exist')
    portable = json.loads((root / '.local/linux-portable-package-result.json').read_text())
    if not same_source(portable['source'], source) or portable['artifactHashes'].get(archive.name) != digest(archive):
        raise ValueError('RPM input must be the exact clean-source verified GUI portable')
    version = portable['version']
    rpmver = rpm_version(version)
    with tempfile.TemporaryDirectory(prefix='linux-rpm-build-', dir=root / '.local') as tmp:
        top = Path(tmp)
        for name in ('SOURCES', 'SPECS', 'BUILD', 'BUILDROOT', 'RPMS', 'SRPMS'):
            (top / name).mkdir()
        extracted = top / 'input'
        with tarfile.open(archive) as tar:
            # The producer hash is checked first; still reject links and traversal.
            if any(not (m.isfile() or m.isdir()) or m.name.startswith('/') or '..' in Path(m.name).parts for m in tar):
                raise ValueError('unsafe portable RPM input')
            tar.extractall(extracted, filter='data')
        bundle, = extracted.iterdir()
        if not (bundle / 'sidravia.portable').is_file():
            raise ValueError('not a GUI portable input')
        # RPM updates are served by CTL's package-manager boundary.
        (bundle / 'sidraviaupdater').unlink()
        (bundle / 'sidravia.portable').unlink()
        (bundle / 'data/linux-portable.md').unlink()
        # Explicit ownership boundary consumed by GUI and updater. No runtime data.
        (bundle / 'sidravia.rpm').write_text('sidravia-rpm-v1\n')
        (bundle / 'sidravia.rpm-lifecycle').write_text('sidravia-rpm-lifecycle-v1\n')
        stage = top / 'payload'
        installed = stage / APP.relative_to('/')
        installed.parent.mkdir(parents=True)
        shutil.move(bundle, installed)
        bindir = stage / 'usr/bin'
        bindir.mkdir()
        for name in COMMANDS:
            (bindir / name).symlink_to('../lib64/sidravia/' + name)
        desktop = stage / f'usr/share/applications/{DESKTOP}.desktop'
        desktop.parent.mkdir(parents=True)
        shutil.copy2(root / 'packaging/linux/sidravia.desktop', desktop)
        icon = stage / f'usr/share/icons/hicolor/256x256/apps/{DESKTOP}.png'
        icon.parent.mkdir(parents=True)
        shutil.copy2(installed / 'data/icons/sidravia.png', icon)
        for name, source_path in (('usr/share/doc/sidravia/linux-rpm.md', root / 'docs/linux-rpm.md'),
                                  ('usr/share/licenses/sidravia/LICENSE', root / 'LICENSE')):
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
        spec = (root / 'packaging/linux/sidravia.spec.in').read_text().replace('@VERSION@', rpmver).replace('@RELEASE@', release)
        spec = spec.replace('@PREIN@', scripts['PREIN'].replace('%', '%%')).replace('@PREUN@', scripts['PREUN'].replace('%', '%%'))
        specpath = top / 'SPECS/sidravia.spec'
        specpath.write_text(spec)
        subprocess.run(['rpmbuild', '-bb', '--define', '_topdir ' + str(top), str(specpath)], env=env, check=True)
        package, = (top / 'RPMS/x86_64').glob('*.rpm')
        receipt = {'version': version, 'release': release, 'source': source, 'source_unchanged': True,
                   'portableSHA256': digest(archive), 'sha256': digest(package), 'files': files,
                   'package': package.name, 'unsigned': True, 'maintenanceScripts': scripts}
        receipt['requires'] = verify(package, receipt)
        if not same_source(current_source(), source):
            raise ValueError('source changed during RPM build')
        output.mkdir()
        shutil.copy2(package, output / package.name)
        (output / (package.name + '.json')).write_text(json.dumps(receipt, indent=2) + '\n')
        (output / 'SHA256SUMS').write_text(receipt['sha256'] + '  ' + package.name + '\n')
    print('Verified unsigned RPM: ' + str(output / receipt['package']))
    return 0


def accept(root, packages, env, source, current_source):
    receipts = [json.loads(Path(str(p) + '.json').read_text()) for p in packages]
    for package, receipt in zip(packages, receipts):
        if not same_source(receipt['source'], source):
            raise ValueError('RPM acceptance requires its exact clean producer source')
        verify(package, receipt)
    if receipts[0]['version'] != receipts[1]['version'] or int(receipts[0]['release']) >= int(receipts[1]['release']):
        raise ValueError('acceptance requires the same product version and increasing packaging revisions')
    # Root maps only to the invoking user. A private PID namespace limits /proc
    # to our own descendants; the shared user namespace lets the transaction
    # root inspect them without acquiring any privilege over the real host.
    with tempfile.TemporaryDirectory(prefix='linux-rpm-session-', dir=root / '.local') as tmp:
        session = Path(tmp)
        runtime = session / 'runtime'
        runtime.mkdir(mode=0o700)
        config = session / 'bus.conf'
        config.write_text('<busconfig><type>session</type><listen>unix:abstract=sidravia-rpm-' + uuid.uuid4().hex + '</listen>'
            '<auth>EXTERNAL</auth><policy context="default"><allow own="*"/>'
            '<allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>')
        wayland = Path(env['WAYLAND_DISPLAY'])
        if not wayland.is_absolute():
            wayland = Path(env['XDG_RUNTIME_DIR']) / wayland
        child_env = dict(env, XDG_RUNTIME_DIR=str(runtime), WAYLAND_DISPLAY=str(wayland))
        command = ['unshare', '--user', '--map-root-user', '--pid', '--fork', '--mount-proc',
            'dbus-run-session', '--config-file=' + str(config), '--', sys.executable,
            str(root / 'tools/developer/linux_rpm_fixture.py'), str(root), *map(str, packages)]
        return subprocess.run(command, env=child_env, check=False).returncode
