import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

import 'connection_presentation_test.dart'
    show configuration, otherConfiguration;

class ReplacementClient extends OfflineDemoClient {
  List<ConfigurationSummary> configurations = [configuration];
  final updatedIds = <String>[];
  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      configurations;
  @override
  Future<List<SessionSummary>> sessionList() async => [];
  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) async {
    updatedIds.add(configurationId);
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      allowInsecureStorage: allowInsecureStorage,
    );
  }
}

void main() {
  for (final creating in [false, true]) {
    testWidgets(
      'draft target replacement never writes to new ID creating=$creating',
      (tester) async {
        final client = ReplacementClient();
        if (creating) client.configurations = [];
        final controller = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(milliseconds: 50),
        );
        await controller.start();
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: ConfigurationPage(controller: controller, onBack: () {}),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.enterText(find.byType(TextField).first, 'draft-for-A');
        await tester.enterText(find.byType(TextField).last, 'transient-draft');
        client.configurations = [otherConfiguration];
        await tester.pump(const Duration(milliseconds: 60));
        await tester.pumpAndSettle();
        expect(
          tester
              .widget<TextField>(find.byType(TextField).first)
              .controller!
              .text,
          'draft-for-A',
        );
        expect(
          tester
              .widget<TextField>(find.byType(TextField).last)
              .controller!
              .text,
          'transient-draft',
        );
        expect(find.textContaining('请重新打开后继续编辑'), findsOneWidget);
        final button = find.widgetWithText(
          FilledButton,
          creating ? '保存配置' : '保存更改',
        );
        expect(tester.widget<FilledButton>(button).onPressed, isNull);
        await tester.tap(button);
        await tester.pumpAndSettle();
        expect(client.updatedIds, isEmpty);
        expect(client.operations, isEmpty);
        await tester.pumpWidget(const SizedBox.shrink());
        controller.dispose();
        client.dispose();
      },
    );
  }
}
