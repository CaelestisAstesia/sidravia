#include "flutter_window.h"

#include <flutter/encodable_value.h>

void FlutterWindow::ConfigureContentChannels() {
  school_announcement_window_ = std::make_unique<SchoolAnnouncementWindow>();
  announcement_channel_ =
      std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
          flutter_controller_->engine()->messenger(), "sidravia/announcements",
          &flutter::StandardMethodCodec::GetInstance());
  announcement_channel_->SetMethodCallHandler(
      [this](const auto& call, auto result) {
        if (call.method_name() == "open") {
          const auto* arguments =
              std::get_if<flutter::EncodableMap>(call.arguments());
          if (!arguments || !school_announcement_window_ ||
              !school_announcement_window_->Open(GetHandle(), *arguments)) {
            result->Error("announcement_unavailable", "Announcement unavailable.");
            return;
          }
          result->Success(flutter::EncodableValue("opened"));
          return;
        }
        if (call.method_name() == "close") {
          if (school_announcement_window_ && !school_announcement_window_->Close()) {
            result->Error("announcement_close_failed", "Announcement close failed.");
            return;
          }
          result->Success();
          return;
        }
        result->NotImplemented();
      });
  system_notification_ =
      std::make_unique<SystemNotification>(GetHandle());
  system_notifications_channel_ =
      std::make_unique<flutter::MethodChannel<flutter::EncodableValue>>(
          flutter_controller_->engine()->messenger(),
          "sidravia/system-notifications",
          &flutter::StandardMethodCodec::GetInstance());
  system_notifications_channel_->SetMethodCallHandler(
      [this](const auto& call, auto result) {
        if (call.method_name() == "show") {
          const auto* arguments = call.arguments()
              ? std::get_if<flutter::EncodableMap>(call.arguments()) : nullptr;
          const auto submission = arguments && system_notification_
              ? system_notification_->Show(*arguments)
              : SystemNotification::Result{"failed", std::nullopt};
          if (submission.status == "submitted") {
            result->Success(flutter::EncodableValue(submission.status));
          } else {
            flutter::EncodableMap details;
            if (submission.error_code) {
              details[flutter::EncodableValue("error_code")] =
                  flutter::EncodableValue(*submission.error_code);
            }
            result->Error("notification_failed", "Notification operation failed.",
                          flutter::EncodableValue(details));
          }
          return;
        }
        if (call.method_name() == "dismiss") {
          const auto* args = call.arguments() ? std::get_if<flutter::EncodableMap>(call.arguments()) : nullptr;
          const std::string* category = nullptr;
          if (args && args->size() == 1) {
            auto it = args->find(flutter::EncodableValue("category"));
            if (it != args->end()) category = std::get_if<std::string>(&it->second);
          }
          if (category && system_notification_ && system_notification_->Dismiss(*category)) result->Success();
          else result->Error("notification_failed", "Notification operation failed.");
          return;
        }
        if (call.method_name() == "dispose") {
          if (!system_notification_ || system_notification_->Dispose()) result->Success();
          else result->Error("notification_failed", "Notification operation failed.");
          return;
        }
        result->NotImplemented();
      });
}
