part of 'ipc_models.dart';

const maximumDiagnosticExportBytes = 32768;
const diagnosticOperatingSystems = {'windows', 'linux', 'darwin', 'other'};
const diagnosticArchitectures = {'amd64', 'arm64', '386', 'other'};
const diagnosticSessionStates = {
  'suspended',
  'waiting_for_network',
  'authenticating',
  'authenticated',
  'waiting_before_retry',
  'blocked_by_error',
  'stopping',
  'other',
};
const diagnosticSessionIntents = {
  'maintain_authentication',
  'suspend_authentication',
  'other',
};
const diagnosticReasonCodes = {
  'network_binding_unavailable',
  'network_unavailable',
  'runtime_definition_unavailable',
  'protocol_run_creation_failed',
  'protocol_run_failed',
  'protocol_contract_violated',
  'automatic_reconnect_disabled',
  'protocol_recovery_exhausted',
  'none',
  'other',
};
const diagnosticSocketStates = {
  'not_observed',
  'open',
  'closed',
  'close_failed',
  'close_unconfirmed',
  'other',
};

const diagnosticFailureCategories = {
  'none',
  'network_timeout',
  'network_io_failure',
  'server_busy',
  'authentication_rejected',
  'binding_rejected',
  'protocol_incompatible',
  'protocol_response_invalid',
  'protocol_contract_violated',
  'logout_cleanup_failed',
  'run_creation_failed',
  'runtime_definition_unavailable',
  'other',
};
const diagnosticRecoveryRecommendations = {
  'none',
  'retry_after_standard_delay',
  'retry_after_extended_delay',
  'retry_after_bounded_recovery',
  'block_until_explicit_restart_or_relevant_input_change',
  'other',
};

class DiagnosticExportNetwork {
  const DiagnosticExportNetwork({
    required this.available,
    this.interfaceCount,
    this.ipv4AssignmentCount,
    this.upInterfaceCount,
    this.automaticCandidateCount,
    this.explicitBindableCount,
  });
  final bool available;
  final int? interfaceCount,
      ipv4AssignmentCount,
      upInterfaceCount,
      automaticCandidateCount,
      explicitBindableCount;
}

class DiagnosticExportCatalog {
  const DiagnosticExportCatalog({
    required this.storageProtection,
    required this.totalConfigurations,
    required this.autoLoginConfigurations,
    required this.autoReconnectConfigurations,
    required this.automaticBindingConfigurations,
    required this.explicitBindingConfigurations,
    required this.availableConfigurations,
    required this.profileUnavailableConfigurations,
    required this.protocolUnavailableConfigurations,
    required this.overrideInvalidConfigurations,
  });
  final String storageProtection;
  final int totalConfigurations,
      autoLoginConfigurations,
      autoReconnectConfigurations,
      automaticBindingConfigurations,
      explicitBindingConfigurations,
      availableConfigurations,
      profileUnavailableConfigurations,
      protocolUnavailableConfigurations,
      overrideInvalidConfigurations;
}

class DiagnosticExportSession {
  const DiagnosticExportSession({
    required this.state,
    required this.intent,
    required this.reasonCode,
    required this.selectedBinding,
    required this.protocolSocketState,
    required this.failureCategory,
    required this.recoveryRecommendation,
    required this.cleanupRequired,
  });
  final String state,
      intent,
      reasonCode,
      protocolSocketState,
      failureCategory,
      recoveryRecommendation;
  final bool selectedBinding, cleanupRequired;
}

class DiagnosticExportSessions {
  DiagnosticExportSessions({
    required this.totalCount,
    required this.truncated,
    required List<DiagnosticExportSession> items,
  }) : items = List.unmodifiable(items);
  final int totalCount;
  final bool truncated;
  final List<DiagnosticExportSession> items;
}

class DiagnosticExport {
  const DiagnosticExport({
    required this.schemaVersion,
    required this.generatedAt,
    required this.productVersion,
    required this.buildId,
    required this.operatingSystem,
    required this.architecture,
    required this.network,
    required this.catalog,
    required this.sessions,
  });
  final int schemaVersion;
  final String generatedAt,
      productVersion,
      buildId,
      operatingSystem,
      architecture;
  final DiagnosticExportNetwork network;
  final DiagnosticExportCatalog catalog;
  final DiagnosticExportSessions sessions;
}

String _diagnosticMetadata(Object? raw) {
  final text = _text(raw);
  final match = RegExp(r'^[A-Za-z0-9._+-]{1,128}$').firstMatch(text);
  if (match == null || match.end != text.length) {
    throw const IpcProtocolException();
  }
  return text;
}

DiagnosticExport decodeDiagnosticExport(String source) {
  if (utf8.encode(source).length > maximumDiagnosticExportBytes) {
    throw const IpcProtocolException();
  }
  return decodeDiagnosticExportValue(
    decodeBindingCheckedJson(source, strictUnsignedNumbers: true),
  );
}

void decodeDiagnosticExportPayload(String source) {
  _object(
    decodeBindingCheckedJson(source, strictUnsignedNumbers: true),
    const {},
  );
}

DiagnosticExport decodeDiagnosticExportValue(Object? raw) {
  final v = _object(raw, const {
    'schemaVersion',
    'generatedAt',
    'productVersion',
    'buildId',
    'operatingSystem',
    'architecture',
    'network',
    'catalog',
    'sessions',
  });
  final schema = _networkUint(v['schemaVersion'], 4294967295);
  if (schema != 1) {
    throw const IpcProtocolException();
  }
  final generated = _networkTime(v['generatedAt']);
  final version = _diagnosticMetadata(v['productVersion']);
  final build = _diagnosticMetadata(v['buildId']);
  final os = _networkEnum(v['operatingSystem'], diagnosticOperatingSystems);
  final arch = _networkEnum(v['architecture'], diagnosticArchitectures);
  if (v['network'] is! Map<String, dynamic>) {
    throw const IpcProtocolException();
  }
  final available = _bool((v['network'] as Map<String, dynamic>)['available']);
  final n = _object(v['network'], {
    'available',
    if (available) 'interfaceCount',
    if (available) 'ipv4AssignmentCount',
    if (available) 'upInterfaceCount',
    if (available) 'automaticCandidateCount',
    if (available) 'explicitBindableCount',
  });
  final network = DiagnosticExportNetwork(
    upInterfaceCount: available
        ? _networkUint(n['upInterfaceCount'], 4294967295)
        : null,
    automaticCandidateCount: available
        ? _networkUint(n['automaticCandidateCount'], 4294967295)
        : null,
    explicitBindableCount: available
        ? _networkUint(n['explicitBindableCount'], 4294967295)
        : null,
    available: available,
    interfaceCount: available
        ? _networkUint(n['interfaceCount'], 4294967295)
        : null,
    ipv4AssignmentCount: available
        ? _networkUint(n['ipv4AssignmentCount'], 4294967295)
        : null,
  );
  if (available &&
      (network.upInterfaceCount! > network.interfaceCount! ||
          network.automaticCandidateCount! > network.ipv4AssignmentCount! ||
          network.explicitBindableCount! > network.ipv4AssignmentCount!)) {
    throw const IpcProtocolException();
  }
  final c = _object(v['catalog'], const {
    'storageProtection',
    'totalConfigurations',
    'autoLoginConfigurations',
    'autoReconnectConfigurations',
    'automaticBindingConfigurations',
    'explicitBindingConfigurations',
    'availableConfigurations',
    'profileUnavailableConfigurations',
    'protocolUnavailableConfigurations',
    'overrideInvalidConfigurations',
  });
  final total = _networkUint(c['totalConfigurations'], 4294967295);
  final login = _networkUint(c['autoLoginConfigurations'], 4294967295);
  final reconnect = _networkUint(c['autoReconnectConfigurations'], 4294967295);
  final automatic = _networkUint(
    c['automaticBindingConfigurations'],
    4294967295,
  );
  final explicit = _networkUint(c['explicitBindingConfigurations'], 4294967295);
  if (login > 1 ||
      login > total ||
      reconnect > total ||
      automatic + explicit != total) {
    throw const IpcProtocolException();
  }
  final availableConfigurations = _networkUint(
    c['availableConfigurations'],
    4294967295,
  );
  final profileUnavailable = _networkUint(
    c['profileUnavailableConfigurations'],
    4294967295,
  );
  final protocolUnavailable = _networkUint(
    c['protocolUnavailableConfigurations'],
    4294967295,
  );
  final overrideInvalid = _networkUint(
    c['overrideInvalidConfigurations'],
    4294967295,
  );
  if (availableConfigurations +
          profileUnavailable +
          protocolUnavailable +
          overrideInvalid !=
      total) {
    throw const IpcProtocolException();
  }
  final catalog = DiagnosticExportCatalog(
    availableConfigurations: availableConfigurations,
    profileUnavailableConfigurations: profileUnavailable,
    protocolUnavailableConfigurations: protocolUnavailable,
    overrideInvalidConfigurations: overrideInvalid,
    storageProtection: _protection(c['storageProtection']),
    totalConfigurations: total,
    autoLoginConfigurations: login,
    autoReconnectConfigurations: reconnect,
    automaticBindingConfigurations: automatic,
    explicitBindingConfigurations: explicit,
  );
  final s = _object(v['sessions'], const {'totalCount', 'truncated', 'items'});
  final sessionTotal = _networkUint(s['totalCount'], 4294967295);
  final truncated = _bool(s['truncated']);
  final items = _list(s['items']).map((raw) {
    final item = _object(raw, const {
      'state',
      'intent',
      'reasonCode',
      'selectedBinding',
      'protocolSocketState',
      'failureCategory',
      'recoveryRecommendation',
      'cleanupRequired',
    });
    return DiagnosticExportSession(
      failureCategory: _networkEnum(
        item['failureCategory'],
        diagnosticFailureCategories,
      ),
      recoveryRecommendation: _networkEnum(
        item['recoveryRecommendation'],
        diagnosticRecoveryRecommendations,
      ),
      cleanupRequired: _bool(item['cleanupRequired']),
      state: _networkEnum(item['state'], diagnosticSessionStates),
      intent: _networkEnum(item['intent'], diagnosticSessionIntents),
      reasonCode: _networkEnum(item['reasonCode'], diagnosticReasonCodes),
      selectedBinding: _bool(item['selectedBinding']),
      protocolSocketState: _networkEnum(
        item['protocolSocketState'],
        diagnosticSocketStates,
      ),
    );
  }).toList();
  if (items.length != (sessionTotal > 64 ? 64 : sessionTotal) ||
      truncated != (sessionTotal > 64)) {
    throw const IpcProtocolException();
  }
  return DiagnosticExport(
    schemaVersion: schema,
    generatedAt: generated,
    productVersion: version,
    buildId: build,
    operatingSystem: os,
    architecture: arch,
    network: network,
    catalog: catalog,
    sessions: DiagnosticExportSessions(
      totalCount: sessionTotal,
      truncated: truncated,
      items: items,
    ),
  );
}
