import 'package:flutter/material.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() => runApp(const _PreviewApp());

class _PreviewApp extends StatelessWidget {
  const _PreviewApp();
  @override
  Widget build(BuildContext context) => MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: AppTheme.light(),
    home: const Scaffold(body: HomeFixture()),
  );
}

/// Fixed, offline fixture for the reference 400x690 design window.
class HomeFixture extends StatelessWidget {
  const HomeFixture({super.key});
  @override
  Widget build(BuildContext context) => HomeView(
    data: HomeViewData(
      institution: '吉林大学',
      username: 'student01',
      state: '已连接',
      detail: '认证成功 · 已连接 2 小时 18 分钟',
      context: '以太网 · 192.168.1.20',
      glyph: '✓',
      tone: HomeTone.success,
      secondaryLabel: '连接详情',
      primaryLabel: '断开连接',
      primaryKind: HomeButtonKind.secondary,
      primaryEnabled: true,
      onHeader: () {},
      onSettings: () {},
      onSecondary: () {},
      onPrimary: () {},
      notice: HomeNotice(title: '校园网维护安排', onOpen: () {}),
    ),
  );
}
