#include "../../windows/runner/installation_gate.h"
#include <windows.h>
#include <sddl.h>
#include <cassert>
#include <string>
#include <thread>
#include <vector>

namespace {
std::wstring Directory() {
  std::vector<wchar_t> path(32768);
  const DWORD length = GetModuleFileNameW(nullptr, path.data(), static_cast<DWORD>(path.size()));
  assert(length && length < path.size());
  std::wstring result(path.data(), length);
  result.resize(result.find_last_of(L"\\/") + 1);
  return result;
}
void Touch(const std::wstring& path) {
  HANDLE file = CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_NEW,
                            FILE_ATTRIBUTE_NORMAL, nullptr);
  assert(file != INVALID_HANDLE_VALUE);
  assert(CloseHandle(file));
}
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

int wmain(int argc, wchar_t** argv) {
  const std::wstring directory = Directory();
  // A real child, copied only into the test build directory, rejects the fake
  // recovery permission. It never stands in for successful signed admission.
  if (argc == 3 && std::wstring(argv[1]) == L"update-internal" &&
      std::wstring(argv[2]) == L"setup-admission") {
    Touch(directory + L"helper-invoked");
    return 1;
  }
  SetEnvironmentVariableW(L"SIDRAVIA_UPDATE_OPERATION", nullptr);
  SetEnvironmentVariableW(L"SIDRAVIA_UPDATE_TOKEN", nullptr);
  assert(InstallationAllowsStart());
  const auto seal = directory + L"sidravia.update";
  Touch(seal);
  assert(!InstallationAllowsStart());
  SetEnvironmentVariableW(L"SIDRAVIA_UPDATE_OPERATION", L"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
  SetEnvironmentVariableW(L"SIDRAVIA_UPDATE_TOKEN", L"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
  assert(!InstallationAllowsStart());
  std::vector<wchar_t> executable(32768);
  assert(GetModuleFileNameW(nullptr, executable.data(), static_cast<DWORD>(executable.size())));
  const auto helper = directory + L"sidraviactl.exe";
  assert(CopyFileW(executable.data(), helper.c_str(), TRUE));
  assert(!InstallationAllowsStart());
  assert(GetFileAttributesW((directory + L"helper-invoked").c_str()) != INVALID_FILE_ATTRIBUTES);
  assert(DeleteFileW(helper.c_str()));
  assert(DeleteFileW((directory + L"helper-invoked").c_str()));
  assert(DeleteFileW(seal.c_str()));
  SetEnvironmentVariableW(L"SIDRAVIA_UPDATE_OPERATION", nullptr);
  SetEnvironmentVariableW(L"SIDRAVIA_UPDATE_TOKEN", nullptr);
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
