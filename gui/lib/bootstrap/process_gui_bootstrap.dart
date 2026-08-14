import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';

class ProcessGuiBootstrap implements GuiBootstrapper {
  ProcessGuiBootstrap({
    this.timeout = const Duration(seconds: 10),
    this.maximumOutputBytes = 64 * 1024,
  });

  final Duration timeout;
  final int maximumOutputBytes;

  @override
  Future<GuiBootstrapResult> bootstrap() async {
    if (!Platform.isWindows) {
      return const GuiBootstrapResult.failure(
        GuiBootstrapFailure.unsupportedPlatform,
      );
    }
    Process? process;
    try {
      final executable = File(Platform.resolvedExecutable);
      final control =
          '${executable.parent.path}${Platform.pathSeparator}sidraviactl.exe';
      process = await Process.start(control, [
        'gui',
        'bootstrap',
        '--owner-pid',
        '$pid',
      ]);
      final stdout = _readBounded(process.stdout);
      final stderr = _readBounded(process.stderr);
      final exitCode = await process.exitCode.timeout(timeout);
      final output = await stdout;
      await stderr;
      if (exitCode != 0) {
        return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
      }
      return decodeGuiBootstrap(utf8.decode(output, allowMalformed: false));
    } on TimeoutException {
      process?.kill();
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    } on Object {
      return const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
    }
  }

  Future<List<int>> _readBounded(Stream<List<int>> stream) async {
    final output = <int>[];
    await for (final chunk in stream) {
      if (output.length + chunk.length > maximumOutputBytes) {
        throw const FormatException('bootstrap output limit');
      }
      output.addAll(chunk);
    }
    return output;
  }
}
