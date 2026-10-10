import 'package:flutter/material.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';

class SidraviaShell extends StatefulWidget {
  const SidraviaShell({
    super.key,
    required this.controller,
    this.announcements,
    this.desktopPresence,
  });
  final GuiController controller;
  final SchoolAnnouncementController? announcements;
  final DesktopPresence? desktopPresence;

  @override
  State<SidraviaShell> createState() => SidraviaShellState();
}

class SidraviaShellState extends State<SidraviaShell> {
  AppPage _page = AppPage.home;
  final _configurationKey = GlobalKey<ConfigurationPageState>();
  final _history = <AppPage>[];

  Future<bool> mayExit() async =>
      _page != AppPage.configuration ||
      await (_configurationKey.currentState?.mayLeave() ?? Future.value(false));

  Future<bool> navigateFromTray(AppPage page) async {
    if (!mounted) return false;
    if (_page == AppPage.configuration &&
        page != AppPage.configuration &&
        !await (_configurationKey.currentState?.mayLeave() ??
            Future.value(false))) {
      return false;
    }
    if (!mounted) return false;
    _go(page);
    return true;
  }

  void _go(AppPage page) {
    if (_page == page) return;
    setState(() {
      _history.add(_page);
      _page = page;
    });
  }

  void _requestBack() {
    if (_page == AppPage.configuration) {
      _configurationKey.currentState?.requestLeave();
    } else {
      _back();
    }
  }

  void _saved() {
    setState(() {
      _history.clear();
      _page = AppPage.home;
    });
  }

  void _back() {
    setState(
      () => _page = _history.isEmpty ? AppPage.home : _history.removeLast(),
    );
  }

  @override
  Widget build(BuildContext context) => CallbackShortcuts(
    bindings: {
      const SingleActivator(LogicalKeyboardKey.escape): () {
        if (_page != AppPage.home) _requestBack();
      },
      const SingleActivator(LogicalKeyboardKey.arrowLeft, alt: true): () {
        if (_page != AppPage.home) _requestBack();
      },
    },
    child: PopScope(
      canPop: _page == AppPage.home,
      onPopInvokedWithResult: (didPop, result) {
        if (!didPop) _requestBack();
      },
      child: Focus(
        autofocus: true,
        child: Scaffold(
          body: SafeArea(
            child: AnimatedBuilder(
              animation: widget.controller,
              builder: (context, _) => switch (_page) {
                AppPage.home => HomePage(
                  controller: widget.controller,
                  announcements: widget.announcements,
                  onNavigate: _go,
                ),
                AppPage.settings => SettingsPage(
                  desktopPresence: widget.desktopPresence,
                  controller: widget.controller,
                  onNavigate: _go,
                  onBack: _back,
                ),
                AppPage.configuration => ConfigurationPage(
                  key: _configurationKey,
                  onSaved: _saved,
                  controller: widget.controller,
                  onBack: _back,
                ),
                AppPage.diagnostics => AdvancedPage(
                  controller: widget.controller,
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
