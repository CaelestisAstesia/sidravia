#include "installation_gate.h"
#include <windows.h>
#include <sddl.h>
#include <string>
#include <vector>

// The process is visible before this check, so an installer that wins the
// race still rejects it in its final target-process check. No file is read or
// daemon launched while the installer owns this current-user mutex.
bool InstallationAllowsStart() {
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

