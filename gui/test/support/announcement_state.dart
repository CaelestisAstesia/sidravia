import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const school = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom',
  announcement: AnnouncementMetadata(
    url: 'http://login.jlu.edu.cn/notice_win.php',
    clientWidth: 600,
    clientHeight: 460,
  ),
);
const noMetadata = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom',
);
final started = DateTime(2026, 10, 10, 8);
final success = DateTime(2026, 10, 10, 9);
final now = DateTime(2026, 10, 10, 10);

ConfigurationSummary configuration({
  String id = 'cfg',
  String profileId = 'jlu',
  bool credentials = false,
  bool autoLogin = false,
}) => ConfigurationSummary(
  id: id,
  displayName: '校园',
  institutionProfileId: profileId,
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom',
  username: 'account',
  credentialStored: credentials,
  storageProtection: 'protected',
  autoLogin: autoLogin,
);

SessionSummary session(
  DateTime? at, {
  String id = 'session',
  int revision = 1,
  String configurationId = 'cfg',
  String? profileId,
  SessionState state = SessionState.authenticated,
}) => SessionSummary(
  id: id,
  displayName: '',
  accountName: 'account',
  configurationId: configurationId,
  institutionProfileId: profileId,
  state: state,
  intent: SessionIntent.maintainAuthentication,
  revision: BigInt.from(revision),
  authenticationEstablishedAt: at,
  lastAuthenticationFailure: const SessionAuthenticationFailure(
    code: 'previous_failure',
    description: 'Retained historical failure',
    handlingRecommendation: 'Check details',
  ),
);

GuiCapabilities capabilities({
  List<SessionSummary> sessions = const [],
  List<InstitutionProfile> profiles = const [school],
  List<ConfigurationSummary>? configurations,
  GuiConnectionState state = GuiConnectionState.ready,
  int pid = 1,
  String daemonStatus = 'running',
  bool busy = false,
}) => GuiCapabilities(
  state: state,
  busy: busy,
  snapshot: GuiSnapshot(
    daemon: DaemonStatus(
      productVersion: 'test',
      buildId: 'test',
      pid: pid,
      status: daemonStatus,
      mode: 'desktop',
    ),
    profiles: profiles,
    configurations: configurations ?? [configuration()],
    sessions: sessions,
  ),
);
