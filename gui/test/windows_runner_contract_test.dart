import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('Windows runner keeps a resizable fused light frame', () {
    final runner = File('windows/runner/win32_window.cpp').readAsStringSync();
    final main = File('windows/runner/main.cpp').readAsStringSync();

    expect(runner, contains('WS_OVERLAPPEDWINDOW'));
    expect(runner, contains('DWMWA_CAPTION_COLOR'));
    expect(runner, contains('DWMWA_TEXT_COLOR'));
    expect(runner, contains('RGB(0xF9, 0xFA, 0xFC)'));
    expect(runner, contains('RGB(0x17, 0x20, 0x33)'));
    expect(runner, contains('WM_GETMINMAXINFO'));
    expect(runner, contains('kMinimumWindowWidth = 900'));
    expect(runner, contains('kMinimumWindowHeight = 600'));
    expect(main, contains('Win32Window::Size size(1280, 720)'));
  });
}
