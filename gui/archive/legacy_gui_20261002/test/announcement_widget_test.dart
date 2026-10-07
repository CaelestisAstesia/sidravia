import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

final _endpoint = Uri.parse('https://notices.example.edu/feed.json');

Map<String, Object?> _item(
  String id,
  String level, {
  String title = '公告标题',
  String body = '公告正文。',
  String? actionLabel,
  String? actionUrl,
}) {
  final now = DateTime.now().toUtc();
  return {
    'id': id,
    'revision': 1,
    'level': level,
    'title': title,
    'body': body,
    'publishedAt': now.subtract(const Duration(hours: 3)).toIso8601String(),
    'startsAt': now.subtract(const Duration(hours: 2)).toIso8601String(),
    'expiresAt': now.add(const Duration(days: 5)).toIso8601String(),
    'actionLabel': ?actionLabel,
    'actionUrl': ?actionUrl,
  };
}

Future<AnnouncementController> _started(
  List<Map<String, Object?>> items,
) async {
  final now = DateTime.now().toUtc();
  final controller = AnnouncementController(
    endpoint: _endpoint,
    store: MemoryAnnouncementStore(
      AnnouncementCache(
        feedJson: jsonEncode({
          'schema': 1,
          'generatedAt': now.toIso8601String(),
          'items': items,
        }),
        lastSuccessUtc: now,
      ),
    ),
  );
  await controller.start();
  return controller;
}

Future<GuiController> _gui() async {
  final controller = createPreviewController(PreviewScenario.failed);
  await controller.start();
  return controller;
}

Widget _shellApp(GuiController gui, AnnouncementController? announcements) =>
    MaterialApp(
      theme: SidraviaTheme.light(),
      home: SidraviaShell(controller: gui, announcements: announcements),
    );

void main() {
  testWidgets('wide layout shows a footer entry with unread semantics', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final announcements = await _started([
      _item('m1', 'maintenance', title: '校园网维护通知'),
    ]);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();

    final entry = find.byKey(const ValueKey('announcement-entry-wide'));
    expect(entry, findsOneWidget);
    expect(
      find.byKey(const ValueKey('announcement-entry-compact')),
      findsNothing,
    );
    expect(tester.getSemantics(entry).label, contains('1 条未读'));
    expect(
      tester
          .widget<Badge>(
            find.descendant(of: entry, matching: find.byType(Badge)),
          )
          .isLabelVisible,
      isTrue,
    );
    final sidebar = find.byKey(const ValueKey<String>('wide-sidebar'));
    expect(
      tester.getRect(entry).bottom,
      lessThanOrEqualTo(tester.getRect(sidebar).bottom),
    );
  });

  testWidgets(
    'compact layout shows a header entry and keeps the four sections',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(390, 844));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final gui = await _gui();
      final announcements = await _started([_item('m1', 'maintenance')]);
      addTearDown(announcements.dispose);

      await tester.pumpWidget(_shellApp(gui, announcements));
      await tester.pump();

      final entry = find.byKey(const ValueKey('announcement-entry-compact'));
      expect(entry, findsOneWidget);
      expect(
        find.byKey(const ValueKey('announcement-entry-wide')),
        findsNothing,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey<String>('compact-header')),
          matching: entry,
        ),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('compact-navigation')), findsOneWidget);
    },
  );

  testWidgets('a disabled controller hides every announcement surface', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final disabled = AnnouncementController.disabled();
    addTearDown(disabled.dispose);

    await tester.pumpWidget(_shellApp(gui, disabled));
    await tester.pump();
    expect(find.byKey(const ValueKey('announcement-entry-wide')), findsNothing);
    expect(
      find.byKey(const ValueKey('announcement-inline-notice')),
      findsNothing,
    );

    await tester.pumpWidget(_shellApp(gui, null));
    await tester.pump();
    expect(find.byKey(const ValueKey('announcement-entry-wide')), findsNothing);
  });

  testWidgets('wide sheet lists active items and marks them read', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final announcements = await _started([
      _item('m1', 'maintenance', title: '校园网维护通知', body: '维护窗口说明。'),
    ]);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('announcement-entry-wide')));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('announcement-sheet')), findsOneWidget);
    expect(find.text('校园网维护通知'), findsOneWidget);
    expect(find.text('维护窗口说明。'), findsOneWidget);
    expect(announcements.unreadCount, 0);

    await tester.tap(find.byTooltip('关闭'));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('announcement-sheet')), findsNothing);
  });

  testWidgets('an empty feed shows the empty sheet copy', (tester) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final announcements = await _started(const []);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('announcement-entry-wide')));
    await tester.pumpAndSettle();

    expect(find.text('暂无公告'), findsOneWidget);
  });

  testWidgets('compact layout opens a scrollable sheet', (tester) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final announcements = await _started([
      _item('i1', 'info', title: '第一条公告'),
      _item('i2', 'info', title: '第二条公告'),
      _item('i3', 'info', title: '第三条公告'),
    ]);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('announcement-entry-compact')));
    await tester.pumpAndSettle();

    final sheet = find.byKey(const ValueKey('announcement-sheet'));
    expect(sheet, findsOneWidget);
    expect(tester.widget(sheet), isA<ListView>());
    expect(find.text('第一条公告'), findsOneWidget);
    expect(announcements.unreadCount, 0);
  });

  testWidgets('inline notice follows the connection status and dismisses', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final announcements = await _started([
      _item('m1', 'maintenance', title: '校园网维护通知'),
    ]);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();

    final status = find.byKey(const ValueKey('connection-status-surface'));
    final inline = find.byKey(const ValueKey('announcement-inline-notice'));
    expect(status, findsOneWidget);
    expect(inline, findsOneWidget);
    expect(
      tester.getBottomLeft(inline).dy,
      lessThan(tester.getTopLeft(status).dy),
    );

    await tester.tap(
      find.descendant(of: inline, matching: find.byTooltip('关闭')),
    );
    await tester.pump();
    expect(inline, findsNothing);

    await tester.tap(find.byKey(const ValueKey('announcement-entry-wide')));
    await tester.pumpAndSettle();
    expect(find.text('校园网维护通知'), findsOneWidget);
  });

  testWidgets('info announcements never reach the home surface', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final gui = await _gui();
    final announcements = await _started([_item('i1', 'info')]);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();

    expect(
      find.byKey(const ValueKey('announcement-inline-notice')),
      findsNothing,
    );
    final entry = find.byKey(const ValueKey('announcement-entry-wide'));
    expect(
      tester
          .widget<Badge>(
            find.descendant(of: entry, matching: find.byType(Badge)),
          )
          .isLabelVisible,
      isTrue,
    );
  });

  testWidgets('the action opener receives only the validated URL', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final announcements = await _started([
      _item(
        'm1',
        'maintenance',
        actionLabel: '查看详情',
        actionUrl: 'https://notices.example.edu/detail',
      ),
    ]);
    addTearDown(announcements.dispose);
    final opened = <Uri>[];

    await tester.pumpWidget(
      MaterialApp(
        theme: SidraviaTheme.light(),
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => showAnnouncementSheet(
                context,
                controller: announcements,
                wide: true,
                linkOpener: (url) async {
                  opened.add(url);
                  return false;
                },
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('查看详情'));
    await tester.pumpAndSettle();

    expect(opened.single, Uri.parse('https://notices.example.edu/detail'));
    expect(find.text('无法打开链接'), findsOneWidget);
  });

  testWidgets('a successful action open shows no failure copy', (tester) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final announcements = await _started([
      _item(
        'm1',
        'maintenance',
        actionLabel: '查看详情',
        actionUrl: 'https://notices.example.edu/detail',
      ),
    ]);
    addTearDown(announcements.dispose);

    await tester.pumpWidget(
      MaterialApp(
        theme: SidraviaTheme.light(),
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => showAnnouncementSheet(
                context,
                controller: announcements,
                wide: true,
                linkOpener: (url) async => true,
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('查看详情'));
    await tester.pumpAndSettle();

    expect(find.text('无法打开链接'), findsNothing);
  });

  testWidgets('announcement surfaces render across responsive sizes', (
    tester,
  ) async {
    final gui = await _gui();
    final announcements = await _started([
      _item('m1', 'maintenance', title: '校园网维护通知'),
    ]);
    addTearDown(announcements.dispose);

    for (final size in [const Size(1280, 720), const Size(390, 844)]) {
      await tester.binding.setSurfaceSize(size);
      await tester.pumpWidget(_shellApp(gui, announcements));
      await tester.pump();
      expect(tester.takeException(), isNull, reason: '$size');
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }

    await tester.binding.setSurfaceSize(const Size(320, 568));
    tester.binding.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(() {
      tester.binding.platformDispatcher.clearTextScaleFactorTestValue();
      tester.binding.setSurfaceSize(null);
    });
    await tester.pumpWidget(_shellApp(gui, announcements));
    await tester.pump();
    expect(tester.takeException(), isNull);

    final entry = find.byKey(const ValueKey('announcement-entry-compact'));
    expect(entry, findsOneWidget);
    final entryRect = tester.getRect(entry);
    expect(entryRect.left, greaterThanOrEqualTo(0));
    expect(entryRect.right, lessThanOrEqualTo(320));

    final inline = find.byKey(const ValueKey('announcement-inline-notice'));
    expect(inline, findsOneWidget);
    await tester.ensureVisible(inline);
    await tester.pump();
    expect(tester.takeException(), isNull);
    final inlineRect = tester.getRect(inline);
    expect(inlineRect.left, greaterThanOrEqualTo(0));
    expect(inlineRect.right, lessThanOrEqualTo(320));
  });
}
