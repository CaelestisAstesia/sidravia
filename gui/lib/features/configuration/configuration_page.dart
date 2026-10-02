import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class ConfigurationPage extends StatefulWidget {
  const ConfigurationPage({
    super.key,
    required this.controller,
    required this.onBack,
  });
  final GuiController controller;
  final VoidCallback onBack;
  @override
  State<ConfigurationPage> createState() => _ConfigurationPageState();
}

class _ConfigurationPageState extends State<ConfigurationPage> {
  late final TextEditingController _username;
  late final TextEditingController _password;
  String? _profile;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _username = TextEditingController();
    _password = TextEditingController();
    _sync();
  }

  void _sync() {
    final c = widget.controller.capabilities.configuration;
    final profiles =
        widget.controller.snapshot?.profiles ?? const <InstitutionProfile>[];
    _username.text = c?.username ?? '';
    _profile =
        c?.institutionProfileId ??
        (profiles.isEmpty ? null : profiles.first.id);
  }

  @override
  void dispose() {
    _username.dispose();
    _password.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final snapshot = widget.controller.snapshot;
    final c = widget.controller.capabilities.configuration;
    final profiles = snapshot?.profiles ?? const <InstitutionProfile>[];
    final creating = c == null;
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(24, 16, 24, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _BackHeader(title: '连接配置', onBack: widget.onBack),
          DropdownButtonFormField<String>(
            initialValue: _profile,
            decoration: const InputDecoration(labelText: '机构'),
            items: [
              for (final profile in profiles)
                DropdownMenuItem(
                  value: profile.id,
                  child: Text(profile.displayName),
                ),
            ],
            onChanged: widget.controller.busy
                ? null
                : (value) => setState(() => _profile = value),
          ),
          const SizedBox(height: 14),
          TextField(
            controller: _username,
            enabled: !widget.controller.busy,
            decoration: const InputDecoration(
              labelText: '认证账号',
              hintText: '输入校园网账号',
            ),
          ),
          const SizedBox(height: 14),
          Text(
            '密码已保存。出于安全原因，Sidravia 不会回显现有密码。',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 7),
          TextField(
            controller: _password,
            obscureText: true,
            enabled: !widget.controller.busy,
            decoration: InputDecoration(
              labelText: creating ? '认证密码' : '设置新密码',
              hintText: creating ? '输入校园网密码' : '仅在需要修改时填写',
            ),
          ),
          const SizedBox(height: 18),
          Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              TextButton(onPressed: widget.onBack, child: const Text('取消')),
              const SizedBox(width: 8),
              FilledButton(
                onPressed: _saving ? null : _save,
                child: Text(creating ? '保存配置' : '保存更改'),
              ),
            ],
          ),
          if (!creating)
            Padding(
              padding: const EdgeInsets.only(top: 28),
              child: OutlinedButton(
                onPressed: null,
                child: const Text('删除登录配置（暂不可用）'),
              ),
            ),
        ],
      ),
    );
  }

  Future<void> _save() async {
    if (_profile == null ||
        _username.text.trim().isEmpty ||
        (_password.text.isEmpty &&
            widget.controller.capabilities.configuration == null)) {
      ScaffoldMessenger.of(context)
          .showSnackBar(const SnackBar(content: Text('请填写机构、账号和密码。')));
      return;
    }
    setState(() => _saving = true);
    final c = widget.controller.capabilities.configuration;
    var ok = false;
    if (c == null) {
      ok = await widget.controller.createConfiguration(
        institutionProfileId: _profile!,
        username: _username.text.trim(),
        password: _password.text,
      );
    } else {
      ok = await widget.controller.updateConfiguration(
        configurationId: c.id,
        institutionProfileId: _profile!,
        username: _username.text.trim(),
      );
      if (ok && _password.text.isNotEmpty) {
        ok = await widget.controller.setPassword(
          configurationId: c.id,
          password: _password.text,
        );
      }
    }
    _password.clear();
    if (mounted) {
      setState(() => _saving = false);
      if (ok) widget.onBack();
    }
  }
}

class _BackHeader extends StatelessWidget {
  const _BackHeader({required this.title, required this.onBack});
  final String title;
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
          label: const Text('返回'),
        ),
        const SizedBox(height: 9),
        Text(
          title,
          style: Theme.of(context).textTheme.headlineSmall
              ?.copyWith(fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}
