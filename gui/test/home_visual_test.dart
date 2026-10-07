import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/rendering.dart';

import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_feed.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/dev/preview_main.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'connected reference fixture renders the production home surface',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      tester.platformDispatcher.textScaleFactorTestValue = 1;
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
      await tester.binding.setSurfaceSize(const Size(398, 642));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      await tester.runAsync(() async {
        final loader = FontLoader('HarmonyOS Sans')
          ..addFont(
            rootBundle.load('assets/fonts/HarmonyOS_Sans_SC_Regular.ttf'),
          )
          ..addFont(
            rootBundle.load('assets/fonts/HarmonyOS_Sans_SC_Medium.ttf'),
          )
          ..addFont(rootBundle.load('assets/fonts/HarmonyOS_Sans_SC_Bold.ttf'));
        await loader.load();
        final icons = FontLoader('MaterialIcons')
          ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
        await icons.load();
        final symbolPath = const String.fromEnvironment(
          'HOME_SYMBOL_FONT',
          defaultValue: '/mnt/c/Windows/Fonts/seguisym.ttf',
        );
        if (await File(symbolPath).exists()) {
          final symbols = FontLoader('Segoe UI Symbol')
            ..addFont(
              File(symbolPath)
                  .readAsBytes()
                  .then((bytes) => ByteData.sublistView(bytes)),
            );
          await symbols.load();
        } else if (const bool.fromEnvironment('GENERATE_HOME_CANDIDATE')) {
          throw StateError(
            'HOME_SYMBOL_FONT must point to the existing Segoe UI Symbol font for candidate generation',
          );
        }
      });
      await tester.pumpWidget(
        MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: AppTheme.light().copyWith(platform: TargetPlatform.windows),
          home: const Scaffold(
            body: SizedBox.expand(
              child: RepaintBoundary(
                key: ValueKey('capture'),
                child: ColoredBox(
                  color: AppColors.bgLight,
                  child: HomeFixture(),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('已连接'), findsOneWidget);
      expect(find.text('校园网维护安排'), findsOneWidget);
      expect(find.byType(HomeNoticeView), findsOneWidget);
      final measurements = <String, Object>{};
      for (final key in [
        'home-header',
        'home-status',
        'home-mark',
        'home-context',
        'home-actions',
        'home-divider',
        'home-notice',
      ]) {
        final rect = tester.getRect(find.byKey(ValueKey(key)));
        measurements[key] = {
          'x': rect.left,
          'y': rect.top,
          'width': rect.width,
          'height': rect.height,
        };
      }
      for (final entry in {
        'institution': '吉林大学',
        'username': 'student01',
        'status-title': '已连接',
        'status-detail': '认证成功 · 已连接 2 小时 18 分钟',
        'notice-meta': '校园网公告',
        'notice-title': '校园网维护安排',
        'notice-description': '点击查看公告页面',
      }.entries) {
        final rect = tester.getRect(find.text(entry.value));
        measurements[entry.key] = {
          'x': rect.left,
          'y': rect.top,
          'width': rect.width,
          'height': rect.height,
        };
      }
      expect(
        tester.getSize(find.byKey(const ValueKey('home-mark'))),
        const Size(44, 44),
      );
      expect(
        tester.getSize(find.byKey(const ValueKey('home-actions'))).width,
        320,
      );
      if (const bool.fromEnvironment('GENERATE_HOME_CANDIDATE')) {
        final boundary = tester.renderObject<RenderRepaintBoundary>(
          find.byKey(const ValueKey('capture')),
        );
        await tester.runAsync(() async {
          final image = await boundary.toImage(pixelRatio: 1);
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          await Directory('test/candidates').create(recursive: true);
          await File('test/candidates/flutter-content.png')
              .writeAsBytes(bytes!.buffer.asUint8List());
          await File('test/candidates/flutter-measurements.json')
              .writeAsString(jsonEncode(measurements));
          image.dispose();
        });
      }
    },
  );

  testWidgets('formal HomePage maps controller.notice and existing action', (
    tester,
  ) async {
    final client = _Client();
    final controller = GuiController(
      bootstrapper: _Bootstrapper(),
      connector: (_) async => client,
      pollDelay: const Duration(days: 1),
    );
    await controller.start();
    client.failStop = true;
    await tester.pumpWidget(
      MaterialApp(
        home: HomePage(controller: controller, onNavigate: (_) {}),
      ),
    );
    expect(find.text('当前状态无法执行此操作。'), findsNothing);
    await tester.tap(find.text('断开连接'));
    await tester.pump();
    expect(find.text('当前状态无法执行此操作。'), findsOneWidget);
    expect(client.stopCalls, 1);
    client.failStop = false;
    await tester.tap(find.text('断开连接'));
    await tester.pump();
    expect(client.stopCalls, 2);
    expect(find.text('当前状态无法执行此操作。'), findsNothing);
    controller.dispose();
  });

  testWidgets(
    'shared home announcement invokes the supplied preview callback',
    (tester) async {
      var calls = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: HomeNoticeView(
            notice: HomeNotice(title: '校园网维护安排', onOpen: () => calls++),
          ),
        ),
      );
      await tester.tap(find.byType(HomeNoticeView));
      expect(calls, 1);
    },
  );

  testWidgets(
    'formal HomePage responds when an announcement arrives and updates',
    (tester) async {
      final now = DateTime.utc(2026, 10, 3, 12);
      final fetcher = _AnnouncementFetcher([
        _announcementResult(now, 'maintenance-a', '校园网维护安排'),
        _announcementResult(now, 'maintenance-b', '校园网升级安排'),
        _emptyAnnouncements(now),
      ]);
      final announcements = AnnouncementController(
        endpoint: Uri.parse('https://notices.example.edu/feed.json'),
        store: MemoryAnnouncementStore(),
        fetcher: fetcher,
        nowUtc: () => now,
        refreshInterval: Duration.zero,
      );
      final controller = _HomeController();
      await tester.pumpWidget(
        MaterialApp(
          home: HomePage(
            controller: controller,
            announcements: announcements,
            onNavigate: (_) {},
          ),
        ),
      );
      expect(find.text('校园网维护安排'), findsNothing);
      await announcements.start();
      await tester.pump();
      expect(find.text('校园网维护安排'), findsOneWidget);
      await announcements.refreshIfDue();
      await tester.pump();
      expect(find.text('校园网升级安排'), findsOneWidget);
      expect(fetcher.calls, 2);
      expect(find.byType(HomeNoticeView), findsOneWidget);
      expect(announcements.unreadCount, 1);
      await tester.tap(find.byType(HomeNoticeView));
      await tester.pumpAndSettle();
      expect(announcements.unreadCount, 0);
      expect(find.text('公告正文'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpWidget(
        MaterialApp(
          home: HomePage(
            controller: controller,
            announcements: announcements,
            onNavigate: (_) {},
          ),
        ),
      );
      await announcements.refreshIfDue();
      await tester.pump();
      expect(find.byType(HomeNoticeView), findsNothing);
      expect(find.byKey(const ValueKey('home-divider')), findsNothing);
      announcements.dispose();
      controller.dispose();
    },
  );
}

AnnouncementFetchResult _announcementResult(
  DateTime now,
  String id,
  String title,
) {
  final raw = jsonEncode({
    'schema': 1,
    'generatedAt': now.toIso8601String(),
    'items': [
      {
        'id': id,
        'revision': 1,
        'level': 'maintenance',
        'title': title,
        'body': '公告正文',
        'publishedAt': now.subtract(const Duration(hours: 1)).toIso8601String(),
        'startsAt': now.subtract(const Duration(minutes: 1)).toIso8601String(),
        'expiresAt': now.add(const Duration(days: 1)).toIso8601String(),
      },
    ],
  });
  return AnnouncementFetchResult.updated(
    feed: AnnouncementDocument.decode(
      raw,
      endpoint: Uri.parse('https://notices.example.edu/feed.json'),
    ),
    rawBody: raw,
  );
}

class _HomeController extends GuiController {
  _HomeController() : super(bootstrapper: const UnsupportedGuiBootstrapper());
  @override
  GuiConnectionState get state => GuiConnectionState.ready;
  @override
  GuiCapabilities get capabilities => GuiCapabilities(
    state: GuiConnectionState.ready,
    snapshot: _homeSnapshot,
    busy: false,
  );
}

final _homeSnapshot = GuiSnapshot(
  daemon: const DaemonStatus(
    productVersion: 'fixture',
    buildId: 'fixture',
    pid: 1,
    status: 'running',
    mode: 'desktop',
  ),
  profiles: const [
    InstitutionProfile(
      id: 'jlu',
      displayName: '吉林大学',
      protocolId: 'drcom-5.2.0-d',
    ),
  ],
  configurations: const [
    ConfigurationSummary(
      id: 'cfg-a',
      displayName: '吉林大学',
      institutionProfileId: 'jlu',
      institutionDisplayName: '吉林大学',
      authenticationProtocolId: 'drcom-5.2.0-d',
      username: 'student01',
      credentialStored: true,
      storageProtection: 'protected',
    ),
  ],
  sessions: [
    SessionSummary(
      id: 'session-a',
      displayName: '吉林大学',
      accountName: 'student01',
      state: SessionState.authenticated,
      intent: SessionIntent.maintainAuthentication,
      configurationId: 'cfg-a',
      selectedNetworkBinding: SessionNetworkBinding(
        interfaceId: 'ethernet',
        displayName: '以太网',
        localIpv4Address: '192.168.1.20',
      ),
    ),
  ],
);

final class _AnnouncementFetcher implements AnnouncementFetcher {
  _AnnouncementFetcher(this.outcomes);
  final List<AnnouncementFetchResult> outcomes;
  var calls = 0;

  @override
  Future<AnnouncementFetchResult> fetch({
    String? etag,
    String? lastModified,
  }) async {
    calls++;
    return outcomes.removeAt(0);
  }

  @override
  void close() {}
}

class _Bootstrapper implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
      token: 'fixture-only',
      productVersion: 'fixture',
      buildId: 'fixture',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );
}

class _Client implements SidraviaIpcClient {
  bool failStop = false;
  int stopCalls = 0;
  @override
  Future<DaemonStatus> daemonStatus() async => _homeSnapshot.daemon;
  @override
  Future<List<InstitutionProfile>> profileList() async =>
      _homeSnapshot.profiles;
  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      _homeSnapshot.configurations;
  @override
  Future<List<SessionSummary>> sessionList() async => _homeSnapshot.sessions;
  @override
  Future<SessionSummary> sessionStop(String sessionId) async {
    expect(sessionId, 'session-a');
    stopCalls++;
    if (failStop) throw const IpcRequestFailure('session_state_conflict');
    return _homeSnapshot.sessions.single;
  }

  @override
  Future<void> close() async {}
  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw StateError('unexpected client call: ${invocation.memberName}');
}

AnnouncementFetchResult _emptyAnnouncements(DateTime now) {
  final raw = jsonEncode({
    'schema': 1,
    'generatedAt': now.toIso8601String(),
    'items': <Object>[],
  });
  return AnnouncementFetchResult.updated(
    feed: AnnouncementDocument.decode(
      raw,
      endpoint: Uri.parse('https://notices.example.edu/feed.json'),
    ),
    rawBody: raw,
  );
}
