import 'dart:io';

import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/licensing/harmony_os_font_license.dart';
import 'package:sidravia_gui/platform/sidravia_platform.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final rawNamespace = Platform.environment['SIDRAVIA_NAMESPACE'];
  final namespace = validateDesktopNamespace(rawNamespace);
  if (namespace == null) exit(2);
  final platform = SidraviaPlatform.detect(namespace: namespace);
  final desktop = platform.desktopPresence;
  final disposition = await desktop.initialize();
  if (disposition == DesktopPresenceDisposition.activatedExisting) {
    desktop.dispose();
    exit(0);
  }
  registerHarmonyOSFontLicense();
  runApp(
    SidraviaApp(
      controller: GuiController(bootstrapper: platform.bootstrapper),
      announcements: _createAnnouncementController(),
      desktopPresence: desktop,
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
