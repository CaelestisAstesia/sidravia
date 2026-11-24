import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';

void main() {
  test(
    'late startup read never overwrites user selection; saves serialize',
    () async {
      final store = ControlledStore();
      final appearance = GuiPreferences(store: store);
      appearance.value = ThemeMode.dark;
      appearance.value = ThemeMode.light;
      appearance.value = ThemeMode.system;
      store.readResult.complete(
        const GuiSettings(
          appearanceMode: GuiAppearanceMode.light,
          diagnosticsExpanded: true,
        ),
      );
      await appearance.settled;
      expect(appearance.value, ThemeMode.system);
      expect(
        store.saved
            .map(
              (settings) =>
                  (settings.appearanceMode, settings.diagnosticsExpanded),
            )
            .toList(),
        [
          (GuiAppearanceMode.dark, true),
          (GuiAppearanceMode.light, true),
          (GuiAppearanceMode.system, true),
        ],
      );
      expect(appearance.diagnosticsExpanded, true);
      expect(store.maximumActive, 1);
      appearance.dispose();
    },
  );

  test(
    'late read hydrates untouched appearance and preserves diagnostic choice',
    () async {
      final store = ControlledStore();
      final appearance = GuiPreferences(store: store);
      appearance.diagnosticsExpanded = true;
      store.readResult.complete(
        const GuiSettings(appearanceMode: GuiAppearanceMode.dark),
      );
      await appearance.settled;
      expect(appearance.value, ThemeMode.dark);
      expect(appearance.diagnosticsExpanded, true);
      expect(
        store.saved
            .map(
              (settings) =>
                  (settings.appearanceMode, settings.diagnosticsExpanded),
            )
            .toList(),
        [(GuiAppearanceMode.dark, true)],
      );
      appearance.dispose();
    },
  );

  for (final field in ['theme', 'fold', 'close', 'hidden']) {
    test(
      'late load preserves edited $field and hydrates other three',
      () async {
        final store = ControlledStore();
        final owner = GuiPreferences(store: store);
        switch (field) {
          case 'theme':
            owner.value = ThemeMode.light;
          case 'fold':
            owner.diagnosticsExpanded = false;
          case 'close':
            owner.closeBehavior = GuiCloseBehavior.hideToTray;
          case 'hidden':
            owner.startHidden = false;
        }
        store.readResult.complete(
          const GuiSettings(
            appearanceMode: GuiAppearanceMode.dark,
            diagnosticsExpanded: true,
            closeBehavior: GuiCloseBehavior.exit,
            startHidden: true,
            announcementFrequency: GuiAnnouncementFrequency.manualOnly,
            hiddenAnnouncementBehavior: GuiHiddenAnnouncementBehavior.onRestore,
            lastAutomaticAnnouncementDate: '2026-10-09',
          ),
        );
        await owner.settled;
        expect(
          owner.value,
          field == 'theme' ? ThemeMode.light : ThemeMode.dark,
        );
        expect(owner.diagnosticsExpanded, field != 'fold');
        expect(
          owner.closeBehavior,
          field == 'close'
              ? GuiCloseBehavior.hideToTray
              : GuiCloseBehavior.exit,
        );
        expect(owner.startHidden, field != 'hidden');
        final saved = store.saved.single;
        expect(saved.appearanceMode.name, owner.value.name);
        expect(saved.diagnosticsExpanded, owner.diagnosticsExpanded);
        expect(saved.closeBehavior, owner.closeBehavior);
        expect(saved.startHidden, owner.startHidden);
        owner.value = ThemeMode.system;
        await owner.settled;
        expect(store.saved.last.closeBehavior, saved.closeBehavior);
        expect(store.saved.last.startHidden, saved.startHidden);
        owner.diagnosticsExpanded = !owner.diagnosticsExpanded;
        await owner.settled;
        expect(store.saved.last.appearanceMode, GuiAppearanceMode.system);
        expect(store.saved.last.closeBehavior, saved.closeBehavior);
        expect(store.saved.last.startHidden, saved.startHidden);
        owner.closeBehavior = GuiCloseBehavior.exit;
        await owner.settled;
        expect(store.saved.last.startHidden, saved.startHidden);
        expect(store.saved.last.diagnosticsExpanded, owner.diagnosticsExpanded);
        owner.startHidden = true;
        await owner.settled;
        expect(store.saved.last.closeBehavior, GuiCloseBehavior.exit);
        expect(store.saved.last.appearanceMode, GuiAppearanceMode.system);
        expect(store.saved.last.diagnosticsExpanded, owner.diagnosticsExpanded);
        expect(store.maximumActive, 1);
        expect(
          owner.announcementFrequency,
          GuiAnnouncementFrequency.manualOnly,
        );
        expect(
          owner.hiddenAnnouncementBehavior,
          GuiHiddenAnnouncementBehavior.onRestore,
        );
        expect(owner.lastAutomaticAnnouncementDate, '2026-10-09');
        expect(
          store.saved.last.announcementFrequency,
          GuiAnnouncementFrequency.manualOnly,
        );
        expect(
          store.saved.last.hiddenAnnouncementBehavior,
          GuiHiddenAnnouncementBehavior.onRestore,
        );
        expect(store.saved.last.lastAutomaticAnnouncementDate, '2026-10-09');
        owner.dispose();
      },
    );
  }

  test('each preference mutation preserves the other field', () async {
    final store = ControlledStore()
      ..readResult.complete(
        const GuiSettings(
          appearanceMode: GuiAppearanceMode.dark,
          diagnosticsExpanded: true,
        ),
      );
    final appearance = GuiPreferences(store: store);
    await appearance.initialize();
    appearance.value = ThemeMode.light;
    await appearance.settled;
    expect(store.saved.last.appearanceMode, GuiAppearanceMode.light);
    expect(store.saved.last.diagnosticsExpanded, true);
    appearance.diagnosticsExpanded = false;
    await appearance.settled;
    expect(store.saved.last.appearanceMode, GuiAppearanceMode.light);
    expect(store.saved.last.diagnosticsExpanded, false);
    appearance.dispose();
  });
  test('failed load blocks overwrite but user theme applies', () async {
    final store = ControlledStore();
    final appearance = GuiPreferences(store: store);
    final loading = appearance.initialize();
    store.readResult.completeError(const FormatException());
    await loading;
    appearance.value = ThemeMode.dark;
    appearance.diagnosticsExpanded = true;
    await appearance.settled;
    expect(appearance.value, ThemeMode.dark);
    expect(appearance.diagnosticsExpanded, true);
    appearance.closeBehavior = GuiCloseBehavior.exit;
    appearance.startHidden = true;
    await appearance.settled;
    expect(appearance.closeBehavior, GuiCloseBehavior.exit);
    expect(appearance.startHidden, true);
    expect(store.saved, isEmpty);
    expect(appearance.feedback, contains('原文件保留'));
    appearance.dispose();
  });
  test(
    'save failure does not report success or revert current theme',
    () async {
      final store = ControlledStore()..failSave = true;
      store.readResult.complete(null);
      final appearance = GuiPreferences(store: store);
      await appearance.initialize();
      appearance.value = ThemeMode.dark;
      await appearance.settled;
      expect(appearance.value, ThemeMode.dark);
      appearance.diagnosticsExpanded = true;
      await appearance.settled;
      expect(appearance.diagnosticsExpanded, true);
      appearance.closeBehavior = GuiCloseBehavior.exit;
      appearance.startHidden = true;
      await appearance.settled;
      expect(appearance.closeBehavior, GuiCloseBehavior.exit);
      expect(appearance.startHidden, true);
      expect(appearance.feedback, contains('未能保存'));
      appearance.dispose();
    },
  );
}

class ControlledStore implements GuiSettingsStore {
  final readResult = Completer<GuiSettings?>();
  final saved = <GuiSettings>[];
  int active = 0, maximumActive = 0;
  bool failSave = false;
  @override
  bool get persistent => true;
  @override
  Future<GuiSettings?> read() => readResult.future;
  @override
  Future<void> save(GuiSettings settings) async {
    active++;
    if (active > maximumActive) maximumActive = active;
    await Future<void>.delayed(Duration.zero);
    active--;
    if (failSave) throw StateError('fixture');
    saved.add(settings);
  }
}
