import 'dart:async';

import 'package:sidravia_gui/features/announcements/school_announcement_window.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_notifications.dart';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

class SidraviaApp extends StatefulWidget {
  const SidraviaApp({
    super.key,
    required this.controller,
    this.announcements,
    this.desktopPresence,
    this.appearance,
  });
  final GuiController controller;
  final Appearance? appearance;
  final SchoolAnnouncementController? announcements;
  final DesktopPresence? desktopPresence;

  @override
  State<SidraviaApp> createState() => _SidraviaAppState();
}

class _SidraviaAppState extends State<SidraviaApp> with WidgetsBindingObserver {
  late final _appearance = widget.appearance ?? Appearance();
  final _messengerKey = GlobalKey<ScaffoldMessengerState>();
  final _navigatorKey = GlobalKey<NavigatorState>();
  final _shellKey = GlobalKey<SidraviaShellState>();
  late final SchoolAnnouncementController _announcements;
  bool _exiting = false;
  GuiCloseBehavior? _lastCloseBehavior;
  bool? _lastStartHidden;
  final _windowObserver = SidraviaWindowObserver();
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _appearance.addListener(_syncDesktopPreferences);
    _syncDesktopPreferences();
    widget.controller.addListener(_handleControllerChanged);
    _announcements =
        widget.announcements ??
        SchoolAnnouncementController(
          preferences: _appearance,
          desktop: widget.desktopPresence ?? DesktopPresence.disabled(),
          window: SchoolAnnouncementWindow.disabled(),
          notifications: SchoolAnnouncementNotifications.disabled(),
          startedAt: DateTime.now(),
        );
    _announcements.updateCapabilities(widget.controller.capabilities);
    unawaited(_announcements.start());
    unawaited(widget.controller.start());
    final desktop = widget.desktopPresence;
    if (desktop != null) {
      desktop.onExitRequested = _handleExitRequested;
      desktop.onTrayAction = _handleTrayAction;
      _handleControllerChanged();
    }
  }

  void _handleControllerChanged() {
    _announcements.updateCapabilities(widget.controller.capabilities);
    final snapshot = widget.controller.snapshot;
    final desktop = widget.desktopPresence;
    if (desktop != null && snapshot != null) {
      unawaited(desktop.considerSnapshot(snapshot));
    }
    _publishTrayPresentation();
  }

  void _publishTrayPresentation() {
    final desktop = widget.desktopPresence;
    if (desktop == null) return;
    final brightness =
        _appearance.value == ThemeMode.dark ||
        (_appearance.value == ThemeMode.system &&
            WidgetsBinding.instance.platformDispatcher.platformBrightness ==
                Brightness.dark);
    unawaited(
      desktop.publishTray(
        TrayPopoverData.fromPresentation(
          widget.controller.connectionPresentation,
          dark: brightness,
        ),
      ),
    );
  }

  Future<void> _handleTrayAction(TrayIntent intent) async {
    final controller = widget.controller;
    final desktop = widget.desktopPresence;
    final presentation = controller.connectionPresentation;
    bool same(GuiConnectionAction action) =>
        (presentation.primaryEnabled &&
            action.kind == presentation.primaryAction.kind &&
            action.targetId == presentation.primaryAction.targetId) ||
        (presentation.secondaryEnabled &&
            action.kind == presentation.secondaryAction.kind &&
            action.targetId == presentation.secondaryAction.targetId);
    if (intent.kind == TrayIntentKind.exit) {
      await _handleExitRequested();
      return;
    }
    if (intent.kind == TrayIntentKind.restoreMain) {
      await desktop?.restoreMain();
      return;
    }
    if (intent.kind == TrayIntentKind.connection) {
      final kind = intent.connectionAction;
      if (kind == null) return;
      final action = GuiConnectionAction(kind, intent.targetId);
      if (!same(action)) {
        _publishTrayPresentation();
        return;
      }
      final accepted = await controller.performConnectionAction(action);
      if (!accepted && mounted) {
        _messengerKey.currentState?.showSnackBar(
          const SnackBar(content: Text('此托盘操作已失效，请打开主界面查看当前状态。')),
        );
      }
      _publishTrayPresentation();
      return;
    }
    final action = switch (intent.kind) {
      TrayIntentKind.addConfiguration => const GuiConnectionAction(
        GuiConnectionActionKind.addConfiguration,
      ),
      TrayIntentKind.editConfiguration => GuiConnectionAction(
        GuiConnectionActionKind.editConfiguration,
        intent.targetId,
      ),
      TrayIntentKind.showSettings => const GuiConnectionAction(
        GuiConnectionActionKind.showSettings,
      ),
      TrayIntentKind.showDetails => const GuiConnectionAction(
        GuiConnectionActionKind.showDetails,
      ),
      _ => null,
    };
    if (action == null || !same(action)) {
      _publishTrayPresentation();
      return;
    }
    if (action.kind == GuiConnectionActionKind.addConfiguration &&
        !controller.capabilities.canCreate) {
      return;
    }
    if (action.kind == GuiConnectionActionKind.editConfiguration &&
        (action.targetId == null ||
            !controller.capabilities.matchesConfiguration(action.targetId!))) {
      return;
    }
    final page = switch (action.kind) {
      GuiConnectionActionKind.addConfiguration ||
      GuiConnectionActionKind.editConfiguration => AppPage.configuration,
      GuiConnectionActionKind.showSettings => AppPage.settings,
      GuiConnectionActionKind.showDetails => AppPage.diagnostics,
      _ => null,
    };
    if (page == null) return;
    await desktop?.restoreMain();
    if (mounted) {
      final moved =
          await _shellKey.currentState?.navigateFromTray(page) ?? false;
      if (!moved) {
        _messengerKey.currentState?.showSnackBar(
          const SnackBar(content: Text('已保留未保存的连接配置。')),
        );
      }
    }
  }

  void _syncDesktopPreferences() {
    final desktop = widget.desktopPresence;
    if (desktop == null ||
        !desktop.enabled ||
        (_lastCloseBehavior == _appearance.closeBehavior &&
            _lastStartHidden == _appearance.startHidden)) {
      return;
    }
    _lastCloseBehavior = _appearance.closeBehavior;
    _lastStartHidden = _appearance.startHidden;
    unawaited(
      desktop.configurePreferences(
        closeBehavior: _lastCloseBehavior!,
        startHidden: _lastStartHidden!,
      ),
    );
  }

  Future<void> _handleExitRequested() async {
    final desktop = widget.desktopPresence;
    if (desktop == null || _exiting) return;
    _exiting = true;
    var begun = false;
    try {
      final context = _navigatorKey.currentContext;
      if (!mounted || context == null || _shellKey.currentState == null) return;
      if (!await _shellKey.currentState!.mayExit() ||
          !mounted ||
          !context.mounted) {
        return;
      }
      final active =
          widget.controller.busy ||
          (widget.controller.snapshot?.sessions.any(
                (s) => [
                  SessionState.authenticated,
                  SessionState.authenticating,
                  SessionState.waitingForNetwork,
                  SessionState.waitingBeforeRetry,
                ].contains(s.state),
              ) ??
              false);
      if (active) {
        final approved = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('断开连接并退出？'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('断开并退出'),
              ),
            ],
          ),
        );
        if (approved != true || !mounted) return;
      }
      if (!await desktop.beginExit()) {
        _messengerKey.currentState?.showSnackBar(
          SnackBar(content: Text(desktop.feedback ?? '无法开始退出，连接保持不变。')),
        );
        return;
      }
      begun = true;
      await widget.controller.exitAndDisconnect();
      await _announcements.close();
      await desktop.destroy();
    } finally {
      if (!begun) {
        await desktop.cancelExit();
        _exiting = false;
      }
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    widget.controller.removeListener(_handleControllerChanged);
    _announcements.dispose();
    final desktop = widget.desktopPresence;
    if (desktop != null) {
      desktop.onExitRequested = null;
      desktop.onTrayAction = null;
      desktop.dispose();
    }
    widget.controller.dispose();
    _appearance.removeListener(_syncDesktopPreferences);
    _appearance.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AppearanceScope(
    appearance: _appearance,
    child: ValueListenableBuilder<ThemeMode>(
      valueListenable: _appearance,
      builder: (context, mode, _) => MaterialApp(
        scaffoldMessengerKey: _messengerKey,
        navigatorKey: _navigatorKey,
        title: 'Sidravia',
        navigatorObservers: [_windowObserver],
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light(),
        darkTheme: AppTheme.dark(),
        themeMode: mode,
        builder: (context, child) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted) _publishTrayPresentation();
          });
          return child ?? const SizedBox.shrink();
        },
        home: SidraviaWindowFrame(
          child: SidraviaShell(
            key: _shellKey,
            desktopPresence: widget.desktopPresence,
            controller: widget.controller,
            announcements: _announcements,
          ),
        ),
      ),
    ),
  );
}
