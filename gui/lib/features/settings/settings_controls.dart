part of 'settings_page.dart';

class _AnnouncementSelector<T> extends StatelessWidget {
  const _AnnouncementSelector({
    required this.dropdownKey,
    required this.label,
    required this.value,
    required this.options,
    required this.onChanged,
    required this.helper,
  });
  final Key dropdownKey;
  final String label;
  final String? helper;
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T>? onChanged;
  @override
  Widget build(BuildContext context) => DesignRow(
    title: label,
    subtitle: helper,
    trailing: SettingsDropdown<T>(
      frameKey: ValueKey('announcement-$label-frame'),
      dropdownKey: dropdownKey,
      value: value,
      labels: {for (final option in options) option.$1: option.$2},
      onChanged: onChanged,
    ),
  );
}

class _LogLevelSettingRow extends StatelessWidget {
  const _LogLevelSettingRow({super.key, required this.controls});
  final Row controls;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final scaler = MediaQuery.textScalerOf(context);
      final title = TextPainter(
        text: TextSpan(
          text: '日志等级',
          style: Theme.of(context).textTheme.bodyMedium!
              .copyWith(fontSize: DesignMetrics.bodyFont),
        ),
        textDirection: Directionality.of(context),
        textScaler: scaler,
      )..layout();
      final titleWidth = title.width;
      title.dispose();
      final restart = TextPainter(
        text: TextSpan(text: '重启核心', style: AppButtonStyles.inlineActionText),
        textDirection: Directionality.of(context),
        textScaler: scaler,
      )..layout();
      final restartWidth =
          restart.width.ceilToDouble() +
          2 * AppButtonStyles.inlineActionHorizontalPadding;
      restart.dispose();
      final dropdownWidth = DesignMetrics.choiceWidth(
        MediaQuery.sizeOf(context).width,
      );
      final controlsWidth =
          dropdownWidth + (controls.children.length > 1 ? restartWidth + 8 : 0);
      if (constraints.maxWidth >= titleWidth + controlsWidth + 48) {
        return DesignRow(
          title: '日志等级',
          subtitle: '重启核心后生效',
          trailing: controls,
        );
      }
      return Material(
        color: Colors.transparent,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const DesignRow(title: '日志等级', subtitle: '重启核心后生效'),
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: Wrap(
                alignment: WrapAlignment.end,
                crossAxisAlignment: WrapCrossAlignment.center,
                spacing: 8,
                runSpacing: 8,
                children: controls.children
                    .where((w) => w is! SizedBox)
                    .toList(),
              ),
            ),
          ],
        ),
      );
    },
  );
}

class AppearanceSelector extends StatelessWidget {
  const AppearanceSelector({
    super.key,
    required this.value,
    required this.onChanged,
  });
  final ThemeMode value;
  final ValueChanged<ThemeMode>? onChanged;
  static const labels = {
    ThemeMode.system: '跟随系统',
    ThemeMode.light: '浅色',
    ThemeMode.dark: '深色',
  };
  @override
  Widget build(BuildContext context) => SettingsDropdown<ThemeMode>(
    frameKey: const ValueKey('appearance-selector'),
    value: value,
    labels: labels,
    regionKey: (mode) => ValueKey('appearance-text-region-${mode.name}'),
    onChanged: onChanged,
  );
}
