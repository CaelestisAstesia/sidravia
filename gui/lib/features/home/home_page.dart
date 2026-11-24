import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/navigation/app_destination.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/preferences/gui_preferences.dart';
import 'package:sidravia_gui/desktop/connection_notifications.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_controller.dart';
import 'package:sidravia_gui/features/home/connection_information_view.dart';
import 'package:sidravia_gui/features/home/server_messages_panel.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/features/home/home_view.dart';

export 'package:sidravia_gui/application/connection_presentation.dart'
    show HomeTone, HomeButtonKind;
export 'package:sidravia_gui/features/home/home_view.dart'
    show
        HomeAnnouncementEntry,
        HomeAnnouncementEntryView,
        HomeView,
        HomeViewData;

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
    final snapshot = controller.snapshot;
    final fallback = snapshot?.configurations;
    final configuration =
        controller.capabilities.configuration ??
        (fallback?.length == 1 ? fallback!.single : null);
    final related = snapshot?.sessions
        .where(
          (s) =>
              s.configurationId == configuration?.id && configuration != null,
        )
        .toList();
    final overrideStatus = projectOverrideStatus(
      configuration,
      related ?? const [],
      fresh:
          controller.capabilities.isReady &&
          snapshot?.daemon.status == 'running',
    );
    final unconfigured =
        controller.capabilities.capability == GuiCapabilityState.createOnly;
    final school = announcements?.profile;
    final announcement = school == null
        ? null
        : HomeAnnouncementEntry(
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
        overrideStatus: overrideStatus,
        overrideWarning:
            configuration?.overrideState == 'invalid' ||
            configuration?.overrideState == 'unavailable' ||
            related?.any((s) => s.cleanupRequired) == true,
        announcement: announcement,
        serverMessages: ServerMessagesPanel(controller: controller.business),
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

String? projectOverrideStatus(
  ConfigurationSummary? configuration,
  List<SessionSummary> related, {
  required bool fresh,
}) {
  if (configuration == null) return null;
  final state = configuration.overrideState;
  final session = related.length == 1 ? related.single : null;
  final usesOverride = session?.hasProtocolOverride == true;
  final cleanup = related.any((s) => s.cleanupRequired);
  final String? status;
  if (cleanup &&
      (related.any((s) => s.cleanupRequired && s.hasOverrideDocument) ||
          usesOverride ||
          state == 'enabled' ||
          state == 'empty' ||
          state == 'invalid')) {
    status = state == 'none' || state == 'empty'
        ? '保存的覆写已清除或停用，旧会话清理未完成。请在连接与诊断中移除当前会话。'
        : '覆写配置已更改，旧会话清理未完成。请在连接与诊断中移除当前会话。';
  } else if (usesOverride && session?.state == SessionState.authenticated) {
    status = '当前连接使用自定义协议覆写规则';
  } else if (usesOverride) {
    status = '当前会话使用自定义协议覆写规则';
  } else {
    status = switch (state) {
      'enabled'
          when session == null || session.state == SessionState.suspended =>
        '自定义协议覆写规则已启用',
      'enabled' => '已保存自定义协议覆写规则',
      'invalid' => '自定义协议覆写规则无效',
      'unavailable' => '自定义协议覆写规则暂不可用',
      _ => null,
    };
  }
  return status == null || fresh ? status : '上次读取：$status';
}
