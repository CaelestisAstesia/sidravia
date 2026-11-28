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

  test(
    'downloads the confirmed ID, then allows the GUI to exit during apply',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'sidravia-update-',
      );
      final started = File('${directory.path}/apply-started');
      final finish = File('${directory.path}/finish');
      final fixture = File('${directory.path}/program');
      await fixture.writeAsString(r'''#!/bin/sh
set -eu
case "$*" in
  "download --candidate 2:22")
    printf '%s' '{"schemaVersion":1,"ok":true,"code":"ok"}'
    ;;
  "update apply --candidate 2:22 --confirm")
    touch "$DIR/apply-started"
    while ! test -f "$DIR/finish"; do sleep 0.05; done
    printf '%s' '{"schemaVersion":1,"ok":true,"code":"handed_over"}'
    ;;
  *) exit 1 ;;
esac
''');
      final process = UpdateCheckProcess();
      expect((await Process.run('chmod', ['+x', fixture.path])).exitCode, 0);
      final result = process.install(
        executable: fixture.path,
        downloader: fixture.path,
        candidateId: '2:22',
        environment: {'DIR': directory.path, 'PATH': '/usr/bin:/bin'},
      );
      final checked = expectLater(result, completes);
      addTearDown(() async {
        await finish.create();
        await checked;
        await directory.delete(recursive: true);
      });
      final wait = Stopwatch()..start();
      while (!await started.exists()) {
        expect(wait.elapsed, lessThan(const Duration(seconds: 5)));
        await Future<void>.delayed(const Duration(milliseconds: 20));
      }
      await process.close().timeout(const Duration(seconds: 1));
      await finish.create();
      await checked;
    },
    skip: !Platform.isLinux,
  );

  test('a failed download never starts apply', () async {
    final directory = await Directory.systemTemp.createTemp('sidravia-update-');
    addTearDown(() => directory.delete(recursive: true));
    final fixture = File('${directory.path}/program');
    final started = File('${directory.path}/apply-started');
    await fixture.writeAsString(r'''#!/bin/sh
case "$*" in
  "download --candidate 2:22")
    printf '%s' '{"schemaVersion":1,"ok":false,"code":"candidate_changed"}'
    exit 1
    ;;
  *) touch "$DIR/apply-started" ;;
esac
''');
    expect((await Process.run('chmod', ['+x', fixture.path])).exitCode, 0);
    final process = UpdateCheckProcess();
    addTearDown(process.close);
    await expectLater(
      process.install(
        executable: fixture.path,
        downloader: fixture.path,
        candidateId: '2:22',
        environment: {'DIR': directory.path, 'PATH': '/usr/bin:/bin'},
      ),
      throwsA(
        isA<IntegrationFailure>().having(
          (error) => error.message,
          'message',
          '候选版本已变化，请重新检查更新。',
        ),
      ),
    );
    expect(await started.exists(), isFalse);
  }, skip: !Platform.isLinux);

  test(
    'closing during download lets the cache owner finish without applying',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'sidravia-update-',
      );
      final partial = File('${directory.path}/package.partial');
      final cached = File('${directory.path}/package.verified');
      final finish = File('${directory.path}/finish');
      final applied = File('${directory.path}/applied');
      final fixture = File('${directory.path}/program');
      await fixture.writeAsString(r'''#!/bin/sh
set -eu
case "$*" in
  "download --candidate 2:22")
    touch "$DIR/package.partial"
    while ! test -f "$DIR/finish"; do sleep 0.05; done
    mv "$DIR/package.partial" "$DIR/package.verified"
    ;;
  *) touch "$DIR/applied" ;;
esac
''');
      expect((await Process.run('chmod', ['+x', fixture.path])).exitCode, 0);
      final process = UpdateCheckProcess();
      Future<void> waitFor(File file) async {
        final wait = Stopwatch()..start();
        while (!await file.exists()) {
          expect(wait.elapsed, lessThan(const Duration(seconds: 5)));
          await Future<void>.delayed(const Duration(milliseconds: 20));
        }
      }

      final result = expectLater(
        process.install(
          executable: fixture.path,
          downloader: fixture.path,
          candidateId: '2:22',
          environment: {'DIR': directory.path, 'PATH': '/usr/bin:/bin'},
        ),
        throwsA(
          isA<IntegrationFailure>().having(
            (error) => error.message,
            'message',
            '下载更新已取消。',
          ),
        ),
      );
      addTearDown(() async {
        await finish.create();
        if (await partial.exists()) await waitFor(cached);
        await directory.delete(recursive: true);
      });
      await waitFor(partial);
      await process.close().timeout(const Duration(seconds: 2));
      await result;
      await finish.create();
      await waitFor(cached);
      expect(await partial.exists(), isFalse);
      expect(await applied.exists(), isFalse);
    },
    skip: !Platform.isLinux,
  );
}
