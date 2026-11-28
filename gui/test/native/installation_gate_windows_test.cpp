#include "../../windows/runner/installation_gate.h"
#include <windows.h>
#include <sddl.h>
#include <cassert>
#include <string>
#include <thread>
#include <vector>

namespace {
std::wstring MutexName() {
  HANDLE token = nullptr;
  assert(OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &token));
  DWORD size = 0;
  GetTokenInformation(token, TokenUser, nullptr, 0, &size);
  std::vector<BYTE> bytes(size);
  assert(GetTokenInformation(token, TokenUser, bytes.data(), size, &size));
  LPWSTR sid = nullptr;
  assert(ConvertSidToStringSidW(reinterpret_cast<TOKEN_USER*>(bytes.data())->User.Sid, &sid));
  const std::wstring result = L"Global\\SidraviaInstall-" + std::wstring(sid);
  LocalFree(sid);
  CloseHandle(token);
  return result;
}
}  // namespace

int wmain() {
  assert(InstallationAllowsStart());
  HANDLE held = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  HANDLE release = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  assert(held && release);
  const auto name = MutexName();
  std::thread installer([&]() {
    HANDLE mutex = CreateMutexW(nullptr, FALSE, name.c_str());
    assert(mutex);
    const DWORD result = WaitForSingleObject(mutex, 0);
    assert(result == WAIT_OBJECT_0 || result == WAIT_ABANDONED);
    SetEvent(held);
    assert(WaitForSingleObject(release, 5000) == WAIT_OBJECT_0);
    ReleaseMutex(mutex);
    CloseHandle(mutex);
  });
  assert(WaitForSingleObject(held, 5000) == WAIT_OBJECT_0);
  assert(!InstallationAllowsStart());
  SetEvent(release);
  installer.join();
  CloseHandle(held);
  CloseHandle(release);
  assert(InstallationAllowsStart());
  return 0;
}
