import 'package:flutter/material.dart';

import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class HomePage extends StatelessWidget {
  const HomePage({
    super.key,
    required this.controller,
    required this.onOpenConfiguration,
    this.onOpenAdvanced,
    this.announcements,
  });

  final GuiController controller;
  final VoidCallback onOpenConfiguration;
  final VoidCallback? onOpenAdvanced;
  final AnnouncementController? announcements;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
        return AnimatedBuilder(
          animation: controller,
          builder: (context, _) => ListView(
            padding: SidraviaLayout.pagePadding(compact: compact),
            children: [
              _PageHeading(eyebrow: 'Sidravia', title: '仪表板', compact: compact),
              if (announcements case final feed? when feed.enabled) ...[
                const SizedBox(height: 20),
                AnnouncementInlineNotice(controller: feed, compact: compact),
              ],
              const SizedBox(height: 20),
              _ConnectionCard(
                controller: controller,
                compact: compact,
                onOpenConfiguration: onOpenConfiguration,
              ),
              if (onOpenAdvanced != null) ...[
                const SizedBox(height: 12),
                OutlinedButton.icon(
                  key: const ValueKey('open-advanced'),
                  onPressed: onOpenAdvanced,
                  icon: const Icon(Icons.analytics_outlined),
                  label: const Text('查看连接详情'),
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}

class _PageHeading extends StatelessWidget {
  const _PageHeading({
    required this.eyebrow,
    required this.title,
    required this.compact,
  });

  final String eyebrow;
  final String title;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          eyebrow,
          style: theme.textTheme.labelLarge?.copyWith(
            color: theme.colorScheme.primary,
            fontWeight: FontWeight.w600,
          ),
        ),
        const SizedBox(height: 5),
        Text(
          title,
          style: compact
              ? theme.textTheme.headlineSmall
              : theme.textTheme.displaySmall,
        ),
      ],
    );
  }
}

class _ConnectionCard extends StatelessWidget {
  const _ConnectionCard({
    required this.controller,
    required this.compact,
    required this.onOpenConfiguration,
  });

  final GuiController controller;
  final bool compact;
  final VoidCallback onOpenConfiguration;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final presentation = _connectionPresentation(
      controller,
      onOpenConfiguration,
    );
    final config = controller.capabilities.configuration;
    final session = controller.capabilities.retainedSession;
    final institution = config?.institutionDisplayName ?? '未选择机构';
    return Semantics(
      label: '连接状态：${presentation.title}。${presentation.detail}',
      child: Card(
        key: const ValueKey<String>('connection-status-surface'),
        child: Padding(
          padding: EdgeInsets.all(SidraviaLayout.cardPadding(compact: compact)),
          child: Column(
            children: [
              Container(
                width: double.infinity,
                padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerHighest.withValues(
                    alpha: 0.55,
                  ),
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Column(
                  children: [
                    Text(
                      institution,
                      textAlign: TextAlign.center,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      config?.username ?? '尚未配置账户',
                      textAlign: TextAlign.center,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 24),
              Text(
                presentation.title,
                textAlign: TextAlign.center,
                style: compact
                    ? theme.textTheme.headlineSmall
                    : theme.textTheme.headlineMedium,
              ),
              const SizedBox(height: 8),
              Text(
                presentation.detail,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                  height: 1.45,
                ),
              ),
              if (session?.authenticationEstablishedAt
                  case final established?) ...[
                const SizedBox(height: 6),
                Text(
                  '认证开始于 ${_formatTime(established)}',
                  style: theme.textTheme.labelMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              if (controller.notice case final notice?) ...[
                const SizedBox(height: 16),
                _Notice(text: notice),
              ],
              if (presentation.action case final action?) ...[
                const SizedBox(height: 24),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton(
                    key: const ValueKey('connection-primary-action'),
                    onPressed: controller.busy
                        ? null
                        : () {
                            action.onPressed();
                          },
                    child: Text(action.label),
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.text});
  final String text;

  @override
  Widget build(BuildContext context) => Container(
    width: double.infinity,
    padding: const EdgeInsets.all(13),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.errorContainer
          .withValues(alpha: 0.65),
      borderRadius: BorderRadius.circular(12),
    ),
    child: Text(
      text,
      style: TextStyle(color: Theme.of(context).colorScheme.onErrorContainer),
    ),
  );
}

class _ConnectionPresentation {
  const _ConnectionPresentation({
    required this.title,
    required this.detail,
    this.action,
  });
  final String title;
  final String detail;
  final _HomeAction? action;
}

class _HomeAction {
  const _HomeAction(this.label, this.onPressed);
  final String label;
  final Future<bool> Function() onPressed;
}

_ConnectionPresentation _connectionPresentation(
  GuiController controller,
  VoidCallback onOpenConfiguration,
) {
  switch (controller.state) {
    case GuiConnectionState.bootstrapping:
      return const _ConnectionPresentation(
        title: '正在准备连接',
        detail: '正在连接本机服务，请稍候。',
      );
    case GuiConnectionState.stale:
      return _ConnectionPresentation(
        title: '状态暂时不可用',
        detail: '无法取得最新连接状态。',
        action: _HomeAction('重试', () async {
          await controller.retry();
          return true;
        }),
      );
    case GuiConnectionState.failed:
      return _ConnectionPresentation(
        title: '服务不可用',
        detail: '无法连接本机服务。',
        action: _HomeAction('重试', () async {
          await controller.retry();
          return true;
        }),
      );
    case GuiConnectionState.unsupported:
      return const _ConnectionPresentation(
        title: '当前平台暂不支持',
        detail: '本版本暂未提供此平台的本机服务连接。',
      );
    case GuiConnectionState.ready:
      break;
  }
  switch (controller.capabilities.capability) {
    case GuiCapabilityState.createOnly:
      return _ConnectionPresentation(
        title: '尚未配置',
        detail: '添加一个校园网账户后即可开始连接。',
        action: _HomeAction('前往配置', () async {
          onOpenConfiguration();
          return true;
        }),
      );
    case GuiCapabilityState.daemonUnavailable:
      return const _ConnectionPresentation(
        title: '服务未运行',
        detail: '本机认证服务当前不可用。',
      );
    case GuiCapabilityState.multipleConfigurations:
      return const _ConnectionPresentation(
        title: '配置需要整理',
        detail: '检测到多个登录配置，请先使用命令行工具处理。',
      );
    case GuiCapabilityState.ambiguousSessions:
      return const _ConnectionPresentation(
        title: '会话状态不明确',
        detail: '当前会话关系无法确认，未执行操作。',
      );
    case GuiCapabilityState.bootstrapping:
    case GuiCapabilityState.stale:
    case GuiCapabilityState.failed:
    case GuiCapabilityState.unsupported:
      return const _ConnectionPresentation(
        title: '状态暂时不可用',
        detail: '无法取得最新连接状态。',
      );
    case GuiCapabilityState.manageable:
      final configuration = controller.capabilities.configuration!;
      final session = controller.capabilities.retainedSession;
      if (session == null) {
        return _ConnectionPresentation(
          title: '未连接',
          detail: '${configuration.institutionDisplayName} 已准备就绪。',
          action: _HomeAction(
            '开始连接',
            () => controller.startConfiguration(configuration.id),
          ),
        );
      }
      return _sessionPresentation(controller, session);
  }
}

_ConnectionPresentation _sessionPresentation(
  GuiController controller,
  SessionSummary session,
) {
  switch (session.state) {
    case 'authenticated':
      return _ConnectionPresentation(
        title: '已连接',
        detail: '认证成功，当前连接正在保持。',
        action: _HomeAction('断开连接', () => controller.stopSession(session.id)),
      );
    case 'authenticating':
      return _ConnectionPresentation(
        title: '正在认证',
        detail: '正在向校园网提交认证请求。',
        action: _HomeAction('取消连接', () => controller.stopSession(session.id)),
      );
    case 'waiting_for_network':
      return _ConnectionPresentation(
        title: '等待网络',
        detail: '未检测到可用的校园网连接。',
        action: _HomeAction('取消连接', () => controller.stopSession(session.id)),
      );
    case 'waiting_before_retry':
      return _ConnectionPresentation(
        title: '等待重试',
        detail: session.nextRetryAt == null
            ? '认证将在稍后自动重试。'
            : '认证将在 ${_formatTime(session.nextRetryAt!)} 重试。',
        action: _HomeAction(
          '立即重试',
          () => controller.restartSession(session.id),
        ),
      );
    case 'blocked_by_error':
      final detail =
          session.lastAuthenticationFailure?.description ??
          session.stateReason?.description ??
          '认证失败，请检查配置后重试。';
      return _ConnectionPresentation(
        title: '连接失败',
        detail: detail,
        action: _HomeAction(
          '重新连接',
          () => controller.restartSession(session.id),
        ),
      );
    case 'stopping':
      return const _ConnectionPresentation(title: '正在断开', detail: '正在结束当前认证。');
    case 'suspended':
      return _ConnectionPresentation(
        title: '未连接',
        detail: '连接已暂停。',
        action: _HomeAction(
          '开始连接',
          () => controller.ensureSessionRunning(session.id),
        ),
      );
    default:
      return const _ConnectionPresentation(
        title: '状态未知',
        detail: '无法识别当前认证状态。',
      );
  }
}

String _formatTime(DateTime value) {
  final local = value.toLocal();
  final hour = local.hour.toString().padLeft(2, '0');
  final minute = local.minute.toString().padLeft(2, '0');
  return '$hour:$minute';
}
