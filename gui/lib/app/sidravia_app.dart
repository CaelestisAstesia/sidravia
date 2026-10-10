import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
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
  final AnnouncementController? announcements;
  final DesktopPresence? desktopPresence;

  @override
  State<SidraviaApp> createState() => _SidraviaAppState();
}

class _SidraviaAppState extends State<SidraviaApp> with WidgetsBindingObserver {
  late final _appearance = widget.appearance ?? Appearance();
  final _messengerKey = GlobalKey<ScaffoldMessengerState>();
  final _navigatorKey = GlobalKey<NavigatorState>();
  final _shellKey = GlobalKey<SidraviaShellState>();
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
    unawaited(widget.controller.start());
    unawaited(widget.announcements?.start() ?? Future<void>.value());
    final desktop = widget.desktopPresence;
    if (desktop != null) {
      desktop.onExitRequested = _handleExitRequested;
      _handleControllerChanged();
    }
  }

  void _handleControllerChanged() {
    final snapshot = widget.controller.snapshot;
    final desktop = widget.desktopPresence;
    if (desktop != null && snapshot != null) {
      unawaited(desktop.considerSnapshot(snapshot));
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
      await desktop.destroy();
    } finally {
      if (!begun) {
        await desktop.cancelExit();
        _exiting = false;
      }
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      unawaited(widget.announcements?.refreshIfDue() ?? Future<void>.value());
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    widget.controller.removeListener(_handleControllerChanged);
    final desktop = widget.desktopPresence;
    if (desktop != null) {
      desktop.onExitRequested = null;
      desktop.dispose();
    }
    widget.announcements?.dispose();
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
        home: SidraviaWindowFrame(
          child: SidraviaShell(
            key: _shellKey,
            desktopPresence: widget.desktopPresence,
            controller: widget.controller,
            announcements: widget.announcements,
          ),
        ),
      ),
    ),
  );
}
