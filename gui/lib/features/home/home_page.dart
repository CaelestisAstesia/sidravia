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
          '认证和停止操作都由 daemon 执行；此界面只显示公开状态。',
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
    final title = _title(controller);
    final detail = _detail(controller);
    final action = _action(controller);
    return Semantics(
      label: '连接状态：$title。$detail',
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
              Text(title, style: theme.textTheme.headlineMedium),
              const SizedBox(height: 10),
              Text(
                detail,
                style: theme.textTheme.bodyLarge?.copyWith(height: 1.5),
              ),
              if (controller.notice case final notice?) ...[
                const SizedBox(height: 12),
                Text(notice, style: theme.textTheme.bodyMedium),
              ],
              const SizedBox(height: 28),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  if (action != null)
                    FilledButton(
                      onPressed: controller.busy ? null : action.onPressed,
                      child: Text(action.label),
                    )
                  else
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

  _HomeAction? _action(GuiController controller) {
    if (controller.state != GuiConnectionState.ready ||
        controller.snapshot == null) {
      return null;
    }
    final snapshot = controller.snapshot!;
    if (snapshot.configurations.length != 1) return null;
    final configuration = snapshot.configurations.single;
    final related = snapshot.sessions
        .where((session) => session.configurationId == configuration.id)
        .toList(growable: false);
    if (related.length > 1 ||
        snapshot.sessions.length != related.length ||
        related.length == 1 && related.single.configurationId == null) {
      return null;
    }
    if (related.isEmpty) {
      return _HomeAction(
        '开始认证',
        () => controller.startConfiguration(configuration.id),
      );
    }
    final session = related.single;
    if (session.state == 'suspended') {
      return _HomeAction(
        '恢复认证',
        () => controller.ensureSessionRunning(session.id),
      );
    }
    if (session.state == 'blocked_by_error' ||
        session.state == 'waiting_before_retry') {
      return _HomeAction('重新认证', () => controller.restartSession(session.id));
    }
    return _HomeAction('停止认证', () => controller.stopSession(session.id));
  }

  String _title(GuiController controller) => switch (controller.state) {
    GuiConnectionState.bootstrapping => '正在接入 daemon',
    GuiConnectionState.ready => _daemonTitle(controller.snapshot!.daemon),
    GuiConnectionState.stale => '状态暂时过期',
    GuiConnectionState.unsupported => '当前平台不支持 GUI bootstrap',
    GuiConnectionState.failed => '无法接入 daemon',
  };

  String _detail(GuiController controller) => switch (controller.state) {
    GuiConnectionState.ready => _daemonDetail(controller.snapshot!),
    GuiConnectionState.stale => '保留上一次完整读取的状态，但连接已失效；请重试以重新获取 daemon 信息。',
    GuiConnectionState.bootstrapping => '正在通过受限的本机 bootstrap 获取连接。',
    _ => '没有可用的运行状态；重试不会读取或保存任何配置文件。',
  };

  String _daemonTitle(DaemonStatus status) =>
      status.status == 'running' ? 'daemon 已就绪' : 'daemon 状态：${status.status}';

  String _daemonDetail(GuiSnapshot snapshot) {
    if (snapshot.configurations.length > 1) {
      return 'daemon 已就绪，但存在多个配置；此 GUI 不会选择其中之一。';
    }
    if (snapshot.sessions.length > 1 ||
        snapshot.sessions.any((session) => session.configurationId == null)) {
      return 'daemon 已就绪，但会话关系不明确；此界面不会执行控制操作。';
    }
    if (snapshot.sessions.isEmpty) {
      return snapshot.configurations.length == 1
          ? '尚未认证，可以开始此配置的认证。'
          : 'daemon 已就绪，但尚无会话；这不代表已经认证。';
    }
    final session = snapshot.sessions.single;
    return _sessionDetail(session);
  }

  String _sessionDetail(SessionSummary session) {
    final label = session.displayName.isEmpty
        ? session.accountName
        : session.displayName;
    return switch (session.state) {
      'authenticated' => '会话 $label 已认证。',
      'authenticating' => '会话 $label 正在认证。',
      'waiting_for_network' => '会话 $label 正在等待网络。',
      'waiting_before_retry' => '会话 $label 将在稍后重试认证。',
      'blocked_by_error' => '会话 $label 因错误未认证。',
      'stopping' => '会话 $label 正在停止。',
      'suspended' => '会话 $label 已暂停，尚未认证。',
      _ => '会话 $label 状态未知。',
    };
  }
}

class _HomeAction {
  const _HomeAction(this.label, this.onPressed);
  final String label;
  final Future<bool> Function() onPressed;
}
