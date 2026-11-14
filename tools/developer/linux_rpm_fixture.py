"""Real RPM transactions in a private root; installed GUI uses a mount sandbox.

Only the RPM dependency inventory and runtime system libraries are borrowed from
the host, read-only. No caller-selectable install root or namespace is accepted.
"""
import contextlib
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


def run(root, packages, receipts, environment, source, current_source):
    from gi.repository import Gio, GLib
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
    events = []
    gui = None
    gui_pid = None
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
    rpm = ['bwrap', '--unshare-user', '--die-with-parent', '--uid', '0', '--gid', '0', '--unshare-net',
           '--cap-add', 'CAP_SYS_CHROOT', '--ro-bind', '/', '/', '--bind', str(sysroot), str(sysroot),
           '--bind', str(sysroot / 'tmp'), '/tmp', '--setenv', 'TMPDIR', '/tmp',
           '--ro-bind', '/usr', str(sysroot / host_alias.relative_to('/')), '--chdir', str(owned),
           '--dev', str(sysroot / 'dev'), '--proc', str(sysroot / 'proc'),
           'rpm', '--root', str(sysroot), '--dbpath', dbpath, '--nosignature', '--noplugins']

    # Do not install over an existing Sidravia system package even in the clone:
    # an unexpected inventory deserves diagnosis rather than silent replacement.
    existing = subprocess.run(rpm + ['-q', 'sidravia'], stdout=subprocess.DEVNULL)
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
    sandbox = ['bwrap', '--unshare-user', '--die-with-parent', '--ro-bind', '/', '/',
               '--dev', '/dev', '--proc', '/proc', '--dev-bind-try', '/dev/dri', '/dev/dri',
               '--tmpfs', '/run', '--bind', session_runtime, session_runtime,
               '--ro-bind', '/usr', str(host_alias), '--ro-bind', str(view), '/usr',
               '--ro-bind', str(sysroot / APP.relative_to('/')), str(APP),
               '--ro-bind', str(sysroot / 'usr/share/applications'), '/usr/share/applications',
               '--ro-bind', str(sysroot / 'usr/share/icons/hicolor'), '/usr/share/icons/hicolor',
               '--tmpfs', '/home', '--bind', str(owned), str(owned),
               '--bind', env['TMPDIR'], '/tmp', '--chdir', str(owned)]
    dart = str(root / '.local/tools/flutter-linux-3.47.0/bin/dart')

    def driver(operation):
        command([dart, 'run', 'tool/linux_portable_fixture.dart', operation, str(runtime)], operation)

    bus = Gio.bus_get_sync(Gio.BusType.SESSION, None)

    def call(service, path, interface, method, params=None):
        return bus.call_sync(service, path, interface, method, params, None, Gio.DBusCallFlags.NONE, 3000, None).unpack()

    def tray_exit():
        found = [None]
        def ready():
            items = call('org.kde.StatusNotifierWatcher', '/StatusNotifierWatcher', 'org.freedesktop.DBus.Properties', 'Get',
                         GLib.Variant('(ss)', ('org.kde.StatusNotifierWatcher', 'RegisteredStatusNotifierItems')))[0]
            for item in items:
                service, sep, path = item.partition('/')
                try:
                    pid = call('org.freedesktop.DBus', '/org/freedesktop/DBus', 'org.freedesktop.DBus', 'GetConnectionUnixProcessID', GLib.Variant('(s)', (service,)))[0]
                    if pid != gui_pid:
                        continue
                    icon = call(service, '/' + path if sep else '/StatusNotifierItem', 'org.freedesktop.DBus.Properties', 'Get', GLib.Variant('(ss)', ('org.kde.StatusNotifierItem', 'IconName')))[0]
                    if icon != str(APP / 'data/icons/sidravia.png'):
                        raise RuntimeError('installed tray icon path mismatch')
                    menu = call(service, '/' + path if sep else '/StatusNotifierItem', 'org.freedesktop.DBus.Properties', 'Get', GLib.Variant('(ss)', ('org.kde.StatusNotifierItem', 'Menu')))[0]
                    layout = call(service, menu, 'com.canonical.dbusmenu', 'GetLayout', GLib.Variant('(iias)', (0, -1, [])))[1]
                    # Exit only after a real shared snapshot reaches the tray.
                    labels = [node[1].get('label', '') for node in layout[2]]
                    if not any('未连接' in s for s in labels):
                        return False
                    for node in layout[2]:
                        if node[1].get('label') == '退出':
                            found[0] = (service, menu, node[0])
                            return True
                except GLib.Error:
                    continue
            return False
        wait_for(ready, 'installed real tray snapshot')
        service, menu, item = found[0]
        call(service, menu, 'com.canonical.dbusmenu', 'Event', GLib.Variant('(isvu)', (item, 'clicked', GLib.Variant('i', 0), 0)))
        if gui.wait(timeout=20) != 0:
            raise RuntimeError('installed GUI failed during normal tray exit')
        wait_for(lambda: not runtime.exists(), 'installed GUI normal owner cleanup')
        events.append('installed-tray-exit')

    def launch():
        nonlocal gui, gui_pid
        pidfile = owned / 'gui.pid'
        pidfile.unlink(missing_ok=True)
        code = ('import os; from pathlib import Path; from gi.repository import Gio,GLib; '
                'app=Gio.DesktopAppInfo.new_from_filename(' + repr(f'/usr/share/applications/{DESKTOP}.desktop') + '); '
                'children=[]; assert app; '
                'assert app.launch_uris_as_manager([],Gio.AppLaunchContext(),GLib.SpawnFlags.DO_NOT_REAP_CHILD,None,None,lambda app,pid,data:children.append(pid),None); '
                'assert len(children)==1; Path(' + repr(str(pidfile)) + ').write_text(str(children[0])); '
                'result=os.waitpid(children[0],0); raise SystemExit(os.waitstatus_to_exitcode(result[1]))')
        with (owned / f'gui-{len(events)}.log').open('w') as log:
            gui = subprocess.Popen(sandbox + ['/usr/bin/python3', '-c', code], env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        wait_for(lambda: pidfile.exists() or gui.poll() is not None, 'installed GUI exec')
        if gui.poll() is not None:
            raise RuntimeError('installed GUI launcher failed')
        gui_pid = int(pidfile.read_text())
        wait_for(runtime.exists, 'installed GUI desktop daemon')
        events.append('installed-GUI-bootstrap')

    try:
        command(rpm + ['-ivh', str(packages[0])], 'rpm-install')
        installed = True
        command(rpm + ['-V', 'sidravia'], 'rpm-verify-install')
        for cache in ('usr/share/applications/mimeinfo.cache', 'usr/share/icons/hicolor/icon-theme.cache'):
            if not (sysroot / cache).is_file():
                raise RuntimeError('system RPM file trigger did not create ' + cache)
        if DESKTOP.encode() not in (sysroot / 'usr/share/icons/hicolor/icon-theme.cache').read_bytes():
            raise RuntimeError('RPM icon cache does not contain the installed application')
        events.append('system-desktop-and-icon-file-triggers')
        if (sysroot / APP.relative_to('/') / 'sidravia.portable').exists():
            raise RuntimeError('RPM incorrectly enabled portable mode')
        command(['desktop-file-validate', str(sysroot / f'usr/share/applications/{DESKTOP}.desktop')], 'desktop-validate')
        # The installed updater must refuse network access/archive selection.
        update = subprocess.run(sandbox + ['/usr/bin/sidraviaupdater', 'check'], env=env, capture_output=True, text=True, timeout=15)
        (owned / 'installed-updater.log').write_text(update.stdout + update.stderr)
        if not update.stdout.strip():
            raise RuntimeError('installed updater failed before JSON response; see installed-updater.log')
        response = json.loads(update.stdout)
        if update.returncode != 1 or response['code'] != 'system_package_managed':
            raise RuntimeError('installed updater selected a portable asset')
        events.append('updater-system-package-managed-without-network')
        launch()
        first_daemon = json.loads(runtime.read_text())['pid']
        command(sandbox + ['/usr/bin/sidravia_gui', '--start-visible'], 'installed-single-instance')
        if json.loads(runtime.read_text())['pid'] != first_daemon:
            raise RuntimeError('installed activation changed daemon generation')
        driver('prepare')
        driver('verify')
        owner_start = Path(f'/proc/{gui_pid}/stat').read_text().rsplit(')', 1)[1].split()[19]
        command(sandbox + ['/usr/bin/sidraviactl', 'gui', 'restart', '--owner-pid', str(gui_pid)], 'installed-core-restart', dict(env, SIDRAVIA_DESKTOP_OWNER_START=owner_start))
        driver('verify')
        tray_exit()
        config_hash = digest(state / 'configurations.json')
        preferences_hash = digest(state / 'gui-settings.json')
        command(rpm + ['-Uvh', str(packages[1])], 'rpm-upgrade')
        command(rpm + ['-V', 'sidravia'], 'rpm-verify-upgrade')
        identity = subprocess.check_output(rpm + ['-q', '--qf', '%{RELEASE}', 'sidravia'], text=True)
        if identity != receipts[1]['release']:
            raise RuntimeError('RPM upgrade did not replace the packaging revision')
        launch()
        driver('verify')
        tray_exit()
        command(rpm + ['-e', 'sidravia'], 'rpm-erase')
        installed = False
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
        if list((owned / 'config').glob('autostart/*.desktop')) or (home / '.local/bin').exists():
            raise RuntimeError('RPM transaction unexpectedly registered user integration')
        events.append('uninstall-preserves-XDG-data-no-user-autostart-or-links')
        if not same_source(current_source(), source):
            raise RuntimeError('source changed during RPM acceptance')
        result = {'passed': True, 'source': source, 'source_unchanged': True, 'events': events,
                  'rpmSHA256': [r['sha256'] for r in receipts], 'dependencyChecks': 'enabled against read-only host inventory snapshot',
                  'hostInstallation': False, 'realAccountIntegration': False, 'campusTraffic': False,
                  'rpmPlugins': 'disabled in user namespace; host SELinux/inhibit plugins not accepted',
                  'signatures': 'unsigned local candidate; all digest checks enabled',
                  'isolation': 'private root/RPM DB, user+mount namespace, host libraries read-only'}
        (owned / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        print('RPM isolated install/upgrade/erase PASS: ' + str(owned / 'result.json'))
        return 0
    finally:
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
        if runtime.exists():
            wait_for(lambda: not runtime.exists(), 'failed fixture owner cleanup')
        if installed:
            command(rpm + ['-e', 'sidravia'], 'rpm-finally-erase')
