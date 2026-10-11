import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class RecordingClient extends OfflineDemoClient {
  final passwords = <String?>[];
  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    NetworkBindingPolicy? networkBindingPolicy,
    bool allowInsecureStorage = false,
  }) {
    passwords.add(password);
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      networkBindingPolicy: networkBindingPolicy,
      allowInsecureStorage: allowInsecureStorage,
    );
  }
}

void main() {
  testWidgets('page header remains visible when body scrolls at 360', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(360, 640));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: DesignPage(
            children: [
              DesignHeader(title: '设置', onBack: () {}),
              for (var i = 0; i < 30; i++)
                DesignRow(title: '示例 $i', value: '值'),
            ],
          ),
        ),
      ),
    );
    final before = tester.getRect(find.text('设置'));
    await tester.drag(
      find.byType(SingleChildScrollView),
      const Offset(0, -500),
    );
    await tester.pumpAndSettle();
    expect(tester.getRect(find.text('设置')), before);
    expect(find.text('返回').hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  for (final replacement in [false, true]) {
    testWidgets(
      'saved password placeholder, B discard, atomic password payload $replacement',
      (tester) async {
        final client = RecordingClient();
        final c = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(days: 1),
        );
        await c.start();
        try {
          await tester.pumpWidget(
            MaterialApp(
              theme: AppTheme.light(),
              home: Scaffold(
                body: ConfigurationPage(controller: c, onBack: () {}),
              ),
            ),
          );
          await tester.pumpAndSettle();
          final account = find.byType(TextField).first,
              password = find.byType(TextField).last;
          expect(
            tester.widget<TextField>(password).decoration!.hintText,
            '••••••••',
          );
          expect(tester.widget<TextField>(password).controller!.text, isEmpty);
          final original = tester.widget<TextField>(account).controller!.text;
          await tester.enterText(account, 'discarded');
          await tester.enterText(password, 'temporary');
          await tester.ensureVisible(find.text('放弃修改'));
          await tester.tap(find.text('放弃修改'));
          await tester.pumpAndSettle();
          expect(tester.widget<TextField>(account).controller!.text, original);
          expect(tester.widget<TextField>(password).controller!.text, isEmpty);
          expect(client.operations, isNot(contains('configuration.update')));
          expect(client.operations, isNot(contains('session.stop')));
          await tester.enterText(account, 'changed');
          if (replacement) await tester.enterText(password, 'replacement');
          await tester.ensureVisible(find.text('保存修改'));
          await tester.tap(find.text('保存修改'));
          await tester.pumpAndSettle();
          await tester.tap(find.text('保存并断开'));
          await tester.pumpAndSettle();
          expect(client.passwords, [replacement ? 'replacement' : null]);
          expect(
            client.operations.where((v) => v == 'configuration.update'),
            hasLength(1),
          );
          expect(
            client.operations,
            isNot(contains('configuration.set_password')),
          );
        } finally {
          await tester.pumpWidget(const SizedBox.shrink());
          await c.close();
          c.dispose();
          client.dispose();
        }
      },
    );
  }
  testWidgets(
    'announcement meaning remains without duplicate settings feedback',
    (tester) async {
      final client = OfflineDemoClient();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      final appearance = Appearance();
      try {
        await tester.pumpWidget(
          AppearanceScope(
            appearance: appearance,
            child: MaterialApp(
              theme: AppTheme.light(),
              home: Scaffold(
                body: SettingsPage(
                  controller: c,
                  onBack: () {},
                  onNavigate: (_) {},
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final card = find.ancestor(
          of: find.text('自动显示公告'),
          matching: find.byType(DesignGroup),
        );
        expect(card, findsOneWidget);
        expect(
          find.descendant(of: card, matching: find.text('通知可点击查看公告。')),
          findsNothing,
        );
        expect(
          find.descendant(of: card, matching: find.text('公告加载失败')),
          findsNothing,
        );
        expect(find.text('自动重连成功也计入连接成功。'), findsNothing);
        expect(find.textContaining('修改后从下次连接生效'), findsNothing);
        final context = tester.element(find.text('自动显示公告'));
        expect(
          Theme.of(context).textTheme.bodyMedium!.fontFamily,
          'HarmonyOS Sans',
        );
        expect(
          Theme.of(context).dialogTheme.contentTextStyle!.fontFamily,
          'HarmonyOS Sans',
        );
      } finally {
        await tester.pumpWidget(const SizedBox.shrink());
        await c.close();
        c.dispose();
        client.dispose();
        appearance.dispose();
      }
    },
  );
}
