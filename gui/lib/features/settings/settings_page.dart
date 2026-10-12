import 'package:sidravia_gui/shared/theme/design_metrics.dart';
import 'package:sidravia_gui/features/settings/about_dialog.dart';
import 'package:sidravia_gui/shared/widgets/core_restart_dialog.dart';
import 'package:sidravia_gui/shared/widgets/insecure_storage_confirmation.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'integration_settings.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({
    super.key,
    required this.controller,
    required this.onNavigate,
    required this.onBack,
    this.desktopPresence,
  });
  final GuiController controller;
  final DesktopPresence? desktopPresence;
  final ValueChanged<AppPage> onNavigate;
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) {
    final c = controller.capabilities.configuration;
    final appearance = GuiPreferencesScope.maybeOf(context);
    final mode = appearance?.value ?? ThemeMode.system;
    final desktopEnabled =
        appearance != null && (desktopPresence?.enabled ?? false);
    return ListenableBuilder(
      listenable: desktopPresence ?? const AlwaysStoppedAnimation(false),
      builder: (context, _) => DesignPage(
        maxWidth: double.infinity,
        horizontalPadding: 24,
        children: [
          DesignHeader(title: '设置', onBack: onBack),
          if (desktopEnabled) const IntegrationSettings(),
          const DesignSectionTitle('连接'),
          DesignGroup(
            children: [
              DesignRow(
                title: '连接配置',
                subtitle: controller.capabilities.canEditAutoLogin
                    ? controller.capabilities.configurationDescription
                    : controller.capabilities.settingsDisabledReason ??
                          controller.capabilities.configurationDescription,
                onTap: () => onNavigate(AppPage.configuration),
              ),
              DesignRow(
                key: const ValueKey('auto-login-row'),
                title: '启动时自动登录',
                subtitle: '启动Sidravia后自动开始认证',
                trailing: DesignSwitch(
                  key: const ValueKey('auto-login-switch'),
                  label: '启动时自动登录',
                  value: c?.autoLogin ?? false,
                  onChanged:
                      c != null && controller.capabilities.canEditAutoLogin
                      ? (value) => controller.setAutoLogin(
                          configurationId: c.id,
                          autoLogin: value,
                          onInsecureStorageConfirmation: (operation) =>
                              confirmInsecureStorage(context, operation),
                        )
                      : null,
                ),
              ),
              DesignRow(
                key: const ValueKey('auto-reconnect-row'),
                title: '自动重连',
                subtitle: '连接中断后自动尝试重新认证',
                trailing: DesignSwitch(
                  key: const ValueKey('auto-reconnect-switch'),
                  label: '自动重连',
                  value: c?.autoReconnect ?? false,
                  onChanged:
                      c != null && controller.capabilities.canEditAutoReconnect
                      ? (value) => controller.setAutoReconnect(
                          configurationId: c.id,
                          autoReconnect: value,
                          onInsecureStorageConfirmation: (operation) =>
                              confirmInsecureStorage(context, operation),
                        )
                      : null,
                ),
              ),
            ],
          ),
          const SizedBox(height: 24),
          const DesignSectionTitle('公告'),
          DesignGroup(
            children: [
              _AnnouncementSelector<GuiAnnouncementFrequency>(
                dropdownKey: const ValueKey('announcement-frequency-selector'),
                label: '自动显示公告',
                helper: switch (appearance?.announcementFrequency ??
                    GuiAnnouncementFrequency.dailyFirstConnection) {
                  GuiAnnouncementFrequency.everyConnection => '连接成功后显示公告',
                  GuiAnnouncementFrequency.dailyFirstConnection => null,
                  GuiAnnouncementFrequency.manualOnly => '需要时手动查看',
                },
                value:
                    appearance?.announcementFrequency ??
                    GuiAnnouncementFrequency.dailyFirstConnection,
                options: const [
                  (GuiAnnouncementFrequency.everyConnection, '每次连接成功后'),
                  (GuiAnnouncementFrequency.dailyFirstConnection, '每天首次连接成功后'),
                  (GuiAnnouncementFrequency.manualOnly, '仅手动查看'),
                ],
                onChanged: appearance == null
                    ? null
                    : (value) => appearance.announcementFrequency = value,
              ),
              if ((appearance?.announcementFrequency ??
                      GuiAnnouncementFrequency.dailyFirstConnection) !=
                  GuiAnnouncementFrequency.manualOnly)
                _AnnouncementSelector<GuiHiddenAnnouncementBehavior>(
                  dropdownKey: const ValueKey(
                    'hidden-announcement-behavior-selector',
                  ),
                  label: '主窗口隐藏时',
                  helper: switch (appearance?.hiddenAnnouncementBehavior ??
                      GuiHiddenAnnouncementBehavior.notify) {
                    GuiHiddenAnnouncementBehavior.notify =>
                      appearance?.notificationMode == GuiNotificationMode.silent
                          ? '系统通知已关闭，可在首页查看公告'
                          : null,
                    GuiHiddenAnnouncementBehavior.onRestore => '恢复主窗口时显示公告',
                    GuiHiddenAnnouncementBehavior.showImmediately =>
                      '可在主窗口隐藏时直接查看公告',
                  },
                  value:
                      appearance?.hiddenAnnouncementBehavior ??
                      GuiHiddenAnnouncementBehavior.notify,
                  options: const [
                    (GuiHiddenAnnouncementBehavior.showImmediately, '直接显示公告'),
                    (GuiHiddenAnnouncementBehavior.notify, '发送系统通知点击查看'),
                    (GuiHiddenAnnouncementBehavior.onRestore, '恢复主窗口时显示'),
                  ],
                  onChanged: appearance == null
                      ? null
                      : (value) =>
                            appearance.hiddenAnnouncementBehavior = value,
                ),
            ],
          ),
          const SizedBox(height: 24),
          const DesignSectionTitle('应用'),
          DesignGroup(
            children: [
              DesignRow(
                key: const ValueKey('appearance-row'),
                title: '外观模式',
                trailing: AppearanceSelector(
                  value: mode,
                  onChanged: appearance == null
                      ? null
                      : (value) => appearance.value = value,
                ),
              ),
              DesignRow(
                title: '通知提醒',
                trailing: _SettingsDropdown<GuiNotificationMode>(
                  frameKey: const ValueKey('notification-mode-frame'),
                  dropdownKey: const ValueKey('notification-mode-selector'),
                  value:
                      appearance?.notificationMode ??
                      GuiNotificationMode.abnormal,
                  labels: const {
                    GuiNotificationMode.silent: '不提醒',
                    GuiNotificationMode.abnormal: '仅异常',
                    GuiNotificationMode.all: '全部提醒',
                  },
                  onChanged: appearance == null
                      ? null
                      : (value) => appearance.notificationMode = value,
                ),
              ),
              _LogLevelSettingRow(
                key: const ValueKey('log-level-row'),
                controls: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (appearance != null &&
                        controller.runningLogLevel != null &&
                        appearance.logLevel != controller.runningLogLevel) ...[
                      OutlinedButton(
                        style: OutlinedButton.styleFrom(
                          foregroundColor: Theme.of(context).colorScheme.error,
                          side: BorderSide(
                            color: Theme.of(context).colorScheme.error,
                          ),
                          padding: const EdgeInsets.symmetric(horizontal: 8),
                          textStyle: const TextStyle(
                            fontFamily: 'HarmonyOS Sans',
                            fontSize: DesignMetrics.captionFont,
                          ),
                        ),
                        onPressed: controller.canRestartCore
                            ? () => confirmCoreRestart(
                                context,
                                controller,
                                appearance.logLevel,
                              )
                            : null,
                        child: const Text('重启核心'),
                      ),
                      const SizedBox(width: 8),
                    ],
                    _SettingsDropdown<String>(
                      frameKey: const ValueKey('log-level-frame'),
                      dropdownKey: const ValueKey('log-level-selector'),
                      value: appearance?.logLevel ?? 'debug',
                      labels: const {
                        'info': 'INFO',
                        'debug': 'DEBUG',
                        'trace': 'TRACE',
                      },
                      onChanged: appearance == null || controller.busy
                          ? null
                          : (value) async {
                              if (value == 'trace') {
                                final accepted = await showDialog<bool>(
                                  context: context,
                                  builder: (c) => DesignConfirmationDialog(
                                    title: const Text('开启详细日志？'),
                                    content: const Text(
                                      '详细日志包含协议数据报，可能含有账号和认证信息。仅建议排障时开启，请谨慎分享日志。',
                                    ),
                                    actions: [
                                      TextButton(
                                        onPressed: () =>
                                            Navigator.pop(c, false),
                                        child: const Text('取消'),
                                      ),
                                      TextButton(
                                        onPressed: () => Navigator.pop(c, true),
                                        child: const Text('开启'),
                                      ),
                                    ],
                                  ),
                                );
                                if (accepted != true) return;
                              }
                              if (context.mounted) {
                                appearance.logLevel = value;
                              }
                            },
                    ),
                  ],
                ),
              ),
              DesignRow(
                title: '关闭主窗口',
                trailing: _SettingsDropdown<GuiCloseBehavior>(
                  frameKey: const ValueKey('close-behavior-frame'),
                  dropdownKey: const ValueKey('close-behavior-selector'),
                  value:
                      appearance?.closeBehavior ?? GuiCloseBehavior.hideToTray,
                  labels: const {
                    GuiCloseBehavior.hideToTray: '隐藏到托盘',
                    GuiCloseBehavior.exit: '退出程序',
                  },
                  onChanged: desktopEnabled
                      ? (v) => appearance.closeBehavior = v
                      : null,
                ),
              ),
              DesignRow(
                key: const ValueKey('start-hidden-row'),
                title: '启动时隐藏到托盘',
                trailing: DesignSwitch(
                  key: const ValueKey('start-hidden-switch'),
                  label: '启动时隐藏到托盘',
                  value: appearance?.startHidden ?? false,
                  onChanged: desktopEnabled
                      ? (v) => appearance.startHidden = v
                      : null,
                ),
              ),
              DesignRow(
                title: '连接与诊断',
                subtitle: '核心、会话与故障信息',
                onTap: () => onNavigate(AppPage.diagnostics),
              ),
              DesignRow(
                title: '关于 Sidravia',
                subtitle: '版本、许可与项目链接',
                onTap: () => showSidraviaAbout(
                  context,
                  version: controller.snapshot?.daemon.productVersion ?? '暂不可用',
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _AnnouncementSelector<T> extends StatelessWidget {
  const _AnnouncementSelector({
    required this.dropdownKey,
    required this.label,
    required this.value,
    required this.options,
    required this.onChanged,
    required this.helper,
  });
  final Key dropdownKey;
  final String label;
  final String? helper;
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T>? onChanged;
  @override
  Widget build(BuildContext context) => DesignRow(
    title: label,
    subtitle: helper,
    trailing: _SettingsDropdown<T>(
      frameKey: ValueKey('announcement-$label-frame'),
      dropdownKey: dropdownKey,
      value: value,
      labels: {for (final option in options) option.$1: option.$2},
      onChanged: onChanged,
    ),
  );
}

class _LogLevelSettingRow extends StatelessWidget {
  const _LogLevelSettingRow({super.key, required this.controls});
  final Row controls;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final scaler = MediaQuery.textScalerOf(context);
      final title = TextPainter(
        text: const TextSpan(
          text: '日志等级',
          style: TextStyle(
            fontFamily: 'HarmonyOS Sans',
            fontSize: DesignMetrics.bodyFont,
          ),
        ),
        textDirection: Directionality.of(context),
        textScaler: scaler,
      )..layout();
      final titleWidth = title.width;
      title.dispose();
      final restart = TextPainter(
        text: const TextSpan(
          text: '重启核心',
          style: TextStyle(
            fontFamily: 'HarmonyOS Sans',
            fontSize: DesignMetrics.captionFont,
          ),
        ),
        textDirection: Directionality.of(context),
        textScaler: scaler,
      )..layout();
      final restartWidth = (restart.width + 32).clamp(64, double.infinity);
      restart.dispose();
      final dropdownWidth = DesignMetrics.choiceWidth(
        MediaQuery.sizeOf(context).width,
      );
      final controlsWidth =
          dropdownWidth + (controls.children.length > 1 ? restartWidth + 8 : 0);
      if (constraints.maxWidth >= titleWidth + controlsWidth + 48) {
        return DesignRow(
          title: '日志等级',
          subtitle: '重启核心后生效',
          trailing: controls,
        );
      }
      return Material(
        color: Colors.transparent,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const DesignRow(title: '日志等级', subtitle: '重启核心后生效'),
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: Wrap(
                alignment: WrapAlignment.end,
                crossAxisAlignment: WrapCrossAlignment.center,
                spacing: 8,
                runSpacing: 8,
                children: controls.children
                    .where((w) => w is! SizedBox)
                    .toList(),
              ),
            ),
          ],
        ),
      );
    },
  );
}

class AppearanceSelector extends StatelessWidget {
  const AppearanceSelector({
    super.key,
    required this.value,
    required this.onChanged,
  });
  final ThemeMode value;
  final ValueChanged<ThemeMode>? onChanged;
  static const labels = {
    ThemeMode.system: '跟随系统',
    ThemeMode.light: '浅色',
    ThemeMode.dark: '深色',
  };
  @override
  Widget build(BuildContext context) => _SettingsDropdown<ThemeMode>(
    frameKey: const ValueKey('appearance-selector'),
    value: value,
    labels: labels,
    regionKey: (mode) => ValueKey('appearance-text-region-${mode.name}'),
    onChanged: onChanged,
  );
}

class _SettingsDropdown<T> extends StatelessWidget {
  const _SettingsDropdown({
    required this.frameKey,
    required this.value,
    required this.labels,
    required this.onChanged,
    this.dropdownKey,
    this.regionKey,
  });
  final Key frameKey;
  final Key? dropdownKey;
  final T value;
  final Map<T, String> labels;
  final ValueChanged<T>? onChanged;
  final Key Function(T)? regionKey;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final style = TextStyle(
      fontFamily: 'HarmonyOS Sans',
      fontSize: DesignMetrics.captionFont,
      height: 1.35,
      color: onChanged == null ? scheme.onSurfaceVariant : scheme.onSurface,
    );
    double textHeight = 0;
    for (final label in labels.values) {
      final painter = TextPainter(
        text: TextSpan(text: label, style: style),
        textDirection: Directionality.of(context),
        textScaler: MediaQuery.textScalerOf(context),
      )..layout();
      if (painter.height > textHeight) textHeight = painter.height;
      painter.dispose();
    }
    final platform = Theme.of(context).platform;
    final touch =
        platform == TargetPlatform.android || platform == TargetPlatform.iOS;
    return Container(
      key: frameKey,
      width: DesignMetrics.choiceWidth(MediaQuery.sizeOf(context).width),
      height: (textHeight + DesignMetrics.rowPadding).clamp(
        touch ? DesignMetrics.touchTarget : DesignMetrics.choiceMinHeight,
        double.infinity,
      ),
      alignment: Alignment.center,
      padding: const EdgeInsets.symmetric(horizontal: 7),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        border: Border.all(color: scheme.outline),
        borderRadius: BorderRadius.circular(DesignMetrics.choiceRadius),
      ),
      child: DropdownButton<T>(
        key: dropdownKey,
        dropdownColor: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(DesignMetrics.groupRadius),
        elevation: 3,
        value: value,
        isDense: true,
        isExpanded: true,
        alignment: Alignment.center,
        underline: const SizedBox.shrink(),
        style: style,
        icon: const SizedBox(
          width: 24,
          child: Icon(Icons.arrow_drop_down, size: 20),
        ),
        selectedItemBuilder: (context) => labels.entries
            .map(
              (entry) => Center(
                key: regionKey?.call(entry.key),
                child: Text(
                  entry.value,
                  style: style,
                  textAlign: TextAlign.center,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            )
            .toList(),
        items: labels.entries
            .map(
              (entry) => DropdownMenuItem<T>(
                value: entry.key,
                alignment: Alignment.center,
                child: Text(
                  entry.value,
                  style: style,
                  textAlign: TextAlign.center,
                ),
              ),
            )
            .toList(),
        onChanged: onChanged == null
            ? null
            : (selected) {
                if (selected != null) onChanged!(selected);
              },
      ),
    );
  }
}
