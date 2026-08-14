import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

void main() {
  test('publishes a complete snapshot only after sequential polling', () async {
    final calls = <String>[];
    final client = _Client(calls);
    final controller = GuiController(
      bootstrapper: _Bootstrapper.success(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );

    await controller.start();

    expect(calls, [
      'daemon.status',
      'profile.list',
      'configuration.list',
      'session.list',
    ]);
    expect(controller.state, GuiConnectionState.ready);
    expect(controller.snapshot?.configurations, hasLength(1));
    controller.dispose();
  });

  test(
    'a refresh failure retains only an existing snapshot as stale',
    () async {
      final client = _Client(<String>[]);
      final controller = GuiController(
        bootstrapper: _Bootstrapper.success(),
        connector: (_) async => client,
        pollDelay: Duration.zero,
      );

      await controller.start();
      client.fail = true;
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);

      expect(controller.state, GuiConnectionState.stale);
      expect(controller.snapshot, isNotNull);
      controller.dispose();
    },
  );
}

class _Bootstrapper implements GuiBootstrapper {
  _Bootstrapper.success()
    : result = GuiBootstrapResult.success(
        GuiBootstrap(
          endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
          token: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
          productVersion: 'fixture',
          buildId: 'fixture',
          daemonPid: 1,
          mode: 'desktop',
        ),
      );

  final GuiBootstrapResult result;

  @override
  Future<GuiBootstrapResult> bootstrap() async => result;
}

class _Client implements SidraviaIpcClient {
  _Client(this.calls);

  final List<String> calls;
  bool fail = false;

  void _call(String name) {
    calls.add(name);
    if (fail) throw const IpcProtocolException();
  }

  @override
  Future<DaemonStatus> daemonStatus() async {
    _call('daemon.status');
    return const DaemonStatus(
      productVersion: 'fixture',
      buildId: 'fixture',
      pid: 1,
      status: 'running',
      mode: 'desktop',
    );
  }

  @override
  Future<List<InstitutionProfile>> profileList() async {
    _call('profile.list');
    return const [];
  }

  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    _call('configuration.list');
    return const [
      ConfigurationSummary(
        id: 'cfg-1',
        displayName: '校园登录',
        institutionDisplayName: '示例学校',
        username: 'fixture-user',
        credentialStored: true,
        storageProtection: 'protected',
      ),
    ];
  }

  @override
  Future<List<SessionSummary>> sessionList() async {
    _call('session.list');
    return const [];
  }

  @override
  Future<void> close() async {}
}
