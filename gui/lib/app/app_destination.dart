import 'package:flutter/material.dart';

enum AppPage { home, settings, configuration, details, diagnostics }

extension AppPageCopy on AppPage {
  String get title => switch (this) {
    AppPage.home => '连接',
    AppPage.settings => '设置',
    AppPage.configuration => '连接配置',
    AppPage.details => '连接详情',
    AppPage.diagnostics => '技术诊断',
  };

  IconData get icon => switch (this) {
    AppPage.home => Icons.wifi_rounded,
    AppPage.settings => Icons.settings_outlined,
    AppPage.configuration => Icons.badge_outlined,
    AppPage.details => Icons.info_outline,
    AppPage.diagnostics => Icons.code_outlined,
  };
}
