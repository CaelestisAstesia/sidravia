import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  test(
    'cleanup failure refreshes, requires reset, and clears after removal',
    () async {
      final client = _CleanupFailureClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      addTearDown(controller.dispose);
      await controller.start();
      final id = controller.snapshot!.sessions.single.id;
      expect(
        await controller.updateConfiguration(
          configurationId: 'demo-config',
          institutionProfileId: 'jlu',
          username: 'new',
        ),
        isFalse,
      );
      expect(controller.sessionNeedsReset, isTrue);
      expect(controller.notice, contains('配置已提交'));
      expect(await controller.resetSession(id), isTrue);
      expect(controller.sessionNeedsReset, isFalse);
    },
  );
  for (final state in ['authenticated', 'suspended', 'blocked_by_error']) {
    test('atomic edit retires $state session and refreshes account', () async {
      final client = OfflineDemoClient()..selectScenario(state);
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      addTearDown(controller.dispose);
      await controller.start();
      final oldId = controller.snapshot!.sessions.single.id;
      expect(
        await controller.updateConfiguration(
          configurationId: 'demo-config',
          institutionProfileId: 'jlu',
          username: 'replacement-user',
          password: 'replacement-secret',
        ),
        isTrue,
      );
      expect(
        client.operations.where((op) => op == 'configuration.update'),
        hasLength(1),
      );
      expect(client.operations, isNot(contains('configuration.set_password')));
      expect(
        controller.snapshot!.configurations.single.username,
        'replacement-user',
      );
      expect(controller.snapshot!.sessions, isEmpty);
      expect(await controller.startConfiguration('demo-config'), isTrue);
      expect(controller.snapshot!.sessions.single.id, isNot(oldId));
      expect(
        controller.snapshot!.sessions.single.accountName,
        'replacement-user',
      );
    });
  }
}

class _CleanupFailureClient extends OfflineDemoClient {
  bool fail = true;
  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) async {
    if (fail) {
      throw const IpcRequestFailure(
        'configuration_session_invalidation_failed',
      );
    }
    return super.configurationUpdate(
      configurationId: configurationId,
      institutionProfileId: institutionProfileId,
      username: username,
      password: password,
    );
  }
}
