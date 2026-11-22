import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/platform/gui_settings_storage_io.dart';
import 'package:sidravia_gui/application/preferences/gui_settings.dart';

void main() {
  test(
    'Linux XDG, HOME, portable and namespace use isolated absolute paths',
    () async {
      final root = Directory.systemTemp.createTempSync('sidravia-linux-prefs-');
      addTearDown(() => root.deleteSync(recursive: true));
      final executable = '${root.path}/sidravia_gui';
      GuiSettingsLocation resolve(Map<String, String> env, [String ns = '']) =>
          resolveLinuxGuiSettings(
            executable: executable,
            environment: env,
            namespace: ns,
          );
      expect(
        resolve({'XDG_CONFIG_HOME': '${root.path}/xdg'}).path,
        '${root.path}/xdg/Sidravia/gui-settings.json',
      );
      expect(
        resolve({'XDG_CONFIG_HOME': 'relative', 'HOME': root.path}, 'a').path,
        '${root.path}/.config/Sidravia/namespaces/a/gui-settings.json',
      );
      expect(() => resolve({}), throwsFormatException);
      File('${root.path}/sidravia.portable').writeAsStringSync('');
      expect(resolve({}).path, '${root.path}/config/gui-settings.json');
      final a = resolve({}, 'a'), b = resolve({}, 'b');
      final store = FileGuiSettingsStore(a.path);
      expect(await store.read(), isNull);
      await store.save(
        const GuiSettings(appearanceMode: GuiAppearanceMode.dark),
      );
      expect(
        (await FileGuiSettingsStore(a.path).read())?.appearanceMode,
        GuiAppearanceMode.dark,
      );
      expect(await FileGuiSettingsStore(b.path).read(), isNull);
      expect(() => resolve({}, '../escape'), throwsFormatException);
    },
    skip: !Platform.isLinux,
  );
}
