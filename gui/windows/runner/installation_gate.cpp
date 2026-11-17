#include "installation_gate.h"
#include <windows.h>
#include <sddl.h>
#include <string>
#include <vector>

namespace {
// Used only for an update-authorized cold recovery while the durable seal is
// present. The existing CTL validates signed state and the live recovery tree.
bool RecoveryAllowsStart(const std::wstring& directory) {
  wchar_t operation[33] = {}, token[33] = {};
  if (GetEnvironmentVariableW(L"SIDRAVIA_UPDATE_OPERATION", operation, 33) != 32 ||
      GetEnvironmentVariableW(L"SIDRAVIA_UPDATE_TOKEN", token, 33) != 32) return false;
  const std::wstring tool = directory + L"sidraviactl.exe";
  std::wstring command = L"\"" + tool + L"\" update-internal setup-admission";
  STARTUPINFOW startup = {};
  startup.cb = sizeof(startup);
  PROCESS_INFORMATION process = {};
  if (!CreateProcessW(tool.c_str(), command.data(), nullptr, nullptr, FALSE,
                      CREATE_NO_WINDOW, nullptr, directory.c_str(), &startup,
                      &process)) return false;
  CloseHandle(process.hThread);
  DWORD exit_code = 1;
  const bool allowed = WaitForSingleObject(process.hProcess, 5000) == WAIT_OBJECT_0 &&
                       GetExitCodeProcess(process.hProcess, &exit_code) && exit_code == 0;
  CloseHandle(process.hProcess);
  return allowed;
}
}  // namespace

// The process is visible before this check, so an installer that wins the
// race still rejects it in its final target-process check. No file is read or
// daemon launched while the installer owns this current-user mutex.
bool InstallationAllowsStart() {
  // Windows has already loaded this process's imported DLLs. This seal blocks
  // entering the application after an interrupted update; it is not a loader.
  std::vector<wchar_t> executable(32768);
  const DWORD length = GetModuleFileNameW(nullptr, executable.data(),
                                        static_cast<DWORD>(executable.size()));
  if (!length || length >= executable.size()) return false;
  std::wstring path(executable.data(), length);
  const auto separator = path.find_last_of(L"\\/");
  if (separator == std::wstring::npos) return false;
  path.resize(separator + 1);
  const std::wstring directory = path;
  path += L"sidravia.update";
  if (GetFileAttributesW(path.c_str()) != INVALID_FILE_ATTRIBUTES)
    return RecoveryAllowsStart(directory);
  const DWORD error = GetLastError();
  if (error != ERROR_FILE_NOT_FOUND && error != ERROR_PATH_NOT_FOUND) return false;

  HANDLE token = nullptr;
  if (!OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &token)) return false;
  DWORD size = 0;
  GetTokenInformation(token, TokenUser, nullptr, 0, &size);
  std::vector<BYTE> data(size);
  bool allowed = false;
  if (size && GetTokenInformation(token, TokenUser, data.data(), size, &size)) {
    LPWSTR sid = nullptr;
    if (ConvertSidToStringSidW(reinterpret_cast<TOKEN_USER*>(data.data())->User.Sid, &sid)) {
      const std::wstring name = L"Global\\SidraviaInstall-" + std::wstring(sid);
      LocalFree(sid);
      HANDLE gate = CreateMutexW(nullptr, FALSE, name.c_str());
      if (gate) {
        const DWORD result = WaitForSingleObject(gate, 0);
        allowed = result == WAIT_OBJECT_0 || result == WAIT_ABANDONED;
        if (allowed) ReleaseMutex(gate);
        CloseHandle(gate);
      }
    }
  }
  CloseHandle(token);
  return allowed;
}

