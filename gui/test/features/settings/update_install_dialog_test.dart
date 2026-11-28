import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/settings/update_settings.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/package_update_checker.dart';
import 'package:sidravia_gui/shared/localization/app_locale.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import '../../support/capture_fonts.dart' as fonts;

const _capture = String.fromEnvironment('SIDRAVIA_UPDATE_DIALOG_CAPTURE');
final _notes = List.generate(
  32,
  (index) => '${index + 1}. 改善连接中断后的恢复。',
).join('\n');

class _Checker implements PackageUpdateChecker {
  @override
  bool get supportsUpdateCheck => true;
  @override
  bool get usesSystemPackageManager => false;

  final install = Completer<void>();
  final installedCandidates = <String>[];

  @override
  Future<PackageUpdate> checkUpdate() async => PackageUpdate(
    state: UpdateCheckState.available,
    version: '1.2.3',
    notes: _notes,
    installCandidateId: '2:22',
  );

  @override
  Future<void> installUpdate(String candidateId) {
    installedCandidates.add(candidateId);
    return install.future;
  }

  @override
  Future<void> openReleasePage(String version) async {}
}

void main() {
  if (_capture.isNotEmpty) {
    setUpAll(() => fonts.loadCaptureFonts(forCapture: true));
  }
  for (final (width, height, dark, scale) in [
    (360.0, 480.0, true, 1.3),
    (960.0, 700.0, false, 1.0),
  ]) {
    testWidgets(
      'update notes, cancellation, and exact confirmation at $width',
      (tester) async {
        tester.view.devicePixelRatio = 1;
        tester.view.physicalSize = Size(width, height);
        addTearDown(tester.view.resetDevicePixelRatio);
        addTearDown(tester.view.resetPhysicalSize);
        final scene = GlobalKey();
        final checker = _Checker();
        await tester.pumpWidget(
          RepaintBoundary(
            key: scene,
            child: MaterialApp(
              locale: AppLocale.locale,
              supportedLocales: AppLocale.supportedLocales,
              localizationsDelegates: AppLocale.delegates,
              debugShowCheckedModeBanner: false,
              theme: dark ? AppTheme.dark() : AppTheme.light(),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: Scaffold(body: UpdateSettings(checker: checker)),
            ),
          ),
        );
        await tester.tap(find.text('检查更新'));
        await tester.pumpAndSettle();
        expect(find.text('1.2.3'), findsOneWidget);
        expect(find.text(_notes), findsOneWidget);
        expect(find.text('下载并安装更新').hitTestable(), findsOneWidget);
        expect(checker.installedCandidates, isEmpty);
        expect(tester.takeException(), isNull);
        if (_capture.isNotEmpty) {
          await tester.runAsync(() async {
            final boundary =
                scene.currentContext!.findRenderObject()
                    as RenderRepaintBoundary;
            final image = await boundary.toImage();
            try {
              final data = (await image.toByteData(
                format: ui.ImageByteFormat.png,
              ))!;
              await Directory(_capture).create(recursive: true);
              await File('$_capture/update-${width.toInt()}.png')
                  .writeAsBytes(data.buffer.asUint8List());
            } finally {
              image.dispose();
            }
          });
        }
        await tester.drag(
          find.byType(SingleChildScrollView).last,
          const Offset(0, -200),
        );
        await tester.pumpAndSettle();
        expect(find.text('下载并安装更新').hitTestable(), findsOneWidget);
        await tester.tap(find.text('取消'));
        await tester.pumpAndSettle();
        expect(find.byType(AlertDialog), findsNothing);
        expect(checker.installedCandidates, isEmpty);

        await tester.tap(find.text('检查更新'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('下载并安装更新'));
        await tester.pumpAndSettle();
        expect(find.byType(AlertDialog), findsNothing);
        expect(checker.installedCandidates, ['2:22']);
        expect(find.text('正在下载并安装更新…'), findsOneWidget);
        expect(
          tester
              .widget<OutlinedButton>(
                find.widgetWithText(OutlinedButton, '检查更新'),
              )
              .onPressed,
          isNull,
        );
        if (dark) {
          checker.install.completeError(
            const IntegrationFailure('候选版本已变化，请重新检查更新。'),
          );
        } else {
          checker.install.complete();
        }
        await tester.pumpAndSettle();
        expect(
          find.text(dark ? '候选版本已变化，请重新检查更新' : '更新器已接管，请等待更新完成'),
          findsOneWidget,
        );
      },
      variant: TargetPlatformVariant({TargetPlatform.linux}),
    );
  }
}
