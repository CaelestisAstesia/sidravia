part of 'ipc_models.dart';

enum SessionState {
  suspended('suspended'),
  waitingForNetwork('waiting_for_network'),
  authenticating('authenticating'),
  authenticated('authenticated'),
  waitingBeforeRetry('waiting_before_retry'),
  blockedByError('blocked_by_error'),
  stopping('stopping');

  const SessionState(this.wireValue);
  final String wireValue;
  static SessionState decode(String value) => values.firstWhere(
    (state) => state.wireValue == value,
    orElse: () => throw const IpcProtocolException(),
  );
}

enum SessionIntent {
  maintainAuthentication('maintain_authentication'),
  suspendAuthentication('suspend_authentication');

  const SessionIntent(this.wireValue);
  final String wireValue;
  static SessionIntent decode(String value) => values.firstWhere(
    (intent) => intent.wireValue == value,
    orElse: () => throw const IpcProtocolException(),
  );
}

class SessionRemoveResult {
  const SessionRemoveResult({required this.sessionId, required this.status});
  final String sessionId, status;
}

class SessionStateReason {
  const SessionStateReason({required this.code, required this.description});
  final String code, description;
}

class SessionNetworkBinding {
  const SessionNetworkBinding({
    required this.interfaceId,
    required this.displayName,
    required this.localIpv4Address,
  });
  final String interfaceId, displayName, localIpv4Address;
}

class SessionAuthenticationFailure {
  const SessionAuthenticationFailure({
    required this.code,
    required this.description,
    required this.handlingRecommendation,
  });
  final String code, description, handlingRecommendation;
}

class SessionSummary {
  const SessionSummary({
    required this.id,
    required this.displayName,
    required this.accountName,
    required this.state,
    required this.intent,
    this.configurationId,
    this.institutionProfileId,
    this.institutionDisplayName,
    this.authenticationProtocolId,
    this.stateReason,
    this.selectedNetworkBinding,
    this.authenticationEstablishedAt,
    this.nextRetryAt,
    this.lastAuthenticationFailure,
    BigInt? revision,
    NetworkProtocolSocket? protocolSocket,
    this.cleanupRequired = false,
    this.hasProtocolOverride = false,
    this.hasOverrideDocument = false,
    this.updatedAt,
  })
    // Public names and private nullable storage preserve const previews.
    // ignore: prefer_initializing_formals
    : _revision = revision,
       // Public names and private nullable storage preserve const previews.
       // ignore: prefer_initializing_formals
       _protocolSocket = protocolSocket;
  final String id, displayName, accountName;
  final SessionState state;
  final SessionIntent intent;
  final String? configurationId;
  final String? institutionProfileId,
      institutionDisplayName,
      authenticationProtocolId;
  final SessionStateReason? stateReason;
  final SessionNetworkBinding? selectedNetworkBinding;
  final DateTime? authenticationEstablishedAt, nextRetryAt, updatedAt;
  final SessionAuthenticationFailure? lastAuthenticationFailure;
  final BigInt? _revision;
  BigInt get revision => _revision ?? BigInt.zero;
  final NetworkProtocolSocket? _protocolSocket;
  static final _previewSocket = NetworkProtocolSocket(
    state: 'not_observed',
    runGeneration: BigInt.zero,
  );
  NetworkProtocolSocket get protocolSocket => _protocolSocket ?? _previewSocket;
  final bool cleanupRequired, hasProtocolOverride, hasOverrideDocument;
}

SessionRemoveResult decodeSessionRemove(
  String source, {
  required String expectedSessionId,
}) {
  final value = decodeObject(source, const {'sessionId', 'status'});
  final sessionId = _text(value['sessionId']);
  final status = _text(value['status']);
  if (sessionId != expectedSessionId || status != 'removed') {
    throw const IpcProtocolException();
  }
  return SessionRemoveResult(sessionId: sessionId, status: status);
}

Object? _sessionJson(String source) => decodeBindingCheckedJson(
  source,
  preserveUnsignedRevision: true,
  preserveRunGeneration: true,
);
List<SessionSummary> decodeSessions(String source) =>
    decodeSessionsValue(_sessionJson(source));
List<SessionSummary> decodeSessionsValue(Object? raw) {
  final root = _object(raw, const {'sessions', 'cleanupRequiredSessionIds'});
  final rows = _list(root['sessions']);
  final ids = <String>{};
  for (final row in rows) {
    if (row is! Map<String, dynamic> || !ids.add(_text(row['sessionId']))) {
      throw const IpcProtocolException();
    }
  }
  final cleanup = <String>{};
  for (final rawId in _list(root['cleanupRequiredSessionIds'])) {
    final id = _text(rawId);
    if (!ids.contains(id) || !cleanup.add(id)) {
      throw const IpcProtocolException();
    }
  }
  return List.unmodifiable(
    rows.map(
      (row) => _session(
        row,
        cleanupRequired: cleanup.contains((row as Map)['sessionId']),
      ),
    ),
  );
}

SessionSummary decodeSession(String source) =>
    decodeSessionValue(_sessionJson(source));
SessionSummary decodeSessionValue(Object? raw) => _session(raw);
SessionSummary decodeSessionOperation(String source) =>
    decodeSessionOperationValue(_sessionJson(source));
SessionSummary decodeSessionOperationValue(Object? raw) {
  final value = _object(raw, const {'outcome', 'session'});
  if (!const {
    'created',
    'already_running',
    'resumed',
  }.contains(_text(value['outcome']))) {
    throw const IpcProtocolException();
  }
  return _session(value['session']);
}

SessionSummary _session(Object? raw, {bool cleanupRequired = false}) {
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  const required = {
    'sessionId',
    'displayName',
    'institutionProfileId',
    'institutionDisplayName',
    'authenticationProtocolId',
    'accountName',
    'intent',
    'state',
    'revision',
    'protocolSocket',
    'updatedAt',
  };
  const optional = {
    'hasProtocolOverride',
    'hasOverrideDocument',
    'configurationId',
    'stateReason',
    'selectedNetworkBinding',
    'authenticationEstablishedAt',
    'nextRetryAt',
    'lastAuthenticationFailure',
  };
  if (!raw.keys.toSet().containsAll(required) ||
      raw.keys.any(
        (key) => !required.contains(key) && !optional.contains(key),
      )) {
    throw const IpcProtocolException();
  }
  final intent = SessionIntent.decode(_text(raw['intent']));
  final state = SessionState.decode(_text(raw['state']));
  for (final key in const [
    'sessionId',
    'institutionProfileId',
    'institutionDisplayName',
    'authenticationProtocolId',
    'accountName',
  ]) {
    _text(raw[key]);
  }
  final revision = raw['revision'];
  if (revision is! BigInt ||
      revision <= BigInt.zero ||
      revision > BigInt.parse('18446744073709551615')) {
    throw const IpcProtocolException();
  }
  return SessionSummary(
    cleanupRequired: cleanupRequired,
    hasOverrideDocument:
        raw.containsKey('hasOverrideDocument') &&
        _bool(raw['hasOverrideDocument']),
    hasProtocolOverride:
        raw.containsKey('hasProtocolOverride') &&
        _bool(raw['hasProtocolOverride']),
    institutionProfileId: _text(raw['institutionProfileId']),
    institutionDisplayName: _text(raw['institutionDisplayName']),
    authenticationProtocolId: _text(raw['authenticationProtocolId']),
    protocolSocket: _decodeProtocolSocket(raw['protocolSocket']),
    id: _text(raw['sessionId']),
    displayName: _optionalText(raw['displayName']),
    accountName: _text(raw['accountName']),
    state: state,
    intent: intent,
    configurationId: raw.containsKey('configurationId')
        ? _text(raw['configurationId'])
        : null,
    stateReason: raw.containsKey('stateReason')
        ? _reason(raw['stateReason'])
        : null,
    selectedNetworkBinding: raw.containsKey('selectedNetworkBinding')
        ? _binding(raw['selectedNetworkBinding'])
        : null,
    authenticationEstablishedAt: raw.containsKey('authenticationEstablishedAt')
        ? _time(_networkTime(raw['authenticationEstablishedAt']))
        : null,
    nextRetryAt: raw.containsKey('nextRetryAt')
        ? _time(_networkTime(raw['nextRetryAt']))
        : null,
    lastAuthenticationFailure: raw.containsKey('lastAuthenticationFailure')
        ? _failure(raw['lastAuthenticationFailure'])
        : null,
    revision: revision,
    updatedAt: _time(_networkTime(raw['updatedAt'])),
  );
}

SessionStateReason _reason(Object? raw) {
  final v = _object(raw, const {'code', 'description'});
  return SessionStateReason(
    code: _text(v['code']),
    description: _text(v['description']),
  );
}

SessionNetworkBinding _binding(Object? raw) {
  final v = _object(raw, const {
    'interfaceId',
    'displayName',
    'localIpv4Address',
  });
  return SessionNetworkBinding(
    interfaceId: _text(v['interfaceId']),
    displayName: _optionalText(v['displayName']),
    localIpv4Address: _text(v['localIpv4Address']),
  );
}

SessionAuthenticationFailure _failure(Object? raw) {
  final v = _object(raw, const {
    'code',
    'description',
    'handlingRecommendation',
  });
  return SessionAuthenticationFailure(
    code: _text(v['code']),
    description: _text(v['description']),
    handlingRecommendation: _text(v['handlingRecommendation']),
  );
}
