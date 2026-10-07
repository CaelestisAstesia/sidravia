import 'dart:convert';

const daemonModes = {'headless', 'desktop'};

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

class DaemonStatus {
  const DaemonStatus({
    required this.productVersion,
    required this.buildId,
    required this.pid,
    required this.status,
    required this.mode,
    this.desktopOwnerPid,
  });
  final String productVersion, buildId, status, mode;
  final int pid;
  final int? desktopOwnerPid;
}

class DaemonStopResult {
  const DaemonStopResult({required this.status});
  final String status;
}

class SessionRemoveResult {
  const SessionRemoveResult({required this.sessionId, required this.status});
  final String sessionId, status;
}

class ConfigurationRemoveResult {
  const ConfigurationRemoveResult({
    required this.configurationId,
    required this.status,
  });
  final String configurationId, status;
}

class InstitutionProfile {
  const InstitutionProfile({
    required this.id,
    required this.displayName,
    required this.protocolId,
  });
  final String id, displayName, protocolId;
}

class NetworkBindingPolicy {
  const NetworkBindingPolicy.automatic()
    : mode = 'automatically_select_latest_available',
      interfaceId = null,
      localIpv4Address = null;
  const NetworkBindingPolicy._(
    this.mode,
    this.interfaceId,
    this.localIpv4Address,
  );
  factory NetworkBindingPolicy.explicit(String id, String address) {
    final runes = id.runes.toList();
    bool space(int r) =>
        const {
          0x9,
          0xa,
          0xb,
          0xc,
          0xd,
          0x20,
          0x85,
          0xa0,
          0x1680,
          0x2028,
          0x2029,
          0x202f,
          0x205f,
          0x3000,
        }.contains(r) ||
        (r >= 0x2000 && r <= 0x200a);
    if (runes.isEmpty ||
        utf8.encode(id).length > 256 ||
        space(runes.first) ||
        space(runes.last) ||
        runes.any(
          (r) =>
              r <= 0x1f ||
              (r >= 0x7f && r <= 0x9f) ||
              (r >= 0xd800 && r <= 0xdfff),
        )) {
      throw const IpcProtocolException();
    }
    final parts = address.split('.');
    if (parts.length != 4) throw const IpcProtocolException();
    final values = <int>[];
    for (final part in parts) {
      final n = int.tryParse(part);
      if (n == null || n < 0 || n > 255 || n.toString() != part) {
        throw const IpcProtocolException();
      }
      values.add(n);
    }
    if (values.every((n) => n == 0) ||
        (values.first >= 224 && values.first <= 239)) {
      throw const IpcProtocolException();
    }
    return NetworkBindingPolicy._(
      'explicit_interface_and_local_ipv4',
      id,
      address,
    );
  }
  final String mode;
  final String? interfaceId, localIpv4Address;
  Map<String, Object> toJson() => {
    'mode': mode,
    'interfaceId': ?interfaceId,
    'localIpv4Address': ?localIpv4Address,
  };
  String get summary => interfaceId == null
      ? '自动选择可用网卡'
      : '指定网卡：$interfaceId\n本地 IPv4：$localIpv4Address';
  static NetworkBindingPolicy decode(Object? raw) {
    if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
    if (raw['mode'] == 'automatically_select_latest_available') {
      _object(raw, const {'mode'});
      return const NetworkBindingPolicy.automatic();
    }
    final v = _object(raw, const {'mode', 'interfaceId', 'localIpv4Address'});
    if (v['mode'] != 'explicit_interface_and_local_ipv4') {
      throw const IpcProtocolException();
    }
    return NetworkBindingPolicy.explicit(
      _text(v['interfaceId']),
      _text(v['localIpv4Address']),
    );
  }

  @override
  bool operator ==(Object other) =>
      other is NetworkBindingPolicy &&
      mode == other.mode &&
      interfaceId == other.interfaceId &&
      localIpv4Address == other.localIpv4Address;
  @override
  int get hashCode => Object.hash(mode, interfaceId, localIpv4Address);
}

// Scan raw JSON before maps erase duplicates. Only owned binding keys are
// checked; string keys are decoded so escaped and literal aliases coincide.
Object? decodeBindingCheckedJson(String source) {
  var i = 0;
  void whitespace() {
    while (i < source.length &&
        const [9, 10, 13, 32].contains(source.codeUnitAt(i))) {
      i++;
    }
  }

  String string() {
    final start = i;
    if (i >= source.length || source[i] != '"') {
      throw const IpcProtocolException();
    }
    i++;
    while (i < source.length) {
      if (source[i] == '\\') {
        i += 2;
        continue;
      }
      if (source[i++] == '"') {
        return jsonDecode(source.substring(start, i)) as String;
      }
    }
    throw const IpcProtocolException();
  }

  late void Function(bool) value;
  value = (bool policy) {
    whitespace();
    if (i >= source.length) throw const IpcProtocolException();
    if (source[i] == '{') {
      i++;
      whitespace();
      final keys = <String>{};
      if (i < source.length && source[i] == '}') {
        i++;
        return;
      }
      while (true) {
        whitespace();
        final key = string();
        if ((policy || key == 'networkBindingPolicy') && !keys.add(key)) {
          throw const IpcProtocolException();
        }
        keys.add(key);
        whitespace();
        if (i >= source.length || source[i++] != ':') {
          throw const IpcProtocolException();
        }
        value(key == 'networkBindingPolicy');
        whitespace();
        if (i >= source.length) throw const IpcProtocolException();
        final next = source[i++];
        if (next == '}') return;
        if (next != ',') throw const IpcProtocolException();
      }
    }
    if (source[i] == '[') {
      i++;
      whitespace();
      if (i < source.length && source[i] == ']') {
        i++;
        return;
      }
      while (true) {
        value(false);
        whitespace();
        if (i >= source.length) throw const IpcProtocolException();
        final next = source[i++];
        if (next == ']') return;
        if (next != ',') throw const IpcProtocolException();
      }
    }
    if (source[i] == '"') {
      string();
      return;
    }
    final start = i;
    while (i < source.length &&
        !const [',', '}', ']', ' ', '\t', '\n', '\r'].contains(source[i])) {
      i++;
    }
    if (start == i) throw const IpcProtocolException();
    jsonDecode(source.substring(start, i));
  };
  value(false);
  whitespace();
  if (i != source.length) throw const IpcProtocolException();
  return jsonDecode(source);
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
    this.stateReason,
    this.selectedNetworkBinding,
    this.authenticationEstablishedAt,
    this.nextRetryAt,
    this.lastAuthenticationFailure,
    this.revision = 0,
    this.updatedAt,
  });
  final String id, displayName, accountName;
  final SessionState state;
  final SessionIntent intent;
  final String? configurationId;
  final SessionStateReason? stateReason;
  final SessionNetworkBinding? selectedNetworkBinding;
  final DateTime? authenticationEstablishedAt, nextRetryAt, updatedAt;
  final SessionAuthenticationFailure? lastAuthenticationFailure;
  final int revision;
}

class IpcProtocolException implements Exception {
  const IpcProtocolException();
}

/// Stable transport diagnostics; never contains socket text, endpoints or tokens.
class IpcTransportException implements Exception {
  const IpcTransportException(this.code);
  final String code;
}

class IpcRequestFailure implements Exception {
  const IpcRequestFailure(this.code);
  final String code;
}

Map<String, dynamic> decodeObject(String source, Set<String> keys) =>
    _object(decodeBindingCheckedJson(source), keys);

DaemonStatus decodeDaemonStatus(String source) {
  final raw = decodeBindingCheckedJson(source);
  if (raw is! Map<String, dynamic>) throw const IpcProtocolException();
  final mode = _text(raw['mode']);
  final expected = mode == 'desktop'
      ? const {
          'productVersion',
          'buildId',
          'pid',
          'status',
          'mode',
          'desktopOwnerPid',
        }
      : const {'productVersion', 'buildId', 'pid', 'status', 'mode'};
  final value = _object(raw, expected);
  if (!daemonModes.contains(mode)) throw const IpcProtocolException();
  return DaemonStatus(
    productVersion: _text(value['productVersion']),
    buildId: _text(value['buildId']),
    pid: _positive(value['pid']),
    status: _text(value['status']),
    mode: mode,
    desktopOwnerPid: mode == 'desktop'
        ? _positive(value['desktopOwnerPid'])
        : null,
  );
}

DaemonStopResult decodeDaemonStop(String source) {
  final value = decodeObject(source, const {'status'});
  final status = _text(value['status']);
  if (status != 'stopping') throw const IpcProtocolException();
  return DaemonStopResult(status: status);
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

List<InstitutionProfile> decodeProfiles(String source) =>
    _list(decodeObject(source, const {'profiles'})['profiles'])
        .map((raw) {
          final v = _object(raw, const {
            'institutionProfileId',
            'displayName',
            'authenticationProtocolId',
          });
          return InstitutionProfile(
            id: _text(v['institutionProfileId']),
            displayName: _text(v['displayName']),
            protocolId: _text(v['authenticationProtocolId']),
          );
        })
        .toList(growable: false);

List<ConfigurationSummary> decodeConfigurations(String source) {
  final root = decodeObject(source, const {
    'storageProtection',
    'configurations',
  });
  _protection(root['storageProtection']);
  return _list(root['configurations'])
      .map(_configuration)
      .toList(growable: false);
}

ConfigurationSummary decodeConfiguration(String source) =>
    _configuration(decodeBindingCheckedJson(source));

ConfigurationSummary _configuration(Object? raw) {
  final v = _object(raw, const {
    'configurationId',
    'displayName',
    'institutionProfileId',
    'institutionDisplayName',
    'authenticationProtocolId',
    'username',
    'credentialStored',
    'storageProtection',
    'autoLogin',
    'autoReconnect',
    'networkBindingPolicy',
  });
  return ConfigurationSummary(
    id: _text(v['configurationId']),
    displayName: _optionalText(v['displayName']),
    institutionProfileId: _text(v['institutionProfileId']),
    institutionDisplayName: _text(v['institutionDisplayName']),
    authenticationProtocolId: _text(v['authenticationProtocolId']),
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

List<SessionSummary> decodeSessions(String source) =>
    _list(decodeObject(source, const {'sessions'})['sessions'])
        .map(_session)
        .toList(growable: false);

SessionSummary decodeSession(String source) =>
    _session(decodeBindingCheckedJson(source));

SessionSummary decodeSessionOperation(String source) {
  final value = _object(decodeBindingCheckedJson(source), const {
    'outcome',
    'session',
  });
  if (!const {
    'created',
    'already_running',
    'resumed',
  }.contains(_text(value['outcome']))) {
    throw const IpcProtocolException();
  }
  return _session(value['session']);
}

SessionSummary _session(Object? raw) {
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
    'updatedAt',
  };
  const optional = {
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
  if (revision is! int || revision < 0) throw const IpcProtocolException();
  return SessionSummary(
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
        ? _time(raw['authenticationEstablishedAt'])
        : null,
    nextRetryAt: raw.containsKey('nextRetryAt')
        ? _time(raw['nextRetryAt'])
        : null,
    lastAuthenticationFailure: raw.containsKey('lastAuthenticationFailure')
        ? _failure(raw['lastAuthenticationFailure'])
        : null,
    revision: revision,
    updatedAt: _time(raw['updatedAt']),
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
    displayName: _text(v['displayName']),
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

Map<String, dynamic> _object(Object? value, Set<String> keys) {
  if (value is! Map<String, dynamic> ||
      value.length != keys.length ||
      !value.keys.toSet().containsAll(keys)) {
    throw const IpcProtocolException();
  }
  return value;
}

List<dynamic> _list(Object? value) {
  if (value is! List<dynamic>) throw const IpcProtocolException();
  return value;
}

String _text(Object? value) {
  if (value is! String || value.isEmpty) throw const IpcProtocolException();
  return value;
}

String _optionalText(Object? value) {
  if (value is! String) throw const IpcProtocolException();
  return value;
}

bool _bool(Object? value) {
  if (value is! bool) throw const IpcProtocolException();
  return value;
}

int _positive(Object? value) {
  if (value is! int || value <= 0) throw const IpcProtocolException();
  return value;
}

DateTime _time(Object? value) {
  final text = _text(value);
  final match = RegExp(
    r'^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|[+-](\d\d):(\d\d))$',
  ).firstMatch(text);
  if (match == null) {
    throw const IpcProtocolException();
  }
  final year = int.parse(match[1]!);
  final month = int.parse(match[2]!);
  final day = int.parse(match[3]!);
  final hour = int.parse(match[4]!);
  final minute = int.parse(match[5]!);
  final second = int.parse(match[6]!);
  final offsetHour = match[7] == null ? null : int.parse(match[7]!);
  final offsetMinute = match[8] == null ? null : int.parse(match[8]!);
  final daysInMonth = switch (month) {
    1 || 3 || 5 || 7 || 8 || 10 || 12 => 31,
    4 || 6 || 9 || 11 => 30,
    2 when year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
    2 => 28,
    _ => 0,
  };
  if (day < 1 ||
      day > daysInMonth ||
      hour > 23 ||
      minute > 59 ||
      second > 59 ||
      (offsetHour != null && offsetHour > 23) ||
      (offsetMinute != null && offsetMinute > 59)) {
    throw const IpcProtocolException();
  }
  final parsed = DateTime.tryParse(text);
  if (parsed == null) throw const IpcProtocolException();
  return parsed;
}

String _protection(Object? value) {
  final text = _text(value);
  if (!const {'protected', 'unprotected'}.contains(text)) {
    throw const IpcProtocolException();
  }
  return text;
}
