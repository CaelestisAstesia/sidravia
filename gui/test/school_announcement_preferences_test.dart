import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/gui_settings_storage_io.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

const stored = GuiSettings(
  appearanceMode: GuiAppearanceMode.dark,
  diagnosticsExpanded: true,
  closeBehavior: GuiCloseBehavior.exit,
  startHidden: true,
  announcementFrequency: GuiAnnouncementFrequency.everyConnection,
  hiddenAnnouncementBehavior: GuiHiddenAnnouncementBehavior.onRestore,
  lastAutomaticAnnouncementDate: '2026-10-09',
);

void main() {
  late Directory root;
  late String path;
  setUp(() {
    root = Directory.systemTemp.createTempSync('sidravia-announcement-prefs-');
    path = '${root.path}/gui-settings.json';
  });
  tearDown(() => root.deleteSync(recursive: true));

  test(
    'v1 v2 v3 restore old values, add defaults and never rewrite on read',
    () async {
      for (final version in [1, 2, 3]) {
        final source = jsonEncode({
          'schemaVersion': version,
          'appearanceMode': 'dark',
          if (version >= 2) 'diagnosticsExpanded': true,
          if (version >= 3) 'closeBehavior': 'exit',
          if (version >= 3) 'startHidden': true,
        });
        File(path).writeAsStringSync(source);
        final owner = Appearance(store: FileGuiSettingsStore(path));
        await owner.settled;
        expect(owner.value, ThemeMode.dark);
        expect(owner.diagnosticsExpanded, version >= 2);
        expect(
          owner.closeBehavior,
          version >= 3 ? GuiCloseBehavior.exit : GuiCloseBehavior.hideToTray,
        );
        expect(owner.startHidden, version >= 3);
        expect(
          owner.announcementFrequency,
          GuiAnnouncementFrequency.dailyFirstConnection,
        );
        expect(
          owner.hiddenAnnouncementBehavior,
          GuiHiddenAnnouncementBehavior.notify,
        );
        expect(owner.lastAutomaticAnnouncementDate, isNull);
        expect(File(path).readAsStringSync(), source);
        owner.dispose();
      }
    },
  );

  test(
    'schema4 adds info level without rewriting and upgrades on save',
    () async {
      final legacy =
          jsonDecode(encodeGuiSettings(stored)) as Map<String, dynamic>
            ..remove('logLevel');
      legacy['schemaVersion'] = 4;
      final source = jsonEncode(legacy);
      File(path).writeAsStringSync(source);
      final store = FileGuiSettingsStore(path);
      final restored = (await store.read())!;
      expect(restored.logLevel, 'info');
      expect(restored.announcementFrequency, stored.announcementFrequency);
      expect(
        restored.hiddenAnnouncementBehavior,
        stored.hiddenAnnouncementBehavior,
      );
      expect(
        restored.lastAutomaticAnnouncementDate,
        stored.lastAutomaticAnnouncementDate,
      );
      expect(File(path).readAsStringSync(), source);
      await store.save(restored);
      final upgraded =
          jsonDecode(File(path).readAsStringSync()) as Map<String, dynamic>;
      expect(upgraded['schemaVersion'], 5);
      expect(upgraded['logLevel'], 'info');
    },
  );

  test('schema5 exact fields roundtrip announcements and log level, canonical dates', () async {
    for (final frequency in GuiAnnouncementFrequency.values) {
      for (final hidden in GuiHiddenAnnouncementBehavior.values) {
        for (final date in [
          null,
          '2000-02-29',
          '2024-02-29',
          '0001-01-01',
          '9999-12-31',
        ]) {
          final store = FileGuiSettingsStore(path);
          await store.read();
          await store.save(
            GuiSettings(
              appearanceMode: stored.appearanceMode,
              logLevel: 'debug',
              diagnosticsExpanded: stored.diagnosticsExpanded,
              closeBehavior: stored.closeBehavior,
              startHidden: stored.startHidden,
              announcementFrequency: frequency,
              hiddenAnnouncementBehavior: hidden,
              lastAutomaticAnnouncementDate: date,
            ),
          );
          final raw =
              jsonDecode(File(path).readAsStringSync()) as Map<String, dynamic>;
          expect(raw.keys.toSet(), {
            'schemaVersion',
            'logLevel',
            'appearanceMode',
            'diagnosticsExpanded',
            'closeBehavior',
            'startHidden',
            'announcementFrequency',
            'hiddenAnnouncementBehavior',
            'lastAutomaticAnnouncementDate',
          });
          expect(raw['schemaVersion'], 5);
          expect(raw['logLevel'], 'debug');
          final restored = (await FileGuiSettingsStore(path).read())!;
          expect(restored.logLevel, 'debug');
          expect(restored.announcementFrequency, frequency);
          expect(restored.hiddenAnnouncementBehavior, hidden);
          expect(restored.lastAutomaticAnnouncementDate, date);
          expect(restored.appearanceMode, GuiAppearanceMode.dark);
          expect(restored.diagnosticsExpanded, true);
          expect(restored.closeBehavior, GuiCloseBehavior.exit);
          expect(restored.startHidden, true);
        }
      }
    }
  });

  test(
    'invalid dates enum types fields and future versions preserve damaged file',
    () async {
      final base =
          jsonDecode(encodeGuiSettings(stored)) as Map<String, dynamic>;
      final invalid = <Map<String, dynamic>>[
        {...base, 'schemaVersion': 6},
        for (final level in ['unknown', null, true, 1])
          {...base, 'logLevel': level},
        {...base, 'announcementFrequency': 'unknown'},
        {...base, 'announcementFrequency': true},
        {...base, 'hiddenAnnouncementBehavior': 'unknown'},
        {...base, 'hiddenAnnouncementBehavior': null},
        {...base, 'other': false},
        for (final key in base.keys) {...base}..remove(key),
        for (final date in [
          '',
          '2026-2-09',
          '2026-02-9',
          '2026-02-30',
          '1900-02-29',
          '2100-02-29',
          '0000-01-01',
          '2026-00-01',
          '2026-13-01',
          '2026-01-00',
          '2026-04-31',
          '2026-10-10\n',
          '2026-10-10T00:00:00Z',
          ' 2026-10-10',
          20261010,
          true,
          [],
          {},
        ])
          {...base, 'lastAutomaticAnnouncementDate': date},
      ];
      for (final raw in invalid) {
        final source = jsonEncode(raw);
        File(path).writeAsStringSync(source);
        final store = FileGuiSettingsStore(path);
        await expectLater(store.read(), throwsFormatException);
        await expectLater(store.save(stored), throwsStateError);
        expect(File(path).readAsStringSync(), source);
      }
      expect(
        () => encodeGuiSettings(
          const GuiSettings(lastAutomaticAnnouncementDate: '2026-02-30'),
        ),
        throwsFormatException,
      );
    },
  );

  test('fresh owner and separate process preserve all seven values and old helper invocation', () async {
    final owner = Appearance(store: FileGuiSettingsStore(path));
    await owner.initialize();
    owner.value = ThemeMode.dark;
    owner.diagnosticsExpanded = true;
    owner.closeBehavior = GuiCloseBehavior.exit;
    owner.startHidden = true;
    owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
    owner.hiddenAnnouncementBehavior =
        GuiHiddenAnnouncementBehavior.showImmediately;
    owner.recordAutomaticAnnouncement(DateTime(2026, 10, 10, 23, 59));
    await owner.settled;
    owner.dispose();
    final fresh = Appearance(store: FileGuiSettingsStore(path));
    await fresh.initialize();
    expect(fresh.announcementFrequency, GuiAnnouncementFrequency.manualOnly);
    expect(
      fresh.hiddenAnnouncementBehavior,
      GuiHiddenAnnouncementBehavior.showImmediately,
    );
    expect(fresh.lastAutomaticAnnouncementDate, '2026-10-10');
    expect(fresh.value, ThemeMode.dark);
    expect(fresh.diagnosticsExpanded, true);
    expect(fresh.closeBehavior, GuiCloseBehavior.exit);
    expect(fresh.startHidden, true);
    fresh.dispose();
    for (final args in [
      [path],
      [path, 'light'],
      [
        path,
        'dark',
        'true',
        'exit',
        'true',
        'everyConnection',
        'onRestore',
        '2024-02-29',
      ],
      [path],
    ]) {
      final process = await Process.run('dart', [
        'run',
        'test/settings_persistence_process.dart',
        ...args,
      ]);
      expect(process.exitCode, 0, reason: '${process.stderr}');
      final result =
          jsonDecode(process.stdout as String) as Map<String, dynamic>;
      final extended =
          args.length > 5 ||
          result['initialAnnouncementFrequency'] == 'everyConnection';
      expect(
        result['restoredAnnouncementFrequency'],
        extended ? 'everyConnection' : 'manualOnly',
      );
      expect(
        result['restoredHiddenAnnouncementBehavior'],
        extended ? 'onRestore' : 'showImmediately',
      );
      expect(
        result['restoredLastAutomaticAnnouncementDate'],
        extended ? '2024-02-29' : '2026-10-10',
      );
      expect(result['restoredDiagnosticsExpanded'], true);
      expect(result['restoredCloseBehavior'], 'exit');
      expect(result['restoredStartHidden'], true);
    }
  });

  for (final edited in ['frequency', 'hidden', 'date']) {
    test(
      'early $edited survives late read; untouched six fields hydrate',
      () async {
        File(path).writeAsStringSync(encodeGuiSettings(stored));
        final store = DelayedFileStore(FileGuiSettingsStore(path));
        final owner = Appearance(store: store);
        switch (edited) {
          case 'frequency':
            owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
          case 'hidden':
            owner.hiddenAnnouncementBehavior =
                GuiHiddenAnnouncementBehavior.showImmediately;
          case 'date':
            owner.recordAutomaticAnnouncement(DateTime(2026, 10, 10));
        }
        store.readGate.complete();
        await owner.settled;
        expect(owner.value, ThemeMode.dark);
        expect(owner.diagnosticsExpanded, true);
        expect(owner.closeBehavior, GuiCloseBehavior.exit);
        expect(owner.startHidden, true);
        expect(
          owner.announcementFrequency,
          edited == 'frequency'
              ? GuiAnnouncementFrequency.manualOnly
              : stored.announcementFrequency,
        );
        expect(
          owner.hiddenAnnouncementBehavior,
          edited == 'hidden'
              ? GuiHiddenAnnouncementBehavior.showImmediately
              : stored.hiddenAnnouncementBehavior,
        );
        expect(
          owner.lastAutomaticAnnouncementDate,
          edited == 'date'
              ? '2026-10-10'
              : stored.lastAutomaticAnnouncementDate,
        );
        final restored = (await FileGuiSettingsStore(path).read())!;
        expect(restored.announcementFrequency, owner.announcementFrequency);
        expect(
          restored.hiddenAnnouncementBehavior,
          owner.hiddenAnnouncementBehavior,
        );
        expect(
          restored.lastAutomaticAnnouncementDate,
          owner.lastAutomaticAnnouncementDate,
        );
        owner.dispose();
      },
    );
  }

  test(
    'interleaved old new preferences serialize while old write is pending',
    () async {
      File(path).writeAsStringSync(encodeGuiSettings(stored));
      final store = DelayedFileStore(
        FileGuiSettingsStore(path),
        pauseFirstWrite: true,
      );
      store.readGate.complete();
      final owner = Appearance(store: store);
      await owner.initialize();
      owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
      await store.writeStarted.future;
      owner.value = ThemeMode.light;
      owner.hiddenAnnouncementBehavior =
          GuiHiddenAnnouncementBehavior.showImmediately;
      owner.diagnosticsExpanded = false;
      owner.recordAutomaticAnnouncement(DateTime(2026, 10, 10));
      owner.startHidden = false;
      owner.closeBehavior = GuiCloseBehavior.hideToTray;
      final writesBeforeRepeat = store.saved.length;
      owner.recordAutomaticAnnouncement(DateTime(2026, 10, 10, 23));
      expect(store.saved.length, writesBeforeRepeat);
      store.writeGate.complete();
      await owner.settled;
      expect(store.maximumActive, 1);
      expect(store.saved.length, 7);
      final restored = (await FileGuiSettingsStore(path).read())!;
      expect(restored.appearanceMode, GuiAppearanceMode.light);
      expect(restored.diagnosticsExpanded, false);
      expect(restored.closeBehavior, GuiCloseBehavior.hideToTray);
      expect(restored.startHidden, false);
      expect(
        restored.announcementFrequency,
        GuiAnnouncementFrequency.manualOnly,
      );
      expect(
        restored.hiddenAnnouncementBehavior,
        GuiHiddenAnnouncementBehavior.showImmediately,
      );
      expect(restored.lastAutomaticAnnouncementDate, '2026-10-10');
      expect(owner.feedback, isNull);
      owner.dispose();
    },
  );

  test('UTC trigger uses local civil date and choices never clear marker', () async {
    final owner = Appearance(store: FileGuiSettingsStore(path));
    await owner.initialize();
    final at = DateTime.utc(2026, 10, 10, 23, 30);
    final local = at.toLocal();
    final expected =
        '${local.year}-${local.month.toString().padLeft(2, '0')}-${local.day.toString().padLeft(2, '0')}';
    owner.recordAutomaticAnnouncement(at);
    owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
    owner.hiddenAnnouncementBehavior = GuiHiddenAnnouncementBehavior.onRestore;
    await owner.settled;
    expect(owner.lastAutomaticAnnouncementDate, expected);
    expect(
      (await FileGuiSettingsStore(path).read())!.lastAutomaticAnnouncementDate,
      expected,
    );
    owner.dispose();
  });

  test(
    'read failure preserves bytes and choices apply without overwrite',
    () async {
      const damaged = '{"schemaVersion":99}';
      File(path).writeAsStringSync(damaged);
      final owner = Appearance(store: FileGuiSettingsStore(path));
      await owner.initialize();
      owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
      owner.hiddenAnnouncementBehavior =
          GuiHiddenAnnouncementBehavior.onRestore;
      owner.recordAutomaticAnnouncement(DateTime(2026, 10, 10));
      await owner.settled;
      expect(owner.announcementFrequency, GuiAnnouncementFrequency.manualOnly);
      expect(
        owner.hiddenAnnouncementBehavior,
        GuiHiddenAnnouncementBehavior.onRestore,
      );
      expect(owner.lastAutomaticAnnouncementDate, '2026-10-10');
      expect(owner.feedback, contains('原文件保留'));
      expect(File(path).readAsStringSync(), damaged);
      owner.dispose();
    },
  );

  test(
    'write failure preserves disk and reports current choices only in memory',
    () async {
      final oldBytes = encodeGuiSettings(stored);
      File(path).writeAsStringSync(oldBytes);
      final store = FileGuiSettingsStore(
        path,
        replace: (_, _) async => throw const FileSystemException('denied'),
      );
      final owner = Appearance(store: store);
      await owner.initialize();
      owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
      owner.hiddenAnnouncementBehavior =
          GuiHiddenAnnouncementBehavior.showImmediately;
      owner.recordAutomaticAnnouncement(DateTime(2026, 10, 10));
      await owner.settled;
      expect(owner.announcementFrequency, GuiAnnouncementFrequency.manualOnly);
      expect(
        owner.hiddenAnnouncementBehavior,
        GuiHiddenAnnouncementBehavior.showImmediately,
      );
      expect(owner.lastAutomaticAnnouncementDate, '2026-10-10');
      expect(owner.feedback, contains('未能保存'));
      expect(File(path).readAsStringSync(), oldBytes);
      expect(root.listSync(), hasLength(1));
      owner.dispose();
    },
  );

  test('disposal suppresses late load/write listener notifications', () async {
    File(path).writeAsStringSync(encodeGuiSettings(stored));
    final store = DelayedFileStore(FileGuiSettingsStore(path));
    final owner = Appearance(store: store);
    var notifications = 0;
    owner.addListener(() => notifications++);
    owner.announcementFrequency = GuiAnnouncementFrequency.manualOnly;
    final before = notifications;
    owner.dispose();
    store.readGate.complete();
    await owner.settled;
    expect(notifications, before);
    expect(
      store.saved.single.announcementFrequency,
      GuiAnnouncementFrequency.manualOnly,
    );
  });
}

class DelayedFileStore implements GuiSettingsStore {
  DelayedFileStore(this.delegate, {this.pauseFirstWrite = false});
  final FileGuiSettingsStore delegate;
  final bool pauseFirstWrite;
  final readGate = Completer<void>(),
      writeGate = Completer<void>(),
      writeStarted = Completer<void>();
  final saved = <GuiSettings>[];
  int active = 0, maximumActive = 0, writes = 0;
  @override
  bool get persistent => true;
  @override
  Future<GuiSettings?> read() async {
    await readGate.future;
    return delegate.read();
  }

  @override
  Future<void> save(GuiSettings settings) async {
    active++;
    if (active > maximumActive) maximumActive = active;
    writes++;
    if (!writeStarted.isCompleted) writeStarted.complete();
    try {
      if (writes == 1 && pauseFirstWrite) await writeGate.future;
      await delegate.save(settings);
      saved.add(settings);
    } finally {
      active--;
    }
  }
}
