import 'package:sidravia_gui/ipc/ipc_models.dart';

SessionSummary session(
  SessionState state, {
  String id = 'demo-session',
  String? configurationId = 'demo-config',
}) => SessionSummary(
  id: id,
  displayName: '',
  accountName: 'u',
  state: state,
  intent: SessionIntent.maintainAuthentication,
  configurationId: configurationId,
);
const configuration = ConfigurationSummary(
  id: 'demo-config',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'd',
  username: 'u',
  credentialStored: true,
  storageProtection: 'protected',
);
const otherConfiguration = ConfigurationSummary(
  id: 'other',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'd',
  username: 'other',
  credentialStored: true,
  storageProtection: 'protected',
);
