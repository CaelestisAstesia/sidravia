import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

class SidraviaApp extends StatefulWidget {
  const SidraviaApp({
    super.key,
    required this.controller,
    this.announcements,
    this.desktopPresence,
  });

  final GuiController controller;
  final AnnouncementController? announcements;
  final DesktopPresence? desktopPresence;

  @override
  State<SidraviaApp> createState() => _SidraviaAppState();
}

class _SidraviaAppState extends State<SidraviaApp> with WidgetsBindingObserver {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.controller.addListener(_handleControllerChanged);
    widget.controller.start();
    widget.announcements?.start();
    final desktop = widget.desktopPresence;
    if (desktop != null) {
      desktop.onExitRequested = _handleExitRequested;
      _handleControllerChanged();
    }
  }

  void _handleControllerChanged() {
    final desktop = widget.desktopPresence;
    final snapshot = widget.controller.snapshot;
    if (desktop != null && snapshot != null) {
      unawaited(desktop.considerSnapshot(snapshot));
    }
  }

  Future<void> _handleExitRequested() async {
    final desktop = widget.desktopPresence;
    if (desktop == null) return;
    await widget.controller.exitAndDisconnect();
    await desktop.destroy();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      widget.announcements?.refreshIfDue();
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
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Sidravia',
      debugShowCheckedModeBanner: false,
      theme: SidraviaTheme.light(),
      home: SidraviaWindowFrame(
        child: SidraviaShell(
          controller: widget.controller,
          announcements: widget.announcements,
        ),
      ),
    );
  }
}
