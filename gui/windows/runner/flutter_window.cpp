#include "flutter_window.h"

#include <flutter/encodable_value.h>
#include <shellapi.h>
#include <dwmapi.h>
#include <windowsx.h>
#include "frame_geometry.h"

#include <cwchar>
#include <optional>
#include <string>

#include "flutter/generated_plugin_registrant.h"
#include "resource.h"

namespace {
constexpr UINT kTrayIconId = 1;
constexpr UINT kTrayCallbackMessage = WM_APP + 1;
constexpr UINT kVisibilityChangedMessage = WM_APP + 2;
constexpr UINT_PTR kExitFallbackTimerId = 1;
constexpr UINT kExitFallbackMilliseconds = 6000;
constexpr const wchar_t kSingleInstanceMutexName[] =
    L"Local\\Sidravia.Gui.DesktopPresence";
constexpr const wchar_t kActivationMessageName[] = L"Sidravia.Gui.Activate";

std::wstring NamespaceScopedName(const wchar_t* production_name,
                                 const std::string& namespace_name) {
  if (namespace_name.empty()) return production_name;
  std::wstring folded;
  folded.reserve(namespace_name.size());
  for (const unsigned char value : namespace_name) {
    folded.push_back(static_cast<wchar_t>(
        value >= 'A' && value <= 'Z' ? value + ('a' - 'A') : value));
  }
  return std::wstring(production_name) + L"-ns-" + folded;
}
}  // namespace

FlutterWindow::FlutterWindow(const flutter::DartProject& project)
    : project_(project) {}

FlutterWindow::~FlutterWindow() {}

bool FlutterWindow::OnCreate() {
  if (!Win32Window::OnCreate()) {
    return false;
  }

  RECT frame = GetClientArea();

  // The size here must match the window dimensions to avoid unnecessary surface
  // creation / destruction in the startup path.
  flutter_controller_ = std::make_unique<flutter::FlutterViewController>(
      frame.right - frame.left, frame.bottom - frame.top, project_);
  // Ensure that basic setup of the controller was successful.
  if (!flutter_controller_->engine() || !flutter_controller_->view()) {
    return false;
  }
  RegisterPlugins(flutter_controller_->engine());
  school_announcement_window_ = std::make_unique<SchoolAnnouncementWindow>();
  announcement_channel_ =
      std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
          flutter_controller_->engine()->messenger(), "sidravia/announcements",
          &flutter::StandardMethodCodec::GetInstance());
  announcement_channel_->SetMethodCallHandler(
      [this](const auto& call, auto result) {
        if (call.method_name() == "open") {
          const auto* arguments =
              std::get_if<flutter::EncodableMap>(call.arguments());
          if (!arguments || !school_announcement_window_ ||
              !school_announcement_window_->Open(GetHandle(), *arguments)) {
            result->Error("announcement_unavailable", "Announcement unavailable.");
            return;
          }
          result->Success(flutter::EncodableValue("opened"));
          return;
        }
        if (call.method_name() == "close") {
          if (school_announcement_window_ && !school_announcement_window_->Close()) {
            result->Error("announcement_close_failed", "Announcement close failed.");
            return;
          }
          result->Success();
          return;
        }
        result->NotImplemented();
      });
  system_notification_ =
      std::make_unique<SystemNotification>(GetHandle());
  system_notifications_channel_ =
      std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
          flutter_controller_->engine()->messenger(),
          "sidravia/system-notifications",
          &flutter::StandardMethodCodec::GetInstance());
  system_notifications_channel_->SetMethodCallHandler(
      [this](const auto& call, auto result) {
        if (call.method_name() == "show") {
          const auto* arguments = call.arguments()
              ? std::get_if<flutter::EncodableMap>(call.arguments()) : nullptr;
          const auto submission = arguments && system_notification_
              ? system_notification_->Show(*arguments)
              : SystemNotification::Result{"failed", std::nullopt};
          if (submission.status == "submitted") {
            result->Success(flutter::EncodableValue(submission.status));
          } else {
            flutter::EncodableMap details;
            if (submission.error_code) {
              details[flutter::EncodableValue("error_code")] =
                  flutter::EncodableValue(*submission.error_code);
            }
            result->Error("notification_failed", "Notification operation failed.",
                          flutter::EncodableValue(details));
          }
          return;
        }
        if (call.method_name() == "dismiss") {
          const auto* args = call.arguments() ? std::get_if<flutter::EncodableMap>(call.arguments()) : nullptr;
          const std::string* category = nullptr;
          if (args && args->size() == 1) {
            auto it = args->find(flutter::EncodableValue("category"));
            if (it != args->end()) category = std::get_if<std::string>(&it->second);
          }
          if (category && system_notification_ && system_notification_->Dismiss(*category)) result->Success();
          else result->Error("notification_failed", "Notification operation failed.");
          return;
        }
        if (call.method_name() == "dispose") {
          if (!system_notification_ || system_notification_->Dispose()) result->Success();
          else result->Error("notification_failed", "Notification operation failed.");
          return;
        }
        result->NotImplemented();
      });
  window_channel_ =
      std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
          flutter_controller_->engine()->messenger(), "sidravia/window",
          &flutter::StandardMethodCodec::GetInstance());
  window_channel_->SetMethodCallHandler(
      [this](const auto& call, auto result) {
        HWND window = GetHandle();
        if (window == nullptr) {
          result->Error("window_unavailable", "Window is unavailable.");
          return;
        }
        if (call.method_name() == "setDarkMode") {
          const auto* dark = std::get_if<bool>(call.arguments());
          if (!dark) { result->Error("invalid_theme", "Expected bool"); return; }
          // Preserve the Dart-owned appearance through DWM color changes.
          SetFrameDarkMode(*dark);
          result->Success(); return;
        }
        if (call.method_name() == "setModalBlocked") {
          const auto* blocked = std::get_if<bool>(call.arguments());
          if (!blocked) { result->Error("invalid_modal", "Expected bool"); return; }
          SetFrameModalBlocked(*blocked);
          result->Success(); return;
        }
        if (call.method_name() == "configureFrame") {
          const auto* enabled = std::get_if<bool>(call.arguments());
          if (!enabled) { result->Error("invalid_frame", "Expected bool"); return; }
          EnableCustomFrame(*enabled);
          result->Success(flutter::EncodableValue(WindowState()));
          return;
        }
        if (call.method_name() == "getState" ||
            call.method_name() == "diagnostics") {
          result->Success(flutter::EncodableValue(WindowState()));
          return;
        }
        if (call.method_name() == "minimize") {
          ShowWindow(window, SW_MINIMIZE);
          result->Success();
          return;
        }
        if (call.method_name() == "beginDrag") {
          ReleaseCapture();
          SendMessage(window, WM_NCLBUTTONDOWN, HTCAPTION, 0);
          result->Success();
          return;
        }
        if (call.method_name() == "toggleMaximize") {
          ShowWindow(window, IsZoomed(window) ? SW_RESTORE : SW_MAXIMIZE);
          result->Success();
          return;
        }
        if (call.method_name() == "close") {
          result->Success();
          PostMessage(window, WM_CLOSE, 0, 0);
          return;
        }
        result->NotImplemented();
      });

  desktop_channel_ =
      std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
          flutter_controller_->engine()->messenger(), "sidravia/desktop",
          &flutter::StandardMethodCodec::GetInstance());
  desktop_channel_->SetMethodCallHandler(
      [this](const auto& call, auto result) {
        if (call.method_name() == "initialize") {
          const auto* namespace_value =
              std::get_if<std::string>(call.arguments());
          if (namespace_value == nullptr) {
            result->Error("invalid_namespace", "Invalid runtime namespace.");
            return;
          }
          const std::string disposition =
              InitializeDesktopPresence(*namespace_value);
          if (disposition == "failed") {
            result->Error("desktop_presence_unavailable",
                          "Desktop presence is unavailable.");
          } else {
            result->Success(flutter::EncodableValue(disposition));
          }
          return;
        }
        if (call.method_name() == "getVisibility") {
          const auto* arguments = call.arguments();
          HWND window = GetHandle();
          if ((arguments != nullptr &&
               std::get_if<std::monostate>(arguments) == nullptr) ||
              !desktop_presence_initialized_ || window == nullptr ||
              !::IsWindow(window)) {
            result->Error("desktop_presence_unavailable",
                          "Main window visibility is unavailable.");
            return;
          }
          result->Success(flutter::EncodableValue(
              ::IsWindowVisible(window) != FALSE &&
              ::IsIconic(window) == FALSE));
          return;
        }
        if (call.method_name() == "publishTray") {
          const auto* presentation = call.arguments()
              ? std::get_if<flutter::EncodableMap>(call.arguments()) : nullptr;
          if (!presentation || !desktop_presence_initialized_) {
            result->Error("invalid_tray_presentation", "Tray presentation unavailable.");
            return;
          }
          tray_presentation_ = *presentation;
          if (tray_popover_window_) tray_popover_window_->Publish(tray_presentation_);
          result->Success();
          return;
        }
        if (call.method_name() == "restoreMain") {
          RestoreWindow();
          result->Success();
          return;
        }
        if (call.method_name() == "configurePreferences") {
          const auto* arguments = std::get_if<flutter::EncodableMap>(call.arguments());
          if (!desktop_presence_initialized_ || arguments == nullptr || arguments->size() != 2) {
            result->Error("invalid_preferences", "Desktop preferences unavailable."); return;
          }
          const auto close = arguments->find(flutter::EncodableValue("closeToTray"));
          const auto hidden = arguments->find(flutter::EncodableValue("startHidden"));
          if (close == arguments->end() || hidden == arguments->end()) {
            result->Error("invalid_preferences", "Expected desktop preference keys."); return;
          }
          const auto* close_value = std::get_if<bool>(&close->second);
          const auto* hidden_value = std::get_if<bool>(&hidden->second);
          if (!close_value || !hidden_value) {
            result->Error("invalid_preferences", "Expected bool preferences."); return;
          }
          close_to_tray_ = *close_value;
          start_hidden_ = *hidden_value;
          result->Success(); return;
        }
        if (call.method_name() == "beginExit") {
          if (!BeginExplicitExit()) {
            result->Error("exit_unavailable", "Exit request unavailable."); return;
          }
          result->Success(); return;
        }
        if (call.method_name() == "cancelExit") {
          CancelExplicitExit();
          result->Success(); return;
        }
        if (call.method_name() == "destroy") {
          if (!exit_request_pending_ || exit_fallback_timer_ == 0) {
            result->Error("exit_unconfirmed", "Exit has not been confirmed."); return;
          }
          result->Success();
          CompleteExplicitExit();
          return;
        }
        result->NotImplemented();
      });

  taskbar_created_message_ = ::RegisterWindowMessageW(L"TaskbarCreated");
  SetChildContent(flutter_controller_->view()->GetNativeWindow());

  flutter_controller_->engine()->SetNextFrameCallback([&]() {
    if (!(desktop_presence_initialized_ && start_hidden_)) this->Show();
  });

  // Flutter can complete the first frame before the "show window" callback is
  // registered. The following call ensures a frame is pending to ensure the
  // window is shown. It is a no-op if the first frame hasn't completed yet.
  flutter_controller_->ForceRedraw();

  return true;
}

void FlutterWindow::OnDestroy() {
  destroying_ = true;
  visibility_message_queued_ = false;
  if (exit_fallback_timer_ != 0 && GetHandle() != nullptr) {
    ::KillTimer(GetHandle(), exit_fallback_timer_);
    exit_fallback_timer_ = 0;
  }
  if (tray_popover_window_) tray_popover_window_->Destroy();
  tray_popover_window_.reset();
  RemoveTrayIcon();
  ReleaseDesktopPresence();
  system_notification_.reset();
  system_notifications_channel_.reset();
  announcement_channel_.reset();
  school_announcement_window_.reset();
  window_channel_.reset();
  desktop_channel_.reset();
  if (flutter_controller_) {
    flutter_controller_ = nullptr;
  }

  Win32Window::OnDestroy();
}

bool FlutterWindow::CreateTrayIcon() {
  HWND hwnd = GetHandle();
  if (hwnd == nullptr) return false;
  NOTIFYICONDATAW nid{};
  nid.cbSize = sizeof(NOTIFYICONDATAW);
  nid.hWnd = hwnd;
  nid.uID = kTrayIconId;
  nid.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP;
  nid.uCallbackMessage = kTrayCallbackMessage;
  nid.hIcon = ::LoadIcon(::GetModuleHandleW(nullptr), MAKEINTRESOURCE(IDI_APP_ICON));
  wcscpy_s(nid.szTip, L"Sidravia");
  if (::Shell_NotifyIconW(NIM_ADD, &nid) == FALSE) return false;
  nid.uVersion = NOTIFYICON_VERSION_4;
  ::Shell_NotifyIconW(NIM_SETVERSION, &nid);
  return true;
}

std::string FlutterWindow::InitializeDesktopPresence(
    const std::string& namespace_name) {
  if (desktop_presence_initialized_) return "primary";
  single_instance_mutex_name_ =
      NamespaceScopedName(kSingleInstanceMutexName, namespace_name);
  activation_message_name_ =
      NamespaceScopedName(kActivationMessageName, namespace_name);
  activation_message_ =
      ::RegisterWindowMessageW(activation_message_name_.c_str());
  if (activation_message_ == 0) return "failed";
  single_instance_mutex_ =
      ::CreateMutexW(nullptr, TRUE, single_instance_mutex_name_.c_str());
  if (single_instance_mutex_ == nullptr) return "failed";
  if (::GetLastError() == ERROR_ALREADY_EXISTS) {
    ::CloseHandle(single_instance_mutex_);
    single_instance_mutex_ = nullptr;
    ::PostMessageW(HWND_BROADCAST, activation_message_, 0, 0);
    return "activatedExisting";
  }
  if (!CreateTrayIcon()) {
    ReleaseDesktopPresence();
    return "failed";
  }
  desktop_presence_initialized_ = true;
  return "primary";
}

void FlutterWindow::ReleaseDesktopPresence() {
  desktop_presence_initialized_ = false;
  if (single_instance_mutex_ != nullptr) {
    ::CloseHandle(single_instance_mutex_);
    single_instance_mutex_ = nullptr;
  }
}

void FlutterWindow::CompleteExplicitExit() {
  if (exit_armed_) return;
  exit_armed_ = true;
  if (exit_fallback_timer_ != 0 && GetHandle() != nullptr) {
    ::KillTimer(GetHandle(), exit_fallback_timer_);
    exit_fallback_timer_ = 0;
  }
  RemoveTrayIcon();
  ReleaseDesktopPresence();
  ::PostMessageW(GetHandle(), WM_CLOSE, 0, 0);
}

void FlutterWindow::RemoveTrayIcon() {
  HWND hwnd = GetHandle();
  if (hwnd == nullptr) return;
  NOTIFYICONDATAW nid{};
  nid.cbSize = sizeof(NOTIFYICONDATAW);
  nid.hWnd = hwnd;
  nid.uID = kTrayIconId;
  ::Shell_NotifyIconW(NIM_DELETE, &nid);
}

void FlutterWindow::RestoreWindow() {
  HWND hwnd = GetHandle();
  if (hwnd == nullptr) return;
  ::ShowWindow(hwnd, SW_RESTORE);
  if (!::SetForegroundWindow(hwnd)) {
    ::FlashWindow(hwnd, TRUE);
  }
  ::BringWindowToTop(hwnd);
}

void FlutterWindow::ScheduleVisibilityUpdate() {
  HWND hwnd = GetHandle();
  if (destroying_ || visibility_message_queued_ || hwnd == nullptr ||
      !desktop_channel_ || !flutter_controller_) {
    return;
  }
  visibility_message_queued_ = true;
  if (!::PostMessageW(hwnd, kVisibilityChangedMessage, 0, 0)) {
    visibility_message_queued_ = false;
  }
}

void FlutterWindow::PublishVisibilityUpdate() {
  HWND hwnd = GetHandle();
  if (destroying_ || hwnd == nullptr || !desktop_channel_ ||
      !flutter_controller_) {
    return;
  }
  const bool visible = ::IsWindowVisible(hwnd) != FALSE &&
                       ::IsIconic(hwnd) == FALSE;
  if (last_published_visibility_ == visible) return;
  last_published_visibility_ = visible;
  desktop_channel_->InvokeMethod(
      "visibilityChanged",
      std::make_unique<flutter::EncodableValue>(visible));
}

void FlutterWindow::ShowTrayPopover() {
  HWND hwnd = GetHandle();
  if (hwnd == nullptr || tray_presentation_.empty()) return;
  NOTIFYICONIDENTIFIER identity{};
  identity.cbSize = sizeof(identity);
  identity.hWnd = hwnd;
  identity.uID = kTrayIconId;
  RECT anchor{};
  if (FAILED(::Shell_NotifyIconGetRect(&identity, &anchor))) {
    POINT cursor{};
    ::GetCursorPos(&cursor);
    anchor = {cursor.x, cursor.y, cursor.x + 1, cursor.y + 1};
  }
  if (!tray_popover_window_) {
    tray_popover_window_ = std::make_unique<TrayPopoverWindow>(
        project_, hwnd, [this](const auto& action) { HandleTrayAction(action); });
  }
  if (!tray_popover_window_->Open(anchor, tray_presentation_)) {
    if (desktop_channel_) desktop_channel_->InvokeMethod("trayUnavailable", nullptr);
    tray_popover_window_->Destroy();
    tray_popover_window_.reset();
  }
}

void FlutterWindow::HandleTrayAction(const flutter::EncodableMap& action) {
  if (tray_popover_window_) tray_popover_window_->Hide();
  const auto kind = action.find(flutter::EncodableValue("kind"));
  const auto* value = kind == action.end()
      ? nullptr : std::get_if<std::string>(&kind->second);
  if (value == nullptr) return;
  if (*value == "restoreMain") {
    RestoreWindow();
  } else if (*value == "exit") {
    RequestExplicitExit();
  } else if (desktop_channel_ && flutter_controller_) {
    desktop_channel_->InvokeMethod(
        "trayAction", std::make_unique<flutter::EncodableValue>(action));
  }
}

void FlutterWindow::HandleTrayMessage(LPARAM lparam) {
  const UINT event = LOWORD(static_cast<DWORD>(lparam));
  switch (event) {
    case WM_LBUTTONUP:
      if (suppress_tray_button_up_) {
        suppress_tray_button_up_ = false;
        return;
      }
      ShowTrayPopover();
      break;
    case WM_LBUTTONDBLCLK:
      suppress_tray_button_up_ = true;
      if (tray_popover_window_) tray_popover_window_->Hide();
      RestoreWindow();
      break;
    case NIN_BALLOONUSERCLICK:
      RestoreWindow();
      break;
    case WM_RBUTTONUP:
    case WM_CONTEXTMENU:
    case NIN_KEYSELECT:
      ShowTrayPopover();
      break;
    default:
      break;
  }
}

/*
  The classic context menu is intentionally replaced by the shared Flutter
  popover; keep lifecycle and notification handling in this owner.
*/

void FlutterWindow::RequestExplicitExit() {
  if (exit_request_pending_) return;
  exit_request_pending_ = true;
  RestoreWindow();
  if (desktop_channel_ && flutter_controller_) {
    desktop_channel_->InvokeMethod("exitRequested", nullptr);
    return;
  }
  CancelExplicitExit();
}

bool FlutterWindow::BeginExplicitExit() {
  if (!exit_request_pending_) return false;
  if (exit_fallback_timer_ != 0) return true;
  exit_fallback_timer_ = ::SetTimer(GetHandle(), kExitFallbackTimerId,
                                   kExitFallbackMilliseconds, nullptr);
  return exit_fallback_timer_ != 0;
}

void FlutterWindow::CancelExplicitExit() {
  if (exit_fallback_timer_ != 0) return;
  exit_request_pending_ = false;
}

std::wstring FlutterWindow::Utf8ToUtf16(const std::string& utf8) {
  if (utf8.empty()) return std::wstring();
  const int size =
      ::MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, utf8.data(),
                            static_cast<int>(utf8.size()), nullptr, 0);
  if (size <= 0) return std::wstring();
  std::wstring value(static_cast<size_t>(size), L'\0');
  ::MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, utf8.data(),
                        static_cast<int>(utf8.size()), value.data(), size);
  return value;
}

LRESULT
FlutterWindow::MessageHandler(HWND hwnd, UINT const message,
                              WPARAM const wparam,
                              LPARAM const lparam) noexcept {
  if (message == SystemNotification::kCallbackMessage) {
    if (system_notification_) {
      const auto callback = system_notification_->TakeCallback(wparam, lparam);
      if (callback && system_notifications_channel_) {
        try {
          flutter::EncodableMap event;
          event[flutter::EncodableValue("token")] = flutter::EncodableValue(callback->token);
          event[flutter::EncodableValue("category")] = flutter::EncodableValue(callback->category);
          if (callback->method == "failed" && callback->error_code)
            event[flutter::EncodableValue("error_code")] = flutter::EncodableValue(*callback->error_code);
          flutter::EncodableValue payload(event);
          system_notifications_channel_->InvokeMethod(
              callback->method,
              std::make_unique<flutter::EncodableValue>(payload));
        } catch (...) { /* Identity stays consumed when delivery fails. */ }
      }
    }
    return 0;
  }
  if (message == kVisibilityChangedMessage) {
    visibility_message_queued_ = false;
    PublishVisibilityUpdate();
    return 0;
  }
  if (message == WM_SHOWWINDOW || message == WM_SIZE) {
    ScheduleVisibilityUpdate();
  }

  // Frame messages must precede Flutter's top-level handling.
  if (custom_frame_enabled()) {
    if (message == WM_NCHITTEST || message == WM_NCCALCSIZE ||
        message == WM_GETMINMAXINFO) {
      return Win32Window::MessageHandler(hwnd, message, wparam, lparam);
    }
    if (message == WM_NCMOUSEMOVE) {
      const bool hovered = wparam == HTMAXBUTTON;
      if (maximize_hovered_ != hovered) {
        maximize_hovered_ = hovered;
        PublishWindowState();
      }
      TRACKMOUSEEVENT track{sizeof(TRACKMOUSEEVENT), TME_LEAVE | TME_NONCLIENT,
                             hwnd, 0};
      TrackMouseEvent(&track);
      LRESULT dwm_result = 0;
      // Preserve the Windows 11 shell's HTMAXBUTTON hover/snap protocol.
      DwmDefWindowProc(hwnd, message, wparam, lparam, &dwm_result);
    }
    if (message == WM_NCMOUSELEAVE) {
      maximize_hovered_ = false; maximize_pressed_ = false;
      PublishWindowState();
    }
    if (message == WM_NCLBUTTONDOWN && wparam == HTMAXBUTTON) {
      maximize_pressed_ = true; PublishWindowState();
      return 0;
    }
    if (message == WM_NCLBUTTONUP && wparam == HTMAXBUTTON) {
      const bool activate = maximize_pressed_;
      maximize_pressed_ = false; PublishWindowState();
      if (activate) ShowWindow(hwnd, IsZoomed(hwnd) ? SW_RESTORE : SW_MAXIMIZE);
      return 0;
    }
    if (message == WM_SIZE || message == WM_DPICHANGED || message == WM_MOVE ||
        message == WM_ACTIVATE) {
      const auto result = Win32Window::MessageHandler(hwnd, message, wparam, lparam);
      PublishWindowState();
      // Flutter also needs the DPI/size event; its result must not skip native state.
      if (flutter_controller_) {
        flutter_controller_->HandleTopLevelWindowProc(hwnd, message, wparam, lparam);
      }
      return result;
    }
  }
  // Give Flutter, including plugins, an opportunity to handle window messages.
  if (flutter_controller_) {
    std::optional<LRESULT> result =
        flutter_controller_->HandleTopLevelWindowProc(hwnd, message, wparam,
                                                      lparam);
    if (result) {
      return *result;
    }
  }

  if (activation_message_ != 0 && message == activation_message_) {
    if (desktop_presence_initialized_) RestoreWindow();
    return 0;
  }
  if (taskbar_created_message_ != 0 && message == taskbar_created_message_) {
    if (desktop_presence_initialized_) CreateTrayIcon();
    return 0;
  }

  switch (message) {
    case WM_CLOSE:
      if (desktop_presence_initialized_ && !exit_armed_) {
        if (close_to_tray_) {
          ShowWindow(hwnd, SW_HIDE);
        } else {
          RequestExplicitExit();
        }
        return 0;
      }
      break;
    case WM_TIMER:
      if (wparam == kExitFallbackTimerId && exit_fallback_timer_ != 0 && exit_request_pending_) {
        CompleteExplicitExit();
        return 0;
      }
      break;
    case kTrayCallbackMessage:
      HandleTrayMessage(lparam);
      return 0;
    case WM_FONTCHANGE:
      flutter_controller_->engine()->ReloadSystemFonts();
      break;
  }

  return Win32Window::MessageHandler(hwnd, message, wparam, lparam);
}

flutter::EncodableMap FlutterWindow::WindowState() {
  HWND hwnd = GetHandle();
  RECT outer{}, visible{}, client{};
  GetWindowRect(hwnd, &outer);
  visible = outer;
  const HRESULT visible_result = DwmGetWindowAttribute(
      hwnd, DWMWA_EXTENDED_FRAME_BOUNDS, &visible, sizeof(visible));
  BOOL composition = FALSE, non_client_rendering = FALSE;
  const HRESULT composition_result = DwmIsCompositionEnabled(&composition);
  const HRESULT non_client_result = DwmGetWindowAttribute(
      hwnd, DWMWA_NCRENDERING_ENABLED, &non_client_rendering,
      sizeof(non_client_rendering));
  GetClientRect(hwnd, &client);
  const double dpi = static_cast<double>(GetDpiForWindow(hwnd));
  const double scale = dpi / 96.0;
  using flutter::EncodableValue;
  const auto rectangle = [](RECT r) {
    return EncodableValue(flutter::EncodableList{
        EncodableValue(static_cast<int>(r.left)), EncodableValue(static_cast<int>(r.top)),
        EncodableValue(static_cast<int>(r.right-r.left)),
        EncodableValue(static_cast<int>(r.bottom-r.top))});
  };
  return {
    {EncodableValue("maximized"), EncodableValue(IsZoomed(hwnd) != FALSE)},
    {EncodableValue("active"), EncodableValue(GetForegroundWindow() == hwnd)},
    {EncodableValue("minimized"), EncodableValue(IsIconic(hwnd) != FALSE)},
    {EncodableValue("maximizeHovered"), EncodableValue(maximize_hovered_)},
    {EncodableValue("maximizePressed"), EncodableValue(maximize_pressed_)},
    {EncodableValue("dpi"), EncodableValue(dpi)},
    {EncodableValue("outerPhysical"), rectangle(outer)},
    {EncodableValue("visiblePhysical"), rectangle(visible)},
    {EncodableValue("clientPhysical"), rectangle(client)},
    {EncodableValue("clientLogicalWidth"), EncodableValue(client.right/scale)},
    {EncodableValue("clientLogicalHeight"), EncodableValue(client.bottom/scale)},
    {EncodableValue("topLogical"), EncodableValue(custom_frame_enabled() ? 46.0 : 0.0)},
    {EncodableValue("contentLogicalHeight"), EncodableValue(
        client.bottom/scale - (custom_frame_enabled() ? 46.0 : 0.0))},
    {EncodableValue("customFrame"), EncodableValue(custom_frame_enabled())},
    {EncodableValue("darkFrame"), EncodableValue(frame_dark_mode())},
    {EncodableValue("cornerHRESULT"), EncodableValue(static_cast<int>(corner_result()))},
    {EncodableValue("borderHRESULT"), EncodableValue(static_cast<int>(border_result()))},
    {EncodableValue("cornerPreferenceRequested"), EncodableValue(custom_frame_enabled() ? 2 : 0)},
    {EncodableValue("renderingPolicyHRESULT"), EncodableValue(static_cast<int>(rendering_policy_result()))},
    {EncodableValue("frameExtensionHRESULT"), EncodableValue(static_cast<int>(frame_extension_result()))},
    {EncodableValue("frameExtensionPhysicalPixels"), EncodableValue(frame_extension_pixels())},
    {EncodableValue("dwmComposition"), EncodableValue(composition != FALSE)},
    {EncodableValue("compositionHRESULT"), EncodableValue(static_cast<int>(composition_result))},
    {EncodableValue("nonClientRendering"), EncodableValue(non_client_rendering != FALSE)},
    {EncodableValue("nonClientHRESULT"), EncodableValue(static_cast<int>(non_client_result))},
    {EncodableValue("visibleHRESULT"), EncodableValue(static_cast<int>(visible_result))},
    {EncodableValue("closeToTray"), EncodableValue(desktop_presence_initialized_)},
  };
}

void FlutterWindow::PublishWindowState() {
  if (window_channel_ && GetHandle()) {
    window_channel_->InvokeMethod("stateChanged",
        std::make_unique<flutter::EncodableValue>(WindowState()));
  }
}
