import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

enum HomeTone { success, warning, error, idle }

enum HomeButtonKind { primary, secondary }

enum GuiConnectionActionKind {
  addConfiguration,
  editConfiguration,
  start,
  reconnect,
  stop,
  retryIpc,
  showDetails,
  showSettings,
}

/// Target identity travels with the rendered intent; a later snapshot must not
/// silently redirect a click to another Configuration or Session.
class GuiConnectionAction {
  const GuiConnectionAction(this.kind, [this.targetId]);
  final GuiConnectionActionKind kind;
  final String? targetId;
}

class ConnectionPresentation {
  const ConnectionPresentation({
    required this.institution,
    required this.username,
    required this.statusTitle,
    required this.statusDetail,
    required this.statusContext,
    required this.glyph,
    required this.tone,
    required this.primaryLabel,
    required this.secondaryLabel,
    required this.primaryAction,
    required this.secondaryAction,
    required this.primaryEnabled,
    required this.secondaryEnabled,
    required this.primaryKind,
    this.issue,
    this.actionError,
    this.networkBinding,
    this.establishedAt,
    this.protocol,
  });
  final String institution,
      username,
      statusTitle,
      statusDetail,
      statusContext,
      glyph;
  final HomeTone tone;
  final String primaryLabel, secondaryLabel;
  final GuiConnectionAction primaryAction, secondaryAction;
  final bool primaryEnabled, secondaryEnabled;
  final HomeButtonKind primaryKind;
  final SessionAuthenticationFailure? issue;
  final String? actionError, protocol;
  final SessionNetworkBinding? networkBinding;
  final DateTime? establishedAt;

  static ConnectionPresentation project(
    GuiCapabilities caps, {
    bool sessionNeedsReset = false,
    String? notice,
    DateTime? now,
  }) {
    final c = caps.configuration;
    final s = caps.retainedSession;
    final previous = caps.snapshot?.configurations;
    final displayed =
        c ??
        (caps.state != GuiConnectionState.ready && previous?.length == 1
            ? previous!.single
            : null);
    final topology = caps.capability;
    var title = '未连接', detail = '准备就绪，可以开始连接', context = '', glyph = '—';
    var tone = HomeTone.idle;
    var primaryLabel = '开始连接', secondaryLabel = '更改配置';
    var primary = GuiConnectionAction(GuiConnectionActionKind.start, c?.id);
    var secondary = GuiConnectionAction(
      GuiConnectionActionKind.editConfiguration,
      c?.id,
    );
    var enabled = caps.canManage && !sessionNeedsReset;
    var secondaryEnabled = true;
    var kind = HomeButtonKind.primary;
    SessionAuthenticationFailure? issue;
    switch (topology) {
      case GuiCapabilityState.bootstrapping:
        title = '正在连接服务';
        detail = '正在初始化连接服务…';
        glyph = '↻';
        tone = HomeTone.warning;
        enabled = false;
      case GuiCapabilityState.failed:
        title = '服务不可用';
        detail = '暂时无法读取连接状态';
        context = '请重试连接服务';
        glyph = '!';
        tone = HomeTone.error;
      case GuiCapabilityState.stale:
        title = '服务失联';
        detail = '暂时无法确认当前连接状态';
        context = '账号信息来自上次成功读取';
        glyph = '!';
        tone = HomeTone.warning;
      case GuiCapabilityState.unsupported:
        title = '平台暂不支持';
        detail = '此平台暂不支持真实认证';
        enabled = false;
      case GuiCapabilityState.daemonUnavailable:
        title = '服务不可用';
        detail = '核心服务未运行';
        enabled = false;
        tone = HomeTone.error;
      case GuiCapabilityState.createOnly:
        title = '尚未配置';
        detail = '添加连接配置后即可开始连接';
        context = '保存机构、账号和密码后即可开始认证';
        glyph = '+';
        primaryLabel = '添加配置';
        secondaryLabel = '连接设置';
        primary = const GuiConnectionAction(
          GuiConnectionActionKind.addConfiguration,
        );
        secondary = const GuiConnectionAction(
          GuiConnectionActionKind.showSettings,
        );
        enabled = caps.canCreate;
      case GuiCapabilityState.multipleConfigurations:
        title = '检测到多个连接配置';
        detail = '当前 GUI 暂不支持选择连接配置';
        context = '请使用 sidraviactl 管理';
        glyph = '!';
        tone = HomeTone.warning;
        enabled = false;
        primaryLabel = '开始连接';
        secondaryLabel = '连接详情';
        secondary = const GuiConnectionAction(
          GuiConnectionActionKind.showDetails,
        );
      case GuiCapabilityState.ambiguousSessions:
        title = '会话关系不明确';
        detail = '检测到当前 GUI 无法管理的会话';
        context = '请使用 sidraviactl 管理';
        glyph = '!';
        tone = HomeTone.warning;
        enabled = false;
        secondaryLabel = '连接详情';
        secondary = const GuiConnectionAction(
          GuiConnectionActionKind.showDetails,
        );
      case GuiCapabilityState.manageable:
        if (s != null) {
          switch (s.state) {
            case SessionState.suspended:
              break;
            case SessionState.authenticated:
              title = '已连接';
              detail = _connectedDetail(s, now ?? DateTime.now());
              context = _binding(s);
              glyph = '✓';
              tone = HomeTone.success;
              primaryLabel = '断开连接';
            case SessionState.authenticating:
              title = '正在认证';
              detail = '正在向校园网提交认证信息…';
              context = '等待认证结果';
              glyph = '↻';
              tone = HomeTone.warning;
              primaryLabel = '取消连接';
            case SessionState.waitingForNetwork:
              title = '等待网络';
              detail = s.stateReason?.code == 'network_binding_unavailable'
                  ? '指定网卡或 IPv4 地址不可用'
                  : '暂未发现可用的校园网络';
              context = s.stateReason?.code == 'network_binding_unavailable'
                  ? '请检查指定网卡和 IPv4；恢复后继续认证'
                  : '检测到可用网络后会继续认证';
              glyph = '!';
              tone = HomeTone.warning;
              primaryLabel = '取消等待';
            case SessionState.waitingBeforeRetry:
              title = '等待重试';
              detail = '认证暂未成功，将自动重试';
              context = '可以立即重试';
              glyph = '↻';
              tone = HomeTone.warning;
              primaryLabel = '立即重试';
              secondaryLabel = '停止重试';
            case SessionState.blockedByError:
              title = '认证失败';
              detail = '本次认证没有完成';
              context = s.lastAuthenticationFailure?.description ?? '';
              glyph = '×';
              tone = HomeTone.error;
              primaryLabel = '重新连接';
              issue = s.lastAuthenticationFailure;
            case SessionState.stopping:
              title = '正在断开';
              detail = '正在结束当前认证会话…';
              glyph = '↻';
              tone = HomeTone.warning;
              primaryLabel = '正在断开';
              enabled = false;
          }
          switch (s.state) {
            case SessionState.suspended:
              break;
            case SessionState.waitingBeforeRetry:
              primary = GuiConnectionAction(
                GuiConnectionActionKind.reconnect,
                s.id,
              );
              secondary = GuiConnectionAction(
                GuiConnectionActionKind.stop,
                s.id,
              );
              secondaryEnabled = caps.canManage && !sessionNeedsReset;
            case SessionState.blockedByError:
              primary = GuiConnectionAction(
                GuiConnectionActionKind.reconnect,
                s.id,
              );
            case SessionState.authenticated ||
                SessionState.authenticating ||
                SessionState.waitingForNetwork:
              primary = GuiConnectionAction(GuiConnectionActionKind.stop, s.id);
              kind = HomeButtonKind.secondary;
              secondaryLabel = '连接详情';
              secondary = const GuiConnectionAction(
                GuiConnectionActionKind.showDetails,
              );
            case SessionState.stopping:
              primary = GuiConnectionAction(GuiConnectionActionKind.stop, s.id);
              kind = HomeButtonKind.secondary;
              secondaryLabel = '连接详情';
              secondary = const GuiConnectionAction(
                GuiConnectionActionKind.showDetails,
              );
          }
        }
    }
    if (caps.state == GuiConnectionState.failed ||
        caps.state == GuiConnectionState.stale) {
      primary = const GuiConnectionAction(GuiConnectionActionKind.retryIpc);
      primaryLabel = '重试连接';
      enabled = !caps.busy;
    }
    String? error = notice;
    if (error == null) {
      if (caps.state != GuiConnectionState.ready) {
        error = switch (caps.state) {
          GuiConnectionState.bootstrapping => '正在初始化连接服务…',
          GuiConnectionState.unsupported => '此平台暂不支持真实认证。',
          GuiConnectionState.stale => '连接服务失联，当前信息可能已过期。',
          _ => '连接服务不可用，请重试。',
        };
      } else if (sessionNeedsReset) {
        error = '请先在连接配置中移除旧会话。';
      } else if (caps.busy) {
        error = '正在处理操作，请稍候。';
      } else if (!enabled && s?.state != SessionState.stopping) {
        error = caps.settingsDisabledReason;
      }
    }
    final unavailableIdentity = switch (topology) {
      GuiCapabilityState.multipleConfigurations => '多个连接配置',
      GuiCapabilityState.ambiguousSessions => '会话关系不明确',
      GuiCapabilityState.createOnly => '尚未配置',
      _ => '连接服务',
    };
    return ConnectionPresentation(
      institution: displayed?.institutionDisplayName ?? unavailableIdentity,
      username:
          displayed?.username ??
          (topology == GuiCapabilityState.createOnly ? '点击添加连接配置' : '配置状态暂不可用'),
      statusTitle: title,
      statusDetail: detail,
      statusContext: context,
      glyph: glyph,
      tone: tone,
      primaryLabel: primaryLabel,
      secondaryLabel: secondaryLabel,
      primaryAction: primary,
      secondaryAction: secondary,
      primaryEnabled: enabled,
      secondaryEnabled: secondaryEnabled,
      primaryKind: kind,
      issue: issue,
      actionError: error,
      protocol: c?.authenticationProtocolId,
      networkBinding:
          s != null &&
              s.state != SessionState.suspended &&
              s.state != SessionState.waitingForNetwork
          ? s.selectedNetworkBinding
          : null,
      establishedAt: s?.state == SessionState.authenticated
          ? s?.authenticationEstablishedAt
          : null,
    );
  }

  static String _connectedDetail(SessionSummary s, DateTime now) {
    final at = s.authenticationEstablishedAt;
    if (at == null) return '认证成功';
    final elapsed = now.difference(at);
    if (elapsed.inMinutes < 1) return '认证成功 · 刚刚连接';
    return '认证成功 · 已连接 ${elapsed.inHours} 小时 ${elapsed.inMinutes % 60} 分钟';
  }

  static String _binding(SessionSummary s) {
    final b = s.selectedNetworkBinding;
    if (b == null) return '网络适配器：暂不可用';
    return b.localIpv4Address.isEmpty
        ? b.displayName
        : '${b.displayName} · ${b.localIpv4Address}';
  }
}
