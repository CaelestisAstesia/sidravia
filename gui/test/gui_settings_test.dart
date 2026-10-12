import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';

void main() {
  test('fresh preferences use Debug and explicit Info remains saved', () async {
    expect(const GuiSettings().logLevel, 'debug');
    final fresh = GuiPreferences(store: MemoryGuiSettingsStore());
    await fresh.initialize();
    expect(fresh.logLevel, 'debug');
    fresh.dispose();
    final explicit = decodeGuiSettings(
      encodeGuiSettings(const GuiSettings(logLevel: 'info')),
    );
    final store = MemoryGuiSettingsStore()..settings = explicit;
    final restored = GuiPreferences(store: store);
    await restored.initialize();
    expect(restored.logLevel, 'info');
    restored.dispose();
  });
  test(
    'log preference roundtrip and schema4 migration preserve other preferences',
    () async {
      final encoded = encodeGuiSettings(
        const GuiSettings(
          logLevel: 'debug',
          startHidden: true,
          diagnosticsExpanded: true,
        ),
      );
      expect(decodeGuiSettings(encoded).logLevel, 'debug');
      final old = jsonDecode(encoded) as Map<String, dynamic>;
      old['schemaVersion'] = 4;
      old.remove('logLevel');
      old.remove('notificationMode');
      final migrated = decodeGuiSettings(jsonEncode(old));
      expect(migrated.logLevel, 'debug');
      expect(migrated.startHidden, isTrue);
      expect(migrated.diagnosticsExpanded, isTrue);
      final store = MemoryGuiSettingsStore()..settings = migrated;
      final preferences = GuiPreferences(store: store);
      await preferences.initialize();
      preferences.logLevel = 'trace';
      await preferences.settled;
      expect(store.settings!.logLevel, 'trace');
      preferences.dispose();
    },
  );
  test('invalid persisted log level rejected', () {
    final m = jsonDecode(
      encodeGuiSettings(const GuiSettings()),
    ) as Map<String, dynamic>;
    m['logLevel'] = 'verbose';
    expect(() => decodeGuiSettings(jsonEncode(m)), throwsFormatException);
  });
}
