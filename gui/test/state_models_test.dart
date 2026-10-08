import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/state_models.dart';

void main() {
  test(
    'full state preserves original metadata, cleanup, socket and uint64',
    () {
      final fixture = jsonDecode(
        File('../internal/ipc/contract/testdata/v1/conformance.json')
            .readAsStringSync(),
      ) as Map<String, dynamic>;
      final cases = (fixture['stateCases'] as List)
          .cast<Map<String, dynamic>>();
      final bootstrap = decodeStateBootstrap(
        cases.singleWhere((v) => v['name'] == 'full original bootstrap')['data']
            as String,
      );
      final changed = decodeStateEvent(
        cases.singleWhere((v) => v['name'] == 'changed')['data'] as String,
      ) as SessionChanged;
      for (final session in [bootstrap.sessions.single, changed.session]) {
        expect(session.institutionProfileId, 'jlu');
        expect(session.institutionDisplayName, isNotEmpty);
        expect(session.authenticationProtocolId, isNotEmpty);
        expect(session.cleanupRequired, isTrue);
        expect(session.revision, BigInt.parse('18446744073709551615'));
        expect(session.protocolSocket.runGeneration, session.revision);
        expect(session.configurationId, isNotNull);
        expect(session.selectedNetworkBinding, isNotNull);
        expect(session.lastAuthenticationFailure, isNotNull);
      }
      expect(() => bootstrap.sessions.clear(), throwsUnsupportedError);
      expect(bootstrap.network.revision, BigInt.parse('18446744073709551615'));
      final empty =
          cases.singleWhere(
                (v) => v['name'] == 'empty unavailable bootstrap',
              )['data']
              as String;
      expect(decodeStateBootstrap(empty).network.revision, BigInt.zero);
      expect(
        decodeStateBootstrap(empty.padRight(stateFrameLimit)).sessions,
        isEmpty,
      );
      expect(
        () => decodeStateBootstrap(empty.padRight(stateFrameLimit + 1)),
        throwsA(isA<IpcProtocolException>()),
      );
      expect(
        () => decodeStateEvent(
          (cases.singleWhere((v) => v['name'] == 'removed')['data'] as String)
              .padRight(stateFrameLimit + 1),
        ),
        throwsA(isA<IpcProtocolException>()),
      );
    },
  );
  test('bootstrap resource capacity counts the unavailable network', () {
    final sessions = List.generate(
      256,
      (i) => {
        'sessionId': 's$i',
        'displayName': '',
        'institutionProfileId': 'i',
        'institutionDisplayName': 'n',
        'authenticationProtocolId': 'p',
        'accountName': 'a',
        'intent': 'suspend_authentication',
        'state': 'suspended',
        'revision': BigInt.one,
        'protocolSocket': {
          'state': 'not_observed',
          'runGeneration': BigInt.zero,
        },
        'updatedAt': '2026-10-07T01:02:03Z',
      },
    );
    Object value(int count) => {
      'sessions': {
        'sessions': sessions.take(count).toList(),
        'cleanupRequiredSessionIds': <String>[],
      },
      'network': {
        'available': false,
        'revision': BigInt.zero,
        'interfaces': <Object>[],
      },
    };
    expect(decodeStateBootstrapValue(value(255)).sessions, hasLength(255));
    expect(
      () => decodeStateBootstrapValue(value(256)),
      throwsA(isA<IpcProtocolException>()),
    );
  });
}
