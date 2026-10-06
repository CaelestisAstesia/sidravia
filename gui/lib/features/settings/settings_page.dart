import 'package:flutter/material.dart';
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
  });
  final GuiController controller;
  final ValueChanged<AppPage> onNavigate;
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) {
    final c = controller.capabilities.configuration;
    final appearance = AppearanceScope.maybeOf(context);
    final mode = appearance?.value ?? ThemeMode.system;
    return DesignPage(
      children: [
        DesignHeader(title: '设置', onBack: onBack, backLabel: '返回连接'),
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
                onChanged: c != null && controller.capabilities.canEditAutoLogin
                    ? (value) => controller.setAutoLogin(
                        configurationId: c.id,
                        autoLogin: value,
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
            child: DesignHelper('修改自动重连会清理当前会话；下次连接使用新的策略。'),
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
              title: '诊断',
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
        const SizedBox(height: 7),
        if (appearance?.feedback != null)
          DesignHelper(appearance!.feedback!)
        else if (appearance != null && !appearance.persistent)
          const DesignHelper('离线演示：外观只保存在演示内存中。'),
      ],
    );
  }
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
  Widget build(BuildContext context) {
    final style = TextStyle(
      fontFamily: 'HarmonyOS Sans',
      fontSize: 11,
      height: 1.35,
      color: Theme.of(context).colorScheme.onSurface,
    );
    final scaler = MediaQuery.textScalerOf(context);
    final painter = TextPainter(
      text: TextSpan(text: '跟随系统', style: style),
      textDirection: Directionality.of(context),
      textScaler: scaler,
    )..layout();
    final textWidth = painter.width;
    painter.dispose();
    final touch = [
      TargetPlatform.android,
      TargetPlatform.iOS,
    ].contains(Theme.of(context).platform);
    return Container(
      key: const ValueKey('appearance-selector'),
      width: (textWidth + 46).clamp(92, double.infinity),
      constraints: BoxConstraints(minHeight: touch ? 48 : 30),
      alignment: Alignment.center,
      padding: const EdgeInsets.symmetric(horizontal: 7),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        border: Border.all(color: Theme.of(context).colorScheme.outline),
        borderRadius: BorderRadius.circular(7),
      ),
      child: DropdownButton<ThemeMode>(
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
                key: ValueKey('appearance-text-region-${entry.key.name}'),
                child: Text(entry.value, textAlign: TextAlign.center),
              ),
            )
            .toList(),
        items: labels.entries
            .map(
              (entry) => DropdownMenuItem(
                value: entry.key,
                alignment: Alignment.center,
                child: Text(entry.value, textAlign: TextAlign.center),
              ),
            )
            .toList(),
        onChanged: onChanged == null
            ? null
            : (mode) {
                if (mode != null) onChanged!(mode);
              },
      ),
    );
  }
}
