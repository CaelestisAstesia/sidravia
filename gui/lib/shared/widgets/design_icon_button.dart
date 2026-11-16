import 'package:sidravia_gui/shared/theme/design_effects.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/design_metrics.dart';

/// One circular target for page navigation and standalone icon actions.
class DesignIconButton extends StatelessWidget {
  const DesignIconButton({
    super.key,
    required this.tooltip,
    required this.icon,
    required this.onPressed,
  });

  final String tooltip;
  final IconData icon;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final size = DesignMetrics.actionTargetSize(theme.platform);
    return DecoratedBox(
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        boxShadow: onPressed == null
            ? const []
            : DesignEffects.subtleShadow(theme.colorScheme),
      ),
      child: IconButton(
        tooltip: tooltip,
        onPressed: onPressed,
        icon: Icon(icon, size: 20),
        style: IconButton.styleFrom(
          fixedSize: Size.square(size),
          padding: EdgeInsets.zero,
          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          visualDensity: VisualDensity.standard,
          foregroundColor: theme.colorScheme.onSurfaceVariant,
          backgroundColor: theme.colorScheme.surface,
          shape: CircleBorder(
            side: BorderSide(color: theme.colorScheme.outline),
          ),
        ),
      ),
    );
  }
}
