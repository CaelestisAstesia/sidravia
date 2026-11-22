part of 'configuration_page.dart';

extension _ConfigurationFields on ConfigurationPageState {
  Widget _configurationFields(
    List<InstitutionProfile> profiles,
    void Function(VoidCallback) update,
  ) => DesignGroup(
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
        onCancel: () => update(() => _passwordError = null),
      ),
      NetworkBindingSection(
        controller: widget.controller,
        policy: _binding,
        onChanged: _saving || widget.controller.busy || !_canSave
            ? null
            : (value) => update(() => _binding = value),
      ),
    ],
  );
}
