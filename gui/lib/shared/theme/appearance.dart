import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

/// The single frontend appearance owner; storage never contains backend state.
class Appearance extends ValueNotifier<ThemeMode> {
  Appearance({GuiSettingsStore? store})
    : _store = store ?? MemoryGuiSettingsStore(),
      super(ThemeMode.system);
  final GuiSettingsStore _store;
  Future<void>? _initialization;
  Future<void> _writes = Future.value();
  int _revision = 0;
  bool _disposed = false, _canSave = true;
  String? _feedback;
  String? get feedback => _feedback;
  bool get persistent => _store.persistent;
  Future<void> initialize() => _initialization ??= _load();
  Future<void> _load() async {
    final revision = _revision;
    try {
      final stored = await _store.read();
      if (!_disposed && revision == _revision && stored != null) {
        super.value = ThemeMode.values.firstWhere(
          (mode) => mode.name == stored.name,
        );
      }
    } on Object {
      _canSave = false;
      _feedback = '无法读取外观设置，已安全回退；原文件保留，本次选择不会写入。';
      if (!_disposed) notifyListeners();
    }
  }

  @override
  set value(ThemeMode mode) {
    final loading = initialize();
    final revision = ++_revision;
    super.value = mode;
    _writes = _writes.then((_) async {
      await loading;
      if (!_canSave) return;
      try {
        await _store.save(
          GuiAppearanceMode.values.firstWhere(
            (value) => value.name == mode.name,
          ),
        );
        if (revision == _revision) _feedback = null;
      } on Object {
        if (revision == _revision) _feedback = '本次外观已生效，但未能保存；下次启动可能恢复之前的选择。';
      }
      if (!_disposed && revision == _revision) notifyListeners();
    });
  }

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
