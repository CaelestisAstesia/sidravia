import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final scale in [1.0, 2.0]) {
    testWidgets(
      'fixed feedback slot queues and dismisses hints without shifting sections at $scale',
      (tester) async {
        await tester.binding.setSurfaceSize(const Size(960, 1500));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        var dismissed = 0;
        Widget surface(String? error, String? announcement) => MaterialApp(
          theme: AppTheme.light(),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context)
                .copyWith(textScaler: TextScaler.linear(scale)),
            child: child!,
          ),
          home: Scaffold(
            body: HomeView(
              data: HomeViewData(
                institution: '吉林大学',
                username: 'student01',
                state: '已连接',
                detail: '认证成功',
                context: '以太网',
                tone: HomeTone.success,
                secondaryLabel: '连接与诊断',
                primaryLabel: '断开连接',
                primaryKind: HomeButtonKind.secondary,
                primaryEnabled: true,
                onHeader: () {},
                onSettings: () {},
                onSecondary: () {},
                onPrimary: () {},
                actionError: error,
                onDismissActionError: () => dismissed++,
                announcement: HomeAnnouncementEntry(
                  title: '吉林大学公告',
                  onOpen: () {},
                  feedback: announcement,
                ),
                serverMessages: const Text('服务器消息'),
              ),
            ),
          ),
        );
        List<Rect> anchors() => [
          tester.getRect(find.byKey(const ValueKey('home-divider'))),
          tester.getRect(find.byKey(const ValueKey('home-notice'))),
          tester.getRect(find.byKey(const ValueKey('home-messages-divider'))),
          tester.getRect(find.text('服务器消息')),
        ];
        await tester.pumpWidget(surface(null, null));
        await tester.pumpAndSettle();
        final initial = anchors();
        final slot = tester.getRect(
          find.byKey(const ValueKey('home-feedback-slot')),
        );
        await tester.pumpWidget(surface('连接操作失败', '公告暂未显示，可在首页查看'));
        await tester.pumpAndSettle();
        expect(anchors(), initial);
        expect(find.text('连接操作失败'), findsOneWidget);
        expect(find.text('公告暂未显示，可在首页查看'), findsNothing);
        await tester.tap(find.byTooltip('关闭提示'));
        await tester.pump();
        expect(dismissed, 1);
        final announcement = find.text('公告暂未显示，可在首页查看');
        expect(announcement, findsOneWidget);
        expect(find.text('连接操作失败'), findsNothing);
        expect(anchors(), initial);
        final text = tester.widget<Text>(announcement);
        expect(
          text.style!.color,
          Theme.of(tester.element(announcement)).colorScheme.onSurfaceVariant,
        );
        await tester.tap(find.byTooltip('关闭提示'));
        await tester.pump();
        expect(announcement, findsNothing);
        expect(anchors(), initial);
        expect(
          tester.getRect(find.byKey(const ValueKey('home-feedback-slot'))),
          slot,
        );
        await tester.pumpWidget(
          surface(List.filled(80, '较长提示内容').join('，'), null),
        );
        await tester.pumpAndSettle();
        expect(anchors(), initial);
        final scroll = tester.state<ScrollableState>(
          find.descendant(
            of: find.byKey(const ValueKey('home-feedback-slot')),
            matching: find.byType(Scrollable),
          ),
        );
        expect(scroll.position.maxScrollExtent, greaterThan(0));
        expect(tester.takeException(), isNull);
      },
    );
  }
}
