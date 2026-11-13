"""Owned Linux desktop fixture; invoked only by dev.py linux --integration."""
import contextlib
import http.server
import json
import os
import re
from pathlib import Path
import signal
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


def run(root, flutter, environment, source_before, source_metadata, portable_archive=None):
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
    state = Path(env["XDG_CONFIG_HOME"]) / "Sidravia" / "namespaces" / namespace
    package_root = None
    if portable_archive is not None:
        # Input has already passed the distribution consumer; still reject links
        # and traversal at extraction. Keep this fixture under the project.
        extracted = owned / 'extracted with spaces'
        extracted.mkdir()
        with tarfile.open(portable_archive, 'r:gz') as archive:
            archive.extractall(extracted, filter='data')
        package_root, = extracted.iterdir()
        if not (package_root / 'sidravia.portable').is_file():
            raise RuntimeError('missing portable marker')
        state = package_root / 'namespaces' / namespace
    profiles = state / "institution-profiles"
    profiles.mkdir(parents=True)
    # Use the public protocol shape, replace every network endpoint before use.
    profile = json.loads((root / "internal/daemon/configuration/profiles/jlu.json").read_text())
    profile.update(institutionProfileId="linux-loopback", displayName="Linux synthetic loopback")
    profile.pop("announcement", None)
    profile["institutionProtocolConfiguration"].update(serverAddress="127.0.0.1", serverPort=9, localPort={"mode": "system_assigned"})
    (profiles / "linux-loopback.json").write_text(json.dumps(profile))
    runtime = state / "runtime.json"
    events = []

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
        expected_icon = binary.parent / "data/icons/sidravia.png"
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

    try:
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
        def start_gui():
            log = (owned / f"gui-{len(events)}.log").open("w")
            process = subprocess.Popen([str(binary), "--skip-auto-login", "--start-visible"], env=dict(env, WAYLAND_DEBUG="client"), stdout=log, stderr=subprocess.STDOUT)
            log.close()
            return process
        normal_trace = owned / f"gui-{len(events)}.log"
        gui = start_gui()
        wait_for(runtime.exists, "release GUI real daemon")
        daemon = json.loads(runtime.read_text())["pid"]
        command([str(binary), "--start-visible"], "single-instance-activation", timeout=15)
        if json.loads(runtime.read_text())["pid"] != daemon:
            raise RuntimeError("second instance changed daemon generation")
        if portable_archive is not None:
            dart = str(root / '.local/tools/flutter-linux-3.47.0/bin/dart')
            def driver(operation):
                command([dart, 'run', 'tool/linux_portable_fixture.dart', operation, str(runtime)], 'portable-' + operation)
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
            moved = package_root.parent / 'moved portable with spaces'
            package_root.rename(moved)
            package_root = moved
            binary = moved / 'sidravia_gui'
            state = moved / 'namespaces' / namespace
            runtime = state / 'runtime.json'
            events.append('whole-portable-directory-relocated')
        gui = start_gui()
        wait_for(runtime.exists, "crash GUI daemon ready")
        if portable_archive is not None:
            driver('verify')
            driver('cleanup')
            events.append('portable-config-preferences-restored-after-relocation-and-session-cancelled')

        start = time.monotonic()
        gui.kill()
        gui.wait(timeout=5)
        wait_for(lambda: not runtime.exists(), "crashed owner bounded cleanup", timeout=10)
        events.append(f"owner-crash-cleanup-{time.monotonic() - start:.3f}s")
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
        receipt = {"waylandProtocolEvidence": protocol_evidence, "source": source_before, "source_unchanged": unchanged, "passed": True, "namespace": namespace, "events": events, "wayland": env.get("WAYLAND_DISPLAY"), "campusAuthentication": False, "realAccountAutostart": False, "portableArchive": str(portable_archive) if portable_archive else None}
        (owned / "result.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print(f"Linux native fixture PASS: {owned / 'result.json'}")
        return 0
    finally:
        if gui is not None and gui.poll() is None:
            gui.terminate()
            try:
                gui.wait(timeout=5)
            except subprocess.TimeoutExpired:
                gui.kill()
                gui.wait(timeout=5)
        wait_for(lambda: not runtime.exists(), "fixture owner cleanup", timeout=10)
        server.shutdown()
        worker.join(timeout=3)
        server.server_close()
        # Retain synthetic files and logs as evidence, never clean by directory name.
