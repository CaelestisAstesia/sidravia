import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_network_binding.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

/// A controlled editor: the configuration page owns the complete draft.
class NetworkBindingSection extends StatefulWidget {
  const NetworkBindingSection({
    super.key,
    required this.controller,
    required this.policy,
    required this.onChanged,
  });
  final GuiController controller;
  final NetworkBindingPolicy policy;
  final ValueChanged<NetworkBindingPolicy>? onChanged;
  @override
  State<NetworkBindingSection> createState() => _NetworkBindingSectionState();
}

class _NetworkBindingSectionState extends State<NetworkBindingSection> {
  bool _expanded = false;
  @override
  Widget build(BuildContext context) {
    final choices = networkBindingChoices(widget.controller.snapshot?.network);
    final policy = widget.policy;
    final available =
        policy.interfaceId == null || choices.any((c) => c.policy == policy);
    final selected = choices.where((c) => c.policy == policy).firstOrNull;
    Widget choice(
      NetworkBindingPolicy value,
      String label, {
      bool enabled = true,
    }) => Material(
      color: Colors.transparent,
      child: ListTile(
        title: Text(label, softWrap: true),
        subtitle: Text(
          value.interfaceId == null
              ? '自动选择'
              : enabled
              ? '可用'
              : '已不可用，不会自动回退',
        ),
        trailing: value == policy ? const Icon(Icons.check) : null,
        enabled: enabled && widget.onChanged != null,
        onTap: enabled && widget.onChanged != null
            ? () {
                widget.onChanged!(value);
                setState(() => _expanded = false);
              }
            : null,
      ),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const DesignSectionTitle('网络绑定'),
        DesignGroup(
          children: [
            DesignRow(
              title: '下次连接的网络策略',
              subtitle: selected?.label ?? policy.summary,
              trailing: Icon(_expanded ? Icons.expand_less : Icons.expand_more),
              onTap: widget.onChanged == null
                  ? null
                  : () => setState(() => _expanded = !_expanded),
            ),
          ],
        ),
        if (_expanded)
          DesignGroup(
            children: [
              choice(const NetworkBindingPolicy.automatic(), '自动选择可用网卡'),
              for (final c in choices) choice(c.policy, c.label),
              if (!available)
                choice(
                  policy,
                  '指定绑定已不可用：${policy.interfaceId} · ${policy.localIpv4Address}',
                  enabled: false,
                ),
            ],
          ),
        if (!available)
          const DesignHelper('所选网卡或 IPv4 已不可用。请选择当前可用的绑定；不会自动回退。'),
        if (policy.interfaceId != null) ...[
          Text('接口 ID：${policy.interfaceId}', softWrap: true),
          Text('IPv4：${policy.localIpv4Address}', softWrap: true),
        ],
      ],
    );
  }
}
