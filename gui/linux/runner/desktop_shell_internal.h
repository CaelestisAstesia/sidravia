#ifndef SIDRAVIA_DESKTOP_SHELL_INTERNAL_H_
#define SIDRAVIA_DESKTOP_SHELL_INTERNAL_H_

#include "desktop_shell.h"
#include "wayland_window_icon.h"
#include "announcement_window.h"

#include <libayatana-appindicator/app-indicator.h>
#include <map>
#include <string>

struct Notice {
  guint id = 0;
  guint64 generation = 0;
  std::string token;
};
struct DesktopShell {
  GtkWindow* window;
  FlView* view;
  GtkGesture* frame_gesture;
  bool custom_frame = false, frame_configured = false, modal_blocked = false;
  gchar* icon = nullptr;
  WaylandWindowIcon* window_icon = nullptr;
  AnnouncementWindow* announcement = nullptr;
  FlMethodChannel *desktop, *window_channel, *announcements, *notifications;
  AppIndicator* indicator;
  GtkWidget *menu, *status, *primary, *secondary;
  FlValue *primary_intent = nullptr, *secondary_intent = nullptr;
  GDBusConnection* bus = nullptr;
  guint notification_subscription = 0;
  bool dark = false, starting = true, frame_received = false;
  guint startup_timeout = 0;
  guint termination_source = 0;
  bool close_to_tray = false, hidden = false, exiting = false, configured = false;
  std::map<std::string, Notice> notices;
  GCancellable* cancel = g_cancellable_new();
  unsigned pending = 0;
  bool closing = false;
};

namespace sidravia_desktop {

inline FlValue* field(FlValue* args, const char* key) {
  return args && fl_value_get_type(args) == FL_VALUE_TYPE_MAP
      ? fl_value_lookup_string(args, key) : nullptr;
}
inline const char* text(FlValue* value) {
  return value && fl_value_get_type(value) == FL_VALUE_TYPE_STRING
      ? fl_value_get_string(value) : "";
}
inline bool boolean(FlValue* value) {
  return value && fl_value_get_type(value) == FL_VALUE_TYPE_BOOL && fl_value_get_bool(value);
}
inline void reply(FlMethodCall* call, FlValue* value = nullptr) {
  g_autoptr(FlMethodResponse) response = FL_METHOD_RESPONSE(fl_method_success_response_new(value));
  fl_method_call_respond(call, response, nullptr);
}
inline void fail(FlMethodCall* call, const char* message) {
  g_autoptr(FlMethodResponse) response = FL_METHOD_RESPONSE(
      fl_method_error_response_new("desktop_unavailable", message, nullptr));
  fl_method_call_respond(call, response, nullptr);
}
inline void invoke(FlMethodChannel* channel, const char* method, FlValue* value = nullptr) {
  fl_method_channel_invoke_method(channel, method, value, nullptr,
    [](GObject* source, GAsyncResult* result, gpointer) {
      g_autoptr(GError) error = nullptr;
      g_autoptr(FlMethodResponse) response = fl_method_channel_invoke_method_finish(FL_METHOD_CHANNEL(source), result, &error);
      if (error || (response && FL_IS_METHOD_ERROR_RESPONSE(response))) g_debug("Sidravia desktop event was not acknowledged");
    }, nullptr);
}
void dismiss_notice(DesktopShell* shell, Notice& notice);
void notification_signal(GDBusConnection*, const gchar*, const gchar*,
                         const gchar*, const gchar*, GVariant*, gpointer);
void notification_call(FlMethodChannel*, FlMethodCall*, gpointer);

}  // namespace sidravia_desktop

#endif  // SIDRAVIA_DESKTOP_SHELL_INTERNAL_H_
