import 'dart:convert';
import 'dart:io';

import 'gui_settings_storage_io.dart';
import 'installation_manager.dart';

/// Session autostart is owned by one exact executable/namespace. No shell,
/// system service, PATH edits, or daemon-only autostart are implied.
class LinuxInstallationManager implements InstallationManager {
  LinuxInstallationManager({
    required this.executable,
    required this.environment,
  });
  final String executable;
  final Map<String, String> environment;
  bool _closed = false;

  String get _namespace => environment['SIDRAVIA_NAMESPACE'] ?? '';
  String get _path {
    if (!validGuiNamespace(_namespace) || !File(executable).isAbsolute) {
      throw const IntegrationFailure('启动路径或命名空间无效。');
    }
    final xdg = environment['XDG_CONFIG_HOME'];
    final home = environment['HOME'];
    final root = xdg != null && Directory(xdg).isAbsolute
        ? xdg
        : home != null && Directory(home).isAbsolute
        ? '$home/.config'
        : null;
    if (root == null) throw const IntegrationFailure('无法确定 XDG 配置目录。');
    final suffix = _namespace.isEmpty
        ? ''
        : '-${base64Url.encode(utf8.encode(_namespace)).replaceAll('=', '')}';
    return '$root/autostart/io.github.CaelestisAstesia.Sidravia$suffix.desktop';
  }

  String _quote(String value) {
    // Desktop Entry Exec has its own escaping, with %% for field-code literals.
    if (value.contains(RegExp(r'[\x00-\x1f\x7f]'))) {
      throw const IntegrationFailure('启动路径包含不支持的字符。');
    }
    return '"${value.replaceAll('\\', '\\\\\\\\').replaceAll('"', '\\\\"').replaceAll('`', '\\\\`').replaceAll(r'$', r'\\$').replaceAll('%', '%%')}"';
  }

  String get _contents =>
      '[Desktop Entry]\nType=Application\nName=Sidravia\n'
      'Comment=Sidravia session autostart\nIcon=${File(executable).parent.path.replaceAll('\\', '\\\\')}/data/icons/sidravia.png\nTerminal=false\n'
      'Exec=${_quote('/usr/bin/env')} ${_quote('SIDRAVIA_NAMESPACE=$_namespace')} ${_quote(executable)} --start-hidden\n'
      'X-Sidravia-Owner=linux-gui-v1\n';

  String? _existing() {
    if (_closed) throw const IntegrationFailure('系统集成操作已取消。');
    final type = FileSystemEntity.typeSync(_path, followLinks: false);
    if (type == FileSystemEntityType.notFound) return null;
    if (type != FileSystemEntityType.file) {
      throw const IntegrationFailure('自启文件类型冲突，未修改。');
    }
    return File(_path).readAsStringSync();
  }

  @override
  Future<IntegrationStatus> status() async {
    try {
      final existing = _existing();
      return IntegrationStatus(
        pathEnabled: false,
        guiAvailable: File(executable).existsSync(),
        autostart: existing == null
            ? 'disabled'
            : existing == _contents
            ? 'gui'
            : 'conflict',
        userPathSupported: false,
        daemonAutostartSupported: false,
      );
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法读取 Linux 会话自启状态。');
    }
  }

  @override
  Future<void> change(IntegrationChange change) async {
    if (change != IntegrationChange.startGui &&
        change != IntegrationChange.startDisable) {
      throw const IntegrationFailure('Linux 暂不支持修改 PATH 或仅后台自启。');
    }
    try {
      final existing = _existing();
      if (existing != null && existing != _contents) {
        throw const IntegrationFailure('自启项属于其他程序或安装，未修改。');
      }
      final file = File(_path);
      if (change == IntegrationChange.startDisable) {
        if (existing != null) file.deleteSync();
        return;
      }
      if (!File(executable).existsSync()) {
        throw const IntegrationFailure('启动程序不存在。');
      }
      if (existing != null) return;
      file.parent.createSync(recursive: true);
      // Refuse existing entries atomically. A partial write remains a conflict
      // on refresh; it is never silently replaced or reported as enabled.
      file.createSync(exclusive: true);
      file.writeAsStringSync(_contents, flush: true);
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('Linux 会话自启修改失败，请刷新状态。');
    }
  }

  @override
  Future<void> close() async {
    _closed = true;
  }
}
