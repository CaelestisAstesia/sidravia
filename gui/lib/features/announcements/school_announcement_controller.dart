import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_policy.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

/// Dispatches frontend announcement intents; daemon and preferences retain authority.
class SchoolAnnouncementController extends ChangeNotifier {
  SchoolAnnouncementController({
    required this.preferences,
    required this.desktop,
    required this.window,
    required this.notifications,
    required DateTime startedAt,
    this.namespace = '',
    DateTime Function()? clock,
  }) : _policy = SchoolAnnouncementPolicy(startedAt: startedAt),
       _clock = clock ?? DateTime.now {
    if (validateDesktopNamespace(namespace) == null) {
      throw const FormatException('Invalid desktop namespace');
    }
  }

  final Appearance preferences;
  final DesktopPresence desktop;
  final SchoolAnnouncementWindow window;
  final SchoolAnnouncementNotifications notifications;
  final String namespace;
  final SchoolAnnouncementPolicy _policy;
  final DateTime Function() _clock;
  final Set<Future<void>> _work = {};
  GuiCapabilities? _latest;
  _Intent? _active;
  String? _token, _feedback;
  bool _queued = false, _ready = false, _closed = false, _disposed = false;
  int _generation = 0;
  bool _dispatchPending = false;
  Future<void>? _starting, _closing;

  InstitutionProfile? get profile => _closed || _latest == null
      ? null
      : schoolAnnouncementProfileFor(_latest!);
  bool get available => profile != null;
  String? get feedback => _feedback;

  Future<void> start() => _starting ??= _start();
  Future<void> _start() async {
    if (_closed) return;
    preferences.addListener(_preferencesChanged);
    desktop.addListener(_visibilityChanged);
    notifications.onClicked = _clicked;
    notifications.onFailed = _failed;
    await preferences.initialize();
    if (_closed) return;
    _ready = true;
    _observe();
  }

  void updateCapabilities(GuiCapabilities capabilities) {
    if (_closed) return;
    _latest = capabilities;
    if (capabilities.isReady && _active != null && !_matches(_active!)) {
      _invalidate();
      _track(notifications.dismiss());
    }
    if (_ready) {
      _observe();
      _visibilityChanged();
    }
    _notify();
  }

  void _observe() {
    final capabilities = _latest;
    if (_closed || !_ready || capabilities == null) return;
    final school = _policy.consider(capabilities, preferences, _clock());
    if (_closed || school == null) return;
    final intent = _Intent(capabilities.configuration!.id, school);
    _invalidate();
    _active = intent;
    final generation = _generation;
    _setFeedback(null);
    _dispatchPending = true;
    _track(_dispatch(intent, generation));
  }

  Future<void> _dispatch(_Intent intent, int generation) async {
    if (preferences.hiddenAnnouncementBehavior ==
        GuiHiddenAnnouncementBehavior.showImmediately) {
      _dispatchPending = false;
      await _manual(intent, generation);
      return;
    }
    final visible = await desktop.refreshMainWindowVisibility();
    if (!_current(generation)) return;
    _dispatchPending = false;
    if (!_matches(intent)) return;
    if (visible == true ||
        preferences.hiddenAnnouncementBehavior ==
            GuiHiddenAnnouncementBehavior.showImmediately) {
      await _manual(intent, generation);
    } else if (visible == null) {
      _setFeedback('公告暂未显示，无法确认窗口状态，可在首页查看');
      await notifications.dismiss();
    } else if (preferences.hiddenAnnouncementBehavior ==
        GuiHiddenAnnouncementBehavior.onRestore) {
      _queued = true;
      await notifications.dismiss();
      if (_current(generation)) _visibilityChanged();
    } else {
      final random = Random.secure();
      final token = List.generate(
        16,
        (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'),
      ).join();
      _token = token;
      await _send(token, generation);
    }
  }

  Future<void> _send(String token, int generation) async {
    final result = await notifications.show(token: token, namespace: namespace);
    if (!_current(generation) || _token != token) return;
    if (result != SchoolAnnouncementNotificationResult.submitted) {
      _token = null;
      _setFeedback(_notificationFailureMessage);
    }
  }

  bool _current(int generation) => !_closed && generation == _generation;
  bool _matches(_Intent intent) {
    final current = profile;
    return current != null &&
        _latest!.configuration?.id == intent.configurationId &&
        _Intent(intent.configurationId, current).fingerprint ==
            intent.fingerprint;
  }

  Future<SchoolAnnouncementOpenResult> openManually() async {
    if (_closed) return SchoolAnnouncementOpenResult.failed;
    final school = profile;
    _invalidate();
    final generation = _generation;
    if (school == null) {
      _setFeedback('当前学校公告暂不可用');
      _track(notifications.dismiss());
      return SchoolAnnouncementOpenResult.failed;
    }
    final intent = _Intent(_latest!.configuration!.id, school);
    _active = intent;
    _setFeedback(null);
    final result = _manual(intent, generation);
    _track(result);
    return result;
  }

  Future<SchoolAnnouncementOpenResult> _manual(
    _Intent intent,
    int generation,
  ) async {
    await notifications.dismiss();
    if (!_current(generation)) return SchoolAnnouncementOpenResult.failed;
    return _open(intent, generation);
  }

  Future<SchoolAnnouncementOpenResult> _open(
    _Intent intent,
    int generation,
  ) async {
    if (!_current(generation) || !_matches(intent)) {
      if (_current(generation)) _setFeedback('当前学校公告暂不可用');
      return SchoolAnnouncementOpenResult.failed;
    }
    final result = await window.open(intent.metadata, namespace: namespace);
    if (_current(generation)) {
      _setFeedback(switch (result) {
        SchoolAnnouncementOpenResult.opened => null,
        SchoolAnnouncementOpenResult.unsupported => '当前环境暂不支持应用内公告',
        SchoolAnnouncementOpenResult.failed => '公告加载失败',
      });
    }
    return result;
  }

  void _clicked(String token) {
    if (_closed || _token != token) return;
    _token = null;
    final intent = _active;
    if (intent == null || !_matches(intent)) {
      _setFeedback('当前学校公告暂不可用');
      return;
    }
    _track(_open(intent, _generation));
  }

  String get _notificationFailureMessage {
    const message = '公告通知发送失败，可在首页查看';
    final code = notifications.lastFailureCode;
    return code == null ? message : '$message（错误码 $code）';
  }

  void _failed(String token) {
    if (_closed || _token != token) return;
    _token = null;
    _setFeedback(_notificationFailureMessage);
  }

  void _preferencesChanged() {
    if (_closed) return;
    if ((_queued || _dispatchPending) &&
        preferences.announcementFrequency ==
            GuiAnnouncementFrequency.manualOnly) {
      _invalidate();
    }
    _notify();
  }

  void _visibilityChanged() {
    if (_closed ||
        !_ready ||
        !_queued ||
        desktop.isMainWindowVisible != true ||
        _latest?.isReady != true) {
      return;
    }
    _queued = false;
    final intent = _active;
    if (intent != null) _track(_open(intent, _generation));
  }

  void _invalidate() {
    ++_generation;
    _active = null;
    _queued = false;
    _dispatchPending = false;
    _token = null;
  }

  void _setFeedback(String? value) {
    if (_closed || _feedback == value) return;
    _feedback = value;
    _notify();
  }

  void _notify() {
    if (!_closed && !_disposed) notifyListeners();
  }

  void _track<T>(Future<T> future) {
    late final Future<void> guarded;
    guarded = future
        .then<void>(
          (_) {},
          onError: (Object error, StackTrace stack) {
            if (!_closed) _setFeedback('公告加载失败');
          },
        )
        .whenComplete(() => _work.remove(guarded));
    _work.add(guarded);
  }

  Future<void> close() {
    if (_closing != null) return _closing!;
    _closed = true;
    _invalidate();
    preferences.removeListener(_preferencesChanged);
    desktop.removeListener(_visibilityChanged);
    notifications.onClicked = null;
    notifications.onFailed = null;
    return _closing = _close();
  }

  Future<void> _close() async {
    await Future.wait([notifications.dispose(), window.dispose()]);
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

class _Intent {
  _Intent(this.configurationId, InstitutionProfile profile)
    : metadata = profile.announcement!,
      fingerprint = [
        profile.id,
        profile.announcement!.url,
        profile.announcement!.clientWidth,
        profile.announcement!.clientHeight,
      ].join('|');
  final String configurationId, fingerprint;
  final AnnouncementMetadata metadata;
}
