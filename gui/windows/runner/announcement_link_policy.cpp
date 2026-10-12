#include "announcement_link_policy.h"

#include <windows.h>
#include <urlmon.h>
#include <shlwapi.h>
#include <wrl.h>
#include <algorithm>

namespace {
using Microsoft::WRL::ComPtr;

ComPtr<IUri> ParseHttpLink(const std::wstring& text) {
  ComPtr<IUri> uri;
  if (text.empty() || text.size() > 8192 ||
      std::any_of(text.begin(), text.end(), [](wchar_t c) {
        return c <= 0x20 || (c >= 0x7f && c <= 0x9f) || c == 0xa0 ||
            c == 0x1680 || (c >= 0x2000 && c <= 0x200a) || c == 0x2028 ||
            c == 0x2029 || c == 0x202f || c == 0x205f || c == 0x3000 || c == L'"';
      })) return uri;
  const auto separator = text.find(L"://");
  if (separator == std::wstring::npos) return uri;
  const auto end = text.find_first_of(L"/?#", separator + 3);
  if (text.substr(separator + 3, end == std::wstring::npos
      ? end : end - separator - 3).find(L'@') != std::wstring::npos) return uri;
  for (size_t i = 0; i < text.size(); ++i) {
    if (text[i] != L'%') continue;
    const auto hex = [](wchar_t c) {
      return (c >= L'0' && c <= L'9') || (c >= L'a' && c <= L'f') ||
          (c >= L'A' && c <= L'F');
    };
    if (i + 2 >= text.size() || !hex(text[i + 1]) || !hex(text[i + 2])) return uri;
  }
  if (FAILED(CreateUri(text.c_str(), Uri_CREATE_CANONICALIZE, 0, &uri))) return {};
  DWORD scheme = 0;
  BSTR host = nullptr, userinfo = nullptr;
  const HRESULT host_hr = uri->GetHost(&host);
  const HRESULT user_hr = uri->GetUserInfo(&userinfo);
  const bool valid = SUCCEEDED(uri->GetScheme(&scheme)) &&
      (scheme == URL_SCHEME_HTTP || scheme == URL_SCHEME_HTTPS) &&
      SUCCEEDED(host_hr) && host && SysStringLen(host) &&
      SUCCEEDED(user_hr) && (!userinfo || !SysStringLen(userinfo));
  SysFreeString(host);
  SysFreeString(userinfo);
  return valid ? uri : ComPtr<IUri>{};
}

std::wstring DocumentUri(IUri* uri) {
  BSTR absolute = nullptr;
  if (!uri) return {};
  const HRESULT read = uri->GetAbsoluteUri(&absolute);
  if (FAILED(read) || !absolute) { SysFreeString(absolute); return {}; }
  std::wstring result(absolute, SysStringLen(absolute));
  SysFreeString(absolute);
  const auto fragment = result.find(L'#');
  if (fragment != std::wstring::npos) result.resize(fragment);
  return result;
}
}  // namespace

AnnouncementLinkAction DecideAnnouncementLink(
    const std::wstring& document, const std::wstring& target,
    bool user_initiated, bool redirected, bool new_window, bool document_ready) {
  const auto uri = ParseHttpLink(target);
  if (!uri) return AnnouncementLinkAction::block;
  if (new_window && (!user_initiated || !document_ready)) {
    return AnnouncementLinkAction::block;
  }
  if (!user_initiated || redirected || !document_ready) {
    return AnnouncementLinkAction::in_window;
  }
  const auto current = ParseHttpLink(document);
  if (!new_window && current && DocumentUri(current.Get()) == DocumentUri(uri.Get())) {
    return AnnouncementLinkAction::in_window;
  }
  return AnnouncementLinkAction::external_browser;
}
