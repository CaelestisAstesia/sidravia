#ifndef RUNNER_SCHOOL_ANNOUNCEMENT_WINDOW_H_
#define RUNNER_SCHOOL_ANNOUNCEMENT_WINDOW_H_

#include <windows.h>
#include <flutter/encodable_value.h>
#include <memory>

// One independent top-level browser window; all state stays on the UI thread.
class SchoolAnnouncementWindow {
 public:
  SchoolAnnouncementWindow() = default;
  ~SchoolAnnouncementWindow();
  bool Open(HWND main_window, const flutter::EncodableMap& arguments);
  bool Close();

 private:
  struct State;
  std::shared_ptr<State> state_;
  static LRESULT CALLBACK WindowProc(HWND, UINT, WPARAM, LPARAM);
};

#endif  // RUNNER_SCHOOL_ANNOUNCEMENT_WINDOW_H_
