import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/features/home/home_page.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

void main() {
  for (final width in [320.0, 720.0]) {
    for (final scale in [1.0, 2.0]) {
      for (final dark in [false, true]) {
        testWidgets(
          'status labels keep equal bounds at $width/$scale dark=$dark',
          (tester) async {
            await tester.binding.setSurfaceSize(Size(width, 900));
            addTearDown(() => tester.binding.setSurfaceSize(null));
            Size? first;
            for (final label in [
              '已连接',
              '正在认证',
              '等待网络',
              '认证失败',
              '检测到多个连接配置',
              '会话关系不明确',
              '正在断开',
            ]) {
              await tester.pumpWidget(
                MaterialApp(
                  theme: dark ? AppTheme.dark() : AppTheme.light(),
                  builder: (context, child) => MediaQuery(
                    data: MediaQuery.of(context)
                        .copyWith(textScaler: TextScaler.linear(scale)),
                    child: child!,
                  ),
                  home: HomeView(
                    data: HomeViewData(
                      institution: '吉林大学',
                      username: 'student01',
                      state: label,
                      detail: '状态说明',
                      context: '',
                      tone: HomeTone.success,
                      secondaryLabel: '连接与诊断',
                      primaryLabel: '断开连接',
                      primaryKind: HomeButtonKind.secondary,
                      primaryEnabled: true,
                      onHeader: () {},
                      onSettings: () {},
                      onSecondary: () {},
                      onPrimary: () {},
                    ),
                  ),
                ),
              );
              await tester.pumpAndSettle();
              final bounds = tester.getSize(
                find.byKey(const ValueKey('home-mark')),
              );
              first ??= bounds;
              expect(bounds, first);
              expect(
                tester.widget<Text>(find.text(label)).style!.color,
                dark ? Colors.white : Colors.black,
              );
              expect(find.byIcon(Icons.check_rounded), findsNothing);
              expect(tester.takeException(), isNull);
            }
          },
        );
      }
    }
  }
}
