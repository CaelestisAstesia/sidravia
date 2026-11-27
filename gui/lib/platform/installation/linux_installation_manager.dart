import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';
import 'dart:math';

import 'package:ffi/ffi.dart';

import '../integration_failure.dart';
import '../paths/linux_product_executable.dart';
import '../paths/linux_user_directories.dart';
import '../paths/product_namespace.dart';
import '../update/package_update_checker.dart';
import '../update/update_check_process.dart';
import '../update/update_check_response.dart';
import '../update/update_release_version.dart';
import 'installation_manager.dart';

part 'linux_launch_settings.dart';

/// Owns explicit user integration only. Autostart changes do not stop or adopt
/// a running daemon. PATH publishes exact executable links, never shell code.
class LinuxInstallationManager
    implements
        InstallationManager,
        UserServiceManager,
        StartupMigration,
        PackageUpdateChecker {
  LinuxInstallationManager({
    required String executable,
    required this.environment,
    UpdateCheckProcess? updateCheck,
  }) : _updateCheck = updateCheck ?? UpdateCheckProcess(),
       executable = linuxProductExecutable(executable);
  final String executable;
  final Map<String, String> environment;
  final UpdateCheckProcess _updateCheck;
  bool _closed = false;
  Future<void>? _startupWork;

  @override
  Future<UserServiceStatus?> userService(String action) {
    _checkOpen();
    if (action == 'status') return _runService(action);
    return _withStartupWork(() {
      if (action == 'takeover' || action == 'release') {
        return _switchManagement(action);
      }
      return _runService(action);
    });
  }

  @override
  Future<void> migrateStartup() => _withStartupWork(_migrateStartup);

  Future<UserServiceStatus?> _runService(String action) async {
    if (_namespace.isNotEmpty) return null;
    try {
      final result = await Process.run(
        '$_directory/sidraviactl',
        [
          'integration',
          'service',
          action,
          if (action == 'takeover' || action == 'release')
            '--interrupt-session',
          if (action == 'release') ...['--owner-pid', '$pid'],
          if (action == 'discard' || action == 'discard-damaged') '--yes',
        ],
        environment: environment,
        includeParentEnvironment: false,
      );
      if (result.exitCode != 0 && action != 'status') {
        throw const IntegrationFailure('后台服务操作失败，请检查实际状态。');
      }
      final value = jsonDecode(result.stdout as String);
      if (value is! Map<String, dynamic>) {
        if (action == 'status') return UserServiceStatus.unknown;
        throw const IntegrationFailure('无法取得用户服务状态。');
      }
      final available = result.exitCode == 0 && value['supported'] == true;
      return UserServiceStatus(
        available: available,
        directory: value['registered'] == true
            ? value['directory'] as String
            : null,
        running: value['running'] as bool,
        enabled: value['enabled'] as bool,
        linger: value['linger'] as bool,
        matchesInstallation: value['directory'] == _directory,
        recoveryPending: value['recoveryPending'] == true,
        recoveryBlocked: value['recoveryBlocked'] == true,
        canDiscardDamaged: value['canDiscardDamaged'] == true,
        canDiscardReplacement: value['canDiscardReplacement'] == true,
        registrationKnown: available || value['registrationKnown'] == true,
      );
    } on IntegrationFailure {
      rethrow;
    } on Object {
      if (action == 'status') return UserServiceStatus.unknown;
      throw const IntegrationFailure('无法取得用户服务状态。');
    }
  }

  String get _namespace => environment['SIDRAVIA_NAMESPACE'] ?? '';
  String _normalRoot(String path) => File.fromUri(
    Directory(path).absolute.uri.normalizePath().resolve('__path_anchor__'),
  ).parent.path;
  String get _directory => _normalRoot(File(executable).parent.path);
  String? get _packageManager {
    for (final manager in const ['rpm', 'deb']) {
      final marker = 'sidravia-$manager-v1\n';
      final path = '$_directory/sidravia.$manager';
      if (FileSystemEntity.typeSync(path, followLinks: false) ==
              FileSystemEntityType.file &&
          File(path).lengthSync() == marker.length &&
          File(path).readAsStringSync() == marker) {
        return manager;
      }
    }
    return null;
  }

  @override
  bool get supportsUpdateCheck {
    try {
      // The sibling CTL resolves the installation-specific update policy.
      return !_closed && File('$_directory/sidraviactl').existsSync();
    } on FileSystemException {
      return false;
    }
  }

  @override
  bool get usesSystemPackageManager {
    try {
      return _packageManager != null;
    } on FileSystemException {
      return false;
    }
  }

  @override
  Future<PackageUpdate> checkUpdate() async {
    _checkOpen();
    if (!supportsUpdateCheck) {
      throw const IntegrationFailure('当前无法检查更新。');
    }
    try {
      final (:code, :output) = await _updateCheck.run(
        '$_directory/sidraviactl',
        environment: environment,
      );
      if (_closed) throw const IntegrationFailure('检查更新已取消。');
      if (output.isEmpty) {
        throw const IntegrationFailure('无法运行更新检查，请检查安装状态后重试。');
      }
      return decodeUpdateCheckResponse(code, output);
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法运行更新检查，请检查安装是否完整。');
    }
  }

  @override
  Future<UpdateInstallResult> installUpdate(String candidateId) {
    _checkOpen();
    if (usesSystemPackageManager) {
      throw const IntegrationFailure('请使用系统包管理器更新。');
    }
    return _updateCheck.install(
      executable: '$_directory/sidraviactl',
      downloader: '$_directory/sidraviaupdater',
      candidateId: candidateId,
      environment: environment,
    );
  }

  @override
  Future<void> openReleasePage(String version) async {
    _checkOpen();
    const failure = IntegrationFailure('无法打开浏览器。');
    final url = releasePageUrl(version);
    if (url == null) throw failure;
    try {
      final result = await Process.run(
        '/usr/bin/xdg-open',
        [url],
        environment: environment,
        includeParentEnvironment: false,
      );
      if (result.exitCode != 0) throw failure;
    } on ProcessException {
      throw failure;
    }
  }

  String get _configRoot {
    final override = linuxUserConfigOverride(environment);
    return override == null ? '$_home/.config' : _normalRoot(override);
  }

  String get _home {
    final home = linuxUserHome(environment);
    if (home == null) {
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
    if (validateProductNamespace(_namespace) == null ||
        !File(executable).isAbsolute) {
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

  String get _iconDirectory => Directory('$_directory/gui-runtime').existsSync()
      ? '$_directory/gui-runtime'
      : _directory;

  String _header() =>
      '[Desktop Entry]\nType=Application\nName=Sidravia\n'
      'Comment=Sidravia session autostart\nIcon=${_iconDirectory.replaceAll('\\', '\\\\')}/data/icons/sidravia.png\nTerminal=false\n';
  String _entry(String mode, {bool guarded = true, bool legacyHidden = false}) {
    final name = mode == 'gui' ? executable.split('/').last : 'sidraviactl';
    // GNU env treats any NAME=VALUE operand as an assignment, even when it
    // contains '/'. Use a chdir argument so '=' in a portable directory is safe.
    if (name.contains('=')) {
      throw const IntegrationFailure('自启程序文件名不能含等号，请恢复程序原名。');
    }
    final args = mode == 'gui'
        ? (legacyHidden ? ' --start-hidden' : '')
        : ' daemon start --log-level info';
    final tryExec = guarded && (_packageManager != null)
        ? 'TryExec=${_directory.replaceAll('\\', '\\\\')}/$name\n'
        : '';
    return '${_header()}${tryExec}Exec=${_quote('/usr/bin/env')} --chdir ${_quote(_directory)} ${_quote('SIDRAVIA_NAMESPACE=$_namespace')} ${_quote('./$name')}$args\n'
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
    if (existing == _entry('gui') ||
        existing == _entry('gui', guarded: false) ||
        existing == _entry('gui', legacyHidden: true) ||
        existing == _entry('gui', guarded: false, legacyHidden: true) ||
        existing == _legacyGui) {
      return 'gui';
    }
    if (existing == _entry('daemon') ||
        existing == _entry('daemon', guarded: false)) {
      return 'daemon';
    }
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

  // Lock the existing directory inode, so releasing the descriptor leaves
  // no lock file and cannot split contenders across two lock file inodes.
  T _locked<T>(String directory, T Function() operation) {
    _ensureDirectory(Directory(directory));
    final libc = DynamicLibrary.process();
    final open = libc
        .lookupFunction<
          Pointer<Void> Function(Pointer<Utf8>),
          Pointer<Void> Function(Pointer<Utf8>)
        >('opendir');
    final close = libc
        .lookupFunction<
          Int Function(Pointer<Void>),
          int Function(Pointer<Void>)
        >('closedir');
    final descriptor = libc
        .lookupFunction<
          Int Function(Pointer<Void>),
          int Function(Pointer<Void>)
        >('dirfd');
    final flock = libc
        .lookupFunction<Int Function(Int, Int), int Function(int, int)>(
          'flock',
        );
    final handle = using(
      (arena) => open(directory.toNativeUtf8(allocator: arena)),
    );
    if (handle == nullptr) {
      throw const IntegrationFailure('无法打开系统集成目录。');
    }
    try {
      const lockExclusive = 2;
      const lockNonblocking = 4;
      if (flock(descriptor(handle), lockExclusive | lockNonblocking) != 0) {
        throw const IntegrationFailure('无法取得系统集成锁，请稍后重试。');
      }
      return operation();
    } finally {
      close(handle);
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
    await _startupWork;
    try {
      var pathSupported = true;
      var pathEnabled = false;
      String pathNotice;
      try {
        if (_packageManager != null) {
          pathSupported = false;
          pathEnabled = true;
          pathNotice = '命令由系统软件包安装到 /usr/bin，请使用系统包管理器维护。';
        } else {
          final state = _pathState();
          pathEnabled = state.enabled;
          pathNotice = state.notice;
        }
      } on IntegrationFailure catch (error) {
        pathSupported = false;
        pathNotice = error.message;
      }
      final service = await _runService('status');
      String? startupError;
      var desktop = 'conflict';
      try {
        desktop = _mode(_existing());
      } on IntegrationFailure catch (error) {
        startupError = error.message;
      } on Object {
        startupError = '无法读取桌面自启登记。';
      }
      return IntegrationStatus(
        pathEnabled: pathEnabled,
        guiAvailable: _available(executable),
        autostart: _startupMode(desktop, service),
        userPathSupported: pathSupported,
        daemonAutostartSupported: _available('$_directory/sidraviactl'),
        pathNotice: pathNotice,
        userService: service,
        autostartNeedsRepair:
            service?.canControl == true &&
            (desktop == 'daemon' || (desktop == 'gui' && !service!.enabled)),
        autostartNotice:
            startupError ??
            (service?.registrationKnown == false
                ? '无法确认后台管理方式，请检查高级排障或重新读取。'
                : service?.canControl == true
                ? 'systemd 启动后台。选用图形界面时，登录桌面后也会打开界面。'
                : _available('$_directory/sidraviactl')
                ? '登录桌面会话后启动；仅后台模式没有托盘，可通过 CTL 停止。'
                : '包内 CTL 不存在或不可执行，无法设置仅后台自启。'),
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
        if (_packageManager != null) {
          throw const IntegrationFailure('系统软件包安装的命令由系统包管理器维护。');
        }
        _changePath(change == IntegrationChange.pathEnable);
        return;
      }
      final mode = switch (change) {
        IntegrationChange.startGui => 'gui',
        IntegrationChange.startDaemon => 'daemon',
        _ => 'disabled',
      };
      await _withStartupWork(() => _setStartup(mode));
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('Linux 系统集成修改失败，请刷新确认当前状态。');
    }
  }

  void _setDesktopStartup(
    String mode, {
    String? expected,
    bool checkExpected = false,
    String? restoreEntry,
  }) {
    final disabling = mode == 'disabled';
    final target = mode == 'gui' ? executable : '$_directory/sidraviactl';
    if (!disabling && !_available(target)) {
      throw const IntegrationFailure('启动程序不存在或不可执行。');
    }
    final manager = _packageManager;
    _locked(File(_path).parent.path, () {
      var removeNewTrackingOnError = false;
      try {
        final file = File(_path);
        final prefix = manager != null
            ? '.sidravia-autostart-$manager-'
            : '.sidravia-autostart-app-';
        final stagePrefix = '$prefix${file.uri.pathSegments.last}-';
        final noncePattern = RegExp(r'^[0-9a-f]{32}$');
        for (final candidate in file.parent.listSync(followLinks: false)) {
          final name = candidate.path.substring(file.parent.path.length + 1);
          if (!name.startsWith(stagePrefix) ||
              !noncePattern.hasMatch(name.substring(stagePrefix.length))) {
            continue;
          }
          final result = Process.runSync(
            '$_directory/sidraviactl',
            ['linux-recover-autostart-stage', '--', candidate.path],
            environment: environment,
            includeParentEnvironment: false,
          );
          if (result.exitCode != 0) {
            throw const IntegrationFailure('无法清理桌面自启暂存目录。');
          }
        }
        final existing = _existing();
        if (checkExpected && existing != expected) {
          throw const IntegrationFailure('自启登记已变化，请重新选择。');
        }
        if (_mode(existing) == 'conflict') {
          throw const IntegrationFailure('自启项属于其他程序或安装，未修改。');
        }
        if (disabling) {
          if (existing != null) file.deleteSync();
          if (manager != null) _trackPackageAutostart(manager, false);
          return;
        }
        final contents = restoreEntry ?? _entry(mode);
        if (manager != null) {
          _trackPackageAutostart(manager, true);
          removeNewTrackingOnError = existing == null;
        }
        if (existing == contents) return;
        // A surviving no-clobber mv can only consume this operation's entry.
        final random = Random.secure();
        final nonce = List.generate(
          16,
          (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'),
        ).join();
        final stage = Directory('${file.parent.path}/$stagePrefix$nonce');
        final created = Process.runSync('/usr/bin/mkdir', [
          '-m',
          '700',
          '--',
          stage.path,
        ]);
        if (created.exitCode != 0) {
          throw const IntegrationFailure('无法创建桌面自启暂存目录。');
        }
        final marker = File('${stage.path}/.sidravia-stage-v1');
        try {
          marker.createSync(exclusive: true);
          final staged = File('${stage.path}/entry');
          staged.writeAsStringSync(contents, flush: true);
          if (existing == null) {
            // Keep publication on the same filesystem, with no replacement
            // or traversal if another writer created the destination.
            final result = Process.runSync('/usr/bin/mv', [
              '--no-clobber',
              '-T',
              '--',
              staged.path,
              _path,
            ]);
            if (result.exitCode != 0 || staged.existsSync()) {
              throw const IntegrationFailure('自启项已变化，未覆盖原文件。');
            }
            removeNewTrackingOnError = false;
          } else {
            if (_existing() != existing) {
              throw const IntegrationFailure('自启项已变化，未覆盖原文件。');
            }
            staged.renameSync(_path);
          }
        } finally {
          final staged = File('${stage.path}/entry');
          if (staged.existsSync()) staged.deleteSync();
          if (marker.existsSync()) marker.deleteSync();
          stage.deleteSync();
        }
      } on Object {
        if (removeNewTrackingOnError) _trackPackageAutostart(manager!, false);
        rethrow;
      }
    });
  }

  void _trackPackageAutostart(String manager, bool enable) {
    if (_configRoot == '$_home/.config') return;
    final failure = IntegrationFailure(
      enable ? '无法登记桌面自启目录。' : '无法清理桌面自启目录登记。修复安装后可重试关闭。',
    );
    try {
      final result = Process.runSync(
        '$_directory/sidraviactl',
        [
          enable ? '$manager-track-autostart' : '$manager-untrack-autostart',
          '--',
          _configRoot,
        ],
        environment: environment,
        includeParentEnvironment: false,
      );
      if (result.exitCode != 0) throw failure;
    } on ProcessException {
      throw failure;
    }
  }

  @override
  Future<void> close() async {
    _closed = true;
    await _updateCheck.cancel();
    await _startupWork;
  }
}
