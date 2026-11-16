import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/design_metrics.dart';

import 'design_icon_button.dart';

class DesignHeader extends StatelessWidget {
  const DesignHeader({
    super.key,
    required this.title,
    required this.onBack,
    this.bottomSpacing = 24,
    this.titleFontSize = DesignMetrics.titleFont,
    this.actions = const [],
  });
  final String title;
  final double bottomSpacing, titleFontSize;
  final VoidCallback onBack;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) {
    final buttonSize = DesignMetrics.actionTargetSize(
      Theme.of(context).platform,
    );
    Widget backButton() => DesignIconButton(
      key: const ValueKey('design-back-button'),
      tooltip: '返回',
      icon: Icons.arrow_back,
      onPressed: onBack,
    );
    return Padding(
      padding: EdgeInsets.only(bottom: bottomSpacing),
      child: Row(
        children: [
          backButton(),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              title,
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: titleFontSize,
                height: 1.4,
                fontWeight: FontWeight.w500,
                letterSpacing: -.2,
              ),
            ),
          ),
          const SizedBox(width: 12),
          if (actions.isEmpty)
            SizedBox(width: buttonSize, height: buttonSize)
          else
            Row(mainAxisSize: MainAxisSize.min, children: actions),
        ],
      ),
    );
  }
}
