import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';

void main() {
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
      final migrated = decodeGuiSettings(jsonEncode(old));
      expect(migrated.logLevel, 'info');
      expect(migrated.startHidden, isTrue);
      expect(migrated.diagnosticsExpanded, isTrue);
      final store = MemoryGuiSettingsStore()..settings = migrated;
      final preferences = Appearance(store: store);
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
