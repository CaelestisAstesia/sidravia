#include "tray_popover_window.h"

#include <dwmapi.h>

#include <algorithm>

namespace {
constexpr wchar_t kClassName[] = L"Sidravia.TrayPopover";
constexpr int kLogicalWidth = 210;
constexpr int kMinimumHeight = 120;
constexpr int kMaximumHeight = 700;
}

TrayPopoverWindow::TrayPopoverWindow(const flutter::DartProject& project,
                                     HWND owner, ActionHandler on_action)
    : project_(project), owner_(owner), on_action_(std::move(on_action)) {
  project_.set_dart_entrypoint("trayMain");
}

TrayPopoverWindow::~TrayPopoverWindow() { Destroy(); }

bool TrayPopoverWindow::Create() {
  if (window_ != nullptr) return true;
  WNDCLASSEXW klass{};
  klass.cbSize = sizeof(klass);
  klass.lpfnWndProc = WindowProc;
  klass.hInstance = GetModuleHandleW(nullptr);
  klass.hCursor = LoadCursor(nullptr, IDC_ARROW);
  klass.lpszClassName = kClassName;
  RegisterClassExW(&klass);
  window_ = CreateWindowExW(WS_EX_TOOLWINDOW | WS_EX_TOPMOST,
                            kClassName, L"Sidravia", WS_POPUP, 0, 0, 1, 1,
                            owner_, nullptr, klass.hInstance, this);
  if (window_ == nullptr) return false;
  const DWM_WINDOW_CORNER_PREFERENCE corner = DWMWCP_ROUND;
  DwmSetWindowAttribute(window_, DWMWA_WINDOW_CORNER_PREFERENCE, &corner,
                        sizeof(corner));
  const UINT dpi = GetDpiForWindow(window_);
  const int width = MulDiv(kLogicalWidth, static_cast<int>(dpi), 96);
  const int height = MulDiv(logical_height_, static_cast<int>(dpi), 96);
  controller_ = std::make_unique<flutter::FlutterViewController>(
      width, height, project_);
  if (!controller_->engine() || !controller_->view()) {
    Destroy();
    return false;
  }
  SetWindowLongPtrW(window_, GWLP_USERDATA,
                    reinterpret_cast<LONG_PTR>(this));
  SetParent(controller_->view()->GetNativeWindow(), window_);
  SetWindowPos(controller_->view()->GetNativeWindow(), nullptr, 0, 0, width,
               height, SWP_NOZORDER | SWP_NOACTIVATE);
  channel_ = std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
      controller_->engine()->messenger(), "sidravia/tray-view",
      &flutter::StandardMethodCodec::GetInstance());
  channel_->SetMethodCallHandler([this](const auto& call, auto result) {
    if (destroying_) {
      result->Error("tray_unavailable", "Tray popover is closing.");
    } else if (call.method_name() == "getPresentation") {
      if (presentation_) result->Success(flutter::EncodableValue(*presentation_));
      else result->Error("presentation_unavailable", "No tray presentation.");
    } else if (call.method_name() == "resizeToContent") {
      const auto* height = std::get_if<int32_t>(call.arguments());
      if (!height || *height < kMinimumHeight || *height > kMaximumHeight) {
        result->Error("invalid_size", "Tray size is outside supported bounds.");
      } else {
        Resize(*height);
        result->Success();
      }
    } else if (call.method_name() == "action") {
      const auto* action = call.arguments()
          ? std::get_if<flutter::EncodableMap>(call.arguments()) : nullptr;
      if (!action || !on_action_) {
        result->Error("invalid_action", "Tray action is unavailable.");
      } else {
        on_action_(*action);
        result->Success();
      }
    } else if (call.method_name() == "dismiss") {
      Hide();
      result->Success();
    } else {
      result->NotImplemented();
    }
  });
  controller_->engine()->SetNextFrameCallback([this]() {
    if (destroying_ || window_ == nullptr) return;
    frame_ready_ = true;
    if (!requested_visible_) return;
    Position(anchor_);
    ShowWindow(window_, SW_SHOWNOACTIVATE);
    SetForegroundWindow(window_);
  });
  controller_->ForceRedraw();
  return true;
}

bool TrayPopoverWindow::Open(const RECT& anchor,
                             const flutter::EncodableMap& presentation) {
  anchor_ = anchor;
  presentation_ = presentation;
  requested_visible_ = true;
  if (!Create()) return false;
  Position(anchor_);
  if (channel_) {
    channel_->InvokeMethod("presentationChanged",
                           std::make_unique<flutter::EncodableValue>(presentation));
  }
  if (frame_ready_) {
    ShowWindow(window_, SW_SHOWNOACTIVATE);
    SetForegroundWindow(window_);
  }
  return true;
}

void TrayPopoverWindow::Publish(const flutter::EncodableMap& presentation) {
  presentation_ = presentation;
  if (window_ != nullptr && channel_) {
    channel_->InvokeMethod("presentationChanged",
                           std::make_unique<flutter::EncodableValue>(presentation));
  }
}

void TrayPopoverWindow::Resize(int logical_height) {
  if (logical_height_ == logical_height) return;
  logical_height_ = logical_height;
  if (window_ == nullptr) return;
  Position(anchor_);
}

void TrayPopoverWindow::Position(const RECT& anchor) {
  if (window_ == nullptr) return;
  const UINT dpi = GetDpiForWindow(owner_);
  const int width = MulDiv(kLogicalWidth, static_cast<int>(dpi), 96);
  const int height = MulDiv(logical_height_, static_cast<int>(dpi), 96);
  RECT work{};
  MONITORINFO monitor{sizeof(monitor)};
  GetMonitorInfoW(MonitorFromRect(&anchor, MONITOR_DEFAULTTONEAREST), &monitor);
  work = monitor.rcWork;
  int x = anchor.right - width;
  int y = anchor.top - height - 6;
  if (y < work.top) y = anchor.bottom + 6;
  const int left = static_cast<int>(work.left);
  const int top = static_cast<int>(work.top);
  const int right = static_cast<int>(work.right);
  const int bottom = static_cast<int>(work.bottom);
  x = std::clamp(x, left, std::max(left, right - width));
  y = std::clamp(y, top, std::max(top, bottom - height));
  SetWindowPos(window_, HWND_TOPMOST, x, y, width, height, SWP_NOACTIVATE);
}

void TrayPopoverWindow::Hide() {
  requested_visible_ = false;
  if (window_ != nullptr) ShowWindow(window_, SW_HIDE);
}

void TrayPopoverWindow::Destroy() {
  if (destroying_) return;
  destroying_ = true;
  if (channel_) channel_->SetMethodCallHandler(nullptr);
  channel_.reset();
  controller_.reset();
  if (window_ != nullptr && IsWindow(window_)) DestroyWindow(window_);
  window_ = nullptr;
  presentation_.reset();
  on_action_ = nullptr;
}

LRESULT CALLBACK TrayPopoverWindow::WindowProc(HWND window, UINT message,
                                               WPARAM wparam, LPARAM lparam) {
  auto* self = reinterpret_cast<TrayPopoverWindow*>(
      GetWindowLongPtrW(window, GWLP_USERDATA));
  if (message == WM_NCCREATE) {
    const auto* create = reinterpret_cast<CREATESTRUCTW*>(lparam);
    self = static_cast<TrayPopoverWindow*>(create->lpCreateParams);
    self->window_ = window;
    SetWindowLongPtrW(window, GWLP_USERDATA,
                      reinterpret_cast<LONG_PTR>(self));
  }
  return self ? self->HandleMessage(message, wparam, lparam)
              : DefWindowProcW(window, message, wparam, lparam);
}

LRESULT TrayPopoverWindow::HandleMessage(UINT message, WPARAM wparam,
                                         LPARAM lparam) {
  std::optional<LRESULT> flutter_result;
  if (controller_) {
    flutter_result = controller_->HandleTopLevelWindowProc(
        window_, message, wparam, lparam);
  }
  switch (message) {
    case WM_SIZE:
    case WM_DPICHANGED: {
      if (controller_) {
        RECT client{};
        GetClientRect(window_, &client);
        SetWindowPos(controller_->view()->GetNativeWindow(), nullptr, 0, 0,
                     client.right, client.bottom,
                     SWP_NOZORDER | SWP_NOACTIVATE);
      }
      if (message == WM_DPICHANGED) Position(anchor_);
      if (flutter_result) return *flutter_result;
      return 0;
    }
    case WM_KILLFOCUS:
      Hide();
      return 0;
    case WM_KEYDOWN:
      if (wparam == VK_ESCAPE) { Hide(); return 0; }
      break;
    case WM_CLOSE:
      Hide();
      return 0;
    case WM_DESTROY:
      window_ = nullptr;
      return 0;
  }
  if (flutter_result) return *flutter_result;
  return DefWindowProcW(window_, message, wparam, lparam);
}
