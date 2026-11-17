import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/dev/interactive_demo_main.dart';

import 'dart:convert';

import 'home_responsive_test.dart' as fonts;
import 'school_announcement_controller_test.dart' as owners;
import 'support/runtime_snapshot.dart';

import 'package:sidravia_gui/shared/theme/app_theme.dart';

import 'package:sidravia_gui/licensing/source_code_pro_font_license.dart';

const requestFile = String.fromEnvironment('GUI_CAPTURE_REQUEST');
late String out;
late DateTime fixedTime;
late String currentFrame;
late List<String> ordinaryFamilies;
final captured = <Map<String, Object?>>[];

Future<void> tap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f.last);
  await t.pumpAndSettle();
  await t.tap(f.last);
  await t.pumpAndSettle();
}

Future<void> back(WidgetTester t) => tap(t, find.byTooltip('返回'));
Future<void> shot(WidgetTester t, String name) async {
  await t.pumpAndSettle();
  expect(t.takeException(), isNull, reason: name);
  expect(
    fixedTime.toLocal().timeZoneOffset,
    Duration.zero,
    reason: 'Capture requires actual UTC local-time conversion',
  );
  String? capturedAt;
  if (find
      .byKey(const ValueKey('runtime-snapshot-text'))
      .evaluate()
      .isNotEmpty) {
    capturedAt = displayedSnapshot(t)['capturedAt'] as String;
    expect(capturedAt, fixedTime.toUtc().toIso8601String());
  }
  final b = t.renderObject<RenderRepaintBoundary>(
    find.byKey(const ValueKey('capture-root')),
  );
  await t.runAsync(() async {
    final i = await b.toImage(pixelRatio: 1);
    final d = await i.toByteData(format: ui.ImageByteFormat.png);
    await File('$out/$name.png').writeAsBytes(d!.buffer.asUint8List());
    i.dispose();
    captured.add({
      'id': name,
      'file': '$name.png',
      'frame': currentFrame,
      'fontFamilies': ordinaryFamilies,
      'capturedAt': capturedAt,
      'timezoneOffsetSeconds': fixedTime.toLocal().timeZoneOffset.inSeconds,
    });
  });
}

void main() {
  if (requestFile.isEmpty) {
    test(
      'real-page capture is explicit',
      () {},
      skip: 'Use dev visual capture',
    );
    return;
  }
  final request = jsonDecode(File(requestFile).readAsStringSync()) as Map;
  out = request['output'] as String;
  fixedTime = DateTime.parse(request['fixedTime'] as String);
  setUpAll(() async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('sidravia/window'), (
          call,
        ) async {
          if (call.method == 'configureFrame') return {'customFrame': true};
          return null;
        });
    await fonts.loadCaptureFonts(forCapture: true);
    await (FontLoader('Source Code Pro')
          ..addFont(rootBundle.load('assets/fonts/SourceCodePro-Regular.otf')))
        .load();
    final sdk = jsonDecode(
      File(request['sdkInfo'] as String).readAsStringSync(),
    ) as Map;
    final license = await File('${sdk['flutterRoot']}/LICENSE').readAsString();
    registerSourceCodeProFontLicense();
    LicenseRegistry.addLicense(() async* {
      yield LicenseEntryWithLineBreaks(['Flutter'], license);
    });
    // Resolve the unchanged real license entries outside widget FakeAsync;
    // cached rootBundle futures must not retain a previous test's async zone.
    final entries = await LicenseRegistry.licenses.toList();
    LicenseRegistry.reset();
    LicenseRegistry.addLicense(() => Stream.fromIterable(entries));
  });
  for (final raw in request['frames'] as List) {
    final frame = raw as Map;
    final size = Size(
      (frame['width'] as num).toDouble(),
      (frame['height'] as num).toDouble(),
    );
    final dark = frame['dark'] as bool;
    final scale = (frame['scale'] as num).toDouble();
    final prefix = frame['id'] as String;
    testWidgets(
      'capture $prefix actual pages and controls',
      (t) async {
        currentFrame = prefix;
        const manifestPath = String.fromEnvironment('SYSTEM_FONT_MANIFEST');
        final configured =
            jsonDecode(File(manifestPath).readAsStringSync()) as Map;
        final text = AppTheme.light().textTheme.bodyMedium!;
        expect(
          configured.containsKey(text.fontFamily),
          isTrue,
          reason: 'Font manifest must cover the actual platform ordinary font',
        );
        ordinaryFamilies = [
          text.fontFamily!,
          ...?text.fontFamilyFallback,
        ].where(configured.containsKey).toSet().toList();
        await t.binding.setSurfaceSize(size);
        addTearDown(() => t.binding.setSurfaceSize(null));
        t.platformDispatcher.textScaleFactorTestValue = scale;
        addTearDown(t.platformDispatcher.clearTextScaleFactorTestValue);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetDevicePixelRatio);
        try {
          await t.pumpWidget(
            RepaintBoundary(
              key: const ValueKey('capture-root'),
              child: OfflineDemoApp(fixedTime: fixedTime),
            ),
          );
          await t.pumpAndSettle();
          await tap(t, find.byTooltip('演示外观'));
          await tap(t, find.text(dark ? '深色' : '浅色'));
          await shot(t, '$prefix-01-home');
          await tap(t, find.byTooltip('演示场景'));
          await tap(t, find.text('已连接 · 账户业务'));
          await shot(t, '$prefix-02-home-business');
          await tap(t, find.text('查看其余 1 条消息'));
          await shot(t, '$prefix-03-messages');
          await tap(t, find.text('公告'));
          await shot(t, '$prefix-26-announcement');
          await tap(t, find.text('关闭'));
          await tap(t, find.byKey(const ValueKey('home-configuration-button')));
          await shot(t, '$prefix-04-configuration');
          await tap(t, find.text('删除登录配置'));
          await shot(t, '$prefix-20-delete-confirm');
          await tap(t, find.text('取消'));
          await tap(t, find.text('机构'));
          await shot(t, '$prefix-05-institution');
          await back(t);
          await tap(t, find.text('网卡'));
          await shot(t, '$prefix-06-network');
          await back(t);
          await tap(t, find.text('更改'));
          await t.enterText(find.byType(TextField).last, 'preview-only');
          await shot(t, '$prefix-07-password-hidden');
          await tap(t, find.byTooltip('显示密码'));
          await shot(t, '$prefix-08-password-visible');
          await tap(t, find.text('取消更改'));
          await t.enterText(find.byType(TextField).first, 'preview-account');
          await t.pumpAndSettle();
          await tap(t, find.text('保存修改'));
          await shot(t, '$prefix-09-save-confirm');
          await tap(t, find.text('取消'));
          await back(t);
          await shot(t, '$prefix-10-discard-confirm');
          await tap(t, find.text('放弃修改'));
          await tap(t, find.byTooltip('设置'));
          await shot(t, '$prefix-11-settings-top');
          await t.ensureVisible(find.text('外观模式'));
          await t.pumpAndSettle();
          await tap(t, find.byKey(const ValueKey('appearance-selector')));
          await shot(t, '$prefix-12-settings-menu');
          await t.sendKeyEvent(LogicalKeyboardKey.escape);
          await t.pumpAndSettle();
          await t.ensureVisible(find.text('关于 Sidravia'));
          await t.pumpAndSettle();
          await shot(t, '$prefix-13-settings-bottom');
          await tap(t, find.text('关于 Sidravia'));
          await shot(t, '$prefix-14-about');
          await tap(t, find.text('许可'));
          await shot(t, '$prefix-15-licenses');
          await t.enterText(find.byType(TextField).last, 'not-a-component');
          await t.pumpAndSettle();
          await shot(t, '$prefix-22-license-no-results');
          await t.enterText(find.byType(TextField).last, '');
          await t.pumpAndSettle();
          await tap(t, find.text('Flutter'));
          await shot(t, '$prefix-16-license-detail');
          await back(t);
          await back(t);
          await back(t);
          await tap(t, find.text('连接与诊断'));
          await shot(t, '$prefix-17-diagnostics-top');
          await t.enterText(find.byType(TextField).last, 'frontend');
          await t.pumpAndSettle();
          await shot(t, '$prefix-21-snapshot-search');
          await t.enterText(find.byType(TextField).last, '');
          await t.pumpAndSettle();
          await t.ensureVisible(find.text('移除当前会话'));
          await t.pumpAndSettle();
          await shot(t, '$prefix-18-diagnostics-bottom');
          await tap(t, find.text('重启核心'));
          await shot(t, '$prefix-23-restart-control');
          if (find.text('取消').evaluate().isNotEmpty) {
            await tap(t, find.text('取消'));
          }
          await tap(t, find.text('移除当前会话'));
          await shot(t, '$prefix-24-remove-session-confirm');
          await tap(t, find.text('取消'));
          await back(t);
          await back(t);
          await tap(t, find.byTooltip('演示场景'));
          await tap(t, find.text('尚未配置'));
          await tap(t, find.text('添加配置'));
          await shot(t, '$prefix-19-first-configuration');
          await t.enterText(find.byType(TextField).first, 'invalid user');
          await t.enterText(find.byType(TextField).last, 'fixture-only');
          await t.pumpAndSettle();
          await tap(t, find.text('保存修改'));
          await shot(t, '$prefix-27-validation');
          await back(t);
          await tap(t, find.text('放弃修改'));
          await tap(t, find.byTooltip('演示场景'));
          await tap(t, find.text('认证失败'));
          await shot(t, '$prefix-28-home-error');
        } finally {
          await owners.closeShellOwners(t);
          await t.pumpWidget(const SizedBox.shrink());
        }
      },
      variant: TargetPlatformVariant({
        TargetPlatform.values.byName(request['platform'] as String),
      }),
    );
  }
  tearDownAll(() async {
    await File('$out/cases.json')
        .writeAsString(jsonEncode({'schema': 1, 'cases': captured}));
  });
}
