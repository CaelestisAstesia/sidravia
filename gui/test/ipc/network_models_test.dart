import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const networkResult =
    r'{"available":true,"revision":1,"interfaces":[{"interfaceId":"loopback","displayName":"","operationalState":"up","physicalMedium":"unknown","hardwareBacked":false,"physicalConnectorPresent":false,"filterInterface":false,"endpointInterface":false,"addressAssignmentMethod":"unknown","ipv4Assignments":[{"address":"127.0.0.1","prefixLength":8,"automaticCandidate":false,"explicitBindable":true}]}],"observedAt":"2026-10-07T01:02:03.123456789Z"}';

void main() {
  test('network interfaces reject a raw negative-zero prefix length', () {
    expect(
      () => decodeNetworkInterfaces(
        networkResult.replaceFirst('"prefixLength":8', '"prefixLength":-0'),
      ),
      throwsA(isA<IpcProtocolException>()),
    );
  });

  test('optional adapter description preserves device facts and rejects wrong types', () {
    expect(
      decodeNetworkInterfaces(networkResult).interfaces.single.description,
      isEmpty,
    );
    final wire = networkResult.replaceFirst(
      '"displayName":""',
      '"displayName":"","description":"Intel Ethernet Adapter"',
    );
    expect(
      decodeNetworkInterfaces(wire).interfaces.single.description,
      'Intel Ethernet Adapter',
    );
    for (final bad in ['null', 'true', '3', '{}', '[]', r'"\ud800"']) {
      expect(
        () => decodeNetworkInterfaces(
          wire.replaceFirst('"Intel Ethernet Adapter"', bad),
        ),
        throwsA(isA<IpcProtocolException>()),
      );
    }
  });

  test('network rejects isolated surrogate originals consistently with Go', () {
    for (final field in ['interfaceId', 'displayName']) {
      final old = field == 'interfaceId'
          ? '"interfaceId":"loopback"'
          : '"displayName":""';
      for (final malformed in [
        r'\ud800',
        r'\udfff',
        r'\ud800\u0041',
        r'\udc00\ud800',
        r'\ud800 \udc00',
        r'\ud800\\udc00',
      ]) {
        final wire = networkResult.replaceFirst(
          old,
          '"$field":"private-marker$malformed"',
        );
        expect(
          () => decodeNetworkInterfaces(wire),
          throwsA(isA<IpcProtocolException>()),
        );
      }
    }
    for (final wire in [
      networkResult.replaceFirst('"displayName"', r'"private-key\ud800"'),
      networkResult.replaceFirst('"address"', r'"private-key\udfff"'),
    ]) {
      expect(
        () => decodeNetworkInterfaces(wire),
        throwsA(isA<IpcProtocolException>()),
      );
    }
  });

  test(
    'network keeps paired escapes literal Unicode and escaped backslashes',
    () {
      for (final spelling in <String, String>{
        r'"\ud83d\ude00"': '😀',
        r'"\uD83D\uDe00"': '😀',
        '"网卡"': '网卡',
        '"𐐷"': '𐐷',
        '"�"': '�',
        r'"\ufffd"': '�',
        r'"literal\\ud800"': r'literal\ud800',
        r'"quote\"then\\ud800"': r'quote"then\ud800',
        r'"path\\\\ud800"': r'path\\ud800',
      }.entries) {
        for (final field in ['interfaceId', 'displayName']) {
          final old = field == 'interfaceId'
              ? '"interfaceId":"loopback"'
              : '"displayName":""';
          final result = decodeNetworkInterfaces(
            networkResult.replaceFirst(old, '"$field":${spelling.key}'),
          );
          final row = result.interfaces.single;
          expect(
            field == 'interfaceId' ? row.interfaceId : row.displayName,
            spelling.value,
          );
        }
      }
      final result = decodeNetworkInterfaces(
        networkResult.replaceFirst('"displayName"', r'"\u0064isplayName"'),
      );
      expect(result.interfaces.single.displayName, isEmpty);
    },
  );

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
    const broadcast = '255.255.255.255';
    final binding = NetworkBindingPolicy.explicit('Lo-ID', broadcast);
    expect(
      NetworkBindingPolicy.decode(binding.toJson()).localIpv4Address,
      broadcast,
    );
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
}
