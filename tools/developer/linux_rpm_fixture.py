"""Real RPM transactions in a private root; installed GUI uses a mount sandbox.

Only the RPM dependency inventory and runtime system libraries are borrowed from
the host, read-only. No caller-selectable install root or namespace is accepted.
"""
import argparse
import contextlib
import fcntl
import json
import os
from pathlib import Path
import shutil
import signal
import sqlite3
import subprocess
import tempfile
import time
import uuid

from linux_desktop_fixture import wait_for
from linux_rpm import APP, COMMANDS, DESKTOP, digest, same_source


def run(root, packages, receipts, environment, source, current_source, *, dart):
    core = receipts[0].get('kind') == 'core'
    package_name = 'sidravia-core' if core else 'sidravia'
    owned = Path(tempfile.mkdtemp(prefix='linux-rpm-fixture-', dir=root / '.local'))
    namespace = 'linux-gui-test-' + uuid.uuid4().hex[:12]
    (owned / 'fixture-owner').write_text(namespace)
    sysroot = owned / 'root'
    sysroot.mkdir()
    home = owned / 'home'
    home.mkdir()
    env = dict(environment, HOME=str(home), SIDRAVIA_NAMESPACE=namespace,
               SIDRAVIA_GUI_FIXTURE=str(owned), GDK_BACKEND='wayland', PATH='/usr/bin:/bin')
    for key in ('CONFIG', 'CACHE', 'DATA', 'STATE'):
        path = owned / key.lower()
        path.mkdir()
        env[f'XDG_{key}_HOME'] = str(path)
    env['TMPDIR'] = str(owned / 'tmp')
    Path(env['TMPDIR']).mkdir()
    state = Path(env['XDG_CONFIG_HOME']) / 'Sidravia/namespaces' / namespace
    profiles = state / 'institution-profiles'
    profiles.mkdir(parents=True)
    profile = json.loads((root / 'internal/daemon/configuration/profiles/jlu.json').read_text())
    profile.update(institutionProfileId='linux-loopback', displayName='RPM loopback fixture')
    profile.pop('announcement', None)
    profile['institutionProtocolConfiguration'].update(serverAddress='127.0.0.1', serverPort=9, localPort={'mode': 'system_assigned'})
    (profiles / 'linux-loopback.json').write_text(json.dumps(profile))
    runtime = state / 'runtime.json'
    missing_entry = 'from gi.repository import Gio\nimport sys\ntry:\n app=Gio.DesktopAppInfo.new_from_filename(sys.argv[1])\nexcept TypeError:\n pass\nelse:\n assert app is None'
    events = []
    gui = None
    gui_pid = None
    gui_pidfd = None
    bystander = None
    bystander_runtime = None
    installed = False

    def command(argv, label, command_env=env):
        with (owned / (label + '.log')).open('w') as log:
            subprocess.run(argv, cwd=root / 'gui', env=command_env, stdout=log,
                           stderr=subprocess.STDOUT, check=True, timeout=90)
        events.append(label)

    # sqlite backup uses a read-only connection, includes committed WAL records,
    # and avoids copying a concurrently changing database file byte by byte.
    dbpath = subprocess.check_output(['rpm', '--eval', '%{_dbpath}'], text=True).strip()
    hostdb = Path(dbpath) / 'rpmdb.sqlite'
    db = sysroot / Path(dbpath).relative_to('/')
    db.mkdir(parents=True)
    with contextlib.closing(sqlite3.connect(hostdb.as_uri() + '?mode=ro', uri=True)) as src:
        with contextlib.closing(sqlite3.connect(db / 'rpmdb.sqlite')) as dst:
            src.backup(dst)
    host_alias = Path('/run/sidravia-rpm-host/usr')
    # Provide real interpreters/tools for the dependency inventory's icon and
    # desktop file triggers. Only private cache/output directories are writable;
    # all borrowed system executables/libraries remain read-only bind mounts.
    for name in ('bin', 'sbin', 'lib', 'lib64'):
        (sysroot / name).symlink_to('usr/' + name)
    for relative in ('usr/bin', 'usr/lib64', 'usr/lib', 'usr/share', 'usr/lib/sysimage',
                     'usr/share/icons', 'usr/share/icons/hicolor'):
        target = sysroot / relative
        target.mkdir(parents=True, exist_ok=True)
        for item in Path('/' + relative).iterdir():
            child = target / item.name
            if child.exists() or child.is_symlink():
                continue
            # Private destinations for package files and system cache triggers.
            if relative == 'usr/share' and item.name in ('applications', 'icons', 'doc', 'licenses'):
                child.mkdir()
            elif relative == 'usr/share/icons' and item.name == 'hicolor':
                child.mkdir()
            elif relative == 'usr/share/icons/hicolor':
                if item.name == 'index.theme':
                    shutil.copy2(item, child)
            else:
                child.symlink_to(host_alias / Path(relative).relative_to('usr') / item.name)
    # Other /usr top-level resources are read-only aliases.
    for item in Path('/usr').iterdir():
        target = sysroot / 'usr' / item.name
        if not target.exists() and not target.is_symlink():
            target.symlink_to(host_alias / item.name)
    (sysroot / 'etc').mkdir()
    (sysroot / 'etc/passwd').write_text('root:x:0:0:root:/root:/bin/sh\n')
    (sysroot / 'etc/group').write_text('root:x:0:\n')
    (sysroot / 'tmp').mkdir(mode=0o1777)
    (sysroot / 'tmp').chmod(0o1777)
    (sysroot / 'dev').mkdir()
    (sysroot / 'proc').mkdir()
    (sysroot / host_alias.relative_to('/')).mkdir(parents=True)
    rpm = ['bwrap', '--die-with-parent', '--unshare-net',
           '--cap-add', 'CAP_SYS_CHROOT', '--cap-add', 'CAP_SYS_PTRACE',
           '--ro-bind', '/', '/', '--bind', str(sysroot), str(sysroot),
           '--bind', str(sysroot / 'tmp'), '/tmp', '--setenv', 'TMPDIR', '/tmp',
           '--ro-bind', '/usr', str(sysroot / host_alias.relative_to('/')), '--chdir', str(owned),
           '--dev', str(sysroot / 'dev'), '--proc', str(sysroot / 'proc'),
           'rpm', '--root', str(sysroot), '--dbpath', dbpath, '--nosignature', '--noplugins']

    # Do not install over an existing Sidravia system package even in the clone:
    # an unexpected inventory deserves diagnosis rather than silent replacement.
    for name in ('sidravia', 'sidravia-core'):
        existing = subprocess.run(rpm + ['-q', name], stdout=subprocess.DEVNULL)
        if existing.returncode != 1:
            raise RuntimeError('unexpected existing system Sidravia package inventory')

    view = owned / 'view/usr'
    view.mkdir(parents=True)
    for path in Path('/usr').iterdir():
        if path.name in ('bin', 'lib64'):
            directory = view / path.name
            directory.mkdir()
            for leaf in path.iterdir():
                if path.name == 'bin' and leaf.name in COMMANDS:
                    continue
                if path.name == 'lib64' and leaf.name == APP.name:
                    raise RuntimeError('unexpected host Sidravia library directory')
                (directory / leaf.name).symlink_to(host_alias / path.name / leaf.name)
        else:
            (view / path.name).symlink_to(host_alias / path.name)
    (view / 'lib64/sidravia').mkdir()
    for name in COMMANDS:
        (view / 'bin' / name).symlink_to('../lib64/sidravia/' + name)
    session_runtime = environment.get('XDG_RUNTIME_DIR', '')
    if not session_runtime or not Path(session_runtime).is_absolute():
        raise RuntimeError('installed GUI acceptance requires a desktop session runtime')
    def sandbox():
        prefix = ['bwrap', '--die-with-parent', '--ro-bind', '/', '/',
               '--dev', '/dev', '--proc', '/proc', '--dev-bind-try', '/dev/dri', '/dev/dri',
               '--tmpfs', '/run',
               '--bind', str(sysroot / 'run/sidravia'), '/run/sidravia',
               '--ro-bind', '/usr', str(host_alias), '--ro-bind', str(view), '/usr',
               '--ro-bind', str(sysroot / 'usr/share/applications'), '/usr/share/applications',
               '--ro-bind', str(sysroot / 'usr/share/icons/hicolor'), '/usr/share/icons/hicolor',
               '--tmpfs', '/home', '--bind', str(owned), str(owned),
               '--bind', session_runtime, session_runtime,
               '--bind', env['TMPDIR'], '/tmp', '--chdir', str(owned)]
        if not core:
            prefix += ['--ro-bind', environment['WAYLAND_DISPLAY'], environment['WAYLAND_DISPLAY']]
        if (sysroot / APP.relative_to('/')).exists():
            prefix += ['--ro-bind', str(sysroot / APP.relative_to('/')), str(APP)]
        return prefix
    driver_binary = owned / 'contract-driver'
    if not core:
        command([dart, 'compile', 'exe', 'tool/linux_portable_fixture.dart', '-o', str(driver_binary)], 'compile-contract-driver')

    def driver(operation):
        command(sandbox() + [str(driver_binary), operation, str(runtime), str(APP / 'sidravia_gui')], operation)

    def transaction_stopped_gui():
        if gui.wait(timeout=5) != 0:
            raise RuntimeError('maintenance did not gracefully exit installed GUI')
        wait_for(lambda: not runtime.exists(), 'maintenance stopped desktop daemon')
        events.append('RPM-maintenance-graceful-GUI-and-daemon-exit')

    def frozen_gui_blocks_upgrade():
        signal.pidfd_send_signal(gui_pidfd, signal.SIGSTOP)
        transaction = None
        try:
            with (owned / 'rpm-refused-live-upgrade.log').open('w') as log:
                transaction = subprocess.Popen(rpm + ['-Uvh', str(packages[1])], cwd=owned, env=env,
                    stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                def gate_busy():
                    try:
                        with (sysroot / 'run/sidravia/rpm.lock').open() as lock:
                            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
                        return False
                    except BlockingIOError:
                        return True
                wait_for(gate_busy, 'RPM transaction gate active')
                for name, args in (('sidravia_gui', []), ('sidraviactl', ['daemon', 'start'])):
                    blocked = subprocess.run(sandbox() + ['/usr/bin/' + name, *args], env=env, capture_output=True, text=True, timeout=8)
                    if blocked.returncode != 1:
                        raise RuntimeError('RPM transaction allowed a new startup')
                if transaction.wait(timeout=30) == 0:
                    raise RuntimeError('RPM replaced an unresponsive running application')
            command(rpm + ['-V', 'sidravia'], 'rpm-verify-refused-upgrade')
            release = subprocess.check_output(rpm + ['-q', '--qf', '%{RELEASE}', 'sidravia'], text=True)
            if release != receipts[0]['release']:
                raise RuntimeError('refused upgrade changed installed revision')
            events.append('unresponsive-GUI-blocks-upgrade-preserves-package-and-blocks-new-starts')
        finally:
            with contextlib.suppress(ProcessLookupError): signal.pidfd_send_signal(gui_pidfd, signal.SIGCONT)
            if transaction is not None and transaction.poll() is None:
                os.killpg(transaction.pid, signal.SIGTERM)
                transaction.wait(timeout=5)
        transaction_stopped_gui() # Pending SIGTERM now takes the normal exit path.

    def launch():
        nonlocal gui, gui_pid, gui_pidfd
        if gui_pidfd is not None:
            os.close(gui_pidfd)
            gui_pidfd = None
        pidfile = owned / 'gui.pid'
        pidfile.unlink(missing_ok=True)
        code = ('import os; from pathlib import Path; from gi.repository import Gio,GLib; '
                'app=Gio.DesktopAppInfo.new_from_filename(' + repr(f'/usr/share/applications/{DESKTOP}.desktop') + '); '
                'children=[]; assert app; '
                'assert app.launch_uris_as_manager([],Gio.AppLaunchContext(),GLib.SpawnFlags.DO_NOT_REAP_CHILD,None,None,lambda app,pid,data:children.append(pid),None); '
                'assert len(children)==1; Path(' + repr(str(pidfile)) + ').write_text(str(children[0])); '
                'result=os.waitpid(children[0],0); raise SystemExit(os.waitstatus_to_exitcode(result[1]))')
        with (owned / f'gui-{len(events)}.log').open('w') as log:
            gui = subprocess.Popen(sandbox() + ['/usr/bin/python3', '-c', code], env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        wait_for(lambda: pidfile.exists() or gui.poll() is not None, 'installed GUI exec')
        if gui.poll() is not None:
            raise RuntimeError('installed GUI launcher failed')
        gui_pid = int(pidfile.read_text())
        gui_pidfd = os.pidfd_open(gui_pid)
        wait_for(runtime.exists, 'installed GUI desktop daemon')
        events.append('installed-GUI-bootstrap')

    try:
        command(rpm + ['-ivh', str(packages[0])], 'rpm-install')
        installed = True
        # A real portable daemon with the same executable names must survive
        # every transaction. It owns no production configuration or sessions.
        bystander = owned / 'independent-portable'
        bystander.mkdir()
        for name in ('sidraviactl', 'sidraviad'):
            shutil.copy2(sysroot / APP.relative_to('/') / name, bystander / name)
        (bystander / 'sidravia.portable').touch()
        catalog = bystander / 'namespaces' / namespace / 'institution-profiles'
        catalog.mkdir(parents=True)
        shutil.copy2(profiles / 'linux-loopback.json', catalog / 'linux-loopback.json')
        bystander_runtime = catalog.parent / 'runtime.json'
        command([str(bystander / 'sidraviactl'), 'daemon', 'start'], 'independent-portable-start')
        bystander_identity = json.loads(bystander_runtime.read_text())['pid']
        command(rpm + ['-V', package_name], 'rpm-verify-install')
        if core:
            # Exercise real installed CTL/daemon and live maintenance without
            # Flutter, a desktop connection, account credentials or network IO.
            ctl = sandbox() + ['/usr/bin/sidraviactl']
            for args, label in ((['update', 'version'], 'core-version'), (['daemon', 'start'], 'core-daemon-start'),
                                (['daemon', 'status'], 'core-daemon-status')):
                command(ctl + args, label)
            wait_for(runtime.exists, 'installed core daemon runtime')
            saved = state / 'private-user-data'
            saved.write_bytes(b'fixture data must survive maintenance\n')
            saved_hash = digest(saved)
            update = subprocess.run(ctl + ['update', 'download', '--candidate', '1:2'], env=env, capture_output=True, text=True, timeout=15)
            if update.returncode != 1 or json.loads(update.stdout)['code'] != 'system_package_managed':
                raise RuntimeError('core RPM selected a portable update')
            events.append('core-package-manager-update-boundary')
            command(rpm + ['-Uvh', str(packages[1])], 'core-live-upgrade')
            wait_for(lambda: not runtime.exists(), 'upgrade stopped core daemon')
            command(rpm + ['-V', package_name], 'core-verify-upgrade')
            if subprocess.check_output(rpm + ['-q', '--qf', '%{RELEASE}', package_name], text=True) != receipts[1]['release']:
                raise RuntimeError('core RPM upgrade revision mismatch')
            command(ctl + ['daemon', 'start'], 'core-upgraded-daemon-start')
            wait_for(runtime.exists, 'upgraded core daemon runtime')
            command(rpm + ['-e', package_name], 'core-live-erase')
            installed = False
            wait_for(lambda: not runtime.exists(), 'erase stopped core daemon')
            if digest(saved) != saved_hash:
                raise RuntimeError('core RPM changed user data')
            if (sysroot / APP.relative_to('/')).exists() or any((sysroot / 'usr/bin' / n).is_symlink() for n in COMMANDS):
                raise RuntimeError('core RPM left package files after erase')
            if subprocess.run(rpm + ['-q', package_name], stdout=subprocess.DEVNULL).returncode != 1:
                raise RuntimeError('core RPM left package inventory after erase')
            if json.loads(bystander_runtime.read_text())['pid'] != bystander_identity:
                raise RuntimeError('core RPM displaced independent portable daemon')
            command([str(bystander / 'sidraviactl'), 'daemon', 'status'], 'independent-portable-survived')
            if not same_source(current_source(), source):
                raise RuntimeError('source changed during core RPM acceptance')
            result = {'passed': True, 'kind': 'core', 'source': source, 'source_unchanged': True,
                      'events': events, 'rpmSHA256': [r['sha256'] for r in receipts],
                      'dependencyChecks': 'enabled against host inventory snapshot',
                      'hostInstallation': False, 'realAccountIntegration': False, 'campusTraffic': False,
                      'signatures': 'unsigned local candidate; digest checks enabled'}
            (owned / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print('Core RPM isolated install/upgrade/erase PASS: ' + str(owned / 'result.json'))
            return 0
        for cache in ('usr/share/applications/mimeinfo.cache', 'usr/share/icons/hicolor/icon-theme.cache'):
            if not (sysroot / cache).is_file():
                raise RuntimeError('system RPM file trigger did not create ' + cache)
        if DESKTOP.encode() not in (sysroot / 'usr/share/icons/hicolor/icon-theme.cache').read_bytes():
            raise RuntimeError('RPM icon cache does not contain the installed application')
        events.append('system-desktop-and-icon-file-triggers')
        if (sysroot / APP.relative_to('/') / 'sidravia.portable').exists():
            raise RuntimeError('RPM incorrectly enabled portable mode')
        command(['desktop-file-validate', str(sysroot / f'usr/share/applications/{DESKTOP}.desktop')], 'desktop-validate')
        # The installed CLI may check release metadata but must refuse downloads.
        update = subprocess.run(sandbox() + ['/usr/bin/sidraviactl', 'update', 'download', '--candidate', '1:2'], env=env, capture_output=True, text=True, timeout=15)
        (owned / 'installed-updater.log').write_text(update.stdout + update.stderr)
        if not update.stdout.strip():
            raise RuntimeError('installed updater failed before JSON response; see installed-updater.log')
        response = json.loads(update.stdout)
        if update.returncode != 1 or response['code'] != 'system_package_managed':
            raise RuntimeError('installed updater selected a portable asset')
        events.append('updater-system-package-managed-without-network')
        launch()
        first_daemon = json.loads(runtime.read_text())['pid']
        command(sandbox() + ['/usr/bin/sidravia_gui', '--start-visible'], 'installed-single-instance')
        if json.loads(runtime.read_text())['pid'] != first_daemon:
            raise RuntimeError('installed activation changed daemon generation')
        driver('prepare')
        driver('verify')
        owner_start = Path(f'/proc/{gui_pid}/stat').read_text().rsplit(')', 1)[1].split()[19]
        command(sandbox() + ['/usr/bin/sidraviactl', 'gui', 'restart', '--owner-pid', str(gui_pid)], 'installed-core-restart', dict(env, SIDRAVIA_DESKTOP_OWNER_START=owner_start))
        driver('verify')
        driver('autostart-enable')
        entry, = (owned / 'config/autostart').glob('*.desktop')
        autostart_hash = digest(entry)
        driver('start-loopback')
        frozen_gui_blocks_upgrade()
        launch()
        driver('verify')
        driver('start-loopback')
        config_hash = digest(state / 'configurations.json')
        preferences_hash = digest(state / 'gui-settings.json')
        command(rpm + ['-Uvh', str(packages[1])], 'rpm-upgrade')
        transaction_stopped_gui()
        command(rpm + ['-V', 'sidravia'], 'rpm-verify-upgrade')
        identity = subprocess.check_output(rpm + ['-q', '--qf', '%{RELEASE}', 'sidravia'], text=True)
        if identity != receipts[1]['release']:
            raise RuntimeError('RPM upgrade did not replace the packaging revision')
        launch()
        driver('verify')
        command(rpm + ['-e', 'sidravia'], 'rpm-erase')
        transaction_stopped_gui()
        installed = False
        if digest(entry) != autostart_hash:
            raise RuntimeError('package maintenance rewrote user autostart preferences')
        command(sandbox() + ['/usr/bin/python3', '-c',
            missing_entry, str(entry)], 'removed-GUI-TryExec-inactive')
        # Reinstall does not enable anything; the retained explicit user choice
        # becomes available again. Exercise the same contract for headless mode.
        command(rpm + ['-ivh', str(packages[1])], 'rpm-reinstall')
        installed = True
        driver('autostart-daemon')
        code = ('import os,sys; from gi.repository import Gio,GLib; children=[]; '
            'app=Gio.DesktopAppInfo.new_from_filename(sys.argv[1]); assert app; '
            'assert app.launch_uris_as_manager([],Gio.AppLaunchContext(),GLib.SpawnFlags.DO_NOT_REAP_CHILD,None,None,lambda app,pid,data:children.append(pid),None); '
            'pid,status=os.waitpid(children[0],0); raise SystemExit(os.waitstatus_to_exitcode(status))')
        command(sandbox() + ['/usr/bin/python3', '-c', code, str(entry)], 'actual-XDG-headless-start')
        wait_for(runtime.exists, 'headless autostart runtime')
        driver('headless-verify')
        driver('headless-start-loopback')
        command(rpm + ['-e', 'sidravia'], 'rpm-live-headless-erase')
        installed = False
        wait_for(lambda: not runtime.exists(), 'RPM headless daemon graceful stop')
        command(sandbox() + ['/usr/bin/python3', '-c',
            missing_entry, str(entry)], 'removed-headless-TryExec-inactive')
        icon_cache = sysroot / 'usr/share/icons/hicolor/icon-theme.cache'
        if icon_cache.exists() and DESKTOP.encode() in icon_cache.read_bytes():
            raise RuntimeError('RPM uninstall retained stale application icon cache')
        if (sysroot / APP.relative_to('/')).exists() or any((sysroot / 'usr/bin' / n).is_symlink() for n in COMMANDS):
            raise RuntimeError('RPM uninstall retained package-owned programs')
        for name, attributes in receipts[1]['files'].items():
            path = sysroot / Path(name).relative_to('/')
            if attributes['digest'] or attributes['link']:
                if path.exists() or path.is_symlink():
                    raise RuntimeError('RPM uninstall retained package-owned file: ' + name)
        remaining = subprocess.run(rpm + ['-q', 'sidravia'], stdout=subprocess.DEVNULL)
        if remaining.returncode != 1:
            raise RuntimeError('RPM uninstall retained package inventory')
        if digest(state / 'configurations.json') != config_hash or digest(state / 'gui-settings.json') != preferences_hash:
            raise RuntimeError('upgrade/uninstall changed private user data')
        if not entry.exists() or (home / '.local/bin').exists():
            raise RuntimeError('RPM transaction changed explicit user integration')
        events.append('uninstall-preserves-XDG-data-and-inert-explicit-autostart')
        if json.loads(bystander_runtime.read_text())['pid'] != bystander_identity:
            raise RuntimeError('RPM transaction displaced independent portable daemon')
        command([str(bystander / 'sidraviactl'), 'daemon', 'status'], 'independent-portable-survived')
        if not same_source(current_source(), source):
            raise RuntimeError('source changed during RPM acceptance')
        result = {'passed': True, 'source': source, 'source_unchanged': True, 'events': events,
                  'rpmSHA256': [r['sha256'] for r in receipts], 'dependencyChecks': 'enabled against read-only host inventory snapshot',
                  'hostInstallation': False, 'realAccountIntegration': False, 'campusTraffic': False,
                  'rpmPlugins': 'disabled in user namespace; host SELinux/inhibit plugins not accepted',
                  'signatures': 'unsigned local candidate; all digest checks enabled',
                  'isolation': 'private root/RPM DB, shared private user+PID namespace, private session bus, host libraries read-only',
                  'trayAcceptance': 'private bus has no KDE tray host; lifecycle and Wayland window only'}
        (owned / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        print('RPM isolated install/upgrade/erase PASS: ' + str(owned / 'result.json'))
        return 0
    finally:
        try:
            if gui_pidfd is not None:
                with contextlib.suppress(ProcessLookupError): signal.pidfd_send_signal(gui_pidfd, signal.SIGCONT)
            if gui is not None and gui.poll() is None:
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(gui.pid, signal.SIGTERM)
                try:
                    gui.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    with contextlib.suppress(ProcessLookupError):
                        os.killpg(gui.pid, signal.SIGKILL)
                    gui.wait(timeout=5)
            # GUI death triggers the owned daemon's bounded owner observation.
            if core and runtime.exists():
                command(sandbox() + ['/usr/bin/sidraviactl', 'daemon', 'stop'], 'core-finally-stop')
            if runtime.exists():
                wait_for(lambda: not runtime.exists(), 'failed fixture owner cleanup')
            if installed:
                command(rpm + ['-e', package_name], 'rpm-finally-erase')
        finally:
            if bystander_runtime is not None and bystander_runtime.exists():
                command([str(bystander / 'sidraviactl'), 'daemon', 'stop'], 'independent-portable-stop')
            if gui_pidfd is not None:
                os.close(gui_pidfd)


if __name__ == '__main__':
    import dev
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', required=True)
    parser.add_argument('--dart')
    parser.add_argument('repository', type=Path)
    parser.add_argument('packages', nargs=2, type=Path)
    args = parser.parse_args()
    repository = args.repository.resolve()
    packages = args.packages
    receipts = [json.loads(Path(str(p) + '.json').read_text()) for p in packages]
    if receipts[0].get('kind') != 'core' and not args.dart:
        parser.error('--dart is required for GUI RPM acceptance')
    source = dev.metadata(args.go, os.environ)
    mapping = Path('/proc/self/uid_map').read_text().split()
    if (os.geteuid() != 0 or mapping[0] != '0' or mapping[1] == '0' or mapping[2] != '1' or
            Path('/proc/1/comm').read_text().strip() != 'dbus-run-sessio'):
        raise RuntimeError('owned user/PID session namespace is required')
    if any(not same_source(r['source'], source) for r in receipts):
        raise RuntimeError('private fixture source differs from producer')
    raise SystemExit(run(repository, packages, receipts, dict(os.environ), source,
                         lambda: dev.metadata(args.go, os.environ), dart=args.dart))
