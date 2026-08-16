import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class HomePage extends StatelessWidget {
  const HomePage({
    super.key,
    required this.controller,
    required this.onOpenConfiguration,
    this.announcements,
  });

  final GuiController controller;
  final VoidCallback onOpenConfiguration;
  final AnnouncementController? announcements;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
        return ListView(
          padding: SidraviaLayout.pagePadding(compact: compact),
          children: [
            SidraviaLayout.limitTextScale(
              compact: compact,
              maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
              child: Text(
                '校园网',
                style: compact
                    ? theme.textTheme.labelMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      )
                    : theme.textTheme.displaySmall,
              ),
            ),
            SizedBox(height: compact ? 12 : 28),
            AnimatedBuilder(
              animation: controller,
              builder: (context, _) => Align(
                alignment: Alignment.centerLeft,
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 680),
                  child: _ConnectionStatus(
                    controller: controller,
                    onOpenConfiguration: onOpenConfiguration,
                    compact: compact,
                  ),
                ),
              ),
            ),
            if (announcements case final announcements?
                when announcements.enabled) ...[
              const SizedBox(height: 16),
              Align(
                alignment: Alignment.centerLeft,
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 680),
                  child: AnnouncementInlineNotice(
                    controller: announcements,
                    compact: compact,
                  ),
                ),
              ),
            ],
          ],
        );
      },
    );
  }
}

class _ConnectionStatus extends StatelessWidget {
  const _ConnectionStatus({
    required this.controller,
    required this.onOpenConfiguration,
    required this.compact,
  });

  final GuiController controller;
  final VoidCallback onOpenConfiguration;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final presentation = _presentation(controller, onOpenConfiguration);
    return Semantics(
      label: '连接状态：${presentation.title}。${presentation.detail}',
      child: Container(
        key: const ValueKey<String>('connection-status-surface'),
        padding: EdgeInsets.all(compact ? 16 : 28),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(18),
          border: Border.all(color: theme.colorScheme.outlineVariant),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (compact) ...[
              Row(
                children: [
                  _StatusIndicator(presentation: presentation, size: 40),
                  const SizedBox(width: 12),
                  Expanded(
                    child: SidraviaLayout.limitTextScale(
                      compact: compact,
                      maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
                      child: Text(
                        presentation.title,
                        style: theme.textTheme.titleMedium?.copyWith(
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
              if (presentation.action case final _HomeAction action) ...[
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: controller.busy ? null : action.onPressed,
                  child: SidraviaLayout.limitTextScale(
                    compact: compact,
                    maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
                    child: Text(
                      action.label,
                      style: theme.textTheme.labelMedium,
                    ),
                  ),
                ),
              ],
              const SizedBox(height: 12),
              SidraviaLayout.limitTextScale(
                compact: compact,
                maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
                child: Text(
                  presentation.detail,
                  style: theme.textTheme.bodySmall?.copyWith(
                    height: 1.4,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
              if (controller.notice case final notice?) ...[
                const SizedBox(height: 16),
                _StatusNotice(text: notice, compact: compact),
              ],
            ] else ...[
              Row(
                children: [
                  _StatusIndicator(presentation: presentation),
                  const SizedBox(width: 14),
                  Text(
                    '连接状态',
                    style: theme.textTheme.labelLarge?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 24),
              Text(presentation.title, style: theme.textTheme.headlineMedium),
              const SizedBox(height: 8),
              Text(
                presentation.detail,
                style: theme.textTheme.bodyLarge?.copyWith(
                  height: 1.5,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              if (controller.notice case final notice?) ...[
                const SizedBox(height: 16),
                _StatusNotice(text: notice, compact: compact),
              ],
              const SizedBox(height: 28),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  if (presentation.action case final _HomeAction action)
                    FilledButton(
                      onPressed: controller.busy ? null : action.onPressed,
                      child: Text(action.label),
                    ),
                  if (presentation.showConfiguration)
                    OutlinedButton(
                      onPressed: controller.busy ? null : onOpenConfiguration,
                      child: const Text('配置'),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }

  _ConnectionPresentation _presentation(
    GuiController controller,
    VoidCallback onOpenConfiguration,
  ) => switch (controller.state) {
    GuiConnectionState.bootstrapping => const _ConnectionPresentation(
      title: '正在连接…',
      detail: '正在连接本机服务。',
      icon: Icons.sync,
      color: SidraviaColors.progress,
      progress: true,
      showConfiguration: false,
    ),
    GuiConnectionState.ready => _readyPresentation(
      controller,
      onOpenConfiguration,
    ),
    GuiConnectionState.stale => _ConnectionPresentation(
      title: '状态已过期',
      detail: '无法获取最新连接状态。',
      icon: Icons.sync_problem_outlined,
      color: SidraviaColors.warning,
      action: _HomeAction('重试', () async {
        await controller.retry();
        return controller.state == GuiConnectionState.ready;
      }),
    ),
    GuiConnectionState.unsupported => const _ConnectionPresentation(
      title: '当前平台不支持',
      detail: '此版本仅支持 Windows 桌面端。',
      icon: Icons.block_outlined,
      color: SidraviaColors.neutral,
      showConfiguration: false,
    ),
    GuiConnectionState.failed => _ConnectionPresentation(
      title: '服务不可用',
      detail: '无法连接本机服务。',
      icon: Icons.error_outline,
      color: SidraviaColors.danger,
      action: _HomeAction('重试', () async {
        await controller.retry();
        return controller.state == GuiConnectionState.ready;
      }),
    ),
  };

  _ConnectionPresentation _readyPresentation(
    GuiController controller,
    VoidCallback onOpenConfiguration,
  ) {
    final snapshot = controller.snapshot!;
    if (snapshot.daemon.status != 'running') {
      return const _ConnectionPresentation(
        title: '服务不可用',
        detail: '本机服务未运行。',
        icon: Icons.error_outline,
        color: SidraviaColors.danger,
      );
    }
    if (snapshot.configurations.length > 1) {
      return const _ConnectionPresentation(
        title: '状态不可用',
        detail: '检测到多个登录配置，请先使用命令行工具处理。',
        icon: Icons.info_outline,
        color: SidraviaColors.warning,
      );
    }
    if (snapshot.configurations.isEmpty) {
      return _ConnectionPresentation(
        title: '未连接',
        detail: '尚未保存登录配置。',
        icon: Icons.wifi_off_outlined,
        color: SidraviaColors.neutral,
        action: _HomeAction('添加配置', () async {
          onOpenConfiguration();
          return true;
        }),
        showConfiguration: false,
      );
    }
    final configuration = snapshot.configurations.single;
    final related = snapshot.sessions
        .where((session) => session.configurationId == configuration.id)
        .toList(growable: false);
    if (snapshot.sessions.length != related.length || related.length > 1) {
      return const _ConnectionPresentation(
        title: '状态不可用',
        detail: '当前会话关系不明确，未执行任何操作。',
        icon: Icons.info_outline,
        color: SidraviaColors.warning,
      );
    }
    if (related.isEmpty) {
      return _ConnectionPresentation(
        title: '未连接',
        detail: '${configuration.institutionDisplayName} 尚未认证。',
        icon: Icons.wifi_off_outlined,
        color: SidraviaColors.neutral,
        action: _HomeAction(
          '登录',
          () => controller.startConfiguration(configuration.id),
        ),
      );
    }
    return _sessionPresentation(controller, configuration, related.single);
  }

  _ConnectionPresentation _sessionPresentation(
    GuiController controller,
    ConfigurationSummary configuration,
    SessionSummary session,
  ) => switch (session.state) {
    'authenticated' => _ConnectionPresentation(
      title: '已连接',
      detail: '已通过 ${configuration.institutionDisplayName} 认证。',
      icon: Icons.check,
      color: SidraviaColors.connected,
      action: _HomeAction('注销', () => controller.stopSession(session.id)),
    ),
    'authenticating' => _ConnectionPresentation(
      title: '正在认证…',
      detail: '正在使用 ${configuration.institutionDisplayName} 认证。',
      icon: Icons.sync,
      color: SidraviaColors.progress,
      progress: true,
      action: _HomeAction('取消', () => controller.stopSession(session.id)),
    ),
    'waiting_for_network' => _ConnectionPresentation(
      title: '网络不可用',
      detail: '未检测到可用的校园网。',
      icon: Icons.signal_wifi_statusbar_connected_no_internet_4_outlined,
      color: SidraviaColors.warning,
      action: _HomeAction('取消', () => controller.stopSession(session.id)),
    ),
    'waiting_before_retry' => _ConnectionPresentation(
      title: '等待重试…',
      detail: '认证将在稍后自动重试。',
      icon: Icons.schedule_outlined,
      color: SidraviaColors.warning,
      action: _HomeAction('立即重试', () => controller.restartSession(session.id)),
    ),
    'blocked_by_error' => _ConnectionPresentation(
      title: '认证失败',
      detail: _failureDetail(session),
      icon: Icons.error_outline,
      color: SidraviaColors.danger,
      action: _HomeAction('重试', () => controller.restartSession(session.id)),
    ),
    'stopping' => const _ConnectionPresentation(
      title: '正在注销…',
      detail: '正在结束当前认证。',
      icon: Icons.sync,
      color: SidraviaColors.progress,
      progress: true,
    ),
    'suspended' => _ConnectionPresentation(
      title: '未连接',
      detail: '${configuration.institutionDisplayName} 尚未认证。',
      icon: Icons.wifi_off_outlined,
      color: SidraviaColors.neutral,
      action: _HomeAction(
        '登录',
        () => controller.ensureSessionRunning(session.id),
      ),
    ),
    _ => const _ConnectionPresentation(
      title: '状态不可用',
      detail: '无法识别当前认证状态。',
      icon: Icons.help_outline,
      color: SidraviaColors.warning,
    ),
  };

  String _failureDetail(SessionSummary session) {
    final detail =
        session.lastAuthenticationFailure?.description ??
        session.stateReason?.description;
    return detail == null || detail.trim().isEmpty
        ? '请检查用户名、密码和网络后重试。'
        : detail;
  }
}

class _StatusNotice extends StatelessWidget {
  const _StatusNotice({required this.text, required this.compact});

  final String text;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(10),
      ),
      child: SidraviaLayout.limitTextScale(
        compact: compact,
        maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
        child: Text(
          text,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onErrorContainer,
          ),
        ),
      ),
    );
  }
}

class _StatusIndicator extends StatelessWidget {
  const _StatusIndicator({required this.presentation, this.size = 44});

  final _ConnectionPresentation presentation;
  final double size;

  @override
  Widget build(BuildContext context) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(
      color: presentation.color.withValues(alpha: 0.1),
      shape: BoxShape.circle,
    ),
    alignment: Alignment.center,
    child: presentation.progress
        ? SizedBox(
            width: 20,
            height: 20,
            child: CircularProgressIndicator(
              strokeWidth: 2.2,
              color: presentation.color,
            ),
          )
        : Icon(presentation.icon, size: 22, color: presentation.color),
  );
}

class _ConnectionPresentation {
  const _ConnectionPresentation({
    required this.title,
    required this.detail,
    required this.icon,
    required this.color,
    this.action,
    this.progress = false,
    this.showConfiguration = true,
  });

  final String title;
  final String detail;
  final IconData icon;
  final Color color;
  final _HomeAction? action;
  final bool progress;
  final bool showConfiguration;
}

class _HomeAction {
  const _HomeAction(this.label, this.onPressed);

  final String label;
  final Future<bool> Function() onPressed;
}
