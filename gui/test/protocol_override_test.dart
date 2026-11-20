import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/configuration/override_editor.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/state_models.dart';
import 'package:sidravia_gui/ipc/state_subscription.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

const original = '{"schemaVersion":1,"hostName":"sample"}';
const changed = '{"schemaVersion":1,"hostName":"new"}';

class _OverrideClient extends OfflineDemoClient
    implements SidraviaOverrideClient {
  String? document = original;
  Map<String, Object?>? submission;
  String? startVersion;
  String _version = 'initial';
  @override
  Future<List<SessionSummary>> sessionList() async => [];
  @override
  Future<List<InstitutionProfile>> profileList() async => const [
    InstitutionProfile(
      id: 'jlu',
      displayName: '吉林大学',
      protocolId: 'd520',
      overrideSchemaVersion: 1,
    ),
  ];
  ConfigurationSummary _summary() => ConfigurationSummary(
    id: 'demo-config',
    displayName: '',
    institutionProfileId: 'jlu',
    institutionDisplayName: '吉林大学',
    authenticationProtocolId: 'd520',
    username: 'student01',
    credentialStored: true,
    storageProtection: 'protected',
    version: _version,
    overrideState: document == null ? 'none' : 'enabled',
    overrideSchemaVersion: 1,
  );
  @override
  Future<List<ConfigurationSummary>> configurationList() async => [_summary()];
  @override
  Future<ConfigurationOverride> configurationOverride(
    String id, {
    String? expectedDocument,
  }) async => (
    configuration: _summary(),
    document: document,
    matchesDocument: expectedDocument == null
        ? null
        : document == expectedDocument,
  );
  @override
  Future<OverrideValidation> validateOverride(
    String profile,
    String value,
  ) async =>
      (valid: value == changed, field: value == changed ? null : 'hostName');
  @override
  Future<ConfigurationSummary> saveConfigurationEdit(
    Map<String, Object?> fields, {
    required bool creating,
    required bool allowInsecureStorage,
  }) async {
    submission = fields;
    document = fields['overrideDocument'] as String?;
    _version = 'saved';
    return _summary();
  }

  @override
  Future<SessionSummary> startConfigurationVersion(
    String id,
    String version,
  ) async {
    startVersion = version;
    return super.sessionStartConfiguration(id);
  }
}

class _LiveOverrideClient extends _OverrideClient
    implements SidraviaStateClient {
  @override
  Future<StateSubscription> subscribeStateEvents() async => _QuietSubscription(
    StateBootstrap(sessions: [], network: await networkInterfaces()),
  );
}

class _QuietSubscription implements StateSubscription {
  _QuietSubscription(this.initial);
  @override
  final StateBootstrap initial;
  final _events = StreamController<StateEvent>.broadcast(sync: true);
  @override
  Stream<StateEvent> get events => _events.stream;
  @override
  Future<void> close() async {
    final closing = _events.close();
    if (_events.hasListener) await closing;
  }
}

void main() {
  test('formatter preserves field order, escapes and numeric spelling and rejects decoded duplicate names', () {
    expect(
      formatOverride(r'{"z":[1e20,"\u4e2d"],"n":-0}'),
      '{\n  "z": [\n    1e20,\n    "\\u4e2d"\n  ],\n  "n": -0\n}',
    );
    expect(() => formatOverride(r'{"x":1,"\u0078":2}'), throwsFormatException);
    expect(overrideSyntaxError('{"x":\n!}'), contains('第 2 行'));
  });

  test('formatter refuses output that would exceed the document limit', () {
    final input = '{"x":${'[' * 150}0${']' * 150}}';
    expect(() => formatOverride(input), throwsFormatException);
  });

  test('saved configuration refreshes without a session event', () async {
    final client = _LiveOverrideClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
    );
    try {
      await controller.start();
      expect(controller.capabilities.configuration?.overrideState, 'enabled');
      final updated = Completer<void>();
      controller.addListener(() {
        if (!updated.isCompleted &&
            controller.capabilities.configuration?.version ==
                'changed by another client') {
          updated.complete();
        }
      });
      client.document = null;
      client._version = 'changed by another client';
      await updated.future.timeout(const Duration(seconds: 8));
      expect(controller.capabilities.configuration?.overrideState, 'none');
    } finally {
      await controller.close();
      controller.dispose();
      client.dispose();
    }
  });

  testWidgets('formatting is reversible in a narrow dark editor', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(360, 840));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final client = _OverrideClient();
    final controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    try {
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.dark(),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context)
                .copyWith(textScaler: const TextScaler.linear(1.6)),
            child: child!,
          ),
          home: SidraviaShell(controller: controller),
        ),
      );
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('自定义协议覆写规则已启用'));
      await tester.tap(find.text('自定义协议覆写规则已启用'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('格式化'));
      await tester.tap(find.text('格式化'));
      await tester.pumpAndSettle();
      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.controller!.text, contains('\n  "schemaVersion"'));
      await tester.ensureVisible(find.text('撤销格式化'));
      await tester.tap(find.text('撤销格式化'));
      await tester.pumpAndSettle();
      expect(field.controller!.text, original);
      expect(tester.takeException(), isNull);
      expect(client.submission, isNull);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
      client.dispose();
    }
  });

  test('home keeps the cleanup hint after a persisted clear', () {
    final client = _OverrideClient()..document = null;
    final config = client._summary();
    final session = SessionSummary(
      id: 's',
      displayName: '',
      accountName: 'user',
      intent: SessionIntent.maintainAuthentication,
      state: SessionState.stopping,
      cleanupRequired: true,
      hasProtocolOverride: true,
    );
    expect(
      projectOverrideStatus(config, [session], fresh: true),
      contains('清理未完成'),
    );
    expect(
      projectOverrideStatus(config, [session], fresh: false),
      startsWith('上次读取'),
    );
    expect(projectOverrideStatus(config, [], fresh: true), isNull);
    client.dispose();
  });

  testWidgets(
    'home opens the editor and returns an invalid draft to its parent without saving',
    (tester) async {
      final client = _OverrideClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
        pollDelay: const Duration(days: 1),
      );
      await controller.start();
      try {
        await tester.pumpWidget(
          MaterialApp(home: SidraviaShell(controller: controller)),
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text('自定义协议覆写规则已启用'));
        await tester.pumpAndSettle();
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          original,
        );
        await tester.enterText(find.byType(TextField), '{');
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.text('覆写草稿未完成'), findsOneWidget);
        expect(client.submission, isNull);
        await tester.ensureVisible(find.text('协议上报覆写'));
        await tester.tap(find.text('协议上报覆写'));
        await tester.pumpAndSettle();
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          '{',
        );
        await tester.enterText(find.byType(TextField), changed);
        await tester.ensureVisible(find.text('完成编辑'));
        await tester.tap(find.text('完成编辑'));
        await tester.pumpAndSettle();
        await tester.ensureVisible(find.text('保存修改'));
        await tester.tap(find.text('保存修改'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('保存并连接'));
        await tester.pumpAndSettle();
        expect(client.submission, {
          'configurationId': 'demo-config',
          'expectedVersion': 'initial',
          'overrideDocument': changed,
        });
        expect(client.startVersion, 'saved');
      } finally {
        await tester.pumpWidget(const SizedBox.shrink());
        await controller.close();
        controller.dispose();
        client.dispose();
      }
    },
  );
}
