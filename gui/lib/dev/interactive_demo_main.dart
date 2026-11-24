import 'package:sidravia_gui/shared/localization/app_locale.dart';
import 'package:sidravia_gui/licensing/source_code_pro_font_license.dart';

import 'dart:async';

import 'package:sidravia_gui/application/connection_clock.dart';

import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

import 'dart:convert';

import 'package:sidravia_gui/platform/settings/gui_settings_storage.dart';

import 'package:flutter/services.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/semantics.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/platform/installation/installation_manager.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';

part 'offline_demo_platform.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // CanvasKit cannot discover desktop fonts. The developer entry supplies a
  // local host font to the web preview without adding it to native products.
  const systemFont = String.fromEnvironment('SIDRAVIA_DEMO_SYSTEM_FONT');
  if (kIsWeb && systemFont.isNotEmpty) {
    final ordinary = AppTheme.light().textTheme.bodyMedium!;
    final fallbacks = ordinary.fontFamilyFallback;
    // Avoid CanvasKit's built-in Latin-only Roboto face on Linux.
    final family = fallbacks != null && fallbacks.isNotEmpty
        ? fallbacks.first
        : ordinary.fontFamily!;
    await (FontLoader(family)..addFont(rootBundle.load(systemFont))).load();
  }
  registerSourceCodeProFontLicense();
  // Keep the offline web preview inspectable by accessibility/browser tools.
  SemanticsBinding.instance.ensureSemantics();
  const directory = String.fromEnvironment('SIDRAVIA_DEMO_SETTINGS_DIR');
  final appearance = directory.isEmpty
      ? GuiPreferences()
      : GuiPreferences(store: isolatedDemoGuiSettingsStore(directory));
  await appearance.initialize();
  runApp(OfflineDemoApp(appearance: appearance));
}

/// Composition only: every destination is the unchanged production page.
class OfflineDemoApp extends StatefulWidget {
  const OfflineDemoApp({super.key, this.appearance, this.fixedTime});
  final DateTime? fixedTime;
  final GuiPreferences? appearance;
  @override
  State<OfflineDemoApp> createState() => _OfflineDemoAppState();
}

class _OfflineDemoAppState extends State<OfflineDemoApp> {
  late final _appearance = widget.appearance ?? GuiPreferences();
  final _windowObserver = SidraviaWindowObserver();
  late final OfflineDemoClient _client;
  late final GuiController _controller;
  late final SchoolAnnouncementController _announcements;
  final _navigator = GlobalKey<NavigatorState>();
  Size _previewSize = const Size(400, 690);
  String _scenario = 'authenticated';

  @override
  void initState() {
    super.initState();
    final fixedTime = widget.fixedTime;
    _client = OfflineDemoClient(now: fixedTime);
    _controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => _client,
      presentationClock: fixedTime == null
          ? null
          : ConnectionClock(now: () => fixedTime),
    );
    _announcements = SchoolAnnouncementController(
      preferences: _appearance,
      desktop: DesktopPresence.disabled(),
      window: _OfflineAnnouncementWindow(_navigator),
      notifications: SchoolAnnouncementNotifications.disabled(),
      startedAt: fixedTime ?? DateTime.now(),
      clock: () => fixedTime ?? DateTime.now(),
    );
    _controller.addListener(_updateAnnouncements);
    _updateAnnouncements();
    unawaited(_announcements.start());
    unawaited(_controller.start());
  }

  void _updateAnnouncements() =>
      _announcements.updateCapabilities(_controller.capabilities);

  Future<void> _showTrayPreview(BuildContext context) async {
    final presentation = TrayPopoverData.fromPresentation(
      _controller.connectionPresentation,
      dark: Theme.of(context).brightness == Brightness.dark,
    );
    final overlayContext = _navigator.currentState?.overlay?.context;
    if (overlayContext == null) return;
    await showDialog<void>(
      context: overlayContext,
      builder: (dialogContext) => Dialog(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(10),
          child: TrayPopover(
            data: presentation,
            onAction: (intent) {
              if (intent.kind == TrayIntentKind.connection &&
                  intent.connectionAction != null) {
                unawaited(
                  _controller.performConnectionAction(
                    GuiConnectionAction(
                      intent.connectionAction!,
                      intent.targetId,
                    ),
                  ),
                );
              } else if (intent.kind == TrayIntentKind.restoreMain ||
                  intent.kind == TrayIntentKind.exit) {
                Navigator.pop(dialogContext);
              }
            },
            onDismiss: () => Navigator.pop(dialogContext),
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    _controller.removeListener(_updateAnnouncements);
    _controller.dispose();
    _announcements.dispose();
    _client.dispose();
    _appearance.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => GuiPreferencesScope(
    appearance: _appearance,
    child: ValueListenableBuilder<ThemeMode>(
      valueListenable: _appearance,
      builder: (context, mode, _) => MaterialApp(
        locale: AppLocale.locale,
        supportedLocales: AppLocale.supportedLocales,
        localizationsDelegates: AppLocale.delegates,
        title: 'Sidravia 离线演示',
        navigatorKey: _navigator,
        navigatorObservers: [_windowObserver],
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light(),
        darkTheme: AppTheme.dark(),
        themeMode: mode,
        onGenerateRoute: (_) => _PreviewPageRoute(
          builder: (context) => SidraviaWindowFrame(
            child: SidraviaShell(
              websiteLatencyEnabled: false,
              controller: _controller,
              announcements: _announcements,
              createIntegrationManager: _OfflineInstallationManager.new,
            ),
          ),
        ),
        builder: (context, navigator) => _DemoOverlay(
          child: Column(
            children: [
              Theme(
                data: AppTheme.light(),
                child: Material(
                  key: const ValueKey('demo-tools'),
                  color: const Color(0xFFFFF3CF),
                  textStyle: AppTheme.light().textTheme.bodyMedium!.copyWith(
                    color: AppColors.inkLight,
                  ),
                  child: SafeArea(
                    bottom: false,
                    child: SizedBox(
                      width: double.infinity,
                      child: Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 12,
                          vertical: 8,
                        ),
                        child: AnimatedBuilder(
                          animation: _client,
                          builder: (context, _) => Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              SizedBox(
                                height:
                                    MediaQuery.textScalerOf(context).scale(12) *
                                    1.2,
                                child: const Tooltip(
                                  message: '离线演示 · 勿输入真实账号或密码',
                                  child: Text(
                                    '离线演示 · 勿输入真实账号或密码',
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(
                                      fontSize: 12,
                                      height: 1.2,
                                      fontWeight: FontWeight.w700,
                                    ),
                                  ),
                                ),
                              ),
                              const SizedBox(height: 2),
                              SizedBox(
                                height:
                                    MediaQuery.textScalerOf(context).scale(11) *
                                    1.2 *
                                    2,
                                child: Tooltip(
                                  message: _client.feedback,
                                  child: Text(
                                    _client.feedback,
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis,
                                    style: const TextStyle(
                                      fontSize: 11,
                                      height: 1.2,
                                    ),
                                  ),
                                ),
                              ),
                              SizedBox(
                                height:
                                    (MediaQuery.textScalerOf(context)
                                                .scale(20) +
                                            20)
                                        .clamp(48, double.infinity),
                                child: ScrollConfiguration(
                                  behavior: ScrollConfiguration.of(context)
                                      .copyWith(scrollbars: false),
                                  child: SingleChildScrollView(
                                    scrollDirection: Axis.horizontal,
                                    child: Row(
                                      spacing: 8,
                                      children: [
                                        if (kIsWeb)
                                          _DemoMenu<Size>(
                                            tooltip: '预览窗口尺寸',
                                            onSelected: (size) => setState(
                                              () => _previewSize = size,
                                            ),
                                            itemBuilder: (_) => const [
                                              PopupMenuItem(
                                                value: Size(400, 690),
                                                child: Text('400 × 690 · 桌面窗口'),
                                              ),
                                              PopupMenuItem(
                                                value: Size(720, 690),
                                                child: Text('720 × 690 · 宽窗口'),
                                              ),
                                            ],
                                            child: Padding(
                                              padding: const EdgeInsets.all(6),
                                              child: Text(
                                                '窗口：${_previewSize.width.toInt()} × ${_previewSize.height.toInt()}',
                                              ),
                                            ),
                                          ),
                                        TextButton(
                                          onPressed: () =>
                                              _showTrayPreview(context),
                                          child: const Text('离线托盘预览'),
                                        ),
                                        if (Theme.of(context).platform ==
                                            TargetPlatform.windows)
                                          TextButton(
                                            onPressed: () async {
                                              String text;
                                              try {
                                                text =
                                                    const JsonEncoder.withIndent(
                                                      '  ',
                                                    ).convert(
                                                      await SidraviaWindowCommands.diagnostics(),
                                                    );
                                              } catch (error) {
                                                text = 'Windows 窗口诊断不可用：$error';
                                              }
                                              if (!mounted) return;
                                              final overlayContext = _navigator
                                                  .currentState
                                                  ?.overlay
                                                  ?.context;
                                              if (overlayContext == null ||
                                                  !overlayContext.mounted) {
                                                return;
                                              }
                                              await showDialog<void>(
                                                context: overlayContext,
                                                builder: (dialogContext) =>
                                                    AlertDialog(
                                                      title: const Text(
                                                        '原生窗口诊断（只读）',
                                                      ),
                                                      content:
                                                          SingleChildScrollView(
                                                            child:
                                                                SelectableText(
                                                                  text,
                                                                ),
                                                          ),
                                                      actions: [
                                                        TextButton(
                                                          onPressed: () =>
                                                              Clipboard.setData(
                                                                ClipboardData(
                                                                  text: text,
                                                                ),
                                                              ),
                                                          child: const Text(
                                                            '复制诊断',
                                                          ),
                                                        ),
                                                        TextButton(
                                                          onPressed: () =>
                                                              Navigator.pop(
                                                                dialogContext,
                                                              ),
                                                          child: const Text(
                                                            '关闭',
                                                          ),
                                                        ),
                                                      ],
                                                    ),
                                              );
                                            },
                                            child: const Text(
                                              '窗口诊断',
                                              style: TextStyle(fontSize: 11),
                                            ),
                                          ),
                                        _DemoMenu<String>(
                                          tooltip: '演示场景',
                                          onSelected: (value) async {
                                            _client.selectScenario(value);
                                            await _controller.start();
                                            if (mounted) {
                                              setState(() => _scenario = value);
                                            }
                                          },
                                          itemBuilder: (context) => [
                                            for (final entry in const {
                                              'authenticated': '已连接',
                                              'authenticated_business':
                                                  '已连接 · 账户业务',
                                              'suspended': '未连接',
                                              'authenticating': '正在认证',
                                              'waiting_for_network': '等待网络',
                                              'waiting_before_retry': '等待重试',
                                              'blocked_by_error': '认证失败',
                                              'empty': '尚未配置',
                                            }.entries)
                                              PopupMenuItem(
                                                value: entry.key,
                                                child: Text(entry.value),
                                              ),
                                          ],
                                          child: Padding(
                                            padding: const EdgeInsets.all(6),
                                            child: Text(
                                              '场景：$_scenario',
                                              style: const TextStyle(
                                                fontSize: 11,
                                              ),
                                            ),
                                          ),
                                        ),
                                        _DemoMenu<ThemeMode>(
                                          tooltip: '演示外观',
                                          onSelected: (value) =>
                                              _appearance.value = value,
                                          itemBuilder: (context) => const [
                                            PopupMenuItem(
                                              value: ThemeMode.system,
                                              child: Text('跟随系统'),
                                            ),
                                            PopupMenuItem(
                                              value: ThemeMode.light,
                                              child: Text('浅色'),
                                            ),
                                            PopupMenuItem(
                                              value: ThemeMode.dark,
                                              child: Text('深色'),
                                            ),
                                          ],
                                          child: const Padding(
                                            padding: EdgeInsets.all(6),
                                            child: Text(
                                              '演示外观',
                                              style: TextStyle(fontSize: 11),
                                            ),
                                          ),
                                        ),
                                      ],
                                    ),
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
              Expanded(child: _previewStage(context, navigator!)),
            ],
          ),
        ),
      ),
    ),
  );

  Widget _previewStage(BuildContext context, Widget child) {
    if (!kIsWeb) return child;
    return Builder(
      builder: (context) {
        final scheme = Theme.of(context).colorScheme;
        return ColoredBox(
          color: scheme.surfaceContainerHighest,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Center(
              child: ConstrainedBox(
                constraints: BoxConstraints(
                  maxWidth: _previewSize.width,
                  maxHeight: _previewSize.height,
                ),
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(16),
                    border: Border.all(color: scheme.outlineVariant),
                    boxShadow: [
                      BoxShadow(
                        color: Colors.black.withValues(alpha: .12),
                        blurRadius: 24,
                        offset: const Offset(0, 8),
                      ),
                    ],
                  ),
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(16),
                    child: LayoutBuilder(
                      builder: (context, constraints) => MediaQuery(
                        data: MediaQuery.of(context)
                            .copyWith(size: constraints.biggest),
                        child: child,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

/// Leave the developer controls accessible outside the app's base route.
class _PreviewPageRoute extends MaterialPageRoute<void> {
  _PreviewPageRoute({required super.builder});
  @override
  Widget buildModalBarrier() => const SizedBox.shrink();
}

/// Toolbar tooltips and menus use the full browser overlay, independently of
/// the app Navigator confined to the simulated window.
class _DemoOverlay extends StatefulWidget {
  const _DemoOverlay({required this.child});
  final Widget child;
  @override
  State<_DemoOverlay> createState() => _DemoOverlayState();
}

class _DemoOverlayState extends State<_DemoOverlay> {
  late final _entry = OverlayEntry(builder: (_) => widget.child);
  @override
  void didUpdateWidget(covariant _DemoOverlay oldWidget) {
    super.didUpdateWidget(oldWidget);
    _entry.markNeedsBuild();
  }

  @override
  void dispose() {
    _entry.remove();
    _entry.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Overlay(initialEntries: [_entry]);
}

class _DemoMenu<T> extends StatelessWidget {
  const _DemoMenu({
    required this.tooltip,
    required this.onSelected,
    required this.itemBuilder,
    required this.child,
  });
  final String tooltip;
  final ValueChanged<T> onSelected;
  final PopupMenuItemBuilder<T> itemBuilder;
  final Widget child;
  @override
  Widget build(BuildContext context) => MenuAnchor(
    menuChildren: [
      for (final item in itemBuilder(context))
        if (item is PopupMenuItem<T>)
          MenuItemButton(
            onPressed: item.enabled ? () => onSelected(item.value as T) : null,
            child: item.child,
          ),
    ],
    builder: (context, menu, _) => Tooltip(
      message: tooltip,
      child: TextButton(
        onPressed: () => menu.isOpen ? menu.close() : menu.open(),
        child: child,
      ),
    ),
  );
}
