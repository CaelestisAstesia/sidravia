import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/application/business_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

/// One centered group for the actual network binding and optional login values.
class ConnectionInformationView extends StatelessWidget {
  const ConnectionInformationView({
    super.key,
    this.controller,
    this.contextText = '',
    this.detailText = '',
    this.issue,
  });
  final BusinessController? controller;
  final String contextText, detailText;
  final SessionAuthenticationFailure? issue;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: controller ?? const AlwaysStoppedAnimation(0),
    builder: (context, _) {
      final data = controller?.snapshot;
      final stats = data?.statistics;
      final traffic = stats?.trafficBytes;
      final balance = stats?.balanceCents;
      final lines = <String>{
        if (issue == null && detailText.isNotEmpty) detailText,
        if (issue != null) issue!.description,
        if (issue != null) issue!.handlingRecommendation,
        if (contextText.isNotEmpty && contextText != issue?.description)
          contextText,
      }.toList();
      return Column(
        key: const ValueKey('connection-information'),
        mainAxisSize: MainAxisSize.min,
        children: [
          for (var i = 0; i < lines.length; i++) ...[
            if (i > 0) const SizedBox(height: 8),
            Text(
              shortUiText(lines[i]),
              key: lines[i] == contextText
                  ? const ValueKey('home-context')
                  : null,
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 12,
                height: 1.45,
                fontWeight: FontWeight.w400,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (data?.statisticsSupported == true) ...[
            if (lines.isNotEmpty) const SizedBox(height: 8),
            _InformationLine(
              label: '历史计费流量',
              value: traffic == null
                  ? '暂无数据'
                  : '${(traffic / (1024 * 1024)).toStringAsFixed(2)} MiB',
            ),
            const SizedBox(height: 8),
            _InformationLine(
              label: '余额',
              value: balance == null
                  ? '暂无数据'
                  : '${balance ~/ 100}.${(balance % 100).toString().padLeft(2, '0')} 元',
            ),
          ],
        ],
      );
    },
  );
}

class _InformationLine extends StatelessWidget {
  const _InformationLine({required this.label, required this.value});
  final String label, value;
  @override
  Widget build(BuildContext context) {
    final style = TextStyle(
      fontSize: 12,
      height: 1.45,
      fontWeight: FontWeight.w400,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    );
    return Wrap(
      alignment: WrapAlignment.center,
      crossAxisAlignment: WrapCrossAlignment.center,
      spacing: 4,
      children: [
        Text('$label ·', style: style),
        Text(value.replaceAll(' ', '\u00a0'), style: style, softWrap: false),
      ],
    );
  }
}
