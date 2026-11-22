import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../platform/linux_product_executable.dart';

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
    Future<List<int>>? out;
    Future<List<int>>? err;
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
      out = _readBounded(child.stdout);
      err = _readBounded(child.stderr);
      final exit = await child.exitCode.timeout(
        operation == 'restart' ? const Duration(seconds: 30) : timeout,
      );
      exited = true;
      final stdout = await out;
      final stderr = await err;
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
      if (child != null) {
        await child.exitCode.catchError((_) => -1);
      }
      await _settle(out);
      await _settle(err);
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
  }

  Future<void> _settle(Future<List<int>>? value) async {
    if (value == null) return;
    try {
      await value;
    } on Object {
      // A reader can fail only after the fixed safe failure is selected.
    }
  }

  Future<List<int>> _readBounded(Stream<List<int>> stream) async {
    final bytes = <int>[];
    await for (final chunk in stream) {
      if (bytes.length + chunk.length > maximumOutputBytes) {
        throw const FormatException();
      }
      bytes.addAll(chunk);
    }
    return bytes;
  }
}
