import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/app_destination.dart';

void main() {
  test('MVP routes preserve the HTML labels', () {
    expect(AppPage.home.title, '连接');
    expect(AppPage.configuration.title, '连接配置');
    expect(AppPage.diagnostics.title, '连接与诊断');
    expect(AppPage.values, hasLength(4));
  });
}
