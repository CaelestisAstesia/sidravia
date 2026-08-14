import 'dart:convert';

class DaemonStatus {
  const DaemonStatus({
    required this.productVersion,
    required this.buildId,
    required this.pid,
    required this.status,
    required this.mode,
  });

  final String productVersion;
  final String buildId;
  final int pid;
  final String status;
  final String mode;
}

class InstitutionProfile {
  const InstitutionProfile({
    required this.id,
    required this.displayName,
    required this.protocolId,
  });

  final String id;
  final String displayName;
  final String protocolId;
}

class ConfigurationSummary {
  const ConfigurationSummary({
    required this.id,
    required this.displayName,
    required this.institutionDisplayName,
    required this.username,
    required this.credentialStored,
    required this.storageProtection,
  });

  final String id;
  final String displayName;
  final String institutionDisplayName;
  final String username;
  final bool credentialStored;
  final String storageProtection;
}

class SessionSummary {
  const SessionSummary({
    required this.id,
    required this.displayName,
    required this.state,
    required this.intent,
  });

  final String id;
  final String displayName;
  final String state;
  final String intent;
}

class GuiSnapshot {
  const GuiSnapshot({
    required this.daemon,
    required this.profiles,
    required this.configurations,
    required this.sessions,
  });

  final DaemonStatus daemon;
  final List<InstitutionProfile> profiles;
  final List<ConfigurationSummary> configurations;
  final List<SessionSummary> sessions;
}

class IpcProtocolException implements Exception {
  const IpcProtocolException();
}

Map<String, dynamic> decodeObject(String source, Set<String> keys) {
  final value = jsonDecode(source);
  if (value is! Map<String, dynamic> ||
      value.length != keys.length ||
      !value.keys.toSet().containsAll(keys)) {
    throw const IpcProtocolException();
  }
  return value;
}

DaemonStatus decodeDaemonStatus(String source) {
  final value = decodeObject(source, const {
    'productVersion',
    'buildId',
    'pid',
    'status',
    'mode',
  });
  if (value['productVersion'] is! String ||
      value['buildId'] is! String ||
      value['pid'] is! int ||
      value['status'] is! String ||
      value['mode'] is! String ||
      (value['productVersion'] as String).isEmpty ||
      (value['buildId'] as String).isEmpty ||
      (value['pid'] as int) <= 0 ||
      !const {'headless', 'desktop'}.contains(value['mode'])) {
    throw const IpcProtocolException();
  }
  return DaemonStatus(
    productVersion: value['productVersion'] as String,
    buildId: value['buildId'] as String,
    pid: value['pid'] as int,
    status: value['status'] as String,
    mode: value['mode'] as String,
  );
}

List<InstitutionProfile> decodeProfiles(String source) {
  final value = decodeObject(source, const {'profiles'});
  return _list(value['profiles'])
      .map((entry) {
        final item = _object(entry, const {
          'institutionProfileId',
          'displayName',
          'authenticationProtocolId',
        });
        return InstitutionProfile(
          id: _nonEmpty(item['institutionProfileId']),
          displayName: _nonEmpty(item['displayName']),
          protocolId: _nonEmpty(item['authenticationProtocolId']),
        );
      })
      .toList(growable: false);
}

List<ConfigurationSummary> decodeConfigurations(String source) {
  final value = decodeObject(source, const {
    'storageProtection',
    'configurations',
  });
  _storageProtection(value['storageProtection']);
  return _list(value['configurations'])
      .map((entry) {
        final item = _object(entry, const {
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
        });
        return ConfigurationSummary(
          id: _nonEmpty(item['configurationId']),
          displayName: _nonEmpty(item['displayName']),
          institutionDisplayName: _nonEmpty(item['institutionDisplayName']),
          username: _nonEmpty(item['username']),
          credentialStored: _bool(item['credentialStored']),
          storageProtection: _storageProtection(item['storageProtection']),
        );
      })
      .toList(growable: false);
}

List<SessionSummary> decodeSessions(String source) {
  final value = decodeObject(source, const {'sessions'});
  return _list(value['sessions'])
      .map((entry) {
        final item = _object(entry, const {
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
        });
        final state = _nonEmpty(item['state']);
        final intent = _nonEmpty(item['intent']);
        if (!const {
              'suspended',
              'waiting_for_network',
              'authenticating',
              'authenticated',
              'waiting_before_retry',
              'blocked_by_error',
              'stopping',
            }.contains(state) ||
            !const {
              'maintain_authentication',
              'suspend_authentication',
            }.contains(intent) ||
            item['revision'] is! int ||
            item['updatedAt'] is! String) {
          throw const IpcProtocolException();
        }
        return SessionSummary(
          id: _nonEmpty(item['sessionId']),
          displayName: _nonEmpty(item['displayName']),
          state: state,
          intent: intent,
        );
      })
      .toList(growable: false);
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

String _nonEmpty(Object? value) {
  if (value is! String || value.isEmpty) throw const IpcProtocolException();
  return value;
}

bool _bool(Object? value) {
  if (value is! bool) throw const IpcProtocolException();
  return value;
}

String _storageProtection(Object? value) {
  final protection = _nonEmpty(value);
  if (!const {'protected', 'unprotected'}.contains(protection)) {
    throw const IpcProtocolException();
  }
  return protection;
}
