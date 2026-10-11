import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

/// The single frontend preference owner; storage never contains backend state.
class Appearance extends ValueNotifier<ThemeMode> {
  Appearance({GuiSettingsStore? store})
    : _store = store ?? MemoryGuiSettingsStore(),
      super(ThemeMode.system);
  final GuiSettingsStore _store;
  Future<void>? _initialization;
  Future<void> _writes = Future.value();
  int _revision = 0, _appearanceRevision = 0, _diagnosticsRevision = 0;
  int _logRevision = 0;
  String _logLevel = "info";
  String get logLevel => _logLevel;
  set logLevel(String value) {
    if (!const {"info", "debug", "trace"}.contains(value)) {
      throw const FormatException("Invalid log level");
    }
    final loading = initialize();
    _logRevision++;
    final revision = ++_revision;
    final changed = _logLevel != value;
    _logLevel = value;
    if (!_disposed && changed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  int _closeRevision = 0, _startHiddenRevision = 0;
  int _frequencyRevision = 0,
      _hiddenBehaviorRevision = 0,
      _automaticDateRevision = 0;
  GuiAnnouncementFrequency _announcementFrequency =
      GuiAnnouncementFrequency.dailyFirstConnection;
  GuiHiddenAnnouncementBehavior _hiddenAnnouncementBehavior =
      GuiHiddenAnnouncementBehavior.notify;
  String? _lastAutomaticAnnouncementDate;
  GuiCloseBehavior _closeBehavior = GuiCloseBehavior.hideToTray;
  bool _startHidden = false;
  bool _disposed = false, _canSave = true;
  bool _diagnosticsExpanded = false;
  String? _feedback;
  String? get feedback => _feedback;
  bool get persistent => _store.persistent;
  bool get diagnosticsExpanded => _diagnosticsExpanded;
  set diagnosticsExpanded(bool expanded) {
    final loading = initialize();
    _diagnosticsRevision++;
    final revision = ++_revision;
    final changed = _diagnosticsExpanded != expanded;
    _diagnosticsExpanded = expanded;
    if (!_disposed && changed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  GuiCloseBehavior get closeBehavior => _closeBehavior;
  set closeBehavior(GuiCloseBehavior next) {
    final loading = initialize();
    _closeRevision++;
    final revision = ++_revision;
    final changed = _closeBehavior != next;
    _closeBehavior = next;
    if (!_disposed && changed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  bool get startHidden => _startHidden;
  set startHidden(bool next) {
    final loading = initialize();
    _startHiddenRevision++;
    final revision = ++_revision;
    final changed = _startHidden != next;
    _startHidden = next;
    if (!_disposed && changed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  GuiAnnouncementFrequency get announcementFrequency => _announcementFrequency;
  set announcementFrequency(GuiAnnouncementFrequency next) {
    final loading = initialize();
    _frequencyRevision++;
    final revision = ++_revision;
    final changed = _announcementFrequency != next;
    _announcementFrequency = next;
    if (!_disposed && changed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  GuiHiddenAnnouncementBehavior get hiddenAnnouncementBehavior =>
      _hiddenAnnouncementBehavior;
  set hiddenAnnouncementBehavior(GuiHiddenAnnouncementBehavior next) {
    final loading = initialize();
    _hiddenBehaviorRevision++;
    final revision = ++_revision;
    final changed = _hiddenAnnouncementBehavior != next;
    _hiddenAnnouncementBehavior = next;
    if (!_disposed && changed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  String? get lastAutomaticAnnouncementDate => _lastAutomaticAnnouncementDate;
  void recordAutomaticAnnouncement(DateTime at) {
    final local = at.toLocal();
    if (local.year < 1 || local.year > 9999) {
      throw const FormatException('Invalid announcement date');
    }
    final date =
        '${local.year.toString().padLeft(4, '0')}-${local.month.toString().padLeft(2, '0')}-${local.day.toString().padLeft(2, '0')}';
    final loading = initialize();
    // Claim this field even when an early trigger matches the current value.
    _automaticDateRevision++;
    if (_lastAutomaticAnnouncementDate == date) return;
    final revision = ++_revision;
    _lastAutomaticAnnouncementDate = date;
    if (!_disposed) notifyListeners();
    _queueCurrent(loading, revision);
  }

  GuiSettings _snapshot() => GuiSettings(
    logLevel: _logLevel,
    appearanceMode: _appearanceMode(value),
    diagnosticsExpanded: _diagnosticsExpanded,
    closeBehavior: _closeBehavior,
    startHidden: _startHidden,
    announcementFrequency: _announcementFrequency,
    hiddenAnnouncementBehavior: _hiddenAnnouncementBehavior,
    lastAutomaticAnnouncementDate: _lastAutomaticAnnouncementDate,
  );

  void _queueCurrent(Future<void> loading, int revision) => _queueSave(
    loading,
    revision,
    _snapshot(),
    _appearanceRevision,
    _diagnosticsRevision,
    _closeRevision,
    _startHiddenRevision,
    _frequencyRevision,
    _hiddenBehaviorRevision,
    _automaticDateRevision,
  );

  Future<void> initialize() => _initialization ??= _load();
  Future<void> _load() async {
    final logRevision = _logRevision;
    final appearanceRevision = _appearanceRevision;
    final diagnosticsRevision = _diagnosticsRevision;
    final closeRevision = _closeRevision;
    final startHiddenRevision = _startHiddenRevision;
    final frequencyRevision = _frequencyRevision;
    final hiddenBehaviorRevision = _hiddenBehaviorRevision;
    final automaticDateRevision = _automaticDateRevision;
    try {
      final stored = await _store.read();
      if (_disposed || stored == null) return;
      var diagnosticsChanged = false;
      if (logRevision == _logRevision) {
        diagnosticsChanged = _logLevel != stored.logLevel;
        _logLevel = stored.logLevel;
      }
      if (appearanceRevision == _appearanceRevision) {
        super.value = ThemeMode.values.firstWhere(
          (mode) => mode.name == stored.appearanceMode.name,
        );
      }
      if (diagnosticsRevision == _diagnosticsRevision) {
        diagnosticsChanged = _diagnosticsExpanded != stored.diagnosticsExpanded;
        _diagnosticsExpanded = stored.diagnosticsExpanded;
      }
      if (closeRevision == _closeRevision) {
        diagnosticsChanged =
            diagnosticsChanged || _closeBehavior != stored.closeBehavior;
        _closeBehavior = stored.closeBehavior;
      }
      if (startHiddenRevision == _startHiddenRevision) {
        diagnosticsChanged =
            diagnosticsChanged || _startHidden != stored.startHidden;
        _startHidden = stored.startHidden;
      }
      if (frequencyRevision == _frequencyRevision) {
        diagnosticsChanged =
            diagnosticsChanged ||
            _announcementFrequency != stored.announcementFrequency;
        _announcementFrequency = stored.announcementFrequency;
      }
      if (hiddenBehaviorRevision == _hiddenBehaviorRevision) {
        diagnosticsChanged =
            diagnosticsChanged ||
            _hiddenAnnouncementBehavior != stored.hiddenAnnouncementBehavior;
        _hiddenAnnouncementBehavior = stored.hiddenAnnouncementBehavior;
      }
      if (automaticDateRevision == _automaticDateRevision) {
        diagnosticsChanged =
            diagnosticsChanged ||
            _lastAutomaticAnnouncementDate !=
                stored.lastAutomaticAnnouncementDate;
        _lastAutomaticAnnouncementDate = stored.lastAutomaticAnnouncementDate;
      }
      if (!_disposed && diagnosticsChanged) notifyListeners();
    } on Object {
      _canSave = false;
      _feedback = '无法读取偏好设置，已安全回退；原文件保留，本次选择不会写入。';
      if (!_disposed) notifyListeners();
    }
  }

  @override
  set value(ThemeMode mode) {
    final loading = initialize();
    _appearanceRevision++;
    final revision = ++_revision;
    super.value = mode;
    _queueCurrent(loading, revision);
  }

  void _queueSave(
    Future<void> loading,
    int revision,
    GuiSettings queued,
    int appearanceRevision,
    int diagnosticsRevision,
    int closeRevision,
    int startHiddenRevision,
    int frequencyRevision,
    int hiddenBehaviorRevision,
    int automaticDateRevision,
  ) {
    _writes = _writes.then((_) async {
      await loading;
      if (!_canSave) return;
      try {
        await _store.save(
          GuiSettings(
            logLevel: _logRevision == 0 ? _logLevel : queued.logLevel,
            appearanceMode: appearanceRevision == 0
                ? _appearanceMode(value)
                : queued.appearanceMode,
            diagnosticsExpanded: diagnosticsRevision == 0
                ? _diagnosticsExpanded
                : queued.diagnosticsExpanded,
            closeBehavior: closeRevision == 0
                ? _closeBehavior
                : queued.closeBehavior,
            startHidden: startHiddenRevision == 0
                ? _startHidden
                : queued.startHidden,
            announcementFrequency: frequencyRevision == 0
                ? _announcementFrequency
                : queued.announcementFrequency,
            hiddenAnnouncementBehavior: hiddenBehaviorRevision == 0
                ? _hiddenAnnouncementBehavior
                : queued.hiddenAnnouncementBehavior,
            lastAutomaticAnnouncementDate: automaticDateRevision == 0
                ? _lastAutomaticAnnouncementDate
                : queued.lastAutomaticAnnouncementDate,
          ),
        );
        if (revision == _revision) _feedback = null;
      } on Object {
        if (revision == _revision) {
          _feedback = '本次偏好已生效，但未能保存；下次启动可能恢复之前的选择。';
        }
      }
      if (!_disposed && revision == _revision) notifyListeners();
    });
  }

  GuiAppearanceMode _appearanceMode(ThemeMode mode) =>
      GuiAppearanceMode.values.firstWhere((value) => value.name == mode.name);

  Future<void> get settled async {
    await initialize();
    await _writes;
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

class AppearanceScope extends InheritedNotifier<Appearance> {
  const AppearanceScope({
    super.key,
    required Appearance appearance,
    required super.child,
  }) : super(notifier: appearance);
  static Appearance? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppearanceScope>()?.notifier;
}
