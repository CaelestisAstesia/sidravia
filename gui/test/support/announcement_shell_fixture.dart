import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

Future<SchoolAnnouncementController> manualAnnouncements(
  WidgetTester tester,
  GuiController gui,
) async {
  final preferences = GuiPreferences();
  await preferences.initialize();
  final desktop = DesktopPresence.disabled();
  final owner = SchoolAnnouncementController(
    preferences: preferences,
    desktop: desktop,
    window: SchoolAnnouncementWindow.disabled(),
    notifications: SchoolAnnouncementNotifications.disabled(),
    startedAt: DateTime.now(),
  );
  owner.updateCapabilities(gui.capabilities);
  await owner.start();
  addTearDown(() {
    expect(
      owner.available,
      isFalse,
      reason: 'Fixture must close while tester is active',
    );
    owner.dispose();
    preferences.dispose();
    desktop.dispose();
  });
  return owner;
}

/// Flush the zone that owns mock futures before awaiting their completion.
Future<void> pumpCleanup(
  WidgetTester tester,
  Future<void> Function() cleanup,
) async {
  var complete = false;
  Object? failure;
  StackTrace? failureStack;
  final future = cleanup();
  future.then<void>(
    (_) {
      complete = true;
    },
    onError: (Object error, StackTrace stack) {
      failure = error;
      failureStack = stack;
      complete = true;
    },
  );
  for (var i = 0; i < 20 && !complete; i++) {
    await tester.pump();
  }
  expect(
    complete,
    isTrue,
    reason: 'Fixture cleanup must finish within 20 pumps',
  );
  if (failure != null) Error.throwWithStackTrace(failure!, failureStack!);
  await future;
}

Future<void> closeShellOwners(WidgetTester tester) async {
  final shells = find.byType(SidraviaShell);
  if (shells.evaluate().isEmpty) return;
  final shell = tester.widget<SidraviaShell>(shells.first);
  await pumpCleanup(tester, () async {
    await shell.announcements?.close();
    await shell.controller.close();
  });
}
