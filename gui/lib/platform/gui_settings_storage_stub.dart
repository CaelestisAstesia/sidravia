import 'package:sidravia_gui/shared/theme/gui_settings.dart';

bool get needsPrivateGuiDirectory => false;
GuiSettingsStore createPlatformGuiSettingsStore({
  String namespace = '',
  String? privateDirectory,
}) => throw UnsupportedError('File storage is unavailable');
GuiSettingsStore createIsolatedGuiSettingsStore(String directory) =>
    throw UnsupportedError('File storage is unavailable');
