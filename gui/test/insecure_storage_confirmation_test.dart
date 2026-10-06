import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_operation.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

import 'connection_presentation_test.dart' show otherConfiguration;

class ConsentClient extends OfflineDemoClient {
  final attempts = <(GuiOperation, bool, String?)>[];
  int refreshes = 0;
  bool replaced = false;
  void reject(GuiOperation op, bool allow, [String? id]) {
    attempts.add((op, allow, id));
    if (!allow) {
      throw const IpcRequestFailure('insecure_storage_confirmation_required');
    }
  }

  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    refreshes++;
    if (replaced) return [otherConfiguration];
    return super.configurationList();
  }

  @override
  Future<List<SessionSummary>> sessionList() async =>
      replaced ? [] : super.sessionList();
  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
    required bool autoLogin,
    required bool autoReconnect,
    required bool allowInsecureStorage,
  }) async {
    reject(GuiOperation.createConfiguration, allowInsecureStorage);
    return super.configurationCreate(
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      autoLogin: autoLogin,
      autoReconnect: autoReconnect,
      allowInsecureStorage: allowInsecureStorage,
    );
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) async {
    reject(
      password == null
          ? GuiOperation.updateConfiguration
          : GuiOperation.updatePassword,
      allowInsecureStorage,
      configurationId,
    );
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
      allowInsecureStorage: allowInsecureStorage,
    );
  }

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
    bool allowInsecureStorage = false,
  }) async {
    reject(GuiOperation.updatePassword, allowInsecureStorage, configurationId);
    return super.configurationSetPassword(
      configurationId: configurationId,
      password: password,
      allowInsecureStorage: allowInsecureStorage,
    );
  }

  @override
  Future<ConfigurationSummary> configurationSetAutoLogin({
    required String configurationId,
    required bool autoLogin,
    bool allowInsecureStorage = false,
  }) async {
    reject(GuiOperation.autoLogin, allowInsecureStorage, configurationId);
    return super.configurationSetAutoLogin(
      configurationId: configurationId,
      autoLogin: autoLogin,
      allowInsecureStorage: allowInsecureStorage,
    );
  }

  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
    bool allowInsecureStorage = false,
  }) async {
    reject(GuiOperation.autoReconnect, allowInsecureStorage, configurationId);
    return super.configurationSetAutoReconnect(
      configurationId: configurationId,
      autoReconnect: autoReconnect,
      allowInsecureStorage: allowInsecureStorage,
    );
  }

  @override
  Future<ConfigurationRemoveResult> configurationRemove(
    String configurationId, {
    bool allowInsecureStorage = false,
  }) async {
    reject(
      GuiOperation.deleteConfiguration,
      allowInsecureStorage,
      configurationId,
    );
    return super.configurationRemove(
      configurationId,
      allowInsecureStorage: allowInsecureStorage,
    );
  }
}

Future<bool> invoke(
  GuiController c,
  GuiOperation op, {
  InsecureStorageConfirmation? confirm,
}) => switch (op) {
  GuiOperation.createConfiguration => c.createConfiguration(
    institutionProfileId: 'jlu',
    username: 'new',
    password: 'fixture',
    onInsecureStorageConfirmation: confirm,
  ),
  GuiOperation.updateConfiguration => c.updateConfiguration(
    configurationId: 'demo-config',
    institutionProfileId: 'jlu',
    username: 'new',
    onInsecureStorageConfirmation: confirm,
  ),
  GuiOperation.updatePassword => c.setPassword(
    configurationId: 'demo-config',
    password: 'fixture',
    onInsecureStorageConfirmation: confirm,
  ),
  GuiOperation.autoLogin => c.setAutoLogin(
    configurationId: 'demo-config',
    autoLogin: true,
    onInsecureStorageConfirmation: confirm,
  ),
  GuiOperation.autoReconnect => c.setAutoReconnect(
    configurationId: 'demo-config',
    autoReconnect: true,
    onInsecureStorageConfirmation: confirm,
  ),
  GuiOperation.deleteConfiguration => c.deleteConfiguration(
    'demo-config',
    onInsecureStorageConfirmation: confirm,
  ),
  _ => Future.value(false),
};
void main() {
  final operations = GuiOperation.values.where(
    (op) => op != GuiOperation.connection,
  );
  for (final op in operations) {
    test('$op no implicit consent and operation-aware safe guidance', () async {
      final client = ConsentClient();
      if (op == GuiOperation.createConfiguration) {
        client.selectScenario('empty');
      }
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      final snapshot = c.snapshot;
      expect(await invoke(c, op), isFalse);
      expect(client.attempts.map((a) => a.$2), [false]);
      expect(identical(c.snapshot, snapshot), isTrue);
      expect(c.notice, op.insecureStorageGuidance);
      expect(client.refreshes, 1);
      c.dispose();
      client.dispose();
    });
    for (final confirm in [false, true]) {
      testWidgets('$op dialog explicit confirm=$confirm', (tester) async {
        await tester.binding.setSurfaceSize(const Size(800, 1000));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        final client = ConsentClient();
        if (op == GuiOperation.createConfiguration) {
          client.selectScenario('empty');
        }
        final c = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(days: 1),
        );
        await c.start();
        final previous = c.snapshot;
        await tester.pumpWidget(
          MaterialApp(home: SidraviaShell(controller: c)),
        );
        await tester.pumpAndSettle();
        if (op == GuiOperation.autoLogin || op == GuiOperation.autoReconnect) {
          await tester.tap(find.byTooltip('设置'));
          await tester.pumpAndSettle();
          await tester.tap(
            find.byKey(
              ValueKey(
                op == GuiOperation.autoLogin
                    ? 'auto-login-switch'
                    : 'auto-reconnect-switch',
              ),
            ),
          );
        } else {
          await tester.tap(
            find.byKey(const ValueKey('home-configuration-button')),
          );
          await tester.pumpAndSettle();
          if (op == GuiOperation.deleteConfiguration) {
            await tester.ensureVisible(find.text('删除登录配置'));
            await tester.tap(find.text('删除登录配置'));
            await tester.pumpAndSettle();
            await tester.tap(find.text('确认'));
          } else {
            await tester.enterText(find.byType(TextField).first, 'new-user');
            if (op != GuiOperation.updateConfiguration) {
              await tester.enterText(
                find.byType(TextField).last,
                'fixture-password',
              );
            }
            await tester.tap(
              find.text(
                op == GuiOperation.createConfiguration ? '保存配置' : '保存更改',
              ),
            );
          }
        }
        await tester.pumpAndSettle();
        expect(find.text('允许未受保护的存储？'), findsOneWidget);
        expect(find.text(op.securityWarning), findsOneWidget);
        expect(client.attempts.map((a) => a.$2), [false]);
        expect(identical(c.snapshot, previous), isTrue);
        await tester.tap(find.text(confirm ? '仅本次允许' : '取消').last);
        await tester.pumpAndSettle();
        expect(
          client.attempts.map((a) => a.$2),
          confirm ? [false, true] : [false],
        );
        expect(client.refreshes, confirm ? 2 : 1);
        if (confirm && op != GuiOperation.createConfiguration) {
          expect(client.attempts.map((a) => a.$3), [
            'demo-config',
            'demo-config',
          ]);
        }
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
        c.dispose();
        client.dispose();
      });
    }
  }
  testWidgets(
    'confirmation cannot redirect operation after external replacement',
    (tester) async {
      final client = ConsentClient();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(milliseconds: 50),
      );
      await c.start();
      final confirmation = Completer<bool>();
      final operation = invoke(
        c,
        GuiOperation.updateConfiguration,
        confirm: (_) => confirmation.future,
      );
      await tester.pump();
      client.replaced = true;
      await tester.pump(const Duration(milliseconds: 60));
      await tester.pump();
      expect(c.snapshot!.configurations.single.id, 'other');
      confirmation.complete(true);
      await tester.pump();
      expect(await operation, isFalse);
      expect(client.attempts.map((a) => a.$2), [false]);
      c.dispose();
      client.dispose();
    },
  );
  test(
    'confirmation not retained across operations and dispose cancels retry',
    () async {
      final client = ConsentClient();
      final c = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await c.start();
      expect(
        await invoke(c, GuiOperation.autoLogin, confirm: (_) async => true),
        isTrue,
      );
      expect(await invoke(c, GuiOperation.autoLogin), isFalse);
      expect(client.attempts.map((a) => a.$2), [false, true, false]);
      final pending = Completer<bool>();
      final result = invoke(
        c,
        GuiOperation.autoLogin,
        confirm: (_) => pending.future,
      );
      await Future<void>.delayed(Duration.zero);
      c.dispose();
      pending.complete(true);
      expect(await result, isFalse);
      expect(client.attempts.map((a) => a.$2), [false, true, false, false]);
      client.dispose();
    },
  );
}
