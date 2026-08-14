import 'package:sidravia_gui/ipc/ipc_models.dart';

abstract interface class SidraviaIpcClient {
  Future<DaemonStatus> daemonStatus();
  Future<List<InstitutionProfile>> profileList();
  Future<List<ConfigurationSummary>> configurationList();
  Future<List<SessionSummary>> sessionList();
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  });
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  });
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  });
  Future<SessionSummary> sessionStartConfiguration(String configurationId);
  Future<SessionSummary> sessionStop(String sessionId);
  Future<SessionSummary> sessionEnsureRunning(String sessionId);
  Future<SessionSummary> sessionRestart(String sessionId);
  Future<void> close();
}
