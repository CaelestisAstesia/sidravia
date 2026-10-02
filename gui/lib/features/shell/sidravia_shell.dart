import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
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

  static const wideLayoutBreakpoint = 760.0;

  @override
  State<SidraviaShell> createState() => _SidraviaShellState();
}

class _SidraviaShellState extends State<SidraviaShell> {
  var _selectedIndex = 0;

  void _selectDestination(int index) {
    setState(() => _selectedIndex = index);
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= SidraviaShell.wideLayoutBreakpoint;
        final content = IndexedStack(
          index: _selectedIndex,
          children: [
            HomePage(
              controller: widget.controller,
              onOpenConfiguration: () => _selectDestination(1),
              announcements: widget.announcements,
            ),
            ConfigurationPage(controller: widget.controller),
            const SettingsPage(),
          ],
        );
        final contentSurface = Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 1180),
            child: content,
          ),
        );
        return Scaffold(
          body: SafeArea(
            child: wide
                ? Column(
                    children: [
                      _DesktopHeader(
                        selectedIndex: _selectedIndex,
                        announcements: widget.announcements,
                      ),
                      const Divider(height: 1),
                      Expanded(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 48),
                          child: contentSurface,
                        ),
                      ),
                    ],
                  )
                : Column(
                    children: [
                      _CompactHeader(
                        selectedIndex: _selectedIndex,
                        announcements: widget.announcements,
                      ),
                      const Divider(height: 1),
                      Expanded(child: contentSurface),
                    ],
                  ),
          ),
          bottomNavigationBar: wide
              ? null
              : NavigationBar(
                  selectedIndex: _selectedIndex,
                  onDestinationSelected: _selectDestination,
                  destinations: [
                    for (final destination in appDestinations)
                      NavigationDestination(
                        icon: Icon(destination.icon),
                        selectedIcon: Icon(destination.selectedIcon),
                        label: destination.label,
                      ),
                  ],
                ),
        );
      },
    );
  }
}

class _CompactHeader extends StatelessWidget {
  const _CompactHeader({required this.selectedIndex, this.announcements});

  final int selectedIndex;
  final AnnouncementController? announcements;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      key: const ValueKey<String>('compact-header'),
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
      child: Row(
        children: [
          Expanded(
            child: Align(
              alignment: Alignment.centerLeft,
              child: SidraviaLayout.limitTextScale(
                compact: true,
                maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
                child: Text(
                  appDestinations[selectedIndex].title,
                  maxLines: 1,
                  style: theme.textTheme.titleSmall,
                ),
              ),
            ),
          ),
          if (announcements case final announcements?
              when announcements.enabled)
            AnnouncementEntryButton(
              controller: announcements,
              compact: true,
              onPressed: () => showAnnouncementSheet(
                context,
                controller: announcements,
                wide: false,
              ),
            ),
        ],
      ),
    );
  }
}

class _DesktopHeader extends StatelessWidget {
  const _DesktopHeader({required this.selectedIndex, this.announcements});

  final int selectedIndex;
  final AnnouncementController? announcements;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const ValueKey<String>('desktop-header'),
      padding: const EdgeInsets.symmetric(horizontal: 48, vertical: 12),
      child: Row(
        children: [
          Text(
            'Sidravia',
            style: Theme.of(context).textTheme.titleMedium
                ?.copyWith(fontWeight: FontWeight.w700),
          ),
          const SizedBox(width: 28),
          for (final (index, destination) in appDestinations.indexed)
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Semantics(
                key: ValueKey<String>('destination-${destination.id}'),
                button: true,
                selected: index == selectedIndex,
                label: destination.label,
                child: TextButton.icon(
                  onPressed: () => context
                      .findAncestorStateOfType<_SidraviaShellState>()
                      ?._selectDestination(index),
                  icon: Icon(
                    index == selectedIndex
                        ? destination.selectedIcon
                        : destination.icon,
                  ),
                  label: Text(destination.label),
                ),
              ),
            ),
          const Spacer(),
          if (announcements case final announcements?
              when announcements.enabled)
            SizedBox(
              width: 180,
              child: AnnouncementEntryButton(
                controller: announcements,
                compact: false,
                onPressed: () => showAnnouncementSheet(
                  context,
                  controller: announcements,
                  wide: true,
                ),
              ),
            ),
        ],
      ),
    );
  }
}
