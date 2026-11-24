import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';

const _states = {
  'authenticated': ['已连接', '断开连接', '连接与诊断', 'session.stop'],
  'suspended': ['未连接', '开始连接', '更改配置', 'session.start_configuration'],
  'authenticating': ['正在认证', '取消连接', '连接与诊断', 'session.stop'],
  'waiting_for_network': ['等待网络', '取消等待', '连接与诊断', 'session.stop'],
  'waiting_before_retry': ['等待重试', '立即重试', '停止重试', 'session.restart'],
  'blocked_by_error': ['认证失败', '重新连接', '检查账号配置', 'session.restart'],
  'empty': ['尚未配置', '添加配置', '设置', 'configuration'],
};

void main() {
  // These actions are shared by all frontends. Platform, focus and layout
  // differences are exercised separately in desktop_shell_flows_test.dart.
  for (final entry in _states.entries) {
    testWidgets('home ${entry.key} binds the primary action', (tester) async {
      final client = OfflineDemoClient()..selectScenario(entry.key);
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      await tester.pumpAndSettle();

      final [title, primary, secondary, operation] = entry.value;
      expect(find.byType(HomePage), findsOneWidget);
      expect(find.text(title), findsAtLeastNWidgets(1));
      expect(find.text(secondary), findsOneWidget);
      expect(find.textContaining('30 秒'), findsNothing);
      if (entry.key == 'blocked_by_error') {
        expect(find.text('credential_invalid'), findsNothing);
        expect(find.text('服务器拒绝了本次认证'), findsOneWidget);
      }
      await tester.tap(find.text(primary));
      await tester.pumpAndSettle();
      if (entry.key == 'empty') {
        expect(find.text('保存修改'), findsOneWidget);
      } else {
        expect(client.operations.last, operation);
      }
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    });
  }

  testWidgets(
    'retry secondary stops the actual session; failure secondary opens configuration',
    (tester) async {
      final client = OfflineDemoClient()
        ..selectScenario('waiting_before_retry');
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(home: SidraviaShell(controller: controller)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('停止重试'));
      await tester.pumpAndSettle();
      expect(client.operations.last, 'session.stop');
      client.selectScenario('blocked_by_error');
      await controller.start();
      await tester.pumpAndSettle();
      await tester.tap(find.text('检查账号配置'));
      await tester.pumpAndSettle();
      expect(find.text('保存修改'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      client.dispose();
    },
  );
}
