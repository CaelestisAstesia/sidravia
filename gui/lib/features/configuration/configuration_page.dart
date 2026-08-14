import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class ConfigurationPage extends StatelessWidget {
  const ConfigurationPage({super.key, required this.controller});

  final GuiController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 52, 24, 48),
      children: [
        Text('配置', style: theme.textTheme.displaySmall),
        const SizedBox(height: 12),
        Text(
          '配置由 daemon 管理；此界面只读取真实的公开摘要，不提供编辑。',
          style: theme.textTheme.titleMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 32),
        AnimatedBuilder(
          animation: controller,
          builder: (context, _) => _ConfigurationContent(
            snapshot: controller.snapshot,
            ready:
                controller.state == GuiConnectionState.ready ||
                controller.state == GuiConnectionState.stale,
          ),
        ),
      ],
    );
  }
}

class _ConfigurationContent extends StatelessWidget {
  const _ConfigurationContent({required this.snapshot, required this.ready});

  final GuiSnapshot? snapshot;
  final bool ready;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final configurations =
        snapshot?.configurations ?? const <ConfigurationSummary>[];
    final title = !ready
        ? '正在等待 daemon 状态'
        : configurations.isEmpty
        ? '尚无登录配置'
        : configurations.length == 1
        ? configurations.single.displayName
        : '存在多个登录配置';
    final detail = !ready
        ? '此界面不会读取、保留或发送凭据。'
        : configurations.isEmpty
        ? '尚未在 daemon 中找到配置；此只读 MVP 不会创建配置。'
        : configurations.length == 1
        ? _singleDetail(configurations.single)
        : '此 GUI MVP 不会选择或修改多个配置。';
    return Container(
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
          Text(title, style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            detail,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
              height: 1.5,
            ),
          ),
          const SizedBox(height: 24),
          const Text('配置仅由 daemon 管理，当前界面不提供编辑或凭据输入。'),
        ],
      ),
    );
  }

  String _singleDetail(ConfigurationSummary value) =>
      '${value.institutionDisplayName} · ${value.username}。已保存的凭据不会显示在此处。';
}
