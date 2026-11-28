import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../integration_failure.dart';
import 'update_check_response.dart';

/// Owns one update operation for the application, including its download.
class UpdateCheckProcess {
  static const _outputLimit = 2 << 20;

  Future<Object?>? _pending;
  Completer<void>? _cancellation;
  bool _closed = false;

  Future<({int code, String output})> run(
    String executable, {
    Map<String, String>? environment,
  }) => _exclusive((cancellation) async {
    final (:code, :output) = await _run(
      executable,
      const ['update', 'check'],
      environment,
      cancellation,
      const Duration(seconds: 50),
      '检查更新',
    );
    return (code: code!, output: output);
  });

  Future<void> install({
    required String executable,
    required String downloader,
    required String candidateId,
    Map<String, String>? environment,
  }) => _exclusive((cancellation) async {
    // Closing the window can end this sequence without applying the package.
    // Let a running download finish or clean up its own cache.
    final downloaded = await _run(
      downloader,
      ['download', '--candidate', candidateId],
      environment,
      cancellation,
      const Duration(minutes: 11),
      '下载更新',
      keepRunning: true,
    );
    requireUpdateOperationResponse(
      downloaded.code,
      downloaded.output,
      installing: false,
    );
    if (_closed || cancellation.isCompleted) {
      throw const IntegrationFailure('下载更新已取消。');
    }
    // Applying can ask this GUI to exit and wait for it. At that point
    // closing the GUI must neither wait for the updater nor terminate it.
    _cancellation = null;
    final applied = await _run(
      executable,
      ['update', 'apply', '--candidate', candidateId, '--confirm'],
      environment,
      null,
      const Duration(minutes: 12),
      '安装更新',
      keepRunning: true,
    );
    requireUpdateOperationResponse(
      applied.code,
      applied.output,
      installing: true,
    );
  });

  Future<T> _exclusive<T>(
    Future<T> Function(Completer<void> cancellation) start,
  ) async {
    if (_closed) throw const IntegrationFailure('更新操作已取消。');
    if (_pending != null) {
      throw const IntegrationFailure('正在执行另一项更新操作，请稍后重试。');
    }
    final cancellation = Completer<void>();
    _cancellation = cancellation;
    final operation = start(cancellation);
    _pending = operation;
    try {
      return await operation;
    } finally {
      _pending = null;
      _cancellation = null;
    }
  }

  Future<({int? code, String output})> _run(
    String executable,
    List<String> arguments,
    Map<String, String>? environment,
    Completer<void>? cancellation,
    Duration timeout,
    String action, {
    bool keepRunning = false,
  }) async {
    Process? child;
    StreamIterator<List<int>>? stdout;
    StreamIterator<List<int>>? stderr;
    Future<int>? exitCode;
    var exited = false;
    try {
      child = await Process.start(
        executable,
        arguments,
        environment: environment,
        includeParentEnvironment: environment == null,
        mode: keepRunning
            ? ProcessStartMode.detachedWithStdio
            : ProcessStartMode.normal,
      );
      stdout = StreamIterator(child.stdout);
      stderr = StreamIterator(child.stderr);
      if (!keepRunning) {
        exitCode = child.exitCode.then((code) {
          exited = true;
          return code;
        });
      }
      final bytes = BytesBuilder(copy: false);
      await Future.any<void>([
        if (cancellation != null)
          cancellation.future.then<void>((_) {
            throw IntegrationFailure('$action已取消。');
          }),
        Future.wait<void>([
          if (exitCode != null) exitCode.then<void>((_) {}),
          child.stdin.close().then<void>((_) {}),
          _readOutput(stdout, bytes, cancellation == null),
          _discardOutput(stderr),
        ], eagerError: true),
      ]).timeout(
        timeout,
        onTimeout: () => throw IntegrationFailure(
          cancellation == null
              ? '更新结果尚未确认，请运行 sidraviactl update status 查看。'
              : '$action超时，请重试。',
        ),
      );
      return (
        code: exitCode == null ? null : await exitCode,
        output: utf8.decode(bytes.takeBytes()),
      );
    } on IntegrationFailure {
      rethrow;
    } on Object {
      throw IntegrationFailure(
        cancellation == null && child != null
            ? '更新结果尚未确认，请运行 sidraviactl update status 查看。'
            : '无法运行$action，请检查安装是否完整。',
      );
    } finally {
      if (!exited && !keepRunning) child?.kill();
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
    bool installing,
  ) async {
    while (await stream.moveNext()) {
      final chunk = stream.current;
      if (bytes.length + chunk.length > _outputLimit) {
        throw IntegrationFailure(
          installing
              ? '更新结果尚未确认，请运行 sidraviactl update status 查看。'
              : '更新检查返回了过大的结果。',
        );
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
