// Separate process disk probe: no Flutter UI, IPC, daemon or credentials.
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/platform/gui_settings_storage_io.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

Future<void> main(List<String> args) async {
  final store = FileGuiSettingsStore(args[0]);
  final initial = await store.read();
  if (args.length > 1) {
    await store.save(GuiAppearanceMode.values.byName(args[1]));
  }
  stdout.writeln(
    jsonEncode({
      'pid': pid,
      'path': store.path,
      'initial': initial?.name,
      'restored': (await FileGuiSettingsStore(args[0]).read())?.name,
      'platform': Platform.operatingSystem,
    }),
  );
}
