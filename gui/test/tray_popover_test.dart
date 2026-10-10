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
