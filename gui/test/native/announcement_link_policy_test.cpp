#include "announcement_link_policy.h"

#include <cstdio>
#include <string>

#ifdef _WIN32
#include <windows.h>
#endif

namespace {

#ifdef _WIN32
// The shared cases use ASCII, including embedded NUL. Widen the bytes without
// interpreting URI syntax before the Windows policy receives them.
std::wstring NativeText(const std::string& text) {
  return std::wstring(text.begin(), text.end());
}
#else
const std::string& NativeText(const std::string& text) { return text; }
#endif

}  // namespace

int main() {
#ifdef _WIN32
  if (FAILED(CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED))) return 2;
#endif
  int cases = 0, failures = 0;
  const std::string page = "http://login.jlu.edu.cn/notice_win.php";
  const auto check = [&](const std::string& target, bool user, bool redirect,
      bool popup, bool ready, AnnouncementLinkAction expected) {
    ++cases;
    if (DecideAnnouncementLink(NativeText(page), NativeText(target), user, redirect, popup, ready) != expected) {
      ++failures;
      std::printf("failed case %d\n", cases);
    }
  };
  using A = AnnouncementLinkAction;
  check("https://example.org/news?id=1", true, false, false, true, A::external_browser);
  check("http://login.jlu.edu.cn/detail.php?id=1", true, false, false, true, A::external_browser);
  check("https://example.org/news", true, false, true, true, A::external_browser);
  check("https://example.org/news", false, false, true, true, A::block);
  check("https://example.org/news", true, false, true, false, A::block);
  check(page, false, false, false, false, A::in_window);
  check("https://example.org/redirect", true, true, false, true, A::in_window);
  check("https://example.org/script", false, false, false, true, A::in_window);
  check("http://login.jlu.edu.cn/notice_win.php#section", true, false, false, true, A::in_window);
  check("http://LOGIN.JLU.EDU.CN:80/notice_win.php#section", true, false, false, true, A::in_window);
  check("http://login.jlu.edu.cn/notice_win.php#section", true, false, true, true, A::external_browser);
  for (const auto* invalid : {"file:///C:/temp/test.html", "javascript:alert(1)",
      "data:text/html,test", "mailto:test@example.org", "custom://target",
      "https://user:pass@example.org/", "https://@example.org/", "https:///",
      "https://example.org/%GG", "https://example.org/%", " https://example.org/",
      "https://example.org/a b", "https://example.org/\n", "https://example.org/\"x", ""}) {
    check(invalid, true, false, false, true, A::block);
    check(invalid, true, false, true, true, A::block);
  }
  const std::string nul = page + std::string(1, '\0') + "evil";
  ++cases;
  if (DecideAnnouncementLink(NativeText(page), NativeText(nul), true, false, false, true) != A::block) ++failures;
  check("https://example.org/%22quoted%22#part", true, false, false, true, A::external_browser);
  const std::string oversized = "https://example.org/" + std::string(8192, 'a');
  ++cases;
  if (DecideAnnouncementLink(NativeText(page), NativeText(oversized), true, false, false, true) != A::block) ++failures;
  std::printf("announcement link policy: %d cases, %d failures\n", cases, failures);
#ifdef _WIN32
  CoUninitialize();
#endif
  return failures ? 1 : 0;
}
