import 'package:flutter/widgets.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/licensing/harmony_os_font_license.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  registerHarmonyOSFontLicense();
  runApp(const SidraviaApp());
}
