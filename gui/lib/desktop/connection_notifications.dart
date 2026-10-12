import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

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

/// Owns the timer, pending work and presentation feedback for connection notices.
class ConnectionNotificationController extends ChangeNotifier {
  ConnectionNotificationController({
    required this.preferences,
    required this.desktop,
    required this.notifications,
    required DateTime startedAt,
    required this.onOpenDetails,
    this.namespace = '',
    DateTime Function()? clock,
  }) : policy = ConnectionNotificationPolicy(startedAt: startedAt),
       _clock = clock ?? DateTime.now;
  final GuiPreferences preferences;
  final DesktopPresence desktop;
  final SystemNotificationSlot notifications;
  final ConnectionNotificationPolicy policy;
  final String namespace;
  final Future<void> Function() onOpenDetails;
  final DateTime Function() _clock;
  final Set<Future<void>> _work = {};
  Timer? _timer;
  GuiCapabilities? _latest;
  String? _identity, _token, _feedback, _stateSignature;
  int _generation = 0;
  bool _ready = false, _closed = false, _disposed = false;
  GuiNotificationMode? _lastMode;
  Future<void>? _starting, _closing;
  String? get feedback => _feedback;
  Future<void> start() => _starting ??= _start();
  Future<void> _start() async {
    await preferences.initialize();
    if (_closed) return;
    _lastMode = preferences.notificationMode;
    preferences.addListener(_preferencesChanged);
    notifications.onClicked = _clicked;
    notifications.onFailed = _failed;
    _ready = true;
    _observe();
  }

  void updateCapabilities(GuiCapabilities capabilities) {
    if (_closed) return;
    _latest = capabilities;
    if (_ready) _observe();
  }

  void _observe() {
    final caps = _latest;
    final session = caps?.retainedSession;
    final identity =
        caps?.isReady == true &&
            caps!.snapshot!.daemon.status == 'running' &&
            session != null
        ? '${caps.snapshot!.daemon.pid}:${session.id}'
        : null;
    if (_identity != identity) {
      _identity = identity;
      _stateSignature = null;
      policy.reset();
      _timer?.cancel();
      _timer = null;
      ++_generation;
      _token = null;
      _track(notifications.dismiss());
    }
    if (identity == null || session == null || !notifications.enabled) return;
    final signature =
        '${session.state.wireValue}:${session.intent.wireValue}:${session.authenticationEstablishedAt}:${session.lastAuthenticationFailure?.code}:${session.stateReason?.code}';
    if (_stateSignature != signature) {
      _stateSignature = signature;
      ++_generation;
      _token = null;
      _track(notifications.dismiss());
    }
    final notices = policy.observe(
      daemonPid: caps!.snapshot!.daemon.pid,
      session: session,
      now: _clock(),
    );
    if (session.intent != SessionIntent.maintainAuthentication ||
        session.state == SessionState.stopping) {
      ++_generation;
      _token = null;
      _track(notifications.dismiss());
    }
    for (final notice in notices) {
      _track(_send(notice, _generation));
    }
    _schedule();
  }

  void _schedule() {
    _timer?.cancel();
    _timer = null;
    final deadline = policy.nextDeadline;
    if (_closed || deadline == null) return;
    final delay = deadline.difference(_clock());
    _timer = Timer(delay.isNegative ? Duration.zero : delay, () {
      if (_closed) return;
      for (final notice in policy.due(_clock())) {
        _track(_send(notice, _generation));
      }
      _schedule();
    });
  }

  void _preferencesChanged() {
    if (_closed || _lastMode == preferences.notificationMode) return;
    _lastMode = preferences.notificationMode;
    ++_generation;
    _token = null;
    _setFeedback(null);
    _track(notifications.dismiss());
    policy.due(
      _clock(),
    ); // Consume elapsed deadlines, never replay them on a level change.
    _schedule();
  }

  void reportTrayUnavailable() {
    if (!_closed && _ready) {
      _track(
        _send(
          const ConnectionNotice(
            ConnectionNoticeKind.error,
            '托盘界面暂不可用',
            '点击打开主界面',
          ),
          _generation,
        ),
      );
    }
  }

  Future<void> _send(ConnectionNotice notice, int generation) async {
    if (_closed || !notice.allowed(preferences.notificationMode)) return;
    final visible = await desktop.refreshNotificationVisibility(
      context: _latest?.snapshot,
    );
    if (_closed ||
        generation != _generation ||
        !notice.allowed(preferences.notificationMode) ||
        visible == null ||
        notice.backgroundOnly && visible) {
      return;
    }
    final random = Random.secure();
    final token = List.generate(
      16,
      (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'),
    ).join();
    _token = token;
    _setFeedback(null);
    final result = await notifications.show(
      token: token,
      namespace: namespace,
      title: notice.title,
      body: notice.body,
    );
    if (!_closed &&
        generation == _generation &&
        _token == token &&
        result != SystemNotificationResult.submitted) {
      _token = null;
      _failureFeedback();
    }
  }

  void _failureFeedback() {
    final code = notifications.lastFailureCode;
    _setFeedback('系统通知发送失败，可在主界面查看状态${code == null ? '' : '（错误码 $code）'}');
  }

  void _clicked(String token) {
    if (_closed || _token != token) return;
    _token = null;
    _track(onOpenDetails());
  }

  void _failed(String token) {
    if (_closed || _token != token) return;
    _token = null;
    _failureFeedback();
  }

  void _setFeedback(String? message) {
    if (_closed || _feedback == message) return;
    _feedback = message;
    notifyListeners();
  }

  void _track(Future<void> future) {
    late final Future<void> work;
    work = future
        .then<void>(
          (_) {},
          onError: (Object _, StackTrace _) {
            if (!_closed) _setFeedback('系统通知暂不可用，可在主界面查看状态');
          },
        )
        .whenComplete(() => _work.remove(work));
    _work.add(work);
  }

  Future<void> close() => _closing ??= _close();
  Future<void> _close() async {
    _closed = true;
    ++_generation;
    _token = null;
    _timer?.cancel();
    _timer = null;
    policy.reset();
    preferences.removeListener(_preferencesChanged);
    notifications.onClicked = null;
    notifications.onFailed = null;
    await notifications.dispose();
    await Future.wait(_work.toList());
    await _starting;
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    unawaited(close());
    super.dispose();
  }
}
