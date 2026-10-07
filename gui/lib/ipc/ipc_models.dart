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

// Scan raw JSON before maps erase duplicates. Object keys are decoded so
// escaped and literal aliases coincide; each object owns its own key set.
Object? decodeBindingCheckedJson(
  String source, {
  bool preserveNetworkRevision = false,
  bool preserveNetworkRunGeneration = false,
}) {
  try {
    var i = 0;
    final strictUnicode =
        preserveNetworkRevision || preserveNetworkRunGeneration;
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
          final text = jsonDecode(source.substring(start, i)) as String;
          if (strictUnicode &&
              text.runes.any((r) => r >= 0xd800 && r <= 0xdfff)) {
            throw const IpcProtocolException();
          }
          return text;
        }
      }
      throw const IpcProtocolException();
    }

    late Object? Function([String?]) value;
    value = ([String? key]) {
      whitespace();
      if (i >= source.length) {
        throw const IpcProtocolException();
      }
      final exact =
          preserveNetworkRevision && key == 'revision' ||
          preserveNetworkRunGeneration && key == 'runGeneration';
      if (source[i] == '{') {
        if (exact) {
          throw const IpcProtocolException();
        }
        i++;
        whitespace();
        final result = <String, dynamic>{};
        if (i < source.length && source[i] == '}') {
          i++;
          return result;
        }
        while (true) {
          whitespace();
          final name = string();
          if (result.containsKey(name)) {
            throw const IpcProtocolException();
          }
          whitespace();
          if (i >= source.length || source[i++] != ':') {
            throw const IpcProtocolException();
          }
          result[name] = value(name);
          whitespace();
          if (i >= source.length) {
            throw const IpcProtocolException();
          }
          final next = source[i++];
          if (next == '}') {
            return result;
          }
          if (next != ',') {
            throw const IpcProtocolException();
          }
        }
      }
      if (source[i] == '[') {
        if (exact) {
          throw const IpcProtocolException();
        }
        i++;
        whitespace();
        final result = <dynamic>[];
        if (i < source.length && source[i] == ']') {
          i++;
          return result;
        }
        while (true) {
          result.add(value());
          whitespace();
          if (i >= source.length) {
            throw const IpcProtocolException();
          }
          final next = source[i++];
          if (next == ']') {
            return result;
          }
          if (next != ',') {
            throw const IpcProtocolException();
          }
        }
      }
      if (source[i] == '"') {
        if (exact) {
          throw const IpcProtocolException();
        }
        return string();
      }
      final start = i;
      while (i < source.length &&
          !const [',', '}', ']', ' ', '\t', '\n', '\r'].contains(source[i])) {
        i++;
      }
      if (start == i) {
        throw const IpcProtocolException();
      }
      final token = source.substring(start, i);
      if (exact) {
        if (!RegExp(r'^(0|[1-9][0-9]*)$').hasMatch(token)) {
          throw const IpcProtocolException();
        }
        final n = BigInt.parse(token);
        if (n > BigInt.parse('18446744073709551615')) {
          throw const IpcProtocolException();
        }
        return n;
      }
      return jsonDecode(token);
    };
    final decoded = value();
    whitespace();
    if (i != source.length) {
      throw const IpcProtocolException();
    }
    return decoded;
  } on FormatException {
    throw const IpcProtocolException();
  }
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

// These immutable wire models are an optional query capability; the GUI does
// not fetch them or own another network cache.
class NetworkInterfacesSnapshot {
  NetworkInterfacesSnapshot({
    required this.available,
    required this.revision,
    required List<NetworkInterfaceRow> interfaces,
    this.observedAt,
  }) : interfaces = List.unmodifiable(interfaces);
  final bool available;
  final BigInt revision;
  final String? observedAt;
  final List<NetworkInterfaceRow> interfaces;
}

class NetworkInterfaceRow {
  NetworkInterfaceRow({
    required this.interfaceId,
    required this.displayName,
    required this.operationalState,
    required this.physicalMedium,
    required this.hardwareBacked,
    required this.physicalConnectorPresent,
    required this.filterInterface,
    required this.endpointInterface,
    required this.addressAssignmentMethod,
    required List<NetworkIPv4Assignment> ipv4Assignments,
  }) : ipv4Assignments = List.unmodifiable(ipv4Assignments);
  final String interfaceId,
      displayName,
      operationalState,
      physicalMedium,
      addressAssignmentMethod;
  final bool hardwareBacked,
      physicalConnectorPresent,
      filterInterface,
      endpointInterface;
  final List<NetworkIPv4Assignment> ipv4Assignments;
}

class NetworkIPv4Assignment {
  const NetworkIPv4Assignment({
    required this.address,
    required this.prefixLength,
    required this.automaticCandidate,
    required this.explicitBindable,
  });
  final String address;
  final int prefixLength;
  final bool automaticCandidate, explicitBindable;
}

NetworkInterfacesSnapshot decodeNetworkInterfaces(String source) =>
    decodeNetworkInterfacesValue(
      decodeBindingCheckedJson(source, preserveNetworkRevision: true),
    );

NetworkInterfacesSnapshot decodeNetworkInterfacesValue(Object? raw) {
  if (raw is! Map<String, dynamic> || raw['available'] is! bool) {
    throw const IpcProtocolException();
  }
  final available = raw['available'] as bool;
  final root = _object(raw, {
    'available',
    'revision',
    'interfaces',
    if (available) 'observedAt',
  });
  final revision = root['revision'];
  if (revision is! BigInt ||
      (available ? revision <= BigInt.zero : revision != BigInt.zero)) {
    throw const IpcProtocolException();
  }
  final rows = _list(root['interfaces']).map((raw) {
    final row = _object(raw, const {
      'interfaceId',
      'displayName',
      'operationalState',
      'physicalMedium',
      'hardwareBacked',
      'physicalConnectorPresent',
      'filterInterface',
      'endpointInterface',
      'addressAssignmentMethod',
      'ipv4Assignments',
    });
    String enumValue(String key, Set<String> allowed) {
      final value = _text(row[key]);
      if (!allowed.contains(value)) throw const IpcProtocolException();
      return value;
    }

    final id = _text(row['interfaceId']);
    final name = _optionalText(row['displayName']);
    if ([
      id,
      name,
    ].any((text) => text.runes.any((r) => r >= 0xd800 && r <= 0xdfff))) {
      throw const IpcProtocolException();
    }
    return NetworkInterfaceRow(
      interfaceId: id,
      displayName: name,
      operationalState: enumValue('operationalState', const {'up', 'down'}),
      physicalMedium: enumValue('physicalMedium', const {
        'wired',
        'wireless',
        'unknown',
      }),
      addressAssignmentMethod: enumValue('addressAssignmentMethod', const {
        'unknown',
        'static',
        'dhcp',
      }),
      hardwareBacked: _bool(row['hardwareBacked']),
      physicalConnectorPresent: _bool(row['physicalConnectorPresent']),
      filterInterface: _bool(row['filterInterface']),
      endpointInterface: _bool(row['endpointInterface']),
      ipv4Assignments: _list(row['ipv4Assignments']).map((raw) {
        final address = _object(raw, const {
          'address',
          'prefixLength',
          'automaticCandidate',
          'explicitBindable',
        });
        final text = _text(address['address']);
        final parts = text.split('.');
        if (parts.length != 4 ||
            parts.any((p) {
              final n = int.tryParse(p);
              return n == null || n < 0 || n > 255 || n.toString() != p;
            })) {
          throw const IpcProtocolException();
        }
        final prefix = address['prefixLength'];
        if (prefix is! int || prefix < 0 || prefix > 32) {
          throw const IpcProtocolException();
        }
        return NetworkIPv4Assignment(
          address: text,
          prefixLength: prefix,
          automaticCandidate: _bool(address['automaticCandidate']),
          explicitBindable: _bool(address['explicitBindable']),
        );
      }).toList(),
    );
  }).toList();
  String? observedAt;
  if (available) {
    observedAt = _text(root['observedAt']);
    if (!RegExp(
      r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$',
    ).hasMatch(observedAt)) {
      throw const IpcProtocolException();
    }
    final observed = _time(observedAt);
    // Reject the zero Go observation, while retaining nonzero nanoseconds.
    if (observed.isAtSameMomentAs(DateTime.utc(1)) &&
        !RegExp(r'\.\d*[1-9]\d*').hasMatch(observedAt)) {
      throw const IpcProtocolException();
    }
  } else if (rows.isNotEmpty) {
    throw const IpcProtocolException();
  }
  return NetworkInterfacesSnapshot(
    available: available,
    revision: revision,
    interfaces: rows,
    observedAt: observedAt,
  );
}

class NetworkEndpoint {
  const NetworkEndpoint(this.address, this.port);
  final String address;
  final int port;
}

class NetworkDiagnosticRoute {
  const NetworkDiagnosticRoute({
    required this.interfaceId,
    required this.interfaceIndex,
    required this.sourceIPv4,
    required this.destinationPrefix,
    required this.nextHopIPv4,
    required this.routeMetric,
    required this.interfaceMetric,
    required this.effectiveMetric,
  });
  final String interfaceId, sourceIPv4, destinationPrefix, nextHopIPv4;
  final int interfaceIndex, routeMetric, interfaceMetric, effectiveMetric;
}

class NetworkDiagnosticProbe {
  const NetworkDiagnosticProbe(this.status, this.roundTripTimeMs);
  final String status;
  final int? roundTripTimeMs;
}

class NetworkProtocolSocket {
  const NetworkProtocolSocket({
    required this.state,
    required this.runGeneration,
    this.updatedAt,
    this.localEndpoint,
    this.remoteEndpoint,
  });
  final String state;
  final BigInt runGeneration;
  final String? updatedAt;
  final NetworkEndpoint? localEndpoint, remoteEndpoint;
}

class NetworkDiagnosis {
  const NetworkDiagnosis({
    required this.observedAt,
    required this.selectionBasis,
    required this.status,
    this.unsupportedReason,
    this.target,
    this.route,
    this.probe,
    this.protocolSocket,
  });
  final String observedAt, selectionBasis, status;
  final String? unsupportedReason;
  final NetworkEndpoint? target;
  final NetworkDiagnosticRoute? route;
  final NetworkDiagnosticProbe? probe;
  final NetworkProtocolSocket? protocolSocket;
}

const networkSelectionBases = {
  'session_binding',
  'configuration_explicit',
  'os_route_proposal',
};
const networkDiagnosticStatuses = {
  'available',
  'unsupported',
  'binding_unavailable',
  'route_unavailable',
};
const networkUnsupportedReasons = {'platform', 'protocol', 'destination'};
const networkProbeStatuses = {
  'not_requested',
  'reachable',
  'no_reply',
  'unreachable',
  'failed',
};
const networkSocketStates = {
  'not_observed',
  'open',
  'closed',
  'close_failed',
  'close_unconfirmed',
};
int _networkUint(Object? value, int max, {bool positive = false}) {
  if (value is! int || value < (positive ? 1 : 0) || value > max) {
    throw const IpcProtocolException();
  }
  return value;
}

String _networkEnum(Object? value, Set<String> allowed) {
  final text = _text(value);
  if (!allowed.contains(text)) {
    throw const IpcProtocolException();
  }
  return text;
}

String _networkTime(Object? value) {
  final text = _text(value);
  if (!RegExp(
    r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$',
  ).hasMatch(text)) {
    throw const IpcProtocolException();
  }
  final parsed = _time(text);
  if (parsed.isAtSameMomentAs(DateTime.utc(1)) &&
      !RegExp(r'\.\d*[1-9]\d*').hasMatch(text)) {
    throw const IpcProtocolException();
  }
  return text;
}

int _networkIPv4(
  String text, {
  bool zero = false,
  bool broadcast = false,
  bool multicast = false,
}) {
  final parts = text.split('.');
  if (parts.length != 4) {
    throw const IpcProtocolException();
  }
  var n = 0;
  for (final part in parts) {
    final v = int.tryParse(part);
    if (v == null || v < 0 || v > 255 || v.toString() != part) {
      throw const IpcProtocolException();
    }
    n = n * 256 + v;
  }
  if ((!zero && n == 0) ||
      (!broadcast && n == 4294967295) ||
      (!multicast && n >= 3758096384 && n <= 4026531839)) {
    throw const IpcProtocolException();
  }
  return n;
}

NetworkEndpoint _networkEndpoint(Object? raw, {bool broadcast = false}) {
  final v = _object(raw, const {'address', 'port'});
  final address = _text(v['address']);
  _networkIPv4(address, broadcast: broadcast);
  return NetworkEndpoint(
    address,
    _networkUint(v['port'], 65535, positive: true),
  );
}

Map<String, dynamic> _networkObject(
  Object? raw,
  Set<String> required,
  Set<String> optional,
) {
  if (raw is! Map<String, dynamic> ||
      !raw.keys.toSet().containsAll(required) ||
      raw.keys.any((k) => !required.contains(k) && !optional.contains(k)) ||
      raw.values.any((v) => v == null)) {
    throw const IpcProtocolException();
  }
  return raw;
}

NetworkDiagnosis decodeNetworkDiagnosis(String source) =>
    decodeNetworkDiagnosisValue(
      decodeBindingCheckedJson(source, preserveNetworkRunGeneration: true),
    );
NetworkDiagnosis decodeNetworkDiagnosisValue(Object? raw) {
  final v = _networkObject(
    raw,
    const {'observedAt', 'selectionBasis', 'status'},
    const {'unsupportedReason', 'target', 'route', 'probe', 'protocolSocket'},
  );
  final observed = _networkTime(v['observedAt']);
  final basis = _networkEnum(v['selectionBasis'], networkSelectionBases);
  final status = _networkEnum(v['status'], networkDiagnosticStatuses);
  final reason = v.containsKey('unsupportedReason')
      ? _networkEnum(v['unsupportedReason'], networkUnsupportedReasons)
      : null;
  if (status == 'unsupported' ? reason == null : reason != null) {
    throw const IpcProtocolException();
  }
  final target = v.containsKey('target')
      ? _networkEndpoint(
          v['target'],
          broadcast: status == 'unsupported' && reason == 'destination',
        )
      : null;
  if (target == null &&
      !(status == 'unsupported' &&
          (reason == 'protocol' || reason == 'destination'))) {
    throw const IpcProtocolException();
  }
  NetworkDiagnosticRoute? route;
  NetworkDiagnosticProbe? probe;
  if (status == 'available') {
    final r = _object(v['route'], const {
      'interfaceId',
      'interfaceIndex',
      'sourceIPv4',
      'destinationPrefix',
      'nextHopIPv4',
      'routeMetric',
      'interfaceMetric',
      'effectiveMetric',
    });
    final source = _text(r['sourceIPv4']);
    _networkIPv4(source);
    final next = _text(r['nextHopIPv4']);
    _networkIPv4(next, zero: true);
    final prefix = _text(r['destinationPrefix']);
    final parts = prefix.split('/');
    if (parts.length != 2) {
      throw const IpcProtocolException();
    }
    final bits = int.tryParse(parts[1]);
    if (bits == null || bits < 0 || bits > 32 || bits.toString() != parts[1]) {
      throw const IpcProtocolException();
    }
    final address = _networkIPv4(
      parts[0],
      zero: true,
      broadcast: true,
      multicast: true,
    );
    final block = 1 << (32 - bits);
    if (address % block != 0 ||
        _networkIPv4(target!.address) ~/ block != address ~/ block) {
      throw const IpcProtocolException();
    }
    final rm = _networkUint(r['routeMetric'], 4294967295);
    final im = _networkUint(r['interfaceMetric'], 4294967295);
    final em = _networkUint(r['effectiveMetric'], 8589934590);
    if (rm + im != em) {
      throw const IpcProtocolException();
    }
    route = NetworkDiagnosticRoute(
      interfaceId: _text(r['interfaceId']),
      interfaceIndex: _networkUint(
        r['interfaceIndex'],
        4294967295,
        positive: true,
      ),
      sourceIPv4: source,
      destinationPrefix: prefix,
      nextHopIPv4: next,
      routeMetric: rm,
      interfaceMetric: im,
      effectiveMetric: em,
    );
    final p = _networkObject(
      v['probe'],
      const {'status'},
      const {'roundTripTimeMs'},
    );
    final ps = _networkEnum(p['status'], networkProbeStatuses);
    final rtt = p.containsKey('roundTripTimeMs')
        ? _networkUint(p['roundTripTimeMs'], 4294967295)
        : null;
    if ((ps == 'reachable') != (rtt != null)) {
      throw const IpcProtocolException();
    }
    probe = NetworkDiagnosticProbe(ps, rtt);
  } else if (v.containsKey('route') || v.containsKey('probe')) {
    throw const IpcProtocolException();
  }
  NetworkProtocolSocket? socket;
  if (v.containsKey('protocolSocket')) {
    final s = _networkObject(
      v['protocolSocket'],
      const {'state', 'runGeneration'},
      const {'updatedAt', 'localEndpoint', 'remoteEndpoint'},
    );
    final state = _networkEnum(s['state'], networkSocketStates);
    final generation = s['runGeneration'];
    if (generation is! BigInt ||
        generation < BigInt.zero ||
        generation > BigInt.parse('18446744073709551615')) {
      throw const IpcProtocolException();
    }
    final updated = s.containsKey('updatedAt')
        ? _networkTime(s['updatedAt'])
        : null;
    NetworkEndpoint? local, remote;
    if (state == 'not_observed') {
      if (s.containsKey('localEndpoint') ||
          s.containsKey('remoteEndpoint') ||
          (generation == BigInt.zero) != (updated == null)) {
        throw const IpcProtocolException();
      }
    } else {
      if (generation == BigInt.zero || updated == null) {
        throw const IpcProtocolException();
      }
      local = _networkEndpoint(s['localEndpoint']);
      remote = _networkEndpoint(s['remoteEndpoint'], broadcast: true);
    }
    socket = NetworkProtocolSocket(
      state: state,
      runGeneration: generation,
      updatedAt: updated,
      localEndpoint: local,
      remoteEndpoint: remote,
    );
  }
  return NetworkDiagnosis(
    observedAt: observed,
    selectionBasis: basis,
    status: status,
    unsupportedReason: reason,
    target: target,
    route: route,
    probe: probe,
    protocolSocket: socket,
  );
}

Map<String, Object> networkDiagnosisPayload({
  String? configurationId,
  String? sessionId,
  bool probe = false,
}) {
  if ((configurationId == null) == (sessionId == null) ||
      configurationId == '' ||
      sessionId == '' ||
      [configurationId, sessionId].whereType<String>().any(
        (s) => s.runes.any((r) => r >= 0xd800 && r <= 0xdfff),
      )) {
    throw const IpcProtocolException();
  }
  return {
    'configurationId': ?configurationId,
    'sessionId': ?sessionId,
    if (probe) 'probe': true,
  };
}

Map<String, Object> decodeNetworkDiagnosisPayload(String source) {
  final v = _networkObject(
    decodeBindingCheckedJson(source, preserveNetworkRunGeneration: true),
    const {},
    const {'configurationId', 'sessionId', 'probe'},
  );
  if (v.containsKey('configurationId') == v.containsKey('sessionId')) {
    throw const IpcProtocolException();
  }
  return networkDiagnosisPayload(
    configurationId: v.containsKey('configurationId')
        ? _text(v['configurationId'])
        : null,
    sessionId: v.containsKey('sessionId') ? _text(v['sessionId']) : null,
    probe: v.containsKey('probe') ? _bool(v['probe']) : false,
  );
}
