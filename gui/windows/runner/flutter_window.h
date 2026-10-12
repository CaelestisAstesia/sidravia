#ifndef RUNNER_FLUTTER_WINDOW_H_
#define RUNNER_FLUTTER_WINDOW_H_

#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <flutter/method_channel.h>
#include <flutter/standard_method_codec.h>

#include <memory>
#include <optional>
#include <string>

#include "win32_window.h"
#include "school_announcement_window.h"
#include "system_notification.h"
#include "tray_popover_window.h"

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

  std::unique_ptr<SchoolAnnouncementWindow> school_announcement_window_;
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>>
      announcement_channel_;

  std::unique_ptr<SystemNotification> system_notification_;
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>>
      system_notifications_channel_;

  // Application-owned Windows frame commands.
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>>
      window_channel_;

  // Desktop presence commands and native-to-Dart exit requests.
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>>
      desktop_channel_;
  std::unique_ptr<TrayPopoverWindow> tray_popover_window_;
  flutter::EncodableMap tray_presentation_;

  flutter::EncodableMap WindowState();
  void PublishWindowState();
  bool maximize_hovered_ = false;
  bool maximize_pressed_ = false;
  bool exit_armed_ = false;
  bool close_to_tray_ = true;
  bool start_hidden_ = false;
  bool desktop_presence_initialized_ = false;
  bool visibility_message_queued_ = false;
  bool destroying_ = false;
  bool suppress_tray_button_up_ = false;
  std::optional<bool> last_published_visibility_;
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
  void ShowTrayPopover();
  void HandleTrayAction(const flutter::EncodableMap& action);
  void HandleTrayMessage(LPARAM lparam);
  void ScheduleVisibilityUpdate();
  void PublishVisibilityUpdate();
  void RequestExplicitExit();
  bool BeginExplicitExit();
  void CancelExplicitExit();
  static std::wstring Utf8ToUtf16(const std::string& utf8);
};

#endif  // RUNNER_FLUTTER_WINDOW_H_
