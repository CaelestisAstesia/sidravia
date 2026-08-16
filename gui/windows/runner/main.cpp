#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <windows.h>

#include <vector>

#include "flutter_window.h"
#include "utils.h"

namespace {

constexpr const wchar_t kSingleInstanceMutexName[] =
    L"Local\\Sidravia.Gui.DesktopPresence";
constexpr const wchar_t kActivationMessageName[] = L"Sidravia.Gui.Activate";

HANDLE g_single_instance_mutex = nullptr;

// Returns true when this process owns the one per-user desktop presence.
// A second instance signals the existing window before returning false.
bool AcquireDesktopPresence() {
  g_single_instance_mutex =
      ::CreateMutexW(nullptr, TRUE, kSingleInstanceMutexName);
  if (g_single_instance_mutex != nullptr &&
      ::GetLastError() == ERROR_ALREADY_EXISTS) {
    ::CloseHandle(g_single_instance_mutex);
    g_single_instance_mutex = nullptr;
    return false;
  }
  return true;
}

void ActivateExistingWindow() {
  const UINT activate = ::RegisterWindowMessageW(kActivationMessageName);
  HWND target = ::FindWindowW(L"FLUTTER_RUNNER_WIN32_WINDOW", L"Sidravia");
  if (target != nullptr) {
    ::PostMessageW(target, activate, 0, 0);
  } else {
    ::PostMessageW(HWND_BROADCAST, activate, 0, 0);
  }
}

void ReleaseDesktopPresence() {
  if (g_single_instance_mutex != nullptr) {
    ::CloseHandle(g_single_instance_mutex);
    g_single_instance_mutex = nullptr;
  }
}

}  // namespace

int APIENTRY wWinMain(_In_ HINSTANCE instance, _In_opt_ HINSTANCE prev,
                      _In_ wchar_t *command_line, _In_ int show_command) {
  if (!AcquireDesktopPresence()) {
    ActivateExistingWindow();
    return EXIT_SUCCESS;
  }

  // Attach to console when present (e.g., 'flutter run') or create a
  // new console when running with a debugger.
  if (!::AttachConsole(ATTACH_PARENT_PROCESS) && ::IsDebuggerPresent()) {
    CreateAndAttachConsole();
  }

  // Initialize COM, so that it is available for use in the library and/or
  // plugins.
  ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

  flutter::DartProject project(L"data");

  std::vector<std::string> command_line_arguments =
      GetCommandLineArguments();

  project.set_dart_entrypoint_arguments(std::move(command_line_arguments));

  FlutterWindow window(project);
  Win32Window::Point origin(10, 10);
  Win32Window::Size size(1280, 720);
  if (!window.Create(L"Sidravia", origin, size)) {
    ReleaseDesktopPresence();
    return EXIT_FAILURE;
  }
  window.SetQuitOnClose(true);

  ::MSG msg;
  while (::GetMessage(&msg, nullptr, 0, 0)) {
    ::TranslateMessage(&msg);
    ::DispatchMessage(&msg);
  }

  ::CoUninitialize();
  ReleaseDesktopPresence();
  return EXIT_SUCCESS;
}
