import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';
import 'package:sidravia_gui/desktop/connection_notification_policy.dart';

export 'package:sidravia_gui/desktop/connection_notification_policy.dart'
    show ConnectionNotice, ConnectionNoticeKind, ConnectionNotificationPolicy;

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
