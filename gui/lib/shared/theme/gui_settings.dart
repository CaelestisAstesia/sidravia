import 'dart:convert';

enum GuiAppearanceMode { system, light, dark }

class GuiSettings {
  const GuiSettings({
    this.appearanceMode = GuiAppearanceMode.system,
    this.diagnosticsExpanded = false,
  });

  final GuiAppearanceMode appearanceMode;
  final bool diagnosticsExpanded;
}

abstract interface class GuiSettingsStore {
  bool get persistent;
  Future<GuiSettings?> read();
  Future<void> save(GuiSettings settings);
}

class MemoryGuiSettingsStore implements GuiSettingsStore {
  GuiSettings? settings;
  @override
  bool get persistent => false;
  @override
  Future<GuiSettings?> read() async => settings;
  @override
  Future<void> save(GuiSettings value) async {
    settings = value;
  }
}

GuiSettings decodeGuiSettings(String source) {
  final value = jsonDecode(source);
  if (value is! Map<String, dynamic> || value['schemaVersion'] is! int) {
    throw const FormatException('Unsupported GUI settings');
  }

  final version = value['schemaVersion'] as int;
  final appearanceMode = value['appearanceMode'];
  if (appearanceMode is! String) {
    throw const FormatException('Invalid appearance mode');
  }
  final mode = GuiAppearanceMode.values.firstWhere(
    (m) => m.name == appearanceMode,
    orElse: () => throw const FormatException('Invalid appearance mode'),
  );

  if (version == 1 && value.length == 2) {
    return GuiSettings(appearanceMode: mode);
  }
  if (version == 2 &&
      value.length == 3 &&
      value['diagnosticsExpanded'] is bool) {
    return GuiSettings(
      appearanceMode: mode,
      diagnosticsExpanded: value['diagnosticsExpanded'] as bool,
    );
  }
  throw const FormatException('Unsupported GUI settings');
}

String encodeGuiSettings(GuiSettings settings) =>
    '${jsonEncode({'schemaVersion': 2, 'appearanceMode': settings.appearanceMode.name, 'diagnosticsExpanded': settings.diagnosticsExpanded})}\n';
