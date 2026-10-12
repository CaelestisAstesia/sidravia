import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

/// Resolves the configured school's metadata without connection-action guards.
InstitutionProfile? schoolAnnouncementProfileFor(GuiCapabilities capabilities) {
  final configuration = capabilities.configuration;
  if (configuration == null) return null;
  final matches = capabilities.snapshot!.profiles.where(
    (profile) => profile.id == configuration.institutionProfileId,
  );
  if (matches.length != 1) return null;
  final profile = matches.single;
  return profile.announcement == null ? null : profile;
}

/// Owns only current-session observations. Preferences own the durable day.
/// Callers initialize [GuiPreferences] before considering complete snapshots.
class SchoolAnnouncementPolicy {
  SchoolAnnouncementPolicy({required this.startedAt});

  final DateTime startedAt;
  final Map<String, _AuthenticationObservation> _observations = {};
  int? _daemonPid;
  bool _observedReadySnapshot = false;

  InstitutionProfile? consider(
    GuiCapabilities capabilities,
    GuiPreferences preferences,
    DateTime now,
  ) {
    if (!capabilities.isReady ||
        capabilities.snapshot!.daemon.status != 'running') {
      return null;
    }
    final snapshot = capabilities.snapshot!;
    final initial = !_observedReadySnapshot;
    _observedReadySnapshot = true;
    if (_daemonPid != snapshot.daemon.pid) {
      _observations.clear();
      _daemonPid = snapshot.daemon.pid;
    }
    final retainedIds = snapshot.sessions.map((session) => session.id).toSet();
    _observations.removeWhere((id, _) => !retainedIds.contains(id));
    final fresh = <String>{};
    for (final session in snapshot.sessions) {
      final previous = _observations[session.id];
      if (previous != null && session.revision <= previous.revision) continue;
      final established = session.state == SessionState.authenticated
          ? session.authenticationEstablishedAt
          : null;
      final lastEstablished = previous?.established;
      final changed =
          established != null &&
          (lastEstablished == null ||
              !established.isAtSameMomentAs(lastEstablished));
      // Consume observations even when this event cannot produce a dispatch.
      // Complete all identities before notifying the preference owner below.
      _observations[session.id] = _AuthenticationObservation(
        session.revision,
        established ?? previous?.established,
      );
      if (changed && (!initial || !established.isBefore(startedAt))) {
        fresh.add(session.id);
      }
    }
    final session = capabilities.retainedSession;
    final configuration = capabilities.configuration;
    if (session == null ||
        configuration == null ||
        !fresh.contains(session.id) ||
        fresh.length != 1 ||
        session.institutionProfileId != null &&
            session.institutionProfileId !=
                configuration.institutionProfileId) {
      return null;
    }
    final profile = schoolAnnouncementProfileFor(capabilities);
    if (profile == null ||
        preferences.announcementFrequency ==
            GuiAnnouncementFrequency.manualOnly) {
      return null;
    }
    final local = now.toLocal();
    final day =
        '${local.year.toString().padLeft(4, '0')}-${local.month.toString().padLeft(2, '0')}-${local.day.toString().padLeft(2, '0')}';
    if (preferences.announcementFrequency ==
            GuiAnnouncementFrequency.dailyFirstConnection &&
        preferences.lastAutomaticAnnouncementDate == day) {
      return null;
    }
    preferences.recordAutomaticAnnouncement(now);
    return profile;
  }
}

class _AuthenticationObservation {
  const _AuthenticationObservation(this.revision, this.established);
  final BigInt revision;
  final DateTime? established;
}
