import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class HomePage extends StatelessWidget {
  const HomePage({
    super.key,
    required this.controller,
    required this.onOpenConfiguration,
  });

  final GuiController controller;
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
        AnimatedBuilder(
          animation: controller,
          builder: (context, _) => _ConnectionFocus(
            controller: controller,
            onOpenConfiguration: onOpenConfiguration,
          ),
        ),
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
  const _ConnectionFocus({
    required this.controller,
    required this.onOpenConfiguration,
  });

  final VoidCallback onOpenConfiguration;
  final GuiController controller;

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
              Text(_title(controller), style: theme.textTheme.headlineMedium),
              const SizedBox(height: 10),
              Text(
                _detail(controller),
                style: theme.textTheme.bodyLarge?.copyWith(height: 1.5),
              ),
              const SizedBox(height: 28),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  FilledButton(
                    onPressed: controller.state == GuiConnectionState.ready
                        ? null
                        : controller.retry,
                    child: Text(
                      controller.state == GuiConnectionState.ready
                          ? '连接状态已刷新'
                          : '重试连接',
                    ),
                  ),
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

  String _title(GuiController controller) {
    return switch (controller.state) {
      GuiConnectionState.bootstrapping => '正在接入 daemon',
      GuiConnectionState.ready => _daemonTitle(controller.snapshot!.daemon),
      GuiConnectionState.stale => '状态暂时过期',
      GuiConnectionState.unsupported => '当前平台不支持 GUI bootstrap',
      GuiConnectionState.failed => '无法接入 daemon',
    };
  }

  String _detail(GuiController controller) {
    return switch (controller.state) {
      GuiConnectionState.ready => _daemonDetail(controller.snapshot!),
      GuiConnectionState.stale => '保留上一次已读取的状态；请重试以重新获取 daemon 信息。',
      GuiConnectionState.bootstrapping => '正在通过受限的本机 bootstrap 获取连接。',
      _ => '没有可用的运行状态；重试不会读取或保存任何配置文件。',
    };
  }

  String _daemonTitle(DaemonStatus status) =>
      status.status == 'running' ? 'daemon 已就绪' : 'daemon 状态：${status.status}';

  String _daemonDetail(GuiSnapshot snapshot) {
    final sessionCount = snapshot.sessions.length;
    if (sessionCount == 0) return 'daemon 已就绪，但尚无会话；这不代表已经认证。';
    return 'daemon 已就绪，当前有 $sessionCount 个会话；认证状态以会话状态为准。';
  }
}
