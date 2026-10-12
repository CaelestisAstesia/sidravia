#include "school_announcement_window.h"
#include "announcement_link_policy.h"

#include <WebView2.h>
#include <shlobj.h>
#include <shlwapi.h>
#include <shellapi.h>
#include <urlmon.h>
#include <wrl.h>
#include <algorithm>
#include <climits>
#include <deque>
#include <new>
#include <string>

using Microsoft::WRL::Callback;
using Microsoft::WRL::ComPtr;

namespace {
constexpr wchar_t kWindowClass[] = L"Sidravia.SchoolAnnouncement";
constexpr wchar_t kTitle[] = L"\u6821\u56ed\u7f51\u516c\u544a";
constexpr wchar_t kLoading[] = L"\u6b63\u5728\u52a0\u8f7d\u516c\u544a\u2026";
constexpr wchar_t kFailed[] = L"\u516c\u544a\u52a0\u8f7d\u5931\u8d25";
constexpr DWORD kStyle = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU |
                         WS_MINIMIZEBOX;
constexpr UINT_PTR kLoadTimer = 1;
constexpr UINT kLoadTimeout = 30000;
constexpr UINT kOpenExternalLink = WM_APP + 42;
constexpr wchar_t kLinkFailed[] = L"\u65e0\u6cd5\u6253\u5f00\u94fe\u63a5\uff0c\u8bf7\u68c0\u67e5\u9ed8\u8ba4\u6d4f\u89c8\u5668\u8bbe\u7f6e";

bool ConvertUtf8(const std::string& source, std::wstring* target) {
  if (source.empty() || source.size() > INT_MAX) return false;
  const int size = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
      source.data(), static_cast<int>(source.size()), nullptr, 0);
  if (size <= 0) return false;
  target->resize(static_cast<size_t>(size));
  return MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, source.data(),
      static_cast<int>(source.size()), target->data(), size) == size;
}

bool ValidNamespace(const std::string& name, std::wstring* leaf) {
  if (name.empty()) { *leaf = L"production"; return true; }
  if (name.size() > 64 || name.front() == '.' || name.back() == '.' ||
      name.find("..") != std::string::npos) return false;
  std::string folded;
  for (const unsigned char ch : name) {
    if (!((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
          (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-'))
      return false;
    folded.push_back(static_cast<char>(
        ch >= 'A' && ch <= 'Z' ? ch + ('a' - 'A') : ch));
  }
  if (folded == "production" || folded == "default" || folded == "prod")
    return false;
  *leaf = L"ns-" + std::wstring(folded.begin(), folded.end());
  return true;
}

bool IsSpaceOrControl(wchar_t ch) {
  return ch <= 0x20 || (ch >= 0x7f && ch <= 0x9f) || ch == 0xa0 ||
      ch == 0x1680 || (ch >= 0x2000 && ch <= 0x200a) || ch == 0x2028 ||
      ch == 0x2029 || ch == 0x202f || ch == 0x205f || ch == 0x3000;
}

// CreateUri supplies Windows parsing; raw guards prevent normalized-away
// empty userinfo/fragments and control/space characters in initial metadata.
bool HttpUri(const std::wstring& text, bool initial) {
  if (text.empty()) return false;
  if (initial) {
    if (std::any_of(text.begin(), text.end(), IsSpaceOrControl) ||
        text.find(L'#') != std::wstring::npos) return false;
    const auto separator = text.find(L"://");
    if (separator == std::wstring::npos) return false;
    const auto end = text.find_first_of(L"/?#", separator + 3);
    if (text.substr(separator + 3, end == std::wstring::npos
        ? end : end - separator - 3).find(L'@') != std::wstring::npos)
      return false;
    for (size_t i = 0; i < text.size(); ++i) {
      if (text[i] != L'%') continue;
      const auto hex = [](wchar_t c) {
        return (c >= L'0' && c <= L'9') || (c >= L'a' && c <= L'f') ||
            (c >= L'A' && c <= L'F');
      };
      if (i + 2 >= text.size() || !hex(text[i+1]) || !hex(text[i+2]))
        return false;
    }
  }
  ComPtr<IUri> uri;
  if (FAILED(CreateUri(text.c_str(), Uri_CREATE_CANONICALIZE, 0, &uri)))
    return false;
  DWORD scheme = 0;
  BSTR host = nullptr;
  const HRESULT host_result = uri->GetHost(&host);
  const bool valid_host = SUCCEEDED(host_result) && host && SysStringLen(host);
  SysFreeString(host);
  if (FAILED(uri->GetScheme(&scheme)) || !valid_host ||
      (scheme != URL_SCHEME_HTTP && scheme != URL_SCHEME_HTTPS)) return false;
  if (initial) {
    BSTR userinfo = nullptr;
    const HRESULT result = uri->GetUserInfo(&userinfo);
    const bool empty = SUCCEEDED(result) && (!userinfo || !SysStringLen(userinfo));
    SysFreeString(userinfo);
    if (!empty) return false;
  }
  return true;
}

bool CachePath(const std::wstring& leaf, std::wstring* path) {
  PWSTR local = nullptr;
  const HRESULT result = SHGetKnownFolderPath(FOLDERID_LocalAppData, 0,
                                              nullptr, &local);
  if (FAILED(result) || !local) { CoTaskMemFree(local); return false; }
  *path = std::wstring(local) + L"\\Sidravia\\announcement-webview\\" + leaf;
  CoTaskMemFree(local);
  const int created = SHCreateDirectoryExW(nullptr, path->c_str(), nullptr);
  return created == ERROR_SUCCESS || created == ERROR_ALREADY_EXISTS ||
      created == ERROR_FILE_EXISTS;
}

const flutter::EncodableValue* Field(const flutter::EncodableMap& map,
                                     const char* key) {
  const auto it = map.find(flutter::EncodableValue(key));
  return it == map.end() ? nullptr : &it->second;
}

bool Dimension(const flutter::EncodableValue* value, int* result) {
  if (!value) return false;
  int64_t number = 0;
  if (const auto* value32 = std::get_if<int32_t>(value)) number = *value32;
  else if (const auto* large = std::get_if<int64_t>(value)) number = *large;
  else return false;
  if (number < 1 || number > 4096) return false;
  *result = static_cast<int>(number);
  return true;
}
}  // namespace

struct SchoolAnnouncementWindow::State {
  HWND window = nullptr;
  int width = 0, height = 0;
  bool active = true, failed = false, loading = true;
  UINT_PTR timer = 0;
  UINT64 navigation_id = 0;
  std::wstring url, cache;
  std::wstring document_url;
  std::deque<std::wstring> external_links;
  bool external_message_pending = false;
  ComPtr<ICoreWebView2Controller> controller;
  ComPtr<ICoreWebView2> browser;
  HRESULT teardown_result = S_OK;

  bool Live() const { return active && !failed && window != nullptr; }

  bool QueueLink(const std::wstring& link) {
    if (!Live()) return false;
    external_links.push_back(link);
    if (external_message_pending) return true;
    if (!PostMessageW(window, kOpenExternalLink, 0, 0)) {
      external_links.pop_back();
      SetWindowTextW(window, kLinkFailed);
      return false;
    }
    external_message_pending = true;
    return true;
  }

  // Dispatched outside WebView callbacks; no nested modal message pump in those callbacks.
  void OpenQueuedLinks() {
    external_message_pending = false;
    while (Live() && !external_links.empty()) {
      const auto link = std::move(external_links.front());
      external_links.pop_front();
      SHELLEXECUTEINFOW info{};
      info.cbSize = sizeof(info);
      info.fMask = SEE_MASK_FLAG_NO_UI | SEE_MASK_ASYNCOK;
      info.hwnd = window;
      info.lpVerb = L"open";
      info.lpFile = link.c_str();
      info.nShow = SW_SHOWNORMAL;
      if (!ShellExecuteExW(&info) && Live()) {
        MessageBoxW(window, kLinkFailed, kTitle, MB_OK | MB_ICONWARNING);
      }
    }
  }

  void CancelTimer() {
    if (timer && window) KillTimer(window, timer);
    timer = 0;
  }

  void ReleaseBrowser() {
    // Invalidate first, since Stop/Close can synchronously deliver events.
    if (browser) {
      const HRESULT stopped = browser->Stop();
      if (FAILED(stopped)) teardown_result = stopped;
    }
    if (controller) {
      const HRESULT hidden = controller->put_IsVisible(FALSE);
      if (FAILED(hidden)) teardown_result = hidden;
      const HRESULT closed = controller->Close();
      if (FAILED(closed)) teardown_result = closed;
    }
    browser.Reset();
    controller.Reset();
  }

  void Fail() {
    if (!active || failed) return;
    failed = true;
    external_links.clear();
    external_message_pending = false;
    loading = false;
    CancelTimer();
    ReleaseBrowser();
    if (window) InvalidateRect(window, nullptr, TRUE);
  }

  void Deactivate() {
    active = false;
    external_links.clear();
    external_message_pending = false;
    CancelTimer();
    ReleaseBrowser();
  }

  bool StartTimer() {
    CancelTimer();
    timer = SetTimer(window, kLoadTimer, kLoadTimeout, nullptr);
    return timer != 0;
  }

  bool Bounds() {
    if (!controller) return true;
    RECT bounds{};
    return GetClientRect(window, &bounds) &&
        SUCCEEDED(controller->put_Bounds(bounds));
  }

  bool Place(UINT dpi, HMONITOR monitor, bool center, int x, int y) {
    RECT outer{0, 0, MulDiv(width, static_cast<int>(dpi), 96),
                     MulDiv(height, static_cast<int>(dpi), 96)};
    if (!AdjustWindowRectExForDpi(&outer, kStyle, FALSE, 0, dpi)) return false;
    MONITORINFO info{sizeof(MONITORINFO)};
    if (!GetMonitorInfoW(monitor, &info)) return false;
    const int w = outer.right - outer.left, h = outer.bottom - outer.top;
    if (center) {
      x = info.rcWork.left + (info.rcWork.right - info.rcWork.left - w) / 2;
      y = info.rcWork.top + (info.rcWork.bottom - info.rcWork.top - h) / 2;
    }
    x = std::max(static_cast<int>(info.rcWork.left),
        std::min(x, static_cast<int>(info.rcWork.right) - w));
    y = std::max(static_cast<int>(info.rcWork.top),
        std::min(y, static_cast<int>(info.rcWork.bottom) - h));
    return SetWindowPos(window, nullptr, x, y, w, h,
                         SWP_NOZORDER | SWP_NOACTIVATE) != FALSE;
  }

  bool Secure(const std::weak_ptr<State>& weak) {
    ComPtr<ICoreWebView2Settings> settings;
    ComPtr<ICoreWebView2Settings4> settings4;
    ComPtr<ICoreWebView2_4> downloads;
    ComPtr<ICoreWebView2_18> external;
    if (FAILED(browser->get_Settings(&settings)) || !settings ||
        FAILED(settings.As(&settings4)) || FAILED(browser.As(&downloads)) ||
        FAILED(browser.As(&external)) ||
        FAILED(settings->put_AreHostObjectsAllowed(FALSE)) ||
        FAILED(settings->put_IsWebMessageEnabled(FALSE)) ||
        FAILED(settings4->put_IsPasswordAutosaveEnabled(FALSE)) ||
        FAILED(settings4->put_IsGeneralAutofillEnabled(FALSE)) ||
        FAILED(settings->put_IsBuiltInErrorPageEnabled(FALSE))) return false;
    EventRegistrationToken token{};
    if (FAILED(browser->add_PermissionRequested(
        Callback<ICoreWebView2PermissionRequestedEventHandler>(
          [weak](ICoreWebView2*, ICoreWebView2PermissionRequestedEventArgs* args) {
            const HRESULT hr = args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY);
            if (FAILED(hr)) { if (auto state = weak.lock()) state->Fail(); }
            return hr;
          }).Get(), &token))) return false;
    if (FAILED(downloads->add_DownloadStarting(
        Callback<ICoreWebView2DownloadStartingEventHandler>(
          [weak](ICoreWebView2*, ICoreWebView2DownloadStartingEventArgs* args) {
            const HRESULT hr = args->put_Cancel(TRUE);
            if (FAILED(hr)) { if (auto state = weak.lock()) state->Fail(); }
            return hr;
          }).Get(), &token))) return false;
    if (FAILED(browser->add_NewWindowRequested(
        Callback<ICoreWebView2NewWindowRequestedEventHandler>(
          [weak](ICoreWebView2*, ICoreWebView2NewWindowRequestedEventArgs* args) {
            const HRESULT hr = args->put_Handled(TRUE);
            auto state = weak.lock();
            if (FAILED(hr)) { if (state) state->Fail(); return hr; }
            if (!state || !state->Live()) return S_OK;
            BOOL user = FALSE;
            LPWSTR uri = nullptr;
            const HRESULT read = args->get_Uri(&uri);
            const HRESULT gesture = args->get_IsUserInitiated(&user);
            const std::wstring target = SUCCEEDED(read) && uri ? uri : L"";
            CoTaskMemFree(uri);
            if (FAILED(read) || FAILED(gesture)) { state->Fail(); return E_FAIL; }
            if (DecideAnnouncementLink(state->document_url, target, user != FALSE,
                false, true, !state->loading) == AnnouncementLinkAction::external_browser) {
              return state->QueueLink(target) ? S_OK : E_FAIL;
            }
            return S_OK;
          }).Get(), &token))) return false;
    if (FAILED(external->add_LaunchingExternalUriScheme(
        Callback<ICoreWebView2LaunchingExternalUriSchemeEventHandler>(
          [weak](ICoreWebView2*, ICoreWebView2LaunchingExternalUriSchemeEventArgs* args) {
            const HRESULT hr = args->put_Cancel(TRUE);
            if (FAILED(hr)) { if (auto state = weak.lock()) state->Fail(); }
            return hr;
          }).Get(), &token))) return false;
    const auto guard = [weak](ICoreWebView2*,
                            ICoreWebView2NavigationStartingEventArgs* args) {
      auto state = weak.lock();
      LPWSTR uri = nullptr;
      const HRESULT read = args->get_Uri(&uri);
      const bool allowed = state && state->Live() && SUCCEEDED(read) && uri &&
          HttpUri(uri, false);
      CoTaskMemFree(uri);
      if (!allowed) {
        const HRESULT cancel = args->put_Cancel(TRUE);
        if (FAILED(cancel) && state) state->Fail();
        return cancel;
      }
      return S_OK;
    };
    if (FAILED(browser->add_NavigationStarting(
        Callback<ICoreWebView2NavigationStartingEventHandler>(
          [weak, guard](ICoreWebView2* sender,
                       ICoreWebView2NavigationStartingEventArgs* args) {
            const HRESULT hr = guard(sender, args);
            auto state = weak.lock();
            BOOL canceled = TRUE;
            if (FAILED(hr) || !state || !state->Live()) return hr;
            if (FAILED(args->get_Cancel(&canceled))) { state->Fail(); return E_FAIL; }
            if (canceled) return S_OK;
            BOOL user = FALSE, redirected = FALSE;
            LPWSTR uri = nullptr;
            const HRESULT read = args->get_Uri(&uri);
            const HRESULT gesture = args->get_IsUserInitiated(&user);
            const HRESULT redirect = args->get_IsRedirected(&redirected);
            const std::wstring target = SUCCEEDED(read) && uri ? uri : L"";
            CoTaskMemFree(uri);
            if (FAILED(read) || FAILED(gesture) || FAILED(redirect)) { state->Fail(); return E_FAIL; }
            const auto action = DecideAnnouncementLink(state->document_url, target,
                user != FALSE, redirected != FALSE, false, !state->loading);
            if (action != AnnouncementLinkAction::in_window) {
              const HRESULT cancel = args->put_Cancel(TRUE);
              if (FAILED(cancel)) { state->Fail(); return cancel; }
              if (action == AnnouncementLinkAction::external_browser && !state->QueueLink(target)) return E_FAIL;
              // A canceled link never changes the current navigation ID/load deadline.
              return S_OK;
            }
            if (FAILED(args->get_NavigationId(&state->navigation_id))) {
              state->Fail(); return E_FAIL;
            }
            // Preserve the initial environment/controller/load deadline across
            // redirects. Only navigation after a completed load starts anew.
            if (!state->loading && !state->StartTimer()) {
              state->Fail(); return E_FAIL;
            }
            state->loading = true;
            return S_OK;
          }).Get(), &token))) return false;
    if (FAILED(browser->add_NavigationCompleted(
        Callback<ICoreWebView2NavigationCompletedEventHandler>(
          [weak](ICoreWebView2* sender, ICoreWebView2NavigationCompletedEventArgs* args) {
            auto state = weak.lock();
            if (!state || !state->Live()) return S_OK;
            UINT64 id = 0;
            BOOL success = FALSE;
            if (FAILED(args->get_NavigationId(&id)) ||
                FAILED(args->get_IsSuccess(&success))) { state->Fail(); return E_FAIL; }
            if (id != state->navigation_id) return S_OK;
            if (!success) { state->Fail(); return S_OK; }
            LPWSTR source = nullptr;
            const HRESULT read = sender->get_Source(&source);
            if (SUCCEEDED(read) && source) state->document_url = source;
            CoTaskMemFree(source);
            if (FAILED(read) || state->document_url.empty()) { state->Fail(); return E_FAIL; }
            state->loading = false;
            state->CancelTimer();
            if (FAILED(state->controller->put_IsVisible(TRUE))) {
              state->Fail(); return E_FAIL;
            }
            InvalidateRect(state->window, nullptr, TRUE);
            return S_OK;
          }).Get(), &token))) return false;
    return true;
  }

  void Begin(const std::weak_ptr<State>& weak) {
    if (!StartTimer()) { Fail(); return; }
    const HRESULT create = CreateCoreWebView2EnvironmentWithOptions(
      nullptr, cache.c_str(), nullptr,
      Callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler>(
        [weak](HRESULT result, ICoreWebView2Environment* environment) {
          auto state = weak.lock();
          if (!state || !state->Live()) return S_OK;
          if (FAILED(result) || !environment) { state->Fail(); return S_OK; }
          const HRESULT requested = environment->CreateCoreWebView2Controller(
            state->window,
            Callback<ICoreWebView2CreateCoreWebView2ControllerCompletedHandler>(
              [weak](HRESULT hr, ICoreWebView2Controller* controller) {
                auto current = weak.lock();
                if (!current || !current->Live()) {
                  return controller ? controller->Close() : S_OK;
                }
                if (FAILED(hr) || !controller) { current->Fail(); return S_OK; }
                current->controller = controller;
                if (FAILED(controller->put_IsVisible(FALSE)) ||
                    FAILED(controller->get_CoreWebView2(&current->browser)) ||
                    !current->browser || !current->Secure(weak) ||
                    !current->Bounds() ||
                    FAILED(current->browser->Navigate(current->url.c_str()))) {
                  current->Fail();
                }
                return S_OK;
              }).Get());
          if (FAILED(requested)) state->Fail();
          return S_OK;
        }).Get());
    if (FAILED(create)) Fail();
  }
};

SchoolAnnouncementWindow::~SchoolAnnouncementWindow() { Close(); }

bool SchoolAnnouncementWindow::Open(HWND main_window,
                                    const flutter::EncodableMap& arguments) {
  if (arguments.size() != 4) return false;
  const auto* raw_url = Field(arguments, "url");
  const auto* raw_namespace = Field(arguments, "namespace");
  const auto* url = raw_url ? std::get_if<std::string>(raw_url) : nullptr;
  const auto* name = raw_namespace ? std::get_if<std::string>(raw_namespace) : nullptr;
  auto next = std::make_shared<State>();
  std::wstring leaf;
  if (!url || !name || !ConvertUtf8(*url, &next->url) ||
      !HttpUri(next->url, true) || !ValidNamespace(*name, &leaf) ||
      !Dimension(Field(arguments, "clientWidth"), &next->width) ||
      !Dimension(Field(arguments, "clientHeight"), &next->height) ||
      !CachePath(leaf, &next->cache)) return false;
  if (!Close()) return false;
  WNDCLASSW wc{};
  wc.hInstance = GetModuleHandleW(nullptr);
  wc.lpfnWndProc = WindowProc;
  wc.lpszClassName = kWindowClass;
  wc.hCursor = LoadCursorW(nullptr, IDC_ARROW);
  wc.hbrBackground = reinterpret_cast<HBRUSH>(COLOR_WINDOW + 1);
  if (!RegisterClassW(&wc) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS)
    return false;
  state_ = next;
  // No owner HWND: showing the announcement never restores/minimizes main.
  HWND window = CreateWindowExW(0, kWindowClass, kTitle, kStyle, 0, 0, 1, 1,
      nullptr, nullptr, wc.hInstance, &next);
  if (!window) { state_.reset(); return false; }
  const HMONITOR monitor = MonitorFromWindow(main_window, MONITOR_DEFAULTTONEAREST);
  UINT dpi = GetDpiForWindow(main_window);
  if (!dpi) dpi = 96;
  if (!next->Place(dpi, monitor, true, 0, 0)) { Close(); return false; }
  ShowWindow(window, SW_SHOWNORMAL);
  if (!SetForegroundWindow(window)) FlashWindow(window, TRUE);
  next->Begin(next);
  return true;
}

bool SchoolAnnouncementWindow::Close() {
  auto previous = std::move(state_);
  if (!previous) return true;
  // All callbacks capture weak state and require Live; invalidate before HWND
  // or COM release, including when asynchronous creation is still pending.
  previous->Deactivate();
  const HWND window = previous->window;
  const bool destroyed = !window || DestroyWindow(window) != FALSE;
  if (!destroyed) state_ = previous;  // HWND still references GWLP_USERDATA.
  return destroyed && SUCCEEDED(previous->teardown_result);
}

LRESULT CALLBACK SchoolAnnouncementWindow::WindowProc(HWND window, UINT message,
                                                       WPARAM wparam, LPARAM lparam) {
  // The HWND also owns a strong reference until WM_NCDESTROY, so even an OS
  // DestroyWindow failure cannot leave a dangling user-data pointer.
  auto* holder = reinterpret_cast<std::shared_ptr<State>*>(
      GetWindowLongPtrW(window, GWLP_USERDATA));
  if (message == WM_NCCREATE) {
    const auto* create = reinterpret_cast<CREATESTRUCTW*>(lparam);
    holder = new (std::nothrow) std::shared_ptr<State>(
        *static_cast<std::shared_ptr<State>*>(create->lpCreateParams));
    if (!holder) return FALSE;
    (*holder)->window = window;
    SetWindowLongPtrW(window, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(holder));
  }
  auto state = holder ? *holder : nullptr;
  if (!state) return DefWindowProcW(window, message, wparam, lparam);
  switch (message) {
    case kOpenExternalLink:
      state->OpenQueuedLinks();
      return 0;
    case WM_CLOSE:
      state->Deactivate();
      DestroyWindow(window);
      return 0;
    case WM_NCDESTROY:
      state->Deactivate();
      state->window = nullptr;
      SetWindowLongPtrW(window, GWLP_USERDATA, 0);
      delete holder;
      break;
    case WM_TIMER:
      if (wparam == kLoadTimer && state->timer) { state->Fail(); return 0; }
      break;
    case WM_SIZE:
      if (state->Live() && !state->Bounds()) state->Fail();
      return 0;
    case WM_DPICHANGED: {
      const auto* suggested = reinterpret_cast<RECT*>(lparam);
      if (!state->Place(HIWORD(wparam),
          MonitorFromRect(suggested, MONITOR_DEFAULTTONEAREST), false,
          suggested->left, suggested->top)) state->Fail();
      return 0;
    }
    case WM_PAINT: {
      PAINTSTRUCT paint{};
      HDC dc = BeginPaint(window, &paint);
      RECT client{};
      GetClientRect(window, &client);
      if (state->failed || state->loading) {
        SetBkMode(dc, TRANSPARENT);
        DrawTextW(dc, state->failed ? kFailed : kLoading, -1, &client,
                  DT_CENTER | DT_VCENTER | DT_SINGLELINE);
      }
      EndPaint(window, &paint);
      return 0;
    }
  }
  return DefWindowProcW(window, message, wparam, lparam);
}
