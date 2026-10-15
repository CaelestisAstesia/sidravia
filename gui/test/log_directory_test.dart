import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';
import 'package:sidravia_gui/desktop/log_directory.dart';

class Child implements GuiBootstrapProcess {
  final out = StreamController<List<int>>();
  final err = StreamController<List<int>>();
  final exit = Completer<int>();
  int kills = 0;
  @override
  Stream<List<int>> get stdout => out.stream;
  @override
  Stream<List<int>> get stderr => err.stream;
  @override
  Future<int> get exitCode => exit.future;
  void finish(int code) {
    if (!exit.isCompleted) exit.complete(code);
    unawaited(out.close());
    unawaited(err.close());
  }

  @override
  bool kill() {
    kills++;
    finish(-1);
    return true;
  }
}

void main() {
  test(
    'fixed sibling CLI arguments, bounded discarded streams, success',
    () async {
      final child = Child();
      final directory = Directory('app with spaces').absolute;
      final gui = File.fromUri(directory.uri.resolve('Sidravia.exe'));
      final cli = File.fromUri(directory.uri.resolve('sidraviactl.exe'));
      var launches = 0;
      final adapter = DesktopLogDirectory(
        isWindows: () => true,
        resolvedExecutable: () => gui.path,
        launch: (exe, args) async {
          launches++;
          expect(exe, cli.path);
          expect(args, ['gui', 'open-log-directory']);
          return child;
        },
      );
      final pending = adapter.open();
      child.out.add(List.filled(200000, 65));
      child.err.add([66]);
      child.finish(0);
      expect(await pending, isTrue);
      expect(launches, 1);
      expect(child.kills, 0);
      await adapter.close();
    },
  );
  test('unsupported never launches, close prevents later opens', () async {
    var calls = 0;
    final adapter = DesktopLogDirectory(
      isWindows: () => false,
      launch: (e, a) async {
        calls++;
        return Child();
      },
    );
    expect(await adapter.open(), isFalse);
    await adapter.close();
    expect(await adapter.open(), isFalse);
    expect(calls, 0);
  });
  test('nonzero and launch failure produce safe failure', () async {
    final child = Child();
    final adapter = DesktopLogDirectory(
      isWindows: () => true,
      launch: (e, a) async => child,
    );
    final pending = adapter.open();
    child.finish(1);
    expect(await pending, isFalse);
    await adapter.close();
    final failed = DesktopLogDirectory(
      isWindows: () => true,
      launch: (e, a) async => throw StateError('raw path'),
    );
    expect(await failed.open(), isFalse);
    await failed.close();
  });
  test('timeout kills, waits and drains owned child', () async {
    final child = Child();
    final adapter = DesktopLogDirectory(
      isWindows: () => true,
      timeout: const Duration(milliseconds: 5),
      launch: (e, a) async => child,
    );
    expect(await adapter.open(), isFalse);
    expect(child.kills, 1);
    expect(child.exit.isCompleted, isTrue);
    await adapter.close();
  });
  test(
    'close waits late launch and kills child; concurrent click shares work',
    () async {
      final launch = Completer<GuiBootstrapProcess>();
      final child = Child();
      var calls = 0;
      final adapter = DesktopLogDirectory(
        isWindows: () => true,
        launch: (e, a) {
          calls++;
          return launch.future;
        },
      );
      final pending = adapter.open();
      expect(identical(pending, adapter.open()), isTrue);
      final closed = adapter.close();
      launch.complete(child);
      await closed;
      expect(await pending, isFalse);
      expect(child.kills, 1);
      expect(calls, 1);
    },
  );
  test('stream error kills and settles child', () async {
    final child = Child();
    final adapter = DesktopLogDirectory(
      isWindows: () => true,
      launch: (e, a) async => child,
    );
    final pending = adapter.open();
    child.out.addError(StateError('raw output'));
    expect(await pending, isFalse);
    expect(child.kills, 1);
    await adapter.close();
  });
}
