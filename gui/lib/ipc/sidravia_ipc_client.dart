import 'package:sidravia_gui/ipc/ipc_models.dart';

abstract interface class SidraviaIpcClient {
  Future<DaemonStatus> daemonStatus();
  Future<List<InstitutionProfile>> profileList();
  Future<List<ConfigurationSummary>> configurationList();
  Future<List<SessionSummary>> sessionList();
  Future<void> close();
}
