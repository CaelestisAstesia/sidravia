#ifndef RUNNER_SYSTEM_NOTIFICATION_H_
#define RUNNER_SYSTEM_NOTIFICATION_H_

#include <windows.h>
#include <flutter/encodable_value.h>
#include <cstdint>
#include <memory>
#include <optional>
#include <string>
#include <utility>

// Platform-thread owner; worker activation callbacks only post scalar messages.
class SystemNotification {
 public:
  static constexpr UINT kCallbackMessage = WM_APP + 20;
  explicit SystemNotification(HWND window);
  ~SystemNotification();
  struct Result {
    std::string status;
    std::optional<int32_t> error_code;
  };
  struct Callback {
    std::string method, token, category;
    std::optional<int32_t> error_code;
  };
  Result Show(const flutter::EncodableMap& arguments);
  bool Dismiss(const std::string& category) noexcept;
  bool Dispose() noexcept;
  std::optional<Callback> TakeCallback(
      WPARAM action, LPARAM generation) noexcept;

 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

#endif  // RUNNER_SYSTEM_NOTIFICATION_H_
