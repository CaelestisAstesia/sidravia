import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

/// Owns the single strict actionable-target projection shared by Home and
/// Configuration. Neither page guesses Configuration/Session relationships
/// anymore; an actionable target exists only when the state is ready, exactly
/// one Configuration is retained, every observed Session belongs to it and at
/// most one related Session exists. Zero Configuration remains create-only.
enum GuiCapabilityState {
  bootstrapping,
  stale,
  failed,
  unsupported,
  daemonUnavailable,
  createOnly,
  manageable,
  multipleConfigurations,
  ambiguousSessions,
}

class GuiCapabilities {
  const GuiCapabilities({
    required this.state,
    required this.snapshot,
    required this.busy,
  });

  final GuiConnectionState state;
  final GuiSnapshot? snapshot;
  final bool busy;

  bool get isReady => state == GuiConnectionState.ready && snapshot != null;

  GuiCapabilityState get capability {
    if (!isReady) {
      return switch (state) {
        GuiConnectionState.bootstrapping => GuiCapabilityState.bootstrapping,
        GuiConnectionState.stale => GuiCapabilityState.stale,
        GuiConnectionState.failed => GuiCapabilityState.failed,
        GuiConnectionState.unsupported => GuiCapabilityState.unsupported,
        GuiConnectionState.ready => GuiCapabilityState.stale,
      };
    }
    if (snapshot!.daemon.status != 'running') {
      return GuiCapabilityState.daemonUnavailable;
    }
    final configurations = snapshot!.configurations;
    final sessions = snapshot!.sessions;
    if (configurations.isEmpty) {
      return sessions.isEmpty
          ? GuiCapabilityState.createOnly
          : GuiCapabilityState.ambiguousSessions;
    }
    if (configurations.length > 1) {
      return GuiCapabilityState.multipleConfigurations;
    }
    final belongs = sessions.every(
      (session) => session.configurationId == configurations.single.id,
    );
    if (!belongs || sessions.length > 1) {
      return GuiCapabilityState.ambiguousSessions;
    }
    return GuiCapabilityState.manageable;
  }

  bool get canCreate => capability == GuiCapabilityState.createOnly && !busy;

  bool get canManage => capability == GuiCapabilityState.manageable && !busy;

  bool get canEditAutoLogin => canManage;

  bool get canEditAutoReconnect => canManage;

  bool get canDeleteConfiguration => canManage;

  bool get canResetSession => canManage && snapshot!.sessions.isNotEmpty;

  bool matchesConfiguration(String id) => canManage && configuration?.id == id;
  bool matchesSession(String id) => canManage && retainedSession?.id == id;
  bool get canConnect =>
      canManage && retainedSession?.state != SessionState.stopping;
  bool get canStop =>
      canConnect &&
      retainedSession != null &&
      retainedSession!.state != SessionState.suspended;

  String? get settingsDisabledReason {
    if (busy) return '正在处理操作，请稍后修改连接设置。';
    return switch (capability) {
      GuiCapabilityState.manageable => null,
      GuiCapabilityState.unsupported => '当前平台暂不支持连接设置。',
      GuiCapabilityState.bootstrapping ||
      GuiCapabilityState.stale ||
      GuiCapabilityState.failed => '通信尚未就绪，暂时无法修改连接设置。',
      GuiCapabilityState.daemonUnavailable => '核心服务未运行，暂时无法修改连接设置。',
      GuiCapabilityState.createOnly => '请先添加连接配置。',
      GuiCapabilityState.multipleConfigurations =>
        '存在多个连接配置 · 请使用 sidraviactl 管理',
      GuiCapabilityState.ambiguousSessions => '配置或会话关系不明确，暂时无法修改连接设置。',
    };
  }

  String get configurationDescription => switch (capability) {
    GuiCapabilityState.manageable =>
      '${configuration!.institutionDisplayName} · ${configuration!.username}',
    GuiCapabilityState.createOnly => '尚未配置 · 点击添加',
    GuiCapabilityState.multipleConfigurations =>
      '存在多个连接配置 · 请使用 sidraviactl 管理',
    GuiCapabilityState.ambiguousSessions => '会话关系不明确 · 请使用 sidraviactl 管理',
    _ => '配置状态暂不可用',
  };

  ConfigurationSummary? get configuration {
    if (capability != GuiCapabilityState.manageable) return null;
    return snapshot!.configurations.single;
  }

  SessionSummary? get retainedSession {
    if (capability != GuiCapabilityState.manageable ||
        snapshot!.sessions.isEmpty) {
      return null;
    }
    return snapshot!.sessions.single;
  }
}
