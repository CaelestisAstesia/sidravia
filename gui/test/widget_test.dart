import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/app/sidravia_app.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

void main() {
  test('compact text scaling uses two semantic limits', () {
    expect(SidraviaLayout.compactChromeMaxTextScale, 1.3);
    expect(SidraviaLayout.compactContentMaxTextScale, 1.6);
  });

  testWidgets('wide shell uses selected sidebar navigation', (tester) async {
    await tester.binding.setSurfaceSize(const Size(1280, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(_app());
    await tester.pump();

    final sidebar = find.byKey(const ValueKey<String>('wide-sidebar'));
    expect(sidebar, findsOneWidget);
    expect(find.byKey(const ValueKey<String>('compact-header')), findsNothing);
    expect(find.byType(NavigationBar), findsNothing);
    expect(tester.getSize(sidebar).width, 220);
    final brand = tester.widget<Text>(
      find.descendant(of: sidebar, matching: find.text('Sidravia')),
    );
    expect(brand.textAlign, TextAlign.center);
    expect(brand.style?.fontWeight, FontWeight.w700);
    final homeButton = tester.widget<TextButton>(
      find.descendant(
        of: find.byKey(const ValueKey<String>('destination-home')),
        matching: find.byType(TextButton),
      ),
    );
    expect(homeButton.style?.alignment, Alignment.center);
    for (final destination in appDestinations) {
      expect(find.text(destination.label), findsWidgets);
    }
    expect(
      tester
          .getRect(find.byKey(const ValueKey<String>('destination-home')))
          .right,
      lessThanOrEqualTo(tester.getRect(sidebar).right),
    );
    expect(
      tester.getRect(find.text('校园网')).left,
      greaterThan(tester.getRect(sidebar).right + 40),
    );
    expect(
      tester
          .getSemantics(find.byKey(const ValueKey<String>('destination-home')))
          .flagsCollection
          .isSelected,
      ui.Tristate.isTrue,
    );
    expect(
      tester
          .getSemantics(
            find.byKey(const ValueKey<String>('destination-configuration')),
          )
          .flagsCollection
          .isSelected,
      ui.Tristate.isFalse,
    );

    await tester.tap(find.descendant(of: sidebar, matching: find.text('配置')));
    await tester.pumpAndSettle();

    expect(find.text('fixture-user'), findsWidgets);
    final widePassword = tester.widget<TextField>(
      find.byKey(const ValueKey('configuration-password')),
    );
    expect(widePassword.decoration?.labelText, '新密码（可选）');
    expect(widePassword.decoration?.helperText, isNull);
    expect(
      tester
          .getSemantics(
            find.byKey(const ValueKey<String>('destination-configuration')),
          )
          .flagsCollection
          .isSelected,
      ui.Tristate.isTrue,
    );
  });

  testWidgets('Windows frame exposes direct minimize and close controls', (
    tester,
  ) async {
    final calls = <MethodCall>[];
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(SidraviaWindowCommands.channel, (
      call,
    ) async {
      calls.add(call);
      return null;
    });
    addTearDown(
      () => messenger.setMockMethodCallHandler(
        SidraviaWindowCommands.channel,
        null,
      ),
    );

    await tester.pumpWidget(
      const MaterialApp(
        home: SidraviaWindowFrame(
          platform: TargetPlatform.windows,
          child: ColoredBox(color: Colors.white),
        ),
      ),
    );

    final titleBar = find.byKey(const ValueKey<String>('windows-title-bar'));
    final dragRegion = find.byKey(
      const ValueKey<String>('windows-drag-region'),
    );
    expect(tester.getSize(titleBar).height, SidraviaWindowFrame.titleBarHeight);
    expect(
      tester.getSize(dragRegion).width,
      tester.getSize(titleBar).width - SidraviaWindowFrame.controlWidth * 2,
    );
    expect(find.byTooltip('最小化'), findsOneWidget);
    expect(find.byTooltip('关闭'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey<String>('window-minimize')));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey<String>('window-close')));
    await tester.pump();

    expect(calls.map((call) => call.method), ['minimize', 'close']);
  });

  testWidgets('narrow shell uses bottom navigation and switches sections', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(_app());
    await tester.pump();

    expect(find.byKey(const ValueKey<String>('wide-sidebar')), findsNothing);
    expect(
      find.byKey(const ValueKey<String>('compact-header')),
      findsOneWidget,
    );
    expect(find.byType(NavigationBar), findsOneWidget);
    await tester.tap(find.text('关于'));
    await tester.pumpAndSettle();

    expect(find.text('校园网认证工具'), findsOneWidget);
    expect(
      tester.widget<MaterialApp>(find.byType(MaterialApp)).title,
      'Sidravia',
    );
  });

  testWidgets('selection survives a responsive layout change', (tester) async {
    await tester.binding.setSurfaceSize(const Size(390, 844));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(_app());
    await tester.pump();
    await tester.tap(
      find.descendant(
        of: find.byType(NavigationBar),
        matching: find.text('配置'),
      ),
    );
    await tester.pumpAndSettle();
    await tester.binding.setSurfaceSize(const Size(1024, 720));
    await tester.pumpAndSettle();

    expect(find.text('fixture-user'), findsWidgets);
    expect(find.byKey(const ValueKey<String>('wide-sidebar')), findsOneWidget);
    expect(find.byKey(const ValueKey<String>('compact-header')), findsNothing);
    expect(find.byType(NavigationBar), findsNothing);
  });

  testWidgets(
    'compact header keeps one destination title without brand competition',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(220, 360));
      tester.binding.platformDispatcher.textScaleFactorTestValue = 2;
      addTearDown(() {
        tester.binding.platformDispatcher.clearTextScaleFactorTestValue();
        tester.binding.setSurfaceSize(null);
      });

      await tester.pumpWidget(_app());
      await tester.pump();

      expect(tester.takeException(), isNull);
      expect(
        find.byKey(const ValueKey<String>('compact-header')),
        findsOneWidget,
      );
      final header = find.byKey(const ValueKey<String>('compact-header'));
      final theme = Theme.of(tester.element(header));
      expect(
        find.descendant(of: header, matching: find.text('连接')),
        findsOneWidget,
      );
      expect(
        tester
            .widget<Text>(
              find.descendant(of: header, matching: find.text('连接')),
            )
            .style
            ?.fontSize,
        theme.textTheme.titleSmall?.fontSize,
      );
      expect(
        _effectiveTextScale(
          tester,
          find.descendant(of: header, matching: find.text('连接')),
        ),
        closeTo(SidraviaLayout.compactChromeMaxTextScale, 0.001),
      );
      expect(
        find.descendant(of: header, matching: find.text('Sidravia')),
        findsNothing,
      );
    },
  );

  testWidgets(
    'home copy distinguishes ready authentication, stale, and failure',
    (tester) async {
      final authenticated = GuiController(
        bootstrapper: _Bootstrapper(),
        connector: (_) async => _Client(sessions: const [_authenticated]),
        pollDelay: const Duration(days: 1),
      );
      await authenticated.start();
      await tester.pumpWidget(
        MaterialApp(
          home: HomePage(controller: authenticated, onOpenConfiguration: () {}),
        ),
      );
      expect(find.text('已连接'), findsOneWidget);
      expect(find.text('注销'), findsOneWidget);
      authenticated.dispose();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();

      final failing = GuiController(
        bootstrapper: _FailureBootstrapper(),
        pollDelay: const Duration(days: 1),
      );
      await failing.start();
      await tester.pumpWidget(
        MaterialApp(
          home: HomePage(controller: failing, onOpenConfiguration: () {}),
        ),
      );
      expect(find.text('服务不可用'), findsOneWidget);
      expect(find.text('重试'), findsOneWidget);
      failing.dispose();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();

      final stale = GuiController(
        bootstrapper: _SequencedBootstrapper(),
        connector: (_) async => _Client(),
        pollDelay: const Duration(days: 1),
      );
      await stale.start();
      await stale.retry();
      await tester.pumpWidget(
        MaterialApp(
          home: HomePage(controller: stale, onOpenConfiguration: () {}),
        ),
      );
      expect(find.text('状态已过期'), findsOneWidget);
      expect(find.text('无法获取最新连接状态。'), findsOneWidget);
      stale.dispose();
    },
  );

  testWidgets('home presents direct authentication states and safe actions', (
    tester,
  ) async {
    for (final (session, title, action) in [
      (_authenticating, '正在认证…', '取消'),
      (_waitingForNetwork, '网络不可用', '取消'),
      (_blocked, '认证失败', '重试'),
      (_session, '未连接', '登录'),
    ]) {
      await tester.pumpWidget(_app(_Client(sessions: [session])));
      await tester.pump(const Duration(milliseconds: 50));

      expect(find.text(title), findsOneWidget);
      expect(find.text(action), findsOneWidget);
      final surface = tester.widget<Container>(
        find.byKey(const ValueKey<String>('connection-status-surface')),
      );
      expect((surface.decoration! as BoxDecoration).gradient, isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }
  });

  testWidgets(
    'configuration UI keeps zero, one, and multiple snapshots distinct',
    (tester) async {
      for (final configurations in [
        const <ConfigurationSummary>[],
        const [_configuration],
        const [_configuration, _other],
      ]) {
        await tester.pumpWidget(_app(_Client(configurations: configurations)));
        await tester.pumpAndSettle();
        await tester.tap(find.text('配置').first);
        await tester.pumpAndSettle();

        switch (configurations.length) {
          case 0:
            expect(
              find.byKey(const ValueKey('configuration-account')),
              findsOneWidget,
            );
          case 1:
            expect(find.text('fixture-user'), findsWidgets);
            expect(find.textContaining('吉林大学'), findsWidgets);
          default:
            expect(find.text('无法管理多个配置'), findsOneWidget);
            expect(find.textContaining('只支持一个登录配置'), findsOneWidget);
            expect(find.text('fixture-user'), findsNothing);
            expect(find.text('other'), findsNothing);
        }
        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pump();
      }
    },
  );

  testWidgets('zero configuration creates with obscured transient password', (
    tester,
  ) async {
    final client = _Client(configurations: const []);
    await tester.pumpWidget(_app(client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('配置').first);
    await tester.pumpAndSettle();

    final password = find.byKey(const ValueKey('configuration-password'));
    expect(tester.widget<TextField>(password).obscureText, isTrue);
    await tester.tap(find.byTooltip('显示密码'));
    await tester.pump();
    expect(tester.widget<TextField>(password).obscureText, isFalse);
    await tester.enterText(
      find.byKey(const ValueKey('configuration-account')),
      'new-user',
    );
    await tester.enterText(password, 'transient-only');
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();

    expect(client.calls, contains('configuration.create'));
    expect(tester.widget<TextField>(password).controller?.text, isEmpty);
  });

  testWidgets(
    'one configuration updates separately and uses username fallback',
    (tester) async {
      final client = _Client(configurations: const [_configuration]);
      await tester.pumpWidget(_app(client));
      await tester.pumpAndSettle();
      await tester.tap(find.text('配置').first);
      await tester.pumpAndSettle();

      expect(find.text('fixture-user'), findsWidgets);
      await tester.tap(find.text('保存'));
      await tester.pumpAndSettle();
      expect(client.calls, contains('configuration.update'));
    },
  );

  testWidgets(
    'multiple configurations and ambiguous Sessions remain read-only',
    (tester) async {
      final client = _Client(configurations: const [_configuration, _other]);
      await tester.pumpWidget(_app(client));
      await tester.pumpAndSettle();
      await tester.tap(find.text('配置').first);
      await tester.pumpAndSettle();
      expect(find.text('无法管理多个配置'), findsOneWidget);
      expect(find.text('保存'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      await tester.pumpWidget(
        _app(_Client(sessions: const [_session, _session])),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('会话关系不明确'), findsOneWidget);
    },
  );

  testWidgets('home routes one unambiguous session action', (tester) async {
    final client = _Client(sessions: const [_session]);
    await tester.pumpWidget(_app(client));
    await tester.pumpAndSettle();
    expect(find.text('登录'), findsOneWidget);
    await tester.tap(find.text('登录'));
    await tester.pumpAndSettle();
    expect(client.calls, contains('session.ensureRunning'));
  });

  testWidgets('stopping and unknown Sessions expose no mutation', (
    tester,
  ) async {
    for (final session in const [_stopping, _unknown]) {
      final client = _Client(sessions: [session]);
      await tester.pumpWidget(_app(client));
      await tester.pump(const Duration(milliseconds: 50));

      expect(
        find.text(session.state == 'stopping' ? '正在注销…' : '状态不可用'),
        findsOneWidget,
      );
      expect(find.text('登录'), findsNothing);
      expect(find.text('注销'), findsNothing);
      expect(find.text('重试'), findsNothing);
      expect(client.calls, isEmpty);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }
  });

  testWidgets(
    'configuration reconciles refreshed authoritative values safely',
    (tester) async {
      final client = _Client(
        configurations: [_configuration],
        profiles: [_profile, _otherProfile],
      );
      await tester.pumpWidget(_app(client, const Duration(milliseconds: 100)));
      await tester.pump();
      await tester.tap(find.text('配置').first);
      await tester.pump();

      client.profiles = [_otherProfile];
      client.configurations = [_serverUpdatedConfiguration];
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pump();

      final account = find.byKey(const ValueKey('configuration-account'));
      expect(tester.widget<TextField>(account).controller?.text, 'server-user');
      expect(
        tester
            .widget<DropdownButtonFormField<String>>(
              find.byType(DropdownButtonFormField<String>),
            )
            .initialValue,
        'other-university',
      );
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('poll refresh preserves active username and password input', (
    tester,
  ) async {
    final client = _Client(
      configurations: [_configuration],
      profiles: [_profile, _otherProfile],
    );
    await tester.pumpWidget(_app(client, const Duration(milliseconds: 100)));
    await tester.pump();
    await tester.tap(find.text('配置').first);
    await tester.pump();

    final account = find.byKey(const ValueKey('configuration-account'));
    final password = find.byKey(const ValueKey('configuration-password'));
    await tester.enterText(account, 'draft-user');
    await tester.enterText(password, 'draft-secret');
    client.configurations = [_serverRenamedConfiguration];
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump();

    expect(tester.widget<TextField>(account).controller?.text, 'draft-user');
    expect(tester.widget<TextField>(password).controller?.text, 'draft-secret');
    expect(tester.takeException(), isNull);
  });

  testWidgets('all reachable pages fit a compact high-text-scale window', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(320, 568));
    tester.binding.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(() {
      tester.binding.platformDispatcher.clearTextScaleFactorTestValue();
      tester.binding.setSurfaceSize(null);
    });

    await tester.pumpWidget(_app());
    await tester.pump();
    expect(tester.takeException(), isNull);
    final navigationTop = tester.getTopLeft(find.byType(NavigationBar)).dy;
    final state = find.text('未连接').first;
    expect(tester.getBottomRight(state).dy, lessThanOrEqualTo(navigationTop));
    expect(tester.getSize(state).height, lessThanOrEqualTo(72));
    final compactTheme = Theme.of(tester.element(state));
    expect(
      tester.widget<Text>(state).style?.fontSize,
      compactTheme.textTheme.titleMedium?.fontSize,
    );
    expect(
      tester.widget<Text>(find.text('校园网')).style?.fontSize,
      compactTheme.textTheme.labelMedium?.fontSize,
    );
    expect(
      _effectiveTextScale(tester, state),
      closeTo(SidraviaLayout.compactContentMaxTextScale, 0.001),
    );
    expect(
      _effectiveTextScale(tester, find.text('校园网')),
      closeTo(SidraviaLayout.compactChromeMaxTextScale, 0.001),
    );
    expect(
      _effectiveTextScale(tester, find.text('登录')),
      closeTo(SidraviaLayout.compactChromeMaxTextScale, 0.001),
    );
    expect(
      tester.getBottomRight(find.widgetWithText(FilledButton, '登录')).dy,
      lessThanOrEqualTo(navigationTop),
    );

    await tester.tap(
      find.descendant(
        of: find.byType(NavigationBar),
        matching: find.text('配置'),
      ),
    );
    await tester.pump();
    expect(tester.takeException(), isNull);
    expect(find.text('配置'), findsNWidgets(2));
    expect(
      tester.getTopLeft(find.byKey(const ValueKey('configuration-account'))).dy,
      lessThan(navigationTop),
    );
    final compactAccount = tester.widget<TextField>(
      find.byKey(const ValueKey('configuration-account')),
    );
    expect(
      compactAccount.style?.fontSize,
      compactTheme.textTheme.bodyMedium?.fontSize,
    );
    expect(
      compactAccount.decoration?.labelStyle?.fontSize,
      compactTheme.textTheme.bodyMedium?.fontSize,
    );
    expect(
      _effectiveTextScale(
        tester,
        find.byKey(const ValueKey('configuration-account')),
      ),
      closeTo(SidraviaLayout.compactContentMaxTextScale, 0.001),
    );
    final compactPassword = tester.widget<TextField>(
      find.byKey(const ValueKey('configuration-password')),
    );
    expect(compactPassword.decoration?.labelText, '新密码');
    expect(compactPassword.decoration?.helperText, '可选');
    expect(
      compactPassword.style?.fontSize,
      compactTheme.textTheme.bodyMedium?.fontSize,
    );
    expect(
      _effectiveTextScale(
        tester,
        find.byKey(const ValueKey('configuration-password')),
      ),
      closeTo(SidraviaLayout.compactContentMaxTextScale, 0.001),
    );
    expect(
      _effectiveTextScale(tester, find.text('保存')),
      closeTo(SidraviaLayout.compactChromeMaxTextScale, 0.001),
    );

    await tester.tap(
      find.descendant(
        of: find.byType(NavigationBar),
        matching: find.text('关于'),
      ),
    );
    await tester.pump();
    expect(tester.takeException(), isNull);
    expect(find.text('关于'), findsNWidgets(2));
    expect(
      tester.getBottomRight(find.text('字体与许可')).dy,
      lessThan(navigationTop),
    );
    expect(
      tester.widget<Text>(find.text('字体与许可')).style?.fontSize,
      compactTheme.textTheme.labelLarge?.fontSize,
    );
    expect(
      _effectiveTextScale(tester, find.text('Sidravia')),
      closeTo(SidraviaLayout.compactContentMaxTextScale, 0.001),
    );
    expect(
      _effectiveTextScale(tester, find.text('字体与许可')),
      closeTo(SidraviaLayout.compactChromeMaxTextScale, 0.001),
    );
    expect(
      _effectiveTextScale(tester, find.text('查看许可')),
      closeTo(SidraviaLayout.compactChromeMaxTextScale, 0.001),
    );
    await tester.ensureVisible(find.text('查看许可'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('查看许可'));
    await tester.pumpAndSettle();
    expect(find.byType(LicensePage), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}

double _effectiveTextScale(WidgetTester tester, Finder finder) {
  const basis = 10.0;
  return MediaQuery.textScalerOf(tester.element(finder)).scale(basis) / basis;
}

SidraviaApp _app([
  _Client? client,
  Duration pollDelay = const Duration(days: 1),
]) => SidraviaApp(
  controller: GuiController(
    bootstrapper: _Bootstrapper(),
    connector: (_) async => client ?? _Client(),
    pollDelay: pollDelay,
  ),
);

class _Bootstrapper implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async => GuiBootstrapResult.success(
    GuiBootstrap(
      endpoint: Uri.parse('ws://127.0.0.1:4711/ipc'),
      token: _token,
      productVersion: 'fixture',
      buildId: 'fixture',
      daemonPid: 1,
      mode: 'desktop',
    ),
  );
}

class _FailureBootstrapper implements GuiBootstrapper {
  @override
  Future<GuiBootstrapResult> bootstrap() async =>
      const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
}

class _SequencedBootstrapper implements GuiBootstrapper {
  var calls = 0;

  @override
  Future<GuiBootstrapResult> bootstrap() async {
    calls++;
    return calls == 1
        ? _Bootstrapper().bootstrap()
        : const GuiBootstrapResult.failure(GuiBootstrapFailure.failed);
  }
}

class _Client implements SidraviaIpcClient {
  _Client({
    this.configurations = const [_configuration],
    this.sessions = const [],
    this.profiles = const [_profile],
  });
  List<ConfigurationSummary> configurations;
  List<SessionSummary> sessions;
  List<InstitutionProfile> profiles;
  final calls = <String>[];

  @override
  Future<DaemonStatus> daemonStatus() async => _daemon;
  @override
  Future<List<InstitutionProfile>> profileList() async => profiles;
  @override
  Future<List<ConfigurationSummary>> configurationList() async =>
      configurations;
  @override
  Future<List<SessionSummary>> sessionList() async => sessions;
  @override
  Future<ConfigurationSummary> configurationCreate({
    required String institutionProfileId,
    required String username,
    required String password,
  }) async {
    calls.add('configuration.create');
    return _configuration;
  }

  @override
  Future<ConfigurationSummary> configurationUpdate({
    required String configurationId,
    required String institutionProfileId,
    required String username,
  }) async {
    calls.add('configuration.update');
    return _configuration;
  }

  @override
  Future<ConfigurationSummary> configurationSetPassword({
    required String configurationId,
    required String password,
  }) async {
    calls.add('configuration.setPassword');
    return _configuration;
  }

  @override
  Future<SessionSummary> sessionStartConfiguration(
    String configurationId,
  ) async {
    calls.add('session.startConfiguration');
    return _session;
  }

  @override
  Future<SessionSummary> sessionStop(String sessionId) async {
    calls.add('session.stop');
    return _session;
  }

  @override
  Future<SessionSummary> sessionEnsureRunning(String sessionId) async {
    calls.add('session.ensureRunning');
    return _session;
  }

  @override
  Future<SessionSummary> sessionRestart(String sessionId) async {
    calls.add('session.restart');
    return _session;
  }

  @override
  Future<void> close() async {}
}

const _daemon = DaemonStatus(
  productVersion: 'fixture',
  buildId: 'fixture',
  pid: 1,
  status: 'running',
  mode: 'desktop',
);
const _profile = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom-5.2.0-d',
);
const _otherProfile = InstitutionProfile(
  id: 'other-university',
  displayName: '另一所大学',
  protocolId: 'other-protocol',
);
const _configuration = ConfigurationSummary(
  id: 'cfg-a',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'fixture-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _other = ConfigurationSummary(
  id: 'cfg-b',
  displayName: 'other',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'other-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _serverUpdatedConfiguration = ConfigurationSummary(
  id: 'cfg-a',
  displayName: '',
  institutionProfileId: 'other-university',
  institutionDisplayName: '另一所大学',
  authenticationProtocolId: 'other-protocol',
  username: 'server-user',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _serverRenamedConfiguration = ConfigurationSummary(
  id: 'cfg-a',
  displayName: '',
  institutionProfileId: 'jlu',
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom-5.2.0-d',
  username: 'server-renamed',
  credentialStored: true,
  storageProtection: 'protected',
  autoReconnect: true,
);
const _session = SessionSummary(
  id: 'session-a',
  displayName: '',
  accountName: 'fixture-user',
  state: 'suspended',
  intent: 'suspend_authentication',
  configurationId: 'cfg-a',
);
const _authenticated = SessionSummary(
  id: 'session-authenticated',
  displayName: '校园网络',
  accountName: 'fixture-user',
  state: 'authenticated',
  intent: 'maintain_authentication',
  configurationId: 'cfg-a',
);
const _authenticating = SessionSummary(
  id: 'session-authenticating',
  displayName: '校园网络',
  accountName: 'fixture-user',
  state: 'authenticating',
  intent: 'maintain_authentication',
  configurationId: 'cfg-a',
);
const _waitingForNetwork = SessionSummary(
  id: 'session-waiting-network',
  displayName: '校园网络',
  accountName: 'fixture-user',
  state: 'waiting_for_network',
  intent: 'maintain_authentication',
  configurationId: 'cfg-a',
);
const _blocked = SessionSummary(
  id: 'session-blocked',
  displayName: '校园网络',
  accountName: 'fixture-user',
  state: 'blocked_by_error',
  intent: 'maintain_authentication',
  configurationId: 'cfg-a',
  stateReason: SessionStateReason(
    code: 'fixture_failure',
    description: '用户名或密码错误。',
  ),
);
const _stopping = SessionSummary(
  id: 'session-stopping',
  displayName: '',
  accountName: 'fixture-user',
  state: 'stopping',
  intent: 'suspend_authentication',
  configurationId: 'cfg-a',
);
const _unknown = SessionSummary(
  id: 'session-unknown',
  displayName: '',
  accountName: 'fixture-user',
  state: 'unknown',
  intent: 'suspend_authentication',
  configurationId: 'cfg-a',
);
const _token =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
