import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/design_metrics.dart';

class SettingsDropdown<T> extends StatelessWidget {
  const SettingsDropdown({
    super.key,
    required this.frameKey,
    required this.value,
    required this.labels,
    required this.onChanged,
    this.dropdownKey,
    this.regionKey,
    this.hint,
    this.disabledValues = const {},
  });
  final Key frameKey;
  final Key? dropdownKey;
  final T? value;
  final Map<T, String> labels;
  final ValueChanged<T>? onChanged;
  final Key Function(T)? regionKey;
  final String? hint;
  final Set<T> disabledValues;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final style = TextStyle(
      fontFamily: 'HarmonyOS Sans',
      fontSize: DesignMetrics.captionFont,
      height: 1.35,
      color: onChanged == null ? scheme.onSurfaceVariant : scheme.onSurface,
    );
    final width = DesignMetrics.choiceWidth(MediaQuery.sizeOf(context).width);
    // Reserve matching leading/trailing slots so the label is centered in the
    // entire capsule; measure long labels using that same available width.
    const horizontalPadding = 12.0;
    const arrowWidth = 20.0;
    final textWidth = width - 2 * horizontalPadding - 2 * arrowWidth;
    double textHeight = 0;
    for (final label in [...labels.values, ?hint]) {
      final painter = TextPainter(
        text: TextSpan(text: label, style: style),
        textDirection: Directionality.of(context),
        textScaler: MediaQuery.textScalerOf(context),
      )..layout(maxWidth: textWidth);
      if (painter.height > textHeight) textHeight = painter.height;
      painter.dispose();
    }
    final platform = Theme.of(context).platform;
    final touch =
        platform == TargetPlatform.android || platform == TargetPlatform.iOS;
    final height = (textHeight + DesignMetrics.rowPadding)
        .clamp(
          touch ? DesignMetrics.touchTarget : DesignMetrics.choiceMinHeight,
          double.infinity,
        )
        .toDouble();
    final buttonStyle = TextButton.styleFrom(
      fixedSize: Size(width, height),
      minimumSize: Size.zero,
      maximumSize: Size(width, height),
      padding: const EdgeInsets.symmetric(horizontal: horizontalPadding),
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
      visualDensity: VisualDensity.standard,
      alignment: AlignmentDirectional.centerStart,
      textStyle: style,
      foregroundColor: scheme.onSurface,
      shape: const StadiumBorder(),
    );
    return SizedBox(
      key: frameKey,
      width: width,
      height: height,
      child: MenuAnchor(
        crossAxisUnconstrained: false,
        style: MenuStyle(
          padding: const WidgetStatePropertyAll(EdgeInsets.zero),
          minimumSize: WidgetStatePropertyAll(Size(width, 0)),
          maximumSize: WidgetStatePropertyAll(Size(width, double.infinity)),
          backgroundColor: WidgetStatePropertyAll(
            scheme.surfaceContainerHighest,
          ),
          elevation: const WidgetStatePropertyAll(3),
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(DesignMetrics.groupRadius),
            ),
          ),
        ),
        menuChildren: [
          for (final entry in labels.entries)
            MenuItemButton(
              autofocus: entry.key == value,
              trailingIcon: entry.key == value
                  ? const Icon(Icons.check, size: 16)
                  : null,
              style: buttonStyle.copyWith(
                shape: const WidgetStatePropertyAll(RoundedRectangleBorder()),
              ),
              onPressed: onChanged == null || disabledValues.contains(entry.key)
                  ? null
                  : () => onChanged!(entry.key),
              child: Text(
                entry.value,
                style: style,
                textAlign: TextAlign.start,
              ),
            ),
        ],
        builder: (context, menu, _) => DecoratedBox(
          decoration: ShapeDecoration(
            color: scheme.surfaceContainerHighest,
            shape: StadiumBorder(side: BorderSide(color: scheme.outline)),
            shadows: [
              BoxShadow(
                color: scheme.shadow.withValues(alpha: .06),
                blurRadius: 4,
                offset: const Offset(0, 1),
              ),
            ],
          ),
          child: TextButton(
            key: dropdownKey,
            style: buttonStyle,
            onPressed: onChanged == null
                ? null
                : () {
                    if (menu.isOpen) {
                      menu.close();
                    } else {
                      menu.open();
                    }
                  },
            child: Row(
              children: [
                const SizedBox(width: arrowWidth),
                Expanded(
                  child: Text(
                    labels[value] ?? hint ?? '',
                    key: value == null ? null : regionKey?.call(value as T),
                    style: style,
                    textAlign: TextAlign.center,
                  ),
                ),
                const SizedBox(
                  width: arrowWidth,
                  child: Icon(Icons.arrow_drop_down, size: 20),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
