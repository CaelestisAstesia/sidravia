import 'package:sidravia_gui/bootstrap/process_gui_bootstrap.dart';

import 'dart:io';

import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';

import 'package:sidravia_gui/platform/gui_settings_storage.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';

import 'package:flutter/widgets.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/licensing/harmony_os_font_license.dart';
import 'package:sidravia_gui/platform/sidravia_platform.dart';
import 'package:sidravia_gui/desktop/tray_entrypoint.dart';

@pragma('vm:entry-point')
void trayMain() => runTrayPopover();

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final rawNamespace = Platform.environment['SIDRAVIA_NAMESPACE'];
  final namespace = validateDesktopNamespace(rawNamespace);
  if (namespace == null) exit(2);
  // Appearance restoration is independent of bootstrap/IPC readiness.
  late final Appearance appearance;
  try {
    appearance = Appearance(
      store: await productionGuiSettingsStore(namespace: namespace),
    );
  } on Object {
    appearance = Appearance(store: UnavailableGuiSettingsStore());
  }
  await appearance.initialize();
  final platform = SidraviaPlatform.detect(namespace: namespace);
  final desktop = platform.desktopPresence;
  final disposition = await desktop.initialize();
  if (disposition == DesktopPresenceDisposition.activatedExisting) {
    desktop.dispose();
    exit(0);
  }
  await desktop.configurePreferences(
    closeBehavior: appearance.closeBehavior,
    startHidden: appearance.startHidden,
  );
  registerHarmonyOSFontLicense();
  final startedAt = DateTime.now();
  runApp(
    SidraviaApp(
      controller: GuiController(
        bootstrapper: platform.supportsLocalBootstrap
            ? ProcessGuiBootstrap(logLevel: () => appearance.logLevel)
            : platform.bootstrapper,
      ),
      announcements: SchoolAnnouncementController(
        preferences: appearance,
        desktop: desktop,
        window: SchoolAnnouncementWindow(),
        notifications: SchoolAnnouncementNotifications(),
        namespace: namespace,
        startedAt: startedAt,
      ),
      desktopPresence: desktop,
      appearance: appearance,
    ),
  );
}
