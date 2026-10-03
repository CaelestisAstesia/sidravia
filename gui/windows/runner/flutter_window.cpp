#include "flutter_window.h"

#include <flutter/encodable_value.h>
#include <shellapi.h>

#include <cwchar>
#include <optional>
#include <string>

#include "flutter/generated_plugin_registrant.h"
#include "resource.h"

namespace {
constexpr UINT kTrayIconId = 1;
constexpr UINT kTrayCallbackMessage = WM_APP + 1;
constexpr int kTrayOpenCommand = 1001;
constexpr int kTrayExitCommand = 1002;
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
        if (call.method_name() == "notify") {
          std::wstring title = L"Sidravia";
          std::wstring body;
          const auto* arguments =
              std::get_if<flutter::EncodableMap>(call.arguments());
          if (arguments != nullptr) {
            for (const auto& entry : *arguments) {
              const auto key = std::get_if<std::string>(&entry.first);
              const auto value = std::get_if<std::string>(&entry.second);
              if (key == nullptr || value == nullptr) continue;
              if (*key == "title") {
                title = Utf8ToUtf16(*value);
              } else if (*key == "body") {
                body = Utf8ToUtf16(*value);
              }
            }
          }
          NotifyTray(title, body);
          result->Success();
          return;
        }
        if (call.method_name() == "destroy") {
          result->Success();
          CompleteExplicitExit();
          return;
        }
        result->NotImplemented();
      });

  taskbar_created_message_ = ::RegisterWindowMessageW(L"TaskbarCreated");
  SetChildContent(flutter_controller_->view()->GetNativeWindow());

  flutter_controller_->engine()->SetNextFrameCallback([&]() {
    this->Show();
  });

  // Flutter can complete the first frame before the "show window" callback is
  // registered. The following call ensures a frame is pending to ensure the
  // window is shown. It is a no-op if the first frame hasn't completed yet.
  flutter_controller_->ForceRedraw();

  return true;
}

void FlutterWindow::OnDestroy() {
  if (exit_fallback_timer_ != 0 && GetHandle() != nullptr) {
    ::KillTimer(GetHandle(), exit_fallback_timer_);
    exit_fallback_timer_ = 0;
  }
  RemoveTrayIcon();
  ReleaseDesktopPresence();
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

void FlutterWindow::NotifyTray(const std::wstring& title,
                               const std::wstring& body) {
  HWND hwnd = GetHandle();
  if (hwnd == nullptr) return;
  NOTIFYICONDATAW nid{};
  nid.cbSize = sizeof(NOTIFYICONDATAW);
  nid.hWnd = hwnd;
  nid.uID = kTrayIconId;
  nid.uFlags = NIF_INFO;
  nid.dwInfoFlags = NIIF_INFO;
  wcsncpy_s(nid.szInfoTitle, title.c_str(), _TRUNCATE);
  wcsncpy_s(nid.szInfo, body.c_str(), _TRUNCATE);
  ::Shell_NotifyIconW(NIM_MODIFY, &nid);
}

void FlutterWindow::ShowTrayMenu() {
  HWND hwnd = GetHandle();
  HMENU menu = ::CreatePopupMenu();
  // Keep the native menu Unicode and expose the same hierarchy as the HTML
  // tray mockup: a read-only status header followed by lifecycle actions.
  ::AppendMenuW(menu, MF_STRING | MF_GRAYED, 0, L"\u5DF2\u8FDE\u63A5");
  ::AppendMenuW(menu, MF_STRING | MF_GRAYED, 0, L"\u6821\u56ED\u7F51");
  ::AppendMenuW(menu, MF_STRING | MF_GRAYED, 0,
                L"\u72B6\u6001\u7531\u4E3B\u7A97\u53E3\u63D0\u4F9B");
  ::AppendMenuW(menu, MF_SEPARATOR, 0, nullptr);
  ::AppendMenuW(menu, MF_STRING, kTrayOpenCommand,
                L"\u663E\u793A\u4E3B\u754C\u9762");
  ::AppendMenuW(menu, MF_STRING, kTrayExitCommand,
                L"\u9000\u51FA Sidravia");
  ::SetMenuDefaultItem(menu, kTrayOpenCommand, FALSE);
  POINT pt{};
  ::GetCursorPos(&pt);
  ::SetForegroundWindow(hwnd);
  const int command =
      ::TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_RETURNCMD | TPM_NONOTIFY,
                       pt.x, pt.y, 0, hwnd, nullptr);
  ::DestroyMenu(menu);
  if (command == kTrayOpenCommand) {
    RestoreWindow();
  } else if (command == kTrayExitCommand) {
    RequestExplicitExit();
  }
}

void FlutterWindow::HandleTrayMessage(LPARAM lparam) {
  const UINT event = LOWORD(static_cast<DWORD>(lparam));
  switch (event) {
    case WM_LBUTTONUP:
    case WM_LBUTTONDBLCLK:
    case NIN_BALLOONUSERCLICK:
      RestoreWindow();
      break;
    case WM_RBUTTONUP:
    case WM_CONTEXTMENU:
      ShowTrayMenu();
      break;
    default:
      break;
  }
}

void FlutterWindow::RequestExplicitExit() {
  if (exit_request_pending_) return;
  exit_request_pending_ = true;
  exit_fallback_timer_ =
      ::SetTimer(GetHandle(), kExitFallbackTimerId,
                 kExitFallbackMilliseconds, nullptr);
  if (exit_fallback_timer_ == 0) {
    CompleteExplicitExit();
    return;
  }
  if (desktop_channel_ && flutter_controller_) {
    desktop_channel_->InvokeMethod("exitRequested", nullptr);
    return;
  }
  CompleteExplicitExit();
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
        ShowWindow(hwnd, SW_HIDE);
        return 0;
      }
      break;
    case WM_TIMER:
      if (wparam == kExitFallbackTimerId && exit_request_pending_) {
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
