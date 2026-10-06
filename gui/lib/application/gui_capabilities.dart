import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

/// Owns the single strict actionable-target projection shared by Home and
/// Configuration. Neither page guesses Configuration/Session relationships
/// anymore; an actionable target exists only when the state is ready, exactly
/// one Configuration is retained, every observed Session belongs to it and at
/// most one related Session exists. Zero Configuration remains create-only.
enum GuiCapabilityState {
  bootstrapping,
  stale,
  failed,
  unsupported,
  daemonUnavailable,
  createOnly,
  manageable,
  multipleConfigurations,
  ambiguousSessions,
}

class GuiCapabilities {
  const GuiCapabilities({
    required this.state,
    required this.snapshot,
    required this.busy,
  });

  final GuiConnectionState state;
  final GuiSnapshot? snapshot;
  final bool busy;

  bool get isReady => state == GuiConnectionState.ready && snapshot != null;

  GuiCapabilityState get capability {
    if (!isReady) {
      return switch (state) {
        GuiConnectionState.bootstrapping => GuiCapabilityState.bootstrapping,
        GuiConnectionState.stale => GuiCapabilityState.stale,
        GuiConnectionState.failed => GuiCapabilityState.failed,
        GuiConnectionState.unsupported => GuiCapabilityState.unsupported,
        GuiConnectionState.ready => GuiCapabilityState.stale,
      };
    }
    if (snapshot!.daemon.status != 'running') {
      return GuiCapabilityState.daemonUnavailable;
    }
    final configurations = snapshot!.configurations;
    final sessions = snapshot!.sessions;
    if (configurations.isEmpty) {
      return sessions.isEmpty
          ? GuiCapabilityState.createOnly
          : GuiCapabilityState.ambiguousSessions;
    }
    if (configurations.length > 1) {
      return GuiCapabilityState.multipleConfigurations;
    }
    final belongs = sessions.every(
      (session) => session.configurationId == configurations.single.id,
    );
    if (!belongs || sessions.length > 1) {
      return GuiCapabilityState.ambiguousSessions;
    }
    return GuiCapabilityState.manageable;
  }

  bool get canCreate => capability == GuiCapabilityState.createOnly && !busy;

  bool get canManage => capability == GuiCapabilityState.manageable && !busy;

  bool get canEditAutoLogin => canManage;

  bool get canEditAutoReconnect => canManage;

  bool get canDeleteConfiguration => canManage;

  bool get canResetSession => canManage && snapshot!.sessions.isNotEmpty;

  ConfigurationSummary? get configuration {
    if (capability != GuiCapabilityState.manageable) return null;
    return snapshot!.configurations.single;
  }

  SessionSummary? get retainedSession {
    if (capability != GuiCapabilityState.manageable ||
        snapshot!.sessions.isEmpty) {
      return null;
    }
    return snapshot!.sessions.single;
  }
}
