import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../installation/installation_manager.dart';

/// Owns the one CTL child used to check update availability.
class UpdateCheckProcess {
  static const _outputLimit = 2 << 20;

  Process? _child;
  Future<({int code, String output})>? _pending;
  bool _cancelled = false;
  bool _closed = false;

  Future<({int code, String output})> run(
    String executable, {
    Map<String, String>? environment,
  }) async {
    if (_closed) throw const IntegrationFailure('检查更新已取消。');
    if (_pending != null) {
      throw const IntegrationFailure('正在执行另一项更新操作，请稍后重试。');
    }
    _cancelled = false;
    final operation = _run(executable, environment);
    _pending = operation;
    try {
      return await operation;
    } finally {
      _pending = null;
    }
  }

  Future<({int code, String output})> _run(
    String executable,
    Map<String, String>? environment,
  ) async {
    Timer? timer;
    try {
      final child = await Process.start(
        executable,
        const ['update', 'check'],
        environment: environment,
        includeParentEnvironment: environment == null,
      );
      _child = child;
      if (_cancelled) child.kill();
      var timedOut = false;
      var tooLarge = false;
      final bytes = BytesBuilder(copy: false);
      timer = Timer(const Duration(seconds: 50), () {
        timedOut = true;
        child.kill();
      });
      final (code, _, _, _) = await (
        child.exitCode,
        child.stdin.close(),
        child.stdout.forEach((chunk) {
          if (tooLarge) return;
          if (bytes.length + chunk.length > _outputLimit) {
            tooLarge = true;
            child.kill();
            return;
          }
          bytes.add(chunk);
        }),
        child.stderr.drain<void>(),
      ).wait;
      if (_cancelled) throw const IntegrationFailure('检查更新已取消。');
      if (timedOut) throw const IntegrationFailure('检查更新超时，请重试。');
      if (tooLarge) throw const IntegrationFailure('更新检查返回了过大的结果。');
      return (code: code, output: utf8.decode(bytes.takeBytes()));
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw const IntegrationFailure('无法运行更新检查，请检查安装是否完整。');
    } finally {
      timer?.cancel();
      _child = null;
    }
  }

  Future<void> cancel() async {
    final operation = _pending;
    if (operation == null) return;
    _cancelled = true;
    _child?.kill();
    try {
      await operation;
    } on IntegrationFailure {
      // The caller of run receives the result. Cancellation waits for exit.
    }
  }

  Future<void> close() {
    _closed = true;
    return cancel();
  }
}
