part of 'ipc_models.dart';

enum ConfigurationRuntimeAvailability {
  available('available'),
  profileUnavailable('profile_unavailable'),
  protocolUnavailable('protocol_unavailable'),
  overrideInvalid('override_invalid');

  const ConfigurationRuntimeAvailability(this.wireValue);
  final String wireValue;
  static ConfigurationRuntimeAvailability decode(String value) =>
      values.firstWhere(
        (item) => item.wireValue == value,
        orElse: () => throw const IpcProtocolException(),
      );
}

class ConfigurationRemoveResult {
  const ConfigurationRemoveResult({
    required this.configurationId,
    required this.status,
  });
  final String configurationId, status;
}

class ConfigurationSummary {
  const ConfigurationSummary({
    required this.id,
    required this.displayName,
    required this.institutionProfileId,
    required this.institutionDisplayName,
    required this.authenticationProtocolId,
    required this.username,
    required this.credentialStored,
    required this.storageProtection,
    this.networkBindingPolicy = const NetworkBindingPolicy.automatic(),
    this.autoLogin = false,
    this.autoReconnect = false,
    this.runtimeAvailability = ConfigurationRuntimeAvailability.available,
    this.version,
    this.overrideState,
    this.overrideSchemaVersion = 0,
  });
  final String id,
      displayName,
      institutionProfileId,
      institutionDisplayName,
      authenticationProtocolId,
      username,
      storageProtection;
  final NetworkBindingPolicy networkBindingPolicy;
  final bool credentialStored, autoLogin, autoReconnect;
  final ConfigurationRuntimeAvailability runtimeAvailability;
  final String? version, overrideState;
  final int overrideSchemaVersion;
}

ConfigurationRemoveResult decodeConfigurationRemove(
  String source, {
  required String expectedConfigurationId,
}) {
  final value = decodeObject(source, const {'configurationId', 'status'});
  final configurationId = _text(value['configurationId']);
  final status = _text(value['status']);
  if (configurationId != expectedConfigurationId || status != 'removed') {
    throw const IpcProtocolException();
  }
  return ConfigurationRemoveResult(
    configurationId: configurationId,
    status: status,
  );
}

List<ConfigurationSummary> decodeConfigurations(String source) {
  final root = _object(
    decodeBindingCheckedJson(source, preserveUnsignedRevision: true),
    const {'storageProtection', 'configurations'},
  );
  _protection(root['storageProtection']);
  return _list(root['configurations'])
      .map(_configuration)
      .toList(growable: false);
}

ConfigurationSummary decodeConfiguration(String source) => _configuration(
  decodeBindingCheckedJson(source, preserveUnsignedRevision: true),
);

ConfigurationSummary _configuration(Object? raw) {
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  final v = _object(raw, {
    if (raw.containsKey('version')) 'version',
    if (raw.containsKey('overrideState')) 'overrideState',
    if (raw.containsKey('overrideSchemaVersion')) 'overrideSchemaVersion',
    'configurationId',
    'displayName',
    'institutionProfileId',
    'institutionDisplayName',
    'authenticationProtocolId',
    'username',
    'credentialStored',
    'runtimeAvailability',
    'storageProtection',
    'autoLogin',
    'autoReconnect',
    'networkBindingPolicy',
  });
  final availability = ConfigurationRuntimeAvailability.decode(
    _text(v['runtimeAvailability']),
  );
  final institution = _optionalText(v['institutionDisplayName']);
  final protocol = _optionalText(v['authenticationProtocolId']);
  if (availability == ConfigurationRuntimeAvailability.profileUnavailable
      ? institution.isNotEmpty || protocol.isNotEmpty
      : institution.isEmpty || protocol.isEmpty) {
    throw const IpcProtocolException();
  }
  return ConfigurationSummary(
    runtimeAvailability: availability,
    version: v.containsKey('version') ? _text(v['version']) : null,
    overrideState: v.containsKey('overrideState')
        ? _text(v['overrideState'])
        : null,
    overrideSchemaVersion: v.containsKey('overrideSchemaVersion')
        ? _positive(v['overrideSchemaVersion'])
        : 0,
    id: _text(v['configurationId']),
    displayName: _optionalText(v['displayName']),
    institutionProfileId: _text(v['institutionProfileId']),
    institutionDisplayName: institution,
    authenticationProtocolId: protocol,
    username: _text(v['username']),
    credentialStored: _bool(v['credentialStored']),
    storageProtection: _protection(v['storageProtection']),
    autoLogin: _bool(v['autoLogin']),
    autoReconnect: _bool(v['autoReconnect']),
    networkBindingPolicy: NetworkBindingPolicy.decode(
      v['networkBindingPolicy'],
    ),
  );
}

String _protection(Object? value) {
  final text = _text(value);
  if (!const {'protected', 'unprotected'}.contains(text)) {
    throw const IpcProtocolException();
  }
  return text;
}

typedef ConfigurationOverride = ({
  ConfigurationSummary configuration,
  String? document,
  bool? matchesDocument,
});
typedef OverrideValidation = ({bool valid, String? field});

ConfigurationOverride decodeConfigurationOverride(String source) {
  final raw = decodeBindingCheckedJson(source);
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  final v = _object(raw, {
    'configuration',
    'document',
    if (raw.containsKey('matchesDocument')) 'matchesDocument',
  });
  final document = v['document'];
  if (document != null && document is! String) {
    throw const IpcProtocolException();
  }
  return (
    configuration: _configuration(v['configuration']),
    document: document as String?,
    matchesDocument: v.containsKey('matchesDocument')
        ? _bool(v['matchesDocument'])
        : null,
  );
}

OverrideValidation decodeOverrideValidation(String source) {
  final raw = decodeBindingCheckedJson(source);
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  final v = _object(raw, {'valid', if (raw.containsKey('field')) 'field'});
  return (
    valid: _bool(v['valid']),
    field: v.containsKey('field') ? _text(v['field']) : null,
  );
}
