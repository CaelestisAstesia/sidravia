import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/integration_settings.dart';
import 'package:sidravia_gui/platform/linux_installation_manager.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';

class _Fixture {
  _Fixture({String? productLeaf}) {
    root = Directory('.dart_tool')
        .createTempSync('linux-integration-')
        .absolute;
    home = Directory('${root.path}/home')..createSync();
    product = Directory(
      '${root.path}/${productLeaf ?? 'portable = space "quote" %name\\file'}',
    )..createSync();
    environment = {
      'HOME': home.path,
      'XDG_CONFIG_HOME': '${root.path}/config',
      'XDG_DATA_HOME': '${root.path}/data',
      'XDG_CACHE_HOME': '${root.path}/cache',
      'XDG_STATE_HOME': '${root.path}/state',
      'SIDRAVIA_NAMESPACE': 'fixture-a',
      'PATH': '${home.path}/.local/bin:/usr/bin:/bin',
    };
    for (final name in commands) {
      final file = File('${product.path}/$name');
      file.writeAsStringSync('''#!/usr/bin/python3
import os,json,sys
open(${jsonEncode('${root.path}/observed')},"w").write(json.dumps([os.environ.get("SIDRAVIA_NAMESPACE"),sys.argv]))
''');
      final result = Process.runSync('chmod', ['700', file.path]);
      if (result.exitCode != 0) throw StateError('fixture chmod failed');
    }
    addTearDown(() => root.deleteSync(recursive: true));
  }
  static const commands = [
    'sidraviactl',
    'sidraviad',
    'sidraviaupdater',
    'sidravia_gui',
  ];
  late final Directory root, home, product;
  late final Map<String, String> environment;
  String get executable => '${product.path}/sidravia_gui';
  LinuxInstallationManager get manager => LinuxInstallationManager(
    executable: executable,
    environment: environment,
  );
  File get desktop =>
      Directory('${root.path}/config/autostart')
          .listSync()
          .whereType<File>()
          .singleWhere((e) => e.path.endsWith('.desktop'));
  Future<List<dynamic>> launchEntry() async {
    final observed = File('${root.path}/observed');
    if (observed.existsSync()) observed.deleteSync();
    final launch = await Process.run('/usr/bin/python3', [
      '-c',
      'from gi.repository import Gio; import sys; app=Gio.DesktopAppInfo.new_from_filename(sys.argv[1]); assert app; assert app.launch([],None)',
      desktop.path,
    ], environment: environment);
    expect(launch.exitCode, 0, reason: '${launch.stdout}${launch.stderr}');
    for (var i = 0; i < 100 && !observed.existsSync(); i++) {
      await Future<void>.delayed(const Duration(milliseconds: 20));
    }
    return jsonDecode(observed.readAsStringSync()) as List<dynamic>;
  }
}

void main() {
  test('RPM checker uses the bundled CTL and closes a pending query', () async {
    final f = _Fixture();
    final manager = f.manager;
    final marker = File('${f.product.path}/sidravia.rpm');
    expect(manager.supportsUpdateCheck, isFalse);
    marker.writeAsStringSync('sidravia-rpm-v1\n');
    expect(manager.supportsUpdateCheck, isTrue);
    final ctl = File('${f.product.path}/sidraviactl');
    ctl.writeAsStringSync('''#!/usr/bin/python3
import sys
assert sys.argv[1:] == ['update', 'check']
print('{"schemaVersion":1,"ok":true,"code":"ok","result":{"check":{"state":"available","candidate":{"version":"0.6.0-alpha.1"}}}}')
''');
    expect(await manager.checkUpdate(), (
      state: UpdateCheckState.available,
      version: '0.6.0-alpha.1',
    ));

    ctl.writeAsStringSync('''#!/usr/bin/python3
import time
time.sleep(120)
''');
    final pending = expectLater(
      manager.checkUpdate(),
      throwsA(isA<IntegrationFailure>()),
    );
    await manager.close();
    await pending;
  }, skip: !Platform.isLinux);

  test(
    'RPM commands are package managed while autostart remains explicit',
    () async {
      final f = _Fixture();
      File('${f.product.path}/sidravia.rpm')
          .writeAsStringSync('sidravia-rpm-v1\n');
      final manager = f.manager;
      final status = await manager.status();
      expect(status.userPathSupported, isFalse);
      expect(status.pathEnabled, isTrue);
      expect(status.pathNotice, contains('RPM'));
      for (final action in [
        IntegrationChange.pathEnable,
        IntegrationChange.pathDisable,
      ]) {
        await expectLater(
          manager.change(action),
          throwsA(isA<IntegrationFailure>()),
        );
      }
      expect(Directory('${f.home.path}/.local/bin').existsSync(), isFalse);
      await manager.change(IntegrationChange.startDaemon);
      expect((await manager.status()).autostart, 'daemon');
      expect(f.desktop.readAsStringSync(), contains('TryExec='));
      await f.launchEntry();
      final ctl = File('${f.product.path}/sidraviactl');
      final backup = ctl.renameSync('${ctl.path}.missing');
      final missing = await Process.run('/usr/bin/python3', [
        '-c',
        'from gi.repository import Gio\nimport sys\ntry:\n app=Gio.DesktopAppInfo.new_from_filename(sys.argv[1])\nexcept TypeError:\n pass\nelse:\n assert app is None',
        f.desktop.path,
      ]);
      expect(missing.exitCode, 0, reason: '${missing.stdout}${missing.stderr}');
      await expectLater(
        manager.change(IntegrationChange.startDisable),
        throwsA(
          isA<IntegrationFailure>().having(
            (error) => error.message,
            'message',
            contains('重试关闭'),
          ),
        ),
      );
      expect((await manager.status()).autostart, 'disabled');
      backup.renameSync(ctl.path);
      await manager.change(IntegrationChange.startDisable);
      await manager.close();
    },
    skip: !Platform.isLinux,
  );
  test(
    'session GUI/daemon switching is isolated, idempotent and reversible',
    () async {
      final f = _Fixture();
      final m = f.manager;
      expect((await m.status()).autostart, 'disabled');
      expect((await m.status()).daemonAutostartSupported, isTrue);
      expect((await m.status()).userPathSupported, isTrue);
      await m.change(IntegrationChange.startGui);
      final gui = f.desktop.readAsStringSync();
      await m.change(IntegrationChange.startGui);
      expect(f.desktop.readAsStringSync(), gui);
      expect((await m.status()).autostart, 'gui');
      final validation = await Process.run('desktop-file-validate', [
        f.desktop.path,
      ]);
      expect(
        validation.exitCode,
        0,
        reason: '${validation.stdout}${validation.stderr}',
      );
      expect(await f.launchEntry(), [
        'fixture-a',
        ['./sidravia_gui', '--start-hidden'],
      ]);
      await m.change(IntegrationChange.startDaemon);
      expect((await m.status()).autostart, 'daemon');
      expect(await f.launchEntry(), [
        'fixture-a',
        ['./sidraviactl', 'daemon', 'start', '--log-level', 'info'],
      ]);
      await m.change(IntegrationChange.startGui);
      expect(f.desktop.readAsStringSync(), gui);
      await m.change(IntegrationChange.startDisable);
      expect((await m.status()).autostart, 'disabled');
      await m.close();
      await expectLater(m.status(), throwsA(isA<IntegrationFailure>()));
      await expectLater(
        m.change(IntegrationChange.startDaemon),
        throwsA(isA<IntegrationFailure>()),
      );
    },
    skip: !Platform.isLinux,
  );

  test('autostart refuses foreign files, links and missing targets', () async {
    final f = _Fixture();
    final m = f.manager;
    await m.change(IntegrationChange.startGui);
    final entry = f.desktop;
    entry.writeAsStringSync('foreign bytes');
    expect((await m.status()).autostart, 'conflict');
    for (final change in [
      IntegrationChange.startGui,
      IntegrationChange.startDaemon,
      IntegrationChange.startDisable,
    ]) {
      await expectLater(m.change(change), throwsA(isA<IntegrationFailure>()));
      expect(entry.readAsStringSync(), 'foreign bytes');
    }
    entry.deleteSync();
    Link(entry.path).createSync('${f.root.path}/unrelated');
    await expectLater(
      m.change(IntegrationChange.startDisable),
      throwsA(isA<IntegrationFailure>()),
    );
    Link(entry.path).deleteSync();
    File('${f.product.path}/sidraviactl').deleteSync();
    expect((await m.status()).daemonAutostartSupported, isFalse);
    await expectLater(
      m.change(IntegrationChange.startDaemon),
      throwsA(isA<IntegrationFailure>()),
    );
    expect(entry.existsSync(), isFalse);
    await m.close();
  }, skip: !Platform.isLinux);

  test('PATH publishes exact links; shells execute them and removal preserves unrelated files', () async {
    final f = _Fixture();
    final m = f.manager;
    expect((await m.status()).pathEnabled, isFalse);
    await m.change(IntegrationChange.pathEnable);
    await m.change(IntegrationChange.pathEnable);
    expect((await m.status()).pathEnabled, isTrue);
    for (final name in _Fixture.commands) {
      expect(
        Link('${f.home.path}/.local/bin/$name').targetSync(),
        '${f.product.path}/$name',
      );
    }
    final shell = await Process.run('/bin/bash', [
      '--noprofile',
      '--norc',
      '-c',
      'command -v sidraviactl && sidraviactl --help',
    ], environment: f.environment);
    expect(shell.exitCode, 0, reason: '${shell.stdout}${shell.stderr}');
    expect(shell.stdout, contains('${f.home.path}/.local/bin/sidraviactl'));
    final observed =
        jsonDecode(File('${f.root.path}/observed').readAsStringSync()) as List;
    expect(observed[0], 'fixture-a');
    expect((observed[1] as List).last, '--help');
    final unrelated = File('${f.home.path}/.local/bin/unrelated')
      ..writeAsStringSync('preserve');
    await m.change(IntegrationChange.pathDisable);
    await m.change(IntegrationChange.pathDisable);
    expect((await m.status()).pathEnabled, isFalse);
    expect(unrelated.readAsStringSync(), 'preserve');
    for (final name in _Fixture.commands) {
      expect(
        FileSystemEntity.typeSync(
          '${f.home.path}/.local/bin/$name',
          followLinks: false,
        ),
        FileSystemEntityType.notFound,
      );
    }
    await m.close();
  }, skip: !Platform.isLinux);

  test(
    'PATH conflicts cause no takeover and partially owned links can be cleaned',
    () async {
      final f = _Fixture();
      final m = f.manager;
      final bin = Directory('${f.home.path}/.local/bin')
        ..createSync(recursive: true);
      final foreign = File('${bin.path}/sidraviactl')
        ..writeAsStringSync('foreign');
      await expectLater(
        m.change(IntegrationChange.pathEnable),
        throwsA(isA<IntegrationFailure>()),
      );
      expect(bin.listSync().whereType<Link>(), isEmpty);
      expect(foreign.readAsStringSync(), 'foreign');
      foreign.deleteSync();
      await m.change(IntegrationChange.pathEnable);
      final replaced = Link('${bin.path}/sidraviad');
      replaced.deleteSync();
      replaced.createSync('/foreign/daemon');
      expect((await m.status()).pathNotice, contains('冲突'));
      await m.change(IntegrationChange.pathDisable);
      expect(replaced.targetSync(), '/foreign/daemon');
      expect((await m.status()).pathEnabled, isFalse);
      await m.close();
    },
    skip: !Platform.isLinux,
  );

  test(
    'PATH absent or shadowed is explicit; autostart and PATH are independent',
    () async {
      final f = _Fixture();
      f.environment['PATH'] = '/usr/bin:/bin';
      final m = f.manager;
      await m.change(IntegrationChange.pathEnable);
      expect((await m.status()).pathEnabled, isTrue);
      expect((await m.status()).pathNotice, contains('不在当前 PATH'));
      // A fresh Fedora bash loads the public distribution skeleton; no real
      // account profile is read or changed.
      for (final name in ['.bash_profile', '.bashrc']) {
        File('/etc/skel/$name').copySync('${f.home.path}/$name');
      }
      final shell = await Process.run('/bin/bash', [
        '--rcfile',
        '${f.home.path}/.bashrc',
        '-ic',
        'command -v sidraviactl',
      ], environment: f.environment);
      expect(shell.exitCode, 0, reason: '${shell.stdout}${shell.stderr}');
      expect(shell.stdout, contains('${f.home.path}/.local/bin/sidraviactl'));
      final shadow = Directory('${f.root.path}/shadow')..createSync();
      final shadowCommand = File('${f.product.path}/sidraviactl')
          .copySync('${shadow.path}/sidraviactl');
      expect(
        (await Process.run('chmod', ['700', shadowCommand.path])).exitCode,
        0,
      );
      f.environment['PATH'] =
          '${shadow.path}:${f.home.path}/.local/bin/:/usr/bin:/bin';
      expect((await m.status()).pathNotice, contains('前方有同名程序'));
      await m.change(IntegrationChange.startDaemon);
      await m.change(IntegrationChange.pathDisable);
      expect((await m.status()).autostart, 'daemon');
      await m.change(IntegrationChange.startDisable);
      await m.close();
    },
    skip: !Platform.isLinux,
  );

  test('legacy GUI entry migrates only when its exact definition matches', () async {
    final f = _Fixture(productLeaf: 'simple');
    final m = f.manager;
    await m.change(IntegrationChange.startGui);
    final entry = f.desktop;
    final header = entry.readAsStringSync().split('Exec=').first;
    entry.writeAsStringSync(
      '${header}Exec="/usr/bin/env" "SIDRAVIA_NAMESPACE=fixture-a" "${f.executable}" --start-hidden\nX-Sidravia-Owner=linux-gui-v1\n',
    );
    expect((await m.status()).autostart, 'gui');
    await m.change(IntegrationChange.startDaemon);
    expect((await m.status()).autostart, 'daemon');
    expect(entry.readAsStringSync(), contains('linux-desktop-v2'));
    await m.change(IntegrationChange.startDisable);
    await m.close();
  }, skip: !Platform.isLinux);

  test('relative XDG falls back to private HOME and namespace entries stay separate', () async {
    final f = _Fixture();
    f.environment['XDG_CONFIG_HOME'] = 'relative-xdg';
    final m = f.manager;
    await m.change(IntegrationChange.startDaemon);
    final another = LinuxInstallationManager(
      executable: f.executable,
      environment: {...f.environment, 'SIDRAVIA_NAMESPACE': 'fixture-b'},
    );
    await another.change(IntegrationChange.startGui);
    final entries = Directory('${f.home.path}/.config/autostart')
        .listSync()
        .whereType<File>()
        .where((e) => e.path.endsWith('.desktop'))
        .toList();
    expect(entries, hasLength(2));
    expect((await m.status()).autostart, 'daemon');
    expect((await another.status()).autostart, 'gui');
    await m.change(IntegrationChange.startDisable);
    expect((await another.status()).autostart, 'gui');
    await another.change(IntegrationChange.startDisable);
    await m.close();
    await another.close();
  }, skip: !Platform.isLinux);

  test('mutable integration directories refuse symlink redirection', () async {
    final f = _Fixture();
    final m = f.manager;
    final foreign = Directory('${f.root.path}/foreign')..createSync();
    Directory('${f.home.path}/.local').createSync();
    Link('${f.home.path}/.local/bin').createSync(foreign.path);
    await expectLater(
      m.change(IntegrationChange.pathEnable),
      throwsA(isA<IntegrationFailure>()),
    );
    expect(foreign.listSync(), isEmpty);
    expect(Link('${f.home.path}/.local/bin').targetSync(), foreign.path);
    await m.close();
  }, skip: !Platform.isLinux);

  test(
    'another process holding the maintenance lock causes a bounded refusal',
    () async {
      final f = _Fixture();
      final m = f.manager;
      await m.change(IntegrationChange.startGui);
      final directory = Directory('${f.root.path}/config/autostart');
      final oldLock = File('${directory.path}/.sidravia-integration.lock');
      expect(oldLock.existsSync(), isFalse);
      final child = await Process.start('/usr/bin/flock', [
        '--exclusive',
        directory.path,
        '/usr/bin/sh',
        '-c',
        'echo locked; cat >/dev/null',
      ]);
      try {
        expect(
          await child.stdout
              .transform(utf8.decoder)
              .transform(const LineSplitter())
              .first
              .timeout(const Duration(seconds: 5)),
          'locked',
        );
        await expectLater(
          m.change(IntegrationChange.startDaemon),
          throwsA(isA<IntegrationFailure>()),
        );
        expect((await m.status()).autostart, 'gui');
      } finally {
        await child.stdin.close();
        expect(await child.exitCode.timeout(const Duration(seconds: 5)), 0);
      }
      await m.change(IntegrationChange.startDisable);
      expect(directory.listSync(), isEmpty);
      await m.close();
    },
    skip: !Platform.isLinux,
  );

  testWidgets(
    'Linux integration controls enable background autostart and explain PATH registration',
    (tester) async {
      final f = _Fixture();
      final m = f.manager;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: IntegrationSettings(manager: m)),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('~/.local/bin'), findsOneWidget);
      expect(find.textContaining('仅后台模式没有托盘'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
    skip: !Platform.isLinux,
  );

  test(
    'runtime layout keeps public commands and nested autostart icon',
    () async {
      final f = _Fixture(productLeaf: 'runtime layout');
      final runtime = Directory('${f.product.path}/gui-runtime')..createSync();
      File('${runtime.path}/sidravia_gui').writeAsStringSync('runtime');
      final manager = LinuxInstallationManager(
        executable: '${runtime.path}/sidravia_gui',
        environment: f.environment,
      );
      await manager.change(IntegrationChange.pathEnable);
      expect(
        Link('${f.home.path}/.local/bin/sidravia_gui').targetSync(),
        f.executable,
      );
      await manager.change(IntegrationChange.startGui);
      expect(
        f.desktop.readAsStringSync(),
        contains('Icon=${runtime.path}/data/icons/sidravia.png'),
      );
      expect(
        f.desktop.readAsStringSync(),
        isNot(contains('--chdir=${runtime.path}')),
      );
      await manager.close();
    },
    skip: !Platform.isLinux,
  );
}
