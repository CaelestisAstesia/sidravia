import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';

void main() {
  const data = TrayPopoverData(
    statusTitle: '等待重试',
    dark: false,
    institution: '吉林大学',
    username: 'a-very-long-school-account-name@example.edu.cn',
    statusDetail: '认证暂未成功，将自动重试',
    statusContext: '可以立即重试',
    glyph: '↻',
    tone: HomeTone.warning,
    primaryLabel: '立即重试',
    primaryAction: TrayIntent(
      TrayIntentKind.connection,
      connectionAction: GuiConnectionActionKind.reconnect,
      targetId: 'session-1',
    ),
    primaryEnabled: true,
    disabledReason: '',
    secondaryLabel: '停止重试',
    secondaryAction: TrayIntent(
      TrayIntentKind.connection,
      connectionAction: GuiConnectionActionKind.stop,
      targetId: 'session-1',
    ),
    secondaryEnabled: true,
  );

  test('typed intent rejects malformed payload and round trips identity', () {
    final intent = TrayIntent.fromMap(data.primaryAction.toMap());
    expect(intent?.connectionAction, GuiConnectionActionKind.reconnect);
    expect(intent?.targetId, 'session-1');
    expect(TrayIntent.fromMap({'kind': 'connection'}), isNull);
    expect(TrayIntent.fromMap({'kind': 'other'}), isNull);
    expect(TrayPopoverData.fromMap({'tone': 'impossible'}), isNull);
  });

  test(
    'all presentation actions preserve navigation kind and edit identity',
    () {
      final cases = <(GuiConnectionAction, TrayIntentKind, String?)>[
        (
          const GuiConnectionAction(GuiConnectionActionKind.start, 's1'),
          TrayIntentKind.connection,
          's1',
        ),
        (
          const GuiConnectionAction(GuiConnectionActionKind.reconnect, 's1'),
          TrayIntentKind.connection,
          's1',
        ),
        (
          const GuiConnectionAction(GuiConnectionActionKind.stop, 's1'),
          TrayIntentKind.connection,
          's1',
        ),
        (
          const GuiConnectionAction(GuiConnectionActionKind.retryIpc),
          TrayIntentKind.connection,
          null,
        ),
        (
          const GuiConnectionAction(GuiConnectionActionKind.addConfiguration),
          TrayIntentKind.addConfiguration,
          null,
        ),
        (
          const GuiConnectionAction(
            GuiConnectionActionKind.editConfiguration,
            'config-7',
          ),
          TrayIntentKind.editConfiguration,
          'config-7',
        ),
        (
          const GuiConnectionAction(GuiConnectionActionKind.showSettings),
          TrayIntentKind.showSettings,
          null,
        ),
        (
          const GuiConnectionAction(GuiConnectionActionKind.showDetails),
          TrayIntentKind.showDetails,
          null,
        ),
      ];
      for (final (action, kind, targetId) in cases) {
        final intent = TrayIntent.fromConnectionAction(action);
        final decoded = TrayIntent.fromMap(intent.toMap());
        expect(decoded?.kind, kind);
        expect(decoded?.targetId, targetId);
        final projection = data.toMap()..['primaryAction'] = intent.toMap();
        final roundTrip = TrayPopoverData.fromMap(projection);
        expect(roundTrip?.primaryAction.kind, kind);
        expect(roundTrip?.primaryAction.targetId, targetId);
      }
      expect(TrayIntent.fromMap({'kind': 'editConfiguration'}), isNull);
      expect(
        TrayIntent.fromMap({'kind': 'editConfiguration', 'targetId': ''}),
        isNull,
      );
      expect(
        TrayIntent.fromMap({'kind': 'showSettings', 'targetId': 'config-7'}),
        isNull,
      );
      expect(
        TrayIntent.fromMap({
          'kind': 'addConfiguration',
          'connectionKind': 'start',
        }),
        isNull,
      );
    },
  );

  test(
    'first-use create and auth-failure edit survive projection round trip',
    () {
      ConnectionPresentation presentation({
        required String title,
        required GuiConnectionAction primary,
        required GuiConnectionAction secondary,
      }) => ConnectionPresentation(
        institution: '吉林大学',
        username: 'demo',
        statusTitle: title,
        statusDetail: '详情',
        statusContext: '',
        glyph: '!',
        tone: HomeTone.error,
        primaryLabel: '继续',
        secondaryLabel: '编辑配置',
        primaryAction: primary,
        secondaryAction: secondary,
        primaryEnabled: true,
        secondaryEnabled: true,
        primaryKind: HomeButtonKind.primary,
      );

      final create = TrayPopoverData.fromPresentation(
        presentation(
          title: '未连接',
          primary: const GuiConnectionAction(
            GuiConnectionActionKind.addConfiguration,
          ),
          secondary: const GuiConnectionAction(
            GuiConnectionActionKind.showSettings,
          ),
        ),
        dark: false,
      );
      expect(
        TrayPopoverData.fromMap(create.toMap())?.primaryAction.kind,
        TrayIntentKind.addConfiguration,
      );

      final authFailure = TrayPopoverData.fromPresentation(
        presentation(
          title: '认证失败',
          primary: const GuiConnectionAction(
            GuiConnectionActionKind.reconnect,
            'session-4',
          ),
          secondary: const GuiConnectionAction(
            GuiConnectionActionKind.editConfiguration,
            'configuration-4',
          ),
        ),
        dark: false,
      );
      final restored = TrayPopoverData.fromMap(authFailure.toMap());
      expect(restored?.secondaryAction?.kind, TrayIntentKind.editConfiguration);
      expect(restored?.secondaryAction?.targetId, 'configuration-4');
    },
  );

  testWidgets('popover keeps bounded width, current actions and exit rows', (
    tester,
  ) async {
    final actions = <TrayIntent>[];
    await tester.binding.setSurfaceSize(const Size(360, 640));
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Center(
            child: TrayPopover(data: data, onAction: actions.add),
          ),
        ),
      ),
    );
    expect(find.text('等待重试'), findsOneWidget);
    expect(find.text('停止重试'), findsOneWidget);
    expect(find.text('显示主界面'), findsOneWidget);
    expect(find.text('退出 Sidravia'), findsOneWidget);
    expect(tester.getSize(find.byType(TrayPopover)).width, 210);
    await tester.tap(find.text('立即重试'));
    await tester.pump();
    expect(actions.single.targetId, 'session-1');
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.binding.setSurfaceSize(null);
  });

  testWidgets('large text remains scrollable and disabled reason is visible', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(360, 640));
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(2)),
          child: Scaffold(
            body: Center(
              child: TrayPopover(
                data: TrayPopoverData(
                  statusTitle: '服务不可用',
                  dark: true,
                  institution: data.institution,
                  username: data.username,
                  statusDetail: data.statusDetail,
                  statusContext: data.statusContext,
                  glyph: '!',
                  tone: HomeTone.error,
                  primaryLabel: '重试连接',
                  primaryAction: const TrayIntent(
                    TrayIntentKind.connection,
                    connectionAction: GuiConnectionActionKind.retryIpc,
                  ),
                  primaryEnabled: false,
                  disabledReason: '服务暂不可用，请打开主界面检查。',
                ),
                onAction: (_) {},
              ),
            ),
          ),
        ),
      ),
    );
    expect(tester.takeException(), isNull);
    expect(find.byTooltip('服务暂不可用，请打开主界面检查。'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.binding.setSurfaceSize(null);
  });
}
