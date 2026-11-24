import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const _completeSessionWire =
    r'{"sessionId":"s","displayName":"","institutionProfileId":"i","institutionDisplayName":"Institution","authenticationProtocolId":"p","accountName":"a","intent":"suspend_authentication","state":"suspended","revision":1,"updatedAt":"2026-10-07T01:02:03Z","protocolSocket":{"state":"not_observed","runGeneration":0}}';

void main() {
  test('Session binding label may be empty but must be a present string', () {
    final wire = _completeSessionWire.replaceFirst(
      '"revision":1',
      '"selectedNetworkBinding":{"interfaceId":"i","displayName":"","localIpv4Address":"192.0.2.10"},"revision":1',
    );
    expect(decodeSession(wire).selectedNetworkBinding!.displayName, isEmpty);
    for (final replacement in [
      '"displayName":null',
      '"displayName":1',
      '"displayName":true',
      '"displayName":{}',
      '"displayName":[]',
      '"unknown":""',
    ]) {
      final bad = wire.replaceFirst(
        '"selectedNetworkBinding":{"interfaceId":"i","displayName":""',
        '"selectedNetworkBinding":{"interfaceId":"i",$replacement',
      );
      expect(
        () => decodeSession(bad),
        throwsA(isA<IpcProtocolException>()),
        reason: replacement,
      );
    }
    final missing = wire.replaceFirst(
      '"selectedNetworkBinding":{"interfaceId":"i","displayName":"",',
      '"selectedNetworkBinding":{"interfaceId":"i",',
    );
    expect(() => decodeSession(missing), throwsA(isA<IpcProtocolException>()));
  });

  test(
    'complete Session keeps original uint64 and immutable cleanup membership',
    () {
      for (final token in ['9007199254740993', '18446744073709551615']) {
        final wire = _completeSessionWire
            .replaceFirst('"revision":1', '"revision":$token')
            .replaceFirst(
              '"runGeneration":0',
              '"runGeneration":$token,"updatedAt":"2026-10-07T01:02:03Z"',
            );
        final direct = decodeSession(wire);
        for (final item in [
          direct,
          decodeSessionOperation('{"outcome":"resumed","session":$wire}'),
          decodeSessions('{"sessions":[$wire],"cleanupRequiredSessionIds":[]}')
              .single,
        ]) {
          expect(item.institutionProfileId, 'i');
          expect(item.institutionDisplayName, 'Institution');
          expect(item.authenticationProtocolId, 'p');
        }
        expect(direct.revision, BigInt.parse(token));
        expect(direct.protocolSocket.runGeneration, BigInt.parse(token));
        expect(direct.cleanupRequired, isFalse);
        final operation = decodeSessionOperation(
          '{"outcome":"resumed","session":$wire}',
        );
        expect(operation.revision, direct.revision);
        final list = decodeSessions(
          '{"sessions":[$wire],"cleanupRequiredSessionIds":["s"]}',
        );
        expect(list.single.cleanupRequired, isTrue);
        expect(
          list.single.protocolSocket.runGeneration,
          direct.protocolSocket.runGeneration,
        );
        expect(list.single.revision, direct.revision);
        expect(() => list.clear(), throwsUnsupportedError);
        expect(
          decodeSessions('{"sessions":[$wire],"cleanupRequiredSessionIds":[]}')
              .single
              .cleanupRequired,
          isFalse,
        );
      }
      const preview = SessionSummary(
        id: 'preview',
        displayName: '',
        accountName: 'a',
        state: SessionState.suspended,
        intent: SessionIntent.suspendAuthentication,
      );
      expect(preview.revision, BigInt.zero);
      expect(preview.protocolSocket.state, 'not_observed');
      expect(preview.protocolSocket.runGeneration, BigInt.zero);
    },
  );

  test('Session strict originals reject invalid revision, socket and list IDs', () {
    for (final token in [
      '-0',
      '-1',
      '0',
      '1.0',
      '1e0',
      '01',
      '18446744073709551616',
      'null',
      '"1"',
      '{}',
      '[]',
    ]) {
      expect(
        () => decodeSession(
          _completeSessionWire.replaceFirst(
            '"revision":1',
            '"revision":$token',
          ),
        ),
        throwsA(isA<IpcProtocolException>()),
        reason: token,
      );
    }
    for (final wire in [
      _completeSessionWire.replaceFirst('"protocolSocket":', '"unknown":'),
      _completeSessionWire.replaceFirst(
        '"revision":1',
        r'"revision":1,"\u0072evision":1',
      ),
      _completeSessionWire.replaceFirst(
        '"accountName":"a"',
        r'"accountName":"\ud800"',
      ),
      '$_completeSessionWire{}',
      _completeSessionWire.replaceFirst(
        '"runGeneration":0',
        '"runGeneration":-0',
      ),
      _completeSessionWire.replaceFirst(
        '"runGeneration":0',
        '"runGeneration":1',
      ),
      _completeSessionWire.replaceFirst(
        '"state":"not_observed"',
        '"state":"open"',
      ),
      _completeSessionWire.replaceFirst(
        '"updatedAt":"2026-10-07T01:02:03Z"',
        '"updatedAt":"0001-01-01T00:00:00Z"',
      ),
    ]) {
      expect(() => decodeSession(wire), throwsA(isA<IpcProtocolException>()));
    }
    for (final ids in ['null', '["missing"]', '[""]', '["s","s"]', '[1]']) {
      expect(
        () => decodeSessions(
          '{"sessions":[$_completeSessionWire],"cleanupRequiredSessionIds":$ids}',
        ),
        throwsA(isA<IpcProtocolException>()),
        reason: ids,
      );
    }
    expect(
      () => decodeSessions(
        '{"sessions":[$_completeSessionWire,$_completeSessionWire],"cleanupRequiredSessionIds":[]}',
      ),
      throwsA(isA<IpcProtocolException>()),
    );
    expect(
      () => decodeSessions('{"sessions":[$_completeSessionWire]}'),
      throwsA(isA<IpcProtocolException>()),
    );
    for (final field in ['authenticationEstablishedAt', 'nextRetryAt']) {
      for (final time in [
        '2026-10-07T01:02:03.1234567890Z',
        '0001-01-01T00:00:00Z',
        '2026-10-07T01:02:03+24:00',
      ]) {
        final wire = _completeSessionWire.replaceFirst(
          '"revision":1',
          '"$field":"$time","revision":1',
        );
        expect(
          () => decodeSession(wire),
          throwsA(isA<IpcProtocolException>()),
          reason: '$field $time',
        );
      }
    }
  });

  test('Session uses the same state-dependent socket endpoints and times', () {
    for (final state in [
      'open',
      'closed',
      'close_failed',
      'close_unconfirmed',
    ]) {
      final socket =
          '{"state":"$state","runGeneration":9007199254740993,"updatedAt":"2026-10-07T01:02:03Z","localEndpoint":{"address":"127.0.0.1","port":1},"remoteEndpoint":{"address":"255.255.255.255","port":61440}}';
      final wire = _completeSessionWire.replaceFirst(
        '{"state":"not_observed","runGeneration":0}',
        socket,
      );
      final value = decodeSession(wire);
      expect(value.protocolSocket.state, state);
      expect(value.protocolSocket.localEndpoint!.address, '127.0.0.1');
      expect(value.protocolSocket.remoteEndpoint!.port, 61440);
      for (final bad in [
        wire.replaceFirst('"port":1', '"port":0'),
        wire.replaceFirst(
          '"address":"127.0.0.1"',
          '"address":"255.255.255.255"',
        ),
        wire.replaceFirst(
          '"runGeneration":9007199254740993',
          '"runGeneration":0',
        ),
      ]) {
        expect(() => decodeSession(bad), throwsA(isA<IpcProtocolException>()));
      }
    }
  });
}
