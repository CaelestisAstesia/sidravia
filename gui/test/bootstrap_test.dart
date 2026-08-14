import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';

void main() {
  const valid =
      '{"schemaVersion":1,"endpoint":"ws://127.0.0.1:4711/ipc","token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.2.0-dev","buildId":"fixture","daemonPid":42,"mode":"desktop"}';

  test('strictly decodes the machine bootstrap success document', () {
    final result = decodeGuiBootstrap(valid);

    expect(result.isSuccess, isTrue);
    expect(result.value?.endpoint.toString(), 'ws://127.0.0.1:4711/ipc');
    expect(result.value?.daemonPid, 42);
  });

  test('rejects malformed or unsafe success documents', () {
    for (final source in [
      valid.replaceFirst('}', ',"extra":true}'),
      valid.replaceFirst('127.0.0.1', 'example.test'),
      valid.replaceFirst('0123', 'ABCD'),
      valid.replaceFirst('ws://', 'ws://name@'),
      valid.replaceFirst('"desktop"', '"headless"'),
    ]) {
      expect(decodeGuiBootstrap(source).isSuccess, isFalse);
    }
  });

  test('accepts all eight ADR 0047 error mappings only', () {
    const mappings = <String, int>{
      'invalid_arguments': 2,
      'invalid_client_identity': 10,
      'unsupported_platform': 11,
      'mode_conflict': 12,
      'incompatible_build': 13,
      'startup_unconfirmed': 14,
      'bootstrap_failed': 15,
      'output_failed': 16,
    };
    for (final entry in mappings.entries) {
      final failure = guiBootstrapFailures[entry.key]!;
      final document = jsonEncode({
        'schemaVersion': 1,
        'error': {'code': failure.code, 'message': failure.message},
      });
      expect(decodeGuiBootstrapFailure(document, entry.value).code, entry.key);
      expect(decodeGuiBootstrapFailure(document, 1).code, 'bootstrap_failed');
    }
    expect(
      decodeGuiBootstrapFailure('{"schemaVersion":1,"error":{}}', 15).code,
      'bootstrap_failed',
    );
  });

  test('uses sibling control executable, exact argv, and owner PID', () async {
    final child = _Process.completed(0, stdout: utf8.encode(valid));
    String? executable;
    List<String>? arguments;
    final bootstrap = ProcessGuiBootstrap(
      isWindows: () => true,
      resolvedExecutable: () =>
          '${Platform.pathSeparator}fixture${Platform.pathSeparator}Sidravia.exe',
      ownerPid: () => 77,
      launch: (path, argv) async {
        executable = path;
        arguments = argv;
        return child;
      },
    );

    final result = await bootstrap.bootstrap();

    expect(result.isSuccess, isTrue);
    expect(File(executable!).parent.path, endsWith('fixture'));
    expect(File(executable!).uri.pathSegments.last, 'sidraviactl.exe');
    expect(arguments, ['gui', 'bootstrap', '--owner-pid', '77']);
  });

  test('bounds output, kills a live child, and settles both readers', () async {
    final child = _Process.completed(0, stdout: List<int>.filled(5, 1));
    final result = await ProcessGuiBootstrap(
      isWindows: () => true,
      maximumOutputBytes: 4,
      launch: (_, _) async => child,
    ).bootstrap();

    expect(result.failure?.code, 'bootstrap_failed');
    expect(child.killed, isFalse);

    final timedOut = _Process.pending();
    final timedResult = await ProcessGuiBootstrap(
      isWindows: () => true,
      timeout: Duration.zero,
      launch: (_, _) async => timedOut,
    ).bootstrap();
    expect(timedResult.failure?.code, 'bootstrap_failed');
    expect(timedOut.killed, isTrue);
    expect(timedOut.readersClosed, isTrue);
  });

  test('waits for exit and both stream readers before returning', () async {
    final child = _Process.pending();
    final pending = ProcessGuiBootstrap(
      isWindows: () => true,
      launch: (_, _) async => child,
    ).bootstrap();
    child.exits.complete(0);
    await Future<void>.delayed(Duration.zero);
    expect(child.readersClosed, isFalse);
    child.stdoutController.add(utf8.encode(valid));
    await child.closeReaders();
    expect((await pending).isSuccess, isTrue);
  });

  test('nonzero exit discards stdout and decodes only strict stderr', () async {
    final child = _Process.completed(
      12,
      stdout: utf8.encode(valid),
      stderr: utf8.encode(
        '{"schemaVersion":1,"error":{"code":"mode_conflict","message":"另一运行模式正在使用 daemon"}}',
      ),
    );
    final result = await ProcessGuiBootstrap(
      isWindows: () => true,
      launch: (_, _) async => child,
    ).bootstrap();
    expect(result.failure?.code, 'mode_conflict');
  });

  test(
    'non-Windows bootstrap returns only the stable Unsupported state',
    () async {
      if (Platform.isWindows) return;
      final result = await ProcessGuiBootstrap().bootstrap();

      expect(result.failure?.code, 'unsupported_platform');
    },
  );
}

class _Process implements GuiBootstrapProcess {
  _Process.pending()
    : exits = Completer<int>(),
      stdoutController = StreamController<List<int>>(),
      stderrController = StreamController<List<int>>();

  _Process.completed(int exit, {List<int>? stdout, List<int>? stderr})
    : exits = Completer<int>()..complete(exit),
      stdoutController = StreamController<List<int>>(),
      stderrController = StreamController<List<int>>() {
    if (stdout != null) stdoutController.add(stdout);
    if (stderr != null) stderrController.add(stderr);
    unawaited(closeReaders());
  }

  final Completer<int> exits;
  final StreamController<List<int>> stdoutController;
  final StreamController<List<int>> stderrController;
  var killed = false;

  bool get readersClosed =>
      stdoutController.isClosed && stderrController.isClosed;

  @override
  Stream<List<int>> get stdout => stdoutController.stream;
  @override
  Stream<List<int>> get stderr => stderrController.stream;
  @override
  Future<int> get exitCode => exits.future;

  Future<void> closeReaders() async {
    await stdoutController.close();
    await stderrController.close();
  }

  @override
  bool kill() {
    killed = true;
    if (!exits.isCompleted) exits.complete(-1);
    unawaited(closeReaders());
    return true;
  }
}
