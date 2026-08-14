import 'package:flutter/material.dart';

class HomePage extends StatelessWidget {
  const HomePage({super.key, required this.onOpenConfiguration});

  final VoidCallback onOpenConfiguration;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 52, 24, 48),
      children: [
        Text('连接概览', style: theme.textTheme.displaySmall),
        const SizedBox(height: 12),
        Text(
          '在一个清晰的位置了解 Sidravia 的连接状态。',
          style: theme.textTheme.titleMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 40),
        _ConnectionFocus(onOpenConfiguration: onOpenConfiguration),
        const SizedBox(height: 40),
        Text('下一步', style: theme.textTheme.titleLarge),
        const SizedBox(height: 10),
        Text(
          '完成 daemon 接入后，此处才会显示真实的连接与认证状态。当前界面不会保存或发送任何信息。',
          style: theme.textTheme.bodyLarge?.copyWith(
            height: 1.55,
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }
}

class _ConnectionFocus extends StatelessWidget {
  const _ConnectionFocus({required this.onOpenConfiguration});

  final VoidCallback onOpenConfiguration;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Semantics(
      label: '连接状态：daemon 尚未接入',
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(32),
          gradient: const LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [Color(0xFFDCEBFF), Color(0xFFD8F4F5), Color(0xFFEAE3FF)],
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 6,
                ),
                decoration: BoxDecoration(
                  color: Colors.white.withValues(alpha: 0.72),
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Text('当前状态', style: theme.textTheme.labelLarge),
              ),
              const SizedBox(height: 24),
              Text('daemon 尚未接入', style: theme.textTheme.headlineMedium),
              const SizedBox(height: 10),
              Text(
                '连接控制将在 daemon bootstrap 与 IPC 接入后启用。',
                style: theme.textTheme.bodyLarge?.copyWith(height: 1.5),
              ),
              const SizedBox(height: 28),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  FilledButton(onPressed: null, child: const Text('连接（尚不可用）')),
                  OutlinedButton(
                    onPressed: onOpenConfiguration,
                    child: const Text('查看配置'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
