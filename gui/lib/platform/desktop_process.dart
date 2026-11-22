import 'dart:io';

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

class SystemDesktopProcess implements GuiBootstrapProcess {
  SystemDesktopProcess(this.value);
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

String desktopCtlPath(String executable, {required bool windows}) =>
    '${File(executable).parent.path}${Platform.pathSeparator}sidraviactl${windows ? '.exe' : ''}';
