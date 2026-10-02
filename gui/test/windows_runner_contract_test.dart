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
    expect(runner, contains('kWindowControlsWidth = 138'));
    expect(runner, contains('WM_GETMINMAXINFO'));
    expect(runner, contains('kMinimumWindowWidth = 900'));
    expect(runner, contains('kMinimumWindowHeight = 600'));
    expect(flutterWindow, contains('"sidravia/window"'));
    expect(flutterWindow, contains('call.method_name() == "minimize"'));
    expect(flutterWindow, contains('ShowWindow(window, SW_MINIMIZE)'));
    expect(flutterWindow, contains('call.method_name() == "beginDrag"'));
    expect(flutterWindow, contains('WM_NCLBUTTONDOWN'));
    expect(flutterWindow, contains('call.method_name() == "toggleMaximize"'));
    expect(flutterWindow, contains('call.method_name() == "close"'));
    expect(flutterWindow, contains('PostMessage(window, WM_CLOSE, 0, 0)'));
    expect(main, contains('Win32Window::Size size(1280, 720)'));
  });

  test('Windows runner owns the desktop presence surface', () {
    final main = File('windows/runner/main.cpp').readAsStringSync();
    final flutterWindow = File('windows/runner/flutter_window.cpp')
        .readAsStringSync();
    final header = File('windows/runner/flutter_window.h').readAsStringSync();
    final cmake = File('windows/runner/CMakeLists.txt').readAsStringSync();

    // Single-instance activation before Flutter bootstrap.
    expect(main, isNot(contains('CreateMutexW')));
    expect(flutterWindow, contains('call.method_name() == "initialize"'));
    expect(flutterWindow, contains('CreateMutexW'));
    expect(flutterWindow, contains('kSingleInstanceMutexName'));
    expect(flutterWindow, contains('kActivationMessageName'));
    expect(flutterWindow, contains('NamespaceScopedName'));
    expect(flutterWindow, contains('value >= \'A\' && value <= \'Z\''));
    expect(
      flutterWindow,
      contains('InitializeDesktopPresence(*namespace_value)'),
    );
    expect(flutterWindow, contains('activation_message_name_'));
    expect(flutterWindow, contains('single_instance_mutex_name_'));
    expect(flutterWindow, contains('ERROR_ALREADY_EXISTS'));
    expect(flutterWindow, contains('HWND_BROADCAST'));
    expect(flutterWindow, isNot(contains('FindWindowW')));
    expect(flutterWindow, contains('PostMessageW'));
    expect(flutterWindow, contains('CreateTrayIcon()'));

    // Hidden close versus explicit destroy.
    expect(flutterWindow, contains('WM_CLOSE'));
    expect(flutterWindow, contains('SW_HIDE'));
    expect(flutterWindow, contains('exit_armed_'));

    // Tray creation, recreation and deletion.
    expect(flutterWindow, contains('Shell_NotifyIconW'));
    expect(flutterWindow, contains('NIM_ADD'));
    expect(flutterWindow, contains('NIM_DELETE'));
    expect(flutterWindow, contains('NIF_INFO'));
    expect(flutterWindow, contains('TaskbarCreated'));
    expect(
      flutterWindow,
      contains('if (desktop_presence_initialized_) CreateTrayIcon();'),
    );
    expect(flutterWindow, contains('WM_LBUTTONUP'));
    expect(flutterWindow, contains('WM_RBUTTONUP'));
    expect(flutterWindow, contains('NIN_BALLOONUSERCLICK'));

    // Tray menu contract.
    expect(flutterWindow, contains('打开 Sidravia'));
    expect(flutterWindow, contains('退出并断开'));
    expect(flutterWindow, contains('TPM_RETURNCMD'));

    // Explicit exit flows through Dart before native destruction.
    expect(flutterWindow, contains('"sidravia/desktop"'));
    expect(flutterWindow, contains('"exitRequested"'));
    expect(flutterWindow, contains('call.method_name() == "destroy"'));
    expect(flutterWindow, contains('exit_request_pending_'));
    expect(flutterWindow, contains('SetTimer'));
    expect(flutterWindow, contains('WM_TIMER'));

    // Native channel is declared on the window.
    expect(header, contains('desktop_channel_'));
    expect(header, contains('CreateTrayIcon'));
    expect(header, contains('RestoreWindow'));
    expect(header, contains('desktop_presence_initialized_'));

    // The legacy frame channel and desktop presence share shell32 linkage.
    expect(cmake, contains('"shell32.lib"'));
  });
}
