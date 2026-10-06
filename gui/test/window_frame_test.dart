import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/dev/preview_main.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';

import 'home_responsive_test.dart' as fonts;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final calls = <MethodCall>[];
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, (call) async {
          calls.add(call);
          return call.method == 'configureFrame' ? {'maximized': false} : null;
        });
  });
  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, null);
  });
  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.android,
    TargetPlatform.iOS,
  ]) {
    for (final dark in [false, true]) {
      testWidgets('frame constraints $platform dark=$dark', (tester) async {
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetDevicePixelRatio);
        await tester.binding.setSurfaceSize(const Size(400, 690));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        await tester.pumpWidget(
          MaterialApp(
            theme: dark ? AppTheme.dark() : AppTheme.light(),
            home: const SidraviaWindowFrame(
              child: SizedBox.expand(key: ValueKey('business')),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final r = tester.getRect(find.byKey(const ValueKey('business')));
        expect(r.top, platform == TargetPlatform.windows ? 46 : 0);
        expect(r.height, platform == TargetPlatform.windows ? 644 : 690);
        expect(
          find.byTooltip('关闭'),
          platform == TargetPlatform.windows ? findsOneWidget : findsNothing,
        );
        if (platform == TargetPlatform.windows) {
          expect(
            tester.getSize(find.byKey(const ValueKey('window-top'))).height,
            46,
          );
          for (final label in ['最小化', '最大化', '关闭']) {
            final button = find.ancestor(
              of: find.byTooltip(label),
              matching: find.byType(TextButton),
            );
            expect(tester.getSize(button), const Size(36, 32));
            expect(tester.getRect(button).top, 10);
            await tester.tap(find.byTooltip(label));
            await tester.pump();
          }
          expect(
            calls.map((c) => c.method),
            containsAllInOrder([
              'configureFrame',
              'minimize',
              'toggleMaximize',
              'close',
            ]),
          );
        } else {
          expect(calls, isEmpty);
        }
      }, variant: TargetPlatformVariant({platform}));
    }
  }
  testWidgets(
    'native external state event controls restore; failures visible',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(home: const SidraviaWindowFrame(child: SizedBox.expand())),
      );
      await tester.pumpAndSettle();
      await tester.binding.defaultBinaryMessenger.handlePlatformMessage(
        'sidravia/window',
        const StandardMethodCodec().encodeMethodCall(
          const MethodCall('stateChanged', {
            'maximized': true,
            'maximizeHovered': true,
          }),
        ),
        (_) {},
      );
      await tester.pump();
      expect(find.byTooltip('还原'), findsOneWidget);
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            SidraviaWindowCommands.channel,
            (_) async => throw PlatformException(
              code: 'window_unavailable',
              message: '不可用',
            ),
          );
      await tester.tap(find.byTooltip('最小化'));
      await tester.pumpAndSettle();
      expect(find.text('窗口操作失败：不可用'), findsOneWidget);
      expect(find.byTooltip('还原'), findsOneWidget);
    },
    variant: TargetPlatformVariant({TargetPlatform.windows}),
  );
  testWidgets(
    'root dialog covers frame and centers whole window; Escape restores hit testing',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(400, 690));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [SidraviaWindowObserver()],
          home: SidraviaWindowFrame(
            child: Builder(
              builder: (context) => Center(
                child: TextButton(
                  onPressed: () => showDialog<void>(
                    context: context,
                    builder: (_) => const Dialog(
                      child: SizedBox(
                        width: 300,
                        height: 360,
                        key: ValueKey('panel'),
                      ),
                    ),
                  ),
                  child: const Text('弹窗'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('弹窗'));
      await tester.pumpAndSettle();
      expect(
        tester.getRect(find.byKey(const ValueKey('panel'))).center.dy,
        345,
      );
      expect(
        calls.where((c) => c.method == 'setModalBlocked').last.arguments,
        true,
      );
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('panel')), findsNothing);
      expect(
        calls.where((c) => c.method == 'setModalBlocked').last.arguments,
        false,
      );
    },
    variant: TargetPlatformVariant({TargetPlatform.windows}),
  );
  testWidgets('keyboard activates frame controls', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(home: SidraviaWindowFrame(child: SizedBox.expand())),
    );
    await tester.pumpAndSettle();
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(calls.map((c) => c.method), contains('minimize'));
  }, variant: TargetPlatformVariant({TargetPlatform.windows}));
  testWidgets(
    'formal Windows app keeps navigation and centers actual announcement at root',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(400, 690));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final client = OfflineDemoClient();
      final controller = GuiController(
        bootstrapper: OfflineDemoBootstrap(),
        connector: (_) async => client,
      );
      final now = DateTime.utc(2026, 10, 5);
      final announcements = AnnouncementController(
        endpoint: OfflineAnnouncementFetcher.endpoint,
        store: MemoryAnnouncementStore(),
        fetcher: OfflineAnnouncementFetcher(now),
        nowUtc: () => now,
      );
      await tester.pumpWidget(
        SidraviaApp(controller: controller, announcements: announcements),
      );
      await tester.pumpAndSettle();
      expect(
        tester.getRect(find.byKey(const ValueKey('home-settings-button'))).top,
        58,
      );
      await tester.tap(find.byKey(const ValueKey('home-settings-button')));
      await tester.pumpAndSettle();
      expect(find.text('诊断'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.byType(HomePage), findsOneWidget);
      await tester.tap(find.text('校园网维护安排'));
      await tester.pumpAndSettle();
      expect(
        tester
            .getRect(find.byKey(const ValueKey('announcement-panel')))
            .center
            .dy,
        345,
      );
      expect(
        calls.where((c) => c.method == 'setModalBlocked').last.arguments,
        true,
      );
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('announcement-panel')), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
      client.dispose();
    },
    variant: TargetPlatformVariant({TargetPlatform.windows}),
  );
  testWidgets('same business-size frame candidate', (tester) async {
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.binding.setSurfaceSize(const Size(398, 688));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.runAsync(fonts.loadCaptureFonts);
    await tester.pumpWidget(
      MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light(),
        home: RepaintBoundary(
          key: const ValueKey('frame-capture'),
          child: const SidraviaWindowFrame(
            child: RepaintBoundary(
              key: ValueKey('content-capture'),
              child: ColoredBox(color: AppColors.bgLight, child: HomeFixture()),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      tester.getSize(find.byKey(const ValueKey('content-capture'))),
      const Size(398, 642),
    );
    const output = String.fromEnvironment('R5_OUTPUT');
    if (output.isNotEmpty) {
      await tester.runAsync(() async {
        await Directory(output).create(recursive: true);
        for (final name in ['frame', 'content']) {
          final boundary = tester.renderObject<RenderRepaintBoundary>(
            find.byKey(ValueKey('$name-capture')),
          );
          final image = await boundary.toImage(pixelRatio: 1);
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          await File('$output/$name.png')
              .writeAsBytes(bytes!.buffer.asUint8List());
          image.dispose();
        }
      });
    }
  }, variant: TargetPlatformVariant({TargetPlatform.windows}));
}
