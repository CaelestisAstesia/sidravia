import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

void main() {
  testWidgets(
    'announcement feedback stays under notice while connection errors remain at actions',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(960, 900));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: Scaffold(
            body: HomeView(
              data: HomeViewData(
                institution: '吉林大学',
                username: 'student01',
                state: '已连接',
                detail: '认证成功',
                context: '以太网',
                glyph: '√',
                tone: HomeTone.success,
                secondaryLabel: '连接与诊断',
                primaryLabel: '断开连接',
                primaryKind: HomeButtonKind.secondary,
                primaryEnabled: true,
                onHeader: () {},
                onSettings: () {},
                onSecondary: () {},
                onPrimary: () {},
                actionError: '连接操作失败',
                notice: HomeNotice(
                  title: '吉林大学公告',
                  onOpen: () {},
                  feedback: '公告暂未显示，可在首页查看',
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final actionError = find.text('连接操作失败');
      final feedback = find.text('公告暂未显示，可在首页查看');
      final notice = tester.getRect(find.byKey(const ValueKey('home-notice')));
      expect(tester.getRect(actionError).bottom, lessThan(notice.top));
      expect(tester.getRect(feedback).top, greaterThanOrEqualTo(notice.bottom));
      expect(tester.getRect(feedback).left, notice.left + 12);
      expect(
        find.ancestor(of: feedback, matching: find.byType(DesignHelper)),
        findsOneWidget,
      );
      final text = tester.widget<Text>(feedback);
      final context = tester.element(feedback);
      expect(text.style!.color, Theme.of(context).colorScheme.onSurfaceVariant);
      expect(tester.takeException(), isNull);
    },
  );
}
