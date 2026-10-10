import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const schoolAnnouncementChannelName = 'sidravia/announcements';

/// Opened acknowledges a native window, not successful webpage navigation.
enum SchoolAnnouncementOpenResult { opened, unsupported, failed }

class SchoolAnnouncementWindow {
  SchoolAnnouncementWindow({MethodChannel? channel, TargetPlatform? platform})
    : _channel =
          !kIsWeb &&
              (platform ?? defaultTargetPlatform) == TargetPlatform.windows
          ? channel ?? const MethodChannel(schoolAnnouncementChannelName)
          : null;

  SchoolAnnouncementWindow.disabled() : _channel = null;

  final MethodChannel? _channel;
  bool _disposed = false;

  Future<SchoolAnnouncementOpenResult> open(
    AnnouncementMetadata metadata, {
    String namespace = '',
  }) async {
    if (_disposed) return SchoolAnnouncementOpenResult.failed;
    final channel = _channel;
    if (channel == null) return SchoolAnnouncementOpenResult.unsupported;
    if (validateDesktopNamespace(namespace) == null) {
      return SchoolAnnouncementOpenResult.failed;
    }
    final payload = {
      'url': metadata.url,
      'clientWidth': metadata.clientWidth,
      'clientHeight': metadata.clientHeight,
    };
    try {
      // Use the public profile decoder's domain rules for direct callers too.
      decodeProfiles(
        jsonEncode({
          'profiles': [
            {
              'institutionProfileId': 'announcement',
              'displayName': 'Announcement',
              'authenticationProtocolId': 'announcement',
              'announcement': payload,
            },
          ],
        }),
      );
      final result = await channel.invokeMethod<String>('open', {
        ...payload,
        'namespace': namespace,
      });
      return !_disposed && result == 'opened'
          ? SchoolAnnouncementOpenResult.opened
          : SchoolAnnouncementOpenResult.failed;
    } on MissingPluginException {
      return SchoolAnnouncementOpenResult.unsupported;
    } on Object {
      return SchoolAnnouncementOpenResult.failed;
    }
  }

  Future<bool> close() async {
    if (_disposed) return true;
    return _closeNative();
  }

  Future<bool> _closeNative() async {
    if (_channel == null) return true;
    try {
      await _channel.invokeMethod<void>('close');
      return true;
    } on Object {
      return false;
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    await _closeNative();
  }
}
