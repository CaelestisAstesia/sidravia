import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final calls = <MethodCall>[];
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, (call) async {
          calls.add(call);
          return call.method == 'configureFrame'
              ? {'maximized': false, 'customFrame': true}
              : null;
        });
  });
  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, null);
  });
  for (final platform in [
    TargetPlatform.windows,
    TargetPlatform.linux,
    TargetPlatform.macOS,
  ]) {
    for (final size in [const Size(360, 640), const Size(400, 690)]) {
      for (final dark in [false, true]) {
        testWidgets('frame constraints $platform dark=$dark size=$size', (
          tester,
        ) async {
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.resetDevicePixelRatio);
          await tester.binding.setSurfaceSize(size);
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
          expect(
            r.height,
            platform == TargetPlatform.windows ? size.height - 46 : size.height,
          );
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
              expect(
                tester.getRect(button).right,
                size.width -
                    10 -
                    ['最小化', '最大化', '关闭'].reversed.toList().indexOf(label) * 38,
              );
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
            expect(find.byKey(const ValueKey('window-outline')), findsNothing);
            expect(calls, isEmpty);
          }
        }, variant: TargetPlatformVariant({platform}));
      }
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
      final commandsBefore = calls.where((c) => c.method == 'close').length;
      await tester.tapAt(tester.getCenter(find.byTooltip('关闭')));
      await tester.pumpAndSettle();
      expect(
        calls.where((c) => c.method == 'close'),
        hasLength(commandsBefore),
      );
      // Barrier tap may dismiss; reopen to prove Escape also releases native blocking.
      if (find.byKey(const ValueKey('panel')).evaluate().isEmpty) {
        await tester.tap(find.text('弹窗'));
        await tester.pumpAndSettle();
      }
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
    'configure failure keeps native caption fallback without duplicate controls',
    (tester) async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SidraviaWindowCommands.channel, (
            call,
          ) async {
            if (call.method == 'configureFrame') {
              throw PlatformException(code: 'failed', message: '不可用');
            }
            return null;
          });
      await tester.pumpWidget(
        const MaterialApp(
          home: SidraviaWindowFrame(
            child: SizedBox.expand(key: ValueKey('content')),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('content')), findsOneWidget);
      expect(find.byKey(const ValueKey('window-top')), findsNothing);
      expect(find.byTooltip('关闭'), findsNothing);
      expect(find.text('窗口初始化失败：不可用'), findsOneWidget);
    },
    variant: TargetPlatformVariant({TargetPlatform.windows}),
  );

  testWidgets('DWM theme failures remain visible in native fallback', (
    tester,
  ) async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, (call) async {
          if (call.method == 'setDarkMode') {
            throw PlatformException(code: 'window_unavailable');
          }
          return {'customFrame': false};
        });
    await tester.pumpWidget(
      const MaterialApp(home: SidraviaWindowFrame(child: SizedBox.expand())),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('窗口主题更新失败'), findsOneWidget);
    expect(find.byTooltip('关闭'), findsNothing);
  }, variant: TargetPlatformVariant({TargetPlatform.windows}));

  testWidgets('missing plugin preview requires explicit Windows override', (
    tester,
  ) async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          SidraviaWindowCommands.channel,
          (_) async => throw MissingPluginException(),
        );
    await tester.pumpWidget(
      const MaterialApp(
        home: SidraviaWindowFrame(
          platform: TargetPlatform.windows,
          child: SizedBox.expand(),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('window-top')), findsOneWidget);
    expect(find.text('当前环境没有 Windows 窗口控制'), findsOneWidget);
  });
}
