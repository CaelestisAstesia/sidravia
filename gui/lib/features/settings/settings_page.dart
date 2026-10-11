import 'package:sidravia_gui/shared/widgets/insecure_storage_confirmation.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/desktop/desktop_presence.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({
    super.key,
    required this.controller,
    required this.onNavigate,
    required this.onBack,
    this.desktopPresence,
    this.announcementFeedback,
    this.backLabel = '返回仪表盘',
  });
  final GuiController controller;
  final DesktopPresence? desktopPresence;
  final String? announcementFeedback;
  final String backLabel;
  final ValueChanged<AppPage> onNavigate;
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) {
    final c = controller.capabilities.configuration;
    final appearance = AppearanceScope.maybeOf(context);
    final mode = appearance?.value ?? ThemeMode.system;
    final desktopEnabled =
        appearance != null && (desktopPresence?.enabled ?? false);
    return ListenableBuilder(
      listenable: desktopPresence ?? const AlwaysStoppedAnimation(false),
      builder: (context, _) => DesignPage(
        maxWidth: double.infinity,
        horizontalPadding: 24,
        children: [
          DesignHeader(title: '设置', onBack: onBack, backLabel: backLabel),
          const DesignSectionTitle('连接'),
          DesignGroup(
            children: [
              DesignRow(
                title: '连接配置',
                subtitle: controller.capabilities.configurationDescription,
                onTap: () => onNavigate(AppPage.configuration),
              ),
              DesignRow(
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
          if (!controller.capabilities.canEditAutoLogin)
            Padding(
              padding: const EdgeInsets.only(top: 7),
              child: DesignHelper(
                controller.capabilities.settingsDisabledReason ?? '',
              ),
            ),
          if (c != null)
            const Padding(
              padding: EdgeInsets.only(top: 7),
              child: DesignHelper('修改后从下次连接生效。'),
            ),
          if (controller.notice != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                controller.notice!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          const SizedBox(height: 24),
          const DesignSectionTitle('应用'),
          DesignGroup(
            children: [
              DesignRow(
                title: '外观模式',
                trailing: AppearanceSelector(
                  value: mode,
                  onChanged: appearance == null
                      ? null
                      : (value) => appearance.value = value,
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
                title: '启动时隐藏到托盘',
                subtitle: '下次启动生效；不改变自动登录或当前窗口。',
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
                onTap: () => showAboutDialog(
                  context: context,
                  applicationName: 'Sidravia',
                  applicationVersion:
                      controller.snapshot?.daemon.productVersion ?? '暂不可用',
                  children: [
                    const Text('https://github.com/CaelestisAstesia/sidravia'),
                  ],
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
                  GuiAnnouncementFrequency.everyConnection => '连接成功后显示公告。',
                  GuiAnnouncementFrequency.dailyFirstConnection => null,
                  GuiAnnouncementFrequency.manualOnly => '需要时手动查看。',
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
                    GuiHiddenAnnouncementBehavior.notify => null,
                    GuiHiddenAnnouncementBehavior.onRestore => '恢复主窗口时显示公告。',
                    GuiHiddenAnnouncementBehavior.showImmediately =>
                      '可在主窗口隐藏时直接查看公告。',
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
              if (announcementFeedback != null)
                Padding(
                  padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
                  child: DesignHelper(announcementFeedback!),
                ),
            ],
          ),
          const SizedBox(height: 7),
          if (!desktopEnabled) const DesignHelper('桌面托盘不可用；关闭与启动隐藏设置暂不可操作。'),
          if (desktopPresence?.feedback != null)
            DesignHelper(desktopPresence!.feedback!),
          if (appearance?.feedback != null)
            DesignHelper(appearance!.feedback!)
          else if (appearance != null && !appearance.persistent)
            const DesignHelper('离线演示：外观只保存在演示内存中。'),
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
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      DesignRow(
        title: label,
        trailing: _SettingsDropdown<T>(
          frameKey: ValueKey('announcement-$label-frame'),
          dropdownKey: dropdownKey,
          value: value,
          labels: {for (final option in options) option.$1: option.$2},
          onChanged: onChanged,
        ),
      ),
      if (helper != null)
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
          child: DesignHelper(helper!),
        ),
    ],
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
      fontSize: 11,
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
      width: ((MediaQuery.sizeOf(context).width - 74) * .55).clamp(92, 180),
      height: (textHeight + 12).clamp(touch ? 48 : 30, double.infinity),
      alignment: Alignment.center,
      padding: const EdgeInsets.symmetric(horizontal: 7),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        border: Border.all(color: scheme.outline),
        borderRadius: BorderRadius.circular(7),
      ),
      child: DropdownButton<T>(
        key: dropdownKey,
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
