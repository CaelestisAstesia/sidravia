// Test-only native lifecycle harness. Production main and its real adapters are
// called unchanged; this file is never the shipped production entrypoint.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/widgets.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/main.dart' as production;
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

Future<void> main() async {
  if (!Platform.isWindows ||
      Platform.environment['SIDRAVIA_NAMESPACE'] != 'r6-exit-20261006') {
    exit(2);
  }
  WidgetsFlutterBinding.ensureInitialized();
  try {
    await production
        .main(); // A second instance exits here via native presence.
    await WidgetsBinding.instance.endOfFrame;
    SidraviaApp? app;
    void visit(Element element) {
      if (element.widget is SidraviaApp) app = element.widget as SidraviaApp;
      element.visitChildren(visit);
    }

    final root = WidgetsBinding.instance.rootElement;
    if (root == null) throw StateError('app unavailable');
    visit(root);
    final current = app;
    if (current == null) throw StateError('production app unavailable');
    final controller = current.controller;
    await controller.start();
    if (controller.state != GuiConnectionState.ready) {
      throw StateError('IPC unavailable');
    }
    final daemonPid = controller.snapshot!.daemon.pid;
    if (controller.snapshot!.daemon.desktopOwnerPid != pid) {
      throw StateError('owner mismatch');
    }
    final previous = controller.snapshot;
    final state = await SidraviaWindowCommands.channel
        .invokeMapMethod<String, Object?>('getState');
    if (state?['closeToTray'] != true) throw StateError('tray unavailable');
    await SidraviaWindowCommands.close(); // Actual native WM_CLOSE hide path.
    await _until(() => !identical(previous, controller.snapshot));
    if (controller.state != GuiConnectionState.ready ||
        controller.snapshot!.daemon.pid != daemonPid) {
      throw StateError('close changed daemon');
    }
    final second = await Process.start(Platform.resolvedExecutable, []);
    final secondExit = await second.exitCode.timeout(
      const Duration(seconds: 10),
    );
    if (secondExit != 0) throw StateError('restore instance failed');
    await controller.retry();
    if (controller.state != GuiConnectionState.ready ||
        controller.snapshot!.daemon.pid != daemonPid) {
      throw StateError('restore changed daemon');
    }
    final callback = current.desktopPresence?.onExitRequested;
    if (callback == null) throw StateError('explicit exit unavailable');
    final file = File(
      '${File(Platform.resolvedExecutable).parent.path}'
      '/namespaces/r6-exit-20261006/r6-exit-marker.json',
    );
    await file.writeAsString(
      jsonEncode({
        'guiPid': pid,
        'daemonPid': daemonPid,
        'closeNativeChannel': true,
        'pollAfterClose': true,
        'sameDaemonAfterRestore': true,
        'secondInstanceExit': secondExit,
        'productionExitCallbackInvokedNext': true,
        'campusAuthentication': false,
      }),
      flush: true,
    );
    // Actual SidraviaApp callback: controller stop then native presence destroy.
    await callback();
    // Native WM_CLOSE should now destroy this process; timeout signals a defect.
    await Future<void>.delayed(const Duration(seconds: 10));
    exit(1);
  } on Object {
    // Stable test diagnostic only: never print bootstrap result or exceptions.
    stderr.writeln('R6 native lifecycle harness failed');
    exit(1);
  }
}

Future<void> _until(bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 20));
  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) throw StateError('deadline');
    await Future<void>.delayed(const Duration(milliseconds: 25));
  }
}
