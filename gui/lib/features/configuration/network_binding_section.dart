import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_network_binding.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/insecure_storage_confirmation.dart';

class NetworkBindingSection extends StatefulWidget {
  const NetworkBindingSection({super.key, required this.controller});
  final GuiController controller;
  @override
  State<NetworkBindingSection> createState() => _NetworkBindingSectionState();
}

class _NetworkBindingSectionState extends State<NetworkBindingSection> {
  String? _configurationId;
  NetworkBindingPolicy? _draft;
  bool _targetChanged = false;
  bool _saving = false;
  String? _result;

  @override
  void initState() {
    super.initState();
    _sync();
    widget.controller.addListener(_changed);
  }

  void _sync() {
    final c = widget.controller.capabilities.configuration;
    if (_configurationId == null) {
      if (c != null) {
        _configurationId = c.id;
        _draft = c.networkBindingPolicy;
      }
    } else if (widget.controller.capabilities.isReady &&
        c?.id != _configurationId) {
      _targetChanged = true;
    }
  }

  void _changed() {
    if (mounted) setState(_sync);
  }

  @override
  void didUpdateWidget(NetworkBindingSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller.removeListener(_changed);
      widget.controller.addListener(_changed);
      _targetChanged = _configurationId != null;
      _sync();
    }
  }

  @override
  void dispose() {
    widget.controller.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final configuration = controller.capabilities.configuration;
    final choices = networkBindingChoices(controller.snapshot?.network);
    final draft = _draft;
    final eligible =
        draft != null &&
        networkBindingEligible(controller.snapshot?.network, draft);
    final editable =
        !_targetChanged &&
        !_saving &&
        !controller.busy &&
        configuration?.id == _configurationId &&
        controller.networkBindingUnavailable == null;
    final current =
        controller.capabilities.retainedSession?.selectedNetworkBinding;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const DesignSectionTitle('网络绑定'),
        const DesignHelper('独立保存网络绑定，不提交账号或密码编辑。更改会结束旧会话；下次手动连接才使用新策略。'),
        const SizedBox(height: 7),
        if (_configurationId == null)
          const DesignHelper('请先保存连接配置，再选择网络绑定。')
        else ...[
          DesignGroup(
            children: [
              DesignRow(
                title: '已保存策略',
                value: configuration == null
                    ? '暂不可用'
                    : configuration.networkBindingPolicy.interfaceId == null
                    ? '自动选择可用网卡'
                    : '${configuration.networkBindingPolicy.interfaceId} · ${configuration.networkBindingPolicy.localIpv4Address}',
              ),
              DesignRow(
                title: '当前会话绑定',
                value: current == null
                    ? '尚无会话绑定观察'
                    : '${current.displayName} · ${current.interfaceId} · ${current.localIpv4Address}',
              ),
            ],
          ),
          const SizedBox(height: 14),
          DesignField(
            label: '下次连接的网络策略',
            child: DropdownButtonFormField<NetworkBindingPolicy>(
              key: ValueKey((
                draft,
                choices.map((c) => c.policy).toList().join('|'),
              )),
              initialValue: draft,
              isExpanded: true,
              decoration: const InputDecoration(
                isDense: true,
                contentPadding: EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 9,
                ),
              ),
              items: [
                const DropdownMenuItem(
                  value: NetworkBindingPolicy.automatic(),
                  child: Text('自动选择可用网卡'),
                ),
                for (final choice in choices)
                  DropdownMenuItem(
                    value: choice.policy,
                    child: Text(
                      choice.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                if (draft?.interfaceId != null &&
                    !choices.any((c) => c.policy == draft))
                  DropdownMenuItem(
                    value: draft,
                    enabled: false,
                    child: Text(
                      '指定绑定已不可用：${draft!.interfaceId} · ${draft.localIpv4Address}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
              ],
              onChanged: editable
                  ? (value) => setState(() {
                      _draft = value;
                      _result = null;
                    })
                  : null,
            ),
          ),
          if (_targetChanged)
            const DesignHelper('连接配置已变化，请重新打开后编辑网络绑定。')
          else if (controller.networkBindingUnavailable != null)
            DesignHelper(controller.networkBindingUnavailable!)
          else if (!eligible)
            const DesignHelper('所选网卡或 IPv4 已不可用。请选择当前可用的绑定；不会自动回退。'),
          const SizedBox(height: 14),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton(
              onPressed:
                  editable &&
                      eligible &&
                      draft != configuration?.networkBindingPolicy
                  ? _save
                  : null,
              child: Text(_saving ? '正在保存网络绑定…' : '保存网络绑定'),
            ),
          ),
          if (_result != null) DesignHelper(_result!),
        ],
      ],
    );
  }

  Future<void> _save() async {
    final controller = widget.controller;
    final id = _configurationId;
    final policy = _draft;
    if (_targetChanged ||
        id == null ||
        policy == null ||
        !controller.canSaveNetworkBinding(id, policy)) {
      return;
    }
    setState(() {
      _saving = true;
      _result = null;
    });
    final ok = await controller.setNetworkBinding(
      configurationId: id,
      policy: policy,
      onInsecureStorageConfirmation: (operation) =>
          mounted && !_targetChanged && widget.controller == controller
          ? confirmInsecureStorage(context, operation)
          : Future.value(false),
    );
    if (!mounted) return;
    setState(() {
      _saving = false;
      _result = ok
          ? '网络绑定已保存；请手动连接以使用新策略。'
          : '${controller.notice ?? '网络绑定未保存，请检查当前状态。'}${controller.networkBindingErrorCode == null ? '' : ' (${controller.networkBindingErrorCode})'}';
    });
  }
}
