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
  bool unreadable = false, exists = true, failRefreshAfterCreate = false;
  bool legacy = false, _failNextList = false;
  String? readBackId;
  Map<String, Object?>? submission;
  String? startVersion, unversionedStartId;
  String _id = 'demo-config', _version = 'initial';
  @override
  Future<List<SessionSummary>> sessionList() async => [];
  @override
  Future<List<InstitutionProfile>> profileList() async => [
    InstitutionProfile(
      id: 'jlu',
      displayName: '吉林大学',
      protocolId: 'd520',
      overrideSchemaVersion: legacy ? 0 : 1,
    ),
  ];
  ConfigurationSummary _summary() => ConfigurationSummary(
    id: _id,
    displayName: '',
    institutionProfileId: 'jlu',
    institutionDisplayName: '吉林大学',
    authenticationProtocolId: 'd520',
    username: 'student01',
    credentialStored: true,
    storageProtection: 'protected',
    version: legacy ? null : _version,
    overrideState: legacy
        ? null
        : unreadable
        ? 'invalid'
        : document == null
        ? 'none'
        : 'enabled',
    overrideSchemaVersion: legacy ? 0 : 1,
  );
  @override
  Future<List<ConfigurationSummary>> configurationList() async {
    if (_failNextList) {
      _failNextList = false;
      throw const IpcTransportException('ipc_disconnected');
    }
    return [if (exists) _summary()];
  }

  @override
  Future<ConfigurationOverride> configurationOverride(
    String id, {
    String? expectedDocument,
  }) async {
    readBackId = id;
    return (
      configuration: _summary(),
      document: unreadable ? null : document,
      matchesDocument: expectedDocument == null
          ? null
          : document == expectedDocument,
    );
  }

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
    unreadable = false;
    exists = true;
    if (creating) _id = 'created-config';
    _version = creating ? 'created' : 'saved';
    _failNextList = creating && failRefreshAfterCreate;
    return _summary();
  }

  @override
  Future<SessionSummary> sessionStartConfiguration(String id) {
    unversionedStartId = id;
    return super.sessionStartConfiguration('demo-config');
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

Future<void> _tap(WidgetTester tester, Finder target) async {
  await tester.ensureVisible(target);
  await tester.tap(target);
  await tester.pumpAndSettle();
}

Future<void> _openOverride(WidgetTester tester) =>
    _tap(tester, find.text('协议上报覆写'));

Future<void> _openConfiguration(WidgetTester tester) =>
    _tap(tester, find.byKey(const ValueKey('home-configuration-button')));

Future<void> _finishAndSave(WidgetTester tester) async {
  await _tap(tester, find.text('保存修改'));
  await _tap(tester, find.text('保存修改'));
  await _tap(tester, find.text('保存'));
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
      expect(find.text('协议覆写已启用'), findsOneWidget);
      await _openConfiguration(tester);
      await _openOverride(tester);
      await tester.ensureVisible(find.text('格式化'));
      await tester.tap(find.text('格式化'));
      await tester.pumpAndSettle();
      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.controller!.text, contains('\n  "schemaVersion"'));
      await _tap(tester, find.byTooltip('撤销'));
      expect(field.controller!.text, original);
      await _tap(tester, find.byTooltip('重做'));
      expect(field.controller!.text, formatOverride(original));
      await tester.enterText(find.byType(TextField), changed);
      await tester.tap(find.text('格式化'));
      await tester.pump();
      expect(field.controller!.text, formatOverride(changed));
      await tester.tap(find.byTooltip('撤销'));
      await tester.pump();
      expect(field.controller!.text, changed);
      await tester.showKeyboard(find.byType(TextField));
      tester.testTextInput.updateEditingValue(
        const TextEditingValue(
          text: changed,
          selection: TextSelection.collapsed(offset: 20),
          composing: TextRange(start: 17, end: 20),
        ),
      );
      await tester.pump();
      expect(
        tester
            .widget<OutlinedButton>(find.widgetWithText(OutlinedButton, '格式化'))
            .onPressed,
        isNull,
      );
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
    'configuration entry, discard, and version-bound save work together',
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
        await _openConfiguration(tester);
        await _openOverride(tester);
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          original,
        );
        await tester.enterText(find.byType(TextField), '{');
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.text('放弃未保存的修改？'), findsOneWidget);
        await _tap(tester, find.text('继续编辑'));
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          '{',
        );
        await _tap(tester, find.byTooltip('返回'));
        await _tap(tester, find.text('放弃修改'));
        expect(client.submission, isNull);
        await _openOverride(tester);
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          original,
        );
        await tester.enterText(find.byType(TextField), changed);
        await _tap(tester, find.text('保存修改'));
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
        expect(client.unversionedStartId, isNull);
      } finally {
        await tester.pumpWidget(const SizedBox.shrink());
        await controller.close();
        controller.dispose();
        client.dispose();
      }
    },
  );
  for (final unreadable in [false, true]) {
    testWidgets(
      'confirmed clear remains distinct from JSON replacement ($unreadable)',
      (tester) async {
        final client = _OverrideClient()..unreadable = unreadable;
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
          await _openConfiguration(tester);
          await _openOverride(tester);
          await _tap(tester, find.text('恢复协议默认值'));
          await _tap(tester, find.text('确认'));
          if (unreadable) {
            expect(
              tester.widget<TextField>(find.byType(TextField)).readOnly,
              isTrue,
            );
          } else {
            final template = tester
                .widget<TextField>(find.byType(TextField))
                .controller!
                .text;
            await tester.enterText(find.byType(TextField), changed);
            await _tap(tester, find.byTooltip('撤销'));
            expect(
              tester.widget<TextField>(find.byType(TextField)).controller!.text,
              template,
            );
          }
          await _finishAndSave(tester);
          expect(client.submission, {
            'configurationId': 'demo-config',
            'expectedVersion': 'initial',
            'protocolContextOverride': null,
          });
        } finally {
          await tester.pumpWidget(const SizedBox.shrink());
          await controller.close();
          controller.dispose();
          client.dispose();
        }
      },
    );
  }

  for (final creating in [false, true]) {
    testWidgets(
      'save and connect works with an unversioned core (creating: $creating)',
      (tester) async {
        final client = _OverrideClient()
          ..exists = !creating
          ..legacy = true
          ..document = null;
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
          await _openConfiguration(tester);
          await tester.enterText(find.byType(TextField).first, 'student02');
          if (creating) {
            await tester.enterText(
              find.byType(TextField).last,
              'synthetic-password',
            );
          }
          await tester.pumpAndSettle();
          await _tap(tester, find.text('保存修改'));
          await _tap(tester, find.text('保存并连接'));
          expect(
            client.unversionedStartId,
            creating ? 'created-config' : 'demo-config',
          );
          expect(client.startVersion, isNull);
        } finally {
          await tester.pumpWidget(const SizedBox.shrink());
          await controller.close();
          controller.dispose();
          client.dispose();
        }
      },
    );
  }

  for (final changedAfterCommit in [false, true]) {
    testWidgets(
      changedAfterCommit
          ? 'readback preserves the password draft after the receipt version changes'
          : 'a committed create with a failed refresh retains the returned readback target',
      (tester) async {
        final client = _OverrideClient()
          ..exists = false
          ..document = null
          ..failRefreshAfterCreate = true;
        final controller = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => client,
          pollDelay: const Duration(days: 1),
          reconnectDelay: const Duration(days: 1),
        );
        await controller.start();
        try {
          await tester.pumpWidget(
            MaterialApp(home: SidraviaShell(controller: controller)),
          );
          await tester.pumpAndSettle();
          await _openConfiguration(tester);
          await tester.enterText(find.byType(TextField).first, 'student01');
          await tester.enterText(
            find.byType(TextField).last,
            'synthetic-password',
          );
          await _openOverride(tester);
          await tester.enterText(find.byType(TextField), changed);
          await _finishAndSave(tester);
          expect(client.exists, isTrue);
          expect(find.textContaining('配置已保存，但刷新失败'), findsOneWidget);
          if (changedAfterCommit) client._version = 'newer-version';
          await controller.start();
          await tester.pumpAndSettle();
          await _tap(tester, find.text('核对已保存的配置'));
          expect(client.readBackId, 'created-config');
          if (changedAfterCommit) {
            expect(find.textContaining('无法核对已提交的密码'), findsOneWidget);
            expect(find.textContaining('当前保存内容已核对'), findsNothing);
            expect(
              tester
                  .widget<TextField>(find.byType(TextField).last)
                  .controller
                  ?.text,
              'synthetic-password',
            );
          } else {
            expect(find.textContaining('当前保存内容已核对'), findsOneWidget);
          }
          expect(client.startVersion, isNull);
        } finally {
          await tester.pumpWidget(const SizedBox.shrink());
          await controller.close();
          controller.dispose();
          client.dispose();
        }
      },
    );
  }
}
