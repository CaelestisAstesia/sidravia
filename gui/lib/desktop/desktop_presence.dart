import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
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
  Future<bool?>? _notificationVisibility;
  Object? _notificationVisibilityContext;
  VoidCallback? onTrayUnavailable;
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

  /// Notification consumers share one fresh observation, so their queries do not invalidate each other.
  Future<bool?> refreshNotificationVisibility({Object? context}) {
    final current = _notificationVisibility;
    if (current != null && identical(_notificationVisibilityContext, context)) {
      return current;
    }
    final future = refreshMainWindowVisibility();
    _notificationVisibility = future;
    _notificationVisibilityContext = context;
    unawaited(
      future.whenComplete(() {
        if (identical(_notificationVisibility, future)) {
          _notificationVisibility = null;
        }
      }),
    );
    return future;
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
    onTrayUnavailable = null;
    super.dispose();
  }

  Future<Object?> _onNativeCall(MethodCall call) async {
    if (call.method == 'trayUnavailable' && !_disposed) {
      _feedback = '托盘界面暂不可用，可双击图标打开主界面';
      notifyListeners();
      onTrayUnavailable?.call();
    } else if (call.method == 'exitRequested') {
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
