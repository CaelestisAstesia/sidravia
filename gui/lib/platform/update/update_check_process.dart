import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../integration_failure.dart';

/// Owns the one CTL child used to check update availability.
class UpdateCheckProcess {
  static const _outputLimit = 2 << 20;

  Future<({int code, String output})>? _pending;
  Completer<void>? _cancellation;
  bool _closed = false;

  Future<({int code, String output})> run(
    String executable, {
    Map<String, String>? environment,
  }) async {
    if (_closed) throw const IntegrationFailure('检查更新已取消。');
    if (_pending != null) {
      throw const IntegrationFailure('正在执行另一项更新操作，请稍后重试。');
    }
    final cancellation = Completer<void>();
    _cancellation = cancellation;
    final operation = _run(executable, environment, cancellation);
    _pending = operation;
    try {
      return await operation;
    } finally {
      _pending = null;
      _cancellation = null;
    }
  }

  Future<({int code, String output})> _run(
    String executable,
    Map<String, String>? environment,
    Completer<void> cancellation,
  ) async {
    Process? child;
    StreamIterator<List<int>>? stdout;
    StreamIterator<List<int>>? stderr;
    Future<int>? exitCode;
    var exited = false;
    try {
      child = await Process.start(
        executable,
        const ['update', 'check'],
        environment: environment,
        includeParentEnvironment: environment == null,
      );
      stdout = StreamIterator(child.stdout);
      stderr = StreamIterator(child.stderr);
      exitCode = child.exitCode.then((code) {
        exited = true;
        return code;
      });
      final bytes = BytesBuilder(copy: false);
      await Future.any<void>([
        cancellation.future.then<void>((_) {
          throw const IntegrationFailure('检查更新已取消。');
        }),
        Future.wait<void>([
          exitCode.then<void>((_) {}),
          child.stdin.close().then<void>((_) {}),
          _readOutput(stdout, bytes),
          _discardOutput(stderr),
        ], eagerError: true),
      ]).timeout(
        const Duration(seconds: 50),
        onTimeout: () => throw const IntegrationFailure('检查更新超时，请重试。'),
      );
      return (code: await exitCode, output: utf8.decode(bytes.takeBytes()));
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法运行更新检查，请检查安装是否完整。');
    } finally {
      if (!exited) child?.kill();
      try {
        await Future.wait<void>([
          if (stdout != null) stdout.cancel(),
          if (stderr != null) stderr.cancel(),
          if (exitCode != null) exitCode.then<void>((_) {}),
        ]).timeout(const Duration(seconds: 2));
      } on Object {
        // Keep the original result and bound the wait for process cleanup.
      }
    }
  }

  Future<void> _readOutput(
    StreamIterator<List<int>> stream,
    BytesBuilder bytes,
  ) async {
    while (await stream.moveNext()) {
      final chunk = stream.current;
      if (bytes.length + chunk.length > _outputLimit) {
        throw const IntegrationFailure('更新检查返回了过大的结果。');
      }
      bytes.add(chunk);
    }
  }

  Future<void> _discardOutput(StreamIterator<List<int>> stream) async {
    while (await stream.moveNext()) {}
  }

  Future<void> cancel() async {
    final operation = _pending;
    final cancellation = _cancellation;
    if (operation == null || cancellation == null) return;
    if (!cancellation.isCompleted) cancellation.complete();
    try {
      await operation;
    } on IntegrationFailure {
      // The caller of run receives the result after bounded cleanup.
    }
  }

  Future<void> close() {
    _closed = true;
    return cancel();
  }
}
