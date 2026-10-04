import 'dart:async';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() => runApp(const OfflineDemoApp());

/// Composition only: every destination is the unchanged production page.
class OfflineDemoApp extends StatefulWidget {
  const OfflineDemoApp({super.key});
  @override
  State<OfflineDemoApp> createState() => _OfflineDemoAppState();
}

class _OfflineDemoAppState extends State<OfflineDemoApp> {
  late final OfflineDemoClient _client;
  late final GuiController _controller;
  late final AnnouncementController _announcements;

  @override
  void initState() {
    super.initState();
    _client = OfflineDemoClient();
    _controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => _client,
    );
    final now = DateTime.now().toUtc();
    _announcements = AnnouncementController(
      endpoint: OfflineAnnouncementFetcher.endpoint,
      store: MemoryAnnouncementStore(),
      fetcher: OfflineAnnouncementFetcher(now),
      nowUtc: () => now,
    );
    unawaited(_controller.start());
    unawaited(_announcements.start());
  }

  @override
  void dispose() {
    _controller.dispose();
    _announcements.dispose();
    _client.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Sidravia 离线演示',
    debugShowCheckedModeBanner: false,
    theme: AppTheme.light(),
    home: Column(
      children: [
        Material(
          color: const Color(0xFFFFF3CF),
          child: SafeArea(
            bottom: false,
            child: SizedBox(
              width: double.infinity,
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 8,
                ),
                child: AnimatedBuilder(
                  animation: _client,
                  builder: (context, _) => Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        '离线演示 · 勿输入真实账号或密码',
                        style: TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        _client.feedback,
                        style: const TextStyle(fontSize: 11),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
        Expanded(
          child: SidraviaShell(
            controller: _controller,
            announcements: _announcements,
          ),
        ),
      ],
    ),
  );
}
