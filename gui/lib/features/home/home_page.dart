import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

@immutable
class HomeViewData {
  const HomeViewData({
    required this.institution,
    required this.username,
    required this.state,
    required this.detail,
    required this.context,
    required this.glyph,
    required this.tone,
    required this.secondaryLabel,
    required this.primaryLabel,
    required this.primaryKind,
    required this.primaryEnabled,
    required this.onHeader,
    required this.onSettings,
    required this.onSecondary,
    required this.onPrimary,
    this.actionError,
    this.issue,
    this.notice,
  });
  final String institution, username, state, detail, context, glyph;
  final HomeTone tone;
  final String secondaryLabel, primaryLabel;
  final HomeButtonKind primaryKind;
  final bool primaryEnabled;
  final VoidCallback onHeader, onSettings;
  final VoidCallback? onSecondary;
  final VoidCallback? onPrimary;
  final String? actionError;
  final SessionAuthenticationFailure? issue;
  final HomeNotice? notice;
}

enum HomeTone { success, warning, error, idle }

enum HomeButtonKind { primary, secondary }

@immutable
class HomeNotice {
  const HomeNotice({required this.title, required this.onOpen});
  final String title;
  final VoidCallback onOpen;
}

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

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge([controller, ?announcements]),
      builder: (context, _) => _build(context),
    );
  }

  Widget _build(BuildContext context) {
    final session = _session;
    final configuration = _configuration;
    final canRetry =
        controller.state == GuiConnectionState.failed ||
        controller.state == GuiConnectionState.stale;
    final lastConfigurations = controller.snapshot?.configurations;
    final displayedConfiguration =
        configuration ??
        (controller.state != GuiConnectionState.ready &&
                lastConfigurations?.length == 1
            ? lastConfigurations!.single
            : null);
    final state = _state(session, configuration);
    final canStart =
        controller.capabilities.canCreate || controller.capabilities.canManage;
    final primaryEnabled = canRetry
        ? !controller.busy
        : controller.state == GuiConnectionState.ready &&
              (configuration == null
                  ? controller.capabilities.canCreate
                  : canStart);
    final primaryLabel = canRetry
        ? '重试连接'
        : configuration == null
        ? '添加配置'
        : session == null || session.state == 'suspended'
        ? '开始连接'
        : session.state == 'waiting_before_retry'
        ? '立即重试'
        : session.state == 'blocked_by_error'
        ? '重新连接'
        : session.state == 'authenticating'
        ? '取消连接'
        : session.state == 'waiting_for_network'
        ? '取消等待'
        : '断开连接';
    final secondaryLabel = configuration == null
        ? '连接设置'
        : session == null || session.state == 'suspended'
        ? '更改配置'
        : session.state == 'waiting_before_retry'
        ? '停止重试'
        : session.state == 'blocked_by_error'
        ? '更改配置'
        : '连接详情';
    final item =
        announcements?.inlineNotice ??
        (announcements == null || announcements!.announcements.isEmpty
            ? null
            : announcements!.announcements.first);
    final notice = item == null || announcements?.enabled != true
        ? null
        : HomeNotice(
            title: item.title,
            onOpen: () {
              announcements!.markCurrentRead();
              showAnnouncementSheet(context, controller: announcements!);
            },
          );
    return HomeView(
      data: HomeViewData(
        institution:
            displayedConfiguration?.institutionDisplayName ??
            (controller.state == GuiConnectionState.ready ? '尚未配置' : '连接服务'),
        username:
            displayedConfiguration?.username ??
            (controller.state == GuiConnectionState.ready
                ? '点击添加连接配置'
                : '配置状态暂不可用'),
        state: state.title,
        detail: state.detail,
        context: state.context,
        glyph: state.glyph,
        tone: state.tone,
        secondaryLabel: secondaryLabel,
        primaryLabel: primaryLabel,
        primaryEnabled: primaryEnabled,
        primaryKind:
            (session?.state == 'authenticated' ||
                session?.state == 'authenticating' ||
                session?.state == 'waiting_for_network')
            ? HomeButtonKind.secondary
            : HomeButtonKind.primary,
        onHeader: () => onNavigate(AppPage.configuration),
        onSettings: () => onNavigate(AppPage.settings),
        onSecondary:
            session?.state == 'waiting_before_retry' &&
                !controller.capabilities.canManage
            ? null
            : () {
                if (session?.state == 'waiting_before_retry') {
                  if (controller.capabilities.canManage) {
                    controller.stopSession(session!.id);
                  }
                } else {
                  onNavigate(
                    configuration == null
                        ? AppPage.settings
                        : session == null ||
                              [
                                'suspended',
                                'blocked_by_error',
                              ].contains(session.state)
                        ? AppPage.configuration
                        : AppPage.details,
                  );
                }
              },
        onPrimary: primaryEnabled
            ? () {
                if (canRetry) {
                  controller.retry();
                } else {
                  _primary(session, configuration);
                }
              }
            : null,
        actionError:
            controller.notice ??
            (controller.state != GuiConnectionState.ready
                ? switch (controller.state) {
                    GuiConnectionState.bootstrapping => '正在初始化连接服务…',
                    GuiConnectionState.unsupported => '此平台暂不支持真实认证。',
                    GuiConnectionState.stale => '连接服务失联，当前信息可能已过期。',
                    _ => '连接服务不可用，请重试。',
                  }
                : !primaryEnabled
                ? (controller.busy ? '正在处理操作，请稍候。' : '当前配置或会话关系不允许执行连接操作。')
                : null),
        issue: session?.state == 'blocked_by_error'
            ? session?.lastAuthenticationFailure
            : null,
        notice: notice,
      ),
    );
  }

  _HomeState _state(
    SessionSummary? session,
    ConfigurationSummary? configuration,
  ) {
    switch (controller.state) {
      case GuiConnectionState.bootstrapping:
        return const _HomeState(
          '正在连接服务',
          '正在初始化连接服务…',
          '',
          '↻',
          HomeTone.warning,
        );
      case GuiConnectionState.failed:
        return const _HomeState(
          '服务不可用',
          '暂时无法读取连接状态',
          '请重试连接服务',
          '!',
          HomeTone.error,
        );
      case GuiConnectionState.stale:
        return const _HomeState(
          '服务失联',
          '暂时无法确认当前连接状态',
          '账号信息来自上次成功读取',
          '!',
          HomeTone.warning,
        );
      case GuiConnectionState.unsupported:
        return const _HomeState(
          '平台暂不支持',
          '此平台暂不支持真实认证',
          '',
          '—',
          HomeTone.idle,
        );
      case GuiConnectionState.ready:
        break;
    }
    if (configuration == null) {
      return const _HomeState(
        '尚未配置',
        '添加连接配置后即可开始连接',
        '保存机构、账号和密码后即可开始认证',
        '+',
        HomeTone.idle,
      );
    }
    if (session == null) {
      return const _HomeState('未连接', '准备就绪，可以开始连接', '', '—', HomeTone.idle);
    }
    return switch (session.state) {
      'authenticated' => _HomeState(
        '已连接',
        _connectedDetail(session),
        _binding(session),
        '✓',
        HomeTone.success,
      ),
      'authenticating' => const _HomeState(
        '正在认证',
        '正在向校园网提交认证信息…',
        '等待认证结果',
        '↻',
        HomeTone.warning,
      ),
      'waiting_for_network' => const _HomeState(
        '等待网络',
        '暂未发现可用的校园网络',
        '检测到可用网络后会继续认证',
        '!',
        HomeTone.warning,
      ),
      'waiting_before_retry' => const _HomeState(
        '等待重试',
        '认证暂未成功，将自动重试',
        '可以立即重试',
        '↻',
        HomeTone.warning,
      ),
      'blocked_by_error' => _HomeState(
        '认证失败',
        '本次认证没有完成',
        session.lastAuthenticationFailure?.description ?? '',
        '×',
        HomeTone.error,
      ),
      _ => const _HomeState('未连接', '准备就绪，可以开始连接', '', '—', HomeTone.idle),
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
    return binding.localIpv4Address.isEmpty
        ? binding.displayName
        : '${binding.displayName} · ${binding.localIpv4Address}';
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

/// Shared production/preview presentation component. Only [HomeViewData] and
/// callbacks differ between the real controller and the isolated fixture.
class HomeView extends StatelessWidget {
  const HomeView({super.key, required this.data});
  final HomeViewData data;

  @override
  Widget build(BuildContext context) => DesignPage(
    pageKey: const ValueKey('home-page'),
    children: [
      _HomeHeader(data: data),
      _StatusBlock(data: data),
      _HomeActions(data: data),
    ],
  );
}

class _HomeHeader extends StatelessWidget {
  const _HomeHeader({required this.data});
  final HomeViewData data;
  @override
  Widget build(BuildContext context) {
    final platform = Theme.of(context).platform;
    final touch =
        platform == TargetPlatform.android || platform == TargetPlatform.iOS;
    final scaler = MediaQuery.textScalerOf(context);
    final textHeight = scaler.scale(16) * 1.2 + 2 + scaler.scale(12) * 1.2 + 6;
    final height = textHeight > (touch ? 48 : 42)
        ? textHeight.ceilToDouble()
        : (touch ? 48.0 : 42.0);
    return Padding(
      padding: const EdgeInsets.only(bottom: 28),
      child: Row(
        key: const ValueKey('home-header'),
        children: [
          Expanded(
            child: Transform.translate(
              offset: const Offset(-10, 0),
              child: SizedBox(
                height: height,
                child: TextButton(
                  key: const ValueKey('home-configuration-button'),
                  onPressed: data.onHeader,
                  style: TextButton.styleFrom(
                    foregroundColor: Theme.of(context).colorScheme.onSurface,
                    visualDensity: VisualDensity.standard,
                    tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                    alignment: Alignment.centerLeft,
                    padding: const EdgeInsets.fromLTRB(10, 3, 14, 3),
                    minimumSize: const Size(0, 42),
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(10),
                    ),
                  ),
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          mainAxisSize: MainAxisSize.min,
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            Text(
                              data.institution,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                fontSize: 16,
                                fontWeight: FontWeight.w600,
                                height: 1.2,
                              ),
                            ),
                            SizedBox(height: 2),
                            Text(
                              data.username,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                color: Theme.of(context)
                                    .colorScheme
                                    .onSurfaceVariant,
                                fontSize: 12,
                                height: 1.2,
                              ),
                            ),
                          ],
                        ),
                      ),
                      Icon(
                        Icons.chevron_right,
                        size: 16,
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
          SizedBox(width: 12),
          SizedBox(
            width: touch ? 48 : 42,
            height: height,
            child: IconButton(
              key: const ValueKey('home-settings-button'),
              style: IconButton.styleFrom(
                visualDensity: VisualDensity.standard,
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(10),
                ),
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                minimumSize: Size(touch ? 48 : 42, height),
              ),
              tooltip: '设置',
              onPressed: data.onSettings,
              icon: Icon(
                Icons.settings_outlined,
                size: 20,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              padding: EdgeInsets.zero,
              constraints: BoxConstraints.tightFor(
                width: touch ? 48 : 42,
                height: height,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _HomeActions extends StatelessWidget {
  const _HomeActions({required this.data});
  final HomeViewData data;
  @override
  Widget build(BuildContext context) {
    final primary = data.primaryKind == HomeButtonKind.primary;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SizedBox(height: 14),
        Align(
          alignment: Alignment.center,
          child: ConstrainedBox(
            key: const ValueKey('home-actions'),
            constraints: const BoxConstraints(maxWidth: 320),
            child: Row(
              children: [
                Expanded(
                  child: _HomeButton(
                    label: data.secondaryLabel,
                    onPressed: data.onSecondary,
                    primary: false,
                  ),
                ),
                SizedBox(width: 9),
                Expanded(
                  child: _HomeButton(
                    label: data.primaryLabel,
                    onPressed: data.onPrimary,
                    primary: primary,
                  ),
                ),
              ],
            ),
          ),
        ),
        if (data.actionError != null) ...[
          SizedBox(height: 12),
          Text(
            data.actionError!,
            textAlign: TextAlign.center,
            style: TextStyle(
              color: Theme.of(context).colorScheme.error,
              fontSize: 12,
            ),
          ),
        ],
        if (data.notice != null) ...[
          SizedBox(height: 30),
          SizedBox(
            key: ValueKey('home-divider'),
            height: 1,
            child: ColoredBox(
              color: Theme.of(context).colorScheme.outlineVariant,
            ),
          ),
          SizedBox(height: 20),
          HomeNoticeView(notice: data.notice!),
        ],
      ],
    );
  }
}

class HomeNoticeView extends StatelessWidget {
  const HomeNoticeView({super.key, required this.notice});
  final HomeNotice notice;
  @override
  Widget build(BuildContext context) => TextButton(
    key: const ValueKey('home-notice'),
    onPressed: notice.onOpen,
    style: TextButton.styleFrom(
      alignment: Alignment.centerLeft,
      padding: EdgeInsets.zero,
      minimumSize: Size.zero,
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
      foregroundColor: Theme.of(context).colorScheme.onSurface,
      shape: const RoundedRectangleBorder(),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                '校园网公告',
                style: TextStyle(
                  color: Theme.of(context).colorScheme.primary,
                  fontSize: 11,
                  height: 16 / 11,
                  fontWeight: FontWeight.w600,
                ),
              ),
              SizedBox(height: 8),
              Text(
                notice.title,
                style: TextStyle(
                  fontSize: 13,
                  height: 19 / 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              SizedBox(height: 10),
              Text(
                '点击查看公告页面',
                style: TextStyle(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                  fontSize: 12,
                  height: 1.5,
                ),
              ),
            ],
          ),
        ),
        SizedBox(width: 12),
        SizedBox(
          width: 6,
          child: Text(
            '›',
            style: TextStyle(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
              fontSize: 20,
              height: 1.4,
            ),
          ),
        ),
      ],
    ),
  );
}

class _HomeButton extends StatelessWidget {
  const _HomeButton({
    required this.label,
    required this.onPressed,
    required this.primary,
  });
  final String label;
  final VoidCallback? onPressed;
  final bool primary;
  @override
  Widget build(BuildContext context) => DecoratedBox(
    decoration: BoxDecoration(
      borderRadius: BorderRadius.circular(9),
      boxShadow: primary && onPressed != null
          ? [
              BoxShadow(
                color: Theme.of(context).colorScheme.primary
                    .withValues(alpha: .2),
                offset: const Offset(0, 5),
                blurRadius: 14,
              ),
            ]
          : const [],
    ),
    child: ConstrainedBox(
      constraints: BoxConstraints(
        minHeight:
            Theme.of(context).platform == TargetPlatform.android ||
                Theme.of(context).platform == TargetPlatform.iOS
            ? 48
            : 42,
      ),
      child: primary
          ? FilledButton(
              onPressed: onPressed,
              style: AppButtonStyles.primary.copyWith(
                backgroundColor: WidgetStatePropertyAll(
                  Theme.of(context).colorScheme.primary,
                ),
                foregroundColor: WidgetStatePropertyAll(
                  Theme.of(context).colorScheme.onPrimary,
                ),
                textStyle: WidgetStatePropertyAll(
                  TextStyle(
                    fontFamily: 'HarmonyOS Sans',
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                    height: 20 / 13,
                  ),
                ),
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              ),
              child: Text(label),
            )
          : OutlinedButton(
              onPressed: onPressed,
              style: AppButtonStyles.secondary.copyWith(
                backgroundColor: WidgetStatePropertyAll(
                  Theme.of(context).colorScheme.surface,
                ),
                foregroundColor: WidgetStatePropertyAll(
                  Theme.of(context).colorScheme.onSurface,
                ),
                side: WidgetStatePropertyAll(
                  BorderSide(color: Theme.of(context).colorScheme.outline),
                ),
                textStyle: WidgetStatePropertyAll(
                  TextStyle(
                    fontFamily: 'HarmonyOS Sans',
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                    height: 20 / 13,
                  ),
                ),
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              ),
              child: Text(label),
            ),
    ),
  );
}

class _StatusBlock extends StatelessWidget {
  const _StatusBlock({required this.data});
  final HomeViewData data;
  @override
  Widget build(BuildContext context) {
    final color = switch (data.tone) {
      HomeTone.success =>
        Theme.of(context).brightness == Brightness.dark
            ? AppColors.successDark
            : AppColors.success,
      HomeTone.warning => Theme.of(context).colorScheme.tertiary,
      HomeTone.error => Theme.of(context).colorScheme.error,
      HomeTone.idle => Theme.of(context).colorScheme.onSurfaceVariant,
    };
    return ConstrainedBox(
      key: const ValueKey('home-status'),
      constraints: const BoxConstraints(minHeight: 258),
      child: Padding(
        padding: const EdgeInsets.only(top: 8, left: 8, right: 8),
        child: Column(
          children: [
            Container(
              key: const ValueKey('home-mark'),
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: Color.lerp(
                  Theme.of(context).colorScheme.surface,
                  color,
                  .11,
                ),
                borderRadius: BorderRadius.circular(13),
                border: Border.all(
                  color: Color.lerp(
                    Theme.of(context).colorScheme.outline,
                    color,
                    .28,
                  )!,
                ),
              ),
              alignment: Alignment.center,
              child: Text(
                data.glyph,
                style: TextStyle(
                  color: color,
                  fontSize: 21,
                  fontWeight: FontWeight.w700,
                  height: 1,
                  fontFamilyFallback: const ['Segoe UI Symbol'],
                ),
              ),
            ),
            SizedBox(height: 13),
            Text(
              data.state,
              style: TextStyle(
                fontSize: 30,
                height: 1.15,
                fontWeight: FontWeight.w600,
                letterSpacing: -.75,
              ),
            ),
            SizedBox(height: 9),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 300),
              child: Text(
                data.detail,
                textAlign: TextAlign.center,
                style: TextStyle(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                  fontSize: 13,
                  height: 1.55,
                ),
              ),
            ),
            SizedBox(height: 14),
            ConstrainedBox(
              key: const ValueKey('home-context'),
              constraints: const BoxConstraints(minHeight: 92),
              child: Center(
                child: data.issue != null
                    ? _HomeIssue(failure: data.issue!)
                    : Container(
                        constraints: const BoxConstraints(maxWidth: 310),
                        padding: const EdgeInsets.symmetric(
                          vertical: 9,
                          horizontal: 4,
                        ),
                        child: Text(
                          data.context,
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            color: Theme.of(context)
                                .colorScheme
                                .onSurfaceVariant,
                            fontSize: 12,
                            height: 1.45,
                          ),
                        ),
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _HomeState {
  const _HomeState(
    this.title,
    this.detail,
    this.context,
    this.glyph,
    this.tone,
  );
  final String title, detail, context, glyph;
  final HomeTone tone;
}

class _HomeIssue extends StatelessWidget {
  const _HomeIssue({required this.failure});
  final SessionAuthenticationFailure failure;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      constraints: const BoxConstraints(maxWidth: 320),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
      decoration: BoxDecoration(
        color: Color.lerp(scheme.surface, scheme.error, .06),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(
          color: Color.lerp(scheme.outline, scheme.error, .35)!,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            failure.description,
            style: const TextStyle(
              fontSize: 13,
              height: 19 / 13,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 5),
          Text(
            failure.handlingRecommendation,
            style: TextStyle(
              fontSize: 12,
              height: 1.5,
              color: scheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            failure.code,
            style: TextStyle(
              fontSize: 11,
              height: 14 / 11,
              fontFamily: 'monospace',
              color: scheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}
