import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  test('zero Configuration is create-only and never manageable', () {
    final capabilities = _caps(snapshot: _snapshot(configurations: const []));
    expect(capabilities.capability, GuiCapabilityState.createOnly);
    expect(capabilities.canCreate, isTrue);
    expect(capabilities.canManage, isFalse);
    expect(capabilities.configuration, isNull);
    expect(capabilities.retainedSession, isNull);
  });

  test('zero Configuration with an observed Session stays read-only', () {
    final capabilities = _caps(
      snapshot: _snapshot(
        configurations: const [],
        sessions: [_session(id: 's-orphan', configurationId: null)],
      ),
    );
    expect(capabilities.capability, GuiCapabilityState.ambiguousSessions);
    expect(capabilities.canCreate, isFalse);
    expect(capabilities.canManage, isFalse);
  });

  test('one Configuration with no Session is manageable without reset', () {
    final capabilities = _caps(snapshot: _snapshot());
    expect(capabilities.capability, GuiCapabilityState.manageable);
    expect(capabilities.canCreate, isFalse);
    expect(capabilities.canManage, isTrue);
    expect(capabilities.canEditAutoLogin, isTrue);
    expect(capabilities.canDeleteConfiguration, isTrue);
    expect(capabilities.canResetSession, isFalse);
    expect(capabilities.configuration?.id, 'cfg-a');
    expect(capabilities.retainedSession, isNull);
  });

  test('one Configuration with one related Session allows reset', () {
    final session = _session(id: 's-a', configurationId: 'cfg-a');
    final capabilities = _caps(snapshot: _snapshot(sessions: [session]));
    expect(capabilities.capability, GuiCapabilityState.manageable);
    expect(capabilities.canResetSession, isTrue);
    expect(capabilities.retainedSession?.id, 's-a');
  });

  test('multiple Configurations stay read-only', () {
    final capabilities = _caps(
      snapshot: _snapshot(configurations: const [_configuration, _other]),
    );
    expect(capabilities.capability, GuiCapabilityState.multipleConfigurations);
    expect(capabilities.canCreate, isFalse);
    expect(capabilities.canManage, isFalse);
    expect(capabilities.configuration, isNull);
  });

  test('multiple or foreign Sessions stay read-only', () {
    for (final sessions in <List<SessionSummary>>[
      [
        _session(id: 's-a', configurationId: 'cfg-a'),
        _session(id: 's-b', configurationId: 'cfg-a'),
      ],
      [
        _session(id: 's-a', configurationId: 'cfg-a'),
        _session(id: 's-foreign', configurationId: 'cfg-foreign'),
      ],
      [_session(id: 's-one-shot', configurationId: null)],
    ]) {
      final capabilities = _caps(snapshot: _snapshot(sessions: sessions));
      expect(capabilities.capability, GuiCapabilityState.ambiguousSessions);
      expect(capabilities.canManage, isFalse);
      expect(capabilities.retainedSession, isNull, reason: sessions.toString());
    }
  });

  test('non-running daemon is read-only', () {
    final snapshot = _snapshot(
      daemon: const DaemonStatus(
        productVersion: 'v',
        buildId: 'b',
        pid: 1,
        status: 'stopped',
        mode: 'desktop',
        desktopOwnerPid: 2,
      ),
    );
    final capabilities = _caps(snapshot: snapshot);
    expect(capabilities.capability, GuiCapabilityState.daemonUnavailable);
    expect(capabilities.canManage, isFalse);
  });

  test('non-ready states map to their read-only projection', () {
    for (final (state, expected) in [
      (GuiConnectionState.bootstrapping, GuiCapabilityState.bootstrapping),
      (GuiConnectionState.stale, GuiCapabilityState.stale),
      (GuiConnectionState.failed, GuiCapabilityState.failed),
      (GuiConnectionState.unsupported, GuiCapabilityState.unsupported),
    ]) {
      final capabilities = GuiCapabilities(
        state: state,
        snapshot: null,
        busy: false,
      );
      expect(capabilities.capability, expected);
      expect(capabilities.canCreate, isFalse);
      expect(capabilities.canManage, isFalse);
    }
  });

  test('in-flight mutation stays read-only even for a manageable target', () {
    final capabilities = _caps(
      snapshot: _snapshot(
        sessions: [_session(id: 's-a', configurationId: 'cfg-a')],
      ),
      busy: true,
    );
    expect(capabilities.capability, GuiCapabilityState.manageable);
    expect(capabilities.canManage, isFalse);
    expect(capabilities.canEditAutoLogin, isFalse);
    expect(capabilities.canDeleteConfiguration, isFalse);
    expect(capabilities.canResetSession, isFalse);
    expect(capabilities.configuration?.id, 'cfg-a');
    expect(capabilities.retainedSession?.id, 's-a');
  });
}

GuiCapabilities _caps({required GuiSnapshot snapshot, bool busy = false}) =>
    GuiCapabilities(
      state: GuiConnectionState.ready,
      snapshot: snapshot,
      busy: busy,
    );

GuiSnapshot _snapshot({
  List<ConfigurationSummary> configurations = const [_configuration],
  List<SessionSummary> sessions = const [],
  DaemonStatus daemon = _daemon,
}) => GuiSnapshot(
  daemon: daemon,
  profiles: const [_profile],
  configurations: configurations,
  sessions: sessions,
);

SessionSummary _session({required String id, String? configurationId}) =>
    SessionSummary(
      id: id,
      displayName: '',
      accountName: 'fixture-user',
      state: SessionState.suspended,
      intent: SessionIntent.suspendAuthentication,
      configurationId: configurationId,
    );

const _daemon = DaemonStatus(
  productVersion: 'fixture',
  buildId: 'fixture',
  pid: 1,
  status: 'running',
  mode: 'desktop',
  desktopOwnerPid: 2,
);
const _profile = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom-5.2.0-d',
);
const _configuration = ConfigurationSummary(
  id: 'cfg-a',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'fixture-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _other = ConfigurationSummary(
  id: 'cfg-b',
  displayName: 'other',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'other-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
