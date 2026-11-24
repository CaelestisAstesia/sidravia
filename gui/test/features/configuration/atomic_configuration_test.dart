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

  test('atomic edit stores changed binding and retires its session', () async {
    final client = OfflineDemoClient()..selectScenario('authenticated');
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    addTearDown(controller.dispose);
    await controller.start();
    final policy = NetworkBindingPolicy.explicit('demo-iface', '192.0.2.4');

    expect(
      await controller.updateConfiguration(
        configurationId: 'demo-config',
        institutionProfileId: 'jlu',
        username: controller.snapshot!.configurations.single.username,
        networkBindingPolicy: policy,
      ),
      isTrue,
    );
    expect(client.operations, contains('configuration.update'));
    expect(
      client.operations.where((op) => op == 'configuration.update'),
      hasLength(1),
    );
    expect(
      controller.snapshot!.configurations.single.networkBindingPolicy,
      policy,
    );
    expect(controller.snapshot!.sessions, isEmpty);
  });

  test('omitted or unchanged binding preserves the demo session', () async {
    final client = OfflineDemoClient()..selectScenario('authenticated');
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    addTearDown(controller.dispose);
    await controller.start();
    final policy =
        controller.snapshot!.configurations.single.networkBindingPolicy;
    final oldSessionId = controller.snapshot!.sessions.single.id;

    expect(
      await controller.updateConfiguration(
        configurationId: 'demo-config',
        institutionProfileId: 'jlu',
        username: controller.snapshot!.configurations.single.username,
        networkBindingPolicy: policy,
      ),
      isTrue,
    );
    expect(controller.snapshot!.sessions.single.id, oldSessionId);
    expect(
      await controller.updateConfiguration(
        configurationId: 'demo-config',
        institutionProfileId: 'jlu',
        username: controller.snapshot!.configurations.single.username,
      ),
      isTrue,
    );
    expect(controller.snapshot!.sessions.single.id, oldSessionId);
  });
}

class _CleanupFailureClient extends OfflineDemoClient {
  bool fail = true;
  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    NetworkBindingPolicy? networkBindingPolicy,
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
      networkBindingPolicy: networkBindingPolicy,
    );
  }
}
