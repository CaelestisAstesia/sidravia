import 'dart:io';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/linux_installation_manager.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';

void main() {
  test(
    'session autostart is isolated, reversible, and preserves foreign entries',
    () async {
      final root = Directory.systemTemp.createTempSync('sidravia-autostart-');
      addTearDown(() => root.deleteSync(recursive: true));
      final executable = File('${root.path}/space "quote" %name\\file');
      executable.writeAsStringSync('fixture');
      final manager = LinuxInstallationManager(
        executable: executable.path,
        environment: {
          'XDG_CONFIG_HOME': root.path,
          'SIDRAVIA_NAMESPACE': 'fixture-a',
        },
      );
      expect((await manager.status()).autostart, 'disabled');
      expect((await manager.status()).userPathSupported, isFalse);
      await expectLater(
        manager.change(IntegrationChange.startDaemon),
        throwsA(isA<IntegrationFailure>()),
      );
      await manager.change(IntegrationChange.startGui);
      expect((await manager.status()).autostart, 'gui');
      final entry =
          Directory('${root.path}/autostart').listSync().single as File;
      final validation = await Process.run('desktop-file-validate', [
        entry.path,
      ]);
      expect(
        validation.exitCode,
        0,
        reason: '${validation.stdout}${validation.stderr}',
      );
      final bytes = entry.readAsStringSync();
      expect(bytes, contains('%%name'));
      await manager.change(IntegrationChange.startDisable);
      expect(entry.existsSync(), isFalse);
      entry.writeAsStringSync('foreign bytes');
      expect((await manager.status()).autostart, 'conflict');
      await expectLater(
        manager.change(IntegrationChange.startGui),
        throwsA(isA<IntegrationFailure>()),
      );
      await expectLater(
        manager.change(IntegrationChange.startDisable),
        throwsA(isA<IntegrationFailure>()),
      );
      expect(entry.readAsStringSync(), 'foreign bytes');
      await manager.close();
      await expectLater(manager.status(), throwsA(isA<IntegrationFailure>()));
    },
    skip: !Platform.isLinux,
  );
  test(
    'XDG Exec launches exact special-character path and isolated namespace',
    () async {
      final root = Directory.systemTemp.createTempSync('sidravia-exec-');
      addTearDown(() => root.deleteSync(recursive: true));
      final executable = File('${root.path}/space "quote" %name\\file');
      final result = File('${root.path}/observed');
      executable.writeAsStringSync('''#!/usr/bin/python3
import os,json,sys
open('${result.path}',"w").write(json.dumps([os.environ.get("SIDRAVIA_NAMESPACE"),sys.argv]))
''');
      expect(
        (await Process.run('chmod', ['700', executable.path])).exitCode,
        0,
      );
      final manager = LinuxInstallationManager(
        executable: executable.path,
        environment: {
          'XDG_CONFIG_HOME': root.path,
          'SIDRAVIA_NAMESPACE': 'exec-fixture',
        },
      );
      await manager.change(IntegrationChange.startGui);
      final entry = Directory('${root.path}/autostart').listSync().single;
      final launched = await Process.run('/usr/bin/python3', [
        '-c',
        'from gi.repository import Gio; import sys; app=Gio.DesktopAppInfo.new_from_filename(sys.argv[1]); assert app; assert app.launch([],None)',
        entry.path,
      ]);
      expect(launched.exitCode, 0, reason: '${launched.stderr}');
      for (var i = 0; i < 100 && !result.existsSync(); i++) {
        await Future<void>.delayed(const Duration(milliseconds: 20));
      }
      final observed = jsonDecode(result.readAsStringSync()) as List;
      expect(observed, [
        'exec-fixture',
        [executable.path, '--start-hidden'],
      ]);
      await manager.change(IntegrationChange.startDisable);
      await manager.close();
    },
    skip: !Platform.isLinux,
  );
}
