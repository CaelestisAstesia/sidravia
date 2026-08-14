import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';

abstract interface class GuiBootstrapProcess {
  Stream<List<int>> get stdout;
  Stream<List<int>> get stderr;
  Future<int> get exitCode;
  bool kill();
}

typedef GuiBootstrapLauncher = Future<GuiBootstrapProcess> Function(
  String executable,
  List<String> arguments,
);

class ProcessGuiBootstrap implements GuiBootstrapper {
  ProcessGuiBootstrap({
    this.timeout = const Duration(seconds: 10),
    this.maximumOutputBytes = 64 * 1024,
    bool Function()? isWindows,
    String Function()? resolvedExecutable,
    int Function()? ownerPid,
    GuiBootstrapLauncher? launch,
  }) : _isWindows = isWindows ?? (() => Platform.isWindows),
       _resolvedExecutable =
           resolvedExecutable ?? (() => Platform.resolvedExecutable),
       _ownerPid = ownerPid ?? (() => pid),
       _launch = launch ?? _defaultLaunch;
  final Duration timeout;
  final int maximumOutputBytes;
  final bool Function() _isWindows;
  final String Function() _resolvedExecutable;
  final int Function() _ownerPid;
  final GuiBootstrapLauncher _launch;

  static Future<GuiBootstrapProcess> _defaultLaunch(
    String executable,
    List<String> arguments,
  ) async => _SystemProcess(await Process.start(executable, arguments));

  @override
  Future<GuiBootstrapResult> bootstrap() async {
    if (!_isWindows()) {
      return const GuiBootstrapResult.failure(
        GuiBootstrapFailure.unsupportedPlatform,
      );
    }
    GuiBootstrapProcess? child;
    Future<List<int>>? out;
    Future<List<int>>? err;
    var exited = false;
    try {
      final executable =
          '${File(_resolvedExecutable()).parent.path}${Platform.pathSeparator}sidraviactl.exe';
      child = await _launch(executable, [
        'gui',
        'bootstrap',
        '--owner-pid',
        '${_ownerPid()}',
      ]);
      out = _readBounded(child.stdout);
      err = _readBounded(child.stderr);
      final exit = await child.exitCode.timeout(timeout);
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

class _SystemProcess implements GuiBootstrapProcess {
  _SystemProcess(this.value);
  final Process value;
  @override
  Stream<List<int>> get stdout => value.stdout;
  @override
  Stream<List<int>> get stderr => value.stderr;
  @override
  Future<int> get exitCode => value.exitCode;
  @override
  bool kill() => value.kill();
}
