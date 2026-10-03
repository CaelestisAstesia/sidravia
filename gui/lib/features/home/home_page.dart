import 'package:flutter/material.dart';
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
    this.notice,
  });
  final String institution, username, state, detail, context, glyph;
  final HomeTone tone;
  final String secondaryLabel, primaryLabel;
  final HomeButtonKind primaryKind;
  final bool primaryEnabled;
  final VoidCallback onHeader, onSettings, onSecondary;
  final VoidCallback? onPrimary;
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
    final session = _session;
    final configuration = _configuration;
    final state = _state(session, configuration);
    final canStart =
        controller.capabilities.canCreate || controller.capabilities.canManage;
    final primaryEnabled =
        controller.state == GuiConnectionState.ready &&
        (configuration == null ? controller.capabilities.canCreate : canStart);
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
    final item =
        announcements?.inlineNotice ??
        (announcements == null || announcements!.announcements.isEmpty
            ? null
            : announcements!.announcements.first);
    return HomeView(
      data: HomeViewData(
        institution: configuration?.institutionDisplayName ?? '尚未配置',
        username: configuration?.username ?? '点击添加连接配置',
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
        onSecondary: () => onNavigate(
          configuration == null
              ? AppPage.settings
              : session == null || session.state == 'suspended'
              ? AppPage.configuration
              : AppPage.details,
        ),
        onPrimary: primaryEnabled
            ? () => _primary(session, configuration)
            : null,
        notice: item == null
            ? null
            : HomeNotice(title: item.title, onOpen: () {}),
      ),
      announcementController: announcements,
    );
  }

  _HomeState _state(
    SessionSummary? session,
    ConfigurationSummary? configuration,
  ) {
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
  const HomeView({super.key, required this.data, this.announcementController});
  final HomeViewData data;
  final AnnouncementController? announcementController;

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final padding = width >= 760 ? 36.0 : 20.0;
    final notice = data.notice;
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(padding, 12, padding, 26),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _HomeHeader(data: data),
          _StatusBlock(data: data),
          _HomeActions(
            data: data,
            hasLiveNotice: announcementController != null,
          ),
          if (notice != null && announcementController != null) ...[
            const SizedBox(height: 30),
            const Divider(height: 1),
            AnnouncementEntry(
              controller: announcementController!,
              onOpen: () => showAnnouncementSheet(
                context,
                controller: announcementController!,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _HomeHeader extends StatelessWidget {
  const _HomeHeader({required this.data});
  final HomeViewData data;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 28),
    child: Row(
      children: [
        Expanded(
          child: TextButton(
            onPressed: data.onHeader,
            style: TextButton.styleFrom(
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
                    children: [
                      Text(
                        data.institution,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                          height: 1.2,
                        ),
                      ),
                      Text(
                        data.username,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 12, height: 1.2),
                      ),
                    ],
                  ),
                ),
                const Icon(Icons.chevron_right, size: 16),
              ],
            ),
          ),
        ),
        IconButton(
          tooltip: '设置',
          onPressed: data.onSettings,
          icon: const Icon(Icons.settings_outlined, size: 20),
          padding: EdgeInsets.zero,
          constraints: const BoxConstraints.tightFor(width: 42, height: 42),
        ),
      ],
    ),
  );
}

class _HomeActions extends StatelessWidget {
  const _HomeActions({required this.data, required this.hasLiveNotice});
  final HomeViewData data;
  final bool hasLiveNotice;
  @override
  Widget build(BuildContext context) {
    final primary = data.primaryKind == HomeButtonKind.primary;
    return Column(
      children: [
        SizedBox(
          width: 320,
          child: Row(
            children: [
              Expanded(
                child: _HomeButton(
                  label: data.secondaryLabel,
                  onPressed: data.onSecondary,
                  primary: false,
                ),
              ),
              const SizedBox(width: 9),
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
        if (data.notice != null && !hasLiveNotice) ...[
          const SizedBox(height: 30),
          const Divider(height: 1),
          _Notice(data: data),
        ],
      ],
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.data});
  final HomeViewData data;
  @override
  Widget build(BuildContext context) => TextButton(
    onPressed: data.notice!.onOpen,
    style: TextButton.styleFrom(
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.only(top: 18),
      shape: const RoundedRectangleBorder(),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '校园网公告',
                style: TextStyle(
                  color: AppColors.accent,
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 2),
              Text(
                data.notice!.title,
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 2),
              const Text(
                '点击查看公告页面',
                style: TextStyle(fontSize: 12, height: 1.5),
              ),
            ],
          ),
        ),
        const Icon(Icons.chevron_right, size: 20),
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
  Widget build(BuildContext context) => SizedBox(
    height: 42,
    child: primary
        ? FilledButton(
            onPressed: onPressed,
            style: AppButtonStyles.primary,
            child: Text(label),
          )
        : OutlinedButton(
            onPressed: onPressed,
            style: AppButtonStyles.secondary,
            child: Text(label),
          ),
  );
}

class _StatusBlock extends StatelessWidget {
  const _StatusBlock({required this.data});
  final HomeViewData data;
  @override
  Widget build(BuildContext context) {
    final color = switch (data.tone) {
      HomeTone.success => AppColors.success,
      HomeTone.warning => AppColors.warning,
      HomeTone.error => AppColors.danger,
      HomeTone.idle => AppColors.mutedLight,
    };
    return ConstrainedBox(
      constraints: const BoxConstraints(minHeight: 258),
      child: Padding(
        padding: const EdgeInsets.only(top: 8, left: 8, right: 8),
        child: Column(
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: color.withValues(alpha: .11),
                borderRadius: BorderRadius.circular(13),
                border: Border.all(color: color.withValues(alpha: .28)),
              ),
              alignment: Alignment.center,
              child: Text(
                data.glyph,
                style: TextStyle(
                  color: color,
                  fontSize: 21,
                  fontWeight: FontWeight.w700,
                  height: 1,
                ),
              ),
            ),
            const SizedBox(height: 13),
            Text(
              data.state,
              style: const TextStyle(
                fontSize: 30,
                height: 1.15,
                fontWeight: FontWeight.w600,
                letterSpacing: -.75,
              ),
            ),
            const SizedBox(height: 9),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 300),
              child: Text(
                data.detail,
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 13, height: 1.55),
              ),
            ),
            const SizedBox(height: 14),
            SizedBox(
              height: 92,
              child: Center(
                child: Text(
                  data.context,
                  textAlign: TextAlign.center,
                  style: const TextStyle(fontSize: 12, height: 1.45),
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
