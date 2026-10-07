import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';
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

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge([controller, ?announcements]),
      builder: (context, _) => _build(context),
    );
  }

  Widget _build(BuildContext context) {
    final p = controller.connectionPresentation;
    final configurationId = controller.capabilities.configuration?.id;
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
        institution: p.institution,
        username: p.username,
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
        actionError: p.actionError,
        issue: p.issue,
        notice: notice,
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
        onNavigate(AppPage.details);
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
