import 'dart:io';

/// Packaged Linux GUI code lives below gui-runtime; all public commands and
/// portable user data remain beside the static public launcher.
String linuxProductExecutable(String executable) {
  final file = File(executable);
  final directory = file.parent;
  if (directory.path.split(Platform.pathSeparator).last != 'gui-runtime') {
    return executable;
  }
  return directory.parent.uri.resolve('sidravia_gui').toFilePath();
}
