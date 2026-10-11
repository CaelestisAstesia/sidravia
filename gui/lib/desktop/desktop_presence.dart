import 'dart:async';
import 'dart:convert';

import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:flutter/services.dart';
import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';

const desktopPresenceChannelName = 'sidravia/desktop';

enum DesktopPresenceDisposition { primary, activatedExisting }

/// Validates the advanced runtime namespace at the native shell boundary.
/// Null and empty input select the byte-compatible production namespace.
String? validateDesktopNamespace(String? value) {
  if (value == null || value.isEmpty) return '';
  if (value.length > 64 ||
      !RegExp(r'^[A-Za-z0-9._-]+$').hasMatch(value) ||
      value.startsWith('.') ||
      value.endsWith('.') ||
      value.contains('..')) {
    return null;
  }
  final folded = value.toLowerCase();
  if (folded == 'production' || folded == 'default' || folded == 'prod') {
    return null;
  }
  return value;
}

class DesktopPresence extends ChangeNotifier {
  DesktopPresence({MethodChannel? channel, this._namespace = ''})
    : _channel = channel ?? const MethodChannel(desktopPresenceChannelName) {
    _channel!.setMethodCallHandler(_onNativeCall);
  }

  DesktopPresence.disabled() : _channel = null, _namespace = '';

  final MethodChannel? _channel;
  final String _namespace;
  final DesktopNotificationPolicy _policy = DesktopNotificationPolicy();
  bool _operational = true, _initialized = false, _disposed = false;
  bool? _isMainWindowVisible;
  int _visibilityCallbackRevision = 0, _visibilityQueryRevision = 0;
  String? _feedback;
  String? get feedback => _feedback;
  bool? get isMainWindowVisible => _isMainWindowVisible;
  Future<void> Function()? onExitRequested;
  Future<void> Function(TrayIntent intent)? onTrayAction;
  TrayPopoverData? _trayPresentation;

  bool get enabled => _channel != null && _operational && _initialized;

  /// Asks the native runner whether this process is the primary desktop
  /// presence or an already-activating second instance. Any channel failure is
  /// treated as primary so desktop presence failure never blocks the
  /// bootstrap or authentication path.
  Future<DesktopPresenceDisposition> initialize() async {
    final channel = _channel;
    if (channel == null || _disposed) {
      return DesktopPresenceDisposition.primary;
    }
    String? value;
    try {
      value = await channel.invokeMethod<String>('initialize', _namespace);
    } on Object {
      if (!_disposed) _operational = false;
      return DesktopPresenceDisposition.primary;
    }
    if (_disposed) return DesktopPresenceDisposition.primary;
    if (value == 'activatedExisting') {
      _initialized = false;
      return DesktopPresenceDisposition.activatedExisting;
    }
    _initialized = value == 'primary';
    if (_initialized) await refreshMainWindowVisibility();
    return DesktopPresenceDisposition.primary;
  }

  /// Refreshes the native observation without changing desktop availability.
  /// A newer callback (including a duplicate) wins over the requested sample.
  Future<bool?> refreshMainWindowVisibility() async {
    final channel = _channel;
    if (_disposed || !enabled || channel == null) return null;
    final queryRevision = ++_visibilityQueryRevision;
    final callbackRevision = _visibilityCallbackRevision;
    Object? result;
    try {
      result = await channel
          .invokeMethod<Object?>('getVisibility')
          .timeout(const Duration(seconds: 2));
    } on Object {
      // Query errors are observational and never replace command feedback.
    }
    if (_disposed || !enabled) return null;
    if (callbackRevision != _visibilityCallbackRevision) {
      return _isMainWindowVisible;
    }
    if (queryRevision != _visibilityQueryRevision || result is! bool) {
      return null;
    }
    _setMainWindowVisibility(result);
    return _isMainWindowVisible;
  }

  void _setMainWindowVisibility(bool visible) {
    if (_disposed || _isMainWindowVisible == visible) return;
    _isMainWindowVisible = visible;
    notifyListeners();
  }

  Future<bool> configurePreferences({
    required GuiCloseBehavior closeBehavior,
    required bool startHidden,
  }) async {
    if (!enabled) return false;
    try {
      await _channel!.invokeMethod<void>('configurePreferences', {
        'closeToTray': closeBehavior == GuiCloseBehavior.hideToTray,
        'startHidden': startHidden,
      });
      _feedback = null;
      if (!_disposed) notifyListeners();
      return true;
    } on Object {
      _feedback = '桌面行为未能应用；当前关闭行为保持之前的设置。';
      if (!_disposed) notifyListeners();
      return false;
    }
  }

  Future<bool> beginExit() async {
    if (!enabled) return false;
    try {
      await _channel!.invokeMethod<void>('beginExit');
      return true;
    } on Object {
      _feedback = '无法开始退出，连接保持不变，请重试。';
      if (!_disposed) notifyListeners();
      return false;
    }
  }

  Future<void> publishTray(TrayPopoverData data) async {
    if (_disposed ||
        !enabled ||
        (_trayPresentation != null &&
            jsonEncode(_trayPresentation!.toMap()) ==
                jsonEncode(data.toMap()))) {
      return;
    }
    _trayPresentation = data;
    try {
      await _channel!.invokeMethod<void>('publishTray', data.toMap());
    } on Object {
      // Tray rendering is observational and cannot block the main window.
    }
  }

  Future<void> restoreMain() async {
    if (!enabled || _disposed) return;
    try {
      await _channel!.invokeMethod<void>('restoreMain');
    } on Object {
      _feedback = '无法显示主界面，请再次点击托盘图标。';
      if (!_disposed) notifyListeners();
    }
  }

  Future<void> cancelExit() async {
    try {
      await _channel?.invokeMethod<void>('cancelExit');
    } on Object {
      _feedback = '无法取消原生退出请求，请重试。';
      if (!_disposed) notifyListeners();
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

  @override
  void dispose() {
    _disposed = true;
    _channel?.setMethodCallHandler(null);
    onExitRequested = null;
    onTrayAction = null;
    super.dispose();
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
    } else if (call.method == 'trayAction') {
      final intent = TrayIntent.fromMap(call.arguments);
      if (!_disposed && intent != null) await onTrayAction?.call(intent);
    } else if (call.method == 'visibilityChanged') {
      final visible = call.arguments;
      if (!_disposed && enabled && visible is bool) {
        // Even a duplicate callback supersedes an in-flight initial query.
        _visibilityCallbackRevision++;
        _setMainWindowVisibility(visible);
      }
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
/// A session is identified by daemon PID and SessionID; only revisions newer than the
/// last considered revision can trigger a transition. Starting already blocked
/// never emits a startup notification.
class DesktopNotificationPolicy {
  final Map<String, _SessionDesktopState> _sessions = {};
  int? _daemonPid;

  DesktopNotificationDecision? evaluate(GuiSnapshot snapshot) {
    if (_daemonPid != snapshot.daemon.pid) {
      _sessions.clear();
      _daemonPid = snapshot.daemon.pid;
    }
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
    final nowBlocked = session.state == SessionState.blockedByError;
    state.blockedAtLastSeen = nowBlocked;

    if (!state.everSeen) {
      state.everSeen = true;
      return null;
    }

    if (nowBlocked) {
      if (state.notifiedBlockedForEpisode) return null;
      if (wasBlockedAtPrevious) return null;
      state.notifiedBlockedForEpisode = true;
      return DesktopNotificationDecision(
        title: _blockedTitle,
        body: _blockedBody(session),
      );
    }

    if (state.notifiedBlockedForEpisode) {
      state.notifiedBlockedForEpisode = false;
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
  BigInt lastRevision = -BigInt.one;
  bool everSeen = false;
  bool blockedAtLastSeen = false;
  bool notifiedBlockedForEpisode = false;
}
