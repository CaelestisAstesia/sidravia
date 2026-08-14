import 'package:flutter/material.dart';

class ConfigurationPage extends StatelessWidget {
  const ConfigurationPage({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 52, 24, 48),
      children: [
        Text('配置', style: theme.textTheme.displaySmall),
        const SizedBox(height: 12),
        Text(
          '首版界面只呈现一个登录配置的形状；真正的配置仍由 daemon 管理。',
          style: theme.textTheme.titleMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 32),
        Container(
          constraints: const BoxConstraints(maxWidth: 560),
          padding: const EdgeInsets.all(24),
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(24),
            border: Border.all(color: theme.dividerColor),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('校园登录', style: theme.textTheme.headlineSmall),
              const SizedBox(height: 8),
              Text(
                'daemon 尚未接入，以下字段不会接受、保留或发送凭据。',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                  height: 1.5,
                ),
              ),
              const SizedBox(height: 24),
              const TextField(
                enabled: false,
                decoration: InputDecoration(labelText: '账号'),
              ),
              const SizedBox(height: 16),
              const TextField(
                enabled: false,
                obscureText: true,
                decoration: InputDecoration(labelText: '密码'),
              ),
              const SizedBox(height: 24),
              FilledButton.icon(
                onPressed: null,
                icon: const Icon(Icons.lock_outline),
                label: const Text('保存并连接（尚不可用）'),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
