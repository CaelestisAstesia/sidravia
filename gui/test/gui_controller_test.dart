import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

void main() {
  test('publishes a complete snapshot only after sequential polling', () async {
    final calls = <String>[];
    final client = _Client(calls: calls);
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

  test('coalesces retries during one bootstrap transition', () async {
    final pending = Completer<GuiBootstrapResult>();
    final bootstrapper = _Bootstrapper.pending(pending);
    final controller = GuiController(
      bootstrapper: bootstrapper,
      connector: (_) async => _Client(),
      pollDelay: const Duration(days: 1),
    );

    final first = controller.start();
    final second = controller.retry();
    expect(identical(first, second), isTrue);
    expect(bootstrapper.calls, 1);
    pending.complete(_Bootstrapper.successResult);
    await first;
    expect(bootstrapper.calls, 1);
    controller.dispose();
  });

  test(
    'refresh failure retains a snapshot as stale and leaves no dead timer',
    () async {
      final client = _Client();
      final controller = GuiController(
        bootstrapper: _Bootstrapper.success(),
        connector: (_) async => client,
        pollDelay: Duration.zero,
      );

      await controller.start();
      client.fail = true;
      await _settle();

      expect(controller.state, GuiConnectionState.stale);
      expect(controller.snapshot, isNotNull);
      expect(client.closed, isTrue);
      final callsAfterFailure = client.calls.length;
      await _settle();
      expect(client.calls.length, callsAfterFailure);
      controller.dispose();
    },
  );

  test('late completion from an old generation cannot publish or close a new client', () async {
    final oldClient = _Client(pauseAfterFirstRefresh: true);
    final newClient = _Client(configurations: const []);
    final bootstrapper = _Bootstrapper.queue();
    final controller = GuiController(
      bootstrapper: bootstrapper,
      connector: (_) async => bootstrapper.clients.removeAt(0),
      pollDelay: Duration.zero,
    );
    bootstrapper.clients.addAll([oldClient, newClient]);

    await controller.start();
    await oldClient.secondDaemonStarted.future;
    await controller.retry();
    expect(controller.snapshot?.configurations, isEmpty);
    oldClient.releaseSecondDaemon.complete(_daemon);
    await _settle();

    expect(controller.state, GuiConnectionState.ready);
    expect(controller.snapshot?.configurations, isEmpty);
    expect(newClient.closed, isFalse);
    controller.dispose();
  });

  test(
    'dispose prevents a late client from publishing and closes it',
    () async {
      final connector = Completer<SidraviaIpcClient>();
      final lateClient = _Client();
      final controller = GuiController(
        bootstrapper: _Bootstrapper.success(),
        connector: (_) => connector.future,
        pollDelay: const Duration(days: 1),
      );

      final start = controller.start();
      await Future<void>.delayed(Duration.zero);
      controller.dispose();
      connector.complete(lateClient);
      await start;

      expect(lateClient.closed, isTrue);
    },
  );
}

Future<void> _settle() async {
  await Future<void>.delayed(Duration.zero);
  await Future<void>.delayed(Duration.zero);
  await Future<void>.delayed(Duration.zero);
}

const _daemon = DaemonStatus(
  productVersion: 'fixture',
  buildId: 'fixture',
  pid: 1,
  status: 'running',
  mode: 'desktop',
  desktopOwnerPid: 2,
);

class _Bootstrapper implements GuiBootstrapper {
  _Bootstrapper.success() : _result = successResult, pending = null;
  _Bootstrapper.pending(this.pending) : _result = null;
  _Bootstrapper.queue() : _result = successResult, pending = null;

  static final successResult = GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
      token: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
      productVersion: 'fixture',
      buildId: 'fixture',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );

  final GuiBootstrapResult? _result;
  final Completer<GuiBootstrapResult>? pending;
  final clients = <SidraviaIpcClient>[];
  var calls = 0;

  @override
  Future<GuiBootstrapResult> bootstrap() {
    calls++;
    return pending?.future ?? Future.value(_result!);
  }
}

class _Client implements SidraviaIpcClient {
  _Client({
    List<String>? calls,
    this.configurations = const [
      ConfigurationSummary(
        id: 'cfg-1',
        displayName: '校园登录',
        institutionDisplayName: '示例学校',
        username: 'fixture-user',
        credentialStored: true,
        storageProtection: 'protected',
      ),
    ],
    this.pauseAfterFirstRefresh = false,
  }) : calls = calls ?? <String>[];

  final List<String> calls;
  final List<ConfigurationSummary> configurations;
  final bool pauseAfterFirstRefresh;
  final secondDaemonStarted = Completer<void>();
  final releaseSecondDaemon = Completer<DaemonStatus>();
  var daemonCalls = 0;
  var fail = false;
  var closed = false;

  void _call(String name) {
    calls.add(name);
    if (fail) throw const IpcProtocolException();
  }

  @override
  Future<DaemonStatus> daemonStatus() async {
    _call('daemon.status');
    daemonCalls++;
    if (pauseAfterFirstRefresh && daemonCalls == 2) {
      secondDaemonStarted.complete();
      return releaseSecondDaemon.future;
    }
    return _daemon;
  }

  @override
  Future<List<InstitutionProfile>> profileList() async {
    _call('profile.list');
    return const [];
  }

  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    _call('configuration.list');
    return configurations;
  }

  @override
  Future<List<SessionSummary>> sessionList() async {
    _call('session.list');
    return const [];
  }

  @override
  Future<void> close() async {
    closed = true;
  }
}
