import 'package:sidravia_gui/ipc/ipc_models.dart';

abstract interface class SidraviaIpcClient {
  Future<DaemonStatus> daemonStatus();
  Future<List<InstitutionProfile>> profileList();
  Future<List<ConfigurationSummary>> configurationList();
  Future<List<SessionSummary>> sessionList();
  Future<ConfigurationSummary> configurationCreate({
    required NetworkBindingPolicy networkBindingPolicy,
    required String institutionProfileId,
    required String username,
    required String password,
    required bool autoLogin,
    required bool autoReconnect,
    required bool allowInsecureStorage,
  });
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
    String? password,
    bool allowInsecureStorage = false,
  });
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
    bool allowInsecureStorage = false,
  });
  Future<SessionSummary> sessionStartConfiguration(String configurationId);
  Future<SessionSummary> sessionStop(String sessionId);
  Future<SessionRemoveResult> sessionRemove(String sessionId);
  Future<SessionSummary> sessionEnsureRunning(String sessionId);
  Future<SessionSummary> sessionRestart(String sessionId);
  Future<ConfigurationSummary> configurationSetAutoLogin({
    required String configurationId,
    required bool autoLogin,
    bool allowInsecureStorage = false,
  });
  Future<ConfigurationSummary> configurationSetAutoReconnect({
    required String configurationId,
    required bool autoReconnect,
    bool allowInsecureStorage = false,
  });
  Future<ConfigurationRemoveResult> configurationRemove(
    String configurationId, {
    bool allowInsecureStorage = false,
  });
  Future<void> close();
}

/// The already-existing typed `daemon.stop` capability. Keeping this separate
/// lets development previews implement only the shared read/write surface
/// without ever exposing a stop path.
abstract interface class SidraviaDesktopClient implements SidraviaIpcClient {
  Future<DaemonStopResult> daemonStop();
}

/// Optional discovery; preview clients and the shared UI surface stay unchanged.
abstract interface class SidraviaNetworkClient {
  Future<NetworkDiagnosis> networkDiagnose({
    String? configurationId,
    String? sessionId,
    bool probe = false,
  });
  Future<NetworkInterfacesSnapshot> networkInterfaces();
  Future<ConfigurationSummary> configurationSetNetworkBindingPolicy({
    required String configurationId,
    required NetworkBindingPolicy policy,
    bool allowInsecureStorage = false,
  });
}
