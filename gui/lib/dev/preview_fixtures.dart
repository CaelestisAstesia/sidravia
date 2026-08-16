import 'dart:async';
import 'dart:convert';

import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

enum PreviewScenario {
  bootstrapping('正在启动'),
  failed('服务不可用'),
  unsupported('平台不支持'),
  daemonUnavailable('服务未运行'),
  noProfile('无学校配置'),
  noConfiguration('尚未配置'),
  noSession('尚无会话'),
  disconnected('未连接'),
  authenticating('正在认证'),
  authenticated('已认证'),
  waitingForNetwork('等待网络'),
  waitingBeforeRetry('等待重试'),
  blocked('认证错误'),
  stopping('正在注销'),
  unknown('未知状态'),
  ambiguousSessions('会话关系不明确'),
  multipleObjects('多个对象');

  const PreviewScenario(this.label);
  final String label;
}

GuiController createPreviewController(PreviewScenario scenario) {
  final bootstrapper = switch (scenario) {
    PreviewScenario.bootstrapping => const _PendingBootstrapper(),
    PreviewScenario.failed => const _FailedBootstrapper(
      GuiBootstrapFailure.failed,
    ),
    PreviewScenario.unsupported => const _FailedBootstrapper(
      GuiBootstrapFailure.unsupportedPlatform,
    ),
    _ => const _PreviewBootstrapper(),
  };
  return GuiController(
    bootstrapper: bootstrapper,
    connector: (_) async => _PreviewClient(scenario),
    pollDelay: const Duration(days: 1),
  );
}

class _FailedBootstrapper implements GuiBootstrapper {
  const _FailedBootstrapper(this.failure);

  final GuiBootstrapFailure failure;

  @override
  Future<GuiBootstrapResult> bootstrap() async =>
      GuiBootstrapResult.failure(failure);
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
  Future<DaemonStatus> daemonStatus() async =>
      scenario == PreviewScenario.daemonUnavailable ? _stoppedDaemon : _daemon;

  @override
  Future<List<InstitutionProfile>> profileList() async =>
      scenario == PreviewScenario.noProfile ? const [] : const [_profile];

  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      switch (scenario) {
        PreviewScenario.noProfile => const [],
        PreviewScenario.noConfiguration => const [],
        PreviewScenario.multipleObjects => const [
          _configuration,
          _otherConfiguration,
        ],
        _ => const [_configuration],
      };

  @override
  Future<List<SessionSummary>> sessionList() async => switch (scenario) {
    PreviewScenario.disconnected => const [_suspendedSession],
    PreviewScenario.authenticating => const [_authenticatingSession],
    PreviewScenario.authenticated => const [_authenticatedSession],
    PreviewScenario.waitingForNetwork => const [_waitingForNetworkSession],
    PreviewScenario.waitingBeforeRetry => const [_waitingBeforeRetrySession],
    PreviewScenario.blocked => const [_blockedSession],
    PreviewScenario.stopping => const [_stoppingSession],
    PreviewScenario.unknown => const [_unknownSession],
    PreviewScenario.ambiguousSessions => const [
      _authenticatedSession,
      _suspendedSession,
    ],
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

const _stoppedDaemon = DaemonStatus(
  productVersion: 'preview-only',
  buildId: 'preview-only',
  pid: 4711,
  status: 'stopped',
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

const _authenticatingSession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'authenticating',
  intent: 'maintain_authentication',
  configurationId: 'cfg-preview-primary',
);

const _waitingForNetworkSession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'waiting_for_network',
  intent: 'maintain_authentication',
  configurationId: 'cfg-preview-primary',
);

const _waitingBeforeRetrySession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'waiting_before_retry',
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

const _stoppingSession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'stopping',
  intent: 'suspend_authentication',
  configurationId: 'cfg-preview-primary',
);

const _unknownSession = SessionSummary(
  id: 'session-preview-primary',
  displayName: '预览校园网络',
  accountName: 'preview.student',
  state: 'preview_unknown',
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

enum PreviewAnnouncementScenario {
  empty('无公告'),
  info('普通公告'),
  maintenance('维护公告'),
  critical('紧急公告');

  const PreviewAnnouncementScenario(this.label);
  final String label;
}

AnnouncementController createPreviewAnnouncementController(
  PreviewAnnouncementScenario scenario,
) {
  return AnnouncementController(
    endpoint: Uri.parse('https://preview.invalid/announcements.json'),
    store: MemoryAnnouncementStore(_previewAnnouncementCache(scenario)),
  );
}

AnnouncementCache _previewAnnouncementCache(
  PreviewAnnouncementScenario scenario,
) {
  final now = DateTime.now().toUtc();
  final items = switch (scenario) {
    PreviewAnnouncementScenario.empty => const <Map<String, Object?>>[],
    PreviewAnnouncementScenario.info => [
      _previewAnnouncementItem(
        'preview-info',
        'info',
        '版本更新说明',
        '这是一条虚构的普通公告，只在公告入口中呈现。',
        now,
      ),
    ],
    PreviewAnnouncementScenario.maintenance => [
      _previewAnnouncementItem(
        'preview-maintenance',
        'maintenance',
        '校园网维护',
        '虚构的维护窗口：认证可能在此期间短暂中断。',
        now,
      ),
    ],
    PreviewAnnouncementScenario.critical => [
      _previewAnnouncementItem(
        'preview-critical',
        'critical',
        '认证服务故障',
        '虚构的紧急公告：认证服务暂时不可用，请稍后再试。',
        now,
      ),
    ],
  };
  return AnnouncementCache(
    feedJson: jsonEncode({
      'schema': 1,
      'generatedAt': now.toIso8601String(),
      'items': items,
    }),
    lastSuccessUtc: now,
  );
}

Map<String, Object?> _previewAnnouncementItem(
  String id,
  String level,
  String title,
  String body,
  DateTime now,
) => {
  'id': id,
  'revision': 1,
  'level': level,
  'title': title,
  'body': body,
  'publishedAt': now.subtract(const Duration(hours: 3)).toIso8601String(),
  'startsAt': now.subtract(const Duration(hours: 2)).toIso8601String(),
  'expiresAt': now.add(const Duration(days: 5)).toIso8601String(),
};
