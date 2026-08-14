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
}
