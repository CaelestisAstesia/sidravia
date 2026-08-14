import 'package:flutter/widgets.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';
import 'package:sidravia_gui/licensing/harmony_os_font_license.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  registerHarmonyOSFontLicense();
  runApp(
    SidraviaApp(controller: GuiController(bootstrapper: ProcessGuiBootstrap())),
  );
}
