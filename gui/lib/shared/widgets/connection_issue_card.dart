import 'package:flutter/material.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class ConnectionIssueCard extends StatelessWidget {
  const ConnectionIssueCard({super.key, required this.failure});
  final SessionAuthenticationFailure failure;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      constraints: const BoxConstraints(maxWidth: 320),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
      decoration: BoxDecoration(
        color: Color.lerp(scheme.surface, scheme.error, .06),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(
          color: Color.lerp(scheme.outline, scheme.error, .35)!,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            failure.description,
            style: const TextStyle(
              fontSize: 13,
              height: 19 / 13,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 5),
          Text(
            failure.handlingRecommendation,
            style: TextStyle(
              fontSize: 12,
              height: 1.5,
              color: scheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            failure.code,
            style: TextStyle(
              fontSize: 11,
              height: 14 / 11,
              fontFamily: 'HarmonyOS Sans',
              color: scheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}
