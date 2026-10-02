import 'dart:ui';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
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
  static const _items = <_NavigationItem>[
    _NavigationItem(
      'dashboard',
      '仪表板',
      Icons.space_dashboard_outlined,
      Icons.space_dashboard,
    ),
    _NavigationItem('configuration', '配置', Icons.tune_outlined, Icons.tune),
    _NavigationItem(
      'advanced',
      '高级',
      Icons.analytics_outlined,
      Icons.analytics,
    ),
    _NavigationItem('options', '选项', Icons.settings_outlined, Icons.settings),
  ];

  var _selectedIndex = 0;

  void _select(int index) => setState(() => _selectedIndex = index);

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = SidraviaLayout.usesWideNavigation(constraints.maxWidth);
        final pages = [
          HomePage(
            controller: widget.controller,
            onOpenConfiguration: () => _select(1),
            onOpenAdvanced: () => _select(2),
            announcements: widget.announcements,
          ),
          ConfigurationPage(controller: widget.controller),
          AdvancedPage(controller: widget.controller),
          const SettingsPage(),
        ];
        final content = IndexedStack(index: _selectedIndex, children: pages);
        return Scaffold(
          body: SafeArea(
            bottom: false,
            child: wide
                ? Row(
                    children: [
                      _WideNavigation(
                        items: _items,
                        selectedIndex: _selectedIndex,
                        onSelected: _select,
                        announcements: widget.announcements,
                      ),
                      const VerticalDivider(width: 1),
                      Expanded(child: _contentSurface(content)),
                    ],
                  )
                : Column(
                    children: [
                      _CompactHeader(
                        title: _items[_selectedIndex].label,
                        announcements: widget.announcements,
                      ),
                      Expanded(child: _contentSurface(content)),
                    ],
                  ),
          ),
          bottomNavigationBar: wide
              ? null
              : _GlassNavigation(
                  items: _items,
                  selectedIndex: _selectedIndex,
                  onSelected: _select,
                ),
        );
      },
    );
  }

  Widget _contentSurface(Widget content) => Align(
    alignment: Alignment.topCenter,
    child: ConstrainedBox(
      constraints: const BoxConstraints(
        maxWidth: SidraviaLayout.maxContentWidth,
      ),
      child: content,
    ),
  );
}

class _NavigationItem {
  const _NavigationItem(this.id, this.label, this.icon, this.selectedIcon);

  final String id;
  final String label;
  final IconData icon;
  final IconData selectedIcon;
}

class _WideNavigation extends StatelessWidget {
  const _WideNavigation({
    required this.items,
    required this.selectedIndex,
    required this.onSelected,
    this.announcements,
  });

  final List<_NavigationItem> items;
  final int selectedIndex;
  final ValueChanged<int> onSelected;
  final AnnouncementController? announcements;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SizedBox(
      key: const ValueKey<String>('wide-sidebar'),
      width: 220,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 28, 18, 24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Row(
                children: [
                  Container(
                    width: 34,
                    height: 34,
                    decoration: BoxDecoration(
                      color: theme.colorScheme.primary.withValues(alpha: 0.16),
                      borderRadius: BorderRadius.circular(11),
                    ),
                    child: Icon(
                      Icons.wifi_tethering,
                      color: theme.colorScheme.primary,
                      size: 20,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Flexible(
                    child: Text(
                      'Sidravia',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 34),
            for (final (index, item) in items.indexed)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: _NavigationButton(
                  item: item,
                  selected: selectedIndex == index,
                  onPressed: () => onSelected(index),
                ),
              ),
            const Spacer(),
            if (announcements case final announcement?
                when announcement.enabled)
              AnnouncementEntryButton(
                controller: announcement,
                compact: false,
                onPressed: () => showAnnouncementSheet(
                  context,
                  controller: announcement,
                  wide: true,
                ),
              ),
            const SizedBox(height: 18),
            Text(
              '校园网认证工具',
              textAlign: TextAlign.center,
              style: theme.textTheme.labelSmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _CompactHeader extends StatelessWidget {
  const _CompactHeader({required this.title, this.announcements});

  final String title;
  final AnnouncementController? announcements;

  @override
  Widget build(BuildContext context) => Padding(
    key: const ValueKey<String>('compact-header'),
    padding: const EdgeInsets.fromLTRB(20, 12, 20, 8),
    child: Row(
      children: [
        Expanded(
          child: Text(title, style: Theme.of(context).textTheme.titleSmall),
        ),
        if (announcements case final announcement? when announcement.enabled)
          AnnouncementEntryButton(
            controller: announcement,
            compact: true,
            onPressed: () => showAnnouncementSheet(
              context,
              controller: announcement,
              wide: false,
            ),
          ),
      ],
    ),
  );
}

class _GlassNavigation extends StatelessWidget {
  const _GlassNavigation({
    required this.items,
    required this.selectedIndex,
    required this.onSelected,
  });

  final List<_NavigationItem> items;
  final int selectedIndex;
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      key: const ValueKey<String>('compact-navigation'),
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(28),
        child: BackdropFilter(
          filter: ImageFilter.blur(sigmaX: 18, sigmaY: 18),
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: theme.colorScheme.surface.withValues(alpha: 0.82),
              borderRadius: BorderRadius.circular(28),
              border: Border.all(color: theme.colorScheme.outlineVariant),
              boxShadow: [
                BoxShadow(
                  color: Colors.black.withValues(alpha: 0.12),
                  blurRadius: 24,
                  offset: const Offset(0, 8),
                ),
              ],
            ),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 7),
              child: Row(
                children: [
                  for (final (index, item) in items.indexed)
                    Expanded(
                      child: _NavigationButton(
                        item: item,
                        selected: selectedIndex == index,
                        onPressed: () => onSelected(index),
                        compact: true,
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
}

class _NavigationButton extends StatelessWidget {
  const _NavigationButton({
    required this.item,
    required this.selected,
    required this.onPressed,
    this.compact = false,
  });

  final _NavigationItem item;
  final bool selected;
  final VoidCallback onPressed;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final foreground = selected ? scheme.primary : scheme.onSurfaceVariant;
    final child = compact
        ? Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                selected ? item.selectedIcon : item.icon,
                color: foreground,
                size: 20,
              ),
              const SizedBox(height: 3),
              Text(
                item.label,
                style: Theme.of(context).textTheme.labelSmall
                    ?.copyWith(color: foreground),
              ),
            ],
          )
        : Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                selected ? item.selectedIcon : item.icon,
                color: foreground,
                size: 20,
              ),
              const SizedBox(width: 12),
              Text(
                item.label,
                style: TextStyle(
                  color: foreground,
                  fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                ),
              ),
            ],
          );
    return Semantics(
      button: true,
      selected: selected,
      label: item.label,
      child: Material(
        color: selected
            ? scheme.primary.withValues(alpha: 0.14)
            : Colors.transparent,
        borderRadius: BorderRadius.circular(compact ? 20 : 14),
        child: InkWell(
          key: ValueKey<String>('destination-${item.id}'),
          onTap: onPressed,
          borderRadius: BorderRadius.circular(compact ? 20 : 14),
          child: Padding(
            padding: EdgeInsets.symmetric(
              horizontal: compact ? 7 : 14,
              vertical: compact ? 5 : 14,
            ),
            child: child,
          ),
        ),
      ),
    );
  }
}
