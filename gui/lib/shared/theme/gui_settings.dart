import 'dart:convert';

enum GuiAppearanceMode { system, light, dark }

abstract interface class GuiSettingsStore {
  bool get persistent;
  Future<GuiAppearanceMode?> read();
  Future<void> save(GuiAppearanceMode mode);
}

class MemoryGuiSettingsStore implements GuiSettingsStore {
  GuiAppearanceMode? mode;
  @override
  bool get persistent => false;
  @override
  Future<GuiAppearanceMode?> read() async => mode;
  @override
  Future<void> save(GuiAppearanceMode value) async {
    mode = value;
  }
}

GuiAppearanceMode decodeGuiSettings(String source) {
  final value = jsonDecode(source);
  if (value is! Map<String, dynamic> ||
      value.length != 2 ||
      value['schemaVersion'] is! int ||
      value['schemaVersion'] != 1 ||
      value['appearanceMode'] is! String) {
    throw const FormatException('Unsupported GUI settings');
  }
  return GuiAppearanceMode.values.firstWhere(
    (m) => m.name == value['appearanceMode'],
    orElse: () => throw const FormatException('Invalid appearance mode'),
  );
}

String encodeGuiSettings(GuiAppearanceMode mode) =>
    '${jsonEncode({'schemaVersion': 1, 'appearanceMode': mode.name})}\n';
