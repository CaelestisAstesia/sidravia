import 'dart:async';

import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

enum PreviewScenario {
  bootstrapping('正在启动'),
  noConfiguration('尚未配置'),
  authenticated('已认证'),
  blocked('认证错误'),
  multipleObjects('多个对象');

  const PreviewScenario(this.label);
  final String label;
}

GuiController createPreviewController(PreviewScenario scenario) {
  final bootstrapper = scenario == PreviewScenario.bootstrapping
      ? const _PendingBootstrapper()
      : const _PreviewBootstrapper();
  return GuiController(
    bootstrapper: bootstrapper,
    connector: (_) async => _PreviewClient(scenario),
    pollDelay: const Duration(days: 1),
  );
}

class _PendingBootstrapper implements GuiBootstrapper {
  const _PendingBootstrapper();

  @override
  Future<GuiBootstrapResult> bootstrap() =>
      Completer<GuiBootstrapResult>().future;
}

class _PreviewBootstrapper implements GuiBootstrapper {
  const _PreviewBootstrapper();

  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
      token: _previewToken,
      productVersion: 'preview-only',
      buildId: 'preview-only',
      daemonPid: 4711,
      mode: 'desktop',
    ),
  );
}

class _PreviewClient implements SidraviaIpcClient {
  _PreviewClient(this.scenario);

  final PreviewScenario scenario;

  @override
  Future<DaemonStatus> daemonStatus() async => _daemon;

  @override
  Future<List<InstitutionProfile>> profileList() async => const [_profile];

  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      switch (scenario) {
        PreviewScenario.noConfiguration => const [],
        PreviewScenario.multipleObjects => const [
          _configuration,
          _otherConfiguration,
        ],
        _ => const [_configuration],
      };

  @override
  Future<List<SessionSummary>> sessionList() async => switch (scenario) {
    PreviewScenario.authenticated => const [_authenticatedSession],
    PreviewScenario.blocked => const [_blockedSession],
    PreviewScenario.multipleObjects => const [
      _authenticatedSession,
      _otherSession,
    ],
    _ => const [],
  };

  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async => _configuration;

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  }) async => _configuration;

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async => _configuration;

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async => _authenticatedSession;

  @override
  Future<SessionSummary> sessionStop(String sessionId) async =>
      _suspendedSession;

  @override
  Future<SessionSummary> sessionEnsureRunning(String sessionId) async =>
      _authenticatedSession;

  @override
  Future<SessionSummary> sessionRestart(String sessionId) async =>
      _authenticatedSession;

  @override
  Future<void> close() async {}
}

const _daemon = DaemonStatus(
  productVersion: 'preview-only',
  buildId: 'preview-only',
  pid: 4711,
  status: 'running',
  mode: 'desktop',
  desktopOwnerPid: 4700,
);

const _profile = InstitutionProfile(
  id: 'preview-university',
  displayName: '预览大学',
  protocolId: 'preview-protocol',
);

const _configuration = ConfigurationSummary(
  id: 'cfg-preview-primary',
  displayName: '预览校园网络',
  institutionProfileId: 'preview-university',
  institutionDisplayName: '预览大学',
  authenticationProtocolId: 'preview-protocol',
  username: 'preview.student',
  credentialStored: true,
  storageProtection: 'protected',
  autoLogin: true,
  autoReconnect: true,
);

const _otherConfiguration = ConfigurationSummary(
  id: 'cfg-preview-secondary',
  displayName: '预览备用网络',
  institutionProfileId: 'preview-university',
  institutionDisplayName: '预览大学',
  authenticationProtocolId: 'preview-protocol',
  username: 'preview.other',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);

const _authenticatedSession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'authenticated',
  intent: 'maintain_authentication',
  configurationId: 'cfg-preview-primary',
);

const _blockedSession = SessionSummary(
  id: 'session-preview-blocked',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'blocked_by_error',
  intent: 'maintain_authentication',
  configurationId: 'cfg-preview-primary',
  stateReason: SessionStateReason(
    code: 'preview_failure',
    description: '虚构预览错误',
  ),
  lastAuthenticationFailure: SessionAuthenticationFailure(
    code: 'preview_failure',
    description: '虚构预览错误',
    handlingRecommendation: 'restart_explicitly',
  ),
);

const _suspendedSession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'suspended',
  intent: 'suspend_authentication',
  configurationId: 'cfg-preview-primary',
);

const _otherSession = SessionSummary(
  id: 'session-preview-secondary',
  displayName: '预览备用网络',
  accountName: 'preview.other',
  state: 'suspended',
  intent: 'suspend_authentication',
  configurationId: 'cfg-preview-secondary',
);

const _previewToken =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
