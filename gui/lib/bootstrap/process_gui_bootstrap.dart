import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../platform/desktop_process.dart';
import '../platform/paths/linux_product_executable.dart';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';

class ProcessGuiBootstrap implements GuiBootstrapper, GuiCoreRestarter {
  ProcessGuiBootstrap({
    this.logLevel,
    this.skipAutoLogin = false,
    this.timeout = const Duration(seconds: 10),
    this.maximumOutputBytes = 64 * 1024,
    bool Function()? isWindows,
    bool Function()? isLinux,
    String Function()? resolvedExecutable,
    int Function()? ownerPid,
    GuiBootstrapLauncher? launch,
  }) : _isWindows = isWindows ?? (() => Platform.isWindows),
       _isLinux = isLinux ?? (() => isWindows == null && Platform.isLinux),
       _resolvedExecutable =
           resolvedExecutable ?? (() => Platform.resolvedExecutable),
       _ownerPid = ownerPid ?? (() => pid),
       _launch = launch ?? _defaultLaunch;
  final String Function()? logLevel;
  final Duration timeout;
  final bool skipAutoLogin;
  final int maximumOutputBytes;
  final bool Function() _isWindows;
  final bool Function() _isLinux;
  final String Function() _resolvedExecutable;
  final int Function() _ownerPid;
  final GuiBootstrapLauncher _launch;

  static Future<GuiBootstrapProcess> _defaultLaunch(
    String executable,
    List<String> arguments,
  ) async {
    final environment = Map<String, String>.of(Platform.environment);
    if (Platform.isLinux) {
      final stat = await File('/proc/self/stat').readAsString();
      final fields = stat.substring(stat.lastIndexOf(')') + 2).split(' ');
      environment['SIDRAVIA_DESKTOP_OWNER_START'] = fields[19];
    }
    return SystemDesktopProcess(
      await Process.start(executable, arguments, environment: environment),
    );
  }

  @override
  Future<GuiBootstrapResult> bootstrap() => _run('bootstrap', logLevel?.call());
  @override
  Future<GuiBootstrapResult> restartCore(String level) =>
      _run('restart', level);
  Future<GuiBootstrapResult> _run(String operation, String? level) async {
    if (!_isWindows() && !_isLinux()) {
      return const GuiBootstrapResult.failure(
        GuiBootstrapFailure.unsupportedPlatform,
      );
    }
    GuiBootstrapProcess? child;
    StreamIterator<List<int>>? out;
    StreamIterator<List<int>>? err;
    Future<int>? exitCode;
    var exited = false;
    try {
      final executable = desktopCtlPath(
        _isLinux()
            ? linuxProductExecutable(_resolvedExecutable())
            : _resolvedExecutable(),
        windows: _isWindows(),
      );
      child = await _launch(executable, [
        'gui',
        operation,
        if (skipAutoLogin && operation == 'bootstrap') '--skip-auto-login',
        if (level != null) ...['--log-level', level],
        '--owner-pid',
        '${_ownerPid()}',
      ]);
      out = StreamIterator(child.stdout);
      err = StreamIterator(child.stderr);
      exitCode = child.exitCode.then((code) {
        exited = true;
        return code;
      });
      // Observe all three futures immediately. Either pipe may fail while the
      // process is alive, and its exit alone does not guarantee both pipes end.
      final result = await Future.wait<Object>(
        [exitCode, _readBounded(out), _readBounded(err)],
        eagerError: true,
      ).timeout(operation == 'restart' ? const Duration(seconds: 30) : timeout);
      final exit = result[0] as int;
      final stdout = result[1] as List<int>;
      final stderr = result[2] as List<int>;
      if (exit != 0) {
        return GuiBootstrapResult.failure(
          decodeGuiBootstrapFailure(
            utf8.decode(stderr, allowMalformed: false),
            exit,
          ),
        );
      }
      return decodeGuiBootstrap(utf8.decode(stdout, allowMalformed: false));
    } on Object {
      if (child != null && !exited) child.kill();
      try {
        await Future.wait<void>([
          if (out != null) out.cancel(),
          if (err != null) err.cancel(),
          if (exitCode != null) exitCode.then<void>((_) {}),
        ]).timeout(const Duration(seconds: 2));
      } on Object {
        // Cleanup errors must not replace the safe failure or stall the UI.
      }
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
  }

  Future<List<int>> _readBounded(StreamIterator<List<int>> reader) async {
    final bytes = <int>[];
    while (await reader.moveNext()) {
      final chunk = reader.current;
      if (bytes.length + chunk.length > maximumOutputBytes) {
        throw const FormatException();
      }
      bytes.addAll(chunk);
    }
    return bytes;
  }
}
