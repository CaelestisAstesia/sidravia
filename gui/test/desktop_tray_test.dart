import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/tray_entrypoint.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'desktop publishes one current projection and stops after disposal',
    () async {
      const channel = MethodChannel(desktopPresenceChannelName);
      final published = <Object?>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            if (call.method == 'initialize') return 'primary';
            if (call.method == 'getVisibility') return true;
            if (call.method == 'publishTray') published.add(call.arguments);
            return null;
          });
      final desktop = DesktopPresence();
      await desktop.initialize();
      const data = TrayPopoverData(
        statusTitle: '已连接',
        dark: false,
        institution: '吉林大学',
        username: 'demo',
        statusDetail: '认证成功',
        statusContext: '有线网络 · 192.0.2.1',
        glyph: '✓',
        tone: HomeTone.success,
        primaryLabel: '断开连接',
        primaryAction: TrayIntent(
          TrayIntentKind.connection,
          connectionAction: GuiConnectionActionKind.stop,
          targetId: 'session-7',
        ),
        primaryEnabled: true,
        disabledReason: '',
      );
      await desktop.publishTray(data);
      await desktop.publishTray(data);
      expect(published, hasLength(1));
      expect(
        TrayPopoverData.fromMap(published.single)?.primaryAction.targetId,
        'session-7',
      );
      desktop.dispose();
      await desktop.publishTray(data);
      expect(published, hasLength(1));
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null);
    },
  );

  test('native action decode ignores invalid maps', () {
    expect(TrayIntent.fromMap(null), isNull);
    expect(TrayIntent.fromMap({'kind': 'exit'}), isNotNull);
    expect(
      TrayIntent.fromMap({'kind': 'connection', 'connectionKind': 'stop'}),
      isNull,
    );
  });

  testWidgets('popover reports native channel failures and disposes cleanly', (
    tester,
  ) async {
    const channel = MethodChannel(trayViewChannelName);
    final methods = <String>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          methods.add(call.method);
          throw PlatformException(code: 'unavailable');
        });
    await tester.pumpWidget(
      TrayPopoverApp(
        initialData: const TrayPopoverData(
          statusTitle: '等待重试',
          dark: false,
          institution: '吉林大学',
          username: 'demo',
          statusDetail: '服务暂不可用',
          statusContext: '请重试',
          glyph: '!',
          tone: HomeTone.error,
          primaryLabel: '立即重试',
          primaryAction: TrayIntent(
            TrayIntentKind.connection,
            connectionAction: GuiConnectionActionKind.retryIpc,
          ),
          primaryEnabled: true,
          disabledReason: '',
        ),
      ),
    );
    await tester.pump();
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('立即重试'));
    await tester.pump();
    expect(find.text('托盘操作暂不可用，请双击图标显示主界面后重试。'), findsOneWidget);
    expect(
      methods,
      containsAll(['getPresentation', 'resizeToContent', 'action']),
    );
    await tester.pumpWidget(const SizedBox.shrink());
    expect(tester.takeException(), isNull);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });
}
