import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/preview_main.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  testWidgets(
    'connected reference fixture renders the production home surface',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(400, 644));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.pumpWidget(
        MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: AppTheme.light(),
          home: const Scaffold(body: HomeFixture()),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('已连接'), findsOneWidget);
      expect(find.text('校园网维护安排'), findsOneWidget);
      await expectLater(
        find.byType(HomeFixture),
        matchesGoldenFile('goldens/home-connected-400x644.png'),
      );
    },
  );
}
