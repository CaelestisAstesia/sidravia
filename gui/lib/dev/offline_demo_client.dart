import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';

/// Memory-only replacement at the existing typed client boundary.
/// No process, filesystem, socket, HTTP client or credential store is used.
class OfflineDemoClient extends ChangeNotifier implements SidraviaIpcClient {
  OfflineDemoClient({DateTime? now}) {
    _session = _newSession(
      'authenticated',
      establishedAt: (now ?? DateTime.now()).subtract(
        const Duration(hours: 2, minutes: 18),
      ),
    );
  }

  static const profile = InstitutionProfile(
    id: 'jlu',
    displayName: '吉林大学',
    protocolId: 'drcom-5.2.0-d',
  );
  ConfigurationSummary? _configuration = const ConfigurationSummary(
    id: 'demo-config',
    displayName: '吉林大学',
    institutionProfileId: 'jlu',
    institutionDisplayName: '吉林大学',
    authenticationProtocolId: 'drcom-5.2.0-d',
    username: 'student01',
    credentialStored: true,
    storageProtection: 'protected',
  );
  SessionSummary? _session;
  int _revision = 0;
  int _sessionSerial = 0;
  String _feedback = '模拟已连接；没有连接真实校园网';
  final List<String> _operations = [];
  bool _closed = false;

  String get feedback => _feedback;
  List<String> get operations => List.unmodifiable(_operations);
  bool get closed => _closed;

  void selectScenario(String scenario) {
    if (scenario == 'empty') {
      _configuration = null;
      _session = null;
    } else {
      _configuration ??= const ConfigurationSummary(
        id: 'demo-config',
        displayName: '吉林大学',
        institutionProfileId: 'jlu',
        institutionDisplayName: '吉林大学',
        authenticationProtocolId: 'drcom-5.2.0-d',
        username: 'student01',
        credentialStored: true,
        storageProtection: 'protected',
      );
      _session = _newSession(
        scenario,
        establishedAt: scenario == 'authenticated'
            ? DateTime.now().subtract(const Duration(hours: 2, minutes: 18))
            : null,
      );
    }
    _record('demo.scenario', '已切换模拟场景：$scenario；无真实网络操作');
  }

  void _record(String operation, String feedback) {
    _operations.add(operation);
    _feedback = feedback;
    notifyListeners();
  }

  ConfigurationSummary _requireConfiguration(String id) {
    final c = _configuration;
    if (c == null || c.id != id) {
      throw const IpcRequestFailure('configuration_not_found');
    }
    return c;
  }

  SessionSummary _requireSession(String id) {
    final s = _session;
    if (s == null || s.id != id) {
      throw const IpcRequestFailure('session_not_found');
    }
    return s;
  }

  SessionSummary _newSession(String state, {DateTime? establishedAt}) {
    final c = _configuration!;
    return SessionSummary(
      id: _sessionSerial == 0 ? 'demo-session' : 'demo-session-$_sessionSerial',
      configurationId: c.id,
      displayName: c.displayName,
      accountName: c.username,
      state: SessionState.decode(state),
      intent: state == 'suspended'
          ? SessionIntent.suspendAuthentication
          : SessionIntent.maintainAuthentication,
      selectedNetworkBinding: const SessionNetworkBinding(
        interfaceId: 'demo-ethernet',
        displayName: '以太网',
        localIpv4Address: '192.168.1.20',
      ),
      authenticationEstablishedAt: establishedAt,
      lastAuthenticationFailure: state == 'blocked_by_error'
          ? const SessionAuthenticationFailure(
              code: 'credential_invalid',
              description: '服务器拒绝了本次认证',
              handlingRecommendation: '请先核对账号或密码，再重新尝试连接。',
            )
          : null,
      revision: ++_revision,
    );
  }

  ConfigurationSummary _replaceConfiguration({
    String? username,
    String? institutionProfileId,
    bool? credentialStored,
    bool? autoLogin,
    bool? autoReconnect,
  }) {
    final c = _configuration!;
    return _configuration = ConfigurationSummary(
      id: c.id,
      displayName: c.displayName,
      institutionProfileId: institutionProfileId ?? c.institutionProfileId,
      institutionDisplayName: c.institutionDisplayName,
      authenticationProtocolId: c.authenticationProtocolId,
      username: username ?? c.username,
      credentialStored: credentialStored ?? c.credentialStored,
      storageProtection: c.storageProtection,
      autoLogin: autoLogin ?? c.autoLogin,
      autoReconnect: autoReconnect ?? c.autoReconnect,
    );
  }

  @override
  Future<DaemonStatus> daemonStatus() async => const DaemonStatus(
    productVersion: 'offline-demo',
    buildId: 'offline-demo',
    pid: 1,
    status: 'running',
    mode: 'desktop',
    desktopOwnerPid: 1,
  );
  @override
  Future<List<InstitutionProfile>> profileList() async => const [profile];
  @override
  Future<List<ConfigurationSummary>> configurationList() async => [
    ?_configuration,
  ];
  @override
  Future<List<SessionSummary>> sessionList() async => [?_session];

  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async {
    if (_configuration != null) {
      throw const IpcRequestFailure('configuration_conflict');
    }
    if (institutionProfileId != profile.id) {
      throw const IpcRequestFailure('profile_not_found');
    }
    _configuration = ConfigurationSummary(
      id: 'demo-config',
      displayName: profile.displayName,
      institutionProfileId: profile.id,
      institutionDisplayName: profile.displayName,
      authenticationProtocolId: profile.protocolId,
      username: username,
      credentialStored: password.isNotEmpty,
      storageProtection: 'protected',
    );
    _record('configuration.create', '已模拟创建配置；密码内容未保存');
    return _configuration!;
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  }) async {
    final previous = _requireConfiguration(configurationId);
    if (institutionProfileId != profile.id) {
      throw const IpcRequestFailure('profile_not_found');
    }
    final c = _replaceConfiguration(
      username: username,
      institutionProfileId: institutionProfileId,
      credentialStored: password == null ? previous.credentialStored : true,
    );
    if (username != previous.username ||
        institutionProfileId != previous.institutionProfileId ||
        password != null) {
      _session = null;
    }
    _record('configuration.update', '已模拟保存配置；仅保留在本次演示内存中');
    return c;
  }

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async {
    _requireConfiguration(configurationId);
    final c = _replaceConfiguration(credentialStored: password.isNotEmpty);
    _session = null;
    _record('configuration.set_password', '已模拟设置密码；密码内容未保存');
    return c;
  }

  @override
  Future<ConfigurationSummary> configurationSetAutoLogin({
    required String configurationId,
    required bool autoLogin,
  }) async {
    _requireConfiguration(configurationId);
    final c = _replaceConfiguration(autoLogin: autoLogin);
    _record(
      'configuration.set_auto_login',
      '已模拟${autoLogin ? '启用' : '关闭'}自动登录；不会执行真实认证',
    );
    return c;
  }

  @override
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
  }) async {
    final previous = _requireConfiguration(configurationId);
    final c = _replaceConfiguration(autoReconnect: autoReconnect);
    if (previous.autoReconnect != autoReconnect) _session = null;
    _record('configuration.set_auto_reconnect', '已模拟保存自动重连设置；模拟会话按设置变更处理');
    return c;
  }

  @override
  Future<ConfigurationRemoveResult> configurationRemove(
    String configurationId,
  ) async {
    _requireConfiguration(configurationId);
    _configuration = null;
    _session = null;
    _record('configuration.remove', '已模拟删除配置');
    return ConfigurationRemoveResult(
      configurationId: configurationId,
      status: 'removed',
    );
  }

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async {
    _requireConfiguration(configurationId);
    if (_session == null) _sessionSerial++;
    _session = _newSession('authenticated', establishedAt: DateTime.now());
    _record('session.start_configuration', '已模拟连接成功；没有进行真实认证');
    return _session!;
  }

  @override
  Future<SessionSummary> sessionStop(String sessionId) async {
    _requireSession(sessionId);
    _session = _newSession('suspended');
    _record('session.stop', '已模拟断开连接；没有影响真实网络');
    return _session!;
  }

  @override
  Future<SessionSummary> sessionEnsureRunning(String sessionId) async {
    _requireSession(sessionId);
    _session = _newSession('authenticated', establishedAt: DateTime.now());
    _record('session.ensure_running', '已模拟恢复连接；没有进行真实认证');
    return _session!;
  }

  @override
  Future<SessionSummary> sessionRestart(String sessionId) async {
    _requireSession(sessionId);
    _session = _newSession('authenticated', establishedAt: DateTime.now());
    _record('session.restart', '已模拟重新连接；没有进行真实认证');
    return _session!;
  }

  @override
  Future<SessionRemoveResult> sessionRemove(String sessionId) async {
    _requireSession(sessionId);
    _session = null;
    _record('session.remove', '已模拟重置会话');
    return SessionRemoveResult(sessionId: sessionId, status: 'removed');
  }

  @override
  Future<void> close() async {
    _closed = true;
  }
}

class OfflineDemoBootstrap implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://offline.invalid/ipc'),
      token: 'offline-only',
      productVersion: 'offline-demo',
      buildId: 'offline-demo',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );
}

class OfflineAnnouncementFetcher implements AnnouncementFetcher {
  OfflineAnnouncementFetcher(this.now);
  final DateTime now;
  bool active = true;
  int revision = 1;
  bool closed = false;
  static final endpoint = Uri.parse('https://offline.invalid/announcements');
  @override
  Future<AnnouncementFetchResult> fetch({
    String? etag,
    String? lastModified,
  }) async {
    final raw = jsonEncode({
      'schema': 1,
      'generatedAt': now.toIso8601String(),
      'items': [
        if (active)
          {
            'id': 'offline-maintenance',
            'revision': revision,
            'level': 'maintenance',
            'title': revision == 1 ? '校园网维护安排' : '校园网公告更新（模拟）',
            'body': '这是离线演示公告。配置、导航与连接操作仅使用内存模拟数据，不会访问校园网或真实账号。',
            'publishedAt': now.toIso8601String(),
            'startsAt': now.toIso8601String(),
            'expiresAt': now.add(const Duration(days: 365)).toIso8601String(),
          },
      ],
    });
    return AnnouncementFetchResult.updated(
      feed: AnnouncementDocument.decode(raw, endpoint: endpoint),
      rawBody: raw,
    );
  }

  @override
  void close() {
    closed = true;
  }
}
