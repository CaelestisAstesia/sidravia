import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final calls = <MethodCall>[];
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, (call) async {
          calls.add(call);
          return null;
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
    for (final dark in [false, true]) {
      testWidgets(
        'native frame preserves client content $platform dark=$dark',
        (tester) async {
          await tester.binding.setSurfaceSize(const Size(400, 688));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          await tester.pumpWidget(
            MaterialApp(
              theme: dark ? ThemeData.dark() : ThemeData.light(),
              home: SidraviaWindowFrame(
                platform: platform,
                child: const SizedBox.expand(key: ValueKey('content')),
              ),
            ),
          );
          await tester.pump();
          expect(
            tester.getRect(find.byKey(const ValueKey('content'))),
            const Rect.fromLTWH(0, 0, 400, 688),
          );
          expect(find.byKey(const ValueKey('window-top')), findsNothing);
          expect(find.byTooltip('最小化'), findsNothing);
          expect(find.byTooltip('关闭'), findsNothing);
          expect(
            calls.map((call) => call.method),
            platform == TargetPlatform.windows ? ['setDarkMode'] : isEmpty,
          );
          if (platform == TargetPlatform.windows) {
            expect(calls.single.arguments, dark);
          }
          expect(tester.takeException(), isNull);
        },
      );
    }
  }
  testWidgets('native frame reports DWM theme failures', (tester) async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SidraviaWindowCommands.channel, (_) async {
          throw PlatformException(code: 'window_unavailable');
        });
    await tester.pumpWidget(
      const MaterialApp(
        home: SidraviaWindowFrame(
          platform: TargetPlatform.windows,
          child: SizedBox.expand(),
        ),
      ),
    );
    await tester.pump();
    expect(find.textContaining('窗口主题更新失败'), findsOneWidget);
  });
}
