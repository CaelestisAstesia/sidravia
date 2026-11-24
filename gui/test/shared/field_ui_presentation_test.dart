import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/about_dialog.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/feedback_pill.dart';

void main() {
  testWidgets('inline header keeps return and trailing operation usable', (
    tester,
  ) async {
    var backs = 0;
    var actions = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 298,
            child: DesignHeader(
              title: '设置',
              onBack: () => backs++,
              actions: [
                TextButton(onPressed: () => actions++, child: const Text('保存')),
              ],
            ),
          ),
        ),
      ),
    );
    final back = find.byKey(const ValueKey('design-back-button'));
    expect(
      tester.getCenter(back).dy,
      closeTo(tester.getCenter(find.text('设置')).dy, 1),
    );
    expect(
      tester.getCenter(find.text('保存')).dx,
      greaterThan(tester.getCenter(find.text('设置')).dx),
    );
    await tester.tap(back);
    await tester.tap(find.text('保存'));
    expect(backs, 1);
    expect(actions, 1);
    expect(tester.takeException(), isNull);
  });
  testWidgets('long header and feedback wrap at narrow width with large text', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(2)),
          child: Scaffold(
            body: SizedBox(
              width: 298,
              child: Column(
                children: [
                  DesignHeader(title: '连接与诊断', onBack: () {}),
                  const FeedbackPill(message: '公告通知发送失败，可在首页查看。'),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    expect(find.text('公告通知发送失败，可在首页查看'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.tap(find.byTooltip('关闭提示'));
    await tester.pump();
    expect(find.text('公告通知发送失败，可在首页查看'), findsNothing);
  });
  testWidgets(
    'dismissal persists for same feedback and resets for new message',
    (tester) async {
      Future<void> show(String message) => tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: FeedbackPill(message: message)),
        ),
      );
      await show('核心已重启。');
      expect(find.text('核心已重启'), findsOneWidget);
      await tester.tap(find.byTooltip('关闭提示'));
      await tester.pump();
      await show('核心已重启。');
      expect(find.text('核心已重启'), findsNothing);
      await show('修改已保存.');
      expect(find.text('修改已保存'), findsOneWidget);
    },
  );
  testWidgets('about and license detail return through each nested dialog', (
    tester,
  ) async {
    LicenseRegistry.addLicense(
      () => Stream.value(
        const LicenseEntryWithLineBreaks([
          'field-ui-test-package',
        ], 'Test permission text'),
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => showSidraviaAbout(context, version: '1.2.3'),
              child: const Text('打开关于'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('打开关于'));
    await tester.pumpAndSettle();
    expect(find.text('1.2.3'), findsOneWidget);
    await tester.tap(find.text('许可'));
    await tester.pumpAndSettle();
    expect(find.text('开源许可'), findsOneWidget);
    await tester.tap(find.text('field-ui-test-package'));
    await tester.pumpAndSettle();
    expect(find.text('Test permission text'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('design-back-button')).last);
    await tester.pumpAndSettle();
    expect(find.text('Test permission text'), findsNothing);
    expect(find.text('开源许可'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('design-back-button')).last);
    await tester.pumpAndSettle();
    expect(find.text('关于 Sidravia'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('design-back-button')).last);
    await tester.pumpAndSettle();
    expect(find.byType(Dialog), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
