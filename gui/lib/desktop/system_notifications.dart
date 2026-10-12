import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';

const systemNotificationsChannelName = 'sidravia/system-notifications';

enum SystemNotificationCategory { announcement, connection }

enum SystemNotificationResult { submitted, unsupported, failed }

/// One channel owner; category slots keep replacement and callback identities separate.
class SystemNotifications {
  SystemNotifications({MethodChannel? channel, TargetPlatform? platform})
    : _channel =
          !kIsWeb &&
              (platform ?? defaultTargetPlatform) == TargetPlatform.windows
          ? channel ?? const MethodChannel(systemNotificationsChannelName)
          : null {
    _channel?.setMethodCallHandler(_onNativeCall);
  }
  SystemNotifications.disabled() : _channel = null;
  final MethodChannel? _channel;
  final Map<SystemNotificationCategory, SystemNotificationSlot> _slots = {};
  bool _disposed = false;
  Future<void>? _closing;
  SystemNotificationSlot slot(SystemNotificationCategory category) => _slots
      .putIfAbsent(category, () => SystemNotificationSlot._(this, category));

  Future<Object?> _onNativeCall(MethodCall call) async {
    if (_disposed || !const {'clicked', 'failed'}.contains(call.method)) {
      return null;
    }
    final payload = call.arguments;
    if (payload is! Map ||
        !payload.containsKey('category') ||
        !payload.containsKey('token') ||
        payload.keys.any(
          (key) =>
              key != 'category' &&
              key != 'token' &&
              !(call.method == 'failed' && key == 'error_code'),
        )) {
      return null;
    }
    final category = SystemNotificationCategory.values
        .where((c) => c.name == payload['category'])
        .firstOrNull;
    final token = payload['token'];
    if (category == null || token is! String || !_validToken(token)) {
      return null;
    }
    _slots[category]?._terminal(
      call.method,
      token,
      _failureCode(payload['error_code']),
    );
    return null;
  }

  Future<void> dispose() => _closing ??= _dispose();
  Future<void> _dispose() async {
    _disposed = true;
    for (final slot in _slots.values) {
      slot._disposed = true;
      slot._invalidate();
      slot.onClicked = null;
      slot.onFailed = null;
    }
    _channel?.setMethodCallHandler(null);
    try {
      await _channel
          ?.invokeMethod<void>('dispose')
          .timeout(const Duration(seconds: 2));
    } on Object {
      /* Native process teardown remains the final cleanup owner. */
    }
  }
}

class SystemNotificationSlot {
  SystemNotificationSlot._(this._owner, this.category);
  final SystemNotifications _owner;
  final SystemNotificationCategory category;
  int _generation = 0;
  bool _disposed = false;
  String? _pendingToken, _lastFailureCode;
  Future<void>? _closing;
  bool get enabled =>
      !_disposed && !_owner._disposed && _owner._channel != null;
  String? get lastFailureCode => _lastFailureCode;
  void Function(String token)? onClicked, onFailed;
  void _invalidate() {
    ++_generation;
    _pendingToken = null;
    _lastFailureCode = null;
  }

  Future<SystemNotificationResult> show({
    required String token,
    required String title,
    required String body,
    String namespace = '',
  }) async {
    if (_disposed || _owner._disposed) return SystemNotificationResult.failed;
    _lastFailureCode = null;
    final channel = _owner._channel;
    if (channel == null) return SystemNotificationResult.unsupported;
    if (!_validToken(token) ||
        validateDesktopNamespace(namespace) == null ||
        !_validText(title, 128) ||
        !_validText(body, 512)) {
      return SystemNotificationResult.failed;
    }
    final generation = ++_generation;
    _pendingToken = token;
    try {
      final result = await channel
          .invokeMethod<String>('show', {
            'category': category.name,
            'token': token,
            'namespace': namespace,
            'title': title,
            'body': body,
          })
          .timeout(const Duration(seconds: 5));
      if (_disposed || _owner._disposed || generation != _generation) {
        return SystemNotificationResult.failed;
      }
      if (result == 'submitted') return SystemNotificationResult.submitted;
      _pendingToken = null;
      return result == 'unsupported'
          ? SystemNotificationResult.unsupported
          : SystemNotificationResult.failed;
    } on Object catch (error) {
      if (!_disposed &&
          !_owner._disposed &&
          generation == _generation &&
          _pendingToken == token) {
        _pendingToken = null;
        if (error is PlatformException) {
          final details = error.details;
          _lastFailureCode =
              details is Map && details.keys.every((k) => k == 'error_code')
              ? _failureCode(details['error_code'])
              : null;
        }
      }
      return error is MissingPluginException
          ? SystemNotificationResult.unsupported
          : SystemNotificationResult.failed;
    }
  }

  Future<bool> dismiss() async {
    if (_disposed || _owner._disposed) return true;
    _invalidate();
    return _dismissNative();
  }

  Future<bool> _dismissNative() async {
    try {
      await _owner._channel
          ?.invokeMethod<void>('dismiss', {'category': category.name})
          .timeout(const Duration(seconds: 2));
      return true;
    } on Object {
      return false;
    }
  }

  Future<void> dispose() => _closing ??= _dispose();
  Future<void> _dispose() async {
    _disposed = true;
    _invalidate();
    onClicked = null;
    onFailed = null;
    if (!_owner._disposed) await _dismissNative();
  }

  void _terminal(String method, String token, String? errorCode) {
    if (_disposed || _pendingToken != token) return;
    _pendingToken = null;
    if (method == 'failed') _lastFailureCode = errorCode;
    try {
      (method == 'clicked' ? onClicked : onFailed)?.call(token);
    } on Object {
      /* A consumer cannot restore a consumed callback identity. */
    }
  }
}

String? _failureCode(Object? value) =>
    value is int && value >= -0x80000000 && value < 0
    ? '0x${(value & 0xffffffff).toRadixString(16).toUpperCase().padLeft(8, '0')}'
    : null;
bool _validToken(String value) =>
    value.length == 32 && RegExp(r'^[0-9a-f]{32}$').hasMatch(value);
bool _validText(String value, int limit) {
  if (value.isEmpty || value.length > limit || value.contains('\u0000')) {
    return false;
  }
  for (var i = 0; i < value.length; i++) {
    final unit = value.codeUnitAt(i);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      if (++i >= value.length) return false;
      final low = value.codeUnitAt(i);
      if (low < 0xdc00 || low > 0xdfff) return false;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      return false;
    }
  }
  return true;
}
