import 'package:sidravia_gui/shared/theme/design_effects.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/design_metrics.dart';

/// Short display hints omit terminal full stops; paragraphs and values keep
/// their original punctuation at their own presentation boundary.
String shortUiText(String text) =>
    text.trimRight().replaceFirst(RegExp(r'[。.]+$'), '');

class DesignSectionTitle extends StatelessWidget {
  const DesignSectionTitle(this.title, {super.key});
  final String title;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Text(
      title,
      style: TextStyle(
        fontSize: DesignMetrics.captionFont,
        height: 16 / 11,
        letterSpacing: .66,
        fontWeight: FontWeight.w700,
        color: Theme.of(context).colorScheme.onSurfaceVariant,
      ),
    ),
  );
}

class DesignGroup extends StatelessWidget {
  const DesignGroup({super.key, required this.children});
  final List<Widget> children;
  @override
  Widget build(BuildContext context) => Container(
    clipBehavior: Clip.antiAlias,
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.surface,
      borderRadius: BorderRadius.circular(DesignMetrics.groupRadius),
      border: Border.all(color: Theme.of(context).colorScheme.outline),
      boxShadow: DesignEffects.subtleShadow(Theme.of(context).colorScheme),
    ),
    child: Column(
      children: [
        for (var i = 0; i < children.length; i++) ...[
          if (i > 0 && !children.every((w) => w is DesignRow && w.diagnostic))
            Divider(
              height: 1,
              thickness: 1,
              color: Theme.of(context).dividerColor,
            ),
          if (i > 0 && children.every((w) => w is DesignRow && w.diagnostic))
            DecoratedBox(
              position: DecorationPosition.foreground,
              decoration: BoxDecoration(
                border: Border(
                  top: BorderSide(color: Theme.of(context).dividerColor),
                ),
              ),
              child: children[i],
            )
          else
            children[i],
        ],
      ],
    ),
  );
}

class DesignRow extends StatelessWidget {
  const DesignRow({
    super.key,
    required this.title,
    this.subtitle,
    this.value,
    this.trailing,
    this.onTap,
    this.danger = false,
    this.diagnostic = false,
  });
  final String title;
  final String? subtitle, value;
  final Widget? trailing;
  final VoidCallback? onTap;
  final bool danger, diagnostic;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final content = ConstrainedBox(
      constraints: BoxConstraints(minHeight: DesignMetrics.rowMinHeight),
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: DesignMetrics.rowPadding,
          vertical: DesignMetrics.rowPadding,
        ),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: TextStyle(
                      fontSize: diagnostic
                          ? DesignMetrics.valueFont
                          : DesignMetrics.bodyFont,
                      height: diagnostic ? 1.5 : 19 / 13,
                      fontWeight: diagnostic
                          ? FontWeight.w400
                          : FontWeight.w500,
                      color: danger ? scheme.error : scheme.onSurface,
                    ),
                  ),
                  if (subtitle != null) ...[
                    const SizedBox(height: DesignMetrics.subtitleGap),
                    Text(
                      shortUiText(subtitle!),
                      style: TextStyle(
                        fontSize: DesignMetrics.captionFont,
                        height: 1.35,
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ],
              ),
            ),
            if (value != null || trailing != null || onTap != null) ...[
              const SizedBox(width: 16),
              if (value != null)
                Flexible(
                  fit: FlexFit.tight,
                  child: Text(
                    value!,
                    textAlign: TextAlign.right,
                    style: TextStyle(
                      fontSize: diagnostic
                          ? DesignMetrics.captionFont
                          : DesignMetrics.valueFont,
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                ),
              if (value != null && onTap != null)
                Icon(
                  Icons.chevron_right,
                  size: 18,
                  color: scheme.onSurfaceVariant,
                ),
              if (value == null)
                trailing ??
                    Icon(
                      Icons.chevron_right,
                      size: 18,
                      color: scheme.onSurfaceVariant,
                    ),
            ],
          ],
        ),
      ),
    );
    return Semantics(
      button: onTap != null,
      child: Material(
        color: Colors.transparent,
        child: onTap == null
            ? content
            : InkWell(
                onTap: onTap,
                hoverColor: scheme.surfaceContainerHighest,
                child: content,
              ),
      ),
    );
  }
}

class DesignHelper extends StatelessWidget {
  const DesignHelper(this.text, {super.key});
  final String text;
  @override
  Widget build(BuildContext context) => Text(
    shortUiText(text),
    style: TextStyle(
      fontSize: DesignMetrics.captionFont,
      height: 1.5,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    ),
  );
}
