import 'package:sidravia_gui/ipc/ipc_models.dart';

class GuiSnapshot {
  const GuiSnapshot({
    required this.daemon,
    required this.profiles,
    required this.configurations,
    required this.sessions,
    this.network,
  });

  /// Null only for explicit data-only preview clients.
  final NetworkInterfacesSnapshot? network;
  final DaemonStatus daemon;
  final List<InstitutionProfile> profiles;
  final List<ConfigurationSummary> configurations;
  final List<SessionSummary> sessions;
}
