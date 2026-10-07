import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const networkResult =
    r'{"available":true,"revision":1,"interfaces":[{"interfaceId":"loopback","displayName":"","operationalState":"up","physicalMedium":"unknown","hardwareBacked":false,"physicalConnectorPresent":false,"filterInterface":false,"endpointInterface":false,"addressAssignmentMethod":"unknown","ipv4Assignments":[{"address":"127.0.0.1","prefixLength":8,"automaticCandidate":false,"explicitBindable":true}]}],"observedAt":"2026-10-07T01:02:03.123456789Z"}';
void main() {
  test('network facts are immutable and preserve full uint64 raw integers', () {
    final result = decodeNetworkInterfaces(networkResult);
    expect(result.available, isTrue);
    expect(result.observedAt, '2026-10-07T01:02:03.123456789Z');
    expect(
      result.interfaces.single.ipv4Assignments.single.automaticCandidate,
      isFalse,
    );
    expect(
      result.interfaces.single.ipv4Assignments.single.explicitBindable,
      isTrue,
    );
    expect(() => result.interfaces.clear(), throwsUnsupportedError);
    expect(
      () => result.interfaces.single.ipv4Assignments.clear(),
      throwsUnsupportedError,
    );
    for (final revision in ['9223372036854775808', '18446744073709551615']) {
      expect(
        decodeNetworkInterfaces(
          networkResult.replaceFirst('"revision":1', '"revision":$revision'),
        ).revision,
        BigInt.parse(revision),
      );
      expect(
        decodeNetworkInterfaces(
          networkResult.replaceFirst(
            '"revision":1',
            '"\\u0072evision":$revision',
          ),
        ).revision,
        BigInt.parse(revision),
      );
    }
    final unavailable = decodeNetworkInterfaces(
      '{"available":false,"revision":0,"interfaces":[]}',
    );
    expect(unavailable.available, isFalse);
    expect(unavailable.revision, BigInt.zero);
    expect(unavailable.observedAt, isNull);
  });
  test('network owned fields reject missing null types and secret extras', () {
    final root = jsonDecode(networkResult) as Map<String, dynamic>;
    final row = (root['interfaces'] as List).single as Map<String, dynamic>;
    final address =
        (row['ipv4Assignments'] as List).single as Map<String, dynamic>;
    for (final object in [root, row, address]) {
      for (final key in object.keys.toList()) {
        final original = object.remove(key);
        expect(
          () => decodeNetworkInterfaces(jsonEncode(root)),
          throwsA(isA<IpcProtocolException>()),
          reason: 'missing $key',
        );
        object[key] = null;
        expect(
          () => decodeNetworkInterfaces(jsonEncode(root)),
          throwsA(isA<IpcProtocolException>()),
          reason: 'null $key',
        );
        object[key] = <String, dynamic>{};
        expect(
          () => decodeNetworkInterfaces(jsonEncode(root)),
          throwsA(isA<IpcProtocolException>()),
          reason: 'type $key',
        );
        object[key] = original;
      }
      object['password'] = 'private';
      expect(
        () => decodeNetworkInterfaces(jsonEncode(root)),
        throwsA(isA<IpcProtocolException>()),
      );
      object.remove('password');
    }
  });
  test(
    'network rejects invalid enum address time revision and raw duplicates',
    () {
      for (final bad in [
        'null',
        '[]',
        '{}',
        '$networkResult {}',
        networkResult.replaceFirst('"available":true', '"available":false'),
        for (final revision in [
          '0',
          '-1',
          '1.0',
          '1e0',
          '"1"',
          '18446744073709551616',
        ])
          networkResult.replaceFirst('"revision":1', '"revision":$revision'),
        networkResult.replaceFirst('"prefixLength":8', '"prefixLength":33'),
        networkResult.replaceFirst('127.0.0.1', '127.00.0.1'),
        networkResult.replaceFirst('127.0.0.1', '::1'),
        networkResult.replaceFirst(
          '"operationalState":"up"',
          '"operationalState":"unknown"',
        ),
        networkResult.replaceFirst(
          '"physicalMedium":"unknown"',
          '"physicalMedium":"loopback"',
        ),
        networkResult.replaceFirst(
          '"addressAssignmentMethod":"unknown"',
          '"addressAssignmentMethod":"other"',
        ),
        networkResult.replaceFirst(
          '"available":true',
          '"available":true,"Available":true',
        ),
        networkResult.replaceFirst(
          '"revision":1',
          r'"revision":1,"\u0072evision":1',
        ),
        networkResult.replaceFirst(
          '"address":"127.0.0.1"',
          '"address":"127.0.0.1","address":"127.0.0.1"',
        ),
        for (final timestamp in [
          '2026-02-30T01:02:03Z',
          '0001-01-01T00:00:00Z',
          '2026-10-07T01:02:03+24:00',
          '2026-10-07T01:02:03.1234567891Z',
        ])
          networkResult.replaceFirst(
            '2026-10-07T01:02:03.123456789Z',
            timestamp,
          ),
      ]) {
        expect(
          () => decodeNetworkInterfaces(bad),
          throwsA(isA<IpcProtocolException>()),
          reason: bad,
        );
      }
    },
  );
  test('strict binding policy and raw escaped duplicate keys', () {
    final valid = NetworkBindingPolicy.explicit('Lo-ID', '127.0.0.1');
    expect(NetworkBindingPolicy.decode(valid.toJson()), valid);
    for (final raw in [
      null,
      {},
      {'mode': 'unknown'},
      {'mode': 'automatically_select_latest_available', 'interfaceId': null},
      {
        'mode': 'explicit_interface_and_local_ipv4',
        'interfaceId': 'lo',
        'localIpv4Address': '127.00.0.1',
      },
      {
        'mode': 'explicit_interface_and_local_ipv4',
        'interfaceId': 'lo',
        'localIpv4Address': '::ffff:127.0.0.1',
      },
      {
        'mode': 'explicit_interface_and_local_ipv4',
        'interfaceId': 'lo',
        'localIpv4Address': '0.0.0.0',
      },
      {
        'mode': 'explicit_interface_and_local_ipv4',
        'interfaceId': 'lo',
        'localIpv4Address': '224.0.0.1',
      },
      {
        'mode': 'explicit_interface_and_local_ipv4',
        'interfaceId': 'lo\n',
        'localIpv4Address': '127.0.0.1',
      },
    ]) {
      expect(
        () => NetworkBindingPolicy.decode(raw),
        throwsA(isA<IpcProtocolException>()),
      );
    }
    for (final source in [
      r'{"networkBindingPolicy":{"mode":"automatically_select_latest_available","\u006dode":"automatically_select_latest_available"}}',
      r'{"configurations":[{"networkBindingPolicy":{"mode":"explicit_interface_and_local_ipv4","interfaceId":"lo","\u0069nterfaceId":"lo","localIpv4Address":"127.0.0.1"}}]}',
    ]) {
      expect(
        () => decodeBindingCheckedJson(source),
        throwsA(isA<IpcProtocolException>()),
      );
    }
  });

  test('rejects duplicate decoded member names at every object depth', () {
    for (final source in [
      r'{"id":"request-1","id":"request-1"}',
      r'{"id":"request-1","\u0069d":"request-1"}',
      r'{"kind":"response","kind":"response"}',
      r'{"ok":true,"ok":true}',
      r'{"error":{"code":"conflict","code":"conflict","message":"safe"}}',
      r'{"error":{"code":"conflict","message":"safe","\u006dessage":"safe"}}',
      r'{"networkBindingPolicy":{"mode":"automatically_select_latest_available","mode":"automatically_select_latest_available"}}',
    ]) {
      expect(
        () => decodeBindingCheckedJson(source),
        throwsA(isA<IpcProtocolException>()),
        reason: source,
      );
    }

    const session =
        r'{"sessions":[{"sessionId":"s","sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","revision":1,"updatedAt":"2026-08-14T10:00:00Z"}]}';
    expect(() => decodeSessions(session), throwsA(isA<IpcProtocolException>()));

    const configuration =
        r'{"storageProtection":"protected","configurations":[{"configurationId":"cfg-a","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","username":"u","username":"u","credentialStored":true,"storageProtection":"protected","autoLogin":false,"autoReconnect":true,"networkBindingPolicy":{"mode":"automatically_select_latest_available"}}]}';
    expect(
      () => decodeConfigurations(configuration),
      throwsA(isA<IpcProtocolException>()),
    );
    expect(
      () => decodeBindingCheckedJson('{"value":NaN}'),
      throwsA(isA<IpcProtocolException>()),
    );
  });

  test(
    'keeps duplicate names local to each object and ignores string contents',
    () {
      final decoded = decodeBindingCheckedJson(
        r'{"left":{"id":"left"},"right":{"id":"right"},"text":"{\"id\":\"first\",\"id\":\"second\"}"}',
      ) as Map<String, dynamic>;
      expect((decoded['left'] as Map<String, dynamic>)['id'], 'left');
      expect((decoded['right'] as Map<String, dynamic>)['id'], 'right');
      expect(decoded['text'], r'{"id":"first","id":"second"}');
    },
  );

  test('decodes the committed daemon.stop success result strictly', () {
    final result = decodeDaemonStop('{"status":"stopping"}');
    expect(result.status, 'stopping');
  });

  test(
    'rejects wrong keys, status values and malformed daemon.stop results',
    () {
      for (final source in [
        '{"status":"stopped"}',
        '{"status":"stopping","extra":true}',
        '{"status":"stoppping"}',
        '{"status":""}',
        '{"status":null}',
        '{"status":1}',
        '{}',
        'null',
        '[]',
        '"stopping"',
      ]) {
        expect(
          () => decodeDaemonStop(source),
          throwsA(isA<IpcProtocolException>()),
          reason: source,
        );
      }
    },
  );

  test('decodes strict session.remove and configuration.remove results', () {
    final session = decodeSessionRemove(
      '{"sessionId":"s-a","status":"removed"}',
      expectedSessionId: 's-a',
    );
    expect(session.sessionId, 's-a');
    expect(session.status, 'removed');

    final configuration = decodeConfigurationRemove(
      '{"configurationId":"cfg-a","status":"removed"}',
      expectedConfigurationId: 'cfg-a',
    );
    expect(configuration.configurationId, 'cfg-a');
    expect(configuration.status, 'removed');
  });

  test('rejects malformed remove results and mismatched expected IDs', () {
    for (final source in [
      '{"sessionId":"s-a","status":"stopped"}',
      '{"sessionId":"s-a","status":"removed","extra":true}',
      '{"sessionId":"","status":"removed"}',
      '{"sessionId":null,"status":"removed"}',
      '{"sessionId":"s-b","status":"removed"}',
      '{"sessionId":"s-a"}',
      '{"status":"removed"}',
      '{}',
      'null',
      '[]',
    ]) {
      expect(
        () => decodeSessionRemove(source, expectedSessionId: 's-a'),
        throwsA(isA<IpcProtocolException>()),
        reason: source,
      );
    }
    for (final source in [
      '{"configurationId":"cfg-a","status":"stopped"}',
      '{"configurationId":"cfg-a","status":"removed","extra":true}',
      '{"configurationId":"","status":"removed"}',
      '{"configurationId":null,"status":"removed"}',
      '{"configurationId":"cfg-b","status":"removed"}',
      '{"configurationId":"cfg-a"}',
      '{"status":"removed"}',
      '{}',
      'null',
      '[]',
    ]) {
      expect(
        () =>
            decodeConfigurationRemove(source, expectedConfigurationId: 'cfg-a'),
        throwsA(isA<IpcProtocolException>()),
        reason: source,
      );
    }
  });
}
