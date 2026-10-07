import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';

class AnnouncementEntry extends StatelessWidget {
  const AnnouncementEntry({
    super.key,
    required this.controller,
    required this.onOpen,
  });
  final AnnouncementController controller;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final item =
            controller.inlineNotice ??
            (controller.announcements.isEmpty
                ? null
                : controller.announcements.first);
        if (item == null) return const SizedBox.shrink();
        return Padding(
          padding: EdgeInsets.zero,
          child: TextButton(
            onPressed: () {
              controller.markCurrentRead();
              onOpen();
            },
            style: TextButton.styleFrom(
              alignment: Alignment.centerLeft,
              padding: const EdgeInsets.only(top: 18),
              shape: const RoundedRectangleBorder(),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        '校园网公告',
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.primary,
                          fontSize: 12,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    const Icon(Icons.chevron_right, size: 20),
                  ],
                ),
                const SizedBox(height: 7),
                Text(
                  item.title,
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 3),
                Text('点击查看公告页面', style: Theme.of(context).textTheme.bodySmall),
              ],
            ),
          ),
        );
      },
    );
  }
}

/// An explicit preview policy is injected only by development composition.
/// Production follows platform capabilities, never the window's width.
enum AnnouncementPresentation { platform, dialog, bottomSheet }

class AnnouncementPresentationScope extends InheritedWidget {
  const AnnouncementPresentationScope({
    super.key,
    required this.presentation,
    required super.child,
  });
  final AnnouncementPresentation presentation;
  static AnnouncementPresentation of(BuildContext context) =>
      context
          .dependOnInheritedWidgetOfExactType<AnnouncementPresentationScope>()
          ?.presentation ??
      AnnouncementPresentation.platform;
  @override
  bool updateShouldNotify(AnnouncementPresentationScope oldWidget) =>
      oldWidget.presentation != presentation;
}

Future<void> showAnnouncementSheet(
  BuildContext context, {
  required AnnouncementController controller,
}) async {
  controller.markCurrentRead();
  final item = controller.announcements.isEmpty
      ? null
      : controller.announcements.first;
  if (item == null) return;
  final strategy = AnnouncementPresentationScope.of(context);
  final platform = Theme.of(context).platform;
  final mobile =
      strategy == AnnouncementPresentation.bottomSheet ||
      strategy == AnnouncementPresentation.platform &&
          (platform == TargetPlatform.android ||
              platform == TargetPlatform.iOS);
  final shape = RoundedRectangleBorder(
    borderRadius: BorderRadius.circular(12),
    side: BorderSide(color: Theme.of(context).colorScheme.outline),
  );
  if (!mobile) {
    await showDialog<void>(
      context: context,
      builder: (context) => Dialog(
        insetPadding: const EdgeInsets.all(24),
        backgroundColor: Theme.of(context).colorScheme.surface,
        shape: shape,
        child: ConstrainedBox(
          key: const ValueKey('announcement-panel'),
          constraints: const BoxConstraints(maxWidth: 340, maxHeight: 360),
          child: SizedBox(
            width: 340,
            height: 360,
            child: AnnouncementContent(item: item),
          ),
        ),
      ),
    );
    return;
  }
  await showModalBottomSheet<void>(
    context: context,
    useRootNavigator: true,
    isScrollControlled: true,
    useSafeArea: true,
    // Framework gives the handle its own drag recognizer. Reading the body can
    // scroll/resize the panel but cannot accidentally dismiss it at min extent.
    showDragHandle: true,
    enableDrag: false,
    isDismissible: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: RoundedRectangleBorder(
      borderRadius: const BorderRadius.vertical(top: Radius.circular(16)),
      side: BorderSide(color: Theme.of(context).colorScheme.outline),
    ),
    constraints: const BoxConstraints(maxWidth: 620),
    builder: (context) => Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SafeArea(
        top: false,
        child: LayoutBuilder(
          builder: (context, constraints) {
            final available = constraints.maxHeight;
            final minimum =
                ((AnnouncementContent.headerHeight(context, true) + 48) /
                        available)
                    .clamp(.20, 1.0);
            final initial =
                (AnnouncementContent.naturalHeight(
                          context,
                          item,
                          constraints.maxWidth,
                        ) /
                        available)
                    .clamp(minimum, 1.0);
            return DraggableScrollableSheet(
              key: const ValueKey('announcement-mobile-sheet'),
              expand: false,
              minChildSize: minimum,
              initialChildSize: initial,
              maxChildSize: 1,
              shouldCloseOnMinExtent: false,
              builder: (context, scrollController) => AnnouncementContent(
                item: item,
                mobile: true,
                scrollController: scrollController,
              ),
            );
          },
        ),
      ),
    ),
  );
}

/// The same header/body/data render in both shells; only scrolling ownership
/// and the mobile touch target minimum differ.
class AnnouncementContent extends StatefulWidget {
  const AnnouncementContent({
    super.key,
    required this.item,
    this.mobile = false,
    this.scrollController,
  });
  final Announcement item;
  final bool mobile;
  final ScrollController? scrollController;
  static double headerHeight(BuildContext context, bool mobile) => math.max(
    mobile ? 48 : 44,
    MediaQuery.textScalerOf(context).scale(13) * 1.2 + 16,
  );
  static double naturalHeight(
    BuildContext context,
    Announcement item,
    double width,
  ) {
    double height(String text, TextStyle style) {
      final painter = TextPainter(
        text: TextSpan(
          text: text,
          style: DefaultTextStyle.of(context).style.merge(style),
        ),
        textScaler: MediaQuery.textScalerOf(context),
        textDirection: Directionality.of(context),
      )..layout(maxWidth: math.max(1, width - 40));
      final result = painter.height;
      painter.dispose();
      return result;
    }

    return headerHeight(context, true) +
        1 +
        44 +
        height(
          item.title,
          const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
        ) +
        10 +
        height(item.body, const TextStyle(fontSize: 12, height: 1.65)) +
        (item.actionLabel == null
            ? 0
            : 18 + height(item.actionLabel!, const TextStyle()));
  }

  @override
  State<AnnouncementContent> createState() => _AnnouncementContentState();
}

class _AnnouncementContentState extends State<AnnouncementContent> {
  final _ownedController = ScrollController();
  @override
  void dispose() {
    _ownedController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.item;
    final controller = widget.scrollController ?? _ownedController;
    return Column(
      children: [
        SizedBox(
          height: AnnouncementContent.headerHeight(context, widget.mobile),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(15, 0, 10, 0),
            child: Row(
              children: [
                const Expanded(
                  child: Text(
                    '校园网公告',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                  ),
                ),
                IconButton(
                  tooltip: '关闭公告',
                  onPressed: () => Navigator.pop(context),
                  icon: const Icon(Icons.close, size: 20),
                  visualDensity: VisualDensity.standard,
                  padding: EdgeInsets.zero,
                  constraints: BoxConstraints.tightFor(
                    width: widget.mobile ? 48 : 42,
                    height: widget.mobile ? 48 : 42,
                  ),
                ),
              ],
            ),
          ),
        ),
        Divider(height: 1, thickness: 1, color: Theme.of(context).dividerColor),
        Expanded(
          child: ScrollConfiguration(
            behavior: ScrollConfiguration.of(context)
                .copyWith(scrollbars: false),
            child: SingleChildScrollView(
              key: const ValueKey('announcement-scroll'),
              controller: controller,
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 22),
              child: Focus(
                autofocus: true,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      item.title,
                      style: const TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 10),
                    Text(
                      item.body,
                      style: TextStyle(
                        fontSize: 12,
                        height: 1.65,
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                    if (item.actionLabel != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 18),
                        child: Text(
                          item.actionLabel!,
                          style: TextStyle(
                            color: Theme.of(context).colorScheme.primary,
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}
