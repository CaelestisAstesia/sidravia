#include "win32_window.h"

#include <dwmapi.h>
#include <flutter_windows.h>
#include <windowsx.h>
#include <commctrl.h>
#include "frame_geometry.h"

#include "resource.h"

namespace {

/// Window attribute that enables dark mode window decorations.
///
/// Redefined in case the developer's machine has a Windows SDK older than
/// version 10.0.22000.0.
/// See: https://docs.microsoft.com/windows/win32/api/dwmapi/ne-dwmapi-dwmwindowattribute
#ifndef DWMWA_USE_IMMERSIVE_DARK_MODE
#define DWMWA_USE_IMMERSIVE_DARK_MODE 20
#endif
constexpr const wchar_t kWindowClassName[] = L"FLUTTER_RUNNER_WIN32_WINDOW";
// Logical outer-frame minimum; Scale converts it to monitor physical pixels.
constexpr int kMinimumWindowWidth = 360;
constexpr int kMinimumWindowHeight = 640;

// The number of Win32Window objects that currently exist.
static int g_active_window_count = 0;

using EnableNonClientDpiScaling = BOOL __stdcall(HWND hwnd);

// Scale helper to convert logical scaler values to physical using passed in
// scale factor
int Scale(int source, double scale_factor) {
  return static_cast<int>(source * scale_factor);
}

// Dynamically loads the |EnableNonClientDpiScaling| from the User32 module.
// This API is only needed for PerMonitor V1 awareness mode.
void EnableFullDpiSupportIfAvailable(HWND hwnd) {
  HMODULE user32_module = LoadLibraryA("User32.dll");
  if (!user32_module) {
    return;
  }
  auto enable_non_client_dpi_scaling =
      reinterpret_cast<EnableNonClientDpiScaling*>(
          GetProcAddress(user32_module, "EnableNonClientDpiScaling"));
  if (enable_non_client_dpi_scaling != nullptr) {
    enable_non_client_dpi_scaling(hwnd);
  }
  FreeLibrary(user32_module);
}

}  // namespace

// Manages the Win32Window's window class registration.
class WindowClassRegistrar {
 public:
  ~WindowClassRegistrar() = default;

  // Returns the singleton registrar instance.
  static WindowClassRegistrar* GetInstance() {
    if (!instance_) {
      instance_ = new WindowClassRegistrar();
    }
    return instance_;
  }

  // Returns the name of the window class, registering the class if it hasn't
  // previously been registered.
  const wchar_t* GetWindowClass();

  // Unregisters the window class. Should only be called if there are no
  // instances of the window.
  void UnregisterWindowClass();

 private:
  WindowClassRegistrar() = default;

  static WindowClassRegistrar* instance_;

  bool class_registered_ = false;
};

WindowClassRegistrar* WindowClassRegistrar::instance_ = nullptr;

const wchar_t* WindowClassRegistrar::GetWindowClass() {
  if (!class_registered_) {
    WNDCLASS window_class{};
    window_class.hCursor = LoadCursor(nullptr, IDC_ARROW);
    window_class.lpszClassName = kWindowClassName;
    window_class.style = CS_HREDRAW | CS_VREDRAW | CS_DBLCLKS;
    window_class.cbClsExtra = 0;
    window_class.cbWndExtra = 0;
    window_class.hInstance = GetModuleHandle(nullptr);
    window_class.hIcon =
        LoadIcon(window_class.hInstance, MAKEINTRESOURCE(IDI_APP_ICON));
    window_class.hbrBackground = 0;
    window_class.lpszMenuName = nullptr;
    window_class.lpfnWndProc = Win32Window::WndProc;
    RegisterClass(&window_class);
    class_registered_ = true;
  }
  return kWindowClassName;
}

void WindowClassRegistrar::UnregisterWindowClass() {
  UnregisterClass(kWindowClassName, nullptr);
  class_registered_ = false;
}

Win32Window::Win32Window() {
  ++g_active_window_count;
}

Win32Window::~Win32Window() {
  --g_active_window_count;
  Destroy();
}

bool Win32Window::Create(const std::wstring& title,
                         const Point& origin,
                         const Size& size) {
  Destroy();

  const wchar_t* window_class =
      WindowClassRegistrar::GetInstance()->GetWindowClass();

  const POINT target_point = {static_cast<LONG>(origin.x),
                              static_cast<LONG>(origin.y)};
  HMONITOR monitor = MonitorFromPoint(target_point, MONITOR_DEFAULTTONEAREST);
  UINT dpi = FlutterDesktopGetDpiForMonitor(monitor);
  double scale_factor = dpi / 96.0;

  HWND window = CreateWindow(
      window_class, title.c_str(),
      WS_OVERLAPPEDWINDOW,
      Scale(origin.x, scale_factor), Scale(origin.y, scale_factor),
      Scale(size.width, scale_factor), Scale(size.height, scale_factor),
      nullptr, nullptr, GetModuleHandle(nullptr), this);

  if (!window) {
    return false;
  }

  UpdateTheme(window);

  return OnCreate();
}

bool Win32Window::Show() {
  return ShowWindow(window_handle_, SW_SHOWNORMAL);
}

// static
LRESULT CALLBACK Win32Window::WndProc(HWND const window,
                                      UINT const message,
                                      WPARAM const wparam,
                                      LPARAM const lparam) noexcept {
  if (message == WM_NCCREATE) {
    auto window_struct = reinterpret_cast<CREATESTRUCT*>(lparam);
    SetWindowLongPtr(window, GWLP_USERDATA,
                     reinterpret_cast<LONG_PTR>(window_struct->lpCreateParams));

    auto that = static_cast<Win32Window*>(window_struct->lpCreateParams);
    EnableFullDpiSupportIfAvailable(window);
    that->window_handle_ = window;
  } else if (Win32Window* that = GetThisFromHandle(window)) {
    return that->MessageHandler(window, message, wparam, lparam);
  }

  return DefWindowProc(window, message, wparam, lparam);
}

LRESULT
Win32Window::MessageHandler(HWND hwnd,
                            UINT const message,
                            WPARAM const wparam,
                            LPARAM const lparam) noexcept {
  switch (message) {
    case WM_NCCALCSIZE:
      if (custom_frame_enabled_) {
        // Entire outer rectangle becomes client space. No native caption deduction.
        // GETMINMAXINFO constrains maximization to the current monitor work area.
        return 0;
      }
      break;
    case WM_NCHITTEST:
      if (custom_frame_enabled_) return FrameHitTest(lparam);
      break;
    case WM_DESTROY:
      window_handle_ = nullptr;
      Destroy();
      if (quit_on_close_) {
        PostQuitMessage(0);
      }
      return 0;

    case WM_DPICHANGED: {
      auto newRectSize = reinterpret_cast<RECT*>(lparam);
      LONG newWidth = newRectSize->right - newRectSize->left;
      LONG newHeight = newRectSize->bottom - newRectSize->top;

      SetWindowPos(hwnd, nullptr, newRectSize->left, newRectSize->top, newWidth,
                   newHeight, SWP_NOZORDER | SWP_NOACTIVATE);

      return 0;
    }
    case WM_GETMINMAXINFO: {
      auto min_max_info = reinterpret_cast<MINMAXINFO*>(lparam);
      HMONITOR monitor = MonitorFromWindow(hwnd, MONITOR_DEFAULTTONEAREST);
      MONITORINFO monitor_info{};
      monitor_info.cbSize = sizeof(MONITORINFO);
      if (GetMonitorInfo(monitor, &monitor_info)) {
        RECT work = monitor_info.rcWork;
        RECT bounds = monitor_info.rcMonitor;
        min_max_info->ptMaxPosition.x = work.left - bounds.left;
        min_max_info->ptMaxPosition.y = work.top - bounds.top;
        min_max_info->ptMaxSize.x = work.right - work.left;
        min_max_info->ptMaxSize.y = work.bottom - work.top;
      }
      UINT dpi = FlutterDesktopGetDpiForMonitor(monitor);
      min_max_info->ptMinTrackSize.x =
          sidravia_frame::PhysicalMinimum(kMinimumWindowWidth, dpi);
      min_max_info->ptMinTrackSize.y =
          sidravia_frame::PhysicalMinimum(kMinimumWindowHeight, dpi);
      return 0;
    }
    case WM_SIZE: {
      RECT rect = GetClientArea();
      if (child_content_ != nullptr) {
        // Size and position the child window.
        MoveWindow(child_content_, rect.left, rect.top, rect.right - rect.left,
                   rect.bottom - rect.top, TRUE);
      }
      return 0;
    }

    case WM_ACTIVATE:
      if (LOWORD(wparam) != WA_INACTIVE && child_content_ != nullptr) {
        SetFocus(child_content_);
      }
      return 0;

    case WM_DWMCOLORIZATIONCOLORCHANGED:
      UpdateTheme(hwnd);
      return 0;
  }

  return DefWindowProc(window_handle_, message, wparam, lparam);
}

void Win32Window::Destroy() {
  OnDestroy();

  if (window_handle_) {
    DestroyWindow(window_handle_);
    window_handle_ = nullptr;
  }
  if (g_active_window_count == 0) {
    WindowClassRegistrar::GetInstance()->UnregisterWindowClass();
  }
}

Win32Window* Win32Window::GetThisFromHandle(HWND const window) noexcept {
  return reinterpret_cast<Win32Window*>(
      GetWindowLongPtr(window, GWLP_USERDATA));
}

void Win32Window::SetChildContent(HWND content) {
  child_content_ = content;
  SetParent(content, window_handle_);
  SetWindowSubclass(content, ChildFrameProc, 1, reinterpret_cast<DWORD_PTR>(this));
  RECT frame = GetClientArea();

  MoveWindow(content, frame.left, frame.top, frame.right - frame.left,
             frame.bottom - frame.top, true);

  SetFocus(child_content_);
}

RECT Win32Window::GetClientArea() {
  RECT frame;
  GetClientRect(window_handle_, &frame);
  return frame;
}

HWND Win32Window::GetHandle() {
  return window_handle_;
}

void Win32Window::SetQuitOnClose(bool quit_on_close) {
  quit_on_close_ = quit_on_close;
}

bool Win32Window::OnCreate() {
  // No-op; provided for subclasses.
  return true;
}

void Win32Window::OnDestroy() {
  // No-op; provided for subclasses.
}

void Win32Window::UpdateTheme(HWND const window) {
  BOOL enable_dark_mode = frame_dark_ ? TRUE : FALSE;
  DwmSetWindowAttribute(window, DWMWA_USE_IMMERSIVE_DARK_MODE,
                        &enable_dark_mode, sizeof(enable_dark_mode));
}

void Win32Window::EnableCustomFrame(bool enabled) {
  custom_frame_enabled_ = enabled;
  // Attribute 33 is DWMWA_WINDOW_CORNER_PREFERENCE, value 1 is DONOTROUND.
  // Older Windows safely returns E_INVALIDARG; keep native rectangular fallback.
  const int preference = 1;
  corner_result_ = DwmSetWindowAttribute(window_handle_, 33, &preference,
                                        sizeof(preference));
  // DWMWA_BORDER_COLOR (34): the Flutter frame draws one physical-pixel edge.
  // Suppress the Win11 edge to avoid a double outline; restore the system
  // default when disabling the custom frame. Older systems return E_INVALIDARG.
  // This changes only the border, not DWM non-client rendering/shadow policy.
  const COLORREF border = enabled ? 0xFFFFFFFE : 0xFFFFFFFF;
  border_result_ = DwmSetWindowAttribute(window_handle_, 34, &border,
                                        sizeof(border));
  SetWindowPos(window_handle_, nullptr, 0, 0, 0, 0,
               SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE |
               SWP_FRAMECHANGED);
}

LRESULT Win32Window::FrameHitTest(LPARAM screen_point) {
  POINT point{GET_X_LPARAM(screen_point), GET_Y_LPARAM(screen_point)};
  ScreenToClient(window_handle_, &point);
  RECT rect = GetClientArea();
  const auto hit = sidravia_frame::HitTest(
      point.x, point.y, rect.right, rect.bottom, GetDpiForWindow(window_handle_),
      IsZoomed(window_handle_) != FALSE, custom_frame_enabled_, frame_modal_blocked_);
  using sidravia_frame::Hit;
  switch (hit) {
    case Hit::caption: return HTCAPTION;
    case Hit::maximize: return HTMAXBUTTON;
    case Hit::left: return HTLEFT;
    case Hit::right: return HTRIGHT;
    case Hit::top: return HTTOP;
    case Hit::bottom: return HTBOTTOM;
    case Hit::top_left: return HTTOPLEFT;
    case Hit::top_right: return HTTOPRIGHT;
    case Hit::bottom_left: return HTBOTTOMLEFT;
    case Hit::bottom_right: return HTBOTTOMRIGHT;
    default: return HTCLIENT;
  }
}

LRESULT CALLBACK Win32Window::ChildFrameProc(HWND child, UINT message,
    WPARAM wparam, LPARAM lparam, UINT_PTR id, DWORD_PTR data) {
  auto* owner = reinterpret_cast<Win32Window*>(data);
  if (owner->custom_frame_enabled_) {
    if (message == WM_NCHITTEST && owner->FrameHitTest(lparam) != HTCLIENT) {
      // Let the same-thread parent own non-client operations; Flutter's child
      // would otherwise consume caption/resize messages across the full surface.
      return HTTRANSPARENT;
    }
    if (message == WM_SYSKEYDOWN && (lparam & (1L << 29))) {
      if (wparam == VK_SPACE || wparam == VK_F4) {
        return SendMessage(owner->window_handle_, WM_SYSCOMMAND,
            wparam == VK_SPACE ? SC_KEYMENU : SC_CLOSE,
            wparam == VK_SPACE ? L' ' : 0);
      }
    }
  }
  if (message == WM_NCDESTROY) RemoveWindowSubclass(child, ChildFrameProc, id);
  return DefSubclassProc(child, message, wparam, lparam);
}

void Win32Window::SetFrameDarkMode(bool dark) {
  frame_dark_ = dark;
  UpdateTheme(window_handle_);
}
