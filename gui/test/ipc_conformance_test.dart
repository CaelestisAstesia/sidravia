import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  test('shared safe export strict grammar and invariants', () {
    final fixture = _fixture();
    final enums = fixture['enums'] as Map<String, dynamic>;
    expect(
      enums['configurationRuntimeAvailabilities'],
      ConfigurationRuntimeAvailability.values.map((v) => v.wireValue).toList(),
    );
    final expected = {
      'diagnosticOperatingSystems': diagnosticOperatingSystems,
      'diagnosticArchitectures': diagnosticArchitectures,
      'diagnosticSessionStates': diagnosticSessionStates,
      'diagnosticSessionIntents': diagnosticSessionIntents,
      'diagnosticReasonCodes': diagnosticReasonCodes,
      'diagnosticSocketStates': diagnosticSocketStates,
      'diagnosticFailureCategories': diagnosticFailureCategories,
      'diagnosticRecoveryRecommendations': diagnosticRecoveryRecommendations,
    };
    for (final entry in expected.entries) {
      expect((enums[entry.key] as List).toSet(), entry.value);
      expect(enums[entry.key], hasLength(entry.value.length));
    }
    final seen = <String>{};
    for (final raw in fixture['diagnosticExportCases'] as List) {
      final item = raw as Map<String, dynamic>;
      expect(item.keys.toSet(), {
        'name',
        'payload',
        'result',
        'validPayload',
        'validResult',
      });
      expect(seen.add(item['name'] as String), isTrue);
      if (item['validPayload'] == true) {
        decodeDiagnosticExportPayload(item['payload'] as String);
      } else {
        expect(
          () => decodeDiagnosticExportPayload(item['payload'] as String),
          throwsA(isA<IpcProtocolException>()),
          reason: item['name'] as String,
        );
      }
      if (item['validResult'] == true) {
        expect(
          decodeDiagnosticExport(item['result'] as String).schemaVersion,
          1,
        );
      } else {
        expect(
          () => decodeDiagnosticExport(item['result'] as String),
          throwsA(isA<IpcProtocolException>()),
          reason: item['name'] as String,
        );
      }
    }
  });

  test('shared diagnosis metadata, selector and strict result parity', () {
    final fixture = _fixture();
    expect(fixture.keys.toSet(), {
      'schemaVersion',
      'enums',
      'cases',
      'networkDiagnosticCases',
      'diagnosticExportCases',
    });
    final enums = fixture['enums'] as Map<String, dynamic>;
    final expected = {
      'networkSelectionBases': networkSelectionBases,
      'networkDiagnosticStatuses': networkDiagnosticStatuses,
      'networkUnsupportedReasons': networkUnsupportedReasons,
      'networkProbeStatuses': networkProbeStatuses,
      'networkSocketStates': networkSocketStates,
    };
    for (final entry in expected.entries) {
      expect((enums[entry.key] as List).toSet(), entry.value);
      expect(enums[entry.key], hasLength(entry.value.length));
    }
    final seen = <String>{};
    final cases = fixture['networkDiagnosticCases'] as List;
    expect(cases, isNotEmpty);
    for (final raw in cases) {
      final item = raw as Map<String, dynamic>;
      expect(item.keys.toSet(), {
        'name',
        'payload',
        'result',
        'validPayload',
        'validResult',
      });
      expect(item['name'], isA<String>());
      expect(item['name'], isNotEmpty);
      expect(seen.add(item['name'] as String), isTrue);
      expect(item['payload'], isA<String>());
      expect(item['result'], isA<String>());
      expect(item['validPayload'], isA<bool>());
      expect(item['validResult'], isA<bool>());
      final payload = item['payload'] as String;
      final result = item['result'] as String;
      if (item['validPayload'] == true) {
        expect(decodeNetworkDiagnosisPayload(payload), isNotEmpty);
      } else {
        expect(
          () => decodeNetworkDiagnosisPayload(payload),
          throwsA(isA<IpcProtocolException>()),
          reason: item['name'] as String,
        );
      }
      if (item['validResult'] == true) {
        expect(decodeNetworkDiagnosis(result).status, isNotEmpty);
      } else {
        expect(
          () => decodeNetworkDiagnosis(result),
          throwsA(isA<IpcProtocolException>()),
          reason: item['name'] as String,
        );
      }
    }
    final mainCase = (fixture['cases'] as List)
        .cast<Map<String, dynamic>>()
        .singleWhere((c) => c['method'] == 'network.diagnose');
    final request = decodeBindingCheckedJson(
      mainCase['request'] as String,
    ) as Map<String, dynamic>;
    expect(decodeNetworkDiagnosisPayload(jsonEncode(request['payload'])), {
      'configurationId': 'fixture-configuration',
      'probe': true,
    });
    final response = decodeBindingCheckedJson(
      mainCase['successResponse'] as String,
      preserveRunGeneration: true,
    ) as Map<String, dynamic>;
    expect(
      decodeNetworkDiagnosisValue(response['result'])
          .protocolSocket!
          .runGeneration,
      BigInt.parse('18446744073709551615'),
    );
  });

  test('fixture names exactly all twenty owned v1 methods', () {
    final cases = (_fixture()['cases'] as List).cast<Map<String, dynamic>>();
    expect(cases, hasLength(20));
    expect(cases.map((c) => c['method']).toSet(), {
      'daemon.status',
      'daemon.stop',
      'session.startOneShot',
      'session.stop',
      'session.ensureRunning',
      'session.restart',
      'session.remove',
      'session.get',
      'session.list',
      'profile.list',
      'configuration.list',
      'configuration.get',
      'configuration.create',
      'configuration.update',
      'configuration.setPassword',
      'configuration.remove',
      'session.startConfiguration',
      'network.interfaces',
      'network.diagnose',
      'diagnostics.export',
    });
  });
  test(
    'all legal Session states and intents decode; unknown or missing reject',
    () {
      final source = jsonDecode(
        '{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","protocolSocket":{"state":"not_observed","runGeneration":0},"revision":1,"updatedAt":"2026-08-14T10:00:00Z"}',
      ) as Map<String, dynamic>;
      for (final state in SessionState.values) {
        for (final intent in SessionIntent.values) {
          final result = decodeSession(
            jsonEncode({
              ...source,
              'state': state.wireValue,
              'intent': intent.wireValue,
            }),
          );
          expect(result.state, state);
          expect(result.intent, intent);
        }
      }
      for (final key in ['state', 'intent']) {
        for (final value in ['unknown', null, 1]) {
          expect(
            () => decodeSession(jsonEncode({...source, key: value})),
            throwsA(isA<IpcProtocolException>()),
          );
        }
        final missing = {...source}..remove(key);
        expect(
          () => decodeSession(jsonEncode(missing)),
          throwsA(isA<IpcProtocolException>()),
        );
      }
    },
  );

  test('consumes all five read-only IPC fixture examples', () {
    final fixture = jsonDecode(
      File('../internal/ipc/contract/testdata/v1/conformance.json')
          .readAsStringSync(),
    ) as Map<String, dynamic>;
    expect(fixture['schemaVersion'], 1);
    final cases = (fixture['cases'] as List<dynamic>)
        .cast<Map<String, dynamic>>()
        .where(
          (value) => const {
            'daemon.status',
            'profile.list',
            'configuration.list',
            'session.list',
            'network.interfaces',
          }.contains(value['method']),
        )
        .toList(growable: false);
    expect(cases.map((value) => value['method']).toSet(), hasLength(5));

    for (final value in cases) {
      final response = jsonDecode(
        value['successResponse'] as String,
      ) as Map<String, dynamic>;
      final result = jsonEncode(response['result']);
      switch (value['method']) {
        case 'daemon.status':
          expect(decodeDaemonStatus(result).mode, 'headless');
        case 'profile.list':
          expect(decodeProfiles(result), isEmpty);
        case 'configuration.list':
          expect(decodeConfigurations(result), isEmpty);
        case 'session.list':
          expect(decodeSessions(result), isEmpty);
        case 'network.interfaces':
          expect(
            decodeNetworkInterfaces(result)
                .interfaces
                .single
                .ipv4Assignments
                .single
                .explicitBindable,
            isTrue,
          );
      }
    }
  });

  test('fixture request and error envelopes remain read-only and exact', () {
    final fixture = _fixture();
    final cases = _readOnlyCases(fixture);
    for (final value in cases) {
      final request =
          jsonDecode(value['request'] as String) as Map<String, dynamic>;
      expect(request, {
        'kind': 'request',
        'id': request['id'],
        'method': value['method'],
        'payload': <String, dynamic>{},
      });
      final error =
          jsonDecode(value['errorResponse'] as String) as Map<String, dynamic>;
      expect(error.keys.toSet(), {'kind', 'id', 'ok', 'error'});
      expect(error['kind'], 'response');
      expect(error['id'], request['id']);
      expect(error['ok'], isFalse);
      expect((error['error'] as Map<String, dynamic>).keys.toSet(), {
        'code',
        'message',
      });
      expect(jsonEncode(error), isNot(contains('password')));
      expect(jsonEncode(error), isNot(contains('token')));
    }
  });

  test('read-only result decoders reject unknown and wrong-type data', () {
    expect(
      () => decodeDaemonStatus(
        '{"productVersion":"v","buildId":"b","pid":"1","status":"running","mode":"desktop"}',
      ),
      throwsA(isA<IpcProtocolException>()),
    );
    expect(
      () => decodeProfiles('{"profiles":[],"password":"never"}'),
      throwsA(isA<IpcProtocolException>()),
    );
  });

  test(
    'accepts desktop status and the fixture retained full-detail Session',
    () {
      final fixture = jsonDecode(
        File('../internal/ipc/contract/testdata/v1/conformance.json')
            .readAsStringSync(),
      ) as Map<String, dynamic>;
      final full = (fixture['cases'] as List<dynamic>)
          .cast<Map<String, dynamic>>()
          .firstWhere((value) => value['method'] == 'session.get');
      final response =
          jsonDecode(full['successResponse'] as String) as Map<String, dynamic>;
      final session = _decodeOneSession(jsonEncode(response['result']));
      expect(session.configurationId, isNotNull);
      expect(session.stateReason?.code, 'protocol_run_failed');
      expect(session.lastAuthenticationFailure?.code, 'credentials_rejected');
      expect(
        decodeDaemonStatus(
          '{"productVersion":"v","buildId":"b","pid":1,"status":"running","mode":"desktop","desktopOwnerPid":2}',
        ).desktopOwnerPid,
        2,
      );
    },
  );

  test(
    'accepts one-shot omission and rejects enum, optional, and field errors',
    () {
      final fixture = _fixture();
      final oneShot = (fixture['cases'] as List<dynamic>)
          .cast<Map<String, dynamic>>()
          .firstWhere((value) => value['method'] == 'session.startOneShot');
      final oneShotResponse = jsonDecode(
        oneShot['successResponse'] as String,
      ) as Map<String, dynamic>;
      final session =
          (oneShotResponse['result'] as Map<String, dynamic>)['session'];
      expect(_decodeOneSession(jsonEncode(session)).configurationId, isNull);
      expect(
        () => decodeSessions(
          '{"cleanupRequiredSessionIds":[],"sessions":[{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"wrong","state":"authenticated","protocolSocket":{"state":"not_observed","runGeneration":0},"revision":1,"updatedAt":"2026-08-14T10:00:00Z"}]}',
        ),
        throwsA(isA<IpcProtocolException>()),
      );
      expect(
        () => decodeSessions(
          '{"cleanupRequiredSessionIds":[],"sessions":[{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","protocolSocket":{"state":"not_observed","runGeneration":0},"revision":1,"updatedAt":"2026-08-14T10:00:00Z","password":"never"}]}',
        ),
        throwsA(isA<IpcProtocolException>()),
      );
    },
  );

  test('strictly validates RFC3339 calendar, clock, and offset components', () {
    expect(_sessionAt('2024-02-29T23:59:59.123456+23:59').updatedAt, isNotNull);
    expect(_sessionAt('2024-02-29T00:00:00Z').updatedAt, isNotNull);
    for (final timestamp in [
      '2023-02-29T00:00:00Z',
      '2024-02-30T00:00:00Z',
      '2024-04-31T00:00:00Z',
      '2024-01-01T24:00:00Z',
      '2024-01-01T00:60:00Z',
      '2024-01-01T00:00:60Z',
      '2024-01-01T00:00:00+24:00',
      '2024-01-01T00:00:00+23:60',
    ]) {
      expect(() => _sessionAt(timestamp), throwsA(isA<IpcProtocolException>()));
    }
  });

  test('decodes all nine operational fixture requests and success shapes', () {
    final cases = (_fixture()['cases'] as List<dynamic>)
        .cast<Map<String, dynamic>>()
        .where(
          (value) => const {
            'configuration.create',
            'configuration.update',
            'configuration.setPassword',
            'session.startConfiguration',
            'session.stop',
            'session.remove',
            'session.ensureRunning',
            'session.restart',
            'configuration.remove',
          }.contains(value['method']),
        )
        .toList(growable: false);
    expect(cases, hasLength(9));
    for (final value in cases) {
      final request =
          jsonDecode(value['request'] as String) as Map<String, dynamic>;
      expect(request['kind'], 'request');
      expect(request['method'], value['method']);
      final result = (jsonDecode(
        value['successResponse'] as String,
      ) as Map<String, dynamic>)['result'];
      switch (value['method']) {
        case 'configuration.create':
        case 'configuration.update':
        case 'configuration.setPassword':
          expect(
            decodeConfiguration(jsonEncode(result)).credentialStored,
            isTrue,
          );
        case 'session.startConfiguration':
        case 'session.ensureRunning':
          expect(decodeSessionOperation(jsonEncode(result)).id, isNotEmpty);
        case 'session.stop':
        case 'session.restart':
          expect(decodeSession(jsonEncode(result)).id, isNotEmpty);
        case 'session.remove':
          expect(
            decodeSessionRemove(
              jsonEncode(result),
              expectedSessionId: 'session-retained',
            ).status,
            'removed',
          );
        case 'configuration.remove':
          expect(
            decodeConfigurationRemove(
              jsonEncode(result),
              expectedConfigurationId: 'fixture-configuration',
            ).status,
            'removed',
          );
      }
    }
  });

  test(
    'empty configuration and session display names use caller fallbacks',
    () {
      final configuration = decodeConfiguration(
        '{"configurationId":"cfg-a","displayName":"","runtimeAvailability":"available","institutionProfileId":"jlu","institutionDisplayName":"JLU","authenticationProtocolId":"d","username":"account","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"credentialStored":true,"storageProtection":"protected","autoLogin":false,"autoReconnect":true}',
      );
      final session = _sessionAt('2026-08-14T10:00:00Z');
      expect(configuration.displayName, isEmpty);
      expect(configuration.username, 'account');
      expect(session.accountName, 'a');
    },
  );
}

Map<String, dynamic> _fixture() => jsonDecode(
  File('../internal/ipc/contract/testdata/v1/conformance.json')
      .readAsStringSync(),
) as Map<String, dynamic>;

List<Map<String, dynamic>> _readOnlyCases(Map<String, dynamic> fixture) =>
    (fixture['cases'] as List<dynamic>)
        .cast<Map<String, dynamic>>()
        .where(
          (value) => const {
            'daemon.status',
            'profile.list',
            'configuration.list',
            'session.list',
            'network.interfaces',
          }.contains(value['method']),
        )
        .toList(growable: false);

SessionSummary _decodeOneSession(String result) {
  final object = jsonDecode(result) as Map<String, dynamic>;
  return decodeSessions(
    jsonEncode({
      'sessions': [object],
      'cleanupRequiredSessionIds': <String>[],
    }),
  ).single;
}

SessionSummary _sessionAt(String timestamp) => _decodeOneSession(
  '{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","protocolSocket":{"state":"not_observed","runGeneration":0},"revision":1,"updatedAt":"$timestamp"}',
);
