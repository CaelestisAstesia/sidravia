import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  test('consumes all four read-only IPC fixture examples', () {
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
          }.contains(value['method']),
        )
        .toList(growable: false);
    expect(cases.map((value) => value['method']).toSet(), hasLength(4));

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
          '{"sessions":[{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"wrong","state":"authenticated","revision":1,"updatedAt":"2026-08-14T10:00:00Z"}]}',
        ),
        throwsA(isA<IpcProtocolException>()),
      );
      expect(
        () => decodeSessions(
          '{"sessions":[{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","revision":1,"updatedAt":"2026-08-14T10:00:00Z","password":"never"}]}',
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

  test('decodes all seven operational fixture requests and success shapes', () {
    final cases = (_fixture()['cases'] as List<dynamic>)
        .cast<Map<String, dynamic>>()
        .where(
          (value) => const {
            'configuration.create',
            'configuration.update',
            'configuration.setPassword',
            'session.startConfiguration',
            'session.stop',
            'session.ensureRunning',
            'session.restart',
          }.contains(value['method']),
        )
        .toList(growable: false);
    expect(cases, hasLength(7));
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
      }
    }
  });

  test(
    'empty configuration and session display names use caller fallbacks',
    () {
      final configuration = decodeConfiguration(
        '{"configurationId":"cfg-a","displayName":"","institutionProfileId":"jlu","institutionDisplayName":"JLU","authenticationProtocolId":"d","username":"account","credentialStored":true,"storageProtection":"protected","autoLogin":false,"autoReconnect":true}',
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
          }.contains(value['method']),
        )
        .toList(growable: false);

SessionSummary _decodeOneSession(String result) {
  final object = jsonDecode(result) as Map<String, dynamic>;
  return decodeSessions(
    jsonEncode({
      'sessions': [object],
    }),
  ).single;
}

SessionSummary _sessionAt(String timestamp) => _decodeOneSession(
  '{"sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","revision":1,"updatedAt":"$timestamp"}',
);
