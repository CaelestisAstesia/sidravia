import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('school webpage carrier channel shares runner ownership', () {
    final runner = File('windows/runner/flutter_window.cpp').readAsStringSync();
    final header = File('windows/runner/flutter_window.h').readAsStringSync();
    final cmake = File('windows/runner/CMakeLists.txt').readAsStringSync();
    expect(runner, contains('"sidravia/announcements"'));
    expect(
      runner,
      contains('school_announcement_window_->Open(GetHandle(), *arguments)'),
    );
    expect(header, contains('std::unique_ptr<SchoolAnnouncementWindow>'));
    final destruction = runner.split('void FlutterWindow::OnDestroy()').last;
    expect(
      destruction.indexOf('school_announcement_window_.reset()'),
      lessThan(destruction.indexOf('flutter_controller_ = nullptr')),
    );
    expect(cmake, contains('"school_announcement_window.cpp"'));
    expect(cmake, contains('FetchContent_MakeAvailable(sidravia_webview2)'));
    expect(cmake, contains('WebView2LoaderStatic.lib'));
  });

  test('Windows runner owns a DPI-aware custom resizable frame', () {
    final runner = File('windows/runner/win32_window.cpp').readAsStringSync();
    final main = File('windows/runner/main.cpp').readAsStringSync();
    final flutterWindow = File('windows/runner/flutter_window.cpp')
        .readAsStringSync();

    expect(runner, contains('WS_OVERLAPPEDWINDOW'));
    expect(runner, isNot(contains('WS_POPUP')));
    expect(runner, contains('WM_NCCALCSIZE'));
    expect(runner, contains('Entire outer rectangle becomes client space'));
    expect(runner, contains('monitor_info.rcWork'));
    expect(runner, contains('sidravia_frame::PhysicalMinimum'));
    expect(runner, contains('SetWindowSubclass(content, ChildFrameProc'));
    expect(runner, contains('return HTTRANSPARENT'));
    expect(runner, contains('VK_F4'));
    expect(runner, contains('SC_CLOSE'));
    expect(runner, contains('DWMWA_WINDOW_CORNER_PREFERENCE'));
    expect(runner, contains('DwmExtendFrameIntoClientArea'));
    expect(runner, isNot(contains('CreateRoundRectRgn')));
    expect(runner, contains('WM_NCHITTEST'));
    expect(runner, contains('WM_GETMINMAXINFO'));
    expect(flutterWindow, contains('configureFrame'));
    expect(flutterWindow, contains('setModalBlocked'));
    expect(flutterWindow, contains('HTMAXBUTTON'));
    expect(flutterWindow, contains('DwmDefWindowProc'));
    expect(
      flutterWindow.indexOf('// Frame messages must precede'),
      lessThan(flutterWindow.indexOf('// Give Flutter')),
    );
    for (final field in [
      'cornerHRESULT',
      'borderHRESULT',
      'cornerPreferenceRequested',
      'renderingPolicyHRESULT',
      'frameExtensionHRESULT',
      'frameExtensionPhysicalPixels',
    ]) {
      expect(flutterWindow, contains(field));
    }
    expect(flutterWindow, contains('WM_NCHITTEST'));
    expect(flutterWindow, contains('setDarkMode'));
    expect(runner, contains('kMinimumWindowWidth = 360'));
    expect(runner, contains('kMinimumWindowHeight = 640'));
    expect(flutterWindow, contains('"sidravia/window"'));
    expect(flutterWindow, contains('call.method_name() == "minimize"'));
    expect(flutterWindow, contains('ShowWindow(window, SW_MINIMIZE)'));
    expect(flutterWindow, contains('call.method_name() == "close"'));
    expect(flutterWindow, contains('PostMessage(window, WM_CLOSE, 0, 0)'));
    expect(main, contains('Win32Window::Size size(400, 690)'));
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
    final destruction = flutterWindow
        .split('void FlutterWindow::OnDestroy()')
        .last;
    expect(
      destruction.indexOf('tray_popover_window_.reset()'),
      lessThan(destruction.indexOf('flutter_controller_ = nullptr')),
    );

    expect(flutterWindow, contains('ShowTrayPopover'));
    expect(flutterWindow, contains('"publishTray"'));
    expect(flutterWindow, contains('"restoreMain"'));
    expect(header, contains('std::unique_ptr<TrayPopoverWindow>'));
    final tray = File('windows/runner/tray_popover_window.cpp')
        .readAsStringSync();
    final trayHeader = File('windows/runner/tray_popover_window.h')
        .readAsStringSync();
    expect(tray, contains('set_dart_entrypoint("trayMain")'));
    expect(tray, contains('HandleTopLevelWindowProc'));
    expect(tray, contains('resizeToContent'));
    expect(tray, contains('WM_DPICHANGED'));
    expect(tray, contains('GetDpiForMonitor'));
    expect(
      tray,
      contains('MonitorFromRect(&anchor, MONITOR_DEFAULTTONEAREST)'),
    );
    expect(tray, contains('HIWORD(wparam)'));
    expect(tray, contains('IsChild(window, next_focus)'));
    expect(tray, contains('case WM_ACTIVATE:'));
    expect(tray, contains('LOWORD(wparam) == WA_INACTIVE'));
    expect(tray, contains('right - left'));
    expect(tray, contains('bottom - top'));
    expect(tray, contains('message == WM_NCDESTROY'));
    expect(
      tray,
      contains('self->HandleMessage(window, message, wparam, lparam)'),
    );
    expect(tray, contains('SetWindowLongPtrW(window, GWLP_USERDATA, 0)'));
    expect(tray, contains('controller_->HandleTopLevelWindowProc'));
    expect(trayHeader, contains('bool handling_dpi_change_'));
    final singleClick = flutterWindow
        .split('case WM_LBUTTONUP:')
        .last
        .split('case WM_LBUTTONDBLCLK:')
        .first;
    expect(singleClick, contains('ShowTrayPopover();'));
    expect(singleClick, contains('suppress_tray_button_up_ = false;'));
    expect(
      singleClick.indexOf('return;'),
      lessThan(singleClick.indexOf('ShowTrayPopover();')),
    );
    expect(singleClick, isNot(contains('SetTimer')));
    expect(flutterWindow, isNot(contains('GetDoubleClickTime')));
    expect(flutterWindow, isNot(contains('tray_single_click_timer_')));
    expect(header, isNot(contains('tray_single_click_timer_')));
    final doubleClick = flutterWindow
        .split('case WM_LBUTTONDBLCLK:')
        .last
        .split('case NIN_BALLOONUSERCLICK:')
        .first;
    expect(doubleClick, contains('suppress_tray_button_up_ = true;'));
    expect(doubleClick, contains('tray_popover_window_->Hide();'));
    expect(doubleClick, contains('RestoreWindow();'));
    expect(doubleClick, isNot(contains('ShowTrayPopover')));
    expect(flutterWindow, contains('WM_LBUTTONDBLCLK'));
    expect(flutterWindow, contains('suppress_tray_button_up_'));
    expect(flutterWindow, isNot(contains('TrackPopupMenu')));

    // Explicit exit flows through Dart before native destruction.
    expect(flutterWindow, contains('"sidravia/desktop"'));
    expect(flutterWindow, contains('"exitRequested"'));
    expect(flutterWindow, contains('call.method_name() == "destroy"'));
    expect(flutterWindow, contains('exit_request_pending_'));
    expect(flutterWindow, contains('SetTimer'));
    expect(flutterWindow, contains('WM_TIMER'));

    final request = flutterWindow
        .split('void FlutterWindow::RequestExplicitExit()')
        .last
        .split('bool FlutterWindow::BeginExplicitExit()')
        .first;
    expect(request, isNot(contains('SetTimer')));
    expect(request, isNot(contains('CompleteExplicitExit')));
    expect(request, contains('RestoreWindow()'));
    final begin = flutterWindow
        .split('bool FlutterWindow::BeginExplicitExit()')
        .last
        .split('void FlutterWindow::CancelExplicitExit()')
        .first;
    expect(begin, contains('if (!exit_request_pending_) return false'));
    expect(begin, contains('::SetTimer'));
    expect(flutterWindow, contains('call.method_name() == "beginExit"'));
    expect(flutterWindow, contains('call.method_name() == "cancelExit"'));
    expect(
      flutterWindow,
      contains('call.method_name() == "configurePreferences"'),
    );
    expect(flutterWindow, contains('arguments->size() != 2'));
    expect(flutterWindow, contains('if (!close_value || !hidden_value)'));
    expect(flutterWindow, contains('if (close_to_tray_)'));
    expect(
      flutterWindow,
      contains(
        'if (!(desktop_presence_initialized_ && start_hidden_)) this->Show()',
      ),
    );

    // Native channel is declared on the window.
    expect(header, contains('desktop_channel_'));
    expect(header, contains('CreateTrayIcon'));
    expect(header, contains('RestoreWindow'));
    expect(header, contains('desktop_presence_initialized_'));

    // The legacy frame channel and desktop presence share shell32 linkage.
    expect(cmake, contains('"shell32.lib"'));
  });
}
