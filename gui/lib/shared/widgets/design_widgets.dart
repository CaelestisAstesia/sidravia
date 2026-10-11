import 'package:flutter/material.dart';

class DesignPage extends StatelessWidget {
  const DesignPage({super.key, required this.children, this.pageKey});
  final Key? pageKey;

  /// Preserve reference spacing through 620px, then reach the wide endpoint
  /// continuously over the available parent width (not the capped page width).
  static EdgeInsets paddingForWidth(double width) {
    final t = ((width - 620) / (758 - 620)).clamp(0.0, 1.0);
    return EdgeInsets.fromLTRB(
      24 + 12 * t,
      12 + 4 * t,
      24 + 12 * t,
      26 + 6 * t,
    );
  }

  final List<Widget> children;
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final header = children.firstOrNull is DesignHeader
          ? children.first
          : null;
      if (header != null) {
        final padding = paddingForWidth(constraints.maxWidth);
        return Column(
          children: [
            Align(
              alignment: Alignment.topCenter,
              child: ConstrainedBox(
                key: pageKey,
                constraints: const BoxConstraints(maxWidth: 620),
                child: Padding(
                  padding: EdgeInsets.fromLTRB(
                    padding.left,
                    padding.top,
                    padding.right,
                    0,
                  ),
                  child: header,
                ),
              ),
            ),
            Expanded(
              child: ScrollConfiguration(
                behavior: ScrollConfiguration.of(context)
                    .copyWith(scrollbars: false),
                child: SingleChildScrollView(
                  child: Align(
                    alignment: Alignment.topCenter,
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 620),
                      child: Padding(
                        padding: EdgeInsets.fromLTRB(
                          padding.left,
                          0,
                          padding.right,
                          padding.bottom,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: children.skip(1).toList(),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ],
        );
      }
      return ScrollConfiguration(
        behavior: ScrollConfiguration.of(context).copyWith(scrollbars: false),
        child: SingleChildScrollView(
          child: Align(
            alignment: Alignment.topCenter,
            child: ConstrainedBox(
              key: pageKey,
              constraints: BoxConstraints(
                maxWidth: 620,
                minHeight: constraints.maxHeight,
              ),
              child: Padding(
                padding: paddingForWidth(constraints.maxWidth),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: children,
                ),
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
    this.bottomSpacing = 24,
  });
  final String title, backLabel;
  final double bottomSpacing;
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.only(bottom: bottomSpacing),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TextButton(
          onPressed: onBack,
          style: TextButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 4),
            minimumSize:
                (Theme.of(context).platform == TargetPlatform.android ||
                    Theme.of(context).platform == TargetPlatform.iOS)
                ? const Size(48, 48)
                : Size.zero,
            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
            visualDensity: VisualDensity.standard,
            foregroundColor: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.chevron_left, size: 16),
              const SizedBox(width: 7),
              Text(
                backLabel,
                style: const TextStyle(fontSize: 13, height: 20 / 13),
              ),
            ],
          ),
        ),
        const SizedBox(height: 12),
        Text(
          title,
          style: const TextStyle(
            fontSize: 20,
            height: 1.4,
            fontWeight: FontWeight.w500,
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
      borderRadius: BorderRadius.circular(12),
      border: Border.all(color: Theme.of(context).colorScheme.outline),
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
      constraints: BoxConstraints(minHeight: 56),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
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
                      height: diagnostic ? 1.5 : 19 / 13,
                      fontWeight: diagnostic
                          ? FontWeight.w400
                          : FontWeight.w500,
                      fontFamily: 'HarmonyOS Sans',
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
                      fontFamily: 'HarmonyOS Sans',
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
          height: 1.5,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
      const SizedBox(height: 6),
      ConstrainedBox(
        constraints: BoxConstraints(
          minHeight:
              Theme.of(context).platform == TargetPlatform.android ||
                  Theme.of(context).platform == TargetPlatform.iOS
              ? 48
              : 0,
        ),
        child: child,
      ),
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

Future<T?> showDesignChoice<T>(
  BuildContext context, {
  required String title,
  required T value,
  required List<(T, String)> options,
}) => showDialog<T>(
  context: context,
  builder: (context) => Dialog(
    child: SizedBox(
      width: 360,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .8,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: DesignHeader(
                title: title,
                onBack: () => Navigator.pop(context),
                bottomSpacing: 0,
              ),
            ),
            Flexible(
              child: SingleChildScrollView(
                child: Column(
                  children: [
                    for (final (option, label) in options)
                      ListTile(
                        title: Text(label),
                        trailing: option == value
                            ? const Icon(Icons.check)
                            : null,
                        onTap: () => Navigator.pop(context, option),
                      ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    ),
  ),
);

class DesignChoiceRow<T> extends StatelessWidget {
  const DesignChoiceRow({
    super.key,
    required this.title,
    required this.value,
    required this.options,
    required this.onChanged,
    this.helper,
  });
  final String title;
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T>? onChanged;
  final String? helper;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      DesignRow(
        title: title,
        value: options.where((e) => e.$1 == value).firstOrNull?.$2 ?? '暂无数据',
        onTap: onChanged == null
            ? null
            : () async {
                final selected = await showDesignChoice<T>(
                  context,
                  title: title,
                  value: value,
                  options: options,
                );
                if (selected != null && context.mounted) {
                  onChanged?.call(selected);
                }
              },
      ),
      if (helper != null)
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
          child: DesignHelper(helper!),
        ),
    ],
  );
}

class DesignInputRow extends StatelessWidget {
  const DesignInputRow({
    super.key,
    required this.label,
    required this.controller,
    required this.enabled,
    this.obscureText = false,
    this.hint,
    this.error,
  });
  final String label;
  final TextEditingController controller;
  final bool enabled, obscureText;
  final String? hint, error;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 12),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 56),
          child: Row(
            children: [
              Text(
                label,
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w500,
                ),
              ),
              const SizedBox(width: 16),
              Expanded(
                child: TextField(
                  controller: controller,
                  enabled: enabled,
                  obscureText: obscureText,
                  textAlign: TextAlign.right,
                  decoration: InputDecoration(
                    hintText: hint,
                    isDense: true,
                    filled: false,
                    border: InputBorder.none,
                    enabledBorder: InputBorder.none,
                    focusedBorder: InputBorder.none,
                    disabledBorder: InputBorder.none,
                    contentPadding: const EdgeInsets.symmetric(vertical: 12),
                  ),
                ),
              ),
            ],
          ),
        ),
        if (error != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Text(
              error!,
              style: TextStyle(
                color: Theme.of(context).colorScheme.error,
                fontSize: 12,
              ),
            ),
          ),
      ],
    ),
  );
}
