import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

void main() {
  test(
    'IPC diagnostics distinguish connection, timeout and protocol',
    () async {
      for (final code in [
        'ipc_connection_failed',
        'ipc_handshake_failed',
        'ipc_timeout',
      ]) {
        final controller = GuiController(
          bootstrapper: _Bootstrapper(),
          connector: (_) async => throw IpcTransportException(code),
        );
        await controller.start();
        expect(controller.state, GuiConnectionState.failed);
        expect(controller.failure?.code, code);
        controller.dispose();
      }
    },
  );

  test(
    'mutation success followed by refresh failure preserves stale snapshot',
    () async {
      final client = _Client();
      final controller = _controller(client);
      await controller.start();
      final previous = controller.snapshot;
      client.nextDaemon = Completer<DaemonStatus>();
      final mutation = controller.stopSession('session-a');
      client.nextDaemon!.completeError(
        const IpcTransportException('ipc_timeout'),
      );
      expect(await mutation, isFalse);
      expect(controller.state, GuiConnectionState.stale);
      expect(controller.failure?.code, 'ipc_timeout');
      expect(identical(controller.snapshot, previous), isTrue);
      expect(client.closed, isTrue);
      controller.dispose();
    },
  );

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
      expect(controller.notice, '学校配置已不存在，请刷新。');
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

  test(
    'polling stays passive and a concurrent user action waits for it',
    () async {
      final client = _Client();
      final controller = GuiController(
        bootstrapper: _Bootstrapper(),
        connector: (_) async => client,
        pollDelay: const Duration(milliseconds: 5),
      );
      await controller.start();
      expect(controller.busy, isFalse);

      final pendingDaemon = Completer<DaemonStatus>();
      client.nextDaemon = pendingDaemon;
      await Future<void>.delayed(const Duration(milliseconds: 15));

      expect(controller.busy, isFalse);
      final mutation = controller.startConfiguration('cfg-a');
      await Future<void>.delayed(Duration.zero);
      expect(client.calls, isNot(contains('session.startConfiguration')));

      pendingDaemon.complete(_daemon);
      expect(await mutation, isTrue);
      expect(client.calls, contains('session.startConfiguration'));
      expect(controller.busy, isFalse);
      controller.dispose();
    },
  );

  test('exitAndDisconnect cancels polling, submits daemon.stop once, and is one-shot', () async {
    final client = _Client();
    final controller = GuiController(
      bootstrapper: _Bootstrapper(),
      connector: (_) async => client,
      pollDelay: const Duration(milliseconds: 5),
    );
    await controller.start();
    await Future<void>.delayed(const Duration(milliseconds: 15));

    expect(await controller.exitAndDisconnect(), isTrue);
    expect(client.daemonStopCalls, 1);
    expect(await controller.exitAndDisconnect(), isFalse);
    expect(client.daemonStopCalls, 1);
    controller.dispose();
  });

  test('exitAndDisconnect remains one-shot despite stop failures', () async {
    final client = _Client();
    final controller = GuiController(
      bootstrapper: _Bootstrapper(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    client.stopFailure = const IpcRequestFailure('invalid_argument');

    expect(await controller.exitAndDisconnect(), isFalse);
    expect(client.daemonStopCalls, 1);
    expect(await controller.exitAndDisconnect(), isFalse);
    expect(client.daemonStopCalls, 1);
    controller.dispose();
  });

  test(
    'exitAndDisconnect times out a hung daemon.stop and still completes',
    () async {
      final client = _Client();
      final controller = GuiController(
        bootstrapper: _Bootstrapper(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
        stopTimeout: const Duration(milliseconds: 40),
      );
      await controller.start();
      client.pendingStop = Completer<DaemonStopResult>();
      final watch = Stopwatch()..start();

      expect(await controller.exitAndDisconnect(), isFalse);
      expect(watch.elapsedMilliseconds, lessThan(500));
      expect(client.daemonStopCalls, 1);
      controller.dispose();
    },
  );

  test('exitAndDisconnect applies one deadline to an existing refresh and schedules no poll', () async {
    final client = _Client();
    final controller = GuiController(
      bootstrapper: _Bootstrapper(),
      connector: (_) async => client,
      pollDelay: const Duration(milliseconds: 5),
      stopTimeout: const Duration(milliseconds: 40),
    );
    await controller.start();
    final pendingDaemon = Completer<DaemonStatus>();
    client.nextDaemon = pendingDaemon;
    await Future<void>.delayed(const Duration(milliseconds: 15));
    final callsBeforeExit = client.calls.length;
    final watch = Stopwatch()..start();

    expect(await controller.exitAndDisconnect(), isFalse);
    expect(watch.elapsedMilliseconds, lessThan(500));
    expect(client.daemonStopCalls, 0);

    pendingDaemon.complete(_daemon);
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(client.calls.length, callsBeforeExit);
    controller.dispose();
  });

  test('ordinary dispose never submits daemon.stop', () async {
    final client = _Client();
    final controller = _controller(client);
    await controller.start();
    controller.dispose();

    expect(client.daemonStopCalls, 0);
  });

  test('resetSession removes the Session once then refreshes', () async {
    final client = _Client();
    final controller = _controller(client);
    await controller.start();
    client.calls.clear();

    expect(await controller.resetSession('s-a'), isTrue);

    expect(client.calls, [
      'session.remove',
      'daemon',
      'profiles',
      'configurations',
      'sessions',
    ]);
    expect(client.sessionRemoveCalls, 1);
    expect(controller.notice, isNull);
    controller.dispose();
  });

  test(
    'deleteConfiguration removes the Configuration once then refreshes',
    () async {
      final client = _Client();
      final controller = _controller(client);
      await controller.start();
      client.calls.clear();

      expect(await controller.deleteConfiguration('cfg-a'), isTrue);

      expect(client.calls, [
        'configuration.remove',
        'daemon',
        'profiles',
        'configurations',
        'sessions',
      ]);
      expect(client.configurationRemoveCalls, 1);
      expect(controller.notice, isNull);
      controller.dispose();
    },
  );

  test('setAutoLogin updates once then refreshes from the Snapshot', () async {
    final client = _Client();
    final controller = _controller(client);
    await controller.start();
    client.calls.clear();

    expect(
      await controller.setAutoLogin(configurationId: 'cfg-a', autoLogin: false),
      isTrue,
    );

    expect(client.calls, [
      'configuration.setAutoLogin',
      'daemon',
      'profiles',
      'configurations',
      'sessions',
    ]);
    expect(client.autoLoginCalls, 1);
    expect(controller.snapshot?.configurations.single.autoLogin, isFalse);
    expect(controller.notice, isNull);
    controller.dispose();
  });

  test(
    'lifecycle mutations revalidate targets after an in-flight poll',
    () async {
      for (final (name, action, mutationCall)
          in <(String, Future<bool> Function(GuiController), String)>[
            (
              'reset',
              (controller) => controller.resetSession('s-a'),
              'session.remove',
            ),
            (
              'delete',
              (controller) => controller.deleteConfiguration('cfg-a'),
              'configuration.remove',
            ),
            (
              'auto-login',
              (controller) => controller.setAutoLogin(
                configurationId: 'cfg-a',
                autoLogin: true,
              ),
              'configuration.setAutoLogin',
            ),
          ]) {
        final client = _Client();
        final controller = GuiController(
          bootstrapper: _Bootstrapper(),
          connector: (_) async => client,
          pollDelay: const Duration(milliseconds: 5),
        );
        await controller.start();
        client.calls.clear();

        final pendingDaemon = Completer<DaemonStatus>();
        client.nextDaemon = pendingDaemon;
        await Future<void>.delayed(const Duration(milliseconds: 15));
        client.configurations = const [_otherConfiguration];
        client.sessions = const [_otherSession];

        final mutation = action(controller);
        await Future<void>.delayed(Duration.zero);
        expect(client.calls, isNot(contains(mutationCall)), reason: name);

        pendingDaemon.complete(_daemon);
        expect(await mutation, isFalse, reason: name);
        expect(client.calls, isNot(contains(mutationCall)), reason: name);
        controller.dispose();
      }
    },
  );

  test(
    'new lifecycle operations preserve the connection and map safe guidance',
    () async {
      for (final (count, failure, action, expected) in [
        (
          'reset',
          const IpcRequestFailure('session_not_found'),
          (GuiController c) => c.resetSession('s-a'),
          '会话已不存在，请刷新。',
        ),
        (
          'delete',
          const IpcRequestFailure('configuration_not_found'),
          (GuiController c) => c.deleteConfiguration('cfg-a'),
          '登录配置已不存在，请刷新。',
        ),
        (
          'auto-login',
          const IpcRequestFailure('configuration_auto_login_conflict'),
          (GuiController c) =>
              c.setAutoLogin(configurationId: 'cfg-a', autoLogin: true),
          '其他配置已启用自动登录，请先处理。',
        ),
        (
          'reset fallback',
          const IpcRequestFailure('unknown_code'),
          (GuiController c) => c.resetSession('s-a'),
          '重置会话失败，请重试。',
        ),
        (
          'deletion fallback',
          const IpcRequestFailure('unknown_code'),
          (GuiController c) => c.deleteConfiguration('cfg-a'),
          '删除配置失败，请重试。',
        ),
        (
          'auto-login fallback',
          const IpcRequestFailure('unknown_code'),
          (GuiController c) =>
              c.setAutoLogin(configurationId: 'cfg-a', autoLogin: true),
          '自动登录设置失败，请重试。',
        ),
      ]) {
        final client = _Client();
        final controller = _controller(client);
        await controller.start();
        client.nextFailure = failure;

        expect(await action(controller), isFalse, reason: count);
        expect(controller.state, GuiConnectionState.ready, reason: count);
        expect(controller.notice, expected, reason: count);
        expect(client.closed, isFalse, reason: count);
        controller.dispose();
      }
    },
  );

  test(
    'protocol failures on new lifecycle operations invalidate the client',
    () async {
      for (final action in <Future<bool> Function(GuiController)>[
        (c) => c.resetSession('s-a'),
        (c) => c.deleteConfiguration('cfg-a'),
        (c) => c.setAutoLogin(configurationId: 'cfg-a', autoLogin: true),
      ]) {
        final client = _Client();
        final controller = _controller(client);
        await controller.start();
        client.nextFailure = const IpcProtocolException();

        expect(await action(controller), isFalse);
        expect(controller.state, GuiConnectionState.stale);
        expect(client.closed, isTrue);
        controller.dispose();
      }
    },
  );

  test(
    'exit-start suppresses all three lifecycle operations before IPC',
    () async {
      final client = _Client();
      final controller = _controller(client);
      await controller.start();
      expect(await controller.exitAndDisconnect(), isTrue);
      client.calls.clear();

      expect(await controller.resetSession('s-a'), isFalse);
      expect(await controller.deleteConfiguration('cfg-a'), isFalse);
      expect(
        await controller.setAutoLogin(
          configurationId: 'cfg-a',
          autoLogin: true,
        ),
        isFalse,
      );

      expect(client.calls, isEmpty);
      expect(client.sessionRemoveCalls, 0);
      expect(client.configurationRemoveCalls, 0);
      expect(client.autoLoginCalls, 0);
      controller.dispose();
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

class _Client implements SidraviaDesktopClient {
  final calls = <String>[];
  List<ConfigurationSummary> configurations = const [_configuration];
  List<SessionSummary> sessions = const [_session];
  Object? nextFailure;
  Completer<DaemonStatus>? nextDaemon;
  Object? stopFailure;
  Completer<DaemonStopResult>? pendingStop;
  var daemonStopCalls = 0;
  var sessionRemoveCalls = 0;
  var configurationRemoveCalls = 0;
  var autoLoginCalls = 0;
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
    final pending = nextDaemon;
    nextDaemon = null;
    if (pending != null) return pending.future;
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
    return configurations;
  }

  @override
  Future<List<SessionSummary>> sessionList() async {
    _call('sessions');
    return sessions;
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
    String? password,
    bool allowInsecureStorage = false,
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
  Future<SessionRemoveResult> sessionRemove(String sessionId) async {
    sessionRemoveCalls++;
    _call('session.remove');
    sessions = sessions
        .where((session) => session.id != sessionId)
        .toList(growable: false);
    return SessionRemoveResult(sessionId: sessionId, status: 'removed');
  }

  @override
  Future<ConfigurationSummary> configurationSetAutoLogin({
    required String configurationId,
    required bool autoLogin,
  }) async {
    autoLoginCalls++;
    _call('configuration.setAutoLogin');
    final updated = ConfigurationSummary(
      id: _configuration.id,
      displayName: _configuration.displayName,
      institutionProfileId: _configuration.institutionProfileId,
      institutionDisplayName: _configuration.institutionDisplayName,
      authenticationProtocolId: _configuration.authenticationProtocolId,
      username: _configuration.username,
      credentialStored: _configuration.credentialStored,
      storageProtection: _configuration.storageProtection,
      autoLogin: autoLogin,
      autoReconnect: _configuration.autoReconnect,
    );
    configurations = [updated];
    return updated;
  }

  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
  }) async {
    _call('configuration.setAutoReconnect');
    final c = configurations.single;
    final updated = ConfigurationSummary(
      id: c.id,
      displayName: c.displayName,
      institutionProfileId: c.institutionProfileId,
      institutionDisplayName: c.institutionDisplayName,
      authenticationProtocolId: c.authenticationProtocolId,
      username: c.username,
      credentialStored: c.credentialStored,
      storageProtection: c.storageProtection,
      autoLogin: c.autoLogin,
      autoReconnect: autoReconnect,
    );
    configurations = [updated];
    return updated;
  }

  @override
  Future<ConfigurationRemoveResult> configurationRemove(
    String configurationId,
  ) async {
    configurationRemoveCalls++;
    _call('configuration.remove');
    configurations = const [];
    sessions = const [];
    return ConfigurationRemoveResult(
      configurationId: configurationId,
      status: 'removed',
    );
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
  Future<DaemonStopResult> daemonStop() async {
    daemonStopCalls++;
    final pending = pendingStop;
    pendingStop = null;
    if (pending != null) return pending.future;
    final failure = stopFailure;
    stopFailure = null;
    if (failure != null) throw failure;
    return const DaemonStopResult(status: 'stopping');
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
const _otherConfiguration = ConfigurationSummary(
  id: 'cfg-b',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'other-user',
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
const _otherSession = SessionSummary(
  id: 's-b',
  displayName: '',
  accountName: 'other-user',
  state: 'suspended',
  intent: 'suspend_authentication',
  configurationId: 'cfg-b',
);
const _token =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
