import 'dart:io';

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
}
