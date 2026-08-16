import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/licensing/harmony_os_font_license.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  registerHarmonyOSFontLicense();
  runApp(
    SidraviaApp(
      controller: GuiController(bootstrapper: ProcessGuiBootstrap()),
      announcements: _createAnnouncementController(),
    ),
  );
}

AnnouncementController _createAnnouncementController() {
  final endpoint = parseAnnouncementEndpoint(
    const String.fromEnvironment('SIDRAVIA_ANNOUNCEMENT_FEED_URL'),
  );
  if (endpoint == null) return AnnouncementController.disabled();
  return AnnouncementController(
    endpoint: endpoint,
    store: SharedPreferencesAnnouncementStore(SharedPreferencesAsync()),
    fetcher: HttpAnnouncementFetcher(endpoint: endpoint, client: http.Client()),
  );
}
