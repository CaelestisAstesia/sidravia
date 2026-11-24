import 'package:sidravia_gui/application/preferences/gui_settings.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

enum ConnectionNoticeKind { success, recovery, error, progress, delayed }

class ConnectionNotice {
  const ConnectionNotice(this.kind, this.title, this.body);
  final ConnectionNoticeKind kind;
  final String title, body;
  bool allowed(GuiNotificationMode mode) =>
      mode != GuiNotificationMode.silent &&
      (kind != ConnectionNoticeKind.success || mode == GuiNotificationMode.all);
  bool get backgroundOnly =>
      kind == ConnectionNoticeKind.success ||
      kind == ConnectionNoticeKind.progress;
}

class _Episode {
  _Episode(this.startedAt, this.hadConnected);
  final DateTime startedAt;
  final bool hadConnected;
  bool progressSent = false, delayedSent = false, blockedSent = false;
}

/// Observes facts, never reconnects. One episode survives retries until authentication or an explicit stop.
class ConnectionNotificationPolicy {
  ConnectionNotificationPolicy({required this.startedAt});
  final DateTime startedAt;
  String? _identity;
  BigInt _revision = -BigInt.one;
  DateTime? _established;
  bool _connected = false, _blocked = false;
  _Episode? _episode;
  void reset() {
    _identity = null;
    _revision = -BigInt.one;
    _established = null;
    _connected = false;
    _blocked = false;
    _episode = null;
  }

  DateTime? get nextDeadline {
    final episode = _episode;
    if (episode == null || episode.blockedSent || _blocked) return null;
    if (episode.hadConnected && !episode.progressSent) {
      return episode.startedAt.add(const Duration(seconds: 5));
    }
    return episode.delayedSent
        ? null
        : episode.startedAt.add(const Duration(seconds: 30));
  }

  List<ConnectionNotice> due(DateTime now) {
    final e = _episode;
    if (e == null || _blocked || e.blockedSent) return [];
    final notices = <ConnectionNotice>[];
    if (e.hadConnected &&
        !e.progressSent &&
        !now.isBefore(e.startedAt.add(const Duration(seconds: 5)))) {
      e.progressSent = true;
      notices.add(
        const ConnectionNotice(
          ConnectionNoticeKind.progress,
          '连接已中断',
          '正在尝试重新连接',
        ),
      );
    }
    if (!e.delayedSent &&
        !now.isBefore(e.startedAt.add(const Duration(seconds: 30)))) {
      e.delayedSent = true;
      notices.add(
        ConnectionNotice(
          ConnectionNoticeKind.delayed,
          e.hadConnected ? '暂未恢复连接' : '暂未连接',
          e.hadConnected ? '仍在尝试重连，点击查看详情' : '仍在尝试认证，点击查看详情',
        ),
      );
    }
    // A delayed callback produces the current useful notice, not a burst of obsolete intermediate notices.
    return notices.isEmpty ? notices : [notices.last];
  }

  List<ConnectionNotice> observe({
    required int daemonPid,
    required SessionSummary session,
    required DateTime now,
  }) {
    final identity = '$daemonPid:${session.id}';
    if (_identity != identity) {
      reset();
      _identity = identity;
    }
    if (session.revision <= _revision) return [];
    final initial = _revision < BigInt.zero;
    _revision = session.revision;
    if (session.intent != SessionIntent.maintainAuthentication ||
        session.state == SessionState.stopping) {
      _episode = null;
      _connected = false;
      _blocked = false;
      _established = null;
      return [];
    }
    if (session.state == SessionState.authenticated) {
      final at = session.authenticationEstablishedAt;
      final fresh =
          at != null &&
          (_established == null || !at.isAtSameMomentAs(_established!));
      final e = _episode;
      _episode = null;
      _blocked = false;
      _connected = true;
      _established = at;
      if (e != null && (e.hadConnected || e.blockedSent || e.delayedSent)) {
        return [
          ConnectionNotice(
            ConnectionNoticeKind.recovery,
            e.hadConnected ? '已重新连接' : '已连接',
            e.hadConnected ? '校园网认证已恢复' : '校园网认证成功',
          ),
        ];
      }
      return fresh && (!initial || !at.isBefore(startedAt))
          ? [
              const ConnectionNotice(
                ConnectionNoticeKind.success,
                '已连接',
                '校园网认证成功',
              ),
            ]
          : [];
    }
    final newEvent =
        !initial ||
        (session.updatedAt != null && !session.updatedAt!.isBefore(startedAt));
    if (!newEvent) return [];
    _episode ??= _Episode(now, _connected);
    final e = _episode!;
    final disabled =
        session.stateReason?.code == 'automatic_reconnect_disabled';
    _blocked = session.state == SessionState.blockedByError || disabled;
    if (_blocked && !e.blockedSent) {
      e.blockedSent = true;
      return [
        ConnectionNotice(
          ConnectionNoticeKind.error,
          disabled
              ? '连接已中断'
              : e.hadConnected
              ? '连接需要处理'
              : '认证失败',
          disabled
              ? '自动重连已关闭，点击查看'
              : _failureBody(
                  session.lastAuthenticationFailure?.code ??
                      session.stateReason?.code,
                ),
        ),
      ];
    }
    return [];
  }

  static String _failureBody(String? code) => switch (code) {
    'authentication_rejected' => '认证被服务器拒绝，点击查看处理建议',
    'binding_rejected' => '当前网卡或地址未通过校验，点击查看详情',
    'protocol_response_invalid' => '服务器响应异常，点击查看详情',
    'protocol_incompatible' => '当前认证方式不兼容，点击查看详情',
    'network_binding_unavailable' => '指定网卡或地址不可用，点击查看详情',
    _ => '无法继续认证，点击查看详情',
  };
}
