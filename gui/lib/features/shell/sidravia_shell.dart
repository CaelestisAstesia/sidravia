import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';

class SidraviaShell extends StatefulWidget {
  const SidraviaShell({super.key});

  static const wideLayoutBreakpoint = 720.0;

  @override
  State<SidraviaShell> createState() => _SidraviaShellState();
}

class _SidraviaShellState extends State<SidraviaShell> {
  var _selectedIndex = 0;

  void _selectDestination(int index) {
    setState(() {
      _selectedIndex = index;
    });
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= SidraviaShell.wideLayoutBreakpoint;
        final content = IndexedStack(
          index: _selectedIndex,
          children: [
            for (final destination in appDestinations)
              _SectionSurface(destination: destination),
          ],
        );

        if (wide) {
          return Scaffold(
            body: Row(
              children: [
                NavigationRail(
                  selectedIndex: _selectedIndex,
                  labelType: NavigationRailLabelType.all,
                  onDestinationSelected: _selectDestination,
                  destinations: [
                    for (final destination in appDestinations)
                      NavigationRailDestination(
                        icon: Icon(destination.icon),
                        selectedIcon: Icon(destination.selectedIcon),
                        label: Text(destination.label),
                      ),
                  ],
                ),
                const VerticalDivider(width: 1),
                Expanded(child: content),
              ],
            ),
          );
        }

        return Scaffold(
          body: content,
          bottomNavigationBar: NavigationBar(
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

class _SectionSurface extends StatelessWidget {
  const _SectionSurface({required this.destination});

  final AppDestination destination;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Text(
        destination.title,
        key: destination.sectionKey,
        style: Theme.of(context).textTheme.headlineMedium,
      ),
    );
  }
}
