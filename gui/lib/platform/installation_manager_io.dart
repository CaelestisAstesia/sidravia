import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'installation_manager.dart';
import 'linux_installation_manager.dart';
import 'installation_manager_stub.dart' as stub;
import 'update_check_process.dart';
import 'update_check_response.dart';

InstallationManager createManager({UpdateCheckProcess? updateCheck}) =>
    Platform.isWindows
    ? _ProcessManager(updateCheck ?? UpdateCheckProcess())
    : Platform.isLinux
    ? LinuxInstallationManager(
        executable: Platform.resolvedExecutable,
        environment: Platform.environment,
        updateCheck: updateCheck,
      )
    : stub.createManager();

class _ProcessManager implements InstallationManager, PackageUpdateChecker {
  _ProcessManager(this._updateCheck);

  final UpdateCheckProcess _updateCheck;
  Process? _process;
  bool _busy = false, _closed = false;

  @override
  bool get supportsUpdateCheck => !_closed;

  @override
  Future<PackageUpdate> checkUpdate() async {
    if (_closed) throw const IntegrationFailure('检查更新已取消。');
    final executable = File(Platform.resolvedExecutable).parent.uri
        .resolve('sidraviactl.exe')
        .toFilePath();
    final (:code, :output) = await _updateCheck.run(executable);
    if (_closed) throw const IntegrationFailure('检查更新已取消。');
    if (output.isEmpty) {
      throw const IntegrationFailure('无法运行更新检查，请检查安装是否完整。');
    }
    try {
      return decodeUpdateCheckResponse(code, output);
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法检查更新，请检查安装是否完整。');
    }
  }

  @override
  Future<void> openReleasePage(String version) async {
    if (!RegExp(r'^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$').hasMatch(version)) {
      throw const IntegrationFailure('无法打开浏览器。');
    }
    final result = await Process.run('cmd.exe', [
      '/c',
      'start',
      '',
      'https://github.com/CaelestisAstesia/sidravia/releases/tag/v$version',
    ]);
    if (result.exitCode != 0) throw const IntegrationFailure('无法打开浏览器。');
  }

  @override
  Future<IntegrationStatus> status() async =>
      IntegrationStatus.decode(await _run(const ['status']));
  @override
  Future<void> change(IntegrationChange change) async {
    final args = switch (change) {
      IntegrationChange.pathEnable => const ['path', 'enable'],
      IntegrationChange.pathDisable => const ['path', 'disable'],
      IntegrationChange.startGui => const ['autostart', 'set', 'gui'],
      IntegrationChange.startDaemon => const ['autostart', 'set', 'daemon'],
      IntegrationChange.startDisable => const ['autostart', 'disable'],
    };
    decodeManagerResponse(await _run(args));
  }

  Future<String> _run(List<String> args) async {
    if (_busy || _closed) throw const IntegrationFailure('维护操作正在进行或已取消。');
    _busy = true;
    Process? child;
    Timer? timer;
    try {
      final executable = File(Platform.resolvedExecutable).parent.uri
          .resolve('sidraviamgr.exe')
          .toFilePath();
      child = await Process.start(executable, args, runInShell: false);
      _process = child;
      if (_closed) {
        child.kill();
        await child.exitCode;
        throw const IntegrationFailure('维护操作已取消。');
      }
      var timedOut = false, tooLarge = false;
      final output = StringBuffer();
      final stdoutDone = child.stdout.transform(utf8.decoder).forEach((chunk) {
        if (output.length + chunk.length > 65536) {
          tooLarge = true;
          child!.kill();
        } else {
          output.write(chunk);
        }
      });
      final stderrDone = child.stderr.drain<void>();
      timer = Timer(const Duration(seconds: 45), () {
        timedOut = true;
        child!.kill();
      });
      final code = await child.exitCode;
      await Future.wait([stdoutDone, stderrDone]);
      if (_closed) throw const IntegrationFailure('维护操作已取消。');
      if (timedOut) throw const IntegrationFailure('维护操作超时，请刷新状态。');
      if (tooLarge) throw const IntegrationFailure('维护工具返回了过大的结果。');
      if (code != 0) {
        decodeManagerResponse(output.toString());
        throw const IntegrationFailure('维护操作未成功完成。');
      }
      return output.toString();
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法运行维护工具，请检查安装是否完整。');
    } finally {
      timer?.cancel();
      if (child != null) {
        child.kill();
        await child.exitCode;
      }
      _process = null;
      _busy = false;
    }
  }

  @override
  Future<void> close() async {
    _closed = true;
    final cancelledUpdate = _updateCheck.cancel();
    final child = _process;
    if (child != null) {
      child.kill();
      await child.exitCode;
    }
    await cancelledUpdate;
  }
}
