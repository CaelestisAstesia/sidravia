import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../integration_failure.dart';
import '../update/package_update_checker.dart';
import '../update/update_check_process.dart';
import '../update/update_check_response.dart';
import '../update/update_release_version.dart';
import 'installation_manager.dart';

class WindowsInstallationManager
    implements InstallationManager, PackageUpdateChecker {
  WindowsInstallationManager(this._updateCheck);

  final UpdateCheckProcess _updateCheck;
  Future<String>? _pending;
  Completer<void>? _cancellation;
  bool _closed = false;

  @override
  bool get supportsUpdateCheck => !_closed;

  @override
  bool get usesSystemPackageManager => false;

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
    const failure = IntegrationFailure('无法打开浏览器。');
    if (_closed) throw failure;
    final url = releasePageUrl(version);
    if (url == null) throw failure;
    try {
      final result = await Process.run('cmd.exe', ['/c', 'start', '', url]);
      if (result.exitCode != 0) throw failure;
    } on ProcessException {
      throw failure;
    }
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
    if (_pending != null || _closed) {
      throw const IntegrationFailure('维护操作正在进行或已取消。');
    }
    final cancellation = Completer<void>();
    _cancellation = cancellation;
    final operation = _invoke(args, cancellation.future);
    _pending = operation;
    try {
      return await operation;
    } finally {
      _pending = null;
      _cancellation = null;
    }
  }

  Future<String> _invoke(List<String> args, Future<void> cancellation) async {
    Process? child;
    StreamIterator<String>? stdout;
    StreamIterator<List<int>>? stderr;
    Future<int>? exit;
    var exited = false;
    try {
      final executable = File(Platform.resolvedExecutable).parent.uri
          .resolve('sidraviamgr.exe')
          .toFilePath();
      child = await Process.start(executable, args, runInShell: false);
      stdout = StreamIterator(child.stdout.transform(utf8.decoder));
      stderr = StreamIterator(child.stderr);
      exit = child.exitCode.then((code) {
        exited = true;
        return code;
      });
      final output = StringBuffer();
      await Future.any<void>([
        cancellation.then<void>((_) {
          throw const IntegrationFailure('维护操作已取消。');
        }),
        Future.wait<void>([
          exit.then<void>((_) {}),
          child.stdin.close().then<void>((_) {}),
          _collect(stdout, output),
          _drain(stderr),
        ], eagerError: true),
      ]).timeout(
        const Duration(seconds: 45),
        onTimeout: () => throw const IntegrationFailure('维护操作超时，请刷新状态。'),
      );
      final result = output.toString();
      if (await exit != 0) {
        decodeManagerResponse(result);
        throw const IntegrationFailure('维护操作未成功完成。');
      }
      return result;
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法运行维护工具，请检查安装是否完整。');
    } finally {
      if (!exited) child?.kill();
      try {
        await Future.wait<void>([
          if (stdout != null) stdout.cancel(),
          if (stderr != null) stderr.cancel(),
          if (exit != null) exit.then<void>((_) {}),
        ]).timeout(const Duration(seconds: 2));
      } on Object {
        // Cleanup must preserve the original operation result and deadline.
      }
    }
  }

  Future<void> _collect(
    StreamIterator<String> stream,
    StringBuffer output,
  ) async {
    while (await stream.moveNext()) {
      final chunk = stream.current;
      if (output.length + chunk.length > 65536) {
        throw const IntegrationFailure('维护工具返回了过大的结果。');
      }
      output.write(chunk);
    }
  }

  Future<void> _drain(StreamIterator<List<int>> stream) async {
    while (await stream.moveNext()) {}
  }

  @override
  Future<void> close() async {
    if (!_closed) {
      _closed = true;
      _cancellation?.complete();
    }
    final cancelledUpdate = _updateCheck.cancel();
    try {
      await _pending;
    } on IntegrationFailure {
      // The operation's caller receives the original result.
    }
    await cancelledUpdate;
  }
}
