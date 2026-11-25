part of 'linux_installation_manager.dart';

extension _LinuxLaunchSettings on LinuxInstallationManager {
  Future<T> _withStartupWork<T>(Future<T> Function() action) async {
    _checkOpen();
    if (_startupWork != null) {
      throw const IntegrationFailure('正在处理后台或自启设置，请稍后重试。');
    }
    final completion = Completer<void>();
    _startupWork = completion.future;
    try {
      return await action();
    } finally {
      _startupWork = null;
      completion.complete();
    }
  }

  String _startupMode(String desktop, UserServiceStatus? service) {
    if (desktop == 'conflict' ||
        service?.registrationKnown == false ||
        service?.recoveryPending == true ||
        service?.recoveryBlocked == true ||
        (service?.directory != null && service?.canControl != true)) {
      return 'conflict';
    }
    // These exact desktop entries also carry the user's intent while
    // migrating old settings or finishing a management-mode change.
    if (desktop != 'disabled') return desktop;
    return service?.canControl == true && service!.enabled
        ? 'daemon'
        : 'disabled';
  }

  Future<void> _migrateStartup() async {
    if (!_available('$_directory/sidraviactl')) return;
    final entry = _existing();
    final mode = _mode(entry);
    if (mode == 'disabled' || mode == 'conflict') return;
    final service = await _runService('status');
    if (_startupMode(mode, service) == 'conflict') return;
    if ((service?.canControl != true && entry == _entry(mode)) ||
        (mode == 'gui' && service?.enabled == true && entry == _entry(mode))) {
      return;
    }
    await _setStartup(mode);
  }

  Future<UserServiceStatus?> _switchManagement(String action) async {
    final before = await _runService('status');
    final original = _existing();
    final desktop = _mode(original);
    final intent = _startupMode(desktop, before);
    if (intent == 'conflict' || before?.available != true) {
      throw const IntegrationFailure('无法确认本安装的后台与自启状态，请先处理冲突。');
    }
    if (action == 'takeover') {
      if (before!.canControl) return before;
      await _runService(action);
      try {
        // Keep the owned XDG entry until the corresponding systemd
        // enablement exists. A later migration can recover this intent.
        await _setStartup(intent);
      } on Object {
        throw const IntegrationFailure('后台管理方式已切换，自启设置待确认，请检查当前状态。');
      }
      return _runService('status');
    }
    if (!before!.canControl) return before;
    // Preserve the intent across unregistering the old service.
    _setDesktopStartup(intent, expected: original, checkExpected: true);
    final prepared = _existing();
    try {
      return await _runService(action);
    } on Object {
      final current = await _runService('status');
      if (current?.canControl == true) {
        _setDesktopStartup(
          desktop,
          expected: prepared,
          checkExpected: true,
          restoreEntry: original,
        );
      }
      rethrow;
    }
  }

  Future<void> _setStartup(String mode) async {
    final original = _existing();
    final desktop = _mode(original);
    final service = await _runService('status');
    if (_startupMode(desktop, service) == 'conflict') {
      throw const IntegrationFailure('自启项或后台登记存在冲突，未修改。');
    }
    if (service?.canControl != true) {
      _setDesktopStartup(mode, expected: original, checkExpected: true);
      return;
    }

    final enabled = service!.enabled;
    if (mode == 'disabled') {
      // Remove GUI autostart first. A stopped current service stays stopped.
      _setDesktopStartup('disabled', expected: original, checkExpected: true);
      try {
        if (enabled) await _runService('disable');
      } on Object {
        final actual = await _runService('status');
        if (actual?.canControl == true && actual!.enabled) {
          _setDesktopStartup(
            desktop,
            expected: null,
            checkExpected: true,
            restoreEntry: original,
          );
        }
        rethrow;
      }
      return;
    }

    if (!enabled) await _runService('enable');
    try {
      _setDesktopStartup(
        mode == 'gui' ? 'gui' : 'disabled',
        expected: original,
        checkExpected: true,
      );
    } on Object {
      // Keep the backend enabled if this operation or another writer
      // published an owned GUI entry before an error was reported.
      if (!enabled && _mode(_existing()) != 'gui') {
        await _runService('disable');
      }
      rethrow;
    }
  }
}
