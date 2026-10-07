import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';

void main() {
  testWidgets('disabled announcements do not add an entry', (tester) async {
    final controller = AnnouncementController.disabled();
    await tester.pumpWidget(
      MaterialApp(
        home: AnnouncementEntry(controller: controller, onOpen: () {}),
      ),
    );
    expect(find.text('校园网公告'), findsNothing);
    controller.dispose();
  });

  for (final outcome in ['success', 'false', 'throw']) {
    testWidgets(
      'announcement action uses external browser and tolerates $outcome',
      (tester) async {
        const endpoint = 'https://notices.example.edu/feed.json';
        const actionUrl =
            'https://notices.example.edu/detail?section=maintenance';
        final item = AnnouncementDocument.decode(
          jsonEncode({
            'schema': 1,
            'generatedAt': '2026-10-07T00:00:00Z',
            'items': [
              {
                'id': 'maintenance-1',
                'revision': 1,
                'level': 'maintenance',
                'title': '校园网维护公告',
                'body': '服务将在今晚进行维护。',
                'publishedAt': '2026-10-07T00:00:00Z',
                'startsAt': '2026-10-07T00:00:00Z',
                'expiresAt': '2026-11-07T00:00:00Z',
                'actionLabel': '查看维护详情',
                'actionUrl': actionUrl,
              },
            ],
          }),
          endpoint: Uri.parse(endpoint),
        ).items.single;
        final calls = <MethodCall>[];
        const channel = MethodChannel('plugins.flutter.io/url_launcher');
        final messenger =
            TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
        messenger.setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          if (outcome == 'throw') {
            throw PlatformException(code: 'launch_failed');
          }
          return outcome == 'success';
        });
        addTearDown(() => messenger.setMockMethodCallHandler(channel, null));

        final semantics = tester.ensureSemantics();
        try {
          await tester.pumpWidget(
            MaterialApp(
              home: Scaffold(body: AnnouncementContent(item: item)),
            ),
          );

          final action = find.byType(TextButton);
          expect(action, findsOneWidget);
          expect(tester.getSemantics(action).flagsCollection.isButton, isTrue);
          await tester.tap(find.text('查看维护详情'));
          await tester.pumpAndSettle();

          expect(calls, hasLength(1));
          expect(calls.single.method, 'launch');
          final arguments = calls.single.arguments as Map<Object?, Object?>;
          expect(arguments['url'], Uri.parse(actionUrl).toString());
          expect(arguments['useWebView'], isFalse);
          expect(arguments['useSafariVC'], isFalse);
          expect(arguments['universalLinksOnly'], isFalse);
          expect(find.text('校园网维护公告'), findsOneWidget);
          expect(find.text('服务将在今晚进行维护。'), findsOneWidget);
          expect(find.text('查看维护详情'), findsOneWidget);
        } finally {
          semantics.dispose();
        }
      },
    );
  }
}
