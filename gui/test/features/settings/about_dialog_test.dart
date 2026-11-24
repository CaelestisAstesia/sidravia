import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/about_dialog.dart';

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
              builder: (context) => TextButton(
                onPressed: () => showSidraviaAbout(context, version: 'test'),
                child: const Text('关于'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('关于'));
      await tester.pumpAndSettle();
      expect(find.text('Sidravia 网络认证客户端'), findsOneWidget);
      expect(find.text('Sidravia 校园网客户端'), findsNothing);
      expect(calls, isEmpty);
      expect(
        tester.widget<SelectableText>(find.byType(SelectableText)).onTap,
        isNotNull,
      );
      await tester.tap(find.text(address));
      await tester.pumpAndSettle();
      expect(calls.single.method, 'openProjectPage');
      expect(calls.single.arguments, isNull);
      expect(
        find.text('无法打开浏览器，请选择并复制项目地址后访问。'),
        outcome == 'opened' ? findsNothing : findsOneWidget,
      );
      expect(tester.takeException(), isNull);
    });
  }
}
