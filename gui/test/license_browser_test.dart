import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/license_browser.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  setUp(LicenseRegistry.reset);
  tearDown(LicenseRegistry.reset);
  for (final scale in [1.0, 2.0]) {
    testWidgets(
      'component search preserves every shared and separate license at $scale',
      (tester) async {
        await tester.binding.setSurfaceSize(const Size(400, 690));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        LicenseRegistry.addLicense(
          () => Stream.fromIterable([
            const LicenseEntryWithLineBreaks([
              'Alpha',
              'Beta',
            ], 'Shared copyright\n\nShared terms.'),
            const LicenseEntryWithLineBreaks([
              'Alpha',
            ], 'Separate copyright\n\nSeparate terms.'),
            const LicenseEntryWithLineBreaks([], 'Unassigned terms.'),
          ]),
        );
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.dark(),
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(context)
                  .copyWith(textScaler: TextScaler.linear(scale)),
              child: child!,
            ),
            home: Scaffold(
              body: Builder(
                builder: (context) => TextButton(
                  onPressed: () => showDialog<void>(
                    context: context,
                    builder: (_) => const LicenseBrowser(),
                  ),
                  child: const Text('许可'),
                ),
              ),
            ),
          ),
        );
        await tester.tap(find.text('许可'));
        await tester.pumpAndSettle();
        expect(find.text('Alpha'), findsOneWidget);
        final titleContext = tester.element(find.text('Alpha'));
        expect(
          DefaultTextStyle.of(titleContext).style.color,
          Theme.of(titleContext).colorScheme.onSurface,
        );
        expect(find.text('Beta'), findsOneWidget);
        expect(find.text('其他组件'), findsOneWidget);
        final searchRect = tester.getRect(find.byType(TextField));
        await tester.enterText(find.byType(TextField), 'aLpHa');
        await tester.pumpAndSettle();
        expect(find.text('Alpha'), findsOneWidget);
        expect(find.text('Beta'), findsNothing);
        expect(find.text('2 份许可'), findsOneWidget);
        expect(tester.getRect(find.byType(TextField)), searchRect);
        await tester.tap(find.text('Alpha'));
        await tester.pumpAndSettle();
        for (final original in [
          'Shared copyright',
          'Shared terms.',
          'Separate copyright',
          'Separate terms.',
        ]) {
          expect(find.text(original), findsOneWidget);
        }
        await tester.tap(find.byTooltip('返回').last);
        await tester.pumpAndSettle();
        await tester.enterText(find.byType(TextField), 'missing-component');
        await tester.pumpAndSettle();
        expect(find.text('未找到匹配组件'), findsOneWidget);
        expect(tester.getRect(find.byType(TextField)), searchRect);
        await tester.tap(find.byTooltip('清除查找'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Beta'));
        await tester.pumpAndSettle();
        expect(find.text('Shared terms.'), findsOneWidget);
        expect(find.text('Separate terms.'), findsNothing);
        expect(tester.takeException(), isNull);
      },
    );
  }
}
