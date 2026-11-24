import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

void main() {
  test('export useful facts survive typed strict decoding', () {
    final value = decodeDiagnosticExport(
      r'{"schemaVersion":1,"generatedAt":"2026-10-07T01:02:03Z","productVersion":"v","buildId":"b","operatingSystem":"windows","architecture":"amd64","network":{"available":true,"interfaceCount":1,"ipv4AssignmentCount":2,"upInterfaceCount":1,"automaticCandidateCount":2,"explicitBindableCount":0},"catalog":{"storageProtection":"protected","totalConfigurations":2,"autoLoginConfigurations":0,"autoReconnectConfigurations":1,"automaticBindingConfigurations":2,"explicitBindingConfigurations":0,"availableConfigurations":1,"profileUnavailableConfigurations":0,"protocolUnavailableConfigurations":0,"overrideInvalidConfigurations":1},"sessions":{"totalCount":1,"truncated":false,"items":[{"state":"stopping","intent":"suspend_authentication","reasonCode":"protocol_run_failed","selectedBinding":true,"protocolSocketState":"close_unconfirmed","failureCategory":"network_io_failure","recoveryRecommendation":"retry_after_standard_delay","cleanupRequired":true}]}}',
    );
    expect(value.network.upInterfaceCount, 1);
    expect(value.network.automaticCandidateCount, 2);
    expect(value.network.explicitBindableCount, 0);
    expect(value.catalog.availableConfigurations, 1);
    expect(value.catalog.overrideInvalidConfigurations, 1);
    expect(value.sessions.items.single.failureCategory, 'network_io_failure');
    expect(
      value.sessions.items.single.recoveryRecommendation,
      'retry_after_standard_delay',
    );
    expect(value.sessions.items.single.cleanupRequired, isTrue);
    expect(() => value.sessions.items.clear(), throwsUnsupportedError);
  });

  test('export unsigned raw mode is opt in and typed items immutable', () {
    expect(decodeBindingCheckedJson('{"old":1e0}'), {'old': 1.0});
    for (final token in ['-0', '0.0', '0e0']) {
      expect(
        () => decodeBindingCheckedJson(
          '{"n":$token}',
          strictUnsignedNumbers: true,
        ),
        throwsA(isA<IpcProtocolException>()),
      );
    }
    expect(decodeBindingCheckedJson('{"n":0}', strictUnsignedNumbers: true), {
      'n': 0,
    });
    final items = <DiagnosticExportSession>[];
    final sessions = DiagnosticExportSessions(
      totalCount: 0,
      truncated: false,
      items: items,
    );
    items.add(
      const DiagnosticExportSession(
        state: 'other',
        intent: 'other',
        reasonCode: 'other',
        selectedBinding: false,
        protocolSocketState: 'other',
        failureCategory: 'other',
        recoveryRecommendation: 'other',
        cleanupRequired: false,
      ),
    );
    expect(sessions.items, isEmpty);
    expect(() => sessions.items.clear(), throwsUnsupportedError);
    expect(
      () => decodeDiagnosticExport(' ' * 32769),
      throwsA(isA<IpcProtocolException>()),
    );
  });
}
