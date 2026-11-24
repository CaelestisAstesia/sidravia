import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
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
