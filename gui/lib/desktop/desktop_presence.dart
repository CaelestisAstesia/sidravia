import 'package:flutter/services.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const desktopPresenceChannelName = 'sidravia/desktop';

enum DesktopPresenceDisposition { primary, activatedExisting }

class DesktopPresence {
  DesktopPresence({MethodChannel? channel})
    : _channel = channel ?? const MethodChannel(desktopPresenceChannelName) {
    _channel!.setMethodCallHandler(_onNativeCall);
  }

  DesktopPresence.disabled() : _channel = null;

  final MethodChannel? _channel;
  final DesktopNotificationPolicy _policy = DesktopNotificationPolicy();
  bool _operational = true;
  Future<void> Function()? onExitRequested;

  bool get enabled => _channel != null && _operational;

  /// Asks the native runner whether this process is the primary desktop
  /// presence or an already-activating second instance. Any channel failure is
  /// treated as primary so desktop presence failure never blocks the
  /// bootstrap or authentication path.
  Future<DesktopPresenceDisposition> initialize() async {
    final channel = _channel;
    if (channel == null) return DesktopPresenceDisposition.primary;
    try {
      final value = await channel.invokeMethod<String>('initialize');
      if (value == 'activatedExisting') {
        return DesktopPresenceDisposition.activatedExisting;
      }
      return DesktopPresenceDisposition.primary;
    } on Object {
      _operational = false;
      return DesktopPresenceDisposition.primary;
    }
  }

  /// Evaluate one complete snapshot against the blocked/recovered policy.
  Future<void> considerSnapshot(GuiSnapshot snapshot) async {
    final decision = _policy.evaluate(snapshot);
    if (decision == null) return;
    await _notify(title: decision.title, body: decision.body);
  }

  /// Tell native code to remove the tray icon and permit window destruction.
  Future<void> destroy() async {
    final channel = _channel;
    if (channel == null || !_operational) return;
    try {
      await channel.invokeMethod<void>('destroy');
    } on Object {
      // Process destruction remains the native fallback even when this
      // channel call fails.
    }
  }

  void dispose() {
    _channel?.setMethodCallHandler(null);
    onExitRequested = null;
  }

  Future<void> _notify({required String title, required String body}) async {
    final channel = _channel;
    if (channel == null || !_operational) return;
    try {
      await channel.invokeMethod<void>('notify', {
        'title': title,
        'body': body,
      });
    } on Object {
      // Notification failure is non-fatal and isolated from the app flow.
    }
  }

  Future<Object?> _onNativeCall(MethodCall call) async {
    if (call.method == 'exitRequested') {
      await onExitRequested?.call();
    }
    return null;
  }
}

class DesktopNotificationDecision {
  const DesktopNotificationDecision({required this.title, required this.body});
  final String title;
  final String body;
}

const _blockedTitle = '校园网需要处理';
const _blockedFallbackBody = '请打开 Sidravia 查看。';
const _recoveryTitle = '校园网状态已恢复';
const _recoveryBody = '连接状态已更新。';

/// Derives at most one blocked/recovered notification per complete snapshot.
///
/// A session is identified by its SessionID and only revisions newer than the
/// last considered revision can trigger a transition. Starting already blocked
/// never emits a startup notification.
class DesktopNotificationPolicy {
  final Map<String, _SessionDesktopState> _sessions = {};

  DesktopNotificationDecision? evaluate(GuiSnapshot snapshot) {
    for (final session in snapshot.sessions) {
      final decision = _evaluateSession(session);
      if (decision != null) return decision;
    }
    return null;
  }

  DesktopNotificationDecision? _evaluateSession(SessionSummary session) {
    final state = _sessions.putIfAbsent(session.id, _SessionDesktopState.new);
    if (session.revision <= state.lastRevision) return null;
    state.lastRevision = session.revision;
    final wasBlockedAtPrevious = state.blockedAtLastSeen;
    final nowBlocked = session.state == 'blocked_by_error';
    state.blockedAtLastSeen = nowBlocked;

    if (!state.everSeen) {
      state.everSeen = true;
      if (nowBlocked) {
        // An already-blocked initial snapshot is not a new human action.
        state.notifiedBlockedForEpisode = true;
      }
      return null;
    }

    if (nowBlocked) {
      if (state.notifiedBlockedForEpisode) return null;
      if (wasBlockedAtPrevious) return null;
      state.notifiedBlockedForEpisode = true;
      state.notifiedRecoveryForPrevious = false;
      return DesktopNotificationDecision(
        title: _blockedTitle,
        body: _blockedBody(session),
      );
    }

    if (state.notifiedBlockedForEpisode) {
      state.notifiedBlockedForEpisode = false;
      state.notifiedRecoveryForPrevious = true;
      return const DesktopNotificationDecision(
        title: _recoveryTitle,
        body: _recoveryBody,
      );
    }
    return null;
  }

  String _blockedBody(SessionSummary session) {
    final description =
        session.stateReason?.description ??
        session.lastAuthenticationFailure?.description;
    if (description == null || description.isEmpty) {
      return _blockedFallbackBody;
    }
    return description;
  }
}

class _SessionDesktopState {
  int lastRevision = -1;
  bool everSeen = false;
  bool blockedAtLastSeen = false;
  bool notifiedBlockedForEpisode = false;
  bool notifiedRecoveryForPrevious = false;
}
