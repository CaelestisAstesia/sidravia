import 'dart:convert';
import 'dart:io';

import 'gui_settings_storage_io.dart';
import 'installation_manager.dart';

/// Owns explicit user integration only. Autostart changes do not stop or adopt
/// a running daemon. PATH publishes exact executable links, never shell code.
class LinuxInstallationManager implements InstallationManager {
  LinuxInstallationManager({
    required this.executable,
    required this.environment,
  });
  final String executable;
  final Map<String, String> environment;
  bool _closed = false;

  String get _namespace => environment['SIDRAVIA_NAMESPACE'] ?? '';
  String _normalRoot(String path) => File.fromUri(
    Directory(path).absolute.uri.normalizePath().resolve('__path_anchor__'),
  ).parent.path;
  String get _directory => _normalRoot(File(executable).parent.path);
  String get _configRoot {
    final xdg = environment['XDG_CONFIG_HOME'];
    if (xdg != null && xdg.isNotEmpty && Directory(xdg).isAbsolute) {
      return _normalRoot(xdg);
    }
    return '$_home/.config';
  }

  String get _home {
    final home = environment['HOME'];
    if (home == null || home.isEmpty || !Directory(home).isAbsolute) {
      throw const IntegrationFailure('无法确定用户 HOME，未修改命令目录。');
    }
    return _normalRoot(home);
  }

  String get _path {
    final suffix = _namespace.isEmpty
        ? ''
        : '-${base64Url.encode(utf8.encode(_namespace)).replaceAll('=', '')}';
    return '$_configRoot/autostart/io.github.CaelestisAstesia.Sidravia$suffix.desktop';
  }

  String get _bin => '$_home/.local/bin';
  Map<String, String> get _commands => {
    'sidraviactl': '$_directory/sidraviactl',
    'sidraviad': '$_directory/sidraviad',
    'sidraviaupdater': '$_directory/sidraviaupdater',
    'sidravia_gui': executable,
  };

  void _checkOpen() {
    if (_closed) throw const IntegrationFailure('系统集成操作已取消。');
    if (!validGuiNamespace(_namespace) || !File(executable).isAbsolute) {
      throw const IntegrationFailure('启动路径或命名空间无效。');
    }
  }

  bool _available(String path) {
    final info = File(path).statSync();
    return info.type == FileSystemEntityType.file && info.mode & 0x49 != 0;
  }

  String _quote(String value) {
    if (value.contains(RegExp(r'[\x00-\x1f\x7f]'))) {
      throw const IntegrationFailure('启动路径包含不支持的字符。');
    }
    return '"${value.replaceAll('\\', '\\\\\\\\').replaceAll('"', '\\\\"').replaceAll('`', '\\\\`').replaceAll(r'$', r'\\$').replaceAll('%', '%%')}"';
  }

  String _header() =>
      '[Desktop Entry]\nType=Application\nName=Sidravia\n'
      'Comment=Sidravia session autostart\nIcon=${_directory.replaceAll('\\', '\\\\')}/data/icons/sidravia.png\nTerminal=false\n';
  String _entry(String mode) {
    final name = mode == 'gui' ? executable.split('/').last : 'sidraviactl';
    // GNU env treats any NAME=VALUE operand as an assignment, even when it
    // contains '/'. Use a chdir argument so '=' in a portable directory is safe.
    if (name.contains('=')) {
      throw const IntegrationFailure('自启程序文件名不能含等号，请恢复程序原名。');
    }
    final args = mode == 'gui'
        ? '--start-hidden'
        : 'daemon start --log-level info';
    return '${_header()}Exec=${_quote('/usr/bin/env')} --chdir ${_quote(_directory)} ${_quote('SIDRAVIA_NAMESPACE=$_namespace')} ${_quote('./$name')} $args\n'
        'X-Sidravia-Owner=linux-desktop-v2\n';
  }

  // Read only an exact prior definition; never adopt arbitrary .desktop files.
  String get _legacyGui =>
      '${_header()}Exec=${_quote('/usr/bin/env')} ${_quote('SIDRAVIA_NAMESPACE=$_namespace')} ${_quote(executable)} --start-hidden\n'
      'X-Sidravia-Owner=linux-gui-v1\n';
  String? _existing() {
    List<FileSystemEntity> entries;
    try {
      entries = File(_path).parent.listSync(followLinks: false);
    } on FileSystemException catch (error) {
      if (error.osError?.errorCode == 2) return null;
      throw const IntegrationFailure('无法读取会话自启目录，未确认登记状态。');
    }
    for (final entry in entries) {
      if (entry.path != _path) continue;
      if (entry is! File || entry.lengthSync() > 65536) {
        throw const IntegrationFailure('自启文件类型或大小冲突，未修改。');
      }
      return entry.readAsStringSync();
    }
    return null;
  }

  String _mode(String? existing) {
    if (existing == null) return 'disabled';
    if (existing == _entry('gui') || existing == _legacyGui) return 'gui';
    if (existing == _entry('daemon')) return 'daemon';
    return 'conflict';
  }

  /// Refuse links/non-directories in mutable registration paths. The portable
  /// executable itself may still be reached through its own resolved path.
  void _ensureDirectory(Directory directory) {
    if (directory.parent.path != directory.path) {
      _ensureDirectory(directory.parent);
    }
    final type = FileSystemEntity.typeSync(directory.path, followLinks: false);
    if (type == FileSystemEntityType.directory) return;
    if (type != FileSystemEntityType.notFound) {
      throw const IntegrationFailure('系统集成目录存在文件或符号链接冲突，未修改。');
    }
    directory.createSync();
  }

  T _locked<T>(String directory, T Function() operation) {
    _ensureDirectory(Directory(directory));
    final path = '$directory/.sidravia-integration.lock';
    final type = FileSystemEntity.typeSync(path, followLinks: false);
    if ((type != FileSystemEntityType.notFound &&
            type != FileSystemEntityType.file) ||
        (type == FileSystemEntityType.file && File(path).lengthSync() != 0)) {
      throw const IntegrationFailure('维护锁文件存在冲突，未修改。');
    }
    final lock = File(path).openSync(mode: FileMode.append);
    var held = false;
    try {
      try {
        lock.lockSync(
          FileLock.exclusive,
        ); // Nonblocking: do not freeze GTK/Flutter.
        held = true;
      } on FileSystemException {
        throw const IntegrationFailure('另一项系统集成操作正在进行，请稍后重试。');
      }
      return operation();
    } finally {
      try {
        if (held) lock.unlockSync();
      } finally {
        lock.closeSync();
      }
    }
  }

  String _linkState(String path, String target) {
    // Directory listing preserves access errors that Dart stat/type APIs may
    // collapse to notFound. Unknown ownership must never mean "disabled".
    final parent = File(path).parent;
    List<FileSystemEntity> entries;
    try {
      entries = parent.listSync(followLinks: false);
    } on FileSystemException catch (error) {
      if (error.osError?.errorCode == 2) return 'missing';
      throw const IntegrationFailure('无法读取用户命令目录，未确认登记状态。');
    }
    for (final entry in entries) {
      if (entry.path != path) continue;
      return entry is Link && entry.targetSync() == target
          ? 'owned'
          : 'conflict';
    }
    return 'missing';
  }

  ({bool enabled, String notice}) _pathState() {
    final states = [
      for (final e in _commands.entries) _linkState('$_bin/${e.key}', e.value),
    ];
    final owned = states.where((s) => s == 'owned').length;
    if (states.contains('conflict')) {
      return (enabled: owned > 0, notice: '用户命令名存在冲突，未接管其他安装；关闭只移除本安装的链接。');
    }
    if (owned > 0 && owned != states.length) {
      return (enabled: true, notice: '命令登记不完整，请关闭后重新启用。');
    }
    final paths = (environment['PATH'] ?? '')
        .split(':')
        .map((p) => _normalRoot(p.isEmpty ? Directory.current.path : p))
        .toList();
    if (!paths.contains(_bin)) {
      return (
        enabled: owned == states.length,
        notice: '通过 ~/.local/bin 登记命令；该目录不在当前 PATH 中，请重新登录或按所用 shell 配置 PATH。',
      );
    }
    final before = paths.take(paths.indexOf(_bin));
    if (before.any(
      (d) => _commands.keys.any(
        (name) => _available('${d.isEmpty ? Directory.current.path : d}/$name'),
      ),
    )) {
      return (
        enabled: owned == states.length,
        notice: '通过 ~/.local/bin 登记命令；当前 PATH 前方有同名程序，终端可能优先运行其他安装。',
      );
    }
    return (
      enabled: owned == states.length,
      notice: '在 ~/.local/bin 登记本安装的命令；关闭只移除对应链接。',
    );
  }

  void _changePath(bool enable) {
    final commands = _commands;
    _locked(_bin, () {
      if (!enable) {
        for (final e in commands.entries) {
          final path = '$_bin/${e.key}';
          if (_linkState(path, e.value) == 'owned') Link(path).deleteSync();
        }
        return;
      }
      for (final e in commands.entries) {
        if (!_available(e.value)) {
          throw IntegrationFailure('程序 ${e.key} 不存在或不可执行，请检查软件包。');
        }
        if (_linkState('$_bin/${e.key}', e.value) == 'conflict') {
          throw IntegrationFailure('用户命令 ${e.key} 属于其他文件或安装，未修改。');
        }
      }
      final created = <MapEntry<String, String>>[];
      try {
        for (final e in commands.entries) {
          final path = '$_bin/${e.key}';
          if (_linkState(path, e.value) == 'missing') {
            Link(path)
                .createSync(e.value); // Exclusive; never replace a peer file.
            created.add(MapEntry(path, e.value));
          }
        }
      } on Object {
        for (final e in created.reversed) {
          if (_linkState(e.key, e.value) == 'owned') Link(e.key).deleteSync();
        }
        rethrow;
      }
    });
  }

  @override
  Future<IntegrationStatus> status() async {
    _checkOpen();
    try {
      var pathSupported = true;
      var pathEnabled = false;
      String pathNotice;
      try {
        final state = _pathState();
        pathEnabled = state.enabled;
        pathNotice = state.notice;
      } on IntegrationFailure catch (error) {
        pathSupported = false;
        pathNotice = error.message;
      }
      return IntegrationStatus(
        pathEnabled: pathEnabled,
        guiAvailable: _available(executable),
        autostart: _mode(_existing()),
        userPathSupported: pathSupported,
        daemonAutostartSupported: _available('$_directory/sidraviactl'),
        pathNotice: pathNotice,
        autostartNotice: _available('$_directory/sidraviactl')
            ? '登录桌面会话后启动；仅后台模式没有托盘，可通过 CTL 停止。'
            : '包内 CTL 不存在或不可执行，无法设置仅后台自启。',
      );
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法读取 Linux 系统集成状态。');
    }
  }

  @override
  Future<void> change(IntegrationChange change) async {
    _checkOpen();
    try {
      if (change == IntegrationChange.pathEnable ||
          change == IntegrationChange.pathDisable) {
        _changePath(change == IntegrationChange.pathEnable);
        return;
      }
      _locked(File(_path).parent.path, () {
        final existing = _existing();
        if (_mode(existing) == 'conflict') {
          throw const IntegrationFailure('自启项属于其他程序或安装，未修改。');
        }
        final file = File(_path);
        if (change == IntegrationChange.startDisable) {
          if (existing != null) file.deleteSync();
          return;
        }
        final mode = change == IntegrationChange.startGui ? 'gui' : 'daemon';
        final target = mode == 'gui' ? executable : '$_directory/sidraviactl';
        if (!_available(target)) {
          throw const IntegrationFailure('启动程序不存在或不可执行。');
        }
        final contents = _entry(mode);
        if (existing == contents) return;
        // Serialize our writers; after removing an exact owned definition,
        // exclusive creation refuses an unrelated writer rather than replacing it.
        if (existing != null) file.deleteSync();
        file.createSync(exclusive: true);
        file.writeAsStringSync(contents, flush: true);
      });
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('Linux 系统集成修改失败，请刷新确认当前状态。');
    }
  }

  @override
  Future<void> close() async {
    _closed = true;
  }
}
