import 'dart:io';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/gui_settings_storage_io.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

void main() {
  late Directory root;
  setUp(() {
    root = Directory.systemTemp.createTempSync('sidravia-gui-settings-');
  });
  tearDown(() => root.deleteSync(recursive: true));

  testWidgets(
    'new formal application restores disk theme; system still follows brightness',
    (tester) async {
      final path = '${root.path}/gui-settings.json';
      for (final mode in [ThemeMode.dark, ThemeMode.system]) {
        final first = (await tester.runAsync(() async {
          final owner = Appearance(store: FileGuiSettingsStore(path));
          await owner.initialize();
          owner.value = mode;
          await owner.settled;
          return owner;
        }))!;
        final client = OfflineDemoClient();
        final controller = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
        );
        await tester.pumpWidget(
          SidraviaApp(controller: controller, appearance: first),
        );
        await tester.pumpAndSettle();
        await tester.pumpWidget(const SizedBox.shrink());
        client.dispose();
        final second = (await tester.runAsync(() async {
          final owner = Appearance(store: FileGuiSettingsStore(path));
          await owner.initialize();
          return owner;
        }))!;
        expect(second.value, mode);
        final client2 = OfflineDemoClient();
        final controller2 = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client2,
        );
        tester.platformDispatcher.platformBrightnessTestValue =
            Brightness.light;
        await tester.pumpWidget(
          SidraviaApp(controller: controller2, appearance: second),
        );
        await tester.pumpAndSettle();
        if (mode == ThemeMode.system) {
          expect(
            Theme.of(tester.element(find.byType(HomePage))).brightness,
            Brightness.light,
          );
          tester.platformDispatcher.platformBrightnessTestValue =
              Brightness.dark;
          await tester.pumpAndSettle();
          expect(
            Theme.of(tester.element(find.byType(HomePage))).brightness,
            Brightness.dark,
          );
          expect(
            await tester.runAsync(() => FileGuiSettingsStore(path).read()),
            GuiAppearanceMode.system,
          );
        } else {
          expect(
            Theme.of(tester.element(find.byType(HomePage))).brightness,
            Brightness.dark,
          );
        }
        await tester.pumpWidget(const SizedBox.shrink());
        client2.dispose();
        tester.platformDispatcher.clearPlatformBrightnessTestValue();
      }
    },
  );
  test('installed portable namespace mirror Go layouts and ignore working directory', () {
    final exe = '${root.path}/program';
    Directory(exe).createSync();
    final user = '${root.path}/user';
    for (final portable in [false, true]) {
      if (portable) File('$exe/sidravia.portable').writeAsStringSync('');
      for (final ns in ['', 'mock-test', 'a.b_c-9']) {
        final result = resolveWindowsGuiSettings(
          executable: '$exe/Sidravia.exe',
          userConfigRoot: user,
          namespace: ns,
        );
        final prefix = portable
            ? (ns.isEmpty ? '$exe/config' : '$exe/namespaces/$ns')
            : (ns.isEmpty ? '$user/Sidravia' : '$user/Sidravia/namespaces/$ns');
        expect(result.path, '$prefix/gui-settings.json');
        expect(result.mode, portable ? 'portable' : 'installed');
        final previous = Directory.current;
        try {
          Directory.current = root;
          expect(
            resolveWindowsGuiSettings(
              executable: '$exe/Sidravia.exe',
              userConfigRoot: user,
              namespace: ns,
            ).path,
            result.path,
          );
        } finally {
          Directory.current = previous;
        }
      }
    }
  });
  test('drive or filesystem root produces a clean joined path', () {
    final driveRoot = Directory(root.path).uri.resolve('/').toFilePath();
    final result = resolveWindowsGuiSettings(
      executable: '${driveRoot}Sidravia.exe',
      userConfigRoot: driveRoot,
      inspectMarker: (_) => FileSystemEntityType.file,
    );
    expect(
      result.path,
      '${driveRoot}config${Platform.pathSeparator}gui-settings.json',
    );
  });
  test('same Go namespace valid invalid cases', () {
    for (final value in ['', 'isolated-1', 'mock-test', 'a.b_c-9']) {
      expect(validGuiNamespace(value), true);
    }
    for (final value in [
      '   ',
      ' spaced ',
      'a/b',
      r'a\b',
      'c:drive',
      '..',
      '.hidden',
      'hidden.',
      'production',
      'default',
      'PROD',
      'control\nx',
      '中文',
      'x12345678901234567890123456789012345678901234567890123456789012345',
    ]) {
      expect(validGuiNamespace(value), false);
    }
  });
  test('invalid marker and inspection errors never fall back', () {
    final exe = '${root.path}/program';
    Directory(exe).createSync();
    final marker = '$exe/sidravia.portable';
    Directory(marker).createSync();
    expect(
      () => resolveWindowsGuiSettings(
        executable: '$exe/Sidravia.exe',
        userConfigRoot: root.path,
      ),
      throwsFormatException,
    );
    Directory(marker).deleteSync();
    Link(marker).createSync(root.path);
    expect(
      () => resolveWindowsGuiSettings(
        executable: '$exe/Sidravia.exe',
        userConfigRoot: root.path,
      ),
      throwsFormatException,
    );
    Link(marker).deleteSync();
    expect(
      () => resolveWindowsGuiSettings(
        executable: '$exe/Sidravia.exe',
        userConfigRoot: root.path,
        inspectMarker: (_) => throw FileSystemException(
          'denied',
          marker,
          const OSError('denied', 13),
        ),
      ),
      throwsA(isA<FileSystemException>()),
    );
  });
  test(
    'missing default and new file storage restores last selection',
    () async {
      final path = '${root.path}/config/gui-settings.json';
      final store = FileGuiSettingsStore(path);
      expect(await store.read(), isNull);
      await store.save(GuiAppearanceMode.dark);
      expect(await FileGuiSettingsStore(path).read(), GuiAppearanceMode.dark);
      await store.save(GuiAppearanceMode.system);
      expect(await FileGuiSettingsStore(path).read(), GuiAppearanceMode.system);
      expect(
        File(path).readAsStringSync(),
        encodeGuiSettings(GuiAppearanceMode.system),
      );
    },
  );
  test('invalid damaged and future data preserved without overwrite', () async {
    for (final text in [
      'broken',
      '{}',
      '{"schemaVersion":2,"appearanceMode":"dark"}',
      '{"schemaVersion":1,"appearanceMode":false}',
      '{"schemaVersion":1,"appearanceMode":"invalid"}',
      '{"schemaVersion":1,"appearanceMode":"light","autoReconnect":true}',
    ]) {
      final path = '${root.path}/gui-settings.json';
      final file = File(path)..writeAsStringSync(text);
      final store = FileGuiSettingsStore(path);
      await expectLater(store.read(), throwsFormatException);
      await expectLater(store.save(GuiAppearanceMode.light), throwsStateError);
      expect(file.readAsStringSync(), text);
    }
  });
  test(
    'replacement failure preserves old bytes and cleans temporary',
    () async {
      final path = '${root.path}/gui-settings.json';
      final old = encodeGuiSettings(GuiAppearanceMode.light);
      File(path).writeAsStringSync(old);
      final store = FileGuiSettingsStore(
        path,
        replace: (source, destination) async {
          expect(
            await source.readAsString(),
            encodeGuiSettings(GuiAppearanceMode.dark),
          );
          throw const FileSystemException('replacement denied');
        },
      );
      await store.read();
      await expectLater(
        store.save(GuiAppearanceMode.dark),
        throwsA(isA<FileSystemException>()),
      );
      expect(File(path).readAsStringSync(), old);
      expect(root.listSync().length, 1);
    },
  );
  test(
    'unwritable parent preserves installed old data; no fallback directory',
    () async {
      final dir = Directory('${root.path}/readonly')..createSync();
      final path = '${dir.path}/gui-settings.json';
      File(path).writeAsStringSync(encodeGuiSettings(GuiAppearanceMode.light));
      if (!Platform.isWindows) {
        Process.runSync('chmod', ['555', dir.path]);
      }
      final store = FileGuiSettingsStore(path);
      await store.read();
      if (!Platform.isWindows) {
        try {
          await expectLater(
            store.save(GuiAppearanceMode.dark),
            throwsA(isA<FileSystemException>()),
          );
          expect(
            File(path).readAsStringSync(),
            encodeGuiSettings(GuiAppearanceMode.light),
          );
        } finally {
          Process.runSync('chmod', ['755', dir.path]);
        }
      }
    },
  );
}
