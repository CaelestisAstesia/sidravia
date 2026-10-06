import 'dart:io';
import 'dart:math';

import 'package:sidravia_gui/shared/theme/gui_settings.dart';

bool get needsPrivateGuiDirectory => Platform.isAndroid || Platform.isIOS;

bool _missing(FileSystemException error) =>
    [2, 3].contains(error.osError?.errorCode);

/// Mirrors productlayout namespace grammar; no CWD or daemon dependency.
bool validGuiNamespace(String value) =>
    value.isEmpty ||
    value.length <= 64 &&
        RegExp(r'^[A-Za-z0-9._-]+$').hasMatch(value) &&
        !value.startsWith('.') &&
        !value.endsWith('.') &&
        !value.contains('..') &&
        !['production', 'default', 'prod'].contains(value.toLowerCase());

class GuiSettingsLocation {
  const GuiSettingsLocation(this.mode, this.namespace, this.path);
  final String mode, namespace, path;
}

// Dart stat/type APIs collapse some OS failures to notFound. Directory listing
// preserves non-missing errors and classifies links without following them.
FileSystemEntityType _portableMarker(String marker) {
  final parent = File(marker).parent;
  for (final entry in parent.listSync(followLinks: false)) {
    if (entry.path == marker) {
      if (entry is Link) return FileSystemEntityType.link;
      if (entry is Directory) return FileSystemEntityType.directory;
      if (entry is File) {
        final info = entry.statSync();
        if (info.type != FileSystemEntityType.file ||
            (info.mode & 0xf000) != 0x8000) {
          throw const FormatException('Portable marker is not a regular file');
        }
        return FileSystemEntityType.file;
      }
    }
  }
  return FileSystemEntityType.notFound;
}

GuiSettingsLocation resolveWindowsGuiSettings({
  required String executable,
  required String? userConfigRoot,
  String namespace = '',
  FileSystemEntityType Function(String)? inspectMarker,
}) {
  if (!validGuiNamespace(namespace)) {
    throw const FormatException('Invalid namespace');
  }
  final directory = File(executable).parent;
  if (!directory.isAbsolute) {
    throw const FormatException('Executable directory must be absolute');
  }
  final exeRoot = File.fromUri(
    directory.uri.normalizePath().resolve('__path_anchor__'),
  ).parent.path;
  final marker = '$exeRoot${Platform.pathSeparator}sidravia.portable';
  final kind = (inspectMarker ?? _portableMarker)(marker);
  if (kind != FileSystemEntityType.file &&
      kind != FileSystemEntityType.notFound) {
    throw const FormatException('Portable marker is not a regular file');
  }
  final portable = kind == FileSystemEntityType.file;
  String root;
  if (portable) {
    root = exeRoot;
    root += namespace.isEmpty
        ? '${Platform.pathSeparator}config'
        : '${Platform.pathSeparator}namespaces${Platform.pathSeparator}$namespace';
  } else {
    if (userConfigRoot == null || !Directory(userConfigRoot).isAbsolute) {
      throw const FormatException('User configuration root is unavailable');
    }
    root =
        '${File.fromUri(Directory(userConfigRoot).uri.normalizePath().resolve('__path_anchor__')).parent.path}${Platform.pathSeparator}Sidravia';
    if (namespace.isNotEmpty) {
      root +=
          '${Platform.pathSeparator}namespaces${Platform.pathSeparator}$namespace';
    }
  }
  return GuiSettingsLocation(
    portable ? 'portable' : 'installed',
    namespace,
    '$root${Platform.pathSeparator}gui-settings.json',
  );
}

GuiSettingsStore createPlatformGuiSettingsStore({
  String namespace = '',
  String? privateDirectory,
}) {
  if (Platform.isWindows) {
    final location = resolveWindowsGuiSettings(
      executable: Platform.resolvedExecutable,
      userConfigRoot: Platform.environment['APPDATA'],
      namespace: namespace,
    );
    return FileGuiSettingsStore(location.path);
  }
  if (needsPrivateGuiDirectory &&
      privateDirectory != null &&
      Directory(privateDirectory).isAbsolute) {
    if (!validGuiNamespace(namespace)) {
      throw const FormatException('Invalid namespace');
    }
    final root = namespace.isEmpty
        ? privateDirectory
        : '$privateDirectory/namespaces/$namespace';
    return FileGuiSettingsStore('$root/gui-settings.json');
  }
  throw UnsupportedError(
    'GUI preference storage is unsupported on this platform',
  );
}

GuiSettingsStore createIsolatedGuiSettingsStore(String directory) {
  if (!Directory(directory).isAbsolute) {
    throw const FormatException('Demo directory must be absolute');
  }
  return FileGuiSettingsStore(
    '$directory${Platform.pathSeparator}gui-settings.json',
  );
}

/// Owns only gui-settings.json; never opens the configuration/credential files.
class FileGuiSettingsStore implements GuiSettingsStore {
  FileGuiSettingsStore(
    this.path, {
    Future<void> Function(File, String)? replace,
  }) : _replace =
           replace ??
           ((source, destination) async {
             await source.rename(destination);
           });
  final String path;
  final Future<void> Function(File, String) _replace;
  bool _loaded = false, _writable = false;
  @override
  bool get persistent => true;
  void _checkType() {
    final kind = FileSystemEntity.typeSync(path, followLinks: false);
    if (kind != FileSystemEntityType.file &&
        kind != FileSystemEntityType.notFound) {
      throw const FormatException('GUI settings must be a regular file');
    }
  }

  @override
  Future<GuiAppearanceMode?> read() async {
    _loaded = true;
    _writable = false;
    _checkType();
    try {
      final mode = decodeGuiSettings(await File(path).readAsString());
      _writable = true;
      return mode;
    } on FileSystemException catch (error) {
      if (!_missing(error)) rethrow;
      _writable = true;
      return null;
    }
  }

  @override
  Future<void> save(GuiAppearanceMode mode) async {
    if (!_loaded || !_writable) {
      throw StateError('GUI settings are read-only after load failure');
    }
    _checkType();
    final file = File(path);
    await file.parent.create(recursive: true);
    final temporary = File(
      '$path.$pid.${Random.secure().nextInt(1 << 32)}.tmp',
    );
    await temporary.create(exclusive: true);
    try {
      await temporary.writeAsString(encodeGuiSettings(mode), flush: true);
      _checkType();
      // Dart 3.13 Windows File.rename uses MoveFileExW REPLACE_EXISTING |
      // WRITE_THROUGH. Same-directory replacement never pre-deletes the target.
      await _replace(temporary, path);
    } finally {
      if (await temporary.exists()) await temporary.delete();
    }
  }
}
