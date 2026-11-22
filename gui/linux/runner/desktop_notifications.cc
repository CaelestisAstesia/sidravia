#include "desktop_shell_internal.h"

#include <cstring>

namespace sidravia_desktop {

void dismiss_notice(DesktopShell* s, Notice& notice) {
  ++notice.generation;
  if (s->bus && notice.id) {
    g_dbus_connection_call(s->bus, "org.freedesktop.Notifications", "/org/freedesktop/Notifications", "org.freedesktop.Notifications", "CloseNotification",
      g_variant_new("(u)", notice.id), nullptr, G_DBUS_CALL_FLAGS_NONE, 1000, nullptr, nullptr, nullptr);
  }
  notice.id = 0; notice.token.clear();
}
void notification_signal(GDBusConnection*, const gchar*, const gchar*, const gchar*, const gchar* signal, GVariant* args, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  guint id; const gchar* action = nullptr;
  if (g_str_equal(signal, "ActionInvoked")) g_variant_get(args, "(u&s)", &id, &action);
  else if (g_str_equal(signal, "NotificationClosed")) { guint reason; g_variant_get(args, "(uu)", &id, &reason); }
  else return;
  for (auto& entry : s->notices) {
    if (entry.second.id != id) continue;
    if (action && g_str_equal(action, "default")) {
      g_autoptr(FlValue) value = fl_value_new_map();
      fl_value_set_string_take(value, "category", fl_value_new_string(entry.first.c_str()));
      fl_value_set_string_take(value, "token", fl_value_new_string(entry.second.token.c_str()));
      invoke(s->notifications, "clicked", value);
    }
    entry.second.id = 0; entry.second.token.clear();
    break;
  }
}
void notification_call(FlMethodChannel*, FlMethodCall* call, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  const char* method = fl_method_call_get_name(call);
  auto* args = fl_method_call_get_args(call);
  if (g_str_equal(method, "dispose")) {
    for (auto& entry : s->notices) dismiss_notice(s, entry.second);
    reply(call); return;
  }
  std::string category = text(field(args, "category"));
  if (category != "connection" && category != "announcement") { fail(call, "无效通知类别"); return; }
  auto& notice = s->notices[category];
  if (g_str_equal(method, "dismiss")) { dismiss_notice(s, notice); reply(call); return; }
  if (!g_str_equal(method, "show")) { fl_method_call_respond_not_implemented(call, nullptr); return; }
  if (!s->bus) { g_autoptr(FlValue) value = fl_value_new_string("unsupported"); reply(call, value); return; }
  const char* token = text(field(args, "token"));
  if (strlen(token) != 32 || !*text(field(args, "title")) || !*text(field(args, "body"))) { fail(call, "无效通知参数"); return; }
  GVariantBuilder actions, hints;
  g_variant_builder_init(&actions, G_VARIANT_TYPE("as"));
  g_variant_builder_add(&actions, "s", "default"); g_variant_builder_add(&actions, "s", "打开 Sidravia");
  g_variant_builder_init(&hints, G_VARIANT_TYPE("a{sv}"));
  g_variant_builder_add(&hints, "{sv}", "desktop-entry", g_variant_new_string(APPLICATION_ID));
  // Async IPC keeps an unavailable notification server off the GTK input path.
  struct PendingNotice {
    DesktopShell* shell;
    FlMethodCall* call;
    std::string category, token;
    guint64 generation;
  };
  auto* pending = new PendingNotice{s, FL_METHOD_CALL(g_object_ref(call)), category, token, ++notice.generation};
  ++s->pending;
  g_dbus_connection_call(s->bus, "org.freedesktop.Notifications", "/org/freedesktop/Notifications", "org.freedesktop.Notifications", "Notify",
    g_variant_new("(susssasa{sv}i)", "Sidravia", notice.id, s->icon, text(field(args, "title")), text(field(args, "body")), &actions, &hints, -1),
    G_VARIANT_TYPE("(u)"), G_DBUS_CALL_FLAGS_NONE, 1500, s->cancel,
    [](GObject* source, GAsyncResult* result, gpointer data) {
      auto* p = static_cast<PendingNotice*>(data);
      auto* s = p->shell;
      g_autoptr(GError) error = nullptr;
      g_autoptr(GVariant) value = g_dbus_connection_call_finish(G_DBUS_CONNECTION(source), result, &error);
      auto& current = s->notices[p->category];
      guint id = 0;
      if (value) g_variant_get(value, "(u)", &id);
      if (s->closing || current.generation != p->generation) {
        if (id && id != current.id) {
          Notice stale; stale.id = id; dismiss_notice(s, stale);
        }
        fail(p->call, "通知请求已取消");
      } else if (!value) {
        fail(p->call, "通知服务不可用");
      } else {
        current.id = id; current.token = p->token;
        g_autoptr(FlValue) submitted = fl_value_new_string("submitted");
        reply(p->call, submitted);
      }
      g_object_unref(p->call); delete p;
      --s->pending;
      if (s->closing && !s->pending) desktop_shell_free(s);
    }, pending);

}

}  // namespace sidravia_desktop
