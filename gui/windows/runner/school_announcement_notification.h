#ifndef RUNNER_SCHOOL_ANNOUNCEMENT_NOTIFICATION_H_
#define RUNNER_SCHOOL_ANNOUNCEMENT_NOTIFICATION_H_

#include <windows.h>
#include <flutter/encodable_value.h>
#include <memory>
#include <optional>
#include <string>
#include <utility>

// Platform-thread owner; worker activation callbacks only post scalar messages.
class SchoolAnnouncementNotification {
 public:
  static constexpr UINT kCallbackMessage = WM_APP + 20;
  explicit SchoolAnnouncementNotification(HWND window);
  ~SchoolAnnouncementNotification();
  std::string Show(const flutter::EncodableMap& arguments);
  bool Dismiss() noexcept;
  bool Dispose() noexcept;
  std::optional<std::pair<std::string, std::string>> TakeCallback(
      WPARAM action, LPARAM generation) noexcept;

 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

#endif  // RUNNER_SCHOOL_ANNOUNCEMENT_NOTIFICATION_H_
