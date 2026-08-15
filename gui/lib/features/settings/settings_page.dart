import 'package:flutter/material.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
        return ListView(
          padding: SidraviaLayout.pagePadding(compact: compact),
          children: [
            if (!compact) ...[
              Text('关于', style: theme.textTheme.displaySmall),
              const SizedBox(height: 28),
            ],
            Align(
              alignment: Alignment.centerLeft,
              child: Container(
                constraints: const BoxConstraints(maxWidth: 600),
                padding: EdgeInsets.all(
                  SidraviaLayout.cardPadding(compact: compact),
                ),
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(16),
                  border: Border.all(color: theme.colorScheme.outlineVariant),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Container(
                          width: 44,
                          height: 44,
                          decoration: BoxDecoration(
                            color: theme.colorScheme.primary.withValues(
                              alpha: 0.1,
                            ),
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: Icon(
                            Icons.wifi,
                            color: theme.colorScheme.primary,
                          ),
                        ),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                'Sidravia',
                                style: compact
                                    ? theme.textTheme.titleMedium
                                    : theme.textTheme.titleLarge,
                              ),
                              const SizedBox(height: 2),
                              Text(
                                '校园网认证工具',
                                style:
                                    (compact
                                            ? theme.textTheme.bodySmall
                                            : theme.textTheme.bodyMedium)
                                        ?.copyWith(
                                          color: theme
                                              .colorScheme
                                              .onSurfaceVariant,
                                        ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                    SizedBox(height: compact ? 18 : 28),
                    Text(
                      '字体与许可',
                      style: compact
                          ? theme.textTheme.titleSmall
                          : theme.textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '界面使用 HarmonyOS Sans。',
                      style:
                          (compact
                                  ? theme.textTheme.bodySmall
                                  : theme.textTheme.bodyMedium)
                              ?.copyWith(
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
