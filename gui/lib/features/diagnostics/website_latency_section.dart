import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

import 'website_latency.dart';

/// Renders the independent page controller; never reads daemon state.
class WebsiteLatencySection extends StatelessWidget {
  const WebsiteLatencySection({super.key, required this.controller});
  final WebsiteLatencyController controller;

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: controller,
    builder: (context, _) {
      final time = controller.testedAt;
      final timeLabel = time == null
          ? null
          : '${time.hour.toString().padLeft(2, '0')}:'
                '${time.minute.toString().padLeft(2, '0')}:'
                '${time.second.toString().padLeft(2, '0')}';
      return LayoutBuilder(
        builder: (context, constraints) {
          final resultWidth = math.min(
            96 * MediaQuery.textScalerOf(context).scale(12) / 12,
            constraints.maxWidth * .45,
          );
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const DesignSectionTitle('网络连通性测试'),
              DesignGroup(
                children: [
                  DesignRow(
                    title: 'HTTPS 响应耗时',
                    subtitle: !controller.available
                        ? '当前环境不支持直连测试'
                        : timeLabel == null
                        ? '应用直连 · 不代表校园网认证状态'
                        : '测试于 $timeLabel · 应用直连',
                    trailing: SizedBox(
                      width: resultWidth,
                      child: OutlinedButton(
                        onPressed: !controller.available
                            ? null
                            : () {
                                if (controller.running) {
                                  controller.cancel();
                                } else {
                                  controller.test();
                                }
                              },
                        child: Text(controller.running ? '取消' : '测试'),
                      ),
                    ),
                  ),
                  for (final name in websiteLatencyTargets.keys)
                    DesignRow(
                      title: name,
                      trailing: SizedBox(
                        width: resultWidth,
                        child: Text(
                          _label(controller.resultFor(name)),
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            fontSize: 12,
                            color: Theme.of(context)
                                .colorScheme
                                .onSurfaceVariant,
                          ),
                        ),
                      ),
                    ),
                ],
              ),
              const SizedBox(height: 24),
            ],
          );
        },
      );
    },
  );
}

String _label(WebsiteLatencyResult result) => switch (result.status) {
  WebsiteLatencyStatus.untested => '未测试',
  WebsiteLatencyStatus.waiting => '等待中',
  WebsiteLatencyStatus.testing => '测试中…',
  WebsiteLatencyStatus.success => '${result.milliseconds} ms',
  WebsiteLatencyStatus.timeout => '超时',
  WebsiteLatencyStatus.failed => '失败',
  WebsiteLatencyStatus.restricted => '访问受限',
  WebsiteLatencyStatus.unexpected => '响应异常',
  WebsiteLatencyStatus.cancelled => '已取消',
};
