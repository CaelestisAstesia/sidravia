import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/update_check_process.dart';

void main() {
  test('cancels when a descendant still holds the output pipes', () async {
    final directory = await Directory.systemTemp.createTemp('sidravia-update-');
    final ready = File('${directory.path}/pipe-owner');
    final fixture = File('${directory.path}/ctl');
    await fixture.writeAsString(r'''#!/bin/sh
sleep 15 &
printf '%s\n' "$!" > "$SIDRAVIA_TEST_READY"
exit 0
''');
    final process = UpdateCheckProcess();
    addTearDown(() async {
      if (await ready.exists()) {
        final pid = int.tryParse((await ready.readAsString()).trim());
        if (pid != null) Process.killPid(pid);
      }
      await process.close();
      await directory.delete(recursive: true);
    });
    expect((await Process.run('chmod', ['+x', fixture.path])).exitCode, 0);

    final result = expectLater(
      process.run(
        fixture.path,
        environment: {
          'SIDRAVIA_TEST_READY': ready.path,
          'PATH': Platform.environment['PATH'] ?? '/usr/bin:/bin',
        },
      ),
      throwsA(
        isA<IntegrationFailure>().having(
          (failure) => failure.message,
          'message',
          '检查更新已取消。',
        ),
      ),
    );
    final startup = Stopwatch()..start();
    while (!await ready.exists() || (await ready.readAsString()).isEmpty) {
      expect(startup.elapsed, lessThan(const Duration(seconds: 5)));
      await Future<void>.delayed(const Duration(milliseconds: 20));
    }

    await process.cancel().timeout(const Duration(seconds: 3));
    await result;
  }, skip: !Platform.isLinux);
}
