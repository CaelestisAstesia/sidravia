import 'package:flutter/material.dart';

import 'package:sidravia_gui/design/sidravia_layout.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({super.key});

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
        final theme = Theme.of(context);
        return ListView(
          padding: SidraviaLayout.pagePadding(compact: compact),
          children: [
            Text(
              '选项',
              style: compact
                  ? theme.textTheme.headlineSmall
                  : theme.textTheme.displaySmall,
            ),
            const SizedBox(height: 6),
            Text(
              '管理界面偏好和桌面行为。',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 22),
            Card(
              child: Column(
                children: [
                  const _OptionHeader(
                    icon: Icons.palette_outlined,
                    title: '外观',
                  ),
                  const Divider(height: 1),
                  ListTile(
                    leading: const Icon(Icons.brightness_auto_outlined),
                    title: const Text('主题模式'),
                    subtitle: const Text('跟随系统设置'),
                    trailing: Chip(label: const Text('系统')),
                  ),
                  ListTile(
                    leading: const Icon(Icons.text_fields_outlined),
                    title: const Text('字体'),
                    subtitle: const Text('HarmonyOS Sans'),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 14),
            Card(
              child: Column(
                children: [
                  const _OptionHeader(
                    icon: Icons.desktop_windows_outlined,
                    title: '桌面行为',
                  ),
                  const Divider(height: 1),
                  const ListTile(
                    leading: Icon(Icons.notifications_none_outlined),
                    title: Text('系统托盘'),
                    subtitle: Text('桌面模式下由系统托盘管理窗口显示状态'),
                  ),
                  const ListTile(
                    leading: Icon(Icons.refresh_outlined),
                    title: Text('状态刷新'),
                    subtitle: Text('连接状态由本机服务持续同步'),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 14),
            Card(
              child: Padding(
                padding: EdgeInsets.all(
                  SidraviaLayout.cardPadding(compact: compact),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '关于 Sidravia',
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      '校园网认证工具',
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                    const SizedBox(height: 16),
                    OutlinedButton.icon(
                      onPressed: () => showLicensePage(
                        context: context,
                        applicationName: 'Sidravia',
                      ),
                      icon: const Icon(Icons.description_outlined),
                      label: const Text('查看许可'),
                    ),
                  ],
                ),
              ),
            ),
          ],
        );
      },
    );
  }
}

class _OptionHeader extends StatelessWidget {
  const _OptionHeader({required this.icon, required this.title});
  final IconData icon;
  final String title;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(18, 16, 18, 14),
    child: Row(
      children: [
        Icon(icon, color: Theme.of(context).colorScheme.primary),
        const SizedBox(width: 10),
        Text(
          title,
          style: Theme.of(context).textTheme.titleMedium
              ?.copyWith(fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}
