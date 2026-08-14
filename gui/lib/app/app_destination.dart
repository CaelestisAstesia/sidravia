import 'package:flutter/material.dart';

enum AppSection { home, sessions, configurations, tray, settings }

@immutable
class AppDestination {
  const AppDestination({
    required this.section,
    required this.id,
    required this.label,
    required this.title,
    required this.icon,
    required this.selectedIcon,
  });

  final AppSection section;
  final String id;
  final String label;
  final String title;
  final IconData icon;
  final IconData selectedIcon;

  ValueKey<String> get sectionKey => ValueKey<String>('section-$id');
}

const appDestinations = <AppDestination>[
  AppDestination(
    section: AppSection.home,
    id: 'home',
    label: '主页',
    title: '主页',
    icon: Icons.home_outlined,
    selectedIcon: Icons.home,
  ),
  AppDestination(
    section: AppSection.sessions,
    id: 'sessions',
    label: '会话',
    title: '会话管理',
    icon: Icons.sync_alt_outlined,
    selectedIcon: Icons.sync_alt,
  ),
  AppDestination(
    section: AppSection.configurations,
    id: 'configurations',
    label: '配置',
    title: '配置',
    icon: Icons.tune_outlined,
    selectedIcon: Icons.tune,
  ),
  AppDestination(
    section: AppSection.tray,
    id: 'tray',
    label: '托盘',
    title: '托盘',
    icon: Icons.dock_outlined,
    selectedIcon: Icons.dock,
  ),
  AppDestination(
    section: AppSection.settings,
    id: 'settings',
    label: '设置',
    title: '设置',
    icon: Icons.settings_outlined,
    selectedIcon: Icons.settings,
  ),
];
