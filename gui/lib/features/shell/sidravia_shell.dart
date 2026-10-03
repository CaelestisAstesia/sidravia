import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';

class SidraviaShell extends StatefulWidget {
  const SidraviaShell({
    super.key,
    required this.controller,
    this.announcements,
  });
  final GuiController controller;
  final AnnouncementController? announcements;

  @override
  State<SidraviaShell> createState() => _SidraviaShellState();
}

class _SidraviaShellState extends State<SidraviaShell> {
  AppPage _page = AppPage.home;
  final _history = <AppPage>[];

  void _go(AppPage page) {
    if (_page == page) return;
    setState(() {
      _history.add(_page);
      _page = page;
    });
  }

  void _back() {
    setState(
      () => _page = _history.isEmpty ? AppPage.home : _history.removeLast(),
    );
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) => Scaffold(
      body: SafeArea(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 960),
            child: AnimatedBuilder(
              animation: widget.controller,
              builder: (context, _) => switch (_page) {
                AppPage.home => HomePage(
                  controller: widget.controller,
                  announcements: widget.announcements,
                  onNavigate: _go,
                ),
                AppPage.settings => SettingsPage(
                  controller: widget.controller,
                  onNavigate: _go,
                  onBack: _back,
                ),
                AppPage.configuration => ConfigurationPage(
                  controller: widget.controller,
                  onBack: _back,
                ),
                AppPage.details => AdvancedPage(
                  controller: widget.controller,
                  detailsOnly: true,
                  onBack: _back,
                  onDiagnostics: () => _go(AppPage.diagnostics),
                ),
                AppPage.diagnostics => AdvancedPage(
                  controller: widget.controller,
                  detailsOnly: false,
                  onBack: _back,
                ),
              },
            ),
          ),
        ),
      ),
    ),
  );
}
