import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/advanced/runtime_snapshot.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

import 'support/runtime_snapshot.dart';

class SecretBootstrap implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:49234/ipc'),
      token: 'private-ipc-bearer-sentinel',
      productVersion: 'test',
      buildId: 'test',
      daemonPid: 7,
      mode: 'desktop',
    ),
  );
}

class SnapshotClient extends OfflineDemoClient {
  @override
  Future<List<SessionSummary>> sessionList() async => [
    SessionSummary(
      id: 'session-original',
      displayName: '原始名称',
      accountName: 'student01@example.edu.cn',
      state: SessionState.authenticated,
      intent: SessionIntent.maintainAuthentication,
      revision: BigInt.parse('18446744073709551615'),
      protocolSocket: NetworkProtocolSocket(
        state: 'open',
        runGeneration: BigInt.parse('9007199254740993'),
        localEndpoint: const NetworkEndpoint('198.51.100.42', 61440),
        remoteEndpoint: const NetworkEndpoint('203.0.113.77', 61440),
      ),
      selectedNetworkBinding: const SessionNetworkBinding(
        interfaceId: '{adapter-raw}',
        displayName: '以太网 · 原名',
        localIpv4Address: '198.51.100.42',
      ),
    ),
  ];
}

void main() {
  testWidgets('bundled snapshot font has equal ASCII advances', (tester) async {
    await tester.runAsync(() async {
      final loader = FontLoader('Source Code Pro')
        ..addFont(rootBundle.load('assets/fonts/SourceCodePro-Regular.otf'));
      await loader.load();
    });
    double width(String value) {
      final painter = TextPainter(
        text: TextSpan(
          text: value,
          style: const TextStyle(fontFamily: 'Source Code Pro', fontSize: 12),
        ),
        textDirection: TextDirection.ltr,
      )..layout();
      final valueWidth = painter.width;
      painter.dispose();
      return valueWidth;
    }

    expect(width('iiiiiiii'), closeTo(width('WWWWWWWW'), .001));
    expect(width('01234567'), closeTo(width('abcdefgh'), .001));
  });

  test('snapshot excludes bootstrap secrets and preserves public identities and uint64', () async {
    final client = SnapshotClient();
    final controller = GuiController(
      bootstrapper: SecretBootstrap(),
      connector: (_) async => client,
    );
    await controller.start();
    await client.configurationSetPassword(
      configurationId: 'demo-config',
      password: 'private-password-sentinel',
    );
    final text = captureRuntimeSnapshot(
      controller,
      capturedAt: DateTime.utc(2026, 11, 15),
    );
    final value = jsonDecode(text) as Map<String, dynamic>;
    expect(text, isNot(contains('private-ipc-bearer-sentinel')));
    expect(text, isNot(contains('private-password-sentinel')));
    expect(text, isNot(contains('"password"')));
    expect(text, isNot(contains('"token"')));
    expect(value['sessions'][0]['accountName'], 'student01@example.edu.cn');
    expect(
      value['sessions'][0]['selectedNetworkBinding']['interfaceId'],
      '{adapter-raw}',
    );
    expect(
      value['sessions'][0]['selectedNetworkBinding']['displayName'],
      '以太网 · 原名',
    );
    expect(
      value['sessions'][0]['protocolSocket']['remoteEndpoint']['address'],
      '203.0.113.77',
    );
    expect(value['sessions'][0]['revision'], '18446744073709551615');
    expect(
      value['sessions'][0]['protocolSocket']['runGeneration'],
      '9007199254740993',
    );
    expect(value['configurations'][0]['username'], 'student01');
    expect(
      value['network']['interfaces'][0]['ipv4Assignments'][0]['address'],
      '192.0.2.4',
    );
    expect(value['capturedAt'], '2026-11-15T00:00:00.000Z');
    await controller.close();
    controller.dispose();
    client.dispose();
  });

  testWidgets(
    'capture stays fixed while backend changes and copy matches display before and after refresh',
    (tester) async {
      String? copied;
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          if (call.method == 'Clipboard.setData') {
            copied = (call.arguments as Map)['text'] as String;
          }
          return null;
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: SecretBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AdvancedPage(controller: controller, onBack: () {}),
          ),
        ),
      );
      await tester.pumpAndSettle();
      String shown() => tester
          .widget<SelectableText>(
            find.byKey(const ValueKey('runtime-snapshot-text')),
          )
          .data!;
      final initial = shown();
      client.selectScenario('suspended');
      await controller.retry();
      await tester.pumpAndSettle();
      expect(shown(), initial);
      await tester.tap(find.byTooltip('复制快照'));
      await tester.pumpAndSettle();
      expect(copied, initial);
      await tester.tap(find.byTooltip('刷新快照'));
      await tester.pump();
      expect(displayedSnapshot(tester)['sessions'][0]['state'], 'suspended');
      expect(shown(), isNot(initial));
      await tester.tap(find.byTooltip('复制快照'));
      await tester.pumpAndSettle();
      expect(copied, shown());
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
      client.dispose();
    },
  );
}
