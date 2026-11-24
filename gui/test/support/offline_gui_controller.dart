import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';

/// Connects a controller to the supplied in-memory client. The test owns
/// initialization and teardown, including cases with controlled futures.
GuiController offlineGuiController(OfflineDemoClient client) => GuiController(
  bootstrapper: OfflineDemoBootstrap(),
  connector: (_) async => client,
);
