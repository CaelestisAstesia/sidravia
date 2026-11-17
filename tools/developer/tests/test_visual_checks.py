import contextlib
import copy
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import visual_checks as visual
import validation_run as runner

try:
    from PIL import Image
except ImportError:
    Image = None


@unittest.skipIf(Image is None, 'Pillow is not installed')
class VisualContracts(unittest.TestCase):
    def fixture(self, root, names=('frame-a','frame-b')):
        (root/'images').mkdir(parents=True)
        cases=[]
        for name in names:
            path=root/'images'/(name+'.png')
            Image.new('RGBA',(300,300),(255,255,255,255)).save(path)
            cases.append({'id':name,'file':'images/'+name+'.png','sha256':visual.sha(path),'size':[300,300], 'timezoneOffsetSeconds':0, 'frame':{'id':'frame','width':300,'height':300,'scale':1.0,'dark':False}})
        report={'schema':1,'kind':'visual-capture','status':'passed','source_unchanged':True,'source':{'commit':'a'*40,'source_fingerprint':'b'*64},'cases':cases,'context':{'scenarioVersion':1,'fixedTime':'2026-01-02T12:34:56.000Z','timezone':'UTC','targetPlatform':'linux','renderer':'flutter-widget-dpr1','host':['linux','x86_64'],'flutter':{'frameworkVersion':'fixture','engineRevision':'fixture'},'fonts':{'Roboto':['c'*64]},'diagnosticFont':'d'*64,'license':'e'*64}}
        runner.write_json(root/'report.json',report)
        return report

    def compare(self, base, after, output):
        stdout=io.StringIO()
        with contextlib.redirect_stdout(stdout):
            code=visual.compare(SimpleNamespace(baseline=str(base),candidate=str(after),output=str(output),format='json'))
        summary=json.loads(stdout.getvalue())
        return code,runner.read_json(summary['report'])

    def save_change(self, root, report, name, image):
        path=root/'images'/(name+'.png')
        image.save(path)
        row=next(c for c in report['cases'] if c['id']==name)
        row.update(sha256=visual.sha(path),size=list(image.size))
        runner.write_json(root/'report.json',report)

    def test_opaque_text_color_change_and_alpha_cannot_disappear(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);base=root/'before';after=root/'after'
            self.fixture(base);report=self.fixture(after)
            image=Image.new('RGBA',(300,300),'white');image.putpixel((5,7),(0,0,0,255))
            self.save_change(after,report,'frame-a',image)
            image=Image.new('RGBA',(300,300),'white');image.putpixel((3,4),(255,255,255,254))
            self.save_change(after,report,'frame-b',image)
            code,result=self.compare(base,after,root/'comparison')
            self.assertEqual(code,1)
            self.assertEqual(result['needs_review'],['frame-a','frame-b'])
            self.assertEqual(result['cases'][0]['changed_pixels'],1)
            self.assertEqual(result['cases'][0]['bbox'],[5,7,6,8])
            self.assertEqual(result['cases'][1]['changed_pixels'],1)
            self.assertTrue((root/'comparison/index.html').is_file())

    def test_pixel_identical_png_encoding_metadata_is_filtered(self):
        from PIL.PngImagePlugin import PngInfo
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);base=root/'before';after=root/'after'
            self.fixture(base);report=self.fixture(after)
            path=after/'images/frame-a.png';info=PngInfo();info.add_text('note','different encoder metadata')
            with Image.open(path) as image:image.save(path,pnginfo=info,compress_level=1)
            report['cases'][0]['sha256']=visual.sha(path);runner.write_json(after/'report.json',report)
            code,result=self.compare(base,after,root/'comparison')
            self.assertEqual(code,0)
            self.assertEqual(result['summary']['unchanged'],2)
            self.assertEqual(result['needs_review'],[])
            self.assertEqual(list((root/'comparison/changes').iterdir()),[])

    def test_coverage_and_canvas_size_are_explicit(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);base=root/'before';after=root/'after'
            self.fixture(base,('frame-size','frame-removed'));report=self.fixture(after,('frame-size','frame-added'))
            image=Image.new('RGBA',(301,300),'white');self.save_change(after,report,'frame-size',image)
            code,result=self.compare(base,after,root/'comparison')
            self.assertEqual(code,1)
            self.assertEqual({c['kind'] for c in result['cases']},{'resized','removed','added'})

    def test_font_or_time_mismatch_never_claims_visual_pass(self):
        for key,value in [('fixedTime','2026-01-03T12:34:56.000Z'),('fonts',{'Roboto':['f'*64]}),('flutter',{'engineRevision':'another'})]:
            with self.subTest(key=key),tempfile.TemporaryDirectory() as folder:
                root=Path(folder);base=root/'before';after=root/'after'
                self.fixture(base);report=self.fixture(after);report['context'][key]=value;runner.write_json(after/'report.json',report)
                code,result=self.compare(base,after,root/'comparison')
                self.assertEqual(code,2)
                self.assertEqual(result['status'],'incompatible')
                self.assertEqual(result['incompatible_fields'],[key])
                self.assertEqual(result['cases'],[])

    def test_malformed_tampered_or_failed_receipts_are_rejected(self):
        for change in ('failed','source','case','path','bytes','timezone'):
            with self.subTest(change=change),tempfile.TemporaryDirectory() as folder:
                root=Path(folder);report=self.fixture(root)
                if change=='failed':report['status']='failed'
                if change=='source':report.pop('source')
                if change=='case':report['cases']=[None]
                if change=='path':report['cases'][0]['file']='../elsewhere.png'
                if change=='timezone':report['cases'][0]['timezoneOffsetSeconds']=28800
                if change=='bytes':(root/'images/frame-a.png').write_bytes(b'tampered')
                runner.write_json(root/'report.json',report)
                with self.assertRaises(ValueError):visual.load_capture(root)

    def test_capture_inputs_require_timezone_unique_frames_and_owned_outputs(self):
        with tempfile.TemporaryDirectory() as folder:
            path=Path(folder)/'input.json'
            for data in ({'fixedTime':'2026-01-02T12:34:56'}, {'frames':[visual.DEFAULT_FRAMES[0],visual.DEFAULT_FRAMES[0]]}, {'frames':[dict(visual.DEFAULT_FRAMES[0],width=True)]}):
                runner.write_json(path,data)
                with self.assertRaises(ValueError):visual.capture_input(path)


    def test_rendering_metadata_is_not_treated_as_encoder_noise(self):
        import struct
        from PIL.PngImagePlugin import PngInfo
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);base=root/'before';after=root/'after'
            self.fixture(base);report=self.fixture(after)
            path=after/'images/frame-a.png';info=PngInfo();info.add(b'gAMA',struct.pack('>I',45455))
            with Image.open(path) as image:image.save(path,pnginfo=info)
            report['cases'][0]['sha256']=visual.sha(path);runner.write_json(after/'report.json',report)
            code,result=self.compare(base,after,root/'comparison')
            self.assertEqual(code,1)
            self.assertEqual(result['needs_review'],['frame-a'])
            self.assertEqual(result['cases'][0]['kind'],'metadata_changed')


    def test_latin_only_or_unused_font_cannot_claim_chinese_capture(self):
        font=Path(__file__).resolve().parents[3]/'gui/assets/fonts/SourceCodePro-Regular.otf'
        with self.assertRaisesRegex(ValueError,'lack Chinese'):
            visual.validate_cjk_coverage({'Roboto':[str(font)]},['Roboto'])
        with self.assertRaisesRegex(ValueError,'no explicitly loaded'):
            visual.validate_cjk_coverage({'Misspelled':[str(font)]},['Roboto'])
