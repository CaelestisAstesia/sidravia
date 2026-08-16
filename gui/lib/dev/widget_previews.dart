import 'package:flutter/material.dart';
import 'package:flutter/widget_previews.dart';
import 'package:sidravia_gui/dev/preview_fixtures.dart';
import 'package:sidravia_gui/dev/preview_main.dart';

@Preview(group: 'Sidravia', name: 'Home · authenticated', size: Size(1280, 720))
Widget authenticatedHomePreview() => const PreviewCatalog(
  initialScenario: PreviewScenario.authenticated,
  surface: PreviewSurface.home,
  showToolbar: false,
);

@Preview(
  group: 'Sidravia',
  name: 'Configuration · empty',
  size: Size(1280, 720),
)
Widget emptyConfigurationPreview() => const PreviewCatalog(
  initialScenario: PreviewScenario.noConfiguration,
  surface: PreviewSurface.configuration,
  showToolbar: false,
);

@Preview(group: 'Sidravia', name: 'Settings', size: Size(1280, 720))
Widget settingsPreview() =>
    const PreviewCatalog(surface: PreviewSurface.settings, showToolbar: false);

@Preview(
  group: 'Sidravia',
  name: 'Home · maintenance announcement',
  size: Size(1280, 720),
)
Widget maintenanceAnnouncementHomePreview() => const PreviewCatalog(
  initialScenario: PreviewScenario.authenticated,
  announcementScenario: PreviewAnnouncementScenario.maintenance,
  surface: PreviewSurface.home,
  showToolbar: false,
);
