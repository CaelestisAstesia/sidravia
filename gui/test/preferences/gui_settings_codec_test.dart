import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';

void main() {
  test('old settings migrate to abnormal without losing fields', () {
    for (var schema = 1; schema <= 5; schema++) {
      final fields = <String, Object?>{
        'schemaVersion': schema,
        'appearanceMode': 'dark',
        if (schema >= 2) 'diagnosticsExpanded': true,
        if (schema >= 3) ...{'closeBehavior': 'exit', 'startHidden': true},
        if (schema >= 4) ...{
          'announcementFrequency': 'everyConnection',
          'hiddenAnnouncementBehavior': 'onRestore',
          'lastAutomaticAnnouncementDate': '2026-10-12',
        },
        if (schema == 5) 'logLevel': 'debug',
      };
      final migrated = decodeGuiSettings(jsonEncode(fields));
      expect(
        migrated.notificationMode,
        GuiNotificationMode.abnormal,
        reason: 'schema $schema',
      );
      expect(migrated.appearanceMode, GuiAppearanceMode.dark);
      if (schema >= 4) {
        expect(
          migrated.announcementFrequency,
          GuiAnnouncementFrequency.everyConnection,
        );
        expect(migrated.lastAutomaticAnnouncementDate, '2026-10-12');
      }
      if (schema == 5) expect(migrated.logLevel, 'debug');
    }
  });
  test('schema6 roundtrip accepts only exact typed modes', () {
    for (final mode in GuiNotificationMode.values) {
      final encoded = encodeGuiSettings(GuiSettings(notificationMode: mode));
      expect(jsonDecode(encoded)['schemaVersion'], 6);
      expect(decodeGuiSettings(encoded).notificationMode, mode);
    }
    final base = jsonDecode(
      encodeGuiSettings(const GuiSettings()),
    ) as Map<String, dynamic>;
    for (final invalid in [
      null,
      1,
      true,
      'errors',
      '',
      ['all'],
    ]) {
      expect(
        () => decodeGuiSettings(
          jsonEncode({...base, 'notificationMode': invalid}),
        ),
        throwsFormatException,
      );
    }
  });
}
