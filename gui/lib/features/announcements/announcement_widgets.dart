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
  await showDialog<void>(
    context: context,
    builder: (context) => Dialog(
      insetPadding: const EdgeInsets.all(24),
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(color: Theme.of(context).colorScheme.outline),
      ),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 340, maxHeight: 360),
        child: SizedBox(
          width: 340,
          height: 360,
          child: Column(
            children: [
              SizedBox(
                height: 44,
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(15, 0, 10, 0),
                  child: Row(
                    children: [
                      const Expanded(
                        child: Text(
                          '校园网公告',
                          style: TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      IconButton(
                        tooltip: '关闭公告',
                        onPressed: () => Navigator.pop(context),
                        icon: const Icon(Icons.close, size: 20),
                        visualDensity: VisualDensity.standard,
                        padding: EdgeInsets.zero,
                        constraints: const BoxConstraints.tightFor(
                          width: 42,
                          height: 42,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              Divider(
                height: 1,
                thickness: 1,
                color: Theme.of(context).dividerColor,
              ),
              Expanded(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 20,
                    vertical: 22,
                  ),
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
            ],
          ),
        ),
      ),
    ),
  );
}
