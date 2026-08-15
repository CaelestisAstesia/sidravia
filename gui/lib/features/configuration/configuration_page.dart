import 'package:flutter/material.dart';
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
            if (!compact) ...[
              Text('配置', style: theme.textTheme.displaySmall),
              const SizedBox(height: 10),
              Text(
                '保存用于校园网认证的登录信息。密码不会在界面中回显。',
                style: theme.textTheme.bodyLarge?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 28),
            ],
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
    final ready = controller.state == GuiConnectionState.ready;
    final configurations =
        snapshot?.configurations ?? const <ConfigurationSummary>[];
    final profiles = snapshot?.profiles ?? const <InstitutionProfile>[];
    Widget content;
    if (!ready) {
      content = _MessageCard(title: '服务未连接', detail: '连接恢复后才能查看或修改配置。');
    } else if (configurations.length > 1) {
      content = _MessageCard(
        title: '无法管理多个配置',
        detail: '此版本只支持一个登录配置。请先使用命令行工具处理。',
      );
    } else if (profiles.isEmpty) {
      content = _MessageCard(
        title: configurations.isEmpty ? '没有可用的学校配置' : '无法编辑当前配置',
        detail: '本机服务未提供学校配置。',
      );
    } else if (configurations.isEmpty) {
      content = _CreateConfigurationForm(
        controller: controller,
        profiles: profiles,
        compact: compact,
      );
    } else {
      content = _EditConfigurationForm(
        controller: controller,
        configuration: configurations.single,
        profiles: profiles,
        compact: compact,
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (controller.notice case final notice?) ...[
          _Notice(text: notice),
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
        onChanged: (value) => setState(() => _profileId = value!),
      ),
      const SizedBox(height: 16),
      TextField(
        key: const ValueKey('configuration-account'),
        controller: _account,
        enabled: !widget.controller.busy,
        decoration: const InputDecoration(labelText: '用户名'),
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
        child: const Text('保存'),
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
  });

  final GuiController controller;
  final ConfigurationSummary configuration;
  final List<InstitutionProfile> profiles;
  final bool compact;

  @override
  State<_EditConfigurationForm> createState() => _EditConfigurationFormState();
}

class _EditConfigurationFormState extends State<_EditConfigurationForm> {
  late String _profileId =
      widget.profiles.any(
        (profile) => profile.id == widget.configuration.institutionProfileId,
      )
      ? widget.configuration.institutionProfileId
      : widget.profiles.first.id;
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

    final preferredProfile =
        widget.profiles.any(
          (profile) => profile.id == configuration.institutionProfileId,
        )
        ? configuration.institutionProfileId
        : widget.profiles.first.id;
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
      _ProfileSelector(
        value: _profileId,
        profiles: widget.profiles,
        enabled: !widget.controller.busy,
        onChanged: (value) => setState(() => _profileId = value!),
      ),
      const SizedBox(height: 16),
      TextField(
        key: const ValueKey('configuration-account'),
        controller: _account,
        enabled: !widget.controller.busy,
        decoration: const InputDecoration(labelText: '用户名'),
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
                  institutionProfileId: _profileId,
                  username: _account.text,
                );
              },
        child: const Text('保存'),
      ),
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
        child: const Text('更新密码'),
      ),
    ],
  );
}

class _ProfileSelector extends StatelessWidget {
  const _ProfileSelector({
    required this.value,
    required this.profiles,
    required this.enabled,
    required this.onChanged,
  });

  final String value;
  final List<InstitutionProfile> profiles;
  final bool enabled;
  final ValueChanged<String?> onChanged;

  @override
  Widget build(BuildContext context) => DropdownButtonFormField<String>(
    key: ValueKey<String>('configuration-profile-$value'),
    initialValue: value,
    onChanged: enabled ? onChanged : null,
    decoration: const InputDecoration(labelText: '学校'),
    items: profiles
        .map(
          (profile) => DropdownMenuItem(
            value: profile.id,
            child: Text(profile.displayName),
          ),
        )
        .toList(growable: false),
  );
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
  Widget build(BuildContext context) => TextField(
    key: widget.fieldKey,
    controller: widget.controller,
    enabled: widget.enabled,
    obscureText: _obscured,
    decoration: InputDecoration(
      labelText: widget.compact ? widget.compactLabel : widget.label,
      helperText: widget.compact ? '可选' : null,
      suffixIcon: IconButton(
        onPressed: widget.enabled
            ? () => setState(() => _obscured = !_obscured)
            : null,
        tooltip: _obscured ? '显示密码' : '隐藏密码',
        icon: Icon(
          _obscured ? Icons.visibility_outlined : Icons.visibility_off_outlined,
        ),
      ),
    ),
  );
}

class _MessageCard extends StatelessWidget {
  const _MessageCard({required this.title, required this.detail});
  final String title, detail;

  @override
  Widget build(BuildContext context) =>
      _Card(title: title, detail: detail, children: const []);
}

class _Notice extends StatelessWidget {
  const _Notice({required this.text});
  final String text;

  @override
  Widget build(BuildContext context) => Container(
    constraints: const BoxConstraints(maxWidth: 560),
    padding: const EdgeInsets.all(16),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.errorContainer,
      borderRadius: BorderRadius.circular(16),
    ),
    child: Text(text),
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
      return Container(
        constraints: const BoxConstraints(maxWidth: 600),
        padding: EdgeInsets.all(SidraviaLayout.cardPadding(compact: compact)),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(16),
          border: Border.all(color: theme.colorScheme.outlineVariant),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (showHeading) ...[
              Text(
                title,
                style: compact
                    ? theme.textTheme.titleLarge
                    : theme.textTheme.headlineSmall,
              ),
              SizedBox(height: compact ? 6 : 8),
              Text(
                detail,
                style:
                    (compact
                            ? theme.textTheme.bodySmall
                            : theme.textTheme.bodyMedium)
                        ?.copyWith(height: compact ? 1.4 : 1.5),
              ),
            ],
            if (children.isNotEmpty) ...[
              if (showHeading) SizedBox(height: compact ? 16 : 24),
              ...children,
            ],
          ],
        ),
      );
    },
  );
}
