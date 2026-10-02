import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class ConfigurationPage extends StatelessWidget {
  const ConfigurationPage({super.key, required this.controller});

  final GuiController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
        return ListView(
          padding: SidraviaLayout.pagePadding(compact: compact),
          children: [
            Text(
              '配置',
              style: compact
                  ? theme.textTheme.headlineSmall
                  : theme.textTheme.displaySmall,
            ),
            const SizedBox(height: 6),
            Text(
              '保存用于校园网认证的登录信息。密码不会在界面中回显。',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 22),
            AnimatedBuilder(
              animation: controller,
              builder: (context, _) => _ConfigurationContent(
                controller: controller,
                compact: compact,
              ),
            ),
          ],
        );
      },
    );
  }
}

class _ConfigurationContent extends StatelessWidget {
  const _ConfigurationContent({
    required this.controller,
    required this.compact,
  });

  final GuiController controller;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final snapshot = controller.snapshot;
    final profiles = snapshot?.profiles ?? const <InstitutionProfile>[];
    final capabilities = controller.capabilities;
    Widget content;
    switch (capabilities.capability) {
      case GuiCapabilityState.bootstrapping ||
          GuiCapabilityState.stale ||
          GuiCapabilityState.failed ||
          GuiCapabilityState.unsupported ||
          GuiCapabilityState.daemonUnavailable:
        content = _MessageCard(title: '服务未连接', detail: '连接恢复后才能查看或修改配置。');
      case GuiCapabilityState.multipleConfigurations:
        content = _MessageCard(
          title: '无法管理多个配置',
          detail: '此版本只支持一个登录配置。请先使用命令行工具处理。',
        );
      case GuiCapabilityState.ambiguousSessions:
        content = _MessageCard(title: '无法管理当前会话', detail: '当前会话关系不明确，未执行任何操作。');
      case GuiCapabilityState.createOnly:
        if (profiles.isEmpty) {
          content = _MessageCard(title: '没有可用的学校配置', detail: '本机服务未提供学校配置。');
        } else {
          content = _CreateConfigurationForm(
            controller: controller,
            profiles: profiles,
            compact: compact,
          );
        }
      case GuiCapabilityState.manageable:
        content = _EditConfigurationForm(
          controller: controller,
          configuration: capabilities.configuration!,
          profiles: profiles,
          compact: compact,
          retainedSession: capabilities.retainedSession,
        );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (controller.notice case final notice?) ...[
          _Notice(text: notice, compact: compact),
          const SizedBox(height: 16),
        ],
        content,
      ],
    );
  }
}

class _CreateConfigurationForm extends StatefulWidget {
  const _CreateConfigurationForm({
    required this.controller,
    required this.profiles,
    required this.compact,
  });

  final GuiController controller;
  final List<InstitutionProfile> profiles;
  final bool compact;

  @override
  State<_CreateConfigurationForm> createState() =>
      _CreateConfigurationFormState();
}

class _CreateConfigurationFormState extends State<_CreateConfigurationForm> {
  late String _profileId = widget.profiles.first.id;
  final _account = TextEditingController();
  final _password = TextEditingController();

  @override
  void didUpdateWidget(covariant _CreateConfigurationForm oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!widget.profiles.any((profile) => profile.id == _profileId)) {
      _profileId = widget.profiles.first.id;
    }
  }

  @override
  void dispose() {
    _account.dispose();
    _password.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _Card(
    title: '登录信息',
    detail: '选择学校并填写用户名。',
    children: [
      _ProfileSelector(
        value: _profileId,
        profiles: widget.profiles,
        enabled: !widget.controller.busy,
        compact: widget.compact,
        onChanged: (value) => setState(() => _profileId = value!),
      ),
      const SizedBox(height: 16),
      SidraviaLayout.limitTextScale(
        compact: widget.compact,
        maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
        child: TextField(
          key: const ValueKey('configuration-account'),
          controller: _account,
          enabled: !widget.controller.busy,
          style: widget.compact ? Theme.of(context).textTheme.bodyMedium : null,
          decoration: InputDecoration(
            labelText: '用户名',
            labelStyle: widget.compact
                ? Theme.of(context).textTheme.bodyMedium
                : null,
          ),
        ),
      ),
      const SizedBox(height: 16),
      _PasswordField(
        fieldKey: const ValueKey('configuration-password'),
        controller: _password,
        enabled: !widget.controller.busy,
        label: '密码（可选）',
        compactLabel: '密码',
        compact: widget.compact,
      ),
      const SizedBox(height: 24),
      FilledButton(
        onPressed: widget.controller.busy
            ? null
            : () async {
                final account = _account.text;
                if (account.isEmpty) {
                  ScaffoldMessenger.of(context)
                      .showSnackBar(const SnackBar(content: Text('请输入用户名。')));
                  return;
                }
                final saved = await widget.controller.createConfiguration(
                  institutionProfileId: _profileId,
                  username: account,
                  password: _password.text,
                );
                if (saved && mounted) _password.clear();
              },
        child: SidraviaLayout.limitTextScale(
          compact: widget.compact,
          maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
          child: Text(
            '保存',
            style: widget.compact
                ? Theme.of(context).textTheme.labelMedium
                : null,
          ),
        ),
      ),
    ],
  );
}

class _EditConfigurationForm extends StatefulWidget {
  const _EditConfigurationForm({
    required this.controller,
    required this.configuration,
    required this.profiles,
    required this.compact,
    required this.retainedSession,
  });

  final GuiController controller;
  final ConfigurationSummary configuration;
  final List<InstitutionProfile> profiles;
  final bool compact;
  final SessionSummary? retainedSession;

  @override
  State<_EditConfigurationForm> createState() => _EditConfigurationFormState();
}

class _EditConfigurationFormState extends State<_EditConfigurationForm> {
  late String? _profileId = _preferredProfileId(
    widget.profiles,
    widget.configuration.institutionProfileId,
  );
  late final _account = TextEditingController(
    text: widget.configuration.username,
  );
  final _password = TextEditingController();
  late String _authoritativeConfigurationId = widget.configuration.id;
  late String _authoritativeProfileId =
      widget.configuration.institutionProfileId;
  late String _authoritativeUsername = widget.configuration.username;

  @override
  void didUpdateWidget(covariant _EditConfigurationForm oldWidget) {
    super.didUpdateWidget(oldWidget);
    final configuration = widget.configuration;
    final replaced = configuration.id != _authoritativeConfigurationId;
    if (replaced) {
      _account.text = configuration.username;
      _password.clear();
    } else if (_account.text == _authoritativeUsername &&
        configuration.username != _authoritativeUsername) {
      _account.text = configuration.username;
    }

    final preferredProfile = _preferredProfileId(
      widget.profiles,
      configuration.institutionProfileId,
    );
    final selectedProfileExists = widget.profiles.any(
      (profile) => profile.id == _profileId,
    );
    if (replaced ||
        !selectedProfileExists ||
        (_profileId == _authoritativeProfileId &&
            configuration.institutionProfileId != _authoritativeProfileId)) {
      _profileId = preferredProfile;
    }

    _authoritativeConfigurationId = configuration.id;
    _authoritativeProfileId = configuration.institutionProfileId;
    _authoritativeUsername = configuration.username;
  }

  Future<void> _confirmResetSession(BuildContext context) async {
    final session = widget.retainedSession;
    if (session == null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('重置会话？'),
        content: const Text('会话将被停止并移除，登录配置和已保存密码保留。'),
        actions: [
          TextButton(
            key: const ValueKey('reset-cancel'),
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const ValueKey('reset-confirm'),
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('重置'),
          ),
        ],
      ),
    );
    if (confirmed != true ||
        !mounted ||
        session.id != widget.retainedSession?.id) {
      return;
    }
    await widget.controller.resetSession(session.id);
  }

  Future<void> _confirmDeleteConfiguration(BuildContext context) async {
    final configuration = widget.configuration;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除配置？'),
        content: const Text('登录配置、关联会话和已保存密码都会被删除，且无法恢复。'),
        actions: [
          TextButton(
            key: const ValueKey('delete-cancel'),
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const ValueKey('delete-confirm'),
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true ||
        !mounted ||
        configuration.id != widget.configuration.id) {
      return;
    }
    await widget.controller.deleteConfiguration(configuration.id);
  }

  @override
  void dispose() {
    _account.dispose();
    _password.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _Card(
    title: '登录信息',
    detail:
        '${widget.configuration.institutionDisplayName} · ${widget.configuration.credentialStored ? '密码已保存' : '未保存密码'}',
    children: [
      if (widget.profiles.isEmpty)
        Text(
          '本机服务未提供学校配置，学校和用户名暂不可编辑。',
          style: Theme.of(context).textTheme.bodyMedium
              ?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant),
        )
      else ...[
        _ProfileSelector(
          value: _profileId!,
          profiles: widget.profiles,
          enabled: !widget.controller.busy,
          compact: widget.compact,
          onChanged: (value) => setState(() => _profileId = value!),
        ),
        const SizedBox(height: 16),
        SidraviaLayout.limitTextScale(
          compact: widget.compact,
          maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
          child: TextField(
            key: const ValueKey('configuration-account'),
            controller: _account,
            enabled: !widget.controller.busy,
            style: widget.compact
                ? Theme.of(context).textTheme.bodyMedium
                : null,
            decoration: InputDecoration(
              labelText: '用户名',
              labelStyle: widget.compact
                  ? Theme.of(context).textTheme.bodyMedium
                  : null,
            ),
          ),
        ),
        const SizedBox(height: 16),
        FilledButton.tonal(
          onPressed: widget.controller.busy
              ? null
              : () async {
                  if (_account.text.isEmpty) {
                    ScaffoldMessenger.of(context)
                        .showSnackBar(const SnackBar(content: Text('请输入用户名。')));
                    return;
                  }
                  await widget.controller.updateConfiguration(
                    configurationId: widget.configuration.id,
                    institutionProfileId: _profileId!,
                    username: _account.text,
                  );
                },
          child: SidraviaLayout.limitTextScale(
            compact: widget.compact,
            maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
            child: Text(
              '保存',
              style: widget.compact
                  ? Theme.of(context).textTheme.labelMedium
                  : null,
            ),
          ),
        ),
      ],
      const Divider(height: 40),
      _PasswordField(
        fieldKey: const ValueKey('configuration-password'),
        controller: _password,
        enabled: !widget.controller.busy,
        label: '新密码（可选）',
        compactLabel: '新密码',
        compact: widget.compact,
      ),
      const SizedBox(height: 16),
      FilledButton(
        onPressed: widget.controller.busy
            ? null
            : () async {
                final saved = await widget.controller.setPassword(
                  configurationId: widget.configuration.id,
                  password: _password.text,
                );
                if (saved && mounted) _password.clear();
              },
        child: SidraviaLayout.limitTextScale(
          compact: widget.compact,
          maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
          child: Text(
            '更新密码',
            style: widget.compact
                ? Theme.of(context).textTheme.labelMedium
                : null,
          ),
        ),
      ),
      const Divider(height: 40),
      SidraviaLayout.limitTextScale(
        compact: widget.compact,
        maxScaleFactor: SidraviaLayout.compactChromeMaxTextScale,
        child: Material(
          type: MaterialType.transparency,
          child: SwitchListTile(
            key: const ValueKey('configuration-auto-login'),
            contentPadding: EdgeInsets.zero,
            value: widget.configuration.autoLogin,
            onChanged: widget.controller.busy
                ? null
                : (value) {
                    unawaited(
                      widget.controller.setAutoLogin(
                        configurationId: widget.configuration.id,
                        autoLogin: value,
                      ),
                    );
                  },
            title: Text('自动登录'),
            subtitle: Text('下次桌面服务启动时生效，不会立即登录。'),
          ),
        ),
      ),
      if (widget.retainedSession != null) ...[
        const Divider(height: 40),
        OutlinedButton.icon(
          key: const ValueKey('reset-session'),
          onPressed: widget.controller.busy
              ? null
              : () => _confirmResetSession(context),
          style: OutlinedButton.styleFrom(
            foregroundColor: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          icon: const Icon(Icons.restart_alt_outlined),
          label: const Text('重置会话'),
        ),
      ],
      const Divider(height: 40),
      OutlinedButton.icon(
        key: const ValueKey('delete-configuration'),
        onPressed: widget.controller.busy
            ? null
            : () => _confirmDeleteConfiguration(context),
        style: OutlinedButton.styleFrom(
          foregroundColor: Theme.of(context).colorScheme.error,
        ),
        icon: const Icon(Icons.delete_outline),
        label: const Text('删除配置'),
      ),
    ],
  );
}

String? _preferredProfileId(
  List<InstitutionProfile> profiles,
  String authoritativeId,
) {
  if (profiles.isEmpty) return null;
  return profiles.any((profile) => profile.id == authoritativeId)
      ? authoritativeId
      : profiles.first.id;
}

class _ProfileSelector extends StatelessWidget {
  const _ProfileSelector({
    required this.value,
    required this.profiles,
    required this.enabled,
    required this.compact,
    required this.onChanged,
  });

  final String value;
  final List<InstitutionProfile> profiles;
  final bool enabled;
  final bool compact;
  final ValueChanged<String?> onChanged;

  @override
  Widget build(BuildContext context) {
    final compactStyle = compact
        ? Theme.of(context).textTheme.bodyMedium
        : null;
    return SidraviaLayout.limitTextScale(
      compact: compact,
      maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
      child: DropdownButtonFormField<String>(
        key: ValueKey<String>('configuration-profile-$value'),
        initialValue: value,
        onChanged: enabled ? onChanged : null,
        style: compactStyle,
        decoration: InputDecoration(labelText: '学校', labelStyle: compactStyle),
        items: profiles
            .map(
              (profile) => DropdownMenuItem(
                value: profile.id,
                child: Text(profile.displayName),
              ),
            )
            .toList(growable: false),
      ),
    );
  }
}

class _PasswordField extends StatefulWidget {
  const _PasswordField({
    required this.fieldKey,
    required this.controller,
    required this.enabled,
    required this.label,
    required this.compactLabel,
    required this.compact,
  });

  final Key fieldKey;
  final TextEditingController controller;
  final bool enabled;
  final String label;
  final String compactLabel;
  final bool compact;

  @override
  State<_PasswordField> createState() => _PasswordFieldState();
}

class _PasswordFieldState extends State<_PasswordField> {
  var _obscured = true;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SidraviaLayout.limitTextScale(
      compact: widget.compact,
      maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
      child: TextField(
        key: widget.fieldKey,
        controller: widget.controller,
        enabled: widget.enabled,
        obscureText: _obscured,
        style: widget.compact ? theme.textTheme.bodyMedium : null,
        decoration: InputDecoration(
          labelText: widget.compact ? widget.compactLabel : widget.label,
          labelStyle: widget.compact ? theme.textTheme.bodyMedium : null,
          helperText: widget.compact ? '可选' : null,
          helperStyle: widget.compact ? theme.textTheme.labelSmall : null,
          suffixIcon: IconButton(
            onPressed: widget.enabled
                ? () => setState(() => _obscured = !_obscured)
                : null,
            tooltip: _obscured ? '显示密码' : '隐藏密码',
            icon: Icon(
              _obscured
                  ? Icons.visibility_outlined
                  : Icons.visibility_off_outlined,
            ),
          ),
        ),
      ),
    );
  }
}

class _MessageCard extends StatelessWidget {
  const _MessageCard({required this.title, required this.detail});
  final String title, detail;

  @override
  Widget build(BuildContext context) =>
      _Card(title: title, detail: detail, children: const []);
}

class _Notice extends StatelessWidget {
  const _Notice({required this.text, required this.compact});
  final String text;
  final bool compact;

  @override
  Widget build(BuildContext context) => Container(
    constraints: const BoxConstraints(maxWidth: 600),
    width: double.infinity,
    padding: const EdgeInsets.all(15),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.errorContainer
          .withValues(alpha: 0.7),
      borderRadius: BorderRadius.circular(14),
    ),
    child: SidraviaLayout.limitTextScale(
      compact: compact,
      maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
      child: Text(
        text,
        style: TextStyle(color: Theme.of(context).colorScheme.onErrorContainer),
      ),
    ),
  );
}

class _Card extends StatelessWidget {
  const _Card({
    required this.title,
    required this.detail,
    required this.children,
  });
  final String title, detail;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final theme = Theme.of(context);
      final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
      final showHeading = !compact || children.isEmpty;
      return Card(
        child: Padding(
          padding: EdgeInsets.all(SidraviaLayout.cardPadding(compact: compact)),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (showHeading) ...[
                SidraviaLayout.limitTextScale(
                  compact: compact,
                  maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
                  child: Text(
                    title,
                    style: compact
                        ? theme.textTheme.titleMedium
                        : theme.textTheme.headlineSmall,
                  ),
                ),
                SizedBox(height: compact ? 6 : 8),
                SidraviaLayout.limitTextScale(
                  compact: compact,
                  maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
                  child: Text(
                    detail,
                    style:
                        (compact
                                ? theme.textTheme.bodySmall
                                : theme.textTheme.bodyMedium)
                            ?.copyWith(height: compact ? 1.4 : 1.5),
                  ),
                ),
              ],
              if (children.isNotEmpty) ...[
                if (showHeading) SizedBox(height: compact ? 16 : 24),
                ...children,
              ],
            ],
          ),
        ),
      );
    },
  );
}
