import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('Windows runner owns a DPI-aware resizable custom frame', () {
    final runner = File('windows/runner/win32_window.cpp').readAsStringSync();
    final main = File('windows/runner/main.cpp').readAsStringSync();
    final flutterWindow = File('windows/runner/flutter_window.cpp')
        .readAsStringSync();

    expect(runner, isNot(contains('WS_OVERLAPPEDWINDOW')));
    expect(runner, contains('WS_POPUP | WS_THICKFRAME'));
    expect(runner, contains('WS_MINIMIZEBOX'));
    expect(runner, contains('WS_MAXIMIZEBOX'));
    expect(runner, contains('WS_SYSMENU'));
    expect(runner, contains('WM_NCCALCSIZE'));
    expect(runner, contains('WM_NCHITTEST'));
    expect(runner, contains('HTCAPTION'));
    expect(runner, contains('kTitleBarHeight = 40'));
    expect(runner, contains('kWindowControlsWidth = 92'));
    expect(runner, contains('WM_GETMINMAXINFO'));
    expect(runner, contains('kMinimumWindowWidth = 900'));
    expect(runner, contains('kMinimumWindowHeight = 600'));
    expect(flutterWindow, contains('"sidravia/window"'));
    expect(flutterWindow, contains('call.method_name() == "minimize"'));
    expect(flutterWindow, contains('ShowWindow(window, SW_MINIMIZE)'));
    expect(flutterWindow, contains('call.method_name() == "close"'));
    expect(flutterWindow, contains('PostMessage(window, WM_CLOSE, 0, 0)'));
    expect(main, contains('Win32Window::Size size(1280, 720)'));
  });
}
