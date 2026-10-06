import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

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
  bool _hydrated = false;
  String? _usernameError, _passwordError;

  @override
  void initState() {
    super.initState();
    _username = TextEditingController();
    _password = TextEditingController();
    _sync();
  }

  void _sync() {
    if (_hydrated || !widget.controller.capabilities.isReady) return;
    final c = widget.controller.capabilities.configuration;
    final profiles =
        widget.controller.snapshot?.profiles ?? const <InstitutionProfile>[];
    if (c == null &&
        (!widget.controller.capabilities.canCreate || profiles.isEmpty)) {
      return;
    }
    _username.text = c?.username ?? '';
    _profile =
        c?.institutionProfileId ??
        (profiles.isEmpty ? null : profiles.first.id);
    _hydrated = true;
  }

  @override
  void didUpdateWidget(covariant ConfigurationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    _sync();
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
    return DesignPage(
      children: [
        DesignHeader(title: '连接配置', onBack: widget.onBack),
        DesignField(
          label: '机构',
          child: DropdownButtonFormField<String>(
            initialValue: _profile,
            decoration: const InputDecoration(
              isDense: true,
              constraints: BoxConstraints(minHeight: 40),
              contentPadding: EdgeInsets.symmetric(horizontal: 10, vertical: 7),
            ),
            isExpanded: true,
            items: [
              for (final profile in profiles)
                DropdownMenuItem(
                  value: profile.id,
                  child: Text(profile.displayName),
                ),
            ],
            onChanged:
                _saving ||
                    widget.controller.busy ||
                    !(creating
                        ? widget.controller.capabilities.canCreate
                        : widget.controller.capabilities.canManage)
                ? null
                : (value) => setState(() => _profile = value),
          ),
        ),
        const SizedBox(height: 14),
        DesignField(
          label: '认证账号',
          error: _usernameError,
          child: TextField(
            controller: _username,
            enabled:
                !_saving &&
                !widget.controller.busy &&
                (creating
                    ? widget.controller.capabilities.canCreate
                    : widget.controller.capabilities.canManage),
            decoration: InputDecoration(
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 10,
                vertical: 9,
              ),
              isDense: true,
              constraints: const BoxConstraints(minHeight: 40),
              hintText: '输入校园网账号',
            ),
          ),
        ),
        const SizedBox(height: 21),
        Text(
          creating ? '首次连接需要机构、账号和密码。' : '密码已保存。出于安全原因，Sidravia 不会回显现有密码。',
          style: TextStyle(
            fontSize: 11,
            height: 1.5,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 7),
        DesignField(
          label: creating ? '认证密码' : '设置新密码',
          error: _passwordError,
          child: TextField(
            controller: _password,
            obscureText: true,
            enabled:
                !_saving &&
                !widget.controller.busy &&
                (creating
                    ? widget.controller.capabilities.canCreate
                    : widget.controller.capabilities.canManage),
            decoration: InputDecoration(
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 10,
                vertical: 9,
              ),
              isDense: true,
              constraints: const BoxConstraints(minHeight: 40),
              hintText: creating ? '输入校园网密码' : '仅在需要修改时填写',
            ),
          ),
        ),
        const SizedBox(height: 18),
        Row(
          mainAxisAlignment: MainAxisAlignment.end,
          children: [
            OutlinedButton(onPressed: widget.onBack, child: const Text('取消')),
            const SizedBox(width: 8),
            FilledButton(
              onPressed:
                  _saving ||
                      !(creating
                          ? widget.controller.capabilities.canCreate
                          : widget.controller.capabilities.canManage)
                  ? null
                  : _save,
              child: Text(creating ? '保存配置' : '保存更改'),
            ),
          ],
        ),
        if (widget.controller.notice != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              widget.controller.notice!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ),
        if (!creating) ...[
          const SizedBox(height: 24),
          const DesignSectionTitle('更多操作'),
          DesignGroup(
            children: [
              DesignRow(
                title: '移除当前会话',
                subtitle: widget.controller.capabilities.canResetSession
                    ? '清理当前 Session 记录'
                    : '没有可清理的会话或当前不可操作',
                onTap: widget.controller.capabilities.canResetSession
                    ? () => _confirmMutation(false)
                    : null,
              ),
              DesignRow(
                title: '删除登录配置',
                subtitle: widget.controller.capabilities.canDeleteConfiguration
                    ? '删除已保存的账号与凭据'
                    : '当前状态不允许删除',
                danger: true,
                onTap: widget.controller.capabilities.canDeleteConfiguration
                    ? () => _confirmMutation(true)
                    : null,
              ),
            ],
          ),
        ],
      ],
    );
  }

  Future<void> _confirmMutation(bool delete) async {
    final c = widget.controller.capabilities.configuration;
    final session = widget.controller.capabilities.retainedSession;
    final allowed = delete
        ? widget.controller.capabilities.canDeleteConfiguration
        : widget.controller.capabilities.canResetSession;
    if (!allowed || c == null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(delete ? '删除登录配置？' : '移除当前会话？'),
        content: Text(delete ? '此操作会删除已保存的账号与凭据。' : '此操作会清理当前 Session 记录。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final ok = delete
        ? await widget.controller.deleteConfiguration(c.id)
        : session != null && await widget.controller.resetSession(session.id);
    if (ok && mounted) widget.onBack();
  }

  Future<void> _save() async {
    if (!(widget.controller.capabilities.configuration == null
        ? widget.controller.capabilities.canCreate
        : widget.controller.capabilities.canManage)) {
      return;
    }
    if (_profile == null ||
        _username.text.trim().isEmpty ||
        (_password.text.isEmpty &&
            widget.controller.capabilities.configuration == null)) {
      setState(() {
        _usernameError = _username.text.trim().isEmpty ? '请输入校园网账号。' : null;
        _passwordError =
            _password.text.isEmpty &&
                widget.controller.capabilities.configuration == null
            ? '首次配置需要填写密码。'
            : null;
      });
      return;
    }
    setState(() {
      _saving = true;
      _usernameError = null;
      _passwordError = null;
    });
    final c = widget.controller.capabilities.configuration;
    final controller = widget.controller;
    final profile = _profile!;
    final username = _username.text.trim();
    final password = _password.text;
    var ok = false;
    if (c == null) {
      ok = await controller.createConfiguration(
        institutionProfileId: profile,
        username: username,
        password: password,
      );
    } else {
      ok = await controller.updateConfiguration(
        configurationId: c.id,
        institutionProfileId: profile,
        username: username,
        password: password.isEmpty ? null : password,
      );
    }
    if (mounted) {
      _password.clear();
      setState(() => _saving = false);
      if (ok) widget.onBack();
    }
  }
}
