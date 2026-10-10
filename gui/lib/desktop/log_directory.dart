import 'dart:async';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';

/// Opens only this instance's directory, resolved by the sibling CLI.
class DesktopLogDirectory {
  DesktopLogDirectory({
    bool Function()? isWindows,
    String Function()? resolvedExecutable,
    GuiBootstrapLauncher? launch,
    this.timeout = const Duration(seconds: 10),
  }) : _isWindows = isWindows ?? (() => Platform.isWindows),
       _executable = resolvedExecutable ?? (() => Platform.resolvedExecutable),
       _launch = launch ?? _start;

  final bool Function() _isWindows;
  final String Function() _executable;
  final GuiBootstrapLauncher _launch;
  final Duration timeout;
  Future<bool>? _work;
  Completer<void>? _cancel;
  bool _closed = false;

  bool get supported => !_closed && _isWindows();

  static Future<GuiBootstrapProcess> _start(
    String executable,
    List<String> args,
  ) async => _LogProcess(
    await Process.start(
      executable,
      args,
      runInShell: false,
      includeParentEnvironment: true,
    ),
  );

  Future<bool> open() {
    if (!supported) return Future.value(false);
    return _work ??= _run().whenComplete(() {
      _work = null;
    });
  }

  Future<bool> _run() async {
    final cancel = _cancel = Completer<void>();
    void stop() {
      if (!cancel.isCompleted) cancel.complete();
    }

    final timer = Timer(timeout, stop);
    GuiBootstrapProcess? child;
    Future<int>? exit;
    Future<void>? output;
    Future<void>? error;
    var completed = false;
    var streamFailed = false;
    Future<void> drain(Stream<List<int>> stream) async {
      try {
        await stream.drain<void>();
      } on Object {
        streamFailed = true;
        stop();
      }
    }

    try {
      final executable =
          '${File(_executable()).parent.path}${Platform.pathSeparator}sidraviactl.exe';
      child = await _launch(executable, const ['gui', 'open-log-directory']);
      exit = child.exitCode;
      output = drain(child.stdout);
      error = drain(child.stderr);
      if (cancel.isCompleted || _closed) return false;
      final result = await Future.any<bool>([
        Future.wait<Object?>([exit, output, error]).then((values) {
          completed = true;
          return values.first == 0 && !streamFailed;
        }),
        cancel.future.then((_) => false),
      ]);
      return result && !_closed;
    } on Object {
      return false;
    } finally {
      timer.cancel();
      if (child != null && !completed) child.kill();
      if (exit != null) {
        try {
          await exit;
        } on Object {
          /* safe failure */
        }
      }
      if (output != null) await output;
      if (error != null) await error;
      if (identical(_cancel, cancel)) _cancel = null;
    }
  }

  Future<void> close() async {
    _closed = true;
    final cancel = _cancel;
    if (cancel != null && !cancel.isCompleted) cancel.complete();
    await _work;
  }
}

class _LogProcess implements GuiBootstrapProcess {
  _LogProcess(this.process);
  final Process process;
  @override
  Stream<List<int>> get stdout => process.stdout;
  @override
  Stream<List<int>> get stderr => process.stderr;
  @override
  Future<int> get exitCode => process.exitCode;
  @override
  bool kill() => process.kill();
}
