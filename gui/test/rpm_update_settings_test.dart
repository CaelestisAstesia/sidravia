import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';
import 'package:sidravia_gui/features/settings/update_settings.dart';
import 'package:sidravia_gui/platform/integration_failure.dart';
import 'package:sidravia_gui/platform/update/package_update_checker.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

import 'support/capture_fonts.dart' as fonts;
import 'support/installation_manager_fixture.dart' show FakeManager;

const _capture = String.fromEnvironment('GEN_FRONTEND_OUTPUT');

class _UpdateManager extends FakeManager implements PackageUpdateChecker {
  @override
  bool supportsUpdateCheck = false;
  @override
  bool usesSystemPackageManager = true;
  var pendingCheck = Completer<PackageUpdate>();
  String? openedVersion;
  int checks = 0;

  @override
  Future<PackageUpdate> checkUpdate() {
    checks++;
    return pendingCheck.future;
  }

  @override
  Future<void> openReleasePage(String version) async {
    openedVersion = version;
  }
}

void main() {
  testWidgets('portable update check omits the package-manager hint', (
    tester,
  ) async {
    final manager = _UpdateManager()..usesSystemPackageManager = false;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: UpdateSettings(checker: manager)),
      ),
    );
    expect(find.text('检查更新'), findsOneWidget);
    expect(find.text('通过系统包管理器安装更新'), findsNothing);
  });

  for (final width in [360.0, 960.0]) {
    for (final dark in [false, true]) {
      testWidgets('RPM update check in settings at $width dark=$dark', (
        tester,
      ) async {
        await tester.binding.setSurfaceSize(Size(width, 760));
        addTearDown(() => tester.binding.setSurfaceSize(null));
        if (_capture.isNotEmpty) {
          await tester.runAsync(() => fonts.loadCaptureFonts(forCapture: true));
        }
        final manager = _UpdateManager();
        final controller = GuiController(
          bootstrapper: OfflineDemoBootstrap(),
          connector: (_) async => OfflineDemoClient(),
        );
        addTearDown(controller.dispose);
        final captureKey = GlobalKey();
        Widget page() => MaterialApp(
          theme: dark ? AppTheme.dark() : AppTheme.light(),
          home: RepaintBoundary(
            key: captureKey,
            child: Scaffold(
              body: SettingsPage(
                controller: controller,
                onNavigate: (_) {},
                onBack: () {},
                createIntegrationManager: () => manager,
              ),
            ),
          ),
        );
        await tester.pumpWidget(page());
        await tester.pumpAndSettle();
        expect(find.text('软件更新'), findsNothing);

        manager.supportsUpdateCheck = true;
        await tester.pumpWidget(page());
        await tester.pumpAndSettle();
        final check = find.widgetWithText(OutlinedButton, '检查更新');
        expect(find.text('通过系统包管理器安装更新'), findsOneWidget);
        await tester.ensureVisible(check);
        final activate = tester.widget<OutlinedButton>(check).onPressed!;
        await tester.tap(check);
        activate();
        await tester.pumpAndSettle();
        expect(find.text('正在检查…'), findsOneWidget);
        expect(tester.widget<OutlinedButton>(check).onPressed, isNull);
        expect(manager.checks, 1);
        manager.pendingCheck.complete((
          state: UpdateCheckState.available,
          version: '0.6.0-alpha.1',
        ));
        await tester.pumpAndSettle();
        expect(find.text('发现新版本 0.6.0-alpha.1'), findsOneWidget);
        final release = find.text('查看发行与下载');
        await tester.ensureVisible(release);
        await tester.tap(release);
        await tester.pumpAndSettle();
        expect(manager.openedVersion, '0.6.0-alpha.1');
        expect(tester.takeException(), isNull);

        if (_capture.isNotEmpty) {
          await tester.runAsync(() async {
            final boundary =
                captureKey.currentContext!.findRenderObject()
                    as RenderRepaintBoundary;
            final image = await boundary.toImage();
            try {
              final data = (await image.toByteData(
                format: ui.ImageByteFormat.png,
              ))!;
              await Directory(_capture).create(recursive: true);
              await File(
                '$_capture/rpm-update-${width.toInt()}-${dark ? 'dark' : 'light'}.png',
              ).writeAsBytes(data.buffer.asUint8List());
            } finally {
              image.dispose();
            }
          });
        }

        manager.pendingCheck = Completer<PackageUpdate>();
        await tester.ensureVisible(check);
        await tester.tap(check);
        await tester.pump();
        manager.pendingCheck.completeError(
          const IntegrationFailure('更新来源暂时不可用。'),
        );
        await tester.pumpAndSettle();
        expect(find.text('更新来源暂时不可用'), findsOneWidget);
        expect(release, findsNothing);

        for (final entry in {
          UpdateCheckState.upToDate: '当前已是最新版本',
          UpdateCheckState.aheadOfChannel: '当前版本高于此通道已发布的版本',
          UpdateCheckState.noCompatibleRelease: '未找到适用于此安装的发行包',
        }.entries) {
          manager.pendingCheck = Completer<PackageUpdate>();
          await tester.ensureVisible(check);
          await tester.tap(check);
          manager.pendingCheck.complete((state: entry.key, version: null));
          await tester.pumpAndSettle();
          expect(find.text(entry.value), findsOneWidget);
          expect(release, findsNothing);
        }
        await tester.pumpWidget(const SizedBox());
        await tester.pumpAndSettle();
        expect(manager.closed, isTrue);
      }, variant: TargetPlatformVariant({TargetPlatform.linux}));
    }
  }
}
