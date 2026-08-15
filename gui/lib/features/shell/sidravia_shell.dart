import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/configuration/configuration_page.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/features/settings/settings_page.dart';

class SidraviaShell extends StatefulWidget {
  const SidraviaShell({super.key, required this.controller});

  final GuiController controller;

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
            ),
            ConfigurationPage(controller: widget.controller),
            const SettingsPage(),
          ],
        );
        final contentSurface = Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 1000),
            child: content,
          ),
        );
        return Scaffold(
          body: SafeArea(
            child: wide
                ? Row(
                    children: [
                      _Sidebar(
                        selectedIndex: _selectedIndex,
                        onSelected: _selectDestination,
                      ),
                      const VerticalDivider(width: 1),
                      Expanded(
                        child: Padding(
                          padding: const EdgeInsets.only(left: 32, right: 24),
                          child: contentSurface,
                        ),
                      ),
                    ],
                  )
                : Column(
                    children: [
                      _CompactHeader(selectedIndex: _selectedIndex),
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

class _Sidebar extends StatelessWidget {
  const _Sidebar({required this.selectedIndex, required this.onSelected});

  final int selectedIndex;
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SizedBox(
      key: const ValueKey<String>('wide-sidebar'),
      width: 220,
      child: ColoredBox(
        color: theme.scaffoldBackgroundColor,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 24, 16, 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 12),
                child: Text('Sidravia', style: theme.textTheme.titleLarge),
              ),
              const SizedBox(height: 32),
              for (final (index, destination) in appDestinations.indexed)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: _SidebarDestination(
                    destination: destination,
                    selected: index == selectedIndex,
                    onPressed: () => onSelected(index),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _CompactHeader extends StatelessWidget {
  const _CompactHeader({required this.selectedIndex});

  final int selectedIndex;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      key: const ValueKey<String>('compact-header'),
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
      child: Align(
        alignment: Alignment.centerLeft,
        child: Text(
          appDestinations[selectedIndex].title,
          maxLines: 1,
          style: theme.textTheme.titleSmall,
        ),
      ),
    );
  }
}

class _SidebarDestination extends StatelessWidget {
  const _SidebarDestination({
    required this.destination,
    required this.selected,
    required this.onPressed,
  });

  final AppDestination destination;
  final bool selected;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Semantics(
      key: ValueKey<String>('destination-${destination.id}'),
      container: true,
      button: true,
      selected: selected,
      label: destination.label,
      child: SizedBox(
        width: double.infinity,
        child: TextButton.icon(
          onPressed: onPressed,
          style: TextButton.styleFrom(
            alignment: Alignment.centerLeft,
            minimumSize: const Size.fromHeight(48),
            padding: const EdgeInsets.symmetric(horizontal: 16),
            backgroundColor: selected
                ? colorScheme.primary.withValues(alpha: 0.1)
                : Colors.transparent,
            foregroundColor: selected
                ? colorScheme.primary
                : colorScheme.onSurfaceVariant,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(14),
            ),
          ),
          icon: Icon(selected ? destination.selectedIcon : destination.icon),
          label: Text(destination.label),
        ),
      ),
    );
  }
}
