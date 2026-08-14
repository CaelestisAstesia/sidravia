import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

void main() {
  testWidgets('zero configuration creates with obscured transient password', (
    tester,
  ) async {
    final client = _Client(configurations: const []);
    await tester.pumpWidget(_app(client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('配置').first);
    await tester.pumpAndSettle();

    final password = find.byKey(const ValueKey('configuration-password'));
    expect(tester.widget<TextField>(password).obscureText, isTrue);
    await tester.enterText(
      find.byKey(const ValueKey('configuration-account')),
      'new-user',
    );
    await tester.enterText(password, 'transient-only');
    await tester.tap(find.text('保存配置'));
    await tester.pumpAndSettle();

    expect(client.calls, contains('configuration.create'));
    expect(tester.widget<TextField>(password).controller?.text, isEmpty);
  });

  testWidgets(
    'one configuration updates separately and uses username fallback',
    (tester) async {
      final client = _Client(configurations: const [_configuration]);
      await tester.pumpWidget(_app(client));
      await tester.pumpAndSettle();
      await tester.tap(find.text('配置').first);
      await tester.pumpAndSettle();

      expect(find.text('fixture-user'), findsWidgets);
      await tester.tap(find.text('保存账号与学校'));
      await tester.pumpAndSettle();
      expect(client.calls, contains('configuration.update'));
    },
  );

  testWidgets(
    'multiple configurations and ambiguous Sessions remain read-only',
    (tester) async {
      final client = _Client(configurations: const [_configuration, _other]);
      await tester.pumpWidget(_app(client));
      await tester.pumpAndSettle();
      await tester.tap(find.text('配置').first);
      await tester.pumpAndSettle();
      expect(find.text('存在多个登录配置'), findsOneWidget);
      expect(find.text('保存配置'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      await tester.pumpWidget(
        _app(_Client(sessions: const [_session, _session])),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('会话关系不明确'), findsOneWidget);
    },
  );

  testWidgets('home routes one unambiguous session action', (tester) async {
    final client = _Client(sessions: const [_session]);
    await tester.pumpWidget(_app(client));
    await tester.pumpAndSettle();
    expect(find.text('恢复认证'), findsOneWidget);
    await tester.tap(find.text('恢复认证'));
    await tester.pumpAndSettle();
    expect(client.calls, contains('session.ensureRunning'));
  });
}

SidraviaApp _app(_Client client) => SidraviaApp(
  controller: GuiController(
    bootstrapper: _Bootstrapper(),
    connector: (_) async => client,
    pollDelay: const Duration(days: 1),
  ),
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

class _Client implements SidraviaIpcClient {
  _Client({
    this.configurations = const [_configuration],
    this.sessions = const [],
  });
  final List<ConfigurationSummary> configurations;
  final List<SessionSummary> sessions;
  final calls = <String>[];

  @override
  Future<DaemonStatus> daemonStatus() async => _daemon;
  @override
  Future<List<InstitutionProfile>> profileList() async => const [_profile];
  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      configurations;
  @override
  Future<List<SessionSummary>> sessionList() async => sessions;
  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async {
    calls.add('configuration.create');
    return _configuration;
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  }) async {
    calls.add('configuration.update');
    return _configuration;
  }

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async {
    calls.add('configuration.setPassword');
    return _configuration;
  }

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async {
    calls.add('session.startConfiguration');
    return _session;
  }

  @override
  Future<SessionSummary> sessionStop(String sessionId) async {
    calls.add('session.stop');
    return _session;
  }

  @override
  Future<SessionSummary> sessionEnsureRunning(String sessionId) async {
    calls.add('session.ensureRunning');
    return _session;
  }

  @override
  Future<SessionSummary> sessionRestart(String sessionId) async {
    calls.add('session.restart');
    return _session;
  }

  @override
  Future<void> close() async {}
}

const _daemon = DaemonStatus(
  productVersion: 'fixture',
  buildId: 'fixture',
  pid: 1,
  status: 'running',
  mode: 'desktop',
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
const _other = ConfigurationSummary(
  id: 'cfg-b',
  displayName: 'other',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'other-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _session = SessionSummary(
  id: 'session-a',
  displayName: '',
  accountName: 'fixture-user',
  state: 'suspended',
  intent: 'suspend_authentication',
  configurationId: 'cfg-a',
);
const _token =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
