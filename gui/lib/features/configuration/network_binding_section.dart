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
  @override
  Widget build(BuildContext context) {
    final choices = networkBindingChoices(widget.controller.snapshot?.network);
    final policy = widget.policy;
    final selected = choices.where((c) => c.policy == policy).firstOrNull;
    final available = policy.interfaceId == null || selected != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        DesignRow(
          title: '网卡',
          centerValue: true,
          value: selected?.label ?? policy.summary,
          onTap: widget.onChanged == null
              ? null
              : () async {
                  final next = await showDialog<NetworkBindingPolicy>(
                    context: context,
                    builder: (context) => Dialog(
                      child: SizedBox(
                        width: 360,
                        child: ConstrainedBox(
                          constraints: BoxConstraints(
                            maxHeight: MediaQuery.sizeOf(context).height * .8,
                          ),
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Padding(
                                padding: const EdgeInsets.all(16),
                                child: DesignHeader(
                                  title: '选择网卡',
                                  onBack: () => Navigator.pop(context),
                                  bottomSpacing: 0,
                                ),
                              ),
                              Flexible(
                                child: SingleChildScrollView(
                                  child: Column(
                                    children: [
                                      ListTile(
                                        title: const Text('自动选择可用网卡'),
                                        subtitle: const Text('自动选择'),
                                        trailing: policy.interfaceId == null
                                            ? const Icon(Icons.check)
                                            : null,
                                        onTap: () => Navigator.pop(
                                          context,
                                          const NetworkBindingPolicy.automatic(),
                                        ),
                                      ),
                                      for (final c in choices)
                                        ListTile(
                                          title: Text(c.label),
                                          subtitle: const Text('可用'),
                                          trailing: c.policy == policy
                                              ? const Icon(Icons.check)
                                              : null,
                                          onTap: () =>
                                              Navigator.pop(context, c.policy),
                                        ),
                                      if (!available)
                                        ListTile(
                                          enabled: false,
                                          title: Text(
                                            '指定绑定已不可用：${policy.interfaceId} · ${policy.localIpv4Address}',
                                          ),
                                          subtitle: const Text('已不可用，不会自动回退'),
                                          trailing: const Icon(Icons.check),
                                        ),
                                    ],
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  );
                  if (next != null && mounted) widget.onChanged?.call(next);
                },
        ),
        if (!available)
          const Padding(
            padding: EdgeInsets.fromLTRB(12, 0, 12, 12),
            child: DesignHelper('所选网卡或 IPv4 已不可用。请选择当前可用的绑定；不会自动回退。'),
          ),
      ],
    );
  }
}
