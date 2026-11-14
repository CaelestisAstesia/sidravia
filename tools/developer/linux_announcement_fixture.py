"""Owned loopback WebKitGTK lifecycle acceptance, through dev.py linux."""
import contextlib
import http.server
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import threading
import time


def run(root, env, before, metadata):
    owned = Path(tempfile.mkdtemp(prefix='linux-announcement-fixture-', dir=root / '.local'))
    (owned / 'fixture-owner').write_text('linux-announcement-native\n')
    build = root / '.local/linux-native-policies'
    for argv in (['cmake','-S',str(root/'gui/test/native'),'-B',str(build),'-G','Ninja','-DCMAKE_BUILD_TYPE=Release'],
                 ['cmake','--build',str(build)],['ctest','--test-dir',str(build),'--output-on-failure']):
        subprocess.run(argv,cwd=root,env=env,check=True)
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path in ('/delayed','/hang'):
                time.sleep(5 if self.path == '/delayed' else 33)
            if self.path == '/broken':
                self.close_connection = True
                return
            self.send_response(302 if self.path == '/redirect' else 404 if self.path == '/missing' else 500 if self.path == '/server-error' else 200)
            if self.path == '/redirect': self.send_header('Location','/notice')
            self.send_header('Content-Type','application/octet-stream' if self.path == '/download' else 'text/html; charset=utf-8')
            if self.path == '/download': self.send_header('Content-Disposition','attachment; filename="fixture.bin"')
            self.end_headers()
            with contextlib.suppress(BrokenPipeError,ConnectionResetError):
                self.wfile.write((b'<script>setTimeout(()=>location.href="/script",1500)</script>' if self.path == '/script-navigation' else b'') + b'<!doctype html><title>Owned announcement fixture</title><h1>Loopback only</h1><a href="#section">anchor</a><h2 id="section">Section</h2>')
        def log_message(self,*args): pass
    environment = dict(env,GDK_BACKEND='wayland')
    for key in ('XDG_CONFIG_HOME','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_STATE_HOME'):
        path = owned / key.lower(); path.mkdir(); environment[key] = str(path)
    # Register a fake URI handler only inside this fixture's XDG directories.
    # Native GTK external-link dispatch must not open the user's real browser.
    handler = owned / 'browser'
    handler.write_text('#!/usr/bin/python3\nimport json,sys\nopen('+repr(str(owned/'external-requested'))+',"w").write(json.dumps(sys.argv[1:]))\n')
    handler.chmod(0o700)
    applications = Path(environment['XDG_DATA_HOME']) / 'applications'
    applications.mkdir()
    desktop = applications / 'sidravia-fixture-browser.desktop'
    desktop.write_text('[Desktop Entry]\nType=Application\nName=Sidravia fixture browser\nExec='+str(handler)+' %u\nMimeType=x-scheme-handler/http;x-scheme-handler/https;\nNoDisplay=true\n')
    subprocess.run(['/usr/bin/python3','-c',
        'from gi.repository import Gio; import sys; app=Gio.DesktopAppInfo.new_from_filename(sys.argv[1]); assert app; assert app.set_as_default_for_type("x-scheme-handler/http"); assert app.set_as_default_for_type("x-scheme-handler/https"); assert Gio.AppInfo.get_default_for_uri_scheme("http").get_id()=="sidravia-fixture-browser.desktop"',str(desktop)],env=environment,check=True)
    server = http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
    worker = threading.Thread(target=server.serve_forever,daemon=True); worker.start()
    process = None
    try:
        with (owned/'native.log').open('w') as log:
            process = subprocess.Popen([str(build/'announcement_window_integration'),f'http://127.0.0.1:{server.server_port}',str(root/'gui/linux/runner/resources/sidravia.png'),str(owned/'external-requested')],cwd=root,env=environment,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
            code = process.wait(timeout=100)
            if code: raise RuntimeError(f'native announcement fixture failed ({code}): '+str(owned/'native.log'))
        after=metadata()
        if any(before[key] != after[key] for key in ('commit','status','source_fingerprint')):
            raise RuntimeError('source changed during announcement acceptance')
        receipt={'passed':True,'source':before,'source_unchanged':True,'nativeLog':str(owned/'native.log'),'realSchoolRequests':False,'realAccountIntegration':False}
        (owned/'result.json').write_text(json.dumps(receipt,indent=2)+'\n')
        print('Announcement native fixture PASS: '+str(owned/'result.json'))
        return 0
    finally:
        if process is not None:
            with contextlib.suppress(ProcessLookupError): os.killpg(process.pid,signal.SIGTERM)
            try: process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                with contextlib.suppress(ProcessLookupError): os.killpg(process.pid,signal.SIGKILL)
                process.wait(timeout=5)
        server.shutdown();worker.join(timeout=3);server.server_close()
