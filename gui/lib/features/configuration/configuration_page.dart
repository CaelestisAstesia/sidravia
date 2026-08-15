import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class ConfigurationPage extends StatelessWidget {
  const ConfigurationPage({super.key, required this.controller});

  final GuiController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 48, 24, 48),
      children: [
        Text('配置', style: theme.textTheme.displaySmall),
        const SizedBox(height: 10),
        Text(
          '保存用于校园网认证的登录信息。密码不会在界面中回显。',
          style: theme.textTheme.bodyLarge?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 28),
        AnimatedBuilder(
          animation: controller,
          builder: (context, _) =>
              _ConfigurationContent(controller: controller),
        ),
      ],
    );
  }
}

class _ConfigurationContent extends StatelessWidget {
  const _ConfigurationContent({required this.controller});

  final GuiController controller;

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
      );
    } else {
      content = _EditConfigurationForm(
        controller: controller,
        configuration: configurations.single,
        profiles: profiles,
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
  });

  final GuiController controller;
  final List<InstitutionProfile> profiles;

  @override
  State<_CreateConfigurationForm> createState() =>
      _CreateConfigurationFormState();
}

class _CreateConfigurationFormState extends State<_CreateConfigurationForm> {
  late String _profileId = widget.profiles.first.id;
  final _account = TextEditingController();
  final _password = TextEditingController();

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
  });

  final GuiController controller;
  final ConfigurationSummary configuration;
  final List<InstitutionProfile> profiles;

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
  });

  final Key fieldKey;
  final TextEditingController controller;
  final bool enabled;
  final String label;

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
      labelText: widget.label,
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
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      constraints: const BoxConstraints(maxWidth: 600),
      padding: const EdgeInsets.all(24),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            detail,
            style: theme.textTheme.bodyMedium?.copyWith(height: 1.5),
          ),
          if (children.isNotEmpty) ...[const SizedBox(height: 24), ...children],
        ],
      ),
    );
  }
}
