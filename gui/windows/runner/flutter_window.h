#ifndef RUNNER_FLUTTER_WINDOW_H_
#define RUNNER_FLUTTER_WINDOW_H_

#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <flutter/method_channel.h>
#include <flutter/standard_method_codec.h>

#include <memory>
#include <string>

#include "win32_window.h"

// A window that hosts a Flutter view and owns the desktop presence surface
// (single-instance activation, tray restore/exit and narrow notifications).
class FlutterWindow : public Win32Window {
 public:
  // Creates a new FlutterWindow hosting a Flutter view running |project|.
  explicit FlutterWindow(const flutter::DartProject& project);
  virtual ~FlutterWindow();

 protected:
  // Win32Window:
  bool OnCreate() override;
  void OnDestroy() override;
  LRESULT MessageHandler(HWND window, UINT const message, WPARAM const wparam,
                         LPARAM const lparam) noexcept override;

 private:
  // The project to run.
  flutter::DartProject project_;

  // The Flutter instance hosted by this window.
  std::unique_ptr<flutter::FlutterViewController> flutter_controller_;

  // Application-owned Windows frame commands.
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>>
      window_channel_;

  // Desktop presence commands and native-to-Dart exit requests.
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>>
      desktop_channel_;

  bool exit_armed_ = false;
  bool desktop_presence_initialized_ = false;
  bool exit_request_pending_ = false;
  HANDLE single_instance_mutex_ = nullptr;
  std::wstring single_instance_mutex_name_;
  std::wstring activation_message_name_;
  UINT_PTR exit_fallback_timer_ = 0;
  UINT activation_message_ = 0;
  UINT taskbar_created_message_ = 0;

  bool CreateTrayIcon();
  std::string InitializeDesktopPresence(const std::string& namespace_name);
  void ReleaseDesktopPresence();
  void CompleteExplicitExit();
  void RemoveTrayIcon();
  void RestoreWindow();
  void NotifyTray(const std::wstring& title, const std::wstring& body);
  void ShowTrayMenu();
  void HandleTrayMessage(LPARAM lparam);
  void RequestExplicitExit();
  static std::wstring Utf8ToUtf16(const std::string& utf8);
};

#endif  // RUNNER_FLUTTER_WINDOW_H_
