"""Owned Linux desktop fixture; invoked only by dev.py linux --integration."""
import contextlib
import hashlib
import http.server
import json
import os
import re
from pathlib import Path
import signal
import shutil
import socket
import struct
import subprocess
import tempfile
import threading
import tarfile
import time
import uuid


def wait_for(condition, label, timeout=20):
    deadline = time.monotonic() + timeout
    while not condition():
        if time.monotonic() >= deadline:
            raise RuntimeError(f"timeout: {label}")
        time.sleep(.05)


def business_peer():
    """Small synthetic exchange, only loopback and fictional login material."""
    peer = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    peer.bind(("127.0.0.1", 0))
    peer.settimeout(.1)
    stopping = threading.Event()
    def serve():
        token = 0
        while not stopping.is_set():
            try:
                request, address = peer.recvfrom(4096)
            except socket.timeout:
                continue
            except OSError:
                return
            if not request:
                continue
            responses = []
            if len(request) == 20 and request[0] == 1:
                response = bytearray(44)
                response[0] = 2
                response[1:4] = request[1:4]
                response[4:8] = bytes.fromhex("11223344")
                responses = [response]
            elif request[0] == 3 and len(request) >= 20:
                token = int.from_bytes(request[4:6], "little")
                response = bytearray(64)
                response[0] = 4
                response[9:17] = struct.pack("<II", 123456, 12345)
                response[23:39] = bytes(range(1,17))
                response[43] = 1
                responses = [response]
            elif request[0] == 255:
                response = bytearray(20)
                response[0], response[2], response[4] = 7, 20, 6
                responses = [response]
            elif request[0] == 7 and len(request) == 40:
                response = bytearray(request)
                response[5] += 1
                response[16:24] = bytes(range(1,9))
                body = b"Synthetic server notice\x00"
                padded = body + b"\x00" * (len(body) % 2)
                checksum = sum(struct.unpack("<" + "H"*(len(padded)//2), padded))*token & 65535
                responses = [response, b"M8" + struct.pack("<H", checksum) + body]
            elif request[0] == 6:
                responses = [bytes([4]) + bytes(38)]
            for response in responses:
                peer.sendto(response, address)
    worker = threading.Thread(target=serve, daemon=True)
    worker.start()
    def close():
        stopping.set()
        worker.join(timeout=1)
        peer.close()
    return peer.getsockname()[1], close


class DesktopEntryChild:
    """Owned Gio-spawned child with explicit wait/reap, like Popen's subset."""
    def __init__(self, pid):
        self.pid = pid
        self.returncode = None
    def poll(self):
        if self.returncode is None:
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                self.returncode = os.waitstatus_to_exitcode(status)
        return self.returncode
    def wait(self, timeout):
        wait_for(lambda: self.poll() is not None, 'owned desktop child exit', timeout)
        return self.returncode
    def terminate(self):
        os.kill(self.pid, signal.SIGTERM)
    def kill(self):
        os.kill(self.pid, signal.SIGKILL)


def run(root, flutter, environment, source_before, source_metadata, portable_archive=None, *, expected_sha256=None):
    from gi.repository import Gio, GLib
    owned = Path(tempfile.mkdtemp(prefix="linux-desktop-fixture-", dir=root / ".local"))
    namespace = "linux-gui-test-" + uuid.uuid4().hex[:12]
    (owned / "fixture-owner").write_text(namespace)
    env = dict(environment, SIDRAVIA_NAMESPACE=namespace, SIDRAVIA_GUI_FIXTURE=str(owned), GDK_BACKEND="wayland", G_MESSAGES_DEBUG="all")
    if environment.get("SIDRAVIA_GUI_WAYLAND_TRACE") == "1":
        env["WAYLAND_DEBUG"] = "client"
    for key, folder in (("XDG_CONFIG_HOME", "config"), ("XDG_CACHE_HOME", "cache"), ("XDG_DATA_HOME", "data"), ("XDG_STATE_HOME", "state")):
        env[key] = str(owned / folder)
        (owned / folder).mkdir()
    business = env.get("SIDRAVIA_GUI_BUSINESS_FIXTURE") == "1"
    if business and portable_archive is not None:
        raise ValueError("business integration tests source, not a portable archive")
    home = owned / 'home'
    home.mkdir()
    env['HOME'] = str(home)
    env['PATH'] = str(home / '.local/bin') + os.pathsep + env.get('PATH', '')
    env['PUB_CACHE'] = environment.get('PUB_CACHE', str(Path(environment['HOME']) / '.pub-cache'))
    state = Path(env["XDG_CONFIG_HOME"]) / "Sidravia" / "namespaces" / namespace
    package_root = None
    archive_sha256 = None
    if portable_archive is not None:
        # Input has already passed the distribution consumer; still reject links
        # and traversal at extraction. Keep this fixture under the project.
        extracted = owned / 'extracted with spaces'
        extracted.mkdir()
        with tempfile.TemporaryFile(dir=owned) as snapshot:
            with portable_archive.open('rb') as incoming:
                shutil.copyfileobj(incoming, snapshot)
            snapshot.seek(0)
            archive_sha256 = hashlib.file_digest(snapshot, 'sha256').hexdigest()
            if archive_sha256 != expected_sha256:
                raise ValueError('package does not match the verified producer receipt')
            snapshot.seek(0)
            with tarfile.open(fileobj=snapshot, mode='r:*') as archive:
                archive.extractall(extracted, filter='data')
        package_root, = extracted.iterdir()
        if not (package_root / 'sidravia.portable').is_file():
            raise RuntimeError('missing portable marker')
        state = package_root / 'namespaces' / namespace
    business_port, close_business = business_peer() if business else (9, lambda: None)
    profiles = state / "institution-profiles"
    profiles.mkdir(parents=True)
    # Use the public protocol shape, replace every network endpoint before use.
    profile = json.loads((root / "internal/daemon/configuration/profiles/jlu.json").read_text())
    profile.update(institutionProfileId="linux-loopback", displayName="Linux synthetic loopback")
    profile.pop("announcement", None)
    profile["institutionProtocolConfiguration"].update(serverAddress="127.0.0.1", serverPort=business_port, localPort={"mode": "system_assigned"})
    if business:
        profile["institutionProtocolConfiguration"]["behavior"] = "jlu-experimental"
    (profiles / "linux-loopback.json").write_text(json.dumps(profile))
    runtime = state / "runtime.json"
    events = []
    if portable_archive is not None:
        # status performs no release query. Its secured cache location exposes
        # the selected package kind without replacing the product's network.
        with (owned / 'updater-auto-package.log').open('w') as log:
            subprocess.run([str(package_root / 'sidraviaupdater'), 'status', '--channel', 'stable'], env=env, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=15)
        key = hashlib.sha256((str(package_root) + '\0gui-portable\0stable').encode()).hexdigest()[:32]
        cache = Path(env['XDG_CACHE_HOME']) / 'Sidravia/updates'
        if not (cache / key).is_dir() or {p.name for p in cache.iterdir()} != {key, 'operations'}:
            raise RuntimeError('packaged updater did not automatically select GUI identity')
        events.append('packaged-updater-auto-gui-identity-without-network')

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            (owned / "notice-requested").touch()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers()
            self.wfile.write(b'<!doctype html><style>:root{color-scheme:light dark}</style><title>Sidravia fixture</title><h1>Local announcement fixture</h1>')
        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    env["SIDRAVIA_GUI_HTTP_PORT"] = str(server.server_port)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    bus = Gio.bus_get_sync(Gio.BusType.SESSION, None)
    gui = None
    headless_attempted = False

    def command(argv, name, cwd=root / "gui", timeout=240):
        with (owned / (name + ".log")).open("w") as log:
            process = subprocess.Popen(argv, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = process.wait(timeout=timeout)
            finally:
                # This process group belongs only to this fixture command. Its
                # GUI must not outlive a failed/hung Flutter driver.
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    with contextlib.suppress(ProcessLookupError):
                        os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(process.pid, signal.SIGKILL)
        if code:
            raise RuntimeError(f"{name} failed ({code}); see {owned / (name + '.log')}")
        events.append(name)

    def call(service, path, interface, method, parameters=None):
        return bus.call_sync(service, path, interface, method, parameters, None, Gio.DBusCallFlags.NONE, 3000, None).unpack()

    def menu_action(label):
        owner = [None, None]
        def registered():
            items = call("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher", "org.freedesktop.DBus.Properties", "Get", GLib.Variant("(ss)", ("org.kde.StatusNotifierWatcher", "RegisteredStatusNotifierItems")))[0]
            for item in items:
                service, separator, suffix = item.partition("/")
                try:
                    process = call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "GetConnectionUnixProcessID", GLib.Variant("(s)", (service,)))[0]
                    if process == gui.pid:
                        owner[:] = [service, "/" + suffix if separator else "/StatusNotifierItem"]
                        return True
                except GLib.Error:
                    continue
            return False
        wait_for(registered, "owned StatusNotifier registration", timeout=5)
        service, item_path = owner
        icon = call(service, item_path, "org.freedesktop.DBus.Properties", "Get", GLib.Variant("(ss)", ("org.kde.StatusNotifierItem", "IconName")))[0]
        expected_icon = binary.parent / ("gui-runtime/data/icons/sidravia.png" if (binary.parent / "gui-runtime").is_dir() else "data/icons/sidravia.png")
        if icon != str(expected_icon) or expected_icon.read_bytes() != (root / "gui/linux/runner/resources/sidravia.png").read_bytes():
            raise RuntimeError("tray application icon does not match the accepted resource")
        menu = call(service, item_path, "org.freedesktop.DBus.Properties", "Get", GLib.Variant("(ss)", ("org.kde.StatusNotifierItem", "Menu")))[0]
        def get_layout():
            return call(service, menu, "com.canonical.dbusmenu", "GetLayout", GLib.Variant("(iias)", (0, -1, [])))[1]
        def ready():
            return any(node[1].get("label", "").startswith("尚未配置") or (portable_archive is not None and "未连接" in node[1].get("label", "")) for node in get_layout()[2])
        # runtime.json precedes completion of GUI bootstrap. Wait for the actual
        # shared read model, otherwise exit correctly asks to cancel startup.
        wait_for(ready, "GUI snapshot reflected in real tray")
        layout = get_layout()
        def find(node):
            item, properties, children = node
            if properties.get("label") == label:
                return item
            for child in children:
                found = find(child)
                if found is not None:
                    return found
            return None
        item = find(layout)
        if item is None:
            raise RuntimeError(f"missing tray action {label}")
        call(service, menu, "com.canonical.dbusmenu", "Event", GLib.Variant("(isvu)", (item, "clicked", GLib.Variant("i", 0), 0)))

    def launch_entry():
        entry, = (Path(env['XDG_CONFIG_HOME']) / 'autostart').glob('*.desktop')
        application = Gio.DesktopAppInfo.new_from_filename(str(entry))
        context = Gio.AppLaunchContext()
        for key, value in env.items(): context.setenv(key,value)
        children = []
        launched = application.launch_uris_as_manager([],context,
            GLib.SpawnFlags.DO_NOT_REAP_CHILD | GLib.SpawnFlags.STDOUT_TO_DEV_NULL | GLib.SpawnFlags.STDERR_TO_DEV_NULL,
            None,None,lambda app,child,data:children.append(child),None)
        if not launched or len(children)!=1: raise RuntimeError('owned XDG desktop launch failed')
        return DesktopEntryChild(children[0])

    receipt = {"source": source_before, "source_unchanged": False, "scenarioPassed": False, "passed": False,
               "namespace": namespace, "events": events, "campusAuthentication": False,
               "realAccountAutostart": False, "wayland": env.get("WAYLAND_DISPLAY"),
               "portableArchive": str(portable_archive) if portable_archive else None}
    if archive_sha256 is not None:
        receipt["portableArchiveSHA256"] = archive_sha256
    try:
        if business:
            command([flutter, "test", "--no-pub", "-d", "linux", "integration_test/linux_business_test.dart"], "business-real-chain")
            wait_for(lambda: not runtime.exists(), "business daemon cleanup")
            source_after = source_metadata()
            unchanged = all(source_before[key] == source_after[key] for key in ("commit", "status", "source_fingerprint"))
            if not unchanged:
                raise RuntimeError("source changed during business verification")
            receipt.update(source_unchanged=True, scenarioPassed=True, passed=True)
            return 0
        if portable_archive is None:
            command([flutter, "test", "--no-pub", "-d", "linux", "integration_test/linux_desktop_test.dart"], "production-main-integration")
            wait_for(lambda: not runtime.exists(), "integration daemon cleanup")
            env["SIDRAVIA_GUI_RESTORED"] = "1"
            command([flutter, "test", "--no-pub", "-d", "linux", "integration_test/linux_desktop_test.dart"], "production-main-restart-preferences-hidden-window")
            wait_for(lambda: not runtime.exists(), "second integration daemon cleanup")
            preferences = json.loads((state / "gui-settings.json").read_text())
            if preferences["appearanceMode"] != "dark":
                raise RuntimeError("persisted appearance mismatch")
            command([flutter, "build", "linux", "--release", "--no-pub"], "release-build")
            binary = root / "gui/build/linux/x64/release/bundle/sidravia_gui"
        else:
            binary = package_root / 'sidravia_gui'
            dart = str(Path(flutter).resolve().with_name('dart'))
            def driver(operation):
                command([dart,'run','tool/linux_portable_fixture.dart',operation,str(runtime)],'portable-'+operation)
            # Refuse before the dynamic loader touches product runtime bytes.
            seal = package_root / 'sidravia.update'
            seal.write_text('synthetic-interrupted-update\n')
            blocked = subprocess.run([str(binary), '--skip-auto-login'], env=dict(env, LD_DEBUG='libs'),
                                     capture_output=True, text=True, timeout=10)
            if blocked.returncode != 1 or 'libflutter_linux_gtk' in blocked.stderr or runtime.exists():
                raise RuntimeError('portable pre-loader startup seal was bypassed')
            seal.unlink()
            events.append('portable-seal-refused-before-Flutter-load')
            driver('path-enable')
            command(['/bin/bash','--noprofile','--norc','-c','command -v sidraviactl && sidraviactl --help && sidraviaupdater --help'],'owned-path-commands',cwd=owned)
            events.append('owned-HOME-command-links-executed-in-new-shell')
        def start_gui():
            log = (owned / f"gui-{len(events)}.log").open("w")
            process = subprocess.Popen([str(home / ".local/bin/sidravia_gui") if portable_archive else str(binary), "--skip-auto-login", "--start-visible"], env=dict(env, WAYLAND_DEBUG="client"), stdout=log, stderr=subprocess.STDOUT)
            log.close()
            return process
        normal_trace = owned / f"gui-{len(events)}.log"
        gui = start_gui()
        wait_for(runtime.exists, "release GUI real daemon")
        daemon = json.loads(runtime.read_text())["pid"]
        if portable_archive is not None:
            gate = Path(env['XDG_CACHE_HOME']) / 'Sidravia/updates/operations' / hashlib.sha256(str(binary.parent.resolve()).encode()).hexdigest() / 'execution.lock'
            inherited = []
            for fd in Path(f'/proc/{gui.pid}/fd').iterdir():
                try:
                    if os.readlink(fd) == str(gate):
                        inherited.append(fd)
                except FileNotFoundError:
                    pass  # Unrelated GTK descriptors can close during inspection.
            if len(inherited) != 1:
                raise RuntimeError('GUI did not retain its exact pre-loader lease')
            flags = int(next(line.split()[1] for line in (Path(f'/proc/{gui.pid}/fdinfo') / inherited[0].name).read_text().splitlines() if line.startswith('flags:')), 8)
            if not flags & os.O_CLOEXEC:
                raise RuntimeError('GUI lease can leak into child exec')
            events.append('GUI-exec-PID-retains-cloexec-admission')
        command([str(binary), "--start-visible"], "single-instance-activation", timeout=15)
        if json.loads(runtime.read_text())["pid"] != daemon:
            raise RuntimeError("second instance changed daemon generation")
        if portable_archive is not None:
            driver('prepare')
            stat = Path(f'/proc/{gui.pid}/stat').read_text()
            owner_start = stat[stat.rfind(')') + 2:].split()[19]
            for index in range(3):
                with (owned / f'core-restart-{index}.log').open('w') as log:
                    subprocess.run([str(binary.parent / 'sidraviactl'), 'gui', 'restart', '--owner-pid', str(gui.pid)], env=dict(env, SIDRAVIA_DESKTOP_OWNER_START=owner_start), stdout=log, stderr=subprocess.STDOUT, check=True, timeout=35)
                driver('verify')
            events.append('packaged-config-and-three-core-restarts')
        # Real shell menu -> Dart exit coordinator -> daemon.stop -> process exit.
        menu_action("打开主界面")
        menu_action("退出")
        gui.wait(timeout=20)
        wait_for(lambda: not runtime.exists(), "normal exit daemon cleanup")
        events.append("release-tray-normal-exit")
        if portable_archive is not None:
            driver('autostart-enable')
            driver('autostart-disable')
            driver('path-disable')
            moved = package_root.parent / 'moved portable with spaces'
            package_root.rename(moved)
            package_root = moved
            binary = moved / 'sidravia_gui'
            state = moved / 'namespaces' / namespace
            runtime = state / 'runtime.json'
            events.append('whole-portable-directory-relocated')
        if portable_archive is not None:
            driver('autostart-enable')
            driver('path-enable')
            gui = launch_entry()
            events.append('actual-packaged-xdg-autostart-entry-launch')
        else:
            gui = start_gui()
        wait_for(runtime.exists, "crash GUI daemon ready")
        if portable_archive is not None:
            driver('verify')
            menu_action('打开主界面')
            events.append('relocated-autostart-owned-tray-ready-and-restored')
            driver('cleanup')
            driver('autostart-disable')
            events.append('portable-config-preferences-restored-after-relocation-and-session-cancelled')

        start = time.monotonic()
        gui.kill()
        gui.wait(timeout=5)
        wait_for(lambda: not runtime.exists(), "crashed owner bounded cleanup", timeout=10)
        events.append(f"owner-crash-cleanup-{time.monotonic() - start:.3f}s")
        if portable_archive is not None:
            driver('autostart-daemon')
            headless_attempted = True
            starter = launch_entry()
            if starter.wait(timeout=20) != 0: raise RuntimeError('headless autostart failed')
            wait_for(runtime.exists,'owned headless daemon ready')
            headless_pid = json.loads(runtime.read_text())['pid']
            driver('headless-verify')
            command(['sidraviactl','daemon','status'],'owned-path-headless-status',cwd=owned)
            repeat = launch_entry()
            if repeat.wait(timeout=20) != 0 or json.loads(runtime.read_text())['pid'] != headless_pid:
                raise RuntimeError('repeat headless autostart changed daemon generation')
            driver('autostart-enable')
            driver('headless-verify')
            if json.loads(runtime.read_text())['pid'] != headless_pid:
                raise RuntimeError('registration change stopped or adopted headless daemon')
            driver('autostart-disable')
            command(['sidraviactl','daemon','stop'],'owned-path-headless-stop',cwd=owned)
            wait_for(lambda:not runtime.exists(),'owned headless daemon stopped')
            driver('path-disable')
            events.append('actual-XDG-headless-start-repeat-status-stop-and-mode-switch')
            events.append('owned-path-and-autostart-removed-without-live-owner-takeover')
        # These are real compositor responses/requests from only the owned
        # release GUI, not Gtk's decoration property or a client-area screenshot.
        protocol_evidence = []
        # Crash cleanup intentionally permits termination before first paint.
        # Prove decoration/icon delivery requests on the fully ready normal GUI.
        for log in [normal_trace]:
            trace = log.read_text()
            client_ack = re.search(r"org_kde_kwin_server_decoration[#@](\d+)\.mode\(1\)", trace)
            icon_set = re.search(r"xdg_toplevel_icon_manager_v1[#@]\d+\.set_icon\(xdg_toplevel[#@](\d+), xdg_toplevel_icon_v1[#@](\d+)\)", trace)
            if not client_ack or not icon_set:
                raise RuntimeError("missing KWin client-decoration acknowledgement or window-icon submission: " + str(log))
            surface = re.search(r"get_xdg_surface\(new id xdg_surface[#@](\d+), wl_surface[#@](\d+)\)", trace)
            if not surface or not re.search(r"wl_surface[#@]" + surface[2] + r"\.commit\(\)", trace[icon_set.end():]):
                raise RuntimeError("window icon lacks a subsequent native surface commit")
            protocol_evidence.append({"log": str(log), "clientDecorationAcknowledged": True, "windowIconSubmittedAndSurfaceCommitted": True})
        source_after = source_metadata()
        unchanged = all(source_before[key] == source_after[key] for key in ("commit", "status", "source_fingerprint"))
        if not unchanged:
            raise RuntimeError("source changed during native verification; rerun with stable source")
        receipt.update(waylandProtocolEvidence=protocol_evidence, source_unchanged=True, scenarioPassed=True, passed=True)
        return 0
    finally:
        def stop_gui():
            if gui is not None and gui.poll() is None:
                gui.terminate()
                try:
                    gui.wait(timeout=5)
                except (subprocess.TimeoutExpired, RuntimeError):
                    gui.kill()
                    gui.wait(timeout=5)

        def stop_headless():
            if headless_attempted and runtime.exists():
                with (owned / 'headless-finally-stop.log').open('w') as log:
                    subprocess.run([str(binary.parent/'sidraviactl'),'daemon','stop'],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=15,check=True)

        def clear_registration(operation):
            with (owned / (operation+'-finally.log')).open('w') as log:
                subprocess.run([dart,'run','tool/linux_portable_fixture.dart',operation,str(runtime)],cwd=root/'gui',env=env,stdout=log,stderr=subprocess.STDOUT,timeout=30,check=True)

        def join_server():
            worker.join(timeout=3)
            if worker.is_alive():
                raise RuntimeError("fixture server did not finish")

        try:
            # Callbacks run in reverse order; every independent resource
            # still gets a cleanup attempt when an earlier callback fails.
            with contextlib.ExitStack() as cleanup:
                cleanup.callback(server.server_close)
                cleanup.callback(join_server)
                cleanup.callback(server.shutdown)
                cleanup.callback(close_business)
                cleanup.callback(wait_for, lambda: not runtime.exists(), "fixture owner cleanup", timeout=10)
                if portable_archive is not None and 'driver' in locals():
                    cleanup.callback(clear_registration, 'path-disable')
                    cleanup.callback(clear_registration, 'autostart-disable')
                cleanup.callback(stop_headless)
                cleanup.callback(stop_gui)
        except BaseException:
            receipt.update(cleanup="failed", passed=False)
            raise
        else:
            receipt["cleanup"] = "passed"
        finally:
            receipt_path = owned / "result.json"
            receipt_path.write_text(json.dumps(receipt, indent=2) + "\n")
            if receipt["passed"]:
                print(f"Linux native fixture PASS: {receipt_path}")
