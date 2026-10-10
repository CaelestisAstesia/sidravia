// Separate process disk probe: no Flutter UI, IPC, daemon or credentials.
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/platform/gui_settings_storage_io.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

Future<void> main(List<String> args) async {
  if (args.first == '--layout') {
    final root = Directory(args[1]);
    await root.create(recursive: true);
    final executable =
        '${root.path}${Platform.pathSeparator}program${Platform.pathSeparator}Sidravia.exe';
    await File(executable).parent.create(recursive: true);
    final marker = File(
      '${File(executable).parent.path}${Platform.pathSeparator}sidravia.portable',
    );
    final records = <Map<String, String>>[];
    for (final portable in [false, true]) {
      if (portable) await marker.writeAsString('');
      for (final namespace in ['', 'review-isolated']) {
        final result = resolveWindowsGuiSettings(
          executable: executable,
          userConfigRoot: '${root.path}${Platform.pathSeparator}user-config',
          namespace: namespace,
        );
        records.add({
          'mode': result.mode,
          'namespace': result.namespace,
          'path': result.path,
          'platform': Platform.operatingSystem,
        });
      }
    }
    await marker.delete();
    await Directory(marker.path).create();
    var rejected = false;
    try {
      resolveWindowsGuiSettings(
        executable: executable,
        userConfigRoot: root.path,
      );
    } on FormatException {
      rejected = true;
    }
    if (!rejected) throw StateError('Invalid marker incorrectly accepted');
    stdout.writeln(
      jsonEncode({'layouts': records, 'invalidDirectoryRejected': rejected}),
    );
    return;
  }
  final store = FileGuiSettingsStore(args[0]);
  final initial = await store.read();
  if (args.length > 1) {
    final diagnosticsExpanded = args.length > 2
        ? bool.parse(args[2])
        : initial?.diagnosticsExpanded ?? false;
    await store.save(
      GuiSettings(
        appearanceMode: GuiAppearanceMode.values.byName(args[1]),
        diagnosticsExpanded: diagnosticsExpanded,
        closeBehavior: args.length > 3
            ? GuiCloseBehavior.values.byName(args[3])
            : initial?.closeBehavior ?? GuiCloseBehavior.hideToTray,
        startHidden: args.length > 4
            ? bool.parse(args[4])
            : initial?.startHidden ?? false,
      ),
    );
  }
  final restored = await FileGuiSettingsStore(args[0]).read();
  stdout.writeln(
    jsonEncode({
      'pid': pid,
      'path': store.path,
      'initial': initial?.appearanceMode.name,
      'initialDiagnosticsExpanded': initial?.diagnosticsExpanded,
      'initialCloseBehavior': initial?.closeBehavior.name,
      'initialStartHidden': initial?.startHidden,
      'restored': restored?.appearanceMode.name,
      'restoredDiagnosticsExpanded': restored?.diagnosticsExpanded,
      'restoredCloseBehavior': restored?.closeBehavior.name,
      'restoredStartHidden': restored?.startHidden,
      'platform': Platform.operatingSystem,
    }),
  );
}
