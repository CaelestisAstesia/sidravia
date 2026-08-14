import 'package:flutter/material.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 52, 24, 48),
      children: [
        Text('设置', style: theme.textTheme.displaySmall),
        const SizedBox(height: 32),
        Text('启动与路径', style: theme.textTheme.titleLarge),
        const SizedBox(height: 8),
        Text(
          '启动、PATH 与系统集成将在后续切片接入；当前不可用。',
          style: theme.textTheme.bodyLarge?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const Divider(height: 56),
        Text('关于', style: theme.textTheme.titleLarge),
        const SizedBox(height: 8),
        Text(
          'Sidravia 使用 HarmonyOS Sans 字体。',
          style: theme.textTheme.bodyLarge,
        ),
        const SizedBox(height: 12),
        OutlinedButton.icon(
          onPressed: () =>
              showLicensePage(context: context, applicationName: 'Sidravia'),
          icon: const Icon(Icons.description_outlined),
          label: const Text('查看字体许可'),
        ),
      ],
    );
  }
}
