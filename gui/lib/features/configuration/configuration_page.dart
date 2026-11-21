import 'dart:async';

import 'package:sidravia_gui/features/configuration/override_editor.dart';
import 'package:sidravia_gui/application/gui_network_binding.dart';
import 'package:sidravia_gui/application/gui_operation.dart';
import 'package:sidravia_gui/shared/widgets/insecure_storage_confirmation.dart';
import 'package:sidravia_gui/features/configuration/network_binding_section.dart';
import 'package:sidravia_gui/features/configuration/configuration_password_row.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/feedback_pill.dart';

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
  final _overrideText = OverrideTextController();
  String? _version, _savedOverride, _overrideError;
  bool _storedOverridePresent = false, _documentUnavailable = false;
  bool _editingOverride = false, _overrideLoaded = false, _overrideBusy = false;
  String? _readBackReceiptVersion;
  bool _clearOverride = false,
      _overrideEdited = false,
      _saveUnconfirmed = false;
  String? _validatedProfile, _validatedText;
  String? _template;
  ({
    TextEditingValue value,
    bool clear,
    bool edited,
    String? validatedProfile,
    String? validatedText,
    String? error,
  })?
  _overrideEntry;

  void _rememberOverrideEntry() {
    _overrideEntry = (
      value: _overrideText.value,
      clear: _clearOverride,
      edited: _overrideEdited,
      validatedProfile: _validatedProfile,
      validatedText: _validatedText,
      error: _overrideError,
    );
  }

  bool get _overrideDirty =>
      _clearOverride ||
      (_overrideEdited && _overrideText.text != (_savedOverride ?? _template));
  bool get _validOverrideDraft =>
      !_overrideDirty ||
      _clearOverride ||
      (_validatedProfile == _profile && _validatedText == _overrideText.text);
  int get _schemaVersion {
    final profiles =
        widget.controller.snapshot?.profiles ?? const <InstitutionProfile>[];
    for (final profile in profiles) {
      if (profile.id == _profile) return profile.overrideSchemaVersion;
    }
    return 0;
  }

  Future<void> _openOverride() async {
    if (!_canSave || _overrideBusy) return;
    setState(() {
      if (!_editingOverride) {
        _overrideEntry = null;
        if (_overrideLoaded) _rememberOverrideEntry();
      }
      _editingOverride = true;
    });
    if (_overrideLoaded) return;
    if (_creating) {
      setState(() {
        _loadOverride(null);
        _rememberOverrideEntry();
      });
      return;
    }
    setState(() => _overrideBusy = true);
    final read = await widget.controller.readConfigurationOverride(
      _boundConfigurationId!,
    );
    if (!mounted) return;
    setState(() {
      _overrideBusy = false;
      if (read == null) {
        _overrideError = '无法读取已保存的覆写';
      } else if (read.configuration.version != _version) {
        _targetChanged = true;
        _overrideError = _changedTargetGuidance;
      } else {
        _loadOverride(
          read.document,
          stored: read.configuration.overrideState != 'none',
        );
        _rememberOverrideEntry();
      }
    });
  }

  void _loadOverride(String? document, {bool stored = false}) {
    _savedOverride = document;
    _storedOverridePresent = stored || document != null;
    _documentUnavailable = _storedOverridePresent && document == null;
    _template = _schemaVersion == 0
        ? ''
        : '{\n  "schemaVersion": $_schemaVersion\n}';
    _setOverrideText(document ?? (_documentUnavailable ? '' : _template!));
    _overrideLoaded = true;
    _overrideError = _documentUnavailable ? '已保存的覆写无法完整读取，仍可恢复协议默认值' : null;
  }

  void _setOverrideText(String text) {
    _overrideText.value = TextEditingValue(
      text: text,
      selection: const TextSelection.collapsed(offset: 0),
    );
  }

  void _changeOverride(bool clearRequested) {
    setState(() {
      _clearOverride = clearRequested;
      _overrideEdited = !clearRequested;
      _overrideError = clearRequested
          ? null
          : overrideSyntaxError(_overrideText.text);
    });
  }

  bool _restoreOverride() {
    setState(() {
      _clearOverride = _storedOverridePresent;
      _overrideEdited = false;
      _setOverrideText(_template ?? '');
      _validatedText = null;
      _overrideError = null;
    });
    return _clearOverride;
  }

  Future<void> _completeOverride() async {
    if (!_overrideDirty || _clearOverride || _validOverrideDraft) {
      setState(() {
        _overrideEntry = null;
        _editingOverride = false;
      });
      return;
    }
    final text = _overrideText.text;
    final profile = _profile;
    final error = overrideSyntaxError(text);
    if (error != null || profile == null || _schemaVersion == 0) {
      setState(() => _overrideError = error ?? '所选机构的协议不可用或不支持覆写');
      return;
    }
    setState(() => _overrideBusy = true);
    final result = await widget.controller.validateConfigurationOverride(
      profile,
      text,
    );
    if (!mounted) return;
    setState(() {
      _overrideBusy = false;
      if (profile != _profile || text != _overrideText.text) return;
      if (result?.valid == true) {
        _validatedProfile = profile;
        _validatedText = text;
        _overrideError = null;
        _overrideEntry = null;
        _editingOverride = false;
      } else {
        _overrideError = result == null
            ? '暂时无法校验协议覆写'
            : result.field == null
            ? '协议拒绝了此覆写，请检查字段和版本'
            : '协议覆写字段 ${result.field} 无效';
      }
    });
  }

  Future<void> _confirmSavedOverride() async {
    final id = _boundConfigurationId;
    if (id == null || _overrideBusy) return;
    final intended = _clearOverride ? null : _overrideText.text;
    final changedOverride = _overrideDirty;
    setState(() => _overrideBusy = true);
    final read = await widget.controller.readConfigurationOverride(
      id,
      expectedDocument: changedOverride ? intended : null,
    );
    if (!mounted) return;
    setState(() {
      _overrideBusy = false;
      if (read == null) {
        _saveResult = '暂时无法核对已保存的配置，请恢复通信后重试。';
        return;
      }
      final config = read.configuration;
      final current = widget.controller.capabilities.configuration;
      if (config.id != current?.id || config.version != current?.version) {
        _saveResult = '核对期间配置已变化，草稿仍保留。';
        return;
      }
      final matches =
          (!changedOverride ||
              (intended == null
                  ? config.overrideState == 'none'
                  : read.matchesDocument == true)) &&
          config.institutionProfileId == _profile &&
          config.username == _username.text &&
          config.networkBindingPolicy == _binding;
      if (_password.text.isNotEmpty &&
          (_readBackReceiptVersion == null ||
              config.version != _readBackReceiptVersion)) {
        _saveResult = '无法核对已提交的密码。草稿仍保留，请核对后重新打开配置。';
        return;
      }
      if (!matches) {
        _saveResult = '无法确认提交目标与当前配置一致。草稿仍保留，请核对后重新打开配置。';
        return;
      }
      _version = config.version;
      _originalProfile = _profile;
      _originalUsername = _username.text;
      _originalBinding = _binding;
      _credentialStored = config.credentialStored;
      _password.clear();
      _clearOverride = _overrideEdited = _saveUnconfirmed = _targetChanged =
          false;
      _loadOverride(
        read.document,
        stored: read.configuration.overrideState != 'none',
      );
      _saveResult = widget.controller.sessionNeedsReset
          ? '当前保存内容已核对。请在“连接与诊断”中移除当前会话。'
          : '当前保存内容已核对。请按需手动连接。';
    });
  }

  void _changeProfile(String? profile) {
    setState(() {
      _profile = profile;
      _validatedProfile = null;
      if (!_documentUnavailable) _overrideError = null;
      if (_overrideLoaded && !_storedOverridePresent && !_overrideEdited) {
        _loadOverride(null);
      }
    });
  }

  bool get _dirty =>
      _hydrated &&
      (_profile != _originalProfile ||
          _username.text != _originalUsername ||
          _password.text.isNotEmpty ||
          _binding != _originalBinding ||
          _overrideDirty);

  bool get canExitWithoutPrompt =>
      !_dirty && !_saving && !_prompting && !_overrideBusy;

  Future<void> requestLeave() async {
    if (_editingOverride) {
      if (_overrideBusy || _prompting) return;
      final entry = _overrideEntry;
      final changed =
          entry != null &&
          (entry.value.text != _overrideText.text ||
              entry.clear != _clearOverride);
      if (changed && !await _confirmDiscard()) return;
      if (!mounted) return;
      setState(() {
        if (entry != null) {
          _overrideText.value = entry.value;
          _clearOverride = entry.clear;
          _overrideEdited = entry.edited;
          _validatedProfile = entry.validatedProfile;
          _validatedText = entry.validatedText;
          _overrideError = entry.error;
        }
        _overrideEntry = null;
        _editingOverride = false;
      });
      return;
    }
    if (await mayLeave() && mounted) widget.onBack();
  }

  Future<bool> mayLeave({bool waitForSave = false}) async {
    if (waitForSave && _saving) {
      await _submission?.future;
      if (!mounted) return true;
    }
    if (_saving || _prompting || _overrideBusy) return false;
    if (!_dirty) return true;
    return _confirmDiscard(showUnknownResult: _saveUnconfirmed);
  }

  Future<bool> _confirmDiscard({bool showUnknownResult = false}) async {
    _prompting = true;
    final discard = await showDialog<bool>(
      context: context,
      builder: (context) => DesignConfirmationDialog(
        title: const Text('放弃未保存的修改？'),
        content: showUnknownResult ? const Text('放弃本地草稿不会撤销后台可能已完成的保存。') : null,
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
  Completer<void>? _submission;
  bool _hydrated = false;
  String? _boundConfigurationId;
  bool _creating = true;
  bool _targetChanged = false;
  static const _changedTargetGuidance = '连接配置已被其他客户端更改，请重新打开后继续编辑。';
  bool get _canSave =>
      _hydrated &&
      !_targetChanged &&
      !_saveUnconfirmed &&
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
      if (_saving) return;
      final caps = widget.controller.capabilities;
      if (!caps.isReady ||
          caps.capability != GuiCapabilityState.manageable && !_creating) {
        return;
      }
      if (_creating
          ? !caps.canCreate && !widget.controller.busy
          : caps.configuration?.id != _boundConfigurationId ||
                caps.configuration?.version != _version) {
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
    _version = c?.version;
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
    _overrideText.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_editingOverride) {
      return OverrideEditor(
        text: _overrideText,
        onBack: requestLeave,
        onComplete: _completeOverride,
        onChanged: _changeOverride,
        clearRequested: _clearOverride,
        enabled:
            _overrideLoaded &&
            !_overrideBusy &&
            _canSave &&
            !_documentUnavailable &&
            _schemaVersion > 0,
        loading: _overrideBusy,
        error: _targetChanged ? _changedTargetGuidance : _overrideError,
        onReload: !_overrideLoaded && !_overrideBusy && _canSave
            ? _openOverride
            : null,
        onRestore:
            _overrideLoaded &&
                !_overrideBusy &&
                _canSave &&
                (_storedOverridePresent && !_clearOverride ||
                    _overrideEdited && _overrideDirty)
            ? _restoreOverride
            : null,
      );
    }
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
        if (_targetChanged)
          const Padding(
            padding: EdgeInsets.only(bottom: 8),
            child: DesignHelper(_changedTargetGuidance),
          ),
        if (unavailable) const DesignHelper('通信或配置状态暂不可用；草稿已保留，请恢复后再保存。'),
        const DesignSectionTitle('基础配置'),
        DesignGroup(
          key: const ValueKey('configuration-fields'),
          children: [
            DesignChoiceRow<String>(
              title: '机构',
              value: _profile ?? '',
              options: [for (final p in profiles) (p.id, p.displayName)],
              onChanged: _saving || widget.controller.busy || !_canSave
                  ? null
                  : _changeProfile,
            ),
            DesignInputRow(
              label: '账号',
              controller: _username,
              enabled: !_saving && !widget.controller.busy && _canSave,
              hint: '输入账号',
              error: _usernameError,
            ),
            ConfigurationPasswordRow(
              controller: _password,
              enabled: !_saving && !widget.controller.busy && _canSave,
              credentialStored: _credentialStored,
              error: _passwordError,
              onCancel: () => setState(() => _passwordError = null),
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
        if (widget.controller.supportsOverrideEditing &&
            (_schemaVersion > 0 ||
                const {'empty', 'enabled', 'invalid', 'unavailable'}.contains(
                  widget.controller.capabilities.configuration?.overrideState,
                ) ||
                _overrideDirty)) ...[
          const SizedBox(height: 24),
          const DesignSectionTitle('高级'),
          DesignGroup(
            children: [
              DesignRow(
                title: '协议上报覆写',
                subtitle: _overrideDirty
                    ? (_validOverrideDraft ? '未保存' : '覆写草稿未完成')
                    : switch (widget
                          .controller
                          .capabilities
                          .configuration
                          ?.overrideState) {
                        'enabled' => '已设置',
                        'empty' => '已保存，无覆写字段',
                        'invalid' => '覆写无效',
                        'unavailable' => '覆写暂不可用',
                        'none' => '使用默认',
                        _ => _creating ? '使用默认' : '覆写状态暂不可用',
                      },
                onTap: _canSave ? _openOverride : null,
              ),
            ],
          ),
        ],
        const SizedBox(height: 18),
        ListenableBuilder(
          listenable: Listenable.merge([_username, _password]),
          builder: (context, _) => Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Align(
                alignment: Alignment.center,
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 320),
                  child: SizedBox(
                    width: double.infinity,
                    child: FilledButton(
                      onPressed:
                          _saving ||
                              !_canSave ||
                              !_dirty ||
                              !_validOverrideDraft
                          ? null
                          : _submit,
                      child: Text(_saving ? '正在保存…' : '保存修改'),
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
        if (_saveResult ?? widget.controller.notice case final String message)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: FeedbackPill(
              message: message,
              onDismiss: () {
                if (_saveResult == message) setState(() => _saveResult = null);
                widget.controller.dismissNotice(message);
              },
            ),
          ),
        if (_saveUnconfirmed &&
            _boundConfigurationId != null &&
            widget.controller.capabilities.matchesConfiguration(
              _boundConfigurationId!,
            ))
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              onPressed: _overrideBusy ? null : _confirmSavedOverride,
              child: const Text('核对已保存的配置'),
            ),
          ),
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

  Future<void> _confirmDelete() async {
    final c = widget.controller.capabilities.configuration;
    if (!widget.controller.capabilities.canDeleteConfiguration ||
        c == null ||
        _targetChanged ||
        c.id != _boundConfigurationId) {
      return;
    }
    final confirmed = await showDesignConfirmation(
      context,
      title: const Text('删除登录配置？'),
      content: const Text('此操作会删除已保存的账号与凭据。'),
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

  Future<void> _submit() async {
    if (_submission != null) return;
    final done = Completer<void>();
    _submission = done;
    try {
      await _save();
    } finally {
      _submission = null;
      done.complete();
    }
  }

  Future<void> _save() async {
    if (_saving || _prompting || !_canSave || !_dirty || !_validOverrideDraft) {
      return;
    }
    String? usernameError;
    String? passwordError;
    const inputRule = '仅允许英文字母、数字和半角符号，不允许空格或其他字符。';
    bool allowed(String value) =>
        value.codeUnits.every((c) => c >= 0x21 && c <= 0x7e);
    if (_username.text.isEmpty) {
      usernameError = '请输入账号。';
    } else if ((_creating || _username.text != _originalUsername) &&
        !allowed(_username.text)) {
      usernameError = inputRule;
    }
    if (_password.text.isEmpty && !_credentialStored) {
      passwordError = '首次配置需要填写密码。';
    } else if (_password.text.isNotEmpty && !allowed(_password.text)) {
      passwordError = inputRule;
    }
    if (_profile == null || usernameError != null || passwordError != null) {
      setState(() {
        _usernameError = usernameError;
        _passwordError = passwordError;
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
    final clearOnly =
        _clearOverride &&
        _profile == _originalProfile &&
        _username.text == _originalUsername &&
        _password.text.isEmpty &&
        _binding == _originalBinding;
    final availability =
        widget.controller.capabilities.configuration?.runtimeAvailability;
    final cannotConnect =
        clearOnly &&
        (availability == ConfigurationRuntimeAvailability.profileUnavailable ||
            availability ==
                ConfigurationRuntimeAvailability.protocolUnavailable);
    _prompting = true;
    final reconnect = await showDialog<bool>(
      context: context,
      builder: (context) => DesignConfirmationDialog(
        title: const Text('保存连接配置？'),
        content: _overrideDirty
            ? Text(
                cannotConnect
                    ? '保存会清除协议覆写并结束关联的旧会话。学校配置或协议仍不可用。'
                    : '更改协议覆写会结束关联的旧会话。仅保存后不会自动连接。',
              )
            : null,
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
            onPressed: cannotConnect
                ? null
                : () => Navigator.pop(context, true),
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
    final username = _username.text;
    final password = _password.text;
    var ok = false;
    GuiMutationResult? result;
    ConfigurationSummary? saved;
    if (controller.supportsOverrideEditing && (_creating || _version != null)) {
      ok = await controller.saveConfigurationEdit(
        {
          if (!_creating) 'configurationId': boundId,
          if (!_creating) 'expectedVersion': _version,
          if (_creating || profile != _originalProfile)
            'institutionProfileId': profile,
          if (_creating || username != _originalUsername) 'username': username,
          if (_creating || password.isNotEmpty) 'password': password,
          if (_creating || _binding != _originalBinding)
            'networkBindingPolicy': _binding.toJson(),
          if (_creating) 'autoLogin': false,
          if (_creating) 'autoReconnect': true,
          if (_clearOverride) 'protocolContextOverride': null,
          if (_overrideDirty && !_clearOverride)
            'overrideDocument': _overrideText.text,
        },
        creating: _creating,
        onSaved: (value) => saved = value,
        onResult: (value) => result = value,
        onInsecureStorageConfirmation: (operation) => mounted
            ? confirmInsecureStorage(context, operation)
            : Future.value(false),
      );
    } else if (_creating) {
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
        if (saved case final ConfigurationSummary committed
            when cannotConnect) {
          setState(() {
            _version = committed.version;
            _clearOverride = _overrideEdited = false;
            _loadOverride(null);
            _saveResult = '协议覆写已清除，学校配置或协议仍不可用。';
          });
          return;
        }
        (widget.onSaved ?? widget.onBack)();
        if (reconnect) {
          final committed = saved;
          if (committed != null &&
              (committed.version != null ||
                  _version != null ||
                  _overrideDirty)) {
            await controller.connectSavedConfiguration(committed);
          } else {
            final id =
                committed?.id ??
                boundId ??
                controller.capabilities.configuration?.id;
            if (id != null) await controller.startConfiguration(id);
          }
        }
      } else {
        final committed = result?.committed == true;
        final unknown = result?.unknown == true;
        final receipt = saved;
        final readBackId = committed && receipt != null ? receipt.id : boundId;
        setState(() {
          if (_creating && committed && receipt != null) {
            _creating = false;
            _boundConfigurationId = receipt.id;
            _version = receipt.version;
            _credentialStored = receipt.credentialStored;
          }
          _readBackReceiptVersion = committed ? receipt?.version : null;
          _saveUnconfirmed = unknown || committed && readBackId != null;
          if (result?.errorCode == 'configuration_version_conflict') {
            _targetChanged = true;
          }
        });
        setState(
          () => _saveResult = unknown
              ? readBackId == null
                    ? '保存结果未知。通信恢复后请重新打开连接配置检查是否已创建。'
                    : '保存结果未知，请恢复通信后确认配置；未自动连接。'
              : result?.committed == true
              ? result?.errorCode == 'configuration_session_invalidation_failed'
                    ? '配置已提交，但旧会话清理失败；请清理会话后再连接。'
                    : '配置已保存，但刷新失败；请恢复通信后确认，未自动连接。'
              : _clearOverride &&
                    !clearOnly &&
                    (result?.errorCode == 'profile_not_found' ||
                        result?.errorCode == 'protocol_not_found')
              ? '仅清除覆写需要先撤销其他配置修改，或修正配置后一起保存。'
              : controller.notice,
        );
      }
    }
  }
}
