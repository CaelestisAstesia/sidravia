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
        return Scaffold(
          body: SafeArea(
            child: Column(
              children: [
                _Header(
                  selectedIndex: _selectedIndex,
                  wide: wide,
                  onSelected: _selectDestination,
                ),
                const Divider(height: 1),
                Expanded(
                  child: Center(
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 1100),
                      child: content,
                    ),
                  ),
                ),
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

class _Header extends StatelessWidget {
  const _Header({
    required this.selectedIndex,
    required this.wide,
    required this.onSelected,
  });

  final int selectedIndex;
  final bool wide;
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: wide ? 32 : 24, vertical: 14),
      child: wide
          ? Row(
              children: [
                Text('Sidravia', style: theme.textTheme.titleLarge),
                const Spacer(),
                for (final (index, destination) in appDestinations.indexed)
                  Padding(
                    padding: const EdgeInsets.only(left: 8),
                    child: _HeaderDestination(
                      destination: destination,
                      selected: index == selectedIndex,
                      onPressed: () => onSelected(index),
                    ),
                  ),
              ],
            )
          : Row(
              children: [
                Expanded(
                  child: Text(
                    'Sidravia',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.titleLarge,
                  ),
                ),
                const SizedBox(width: 12),
                Flexible(
                  child: Text(
                    appDestinations[selectedIndex].title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.end,
                    style: theme.textTheme.labelLarge,
                  ),
                ),
              ],
            ),
    );
  }
}

class _HeaderDestination extends StatelessWidget {
  const _HeaderDestination({
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
      child: TextButton.icon(
        onPressed: onPressed,
        style: TextButton.styleFrom(
          backgroundColor: selected
              ? colorScheme.primaryContainer
              : Colors.transparent,
          foregroundColor: selected
              ? colorScheme.primary
              : colorScheme.onSurfaceVariant,
          shape: const StadiumBorder(),
        ),
        icon: Icon(selected ? destination.selectedIcon : destination.icon),
        label: Text(destination.label),
      ),
    );
  }
}
