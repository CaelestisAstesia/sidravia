#include "announcement_link_policy.h"

#include <windows.h>
#include <cstdio>

int main() {
  if (FAILED(CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED))) return 2;
  int cases = 0, failures = 0;
  const std::wstring page = L"http://login.jlu.edu.cn/notice_win.php";
  const auto check = [&](const wchar_t* target, bool user, bool redirect,
      bool popup, bool ready, AnnouncementLinkAction expected) {
    ++cases;
    if (DecideAnnouncementLink(page, target, user, redirect, popup, ready) != expected) {
      ++failures;
      std::printf("failed case %d\n", cases);
    }
  };
  using A = AnnouncementLinkAction;
  check(L"https://example.org/news?id=1", true, false, false, true, A::external_browser);
  check(L"http://login.jlu.edu.cn/detail.php?id=1", true, false, false, true, A::external_browser);
  check(L"https://example.org/news", true, false, true, true, A::external_browser);
  check(L"https://example.org/news", false, false, true, true, A::block);
  check(L"https://example.org/news", true, false, true, false, A::block);
  check(page.c_str(), false, false, false, false, A::in_window);
  check(L"https://example.org/redirect", true, true, false, true, A::in_window);
  check(L"https://example.org/script", false, false, false, true, A::in_window);
  check(L"http://login.jlu.edu.cn/notice_win.php#section", true, false, false, true, A::in_window);
  check(L"http://LOGIN.JLU.EDU.CN:80/notice_win.php#section", true, false, false, true, A::in_window);
  check(L"http://login.jlu.edu.cn/notice_win.php#section", true, false, true, true, A::external_browser);
  for (const auto* invalid : {L"file:///C:/temp/test.html", L"javascript:alert(1)",
      L"data:text/html,test", L"mailto:test@example.org", L"custom://target",
      L"https://user:pass@example.org/", L"https://@example.org/", L"https:///",
      L"https://example.org/%GG", L"https://example.org/%", L" https://example.org/",
      L"https://example.org/a b", L"https://example.org/\n", L"https://example.org/\"x", L""}) {
    check(invalid, true, false, false, true, A::block);
    check(invalid, true, false, true, true, A::block);
  }
  const std::wstring nul = page + std::wstring(1, L'\0') + L"evil";
  ++cases;
  if (DecideAnnouncementLink(page, nul, true, false, false, true) != A::block) ++failures;
  check(L"https://example.org/%22quoted%22#part", true, false, false, true, A::external_browser);
  const std::wstring oversized = L"https://example.org/" + std::wstring(8192, L'a');
  ++cases;
  if (DecideAnnouncementLink(page, oversized, true, false, false, true) != A::block) ++failures;
  std::printf("announcement link policy: %d cases, %d failures\n", cases, failures);
  CoUninitialize();
  return failures ? 1 : 0;
}
