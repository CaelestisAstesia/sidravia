import 'package:flutter/services.dart';
import 'package:sidravia_gui/desktop/system_notifications.dart';
export 'package:sidravia_gui/desktop/system_notifications.dart'
    show systemNotificationsChannelName;

enum SchoolAnnouncementNotificationResult { submitted, unsupported, failed }

/// Announcement-domain adapter; main injects the application's shared transport.
class SchoolAnnouncementNotifications {
  SchoolAnnouncementNotifications({
    MethodChannel? channel,
    TargetPlatform? platform,
  }) : _system = SystemNotifications(channel: channel, platform: platform),
       _ownsSystem = true;
  SchoolAnnouncementNotifications.shared(SystemNotifications system)
    : _system = system,
      _ownsSystem = false;
  SchoolAnnouncementNotifications.disabled()
    : _system = SystemNotifications.disabled(),
      _ownsSystem = true;
  final SystemNotifications _system;
  final bool _ownsSystem;
  late final _slot = _system.slot(SystemNotificationCategory.announcement);
  String? get lastFailureCode => _slot.lastFailureCode;
  set onClicked(void Function(String)? callback) => _slot.onClicked = callback;
  set onFailed(void Function(String)? callback) => _slot.onFailed = callback;
  Future<SchoolAnnouncementNotificationResult> show({
    required String token,
    String namespace = '',
  }) async => switch (await _slot.show(
    token: token,
    namespace: namespace,
    title: '校园网公告',
    body: '点击查看学校公告',
  )) {
    SystemNotificationResult.submitted =>
      SchoolAnnouncementNotificationResult.submitted,
    SystemNotificationResult.unsupported =>
      SchoolAnnouncementNotificationResult.unsupported,
    SystemNotificationResult.failed =>
      SchoolAnnouncementNotificationResult.failed,
  };
  Future<bool> dismiss() => _slot.dismiss();
  Future<void> dispose() => _ownsSystem ? _system.dispose() : _slot.dispose();
}
