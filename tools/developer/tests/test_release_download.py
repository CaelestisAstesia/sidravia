from __future__ import annotations

import base64
import contextlib
import hashlib
import http.server
import io
import json
import os
from pathlib import Path
import shutil
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import dev
import release_download as download
import release_artifacts as release

ROOT = Path(__file__).resolve().parents[3]


@unittest.skipUnless(shutil.which('openssl') and shutil.which('go'), 'HTTPS/signature fixture requires installed OpenSSL and Go')
class PublicDownload(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.folder = Path(cls.temp.name)
        cls.cert, cls.key = cls.folder/'cert.pem', cls.folder/'tls-key.pem'
        subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1',
                        '-subj','/CN=localhost','-addext','subjectAltName=IP:127.0.0.1,DNS:localhost',
                        '-keyout',str(cls.key),'-out',str(cls.cert)],check=True,capture_output=True)
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                status, headers, body = cls.routes.get(self.path, (404, {}, b''))
                time.sleep(cls.delays.get(self.path,0))
                self.send_response(status)
                for key,value in headers.items(): self.send_header(key,value)
                self.end_headers()
                with contextlib.suppress(BrokenPipeError,ConnectionResetError,ssl.SSLError):
                    self.wfile.write(body)
            def log_message(self,*args): pass
        cls.server = http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cls.cert,cls.key)
        cls.server.socket = context.wrap_socket(cls.server.socket,server_side=True)
        cls.thread = threading.Thread(target=cls.server.serve_forever,daemon=True)
        cls.thread.start()
        def close():
            cls.server.shutdown();cls.server.server_close();cls.thread.join(timeout=3)
        cls.addClassCleanup(close)
        cls.base = 'https://127.0.0.1:' + str(cls.server.server_port)

    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.root = Path(self.work.name)
        # Public RFC 8032 test seed; this is never a production signing key.
        seed = bytes.fromhex('9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60')
        private = self.root/'fixture.der'
        private.write_bytes(bytes.fromhex('302e020100300506032b657004220420') + seed)
        public = bytes.fromhex('d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a')
        self.public = self.root/'public.txt';self.public.write_bytes(base64.b64encode(public))
        self.name = 'sidravia-v1.0.0-linux-amd64-core-portable.tar.gz'
        payload = b'opaque archive bytes: never extract or execute'
        manifest = {'schemaVersion':2, 'keyId':hashlib.sha256(public).hexdigest(), 'version':'1.0.0', 'buildId':'a'*40,
                    'minimumProtocol':1, 'assets':[{'os':'linux','arch':'amd64','kind':'core-portable',
                    'name':self.name,'size':len(payload),'sha256':hashlib.sha256(payload).hexdigest(),
                    'filesSha256':'b'*64,'payloadKind':'core'}]}
        self.manifest = self.root/'manifest.json';self.manifest.write_text(json.dumps(manifest))
        signature = self.root/'signature.bin'
        subprocess.run(['openssl','pkeyutl','-sign','-rawin','-inkey',str(private),'-keyform','DER',
                        '-in',str(self.manifest),'-out',str(signature)],check=True,capture_output=True)
        self.data = {self.name:payload,'sidravia-release.json':self.manifest.read_bytes(), 'sidravia-release.sig':signature.read_bytes(), 'extra.rpm':b'RPM hash-only fixture'}
        type(self).routes = {'/'+name:(200, {'Content-Length':str(len(body))},body) for name,body in self.data.items()}
        type(self).delays = {}
        self.request = self.root/'request.json'
        self.inventory = {'schemaVersion':1,'version':'1.0.0','manifest':'sidravia-release.json','signature':'sidravia-release.sig',
                          'assets':[{'name':name,'url':self.base+'/'+name,'size':len(body),'sha256':hashlib.sha256(body).hexdigest()}
                                    for name,body in self.data.items()]}
        self.save()
        self.environment = mock.patch.dict(os.environ, {'SSL_CERT_FILE':str(self.cert)})
        self.environment.start();self.addCleanup(self.environment.stop)

    def save(self):
        self.request.write_text(json.dumps(self.inventory))

    def consume(self):
        out = self.root/'output';out.mkdir()
        code = download.consume(ROOT,'go',self.request,self.public,out)
        return code,json.loads((out/'download-result.json').read_text()),out

    def test_real_https_and_real_signature_with_exact_scope(self):
        code,result,out = self.consume()
        self.assertEqual(code,0,result)
        self.assertEqual(result['signature'],'passed')
        self.assertEqual(result['signedAssets'],[self.name])
        self.assertEqual(result['hashOnlyAssets'],['extra.rpm'])
        self.assertEqual(result['verifiedMetadata'],['sidravia-release.json','sidravia-release.sig'])
        self.assertEqual((out/'files'/self.name).read_bytes(),self.data[self.name])
        self.assertEqual(result['nativeUpgrade'],'not_run')

    def use_published_metadata_names(self, manifest, signature):
        for field, name in (('manifest', manifest), ('signature', signature)):
            previous = self.inventory[field]
            asset = next(asset for asset in self.inventory['assets'] if asset['name'] == previous)
            type(self).routes['/' + name] = type(self).routes['/' + previous]
            asset.update(name=name, url=self.base + '/' + name)
            self.inventory[field] = name
        self.save()

    def test_rejects_two_unrecognized_published_metadata_names(self):
        self.use_published_metadata_names('manifest.json', 'signature.bin')
        code, result, _ = self.consume()
        self.assertEqual(code, 1, result)
        self.assertEqual(result['verifiedMetadata'], [])

    def test_rejects_valid_signature_at_wrong_published_location(self):
        self.use_published_metadata_names('sidravia-release-extended.json', 'sidravia-release-extended.sig')
        code, result, _ = self.consume()
        self.assertEqual(code, 1, result)
        self.assertEqual(result['signature'], 'failed')
        self.assertEqual(result['verifiedMetadata'], [])

    def test_rejects_metadata_from_different_published_pairs(self):
        self.use_published_metadata_names('sidravia-release.json', 'sidravia-release-xz.sig')
        code, result, _ = self.consume()
        self.assertEqual(code, 1, result)
        self.assertEqual(result['verifiedMetadata'], [])

    def test_truncated_and_hash_mismatched_downloads_preserve_partial_evidence(self):
        type(self).routes['/'+self.name]=(200,{},b'short')
        code,result,out = self.consume()
        self.assertEqual(code,1)
        self.assertEqual(result['error'],'public_asset_size_or_hash_mismatch')
        self.assertEqual(result['assets'][0]['status'],'failed')
        self.assertTrue((out/'partial/0').exists())
        self.assertFalse((out/'files'/self.name).exists())

    def test_extra_bytes_are_rejected_with_no_unbounded_write(self):
        type(self).routes['/'+self.name]=(200,{},self.data[self.name]+b'oversize')
        code,result,out = self.consume()
        self.assertEqual(code,1)
        self.assertEqual(result['error'],'public_asset_oversize')
        self.assertLessEqual((out/'partial/0').stat().st_size,len(self.data[self.name]))

    def test_https_redirect_is_supported_but_downgrade_and_credentials_rejected(self):
        type(self).routes['/redirect']=(302,{'Location':self.base+'/'+self.name},b'')
        self.inventory['assets'][0]['url']=self.base+'/redirect';self.save()
        code,result,_=self.consume()
        self.assertEqual(code,0,result)
        for address in ('http://127.0.0.1/file','https://user:password@example.com/file','https://example.com/file#fragment'):
            with self.subTest(address=address),self.assertRaises(ValueError):download.url(address,redirect=True)

    def test_actual_redirect_downgrade_fails_without_leaking_location(self):
        type(self).routes['/'+self.name]=(302,{'Location':'http://127.0.0.1/private?secret=never-report'},b'')
        code,result,_=self.consume()
        self.assertEqual(code,1)
        self.assertNotIn('never-report',json.dumps(result))
        self.assertEqual(result['error'],'public_https_url_required')

    def test_bad_signature_is_rejected_even_when_inventory_hash_matches(self):
        body=bytes(64)
        type(self).routes['/sidravia-release.sig']=(200,{'Content-Length':'64'},body)
        self.inventory['assets'][2]['sha256']=hashlib.sha256(body).hexdigest();self.save()
        code,result,_=self.consume()
        self.assertEqual(code,1)
        self.assertEqual(result['signature'],'failed')
        self.assertEqual(result['error'],'manifest_structure_or_signature_rejected')

    def test_signed_asset_inventory_must_match_after_valid_signature(self):
        body=b'different unsigned archive bytes'
        type(self).routes['/'+self.name]=(200,{'Content-Length':str(len(body))},body)
        self.inventory['assets'][0].update(size=len(body),sha256=hashlib.sha256(body).hexdigest());self.save()
        code,result,_=self.consume()
        self.assertEqual(code,1)
        self.assertEqual(result['error'],'signed_asset_inventory_mismatch')

    def test_final_rehash_catches_local_mutation_after_signature(self):
        original=release.public_check
        def mutate(*args, **kwargs):
            result=original(*args, **kwargs)
            (self.root/'output/files'/self.name).write_bytes(b'changed locally')
            return result
        with mock.patch.object(release,'public_check',side_effect=mutate):
            code,result,_=self.consume()
        self.assertEqual(code,1)
        self.assertEqual(result['error'],'download_changed_after_verification')

    def test_request_rejects_aliases_duplicate_unknown_and_unsafe_paths(self):
        for name in ('../escape','CON','file.',r'path\escape'):
            self.inventory['assets'][0]['name']=name;self.save()
            with self.subTest(name=name),self.assertRaises(ValueError):download.request(self.request)
        self.inventory['assets'][0]['name']=self.name
        self.inventory['assets'].append(dict(self.inventory['assets'][0]));self.save()
        with self.assertRaisesRegex(ValueError,'duplicate'):download.request(self.request)

    def test_runner_entry_writes_one_json_and_invalidates_changed_source(self):
        out=self.root/'run'
        args=dev.parse_args(['release','download','--request',str(self.request),'--public-key',str(self.public),
                             '--output',str(out),'--format','json'])
        output=io.StringIO()
        identity=iter([{'commit':'before'},{'commit':'after'}])
        with contextlib.redirect_stdout(output):
            code=download.run(ROOT,args,lambda:next(identity))
        result=json.loads(output.getvalue())
        self.assertEqual(code,1)
        self.assertFalse(result['source_unchanged'])
        self.assertEqual(json.loads((out/'report.json').read_text())['download']['signature'],'passed')
        with self.assertRaises(FileExistsError):download.run(ROOT,args,lambda:{'commit':'fixture'})

    def test_shared_stage_deadline_bounds_a_blocked_https_request(self):
        type(self).delays['/'+self.name]=3
        out=self.root/'timeout-run'
        args=dev.parse_args(['release','download','--request',str(self.request),'--public-key',str(self.public),
                             '--output',str(out),'--stage-timeout','1','--format','json'])
        output=io.StringIO()
        with contextlib.redirect_stdout(output):
            code=download.run(ROOT,args,lambda:{'commit':'fixture'})
        result=json.loads(output.getvalue())
        self.assertEqual(code,1)
        self.assertEqual(result['stages'][0]['status'],'timed_out')
        self.assertEqual(result['signature'],'not_run')
        self.assertTrue((out/'download-result.json').exists())


if __name__ == '__main__':
    unittest.main()
