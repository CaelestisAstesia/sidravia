import 'package:flutter/material.dart';

class DesignPage extends StatelessWidget {
  const DesignPage({super.key, required this.children});
  final List<Widget> children;
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final wide = constraints.maxWidth >= 758;
      return SingleChildScrollView(
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxWidth: 620,
              minHeight: constraints.maxHeight,
            ),
            child: Padding(
              padding: wide
                  ? const EdgeInsets.fromLTRB(36, 16, 36, 32)
                  : const EdgeInsets.fromLTRB(24, 12, 24, 26),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: children,
              ),
            ),
          ),
        ),
      );
    },
  );
}

class DesignHeader extends StatelessWidget {
  const DesignHeader({
    super.key,
    required this.title,
    required this.onBack,
    this.backLabel = '返回',
  });
  final String title, backLabel;
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 24),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TextButton(
          onPressed: onBack,
          style: TextButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 4),
            minimumSize: Size.zero,
            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
            visualDensity: VisualDensity.standard,
            foregroundColor: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.chevron_left, size: 16),
              const SizedBox(width: 7),
              Text(backLabel, style: const TextStyle(fontSize: 13)),
            ],
          ),
        ),
        const SizedBox(height: 12),
        Text(
          title,
          style: const TextStyle(
            fontSize: 20,
            fontWeight: FontWeight.w600,
            letterSpacing: -.2,
          ),
        ),
      ],
    ),
  );
}

class DesignSectionTitle extends StatelessWidget {
  const DesignSectionTitle(this.title, {super.key});
  final String title;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Text(
      title,
      style: TextStyle(
        fontSize: 11,
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
      borderRadius: BorderRadius.circular(11),
      border: Border.all(color: Theme.of(context).colorScheme.outline),
    ),
    child: Column(
      children: [
        for (var i = 0; i < children.length; i++) ...[
          if (i > 0)
            Divider(
              height: 1,
              thickness: 1,
              color: Theme.of(context).dividerColor,
            ),
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
      constraints: BoxConstraints(minHeight: diagnostic ? 45 : 52),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: TextStyle(
                      fontSize: diagnostic ? 12 : 13,
                      fontWeight: diagnostic
                          ? FontWeight.w400
                          : FontWeight.w600,
                      fontFamily: diagnostic ? 'monospace' : null,
                      color: danger ? scheme.error : scheme.onSurface,
                    ),
                  ),
                  if (subtitle != null) ...[
                    const SizedBox(height: 3),
                    Text(
                      subtitle!,
                      style: TextStyle(
                        fontSize: 11,
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
                  child: Text(
                    value!,
                    textAlign: TextAlign.right,
                    style: TextStyle(
                      fontSize: diagnostic ? 11 : 12,
                      fontFamily: diagnostic ? 'monospace' : null,
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                )
              else
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
    return Material(
      color: Colors.transparent,
      child: onTap == null
          ? content
          : InkWell(
              onTap: onTap,
              hoverColor: scheme.surfaceContainerHighest,
              child: content,
            ),
    );
  }
}

class DesignHelper extends StatelessWidget {
  const DesignHelper(this.text, {super.key});
  final String text;
  @override
  Widget build(BuildContext context) => Text(
    text,
    style: TextStyle(
      fontSize: 11,
      height: 1.5,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    ),
  );
}

class DesignSwitch extends StatelessWidget {
  const DesignSwitch({
    super.key,
    required this.value,
    required this.onChanged,
    required this.label,
  });
  final bool value;
  final ValueChanged<bool>? onChanged;
  final String label;
  @override
  Widget build(BuildContext context) {
    final platform = Theme.of(context).platform;
    final touch =
        platform == TargetPlatform.android || platform == TargetPlatform.iOS;
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      label: label,
      toggled: value,
      enabled: onChanged != null,
      child: InkWell(
        onTap: onChanged == null ? null : () => onChanged!(!value),
        borderRadius: BorderRadius.circular(22),
        child: SizedBox(
          width: touch ? 48 : 38,
          height: touch ? 48 : 22,
          child: Center(
            child: Opacity(
              opacity: onChanged == null ? .6 : 1,
              child: Container(
                width: 38,
                height: 22,
                decoration: BoxDecoration(
                  color: value ? scheme.primary : scheme.outline,
                  borderRadius: BorderRadius.circular(22),
                ),
                child: Align(
                  alignment: value
                      ? Alignment.centerRight
                      : Alignment.centerLeft,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 3),
                    child: Container(
                      width: 16,
                      height: 16,
                      decoration: const BoxDecoration(
                        color: Colors.white,
                        shape: BoxShape.circle,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class DesignField extends StatelessWidget {
  const DesignField({
    super.key,
    required this.label,
    required this.child,
    this.error,
  });
  final String label;
  final Widget child;
  final String? error;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Text(
        label,
        style: TextStyle(
          fontSize: 12,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
      const SizedBox(height: 6),
      child,
      if (error != null) ...[
        const SizedBox(height: 6),
        Text(
          error!,
          style: TextStyle(
            fontSize: 11,
            height: 1.5,
            color: Theme.of(context).colorScheme.error,
          ),
        ),
      ],
    ],
  );
}
