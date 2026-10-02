import 'package:flutter/material.dart';
import 'package:sidravia_gui/app/app_destination.dart';
import 'package:sidravia_gui/application/gui_controller.dart';

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
    final configuration = controller.capabilities.configuration;
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(24, 16, 24, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _Header(onBack: onBack),
          const _SectionTitle('连接'),
          Card(
            child: Column(
              children: [
                _RowButton(
                  title: '连接配置',
                  subtitle: configuration == null
                      ? '尚未配置 · 点击添加'
                      : '${configuration.institutionDisplayName} · ${configuration.username}',
                  onTap: () => onNavigate(AppPage.configuration),
                ),
                const _Divider(),
                _SwitchRow(
                  title: '启动时自动登录',
                  subtitle: '打开 Sidravia 后自动开始认证',
                  value: configuration?.autoLogin ?? false,
                  enabled:
                      configuration != null &&
                      controller.capabilities.canEditAutoLogin,
                  onChanged: configuration == null
                      ? null
                      : (value) => controller.setAutoLogin(
                          configurationId: configuration.id,
                          autoLogin: value,
                        ),
                ),
                const _Divider(),
                const _SwitchRow(
                  title: '自动重连',
                  subtitle: '后端能力暂不可用',
                  value: false,
                  enabled: false,
                  onChanged: null,
                ),
              ],
            ),
          ),
          const SizedBox(height: 24),
          const _SectionTitle('应用'),
          Card(
            child: Column(
              children: [
                const _RowButton(title: '外观模式', subtitle: '跟随系统', onTap: null),
                const _Divider(),
                _RowButton(
                  title: '技术诊断',
                  subtitle: 'Daemon、Session 与故障信息',
                  onTap: () => onNavigate(AppPage.diagnostics),
                ),
                const _Divider(),
                const _RowButton(
                  title: '关于 Sidravia',
                  subtitle: '版本、许可与项目链接',
                  onTap: null,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.onBack});
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 24),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TextButton.icon(
          onPressed: onBack,
          style: TextButton.styleFrom(padding: EdgeInsets.zero),
          icon: const Icon(Icons.chevron_left),
          label: const Text('返回连接'),
        ),
        const SizedBox(height: 9),
        Text(
          '设置',
          style: Theme.of(context).textTheme.headlineSmall
              ?.copyWith(fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);
  final String text;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 9),
    child: Text(
      text.toUpperCase(),
      style: Theme.of(context).textTheme.labelSmall?.copyWith(
        letterSpacing: 1,
        color: Theme.of(context).colorScheme.onSurfaceVariant,
        fontWeight: FontWeight.w700,
      ),
    ),
  );
}

class _Divider extends StatelessWidget {
  const _Divider();
  @override
  Widget build(BuildContext context) =>
      Divider(height: 1, indent: 14, endIndent: 14);
}

class _RowButton extends StatelessWidget {
  const _RowButton({
    required this.title,
    required this.subtitle,
    required this.onTap,
  });
  final String title, subtitle;
  final VoidCallback? onTap;
  @override
  Widget build(BuildContext context) => ListTile(
    enabled: onTap != null,
    title: Text(title),
    subtitle: Text(subtitle),
    trailing: const Icon(Icons.chevron_right),
    onTap: onTap,
  );
}

class _SwitchRow extends StatelessWidget {
  const _SwitchRow({
    required this.title,
    required this.subtitle,
    required this.value,
    required this.enabled,
    required this.onChanged,
  });
  final String title, subtitle;
  final bool value, enabled;
  final ValueChanged<bool>? onChanged;
  @override
  Widget build(BuildContext context) => SwitchListTile(
    title: Text(title),
    subtitle: Text(subtitle),
    value: value,
    onChanged: enabled ? onChanged : null,
  );
}
