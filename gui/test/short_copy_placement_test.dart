import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/feedback_pill.dart';

void main() {
  for (final width in [360.0, 960.0]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets('input helper belongs to label at $width scale $scale', (
        tester,
      ) async {
        await tester.binding.setSurfaceSize(Size(width, 720));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        final input = TextEditingController();
        addTearDown(input.dispose);
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.light(),
            home: MediaQuery(
              data: MediaQueryData(textScaler: TextScaler.linear(scale)),
              child: Scaffold(
                body: Column(
                  children: [
                    DesignInputRow(
                      label: '密码',
                      controller: input,
                      enabled: true,
                      helper: '首次连接请填写账号和密码。',
                    ),
                    const DesignHelper('第一句。第二句。'),
                    const Text('重启会中断连接。完成后恢复连接。'),
                  ],
                ),
              ),
            ),
          ),
        );
        final hint = find.text('首次连接请填写账号和密码');
        expect(
          find.ancestor(of: hint, matching: find.byType(DesignInputRow)),
          findsOneWidget,
        );
        expect(
          tester.getRect(hint).top - tester.getRect(find.text('密码')).bottom,
          closeTo(3, .01),
        );
        expect(find.text('第一句。第二句'), findsOneWidget);
        expect(find.text('重启会中断连接。完成后恢复连接。'), findsOneWidget);
        expect(tester.takeException(), isNull);
      });
    }
  }
  testWidgets('validation replaces field helper without a full stop', (
    tester,
  ) async {
    final input = TextEditingController();
    addTearDown(input.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DesignInputRow(
            label: '密码',
            controller: input,
            enabled: true,
            helper: '首次填写密码。',
            error: '请输入密码。',
          ),
        ),
      ),
    );
    expect(find.text('请输入密码'), findsOneWidget);
    expect(find.textContaining('首次填写'), findsNothing);
  });
  testWidgets('first credential hint belongs to password and discard closes', (
    tester,
  ) async {
    final client = OfflineDemoClient()..selectScenario('empty');
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await c.start();
    try {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ConfigurationPage(controller: c, onBack: () {}),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final row = find.ancestor(
        of: find.text('首次连接请填写账号和密码'),
        matching: find.byType(DesignInputRow),
      );
      expect(tester.widget<DesignInputRow>(row).label, '密码');
      await tester.enterText(find.byType(TextField).first, 'draft');
      await tester.ensureVisible(find.text('放弃修改'));
      await tester.tap(find.text('放弃修改'));
      await tester.pumpAndSettle();
      expect(find.text('已放弃修改'), findsOneWidget);
      expect(find.byType(FeedbackPill), findsOneWidget);
      expect(
        tester.widget<TextField>(find.byType(TextField).first).controller!.text,
        '',
      );
      final operations = List<String>.of(client.operations);
      await tester.ensureVisible(find.byTooltip('关闭提示'));
      await tester.tap(find.byTooltip('关闭提示'));
      await tester.pump();
      expect(find.text('已放弃修改'), findsNothing);
      expect(client.operations, operations);
      expect(c.snapshot!.configurations, isEmpty);
    } finally {
      await tester.pumpWidget(const SizedBox());
      await c.close();
      c.dispose();
    }
  });
  testWidgets('settings explanations belong to their specific rows', (
    tester,
  ) async {
    final client = OfflineDemoClient()..selectScenario('empty');
    final c = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    final appearance = Appearance(
      store: MemoryGuiSettingsStore()
        ..settings = const GuiSettings(
          appearanceMode: GuiAppearanceMode.system,
          announcementFrequency: GuiAnnouncementFrequency.everyConnection,
        ),
    );
    await appearance.initialize();
    await c.start();
    try {
      await tester.pumpWidget(
        AppearanceScope(
          appearance: appearance,
          child: MaterialApp(
            theme: AppTheme.light(),
            home: SettingsPage(
              controller: c,
              onNavigate: (_) {},
              onBack: () {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      for (final entry in {'请先添加连接配置': '连接配置', '连接成功后显示公告': '自动显示公告'}.entries) {
        final row = find.ancestor(
          of: find.text(entry.key),
          matching: find.byType(DesignRow),
        );
        expect(row, findsOneWidget);
        expect(tester.widget<DesignRow>(row).title, entry.value);
      }
      expect(tester.takeException(), isNull);
    } finally {
      await tester.pumpWidget(const SizedBox());
      await c.close();
      c.dispose();
      appearance.dispose();
    }
  });
}
