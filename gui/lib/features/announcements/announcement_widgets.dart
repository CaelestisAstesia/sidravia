import 'package:flutter/material.dart';
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

Future<void> showAnnouncementSheet(
  BuildContext context, {
  required AnnouncementController controller,
}) async {
  controller.markCurrentRead();
  final item = controller.announcements.isEmpty
      ? null
      : controller.announcements.first;
  if (item == null) return;
  await showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    builder: (context) => SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 28),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('校园网公告', style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 12),
            Text(
              item.title,
              style: Theme.of(context).textTheme.titleLarge
                  ?.copyWith(fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 10),
            Text(item.body, style: Theme.of(context).textTheme.bodyMedium),
            if (item.actionLabel != null)
              Padding(
                padding: const EdgeInsets.only(top: 18),
                child: Text(
                  item.actionLabel!,
                  style: TextStyle(
                    color: Theme.of(context).colorScheme.primary,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
          ],
        ),
      ),
    ),
  );
}
