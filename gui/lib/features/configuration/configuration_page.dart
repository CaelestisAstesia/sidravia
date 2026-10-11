import 'package:sidravia_gui/application/gui_network_binding.dart';
import 'package:sidravia_gui/application/gui_operation.dart';
import 'package:sidravia_gui/shared/widgets/insecure_storage_confirmation.dart';
import 'package:sidravia_gui/features/configuration/network_binding_section.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class ConfigurationPage extends StatefulWidget {
  const ConfigurationPage({
    super.key,
    required this.controller,
    required this.onBack,
    this.onSaved,
  });
  final GuiController controller;
  final VoidCallback onBack;
  final VoidCallback? onSaved;
  @override
  State<ConfigurationPage> createState() => ConfigurationPageState();
}

class ConfigurationPageState extends State<ConfigurationPage> {
  late final TextEditingController _username;
  late final TextEditingController _password;
  String? _profile, _originalProfile, _originalUsername;
  NetworkBindingPolicy _binding = const NetworkBindingPolicy.automatic();
  NetworkBindingPolicy _originalBinding =
      const NetworkBindingPolicy.automatic();
  bool _credentialStored = false, _prompting = false;
  String? _saveResult;
  bool get _dirty =>
      _hydrated &&
      (_profile != _originalProfile ||
          _username.text.trim() != _originalUsername ||
          _password.text.isNotEmpty ||
          _binding != _originalBinding);

  Future<void> requestLeave() async {
    if (await mayLeave() && mounted) widget.onBack();
  }

  Future<bool> mayLeave() async {
    if (_saving || _prompting) return false;
    if (!_dirty) return true;
    _prompting = true;
    final discard = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('放弃未保存的修改？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('放弃修改'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('继续编辑'),
          ),
        ],
      ),
    );
    _prompting = false;
    return discard == true && mounted;
  }

  bool _saving = false;
  bool _hydrated = false;
  String? _boundConfigurationId;
  bool _creating = true;
  bool _targetChanged = false;
  static const _changedTargetGuidance = '连接配置已被其他客户端更改，请重新打开后继续编辑。';
  bool get _canSave =>
      _hydrated &&
      !_targetChanged &&
      (_creating
          ? widget.controller.capabilities.canCreate
          : widget.controller.capabilities.matchesConfiguration(
              _boundConfigurationId!,
            ));
  String? _usernameError, _passwordError;

  @override
  void initState() {
    super.initState();
    _username = TextEditingController();
    _password = TextEditingController();
    _sync();
    widget.controller.addListener(_onSnapshot);
  }

  void _onSnapshot() {
    if (mounted) setState(_sync);
  }

  void _sync() {
    if (!widget.controller.capabilities.isReady) return;
    if (_hydrated) {
      final caps = widget.controller.capabilities;
      if (_creating
          ? !caps.canCreate && !widget.controller.busy
          : caps.configuration?.id != _boundConfigurationId) {
        _targetChanged = true;
      }
      return;
    }
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
    _boundConfigurationId = c?.id;
    _creating = c == null;
    _originalProfile = _profile;
    _originalUsername = _username.text;
    _binding =
        c?.networkBindingPolicy ?? const NetworkBindingPolicy.automatic();
    _originalBinding = _binding;
    _credentialStored = c?.credentialStored ?? false;
    _hydrated = true;
  }

  @override
  void didUpdateWidget(covariant ConfigurationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller.removeListener(_onSnapshot);
      widget.controller.addListener(_onSnapshot);
      if (_hydrated) _targetChanged = true;
    }
    _sync();
  }

  @override
  void dispose() {
    widget.controller.removeListener(_onSnapshot);
    _username.dispose();
    _password.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final capability = widget.controller.capabilities.capability;
    final unavailable =
        capability != GuiCapabilityState.createOnly &&
        capability != GuiCapabilityState.manageable;
    if (!_hydrated) {
      final guidance = switch (capability) {
        GuiCapabilityState.multipleConfigurations =>
          '检测到多个连接配置，当前 GUI 暂不支持选择。请使用 sidraviactl 管理。',
        GuiCapabilityState.ambiguousSessions =>
          '配置或会话关系不明确，当前 GUI 无法安全编辑。请使用 sidraviactl 管理。',
        GuiCapabilityState.daemonUnavailable => '核心服务未运行，暂时无法读取连接配置。',
        GuiCapabilityState.unsupported => '当前平台暂不支持连接配置。',
        GuiCapabilityState.createOnly => '正在准备机构信息，请稍后再试。',
        _ => '通信尚未就绪，暂时无法读取连接配置。',
      };
      return DesignPage(
        children: [
          DesignHeader(title: '连接配置', onBack: requestLeave),
          DesignHelper(guidance),
        ],
      );
    }
    final snapshot = widget.controller.snapshot;
    final profiles = snapshot?.profiles ?? const <InstitutionProfile>[];
    final creating = _creating;
    return DesignPage(
      children: [
        DesignHeader(title: '连接配置', onBack: requestLeave),
        if (unavailable) const DesignHelper('通信或配置状态暂不可用；草稿已保留，请恢复后再保存。'),
        DesignGroup(
          children: [
            DesignChoiceRow<String>(
              title: '机构',
              centerValue: true,
              value: _profile ?? '',
              options: [for (final p in profiles) (p.id, p.displayName)],
              onChanged: _saving || widget.controller.busy || !_canSave
                  ? null
                  : (v) => setState(() => _profile = v),
            ),
            DesignInputRow(
              label: '账号',
              controller: _username,
              enabled: !_saving && !widget.controller.busy && _canSave,
              hint: '输入账号',
              error: _usernameError,
            ),
            DesignInputRow(
              label: '密码',
              controller: _password,
              enabled: !_saving && !widget.controller.busy && _canSave,
              obscureText: true,
              hint: _credentialStored ? '••••••••' : '输入密码',
              error: _passwordError,
            ),
            NetworkBindingSection(
              controller: widget.controller,
              policy: _binding,
              onChanged: _saving || widget.controller.busy || !_canSave
                  ? null
                  : (value) => setState(() => _binding = value),
            ),
          ],
        ),
        const SizedBox(height: 8),
        DesignHelper(
          _credentialStored ? '密码已保存；留空保留原密码，输入则替换。' : '首次连接请填写账号和密码。',
        ),
        const SizedBox(height: 18),
        FilledButton(
          onPressed: _saving || !_canSave ? null : _save,
          child: Text(_saving ? '正在保存…' : '保存修改'),
        ),
        TextButton(
          onPressed: _saving || widget.controller.busy || !_canSave
              ? null
              : _discard,
          child: const Text('放弃修改'),
        ),
        if (_targetChanged)
          const Padding(
            padding: EdgeInsets.only(top: 8),
            child: Text(_changedTargetGuidance),
          ),
        const SizedBox(height: 24),
        if (_saveResult ?? widget.controller.notice case final String message)
          DesignHelper(message),
        if (!creating) ...[
          const SizedBox(height: 24),
          const DesignSectionTitle('删除'),
          DesignGroup(
            children: [
              DesignRow(
                title: '删除登录配置',
                subtitle:
                    !_targetChanged &&
                        widget.controller.capabilities.canDeleteConfiguration
                    ? '删除已保存的账号与凭据'
                    : '当前状态不允许删除',
                danger: true,
                onTap:
                    !_targetChanged &&
                        widget.controller.capabilities.canDeleteConfiguration
                    ? () => _confirmDelete()
                    : null,
              ),
            ],
          ),
        ],
      ],
    );
  }

  void _discard() {
    _sync();
    if (_saving ||
        _prompting ||
        widget.controller.busy ||
        !_canSave ||
        _targetChanged) {
      return;
    }
    setState(() {
      _profile = _originalProfile;
      _username.text = _originalUsername ?? '';
      _password.clear();
      _binding = _originalBinding;
      _usernameError = null;
      _passwordError = null;
      _saveResult = '已放弃修改。';
    });
  }

  Future<void> _confirmDelete() async {
    final c = widget.controller.capabilities.configuration;
    if (!widget.controller.capabilities.canDeleteConfiguration ||
        c == null ||
        _targetChanged ||
        c.id != _boundConfigurationId) {
      return;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除登录配置？'),
        content: const Text('此操作会删除已保存的账号与凭据。'),
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
    final ok = await widget.controller.deleteConfiguration(
      c.id,
      onInsecureStorageConfirmation: (operation) => mounted
          ? confirmInsecureStorage(context, operation)
          : Future.value(false),
    );
    if (ok && mounted) widget.onBack();
  }

  Future<void> _save() async {
    if (_saving || _prompting || !_canSave || !_dirty) return;
    if (_profile == null ||
        _username.text.trim().isEmpty ||
        (_password.text.isEmpty && !_credentialStored)) {
      setState(() {
        _usernameError = _username.text.trim().isEmpty ? '请输入账号。' : null;
        _passwordError = _password.text.isEmpty && !_credentialStored
            ? '首次配置需要填写密码。'
            : null;
      });
      return;
    }
    if (_binding != _originalBinding &&
        _binding.interfaceId != null &&
        !networkBindingEligible(
          widget.controller.snapshot?.network,
          _binding,
        )) {
      return;
    }
    final state = widget.controller.capabilities.retainedSession?.state;
    final connected = state == SessionState.authenticated;
    final active =
        connected ||
        state == SessionState.authenticating ||
        state == SessionState.waitingForNetwork ||
        state == SessionState.waitingBeforeRetry;
    _prompting = true;
    final reconnect = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('保存连接配置？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(
              connected
                  ? '保存并断开'
                  : active
                  ? '保存并停止'
                  : '保存',
            ),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(active ? '保存并重新连接' : '保存并连接'),
          ),
        ],
      ),
    );
    _prompting = false;
    if (reconnect == null || !mounted || !_canSave) return;
    setState(() {
      _saving = true;
      _usernameError = null;
      _passwordError = null;
    });
    final boundId = _boundConfigurationId;
    final controller = widget.controller;
    final profile = _profile!;
    final username = _username.text.trim();
    final password = _password.text;
    var ok = false;
    GuiMutationResult? result;
    if (_creating) {
      ok = await controller.createConfiguration(
        onResult: (value) => result = value,
        networkBindingPolicy: _binding,
        institutionProfileId: profile,
        username: username,
        password: password,
        onInsecureStorageConfirmation: (operation) => mounted
            ? confirmInsecureStorage(context, operation)
            : Future.value(false),
      );
    } else {
      ok = await controller.updateConfiguration(
        configurationId: boundId!,
        onResult: (value) => result = value,
        networkBindingPolicy: _binding,
        institutionProfileId: profile,
        username: username,
        password: password.isEmpty ? null : password,
        onInsecureStorageConfirmation: (operation) => mounted
            ? confirmInsecureStorage(context, operation)
            : Future.value(false),
      );
    }
    if (mounted) {
      setState(() => _saving = false);
      if (ok) {
        _password.clear();
        (widget.onSaved ?? widget.onBack)();
        if (reconnect) {
          final id = controller.capabilities.configuration?.id;
          if (id != null) await controller.startConfiguration(id);
        }
      } else {
        setState(
          () => _saveResult = result?.unknown == true
              ? '保存结果未知，请恢复通信后确认配置；未自动连接。'
              : result?.committed == true
              ? result?.errorCode == 'configuration_session_invalidation_failed'
                    ? '配置已提交，但旧会话清理失败；请清理会话后再连接。'
                    : '配置已保存，但刷新失败；请恢复通信后确认，未自动连接。'
              : controller.notice,
        );
      }
    }
  }
}
