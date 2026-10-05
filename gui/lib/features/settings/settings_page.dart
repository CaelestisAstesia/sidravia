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
    final label = switch (mode) {
      ThemeMode.system => '跟随系统',
      ThemeMode.light => '浅色',
      ThemeMode.dark => '深色',
    };
    return DesignPage(
      children: [
        DesignHeader(title: '设置', onBack: onBack, backLabel: '返回连接'),
        const DesignSectionTitle('连接'),
        DesignGroup(
          children: [
            DesignRow(
              title: '连接配置',
              subtitle: c == null
                  ? '尚未配置 · 点击添加'
                  : '${c.institutionDisplayName} · ${c.username}',
              onTap: () => onNavigate(AppPage.configuration),
            ),
            DesignRow(
              title: '启动时自动登录',
              subtitle: controller.capabilities.canEditAutoLogin
                  ? '打开 Sidravia 后自动开始认证'
                  : '当前配置或连接状态不允许修改',
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
            const DesignRow(
              title: '自动重连',
              subtitle: '后端能力暂不可用',
              trailing: DesignSwitch(
                label: '自动重连',
                value: false,
                onChanged: null,
              ),
            ),
          ],
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
              subtitle: label,
              trailing: DropdownButton<ThemeMode>(
                value: mode,
                underline: const SizedBox.shrink(),
                style: TextStyle(
                  fontFamily: 'HarmonyOS Sans',
                  fontSize: 11,
                  color: Theme.of(context).colorScheme.onSurface,
                ),
                items: const [
                  DropdownMenuItem(
                    value: ThemeMode.system,
                    child: Text('跟随系统'),
                  ),
                  DropdownMenuItem(value: ThemeMode.light, child: Text('浅色')),
                  DropdownMenuItem(value: ThemeMode.dark, child: Text('深色')),
                ],
                onChanged: appearance == null
                    ? null
                    : (value) {
                        if (value != null) appearance.value = value;
                      },
              ),
            ),
            DesignRow(
              title: '技术诊断',
              subtitle: 'Daemon、Session 与故障信息',
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
        const DesignHelper('外观选择仅在本次运行中生效。'),
      ],
    );
  }
}
