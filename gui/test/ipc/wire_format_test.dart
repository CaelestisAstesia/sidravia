import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  test('checked tree preserves simultaneous nested uint64 without precision laundering', () {
    const wire =
        '{"revision":9007199254740993,"nested":{"runGeneration":18446744073709551615},"other":[{"runGeneration":9007199254740995}]}';
    final tree = decodeBindingCheckedJson(
      wire,
      preserveUnsignedRevision: true,
      preserveRunGeneration: true,
    ) as Map<String, dynamic>;
    expect(tree['revision'], BigInt.parse('9007199254740993'));
    expect(
      (tree['nested'] as Map)['runGeneration'],
      BigInt.parse('18446744073709551615'),
    );
    expect(
      ((tree['other'] as List).single as Map)['runGeneration'],
      BigInt.parse('9007199254740995'),
    );
    for (final token in [
      '1.0',
      '1e0',
      '-1',
      '-0',
      '18446744073709551616',
      'null',
    ]) {
      expect(
        () => decodeBindingCheckedJson(
          '{"nested":{"runGeneration":$token}}',
          preserveRunGeneration: true,
        ),
        throwsA(isA<IpcProtocolException>()),
      );
    }
    // Legacy method numeric semantics remain jsonDecode's, with no BigInt opt-in.
    expect((decodeBindingCheckedJson('{"revision":1}') as Map)['revision'], 1);
    expect((decodeBindingCheckedJson('{"value":-0}') as Map)['value'], 0);
    expect(
      decodeBindingCheckedJson('{"value":"-0"}', preserveRunGeneration: true),
      {'value': '-0'},
    );
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
        r'{"cleanupRequiredSessionIds":[],"sessions":[{"sessionId":"s","sessionId":"s","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","accountName":"a","intent":"maintain_authentication","state":"authenticated","protocolSocket":{"state":"not_observed","runGeneration":0},"revision":1,"updatedAt":"2026-08-14T10:00:00Z"}]}';
    expect(() => decodeSessions(session), throwsA(isA<IpcProtocolException>()));

    const configuration =
        r'{"storageProtection":"protected","configurations":[{"configurationId":"cfg-a","displayName":"d","institutionProfileId":"i","institutionDisplayName":"n","authenticationProtocolId":"p","username":"u","username":"u","credentialStored":true,"runtimeAvailability":"available","storageProtection":"protected","autoLogin":false,"autoReconnect":true,"networkBindingPolicy":{"mode":"automatically_select_latest_available"}}]}';
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
}
