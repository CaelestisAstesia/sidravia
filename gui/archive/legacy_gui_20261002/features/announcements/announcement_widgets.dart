import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';
import 'package:sidravia_gui/design/sidravia_theme.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_model.dart';
import 'package:url_launcher/url_launcher.dart';

typedef AnnouncementLinkOpener = Future<bool> Function(Uri url);

Future<bool> defaultAnnouncementLinkOpener(Uri url) async {
  try {
    return await launchUrl(url, mode: LaunchMode.externalApplication);
  } on Object {
    return false;
  }
}

class AnnouncementEntryButton extends StatelessWidget {
  const AnnouncementEntryButton({
    super.key,
    required this.controller,
    required this.compact,
    required this.onPressed,
  });

  final AnnouncementController controller;
  final bool compact;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final unread = controller.unreadCount;
        return Semantics(
          key: ValueKey<String>(
            compact ? 'announcement-entry-compact' : 'announcement-entry-wide',
          ),
          container: true,
          button: true,
          label: unread > 0 ? '公告，$unread 条未读' : '公告',
          child: compact
              ? IconButton(
                  tooltip: '公告',
                  onPressed: onPressed,
                  icon: Badge(
                    isLabelVisible: unread > 0,
                    smallSize: 8,
                    child: const Icon(Icons.campaign_outlined),
                  ),
                )
              : SizedBox(
                  width: double.infinity,
                  child: TextButton.icon(
                    onPressed: onPressed,
                    style: TextButton.styleFrom(
                      alignment: Alignment.center,
                      minimumSize: const Size.fromHeight(48),
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                      foregroundColor: Theme.of(context)
                          .colorScheme
                          .onSurfaceVariant,
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(14),
                      ),
                    ),
                    icon: Badge(
                      isLabelVisible: unread > 0,
                      smallSize: 8,
                      child: const Icon(Icons.campaign_outlined),
                    ),
                    label: const Text('公告'),
                  ),
                ),
        );
      },
    );
  }
}

Future<void> showAnnouncementSheet(
  BuildContext context, {
  required AnnouncementController controller,
  required bool wide,
  AnnouncementLinkOpener? linkOpener,
}) {
  controller.markCurrentRead();
  if (wide) {
    return showGeneralDialog<void>(
      context: context,
      barrierDismissible: true,
      barrierLabel: '关闭公告',
      barrierColor: Colors.black45,
      transitionDuration: const Duration(milliseconds: 200),
      pageBuilder: (dialogContext, _, _) => Align(
        alignment: Alignment.centerRight,
        child: Material(
          color: Theme.of(context).scaffoldBackgroundColor,
          elevation: 8,
          child: SizedBox(
            width: 420,
            height: double.infinity,
            child: SafeArea(
              child: AnnouncementSheetView(
                controller: controller,
                linkOpener: linkOpener,
              ),
            ),
          ),
        ),
      ),
    );
  }
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    backgroundColor: Theme.of(context).scaffoldBackgroundColor,
    builder: (sheetContext) => FractionallySizedBox(
      heightFactor: 0.92,
      child: AnnouncementSheetView(
        controller: controller,
        linkOpener: linkOpener,
      ),
    ),
  );
}

class AnnouncementSheetView extends StatefulWidget {
  const AnnouncementSheetView({
    super.key,
    required this.controller,
    this.linkOpener,
  });

  final AnnouncementController controller;
  final AnnouncementLinkOpener? linkOpener;

  @override
  State<AnnouncementSheetView> createState() => _AnnouncementSheetViewState();
}

class _AnnouncementSheetViewState extends State<AnnouncementSheetView> {
  bool _linkFailed = false;

  void _openAction(Announcement item) {
    final url = item.actionUrl;
    if (url == null) return;
    unawaited(_openLink(url));
  }

  Future<void> _openLink(Uri url) async {
    final opener = widget.linkOpener ?? defaultAnnouncementLinkOpener;
    final opened = await opener(url);
    if (!opened && mounted) setState(() => _linkFailed = true);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 12, 8, 8),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  '公告',
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              IconButton(
                tooltip: '关闭',
                icon: const Icon(Icons.close),
                onPressed: () => Navigator.of(context).maybePop(),
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        Expanded(
          child: AnimatedBuilder(
            animation: widget.controller,
            builder: (context, _) {
              final items = widget.controller.announcements;
              if (items.isEmpty) {
                return const Center(
                  key: ValueKey<String>('announcement-sheet-empty'),
                  child: Text('暂无公告'),
                );
              }
              return ListView.separated(
                key: const ValueKey<String>('announcement-sheet'),
                padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
                itemCount: items.length + (_linkFailed ? 1 : 0),
                separatorBuilder: (_, _) => const SizedBox(height: 12),
                itemBuilder: (context, index) {
                  if (index == items.length) {
                    return Text(
                      '无法打开链接',
                      key: const ValueKey<String>('announcement-link-failure'),
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.error,
                      ),
                    );
                  }
                  return _AnnouncementCard(
                    item: items[index],
                    onOpenAction: _openAction,
                  );
                },
              );
            },
          ),
        ),
      ],
    );
  }
}

class _AnnouncementCard extends StatelessWidget {
  const _AnnouncementCard({required this.item, required this.onOpenAction});

  final Announcement item;
  final ValueChanged<Announcement> onOpenAction;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final (label, color, icon) = switch (item.level) {
      AnnouncementLevel.critical => (
        '紧急',
        SidraviaColors.danger,
        Icons.error_outline,
      ),
      AnnouncementLevel.maintenance => (
        '维护',
        SidraviaColors.warning,
        Icons.build_outlined,
      ),
      AnnouncementLevel.info => (
        '公告',
        SidraviaColors.neutral,
        Icons.campaign_outlined,
      ),
    };
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 16, color: color),
              const SizedBox(width: 6),
              Text(
                label,
                style: theme.textTheme.labelSmall?.copyWith(color: color),
              ),
              const Spacer(),
              Text(
                _formatDate(item.publishedAt),
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            item.title,
            style: theme.textTheme.titleSmall?.copyWith(
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            item.body,
            style: theme.textTheme.bodySmall?.copyWith(
              height: 1.4,
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          if (item.actionLabel case final actionLabel?) ...[
            const SizedBox(height: 12),
            OutlinedButton(
              onPressed: () => onOpenAction(item),
              child: Text(actionLabel),
            ),
          ],
        ],
      ),
    );
  }

  static String _formatDate(DateTime utc) {
    final local = utc.toLocal();
    String two(int value) => value.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)}';
  }
}

class AnnouncementInlineNotice extends StatelessWidget {
  const AnnouncementInlineNotice({
    super.key,
    required this.controller,
    required this.compact,
  });

  final AnnouncementController controller;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final item = controller.inlineNotice;
        if (item == null) return const SizedBox.shrink();
        final theme = Theme.of(context);
        final (color, icon) = switch (item.level) {
          AnnouncementLevel.critical => (
            SidraviaColors.danger,
            Icons.error_outline,
          ),
          _ => (SidraviaColors.warning, Icons.build_outlined),
        };
        return Container(
          key: const ValueKey<String>('announcement-inline-notice'),
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: color.withValues(alpha: 0.5)),
          ),
          child: Row(
            children: [
              Icon(icon, size: 18, color: color),
              const SizedBox(width: 10),
              Expanded(
                child: SidraviaLayout.limitTextScale(
                  compact: compact,
                  maxScaleFactor: SidraviaLayout.compactContentMaxTextScale,
                  child: Text(
                    item.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ),
              IconButton(
                tooltip: '关闭',
                visualDensity: VisualDensity.compact,
                icon: const Icon(Icons.close, size: 18),
                color: theme.colorScheme.onSurfaceVariant,
                onPressed: () => controller.dismissInline(item.key),
              ),
            ],
          ),
        );
      },
    );
  }
}
