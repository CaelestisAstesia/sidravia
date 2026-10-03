import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class HomePage extends StatelessWidget {
  const HomePage({
    super.key,
    required this.controller,
    required this.onNavigate,
    this.announcements,
  });
  final GuiController controller;
  final ValueChanged<AppPage> onNavigate;
  final AnnouncementController? announcements;

  SessionSummary? get _session => controller.capabilities.retainedSession;
  ConfigurationSummary? get _configuration =>
      controller.capabilities.configuration;

  _HomeState _state() {
    final session = _session;
    if (_configuration == null) {
      return const _HomeState(
        '尚未配置',
        '添加连接配置后即可开始连接',
        '保存机构、账号和密码后即可开始认证',
        '＋',
        _HomeTone.idle,
      );
    }
    if (session == null) {
      return const _HomeState('未连接', '准备就绪，可以开始连接', '', '—', _HomeTone.idle);
    }
    return switch (session.state) {
      'authenticated' => _HomeState(
        '已连接',
        _connectedDetail(session),
        _binding(session),
        '✓',
        _HomeTone.success,
      ),
      'authenticating' => const _HomeState(
        '正在认证',
        '正在向校园网提交认证信息…',
        '等待认证结果',
        '↻',
        _HomeTone.warning,
      ),
      'waiting_for_network' => const _HomeState(
        '等待网络',
        '暂未发现可用的校园网络',
        '检测到可用网络后会继续认证',
        '!',
        _HomeTone.warning,
      ),
      'waiting_before_retry' => const _HomeState(
        '等待重试',
        '认证暂未成功，将自动重试',
        '可以立即重试',
        '↻',
        _HomeTone.warning,
      ),
      'blocked_by_error' => _HomeState(
        '认证失败',
        '本次认证没有完成',
        session.lastAuthenticationFailure?.description ?? '',
        '×',
        _HomeTone.error,
      ),
      _ => const _HomeState('未连接', '准备就绪，可以开始连接', '', '—', _HomeTone.idle),
    };
  }

  static String _connectedDetail(SessionSummary session) {
    final at = session.authenticationEstablishedAt;
    if (at == null) return '认证成功';
    final elapsed = DateTime.now().difference(at.toLocal());
    if (elapsed.inMinutes < 1) return '认证成功 · 刚刚连接';
    return '认证成功 · 已连接 ${elapsed.inHours} 小时 ${elapsed.inMinutes % 60} 分钟';
  }

  static String _binding(SessionSummary session) {
    final binding = session.selectedNetworkBinding;
    if (binding == null) return '网络适配器：暂不可用';
    final ipv4 = binding.localIpv4Address;
    return ipv4.isEmpty
        ? binding.displayName
        : '${binding.displayName} · $ipv4';
  }

  @override
  Widget build(BuildContext context) {
    final state = _state();
    final configuration = _configuration;
    final session = _session;
    final canStart =
        controller.capabilities.canCreate || controller.capabilities.canManage;
    final primaryLabel = configuration == null
        ? '添加配置'
        : session == null || session.state == 'suspended'
        ? '开始连接'
        : session.state == 'waiting_before_retry'
        ? '立即重试'
        : session.state == 'blocked_by_error'
        ? '重新连接'
        : '断开连接';
    final secondaryLabel = configuration == null
        ? '连接设置'
        : session == null || session.state == 'suspended'
        ? '更改配置'
        : '连接详情';
    final primaryEnabled =
        controller.state == GuiConnectionState.ready &&
        (configuration == null ? controller.capabilities.canCreate : canStart);
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          constraints.maxWidth >= 760 ? 56 : 24,
          24,
          constraints.maxWidth >= 760 ? 56 : 24,
          36,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: TextButton(
                    onPressed: () => onNavigate(AppPage.configuration),
                    style: TextButton.styleFrom(
                      alignment: Alignment.centerLeft,
                      padding: EdgeInsets.zero,
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                configuration?.institutionDisplayName ?? '尚未配置',
                                style: Theme.of(context).textTheme.titleMedium
                                    ?.copyWith(fontWeight: FontWeight.w700),
                              ),
                              const SizedBox(height: 2),
                              Text(
                                configuration?.username ?? '点击添加连接配置',
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                            ],
                          ),
                        ),
                        Icon(
                          Icons.chevron_right,
                          size: 22,
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ],
                    ),
                  ),
                ),
                IconButton(
                  tooltip: '设置',
                  onPressed: () => onNavigate(AppPage.settings),
                  icon: const Icon(Icons.settings_outlined),
                ),
              ],
            ),
            const SizedBox(height: 28),
            _StatusBlock(state: state),
            const SizedBox(height: 14),
            Align(
              alignment: Alignment.center,
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 520),
                child: Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        onPressed: () => onNavigate(
                          configuration == null
                              ? AppPage.settings
                              : session == null
                              ? AppPage.configuration
                              : AppPage.details,
                        ),
                        child: Text(secondaryLabel),
                      ),
                    ),
                    const SizedBox(width: 9),
                    Expanded(
                      child: FilledButton(
                        onPressed: primaryEnabled
                            ? () => _primary(session, configuration)
                            : null,
                        child: Text(primaryLabel),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            if (controller.notice case final notice?)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  notice,
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            if (announcements?.enabled ?? false) ...[
              const SizedBox(height: 30),
              const Divider(height: 1),
              AnnouncementEntry(
                controller: announcements!,
                onOpen: () =>
                    showAnnouncementSheet(context, controller: announcements!),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Future<void> _primary(
    SessionSummary? session,
    ConfigurationSummary? configuration,
  ) async {
    if (configuration == null) {
      onNavigate(AppPage.configuration);
      return;
    }
    if (session == null || session.state == 'suspended') {
      await controller.startConfiguration(configuration.id);
      return;
    }
    if (session.state == 'waiting_before_retry' ||
        session.state == 'blocked_by_error') {
      await controller.restartSession(session.id);
      return;
    }
    await controller.stopSession(session.id);
  }
}

enum _HomeTone { success, warning, error, idle }

class _HomeState {
  const _HomeState(
    this.title,
    this.detail,
    this.context,
    this.glyph,
    this.tone,
  );
  final String title, detail, context, glyph;
  final _HomeTone tone;
}

class _StatusBlock extends StatelessWidget {
  const _StatusBlock({required this.state});
  final _HomeState state;
  @override
  Widget build(BuildContext context) {
    final color = switch (state.tone) {
      _HomeTone.success => Colors.green,
      _HomeTone.warning => Colors.orange,
      _HomeTone.error => Theme.of(context).colorScheme.error,
      _HomeTone.idle => Theme.of(context).colorScheme.outline,
    };
    return ConstrainedBox(
      constraints: const BoxConstraints(minHeight: 258),
      child: Column(
        children: [
          Container(
            width: 46,
            height: 46,
            decoration: BoxDecoration(
              color: color.withValues(alpha: .1),
              borderRadius: BorderRadius.circular(13),
              border: Border.all(color: color.withValues(alpha: .3)),
            ),
            alignment: Alignment.center,
            child: Text(
              state.glyph,
              style: TextStyle(
                color: color,
                fontSize: 21,
                fontWeight: FontWeight.w700,
              ),
            ),
          ),
          const SizedBox(height: 14),
          Text(
            state.title,
            style: Theme.of(context).textTheme.headlineMedium
                ?.copyWith(fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 8),
          Text(
            state.detail,
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 14),
          ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 92),
            child: Align(
              alignment: Alignment.topCenter,
              child: state.context.isEmpty
                  ? const SizedBox.shrink()
                  : Text(
                      state.context,
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
            ),
          ),
        ],
      ),
    );
  }
}
