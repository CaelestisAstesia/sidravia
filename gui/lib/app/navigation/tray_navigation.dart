import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';

GuiConnectionAction? trayNavigationAction(TrayIntent intent) =>
    switch (intent.kind) {
      TrayIntentKind.addConfiguration => const GuiConnectionAction(
        GuiConnectionActionKind.addConfiguration,
      ),
      TrayIntentKind.editConfiguration => GuiConnectionAction(
        GuiConnectionActionKind.editConfiguration,
        intent.targetId,
      ),
      TrayIntentKind.showSettings => const GuiConnectionAction(
        GuiConnectionActionKind.showSettings,
      ),
      TrayIntentKind.showDetails => const GuiConnectionAction(
        GuiConnectionActionKind.showDetails,
      ),
      _ => null,
    };
