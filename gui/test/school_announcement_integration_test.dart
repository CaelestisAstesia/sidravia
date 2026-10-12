import 'school_announcement_controller_test.dart' as school_dispatch;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final frequency in [
    GuiAnnouncementFrequency.manualOnly,
    GuiAnnouncementFrequency.everyConnection,
  ]) {
    testWidgets(
      'actual App projections dispatch school entry and connection $frequency',
      (tester) async {
        const windowChannel = MethodChannel(schoolAnnouncementChannelName);
        const desktopChannel = MethodChannel(desktopPresenceChannelName);
        final messenger =
            TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
        final effects = <MethodCall>[];
        messenger.setMockMethodCallHandler(windowChannel, (call) async {
          effects.add(call);
          return call.method == 'open' ? 'opened' : null;
        });
        messenger.setMockMethodCallHandler(
          desktopChannel,
          (call) async => switch (call.method) {
            'initialize' => 'primary',
            'getVisibility' => true,
            _ => null,
          },
        );
        final preferences = GuiPreferences();
        await preferences.initialize();
        preferences.announcementFrequency = frequency;
        final desktop = DesktopPresence(channel: desktopChannel);
        await desktop.initialize();
        final startedAt = DateTime.now();
        final client = OfflineDemoClient()..selectScenario('suspended');
        final gui = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
        );
        final owner = SchoolAnnouncementController(
          preferences: preferences,
          desktop: desktop,
          window: SchoolAnnouncementWindow(
            channel: windowChannel,
            platform: TargetPlatform.windows,
          ),
          notifications: SchoolAnnouncementNotifications.disabled(),
          startedAt: startedAt,
        );
        try {
          await tester.pumpWidget(
            SidraviaApp(
              controller: gui,
              announcements: owner,
              appearance: preferences,
              desktopPresence: desktop,
            ),
          );
          await tester.pumpAndSettle();
          expect(find.text('校园网公告'), findsNothing);
          expect(find.text('吉林大学公告'), findsOneWidget);
          expect(find.text('在应用内打开学校公告页面'), findsNothing);
          expect(effects.where((c) => c.method == 'open'), isEmpty);
          await tester.tap(find.byType(HomeNoticeView));
          await tester.pumpAndSettle();
          expect(effects.where((c) => c.method == 'open'), hasLength(1));
          expect(preferences.lastAutomaticAnnouncementDate, isNull);
          await tester.tap(find.text('开始连接'));
          await tester.pumpAndSettle();
          expect(gui.snapshot!.sessions.single.state.name, 'authenticated');
          expect(
            effects.where((c) => c.method == 'open'),
            hasLength(
              frequency == GuiAnnouncementFrequency.everyConnection ? 2 : 1,
            ),
          );
          final payload = effects.first.arguments as Map;
          expect(payload, {
            'url': 'http://login.jlu.edu.cn/notice_win.php',
            'clientWidth': 600,
            'clientHeight': 460,
            'namespace': '',
          });
        } finally {
          await tester.pump();
          await school_dispatch.pumpCleanup(tester, () async {
            await owner.close();
            await gui.close();
            await preferences.settled;
          });
          await tester.pumpWidget(const SizedBox.shrink());
          await tester.pumpAndSettle();
          client.dispose();
          messenger.setMockMethodCallHandler(windowChannel, null);
          messenger.setMockMethodCallHandler(desktopChannel, null);
        }
        expect(effects.where((c) => c.method == 'close'), hasLength(1));
        expect(await owner.openManually(), SchoolAnnouncementOpenResult.failed);
      },
    );
  }
}
