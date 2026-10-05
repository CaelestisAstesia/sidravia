import 'dart:async';
import 'dart:convert';

import 'package:sidravia_gui/features/announcements/announcement_widgets.dart';

import 'package:flutter/services.dart';
import 'package:sidravia_gui/window/sidravia_window_frame.dart';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/dev/offline_demo_client.dart';
import 'package:sidravia_gui/features/announcements/announcement_controller.dart';
import 'package:sidravia_gui/features/announcements/announcement_store.dart';
import 'package:sidravia_gui/features/shell/sidravia_shell.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';

void main() => runApp(const OfflineDemoApp());

/// Composition only: every destination is the unchanged production page.
class OfflineDemoApp extends StatefulWidget {
  const OfflineDemoApp({super.key});
  @override
  State<OfflineDemoApp> createState() => _OfflineDemoAppState();
}

class _OfflineDemoAppState extends State<OfflineDemoApp> {
  final _appearance = Appearance();
  final _windowObserver = SidraviaWindowObserver();
  late final OfflineDemoClient _client;
  late final GuiController _controller;
  late final AnnouncementController _announcements;
  late final OfflineAnnouncementFetcher _fetcher;
  String _scenario = 'authenticated';
  AnnouncementPresentation _noticePresentation =
      AnnouncementPresentation.dialog;

  @override
  void initState() {
    super.initState();
    _client = OfflineDemoClient();
    _controller = GuiController(
      bootstrapper: OfflineDemoBootstrap(),
      connector: (_) async => _client,
    );
    final now = DateTime.now().toUtc();
    _fetcher = OfflineAnnouncementFetcher(now);
    _announcements = AnnouncementController(
      endpoint: OfflineAnnouncementFetcher.endpoint,
      store: MemoryAnnouncementStore(),
      fetcher: _fetcher,
      refreshInterval: Duration.zero,
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
    _appearance.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AppearanceScope(
    appearance: _appearance,
    child: ValueListenableBuilder<ThemeMode>(
      valueListenable: _appearance,
      builder: (context, mode, _) => MaterialApp(
        title: 'Sidravia 离线演示',
        navigatorObservers: [_windowObserver],
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light(),
        darkTheme: AppTheme.dark(),
        themeMode: mode,
        home: SidraviaWindowFrame(
          child: Column(
            children: [
              Material(
                key: const ValueKey('demo-tools'),
                color: const Color(0xFFFFF3CF),
                textStyle: const TextStyle(
                  fontFamily: 'HarmonyOS Sans',
                  color: AppColors.inkLight,
                ),
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
                            SizedBox(
                              height:
                                  MediaQuery.textScalerOf(context).scale(12) *
                                  1.2,
                              child: const Tooltip(
                                message: '离线演示 · 勿输入真实账号或密码',
                                child: Text(
                                  '离线演示 · 勿输入真实账号或密码',
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(
                                    fontSize: 12,
                                    height: 1.2,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                              ),
                            ),
                            const SizedBox(height: 2),
                            SizedBox(
                              height:
                                  MediaQuery.textScalerOf(context).scale(11) *
                                  1.2 *
                                  2,
                              child: Tooltip(
                                message: _client.feedback,
                                child: Text(
                                  _client.feedback,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                    fontSize: 11,
                                    height: 1.2,
                                  ),
                                ),
                              ),
                            ),
                            SizedBox(
                              height: 48,
                              child: ScrollConfiguration(
                                behavior: ScrollConfiguration.of(context)
                                    .copyWith(scrollbars: false),
                                child: SingleChildScrollView(
                                  scrollDirection: Axis.horizontal,
                                  child: Row(
                                    spacing: 8,
                                    children: [
                                      TextButton(
                                        onPressed: () => setState(() {
                                          _noticePresentation =
                                              AnnouncementPresentation
                                                  .values[(_noticePresentation
                                                          .index +
                                                      1) %
                                                  AnnouncementPresentation
                                                      .values
                                                      .length];
                                        }),
                                        child: Text(
                                          '公告预览：${switch (_noticePresentation) {
                                            AnnouncementPresentation.platform => '跟随平台',
                                            AnnouncementPresentation.dialog => 'PC 对话框',
                                            AnnouncementPresentation.bottomSheet => '手机面板',
                                          }}',
                                          style: const TextStyle(fontSize: 11),
                                        ),
                                      ),
                                      if (Theme.of(context).platform ==
                                          TargetPlatform.windows)
                                        TextButton(
                                          onPressed: () async {
                                            String text;
                                            try {
                                              text =
                                                  const JsonEncoder.withIndent(
                                                    '  ',
                                                  ).convert(
                                                    await SidraviaWindowCommands.diagnostics(),
                                                  );
                                            } catch (error) {
                                              text = 'Windows 窗口诊断不可用：$error';
                                            }
                                            if (!context.mounted) return;
                                            await showDialog<void>(
                                              context: context,
                                              builder: (dialogContext) =>
                                                  AlertDialog(
                                                    title: const Text(
                                                      '原生窗口诊断（只读）',
                                                    ),
                                                    content:
                                                        SingleChildScrollView(
                                                          child: SelectableText(
                                                            text,
                                                          ),
                                                        ),
                                                    actions: [
                                                      TextButton(
                                                        onPressed: () =>
                                                            Clipboard.setData(
                                                              ClipboardData(
                                                                text: text,
                                                              ),
                                                            ),
                                                        child: const Text(
                                                          '复制诊断',
                                                        ),
                                                      ),
                                                      TextButton(
                                                        onPressed: () =>
                                                            Navigator.pop(
                                                              dialogContext,
                                                            ),
                                                        child: const Text('关闭'),
                                                      ),
                                                    ],
                                                  ),
                                            );
                                          },
                                          child: const Text(
                                            '窗口诊断',
                                            style: TextStyle(fontSize: 11),
                                          ),
                                        ),
                                      PopupMenuButton<String>(
                                        tooltip: '演示场景',
                                        onSelected: (value) async {
                                          _client.selectScenario(value);
                                          await _controller.start();
                                          if (mounted) {
                                            setState(() => _scenario = value);
                                          }
                                        },
                                        itemBuilder: (context) => [
                                          for (final entry in const {
                                            'authenticated': '已连接',
                                            'suspended': '未连接',
                                            'authenticating': '正在认证',
                                            'waiting_for_network': '等待网络',
                                            'waiting_before_retry': '等待重试',
                                            'blocked_by_error': '认证失败',
                                            'empty': '尚未配置',
                                          }.entries)
                                            PopupMenuItem(
                                              value: entry.key,
                                              child: Text(entry.value),
                                            ),
                                        ],
                                        child: Padding(
                                          padding: const EdgeInsets.all(6),
                                          child: Text(
                                            '场景：$_scenario',
                                            style: const TextStyle(
                                              fontSize: 11,
                                            ),
                                          ),
                                        ),
                                      ),
                                      TextButton(
                                        onPressed: () async {
                                          _fetcher.active = !_fetcher.active;
                                          await _announcements.refreshIfDue();
                                          if (mounted) setState(() {});
                                        },
                                        child: Text(
                                          _fetcher.active
                                              ? '隐藏公告（模拟）'
                                              : '显示公告（模拟）',
                                          style: const TextStyle(fontSize: 11),
                                        ),
                                      ),
                                      TextButton(
                                        onPressed: () async {
                                          _fetcher.active = true;
                                          _fetcher.revision++;
                                          await _announcements.refreshIfDue();
                                          if (mounted) setState(() {});
                                        },
                                        child: const Text(
                                          '更新公告（模拟）',
                                          style: TextStyle(fontSize: 11),
                                        ),
                                      ),
                                      PopupMenuButton<ThemeMode>(
                                        tooltip: '演示外观',
                                        onSelected: (value) =>
                                            _appearance.value = value,
                                        itemBuilder: (context) => const [
                                          PopupMenuItem(
                                            value: ThemeMode.system,
                                            child: Text('跟随系统'),
                                          ),
                                          PopupMenuItem(
                                            value: ThemeMode.light,
                                            child: Text('浅色'),
                                          ),
                                          PopupMenuItem(
                                            value: ThemeMode.dark,
                                            child: Text('深色'),
                                          ),
                                        ],
                                        child: const Padding(
                                          padding: EdgeInsets.all(6),
                                          child: Text(
                                            '演示外观',
                                            style: TextStyle(fontSize: 11),
                                          ),
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
              Expanded(
                child: AnnouncementPresentationScope(
                  presentation: _noticePresentation,
                  child: SidraviaShell(
                    controller: _controller,
                    announcements: _announcements,
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
