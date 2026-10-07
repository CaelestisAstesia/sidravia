import 'package:path_provider/path_provider.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

import 'gui_settings_storage_stub.dart'
    if (dart.library.io) 'gui_settings_storage_io.dart'
    as platform;

Future<GuiSettingsStore> productionGuiSettingsStore({
  String namespace = '',
}) async => platform.createPlatformGuiSettingsStore(
  namespace: namespace,
  privateDirectory: platform.needsPrivateGuiDirectory
      ? (await getApplicationSupportDirectory()).path
      : null,
);
GuiSettingsStore isolatedDemoGuiSettingsStore(String directory) =>
    platform.createIsolatedGuiSettingsStore(directory);

class UnavailableGuiSettingsStore implements GuiSettingsStore {
  @override
  bool get persistent => true;
  @override
  Future<GuiAppearanceMode?> read() async =>
      throw StateError('GUI settings path unavailable');
  @override
  Future<void> save(GuiAppearanceMode mode) async =>
      throw StateError('GUI settings path unavailable');
}
