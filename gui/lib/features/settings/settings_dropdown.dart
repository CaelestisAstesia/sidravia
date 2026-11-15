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
    // Both surfaces use a common height, measured with the trigger's narrower
    // text area (padding and trailing arrow excluded).
    final textWidth = width - 14 - 24;
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
      padding: const EdgeInsets.symmetric(horizontal: 7),
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
      visualDensity: VisualDensity.standard,
      alignment: AlignmentDirectional.centerStart,
      textStyle: style,
      foregroundColor: scheme.onSurface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(DesignMetrics.choiceRadius),
      ),
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
              style: buttonStyle,
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
          decoration: BoxDecoration(
            color: scheme.surfaceContainerHighest,
            border: Border.all(color: scheme.outline),
            borderRadius: BorderRadius.circular(DesignMetrics.choiceRadius),
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
                Expanded(
                  child: Text(
                    labels[value] ?? hint ?? '',
                    key: value == null ? null : regionKey?.call(value as T),
                    style: style,
                    textAlign: TextAlign.start,
                  ),
                ),
                const SizedBox(
                  width: 24,
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
