import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/home/server_messages_panel.dart';
import 'package:sidravia_gui/features/home/connection_information_view.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final width in [320.0, 400.0, 720.0]) {
    for (final scale in [1.0, 2.0]) {
      testWidgets(
        'button top remains stable across scenarios at $width/$scale',
        (tester) async {
          await tester.binding.setSurfaceSize(Size(width, 1100));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          final client = OfflineDemoClient();
          final controller = GuiController(
            bootstrapper: OfflineDemoBootstrap(),
            connector: (_) async => client,
          );
          await controller.start();
          await tester.pumpWidget(
            MaterialApp(
              theme: AppTheme.light(),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: Scaffold(
                body: HomePage(controller: controller, onNavigate: (_) {}),
              ),
            ),
          );
          double? first;
          for (final scenario in [
            'authenticated',
            'authenticated_business',
            'authenticating',
            'waiting_for_network',
            'waiting_before_retry',
            'blocked_by_error',
            'suspended',
            'empty',
          ]) {
            client.selectScenario(scenario);
            await controller.start();
            await tester.pumpAndSettle();
            final top = tester
                .getRect(find.byKey(const ValueKey('home-actions')))
                .top;
            first ??= top;
            if ((width >= 400 && scale == 1) || width >= 720) {
              expect(top, closeTo(first, .01), reason: scenario);
            } else {
              // Below the desktop minimum or with enlarged text, expand rather than clip.
              expect(top, greaterThanOrEqualTo(first - .01), reason: scenario);
            }
            final information = find.byType(ConnectionInformationView);
            expect(information, findsOneWidget, reason: scenario);
            final texts = tester.widgetList<Text>(
              find.descendant(of: information, matching: find.byType(Text)),
            );
            for (final text in texts) {
              expect(
                text.data,
                isNot(matches(RegExp(r'[。.]$'))),
                reason: scenario,
              );
              expect(text.style?.fontSize, 12, reason: scenario);
              expect(text.style?.height, 1.45, reason: scenario);
              expect(text.style?.fontWeight, FontWeight.w400, reason: scenario);
            }
            if (scenario == 'blocked_by_error') {
              expect(find.text('请按下方提示处理后重试'), findsNothing);
              expect(find.text('服务器拒绝了本次认证'), findsOneWidget);
            }
            expect(tester.takeException(), isNull);
          }
          await tester.pumpWidget(const SizedBox.shrink());
          await controller.close();
          controller.dispose();
          client.dispose();
        },
      );
    }
  }

  testWidgets(
    'connection information centers as a group and units stay together',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(320, 1100));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context)
                .copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: Scaffold(
            body: HomePage(controller: controller, onNavigate: (_) {}),
          ),
        ),
      );
      for (final scenario in ['authenticated', 'authenticated_business']) {
        client.selectScenario(scenario);
        await controller.start();
        await tester.pumpAndSettle();
        final information = find.byType(ConnectionInformationView);
        final area = tester.getRect(
          find.ancestor(of: information, matching: find.byType(Center)).first,
        );
        expect(
          tester.getRect(information).center.dy,
          closeTo(area.center.dy, .01),
        );
        final time = find.textContaining('认证成功 ·');
        expect(
          find.descendant(of: information, matching: time),
          findsOneWidget,
        );
        expect(
          tester.getRect(time).bottom,
          lessThan(tester.getRect(find.text('以太网 · 192.168.1.20')).top),
        );
        if (scenario != 'authenticated') {
          final value = find.text('120.56\u00a0MiB');
          final widget = tester.widget<Text>(value);
          final painter = TextPainter(
            text: TextSpan(text: widget.data, style: widget.style),
            textDirection: TextDirection.ltr,
            textScaler: const TextScaler.linear(2),
          )..layout();
          expect(tester.getSize(value).height, closeTo(painter.height, .1));
          expect(tester.getSize(value).width, lessThanOrEqualTo(area.width));
          painter.dispose();
          expect(find.text('登录时服务器快照'), findsNothing);
        }
        expect(tester.takeException(), isNull);
      }
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
      client.dispose();
    },
  );

  testWidgets(
    'announcement precedes messages and both align with configuration text',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(400, 1000));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = OfflineDemoClient()
        ..selectScenario('authenticated_business');
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      await controller.start();
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: Scaffold(
            body: HomeView(
              data: HomeViewData(
                institution: '吉林大学',
                username: 'student01',
                state: '已连接',
                detail: '认证成功',
                context: '以太网 · 192.0.2.4',
                tone: HomeTone.success,
                secondaryLabel: '连接与诊断',
                primaryLabel: '断开连接',
                primaryKind: HomeButtonKind.secondary,
                primaryEnabled: true,
                onHeader: () {},
                onSettings: () {},
                onSecondary: () {},
                onPrimary: () {},
                announcement: HomeAnnouncementEntry(
                  title: '查看吉林大学公告',
                  onOpen: () {},
                ),
                serverMessages: ServerMessagesPanel(
                  controller: controller.business,
                ),
                information: ConnectionInformationView(
                  controller: controller.business,
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final school = tester.getRect(find.text('吉林大学'));
      final announcement = tester.getRect(find.text('吉林大学公告'));
      final messages = tester.getRect(find.text('服务器消息'));
      final divider = tester.getRect(
        find.byKey(const ValueKey('home-divider')),
      );
      final buttons = tester.getRect(
        find.byKey(const ValueKey('home-actions')),
      );
      expect(announcement.left, school.left);
      expect(messages.left, school.left);
      expect(announcement.top, lessThan(messages.top));
      expect(divider.top, greaterThan(buttons.bottom));
      expect(divider.bottom, lessThan(announcement.top));
      expect(
        tester.getRect(find.text('120.56\u00a0MiB')).bottom,
        lessThan(buttons.top),
      );
      await tester.pumpWidget(const SizedBox.shrink());
      await controller.close();
      controller.dispose();
      client.dispose();
    },
  );
}
