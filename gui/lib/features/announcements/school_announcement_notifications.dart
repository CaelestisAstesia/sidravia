import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';

const schoolAnnouncementNotificationsChannelName =
    'sidravia/announcement-notifications';

/// Submission acknowledges the platform API, never actual delivery.
enum SchoolAnnouncementNotificationResult { submitted, unsupported, failed }

class SchoolAnnouncementNotifications {
  SchoolAnnouncementNotifications({
    MethodChannel? channel,
    TargetPlatform? platform,
  }) : _channel =
           !kIsWeb &&
               (platform ?? defaultTargetPlatform) == TargetPlatform.windows
           ? channel ??
                 const MethodChannel(schoolAnnouncementNotificationsChannelName)
           : null {
    _channel?.setMethodCallHandler(_onNativeCall);
  }

  SchoolAnnouncementNotifications.disabled() : _channel = null;

  final MethodChannel? _channel;
  int _generation = 0;
  String? _pendingToken;
  bool _disposed = false;
  String? _lastFailureCode;
  String? get lastFailureCode => _lastFailureCode;
  void Function(String token)? onClicked;
  void Function(String token)? onFailed;

  Future<SchoolAnnouncementNotificationResult> show({
    required String token,
    String namespace = '',
  }) async {
    if (_disposed) return SchoolAnnouncementNotificationResult.failed;
    _lastFailureCode = null;
    final channel = _channel;
    if (channel == null) {
      return SchoolAnnouncementNotificationResult.unsupported;
    }
    if (!_validToken(token) || validateDesktopNamespace(namespace) == null) {
      return SchoolAnnouncementNotificationResult.failed;
    }
    final generation = ++_generation;
    _pendingToken = token;
    try {
      final result = await channel.invokeMethod<String>('show', {
        'token': token,
        'namespace': namespace,
      });
      if (_disposed || generation != _generation) {
        return SchoolAnnouncementNotificationResult.failed;
      }
      if (result == 'submitted') {
        return SchoolAnnouncementNotificationResult.submitted;
      }
      _pendingToken = null;
      return result == 'unsupported'
          ? SchoolAnnouncementNotificationResult.unsupported
          : SchoolAnnouncementNotificationResult.failed;
    } on MissingPluginException {
      if (_disposed || generation != _generation) {
        return SchoolAnnouncementNotificationResult.failed;
      }
      _pendingToken = null;
      return SchoolAnnouncementNotificationResult.unsupported;
    } on PlatformException catch (error) {
      if (!_disposed && generation == _generation && _pendingToken == token) {
        _lastFailureCode = _failureDetails(error.details);
        _pendingToken = null;
      }
      return SchoolAnnouncementNotificationResult.failed;
    } on Object {
      if (!_disposed && generation == _generation) _pendingToken = null;
      return SchoolAnnouncementNotificationResult.failed;
    }
  }

  Future<bool> dismiss() async {
    if (_disposed) return true;
    ++_generation;
    _pendingToken = null;
    _lastFailureCode = null;
    if (_channel == null) return true;
    try {
      await _channel.invokeMethod<void>('dismiss');
      return true;
    } on Object {
      return false;
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    ++_generation;
    _pendingToken = null;
    _lastFailureCode = null;
    _channel?.setMethodCallHandler(null);
    onClicked = null;
    onFailed = null;
    try {
      await _channel?.invokeMethod<void>('dispose');
    } on Object {
      // Teardown cannot change an already terminal request or notify a consumer.
    }
  }

  Future<Object?> _onNativeCall(MethodCall call) async {
    Object? token = call.arguments;
    String? errorCode;
    if (call.method == 'failed') {
      final payload = call.arguments;
      if (payload is! Map ||
          !payload.containsKey('token') ||
          payload.keys.any((key) => key != 'token' && key != 'error_code')) {
        return null;
      }
      token = payload['token'];
      errorCode = _failureCode(payload['error_code']);
    }
    if (_disposed ||
        !const {'clicked', 'failed'}.contains(call.method) ||
        token is! String ||
        !_validToken(token) ||
        token != _pendingToken) {
      return null;
    }
    if (call.method == 'failed') _lastFailureCode = errorCode;
    _pendingToken = null;
    try {
      (call.method == 'clicked' ? onClicked : onFailed)?.call(token);
    } on Object {
      // Consumer errors cannot restore a consumed token or escape the channel.
    }
    return null;
  }

  static String? _failureDetails(Object? details) {
    if (details is! Map || details.keys.any((key) => key != 'error_code')) {
      return null;
    }
    return _failureCode(details['error_code']);
  }

  static String? _failureCode(Object? value) =>
      value is int && value >= -0x80000000 && value < 0
      ? '0x${(value & 0xffffffff).toRadixString(16).toUpperCase().padLeft(8, '0')}'
      : null;

  static bool _validToken(String token) =>
      token.length == 32 && RegExp(r'^[0-9a-f]{32}$').hasMatch(token);
}
