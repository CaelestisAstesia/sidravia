import 'package:sidravia_gui/features/home/business_panel.dart';
import 'package:sidravia_gui/shared/widgets/feedback_pill.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/desktop/connection_notifications.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

export 'package:sidravia_gui/application/connection_presentation.dart'
    show HomeTone, HomeButtonKind;

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
    this.onDismissActionError,
    this.issue,
    this.notice,
    this.business,
    this.information,
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
  final VoidCallback? onDismissActionError;
  final SessionAuthenticationFailure? issue;
  final HomeNotice? notice;
  final Widget? business, information;
}

@immutable
class HomeNotice {
  const HomeNotice({required this.title, required this.onOpen, this.feedback});
  final String title;
  final String? feedback;
  final VoidCallback onOpen;
}

class HomePage extends StatelessWidget {
  const HomePage({
    super.key,
    required this.controller,
    required this.onNavigate,
    this.announcements,
    this.connectionNotifications,
    this.desktopPresence,
  });
  final GuiController controller;
  final ValueChanged<AppPage> onNavigate;
  final SchoolAnnouncementController? announcements;
  final ConnectionNotificationController? connectionNotifications;
  final DesktopPresence? desktopPresence;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge([
        controller,
        ?announcements,
        ?desktopPresence,
        ?connectionNotifications,
      ]),
      builder: (context, _) => _build(context),
    );
  }

  Widget _build(BuildContext context) {
    final p = controller.connectionPresentation;
    final configurationId = controller.capabilities.configuration?.id;
    final unconfigured =
        controller.capabilities.capability == GuiCapabilityState.createOnly;
    final school = announcements?.profile;
    final notice = school == null
        ? null
        : HomeNotice(
            title: '公告',
            feedback: announcements?.feedback,
            onOpen: () {
              announcements!.openManually();
            },
          );
    return HomeView(
      data: HomeViewData(
        institution: unconfigured ? 'Sidravia' : p.institution,
        username: unconfigured ? '校园网接入认证' : p.username,
        state: p.statusTitle,
        detail: p.statusDetail,
        context: p.statusContext,
        glyph: p.glyph,
        tone: p.tone,
        secondaryLabel: p.secondaryLabel,
        primaryLabel: p.primaryLabel,
        primaryEnabled: p.primaryEnabled,
        primaryKind: p.primaryKind,
        onHeader: () => _perform(
          GuiConnectionAction(
            configurationId == null
                ? GuiConnectionActionKind.addConfiguration
                : GuiConnectionActionKind.editConfiguration,
            configurationId,
          ),
        ),
        onSettings: () => onNavigate(AppPage.settings),
        onSecondary: p.secondaryEnabled
            ? () => _perform(p.secondaryAction)
            : null,
        onPrimary: p.primaryEnabled ? () => _perform(p.primaryAction) : null,
        actionError:
            p.actionError ??
            connectionNotifications?.feedback ??
            desktopPresence?.feedback ??
            GuiPreferencesScope.maybeOf(context)?.feedback,
        onDismissActionError: p.actionError == null
            ? null
            : () => controller.dismissNotice(p.actionError!),
        issue: p.issue,
        notice: notice,
        business: BusinessPanel(controller: controller.business),
        information: ConnectionInformationView(
          controller: controller.business,
          contextText: p.statusContext,
          detailText: p.statusDetail,
          issue: p.issue,
        ),
      ),
    );
  }

  void _perform(GuiConnectionAction action) {
    switch (action.kind) {
      case GuiConnectionActionKind.addConfiguration:
        onNavigate(AppPage.configuration);
      case GuiConnectionActionKind.editConfiguration:
        final id = action.targetId;
        if (id != null && controller.capabilities.matchesConfiguration(id)) {
          onNavigate(AppPage.configuration);
        }
      case GuiConnectionActionKind.showSettings:
        onNavigate(AppPage.settings);
      case GuiConnectionActionKind.showDetails:
        onNavigate(AppPage.diagnostics);
      default:
        controller.performConnectionAction(action);
    }
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
    maxWidth: 520,
    horizontalPadding: 24,
    children: [
      _HomeHeader(data: data),
      _StatusBlock(data: data),
      _HomeActions(data: data),
      _HomeFeedbackSlot(data: data),
      SizedBox(
        key: const ValueKey('home-divider'),
        height: 1,
        child: ColoredBox(color: Theme.of(context).colorScheme.outlineVariant),
      ),
      const SizedBox(height: 10),
      if (data.notice != null) HomeNoticeView(notice: data.notice!),
      if (data.notice != null) ...[
        const SizedBox(height: 10),
        SizedBox(
          key: const ValueKey('home-messages-divider'),
          height: 1,
          child: ColoredBox(
            color: Theme.of(context).colorScheme.outlineVariant,
          ),
        ),
      ],
      if (data.business != null)
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: data.business!,
        ),
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
      padding: const EdgeInsets.only(bottom: 20),
      child: Row(
        key: const ValueKey('home-header'),
        children: [
          Expanded(
            child: SizedBox(
              width: double.infinity,
              child: SizedBox(
                height: height,
                child: Tooltip(
                  message: '连接配置',
                  child: TextButton(
                    key: const ValueKey('home-configuration-button'),
                    onPressed: data.onHeader,
                    style: TextButton.styleFrom(
                      foregroundColor: Theme.of(context).colorScheme.onSurface,
                      visualDensity: VisualDensity.standard,
                      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      alignment: Alignment.centerLeft,
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 3,
                      ),
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
                        const SizedBox(width: 6),
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
      ],
    );
  }
}

/// One fixed home feedback band, including when empty or locally dismissed.
class _HomeFeedbackSlot extends StatefulWidget {
  const _HomeFeedbackSlot({required this.data});
  final HomeViewData data;
  @override
  State<_HomeFeedbackSlot> createState() => _HomeFeedbackSlotState();
}

class _HomeFeedbackSlotState extends State<_HomeFeedbackSlot> {
  String? _dismissedAction, _dismissedAnnouncement;
  @override
  void didUpdateWidget(covariant _HomeFeedbackSlot oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.data.actionError != widget.data.actionError) {
      _dismissedAction = null;
    }
    if (oldWidget.data.notice?.feedback != widget.data.notice?.feedback) {
      _dismissedAnnouncement = null;
    }
  }

  @override
  Widget build(BuildContext context) {
    final action = widget.data.actionError;
    final announcement = widget.data.notice?.feedback;
    final isAction = action != null && action != _dismissedAction;
    final message = isAction
        ? action
        : announcement != _dismissedAnnouncement
        ? announcement
        : null;
    return SizedBox(
      key: const ValueKey('home-feedback-slot'),
      height: 52 * MediaQuery.textScalerOf(context).scale(14) / 14,
      child: Center(
        child: SingleChildScrollView(
          child: message == null
              ? const SizedBox.shrink()
              : FeedbackPill(
                  key: ValueKey(
                    isAction
                        ? 'home-action-feedback'
                        : 'home-announcement-feedback',
                  ),
                  message: message,
                  onDismiss: () {
                    setState(() {
                      if (isAction) _dismissedAction = message;
                      if (!isAction || message == announcement) {
                        _dismissedAnnouncement = message;
                      }
                    });
                    if (isAction) widget.data.onDismissActionError?.call();
                  },
                ),
        ),
      ),
    );
  }
}

class HomeNoticeView extends StatelessWidget {
  const HomeNoticeView({super.key, required this.notice});
  final HomeNotice notice;
  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 48,
      width: double.infinity,
      child: TextButton(
        key: const ValueKey('home-notice'),
        onPressed: notice.onOpen,
        style: TextButton.styleFrom(
          alignment: Alignment.centerLeft,
          padding: const EdgeInsets.symmetric(horizontal: 8),
          minimumSize: Size.zero,
          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          foregroundColor: Theme.of(context).colorScheme.onSurface,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(10),
          ),
        ),
        child: Row(
          children: [
            Expanded(
              child: Text(
                notice.title.replaceFirst(RegExp(r'^查看'), ''),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 14,
                  height: 20 / 14,
                  fontWeight: FontWeight.w500,
                ),
              ),
            ),
            const SizedBox(width: 6),
            Icon(
              Icons.chevron_right,
              size: 16,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ],
        ),
      ),
    );
  }
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
  Widget build(BuildContext context) => ConstrainedBox(
    constraints: BoxConstraints(
      minHeight:
          Theme.of(context).platform == TargetPlatform.android ||
              Theme.of(context).platform == TargetPlatform.iOS
          ? 48
          : 42,
    ),
    child: primary
        ? FilledButton(onPressed: onPressed, child: Text(label))
        : OutlinedButton(onPressed: onPressed, child: Text(label)),
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
      constraints: BoxConstraints(
        minHeight: 184 * MediaQuery.textScalerOf(context).scale(14) / 14,
      ),
      child: Padding(
        padding: const EdgeInsets.only(top: 8, left: 8, right: 8),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _StatusLabel(title: data.state, color: color),
            const SizedBox(height: 16),
            ConstrainedBox(
              constraints: BoxConstraints(
                minHeight:
                    100 * MediaQuery.textScalerOf(context).scale(14) / 14,
              ),
              child: Center(
                child:
                    data.information ??
                    ConnectionInformationView(
                      detailText: data.detail,
                      contextText: data.context,
                      issue: data.issue,
                    ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Every title gets the same measured bounds, including at enlarged text sizes.
class _StatusLabel extends StatelessWidget {
  const _StatusLabel({required this.title, required this.color});
  final String title;
  final Color color;
  static const titles = [
    '未连接',
    '已连接',
    '正在认证',
    '等待网络',
    '等待重试',
    '认证失败',
    '正在断开',
    '尚未配置',
    '正在连接服务',
    '服务不可用',
    '服务失联',
    '平台暂不支持',
    '检测到多个连接配置',
    '会话关系不明确',
  ];
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final dark = Theme.of(context).brightness == Brightness.dark;
      final style = Theme.of(context).textTheme.titleLarge!.copyWith(
        fontSize: 26,
        height: 1.2,
        fontWeight: FontWeight.w600,
        color: dark ? Colors.white : Colors.black,
      );
      final scaler = MediaQuery.textScalerOf(context);
      final labels = {...titles, title};
      TextPainter measure(String label, double width) => TextPainter(
        text: TextSpan(text: label, style: style),
        textScaler: scaler,
        textDirection: Directionality.of(context),
      )..layout(maxWidth: width);
      double width = 0;
      for (final label in labels) {
        final painter = measure(label, double.infinity);
        if (painter.width > width) width = painter.width;
        painter.dispose();
      }
      width = (width + 32).clamp(0, constraints.maxWidth);
      double height = 0;
      for (final label in labels) {
        final painter = measure(label, (width - 32).clamp(1, double.infinity));
        if (painter.height > height) height = painter.height;
        painter.dispose();
      }
      return Center(
        child: Container(
          key: const ValueKey('home-mark'),
          width: width,
          height: height + 22,
          alignment: Alignment.center,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 11),
          decoration: BoxDecoration(
            color: Color.lerp(
              Theme.of(context).colorScheme.surface,
              color,
              dark ? .24 : .18,
            ),
            border: Border.all(color: color.withValues(alpha: .35)),
            borderRadius: BorderRadius.circular(12),
          ),
          child: Text(title, style: style, textAlign: TextAlign.center),
        ),
      );
    },
  );
}
