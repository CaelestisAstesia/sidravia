import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/installation/windows_installation_manager.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/update_check_process.dart';

void main() {
  final browserFailure = throwsA(
    isA<IntegrationFailure>().having(
      (failure) => failure.message,
      'message',
      '无法打开浏览器。',
    ),
  );

  test('rejects release links after the installation manager closes', () async {
    final manager = WindowsInstallationManager(UpdateCheckProcess());
    await manager.close();

    await expectLater(manager.openReleasePage('0.6.0'), browserFailure);
  });

  test(
    'reports a missing release-page launcher as a browser failure',
    () async {
      final manager = WindowsInstallationManager(UpdateCheckProcess());
      addTearDown(manager.close);

      await expectLater(manager.openReleasePage('0.6.0'), browserFailure);
    },
    skip: Platform.isWindows || !_windowsLauncherMissing()
        ? 'Runs only where the Windows launcher is unavailable.'
        : false,
  );
}

bool _windowsLauncherMissing() {
  final directories = [
    Directory.current.path,
    ...(Platform.environment['PATH'] ?? '').split(':'),
  ];
  return directories.every(
    (directory) =>
        FileSystemEntity.typeSync(
          '${directory.isEmpty ? '.' : directory}/cmd.exe',
          followLinks: false,
        ) ==
        FileSystemEntityType.notFound,
  );
}
