import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/project_settings.dart';

void main() {
  const channel = MethodChannel('sidravia/window');
  const address = 'https://github.com/CaelestisAstesia/sidravia';

  for (final outcome in ['opened', 'failed', 'unsupported']) {
    testWidgets('project address click: $outcome', (tester) async {
      final calls = <MethodCall>[];
      final messenger = tester.binding.defaultBinaryMessenger;
      messenger.setMockMethodCallHandler(channel, (call) async {
        calls.add(call);
        if (outcome == 'failed') {
          throw PlatformException(code: 'project_page_open_failed');
        }
        if (outcome == 'unsupported') throw MissingPluginException();
        return null;
      });
      addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => Column(
                children: projectSettingsRows(context, version: '1.2.3'),
              ),
            ),
          ),
        ),
      );
      expect(find.text('软件版本'), findsOneWidget);
      expect(find.text('1.2.3'), findsOneWidget);
      expect(find.text('许可信息'), findsOneWidget);
      expect(find.byType(Dialog), findsNothing);
      expect(calls, isEmpty);
      await tester.tap(find.text('项目地址'));
      await tester.pumpAndSettle();
      expect(calls.single.method, 'openProjectPage');
      expect(calls.single.arguments, isNull);
      expect(
        find.text('无法打开浏览器\n$address'),
        outcome == 'opened' ? findsNothing : findsOneWidget,
      );
      expect(tester.takeException(), isNull);
    });
  }
}
