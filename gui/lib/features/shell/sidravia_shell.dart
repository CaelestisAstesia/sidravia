import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/advanced/advanced_page.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
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

  Widget _desktopHeader(BuildContext context) => Column(
    children: [
      Padding(
        key: const ValueKey('desktop-header'),
        padding: const EdgeInsets.symmetric(horizontal: 48, vertical: 12),
        child: Row(
          children: [
            Text(
              'Sidravia',
              style: Theme.of(context).textTheme.titleMedium
                  ?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(width: 28),
            for (final page in [
              AppPage.home,
              AppPage.configuration,
              AppPage.settings,
            ])
              Padding(
                padding: const EdgeInsets.only(right: 8),
                child: Semantics(
                  selected: _page == page,
                  child: TextButton.icon(
                    onPressed: () => _go(page),
                    icon: Icon(page.icon),
                    label: Text(page.title),
                  ),
                ),
              ),
            const Spacer(),
            if (widget.announcements case final announcements?
                when announcements.enabled)
              SizedBox(
                key: const ValueKey('desktop-announcement-entry'),
                width: 180,
                child: AnimatedBuilder(
                  animation: announcements,
                  builder: (context, _) => Semantics(
                    container: true,
                    button: true,
                    label: announcements.unreadCount > 0
                        ? '公告，${announcements.unreadCount} 条未读'
                        : '公告',
                    child: TextButton.icon(
                      onPressed: () => showAnnouncementSheet(
                        context,
                        controller: announcements,
                      ),
                      style: TextButton.styleFrom(
                        alignment: Alignment.center,
                        minimumSize: const Size.fromHeight(48),
                        padding: const EdgeInsets.symmetric(horizontal: 16),
                        foregroundColor: Theme.of(context)
                            .colorScheme
                            .onSurfaceVariant,
                        shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(14),
                        ),
                      ),
                      icon: Badge(
                        isLabelVisible: announcements.unreadCount > 0,
                        smallSize: 8,
                        child: const Icon(Icons.campaign_outlined),
                      ),
                      label: const Text('公告'),
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
      const Divider(height: 1),
    ],
  );

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) => CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.escape): () {
          if (_page != AppPage.home) _back();
        },
        const SingleActivator(LogicalKeyboardKey.arrowLeft, alt: true): () {
          if (_page != AppPage.home) _back();
        },
      },
      child: Focus(
        autofocus: true,
        child: Scaffold(
          body: SafeArea(
            child: Column(
              children: [
                if (constraints.maxWidth >= 900) _desktopHeader(context),
                Expanded(
                  child: Padding(
                    padding: EdgeInsets.symmetric(
                      horizontal: constraints.maxWidth >= 900 ? 48 : 0,
                    ),
                    child: Align(
                      alignment: Alignment.topCenter,
                      child: ConstrainedBox(
                        constraints: BoxConstraints(
                          maxWidth: constraints.maxWidth >= 900 ? 1180 : 960,
                        ),
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
              ],
            ),
          ),
        ),
      ),
    ),
  );
}
