import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
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
