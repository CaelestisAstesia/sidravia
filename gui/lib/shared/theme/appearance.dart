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
    _queueSave(
      loading,
      revision,
      GuiSettings(
        appearanceMode: _appearanceMode(value),
        diagnosticsExpanded: expanded,
      ),
      _appearanceRevision,
      _diagnosticsRevision,
    );
  }

  Future<void> initialize() => _initialization ??= _load();
  Future<void> _load() async {
    final appearanceRevision = _appearanceRevision;
    final diagnosticsRevision = _diagnosticsRevision;
    try {
      final stored = await _store.read();
      if (_disposed || stored == null) return;
      var diagnosticsChanged = false;
      if (appearanceRevision == _appearanceRevision) {
        super.value = ThemeMode.values.firstWhere(
          (mode) => mode.name == stored.appearanceMode.name,
        );
      }
      if (diagnosticsRevision == _diagnosticsRevision) {
        diagnosticsChanged = _diagnosticsExpanded != stored.diagnosticsExpanded;
        _diagnosticsExpanded = stored.diagnosticsExpanded;
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
    _queueSave(
      loading,
      revision,
      GuiSettings(
        appearanceMode: _appearanceMode(mode),
        diagnosticsExpanded: _diagnosticsExpanded,
      ),
      _appearanceRevision,
      _diagnosticsRevision,
    );
  }

  void _queueSave(
    Future<void> loading,
    int revision,
    GuiSettings queued,
    int appearanceRevision,
    int diagnosticsRevision,
  ) {
    _writes = _writes.then((_) async {
      await loading;
      if (!_canSave) return;
      try {
        await _store.save(
          GuiSettings(
            appearanceMode: appearanceRevision == 0
                ? _appearanceMode(value)
                : queued.appearanceMode,
            diagnosticsExpanded: diagnosticsRevision == 0
                ? _diagnosticsExpanded
                : queued.diagnosticsExpanded,
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
