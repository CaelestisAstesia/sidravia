#ifndef RUNNER_TRAY_POPOVER_WINDOW_H_
#define RUNNER_TRAY_POPOVER_WINDOW_H_

#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <flutter/method_channel.h>
#include <flutter/standard_method_codec.h>
#include <windows.h>

#include <functional>
#include <memory>
#include <optional>

class TrayPopoverWindow {
 public:
  using ActionHandler = std::function<void(const flutter::EncodableMap&)>;
  TrayPopoverWindow(const flutter::DartProject& project, HWND owner,
                    ActionHandler on_action);
  ~TrayPopoverWindow();
  bool Open(const RECT& anchor, const flutter::EncodableMap& presentation);
  void Publish(const flutter::EncodableMap& presentation);
  void Hide();
  void Destroy();

 private:
  static LRESULT CALLBACK WindowProc(HWND, UINT, WPARAM, LPARAM);
  LRESULT HandleMessage(UINT, WPARAM, LPARAM);
  bool Create();
  void Resize(int logical_height);
  void Position(const RECT& anchor);

  flutter::DartProject project_;
  HWND owner_ = nullptr;
  HWND window_ = nullptr;
  std::unique_ptr<flutter::FlutterViewController> controller_;
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>> channel_;
  ActionHandler on_action_;
  std::optional<flutter::EncodableMap> presentation_;
  RECT anchor_{};
  int logical_height_ = 172;
  bool destroying_ = false;
  bool frame_ready_ = false;
  bool requested_visible_ = false;
};

#endif
