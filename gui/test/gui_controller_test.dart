import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

void main() {
  test('publishes one complete sequential Snapshot', () async {
    final client = _Client();
    final controller = _controller(client);

    await controller.start();

    expect(client.calls, ['daemon', 'profiles', 'configurations', 'sessions']);
    expect(controller.state, GuiConnectionState.ready);
    expect(controller.snapshot?.configurations.single.username, 'fixture-user');
    controller.dispose();
  });

  test('serializes one mutation then refreshes the full Snapshot', () async {
    final client = _Client();
    final controller = _controller(client);
    await controller.start();
    client.calls.clear();

    expect(
      await controller.createConfiguration(
        institutionProfileId: 'jlu',
        username: 'new-user',
        password: 'transient-only',
      ),
      isTrue,
    );

    expect(client.calls, [
      'configuration.create',
      'daemon',
      'profiles',
      'configurations',
      'sessions',
    ]);
    expect(controller.notice, isNull);
    controller.dispose();
  });

  test(
    'business failures preserve the connection and publish fixed guidance',
    () async {
      final client = _Client();
      final controller = _controller(client);
      await controller.start();
      client.nextFailure = const IpcRequestFailure('profile_not_found');

      expect(
        await controller.updateConfiguration(
          configurationId: 'cfg-a',
          institutionProfileId: 'missing',
          username: 'fixture-user',
        ),
        isFalse,
      );
      expect(controller.state, GuiConnectionState.ready);
      expect(controller.notice, '未找到所选学校配置，请刷新后重试。');
      expect(client.closed, isFalse);
      controller.dispose();
    },
  );

  test(
    'protocol failures close the client and make a Snapshot stale',
    () async {
      final client = _Client();
      final controller = _controller(client);
      await controller.start();
      client.nextFailure = const IpcProtocolException();

      expect(await controller.startConfiguration('cfg-a'), isFalse);
      expect(controller.state, GuiConnectionState.stale);
      expect(client.closed, isTrue);
      controller.dispose();
    },
  );

  test(
    'coalesces bootstrap and suppresses late disposed completions',
    () async {
      final bootstrap = _PendingBootstrapper();
      final late = _Client();
      final connector = Completer<SidraviaIpcClient>();
      final controller = GuiController(
        bootstrapper: bootstrap,
        connector: (_) => connector.future,
        pollDelay: const Duration(days: 1),
      );
      final first = controller.start();
      expect(identical(first, controller.retry()), isTrue);
      bootstrap.complete();
      await Future<void>.delayed(Duration.zero);
      controller.dispose();
      connector.complete(late);
      await first;

      expect(bootstrap.calls, 1);
      expect(late.closed, isTrue);
    },
  );
}

GuiController _controller(_Client client) => GuiController(
  bootstrapper: _Bootstrapper(),
  connector: (_) async => client,
  pollDelay: const Duration(days: 1),
);

class _Bootstrapper implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
      token: _token,
      productVersion: 'fixture',
      buildId: 'fixture',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );
}

class _PendingBootstrapper implements GuiBootstrapper {
  final _value = Completer<GuiBootstrapResult>();
  var calls = 0;
  @override
  Future<GuiBootstrapResult> bootstrap() {
    calls++;
    return _value.future;
  }

  void complete() => _value.complete(
    GuiBootstrapResult.success(
      GuiBootstrap(
        endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
        token: _token,
        productVersion: 'fixture',
        buildId: 'fixture',
        daemonPid: 1,
        mode: 'desktop',
      ),
    ),
  );
}

class _Client implements SidraviaIpcClient {
  final calls = <String>[];
  Object? nextFailure;
  var closed = false;

  void _call(String name) {
    calls.add(name);
    final failure = nextFailure;
    nextFailure = null;
    if (failure != null) throw failure;
  }

  @override
  Future<DaemonStatus> daemonStatus() async {
    _call('daemon');
    return _daemon;
  }

  @override
  Future<List<InstitutionProfile>> profileList() async {
    _call('profiles');
    return const [_profile];
  }

  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    _call('configurations');
    return const [_configuration];
  }

  @override
  Future<List<SessionSummary>> sessionList() async {
    _call('sessions');
    return const [];
  }

  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async {
    _call('configuration.create');
    return _configuration;
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  }) async {
    _call('configuration.update');
    return _configuration;
  }

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async {
    _call('configuration.setPassword');
    return _configuration;
  }

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async {
    _call('session.startConfiguration');
    return _session;
  }

  @override
  Future<SessionSummary> sessionStop(String sessionId) async {
    _call('session.stop');
    return _session;
  }

  @override
  Future<SessionSummary> sessionEnsureRunning(String sessionId) async {
    _call('session.ensureRunning');
    return _session;
  }

  @override
  Future<SessionSummary> sessionRestart(String sessionId) async {
    _call('session.restart');
    return _session;
  }

  @override
  Future<void> close() async => closed = true;
}

const _daemon = DaemonStatus(
  productVersion: 'fixture',
  buildId: 'fixture',
  pid: 1,
  status: 'running',
  mode: 'desktop',
  desktopOwnerPid: 2,
);
const _profile = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom-5.2.0-d',
);
const _configuration = ConfigurationSummary(
  id: 'cfg-a',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'fixture-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _session = SessionSummary(
  id: 's-a',
  displayName: '',
  accountName: 'fixture-user',
  state: 'suspended',
  intent: 'suspend_authentication',
  configurationId: 'cfg-a',
);
const _token =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
