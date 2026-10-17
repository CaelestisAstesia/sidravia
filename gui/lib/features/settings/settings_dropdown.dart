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
    double textHeight = 0;
    for (final label in labels.values) {
      final painter = TextPainter(
        text: TextSpan(text: label, style: style),
        textDirection: Directionality.of(context),
        textScaler: MediaQuery.textScalerOf(context),
      )..layout();
      if (painter.height > textHeight) textHeight = painter.height;
      painter.dispose();
    }
    final platform = Theme.of(context).platform;
    final touch =
        platform == TargetPlatform.android || platform == TargetPlatform.iOS;
    return Container(
      key: frameKey,
      width: DesignMetrics.choiceWidth(MediaQuery.sizeOf(context).width),
      height: (textHeight + DesignMetrics.rowPadding).clamp(
        touch ? DesignMetrics.touchTarget : DesignMetrics.choiceMinHeight,
        double.infinity,
      ),
      alignment: Alignment.center,
      padding: const EdgeInsets.symmetric(horizontal: 7),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        border: Border.all(color: scheme.outline),
        borderRadius: BorderRadius.circular(DesignMetrics.choiceRadius),
      ),
      child: DropdownButton<T>(
        key: dropdownKey,
        dropdownColor: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(DesignMetrics.groupRadius),
        elevation: 3,
        value: value,
        hint: hint == null ? null : Center(child: Text(hint!, style: style)),
        isDense: true,
        isExpanded: true,
        alignment: Alignment.center,
        underline: const SizedBox.shrink(),
        style: style,
        icon: const SizedBox(
          width: 24,
          child: Icon(Icons.arrow_drop_down, size: 20),
        ),
        selectedItemBuilder: (context) => labels.entries
            .map(
              (entry) => Center(
                key: regionKey?.call(entry.key),
                child: Text(
                  entry.value,
                  style: style,
                  textAlign: TextAlign.center,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            )
            .toList(),
        items: labels.entries
            .map(
              (entry) => DropdownMenuItem<T>(
                value: entry.key,
                enabled: !disabledValues.contains(entry.key),
                alignment: Alignment.center,
                child: Text(
                  entry.value,
                  style: style,
                  textAlign: TextAlign.center,
                ),
              ),
            )
            .toList(),
        onChanged: onChanged == null
            ? null
            : (selected) {
                if (selected != null) onChanged!(selected);
              },
      ),
    );
  }
}
