#include "desktop_shell_internal.h"
#include "wayland_window_icon.h"
#include "announcement_window.h"
#include "../../native/frame_geometry.h"

#include <libayatana-appindicator/app-indicator.h>
#include <webkit2/webkit2.h>
#include <glib-unix.h>
#include <csignal>
#ifdef GDK_WINDOWING_WAYLAND
#include <gdk/gdkwayland.h>
#endif
#include <map>
#include <string>

using namespace sidravia_desktop;

static FlValue* main_visibility(DesktopShell* s) {
  auto* window = gtk_widget_get_window(GTK_WIDGET(s->window));
  if (!gtk_widget_get_visible(GTK_WIDGET(s->window)) || !window ||
      (gdk_window_get_state(window) & GDK_WINDOW_STATE_ICONIFIED)) return fl_value_new_bool(false);
#ifdef GDK_WINDOWING_WAYLAND
  // xdg-shell has no observable minimized state. Focus proves visibility;
  // an unfocused mapped surface may be visible OR minimized, so report unknown.
  if (GDK_IS_WAYLAND_DISPLAY(gtk_widget_get_display(GTK_WIDGET(s->window))) &&
      !gtk_window_is_active(s->window)) return fl_value_new_null();
#endif
  return fl_value_new_bool(true);
}
static void visibility(DesktopShell* s) {
  g_autoptr(FlValue) value = main_visibility(s);
  invoke(s->desktop, "visibilityChanged", value);
}
static FlValue* frame_state(DesktopShell* s) {
  auto* value = fl_value_new_map();
  fl_value_set_string_take(value, "customFrame", fl_value_new_bool(s->custom_frame && !gtk_window_get_decorated(s->window)));
  fl_value_set_string_take(value, "waylandWindowIcon", fl_value_new_string(wayland_window_icon_status(s->window_icon)));
  fl_value_set_string_take(value, "maximized", fl_value_new_bool(gtk_window_is_maximized(s->window)));
  return value;
}
static void frame_changed(DesktopShell* s) {
  if (s->closing) return;
  g_autoptr(FlValue) value = frame_state(s);
  invoke(s->window_channel, "stateChanged", value);
}
static sidravia_frame::Hit frame_hit(DesktopShell* s, double x, double y) {
  // GDK events and Flutter layout use logical coordinates on this GTK surface.
  return sidravia_frame::HitTest(x, y, gtk_widget_get_allocated_width(GTK_WIDGET(s->view)),
      gtk_widget_get_allocated_height(GTK_WIDGET(s->view)), 96,
      gtk_window_is_maximized(s->window), s->custom_frame, s->modal_blocked);
}
static bool resize_edge(sidravia_frame::Hit hit, GdkWindowEdge* edge) {
  using H = sidravia_frame::Hit;
  switch (hit) {
    case H::left: *edge = GDK_WINDOW_EDGE_WEST; return true;
    case H::right: *edge = GDK_WINDOW_EDGE_EAST; return true;
    case H::top: *edge = GDK_WINDOW_EDGE_NORTH; return true;
    case H::bottom: *edge = GDK_WINDOW_EDGE_SOUTH; return true;
    case H::top_left: *edge = GDK_WINDOW_EDGE_NORTH_WEST; return true;
    case H::top_right: *edge = GDK_WINDOW_EDGE_NORTH_EAST; return true;
    case H::bottom_left: *edge = GDK_WINDOW_EDGE_SOUTH_WEST; return true;
    case H::bottom_right: *edge = GDK_WINDOW_EDGE_SOUTH_EAST; return true;
    default: return false;
  }
}
static void frame_pressed(GtkGestureMultiPress* gesture, gint presses,
                          gdouble x, gdouble y, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  auto* sequence = gtk_gesture_single_get_current_sequence(GTK_GESTURE_SINGLE(gesture));
  const auto* event = gtk_gesture_get_last_event(GTK_GESTURE(gesture), sequence);
  if (s->closing || !event || (event->type != GDK_BUTTON_PRESS && event->type != GDK_2BUTTON_PRESS)) return;
  const auto* button = reinterpret_cast<const GdkEventButton*>(event);
  if (button->button != 1 || !button->device) return;
  const auto hit = frame_hit(s, x, y);
  auto* surface = gtk_widget_get_window(GTK_WIDGET(s->window));
  if (!surface) return;
  GdkWindowEdge edge;
  const bool resize = resize_edge(hit, &edge);
  if (!resize && hit != sidravia_frame::Hit::caption) return;
  // GTK capture phase precedes Flutter's internal event-box handler. Claim
  // only native frame gestures; buttons, content and modal barriers stay Dart-owned.
  gtk_gesture_set_sequence_state(GTK_GESTURE(gesture), sequence, GTK_EVENT_SEQUENCE_CLAIMED);
  if (resize) {
    gdk_window_begin_resize_drag_for_device(surface, edge, button->device, button->button,
        button->x_root, button->y_root, button->time);
  } else if (presses == 2) {
    if (gtk_window_is_maximized(s->window)) gtk_window_unmaximize(s->window);
    else gtk_window_maximize(s->window);
  } else if (presses == 1) {
    // Preserve the actual press/device/time required by Wayland's input serial.
    gdk_window_begin_move_drag_for_device(surface, button->device, button->button,
        button->x_root, button->y_root, button->time);
  }
}
static void restore(DesktopShell* s) {
  s->starting = false;
  if (s->startup_timeout) { g_source_remove(s->startup_timeout); s->startup_timeout = 0; }
  gtk_window_deiconify(s->window);
  gtk_window_present(s->window);  // Wayland compositor owns placement/activation policy.
  visibility(s);
}
static bool tray_available(DesktopShell* s) {
  // Ayatana reflects whether a StatusNotifier host has connected, including
  // shell restarts. Never hide the only window without a recovery path.
  gboolean connected = FALSE;
  g_object_get(s->indicator, "connected", &connected, nullptr);
  return app_indicator_get_status(s->indicator) == APP_INDICATOR_STATUS_ACTIVE && connected;
}
static void exit_requested(DesktopShell* s) {
  g_debug("Sidravia desktop exit requested");
  if (!s->exiting) invoke(s->desktop, "exitRequested");
}
static gboolean delete_window(GtkWidget*, GdkEvent*, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  if (s->close_to_tray && tray_available(s)) {
    gtk_widget_hide(GTK_WIDGET(s->window));
    visibility(s);
  } else {
    exit_requested(s);
  }
  return TRUE; // Shared exit coordinator stops/waits for daemon before destroy.
}
static void tray_connection(AppIndicator*, gboolean connected, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  if (connected && s->starting && s->frame_received && s->hidden) {
    s->starting = false;
    if (s->startup_timeout) { g_source_remove(s->startup_timeout); s->startup_timeout = 0; }
  } else if (!connected && s->frame_received && !s->starting) {
    restore(s);
    invoke(s->desktop, "trayUnavailable");
  }
}
static void tray_restore(GtkMenuItem*, gpointer data) { restore(static_cast<DesktopShell*>(data)); }
static void tray_exit(GtkMenuItem*, gpointer data) { exit_requested(static_cast<DesktopShell*>(data)); }
static void tray_primary(GtkMenuItem*, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  if (s->primary_intent) invoke(s->desktop, "trayAction", s->primary_intent);
}
static void tray_secondary(GtkMenuItem*, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  if (s->secondary_intent) invoke(s->desktop, "trayAction", s->secondary_intent);
}
static GtkWidget* menu_item(DesktopShell* s, const char* label, GCallback callback) {
  auto* item = gtk_menu_item_new_with_label(label);
  gtk_menu_shell_append(GTK_MENU_SHELL(s->menu), item);
  if (callback) g_signal_connect(item, "activate", callback, s);
  return item;
}
static void set_intent(FlValue** target, FlValue* value) {
  if (*target) fl_value_unref(*target);
  *target = value ? fl_value_ref(value) : nullptr;
}

static void desktop_call(FlMethodChannel*, FlMethodCall* call, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  const char* method = fl_method_call_get_name(call);
  auto* args = fl_method_call_get_args(call);
  if (g_str_equal(method, "initialize")) {
    g_autoptr(FlValue) value = fl_value_new_string("primary"); reply(call, value);
  } else if (g_str_equal(method, "getVisibility")) {
    g_autoptr(FlValue) value = main_visibility(s); reply(call, value);
  } else if (g_str_equal(method, "configurePreferences")) {
    s->close_to_tray = boolean(field(args, "closeToTray"));
    if (!s->configured) {
      s->hidden = boolean(field(args, "startHidden"));
      s->configured = true;
      if (s->frame_received && s->starting) desktop_shell_first_frame(s);
    }
    reply(call);
  } else if (g_str_equal(method, "restoreMain")) {
    restore(s); reply(call);
  } else if (g_str_equal(method, "beginExit")) {
    s->exiting = true; reply(call);
  } else if (g_str_equal(method, "beginUpdateExit")) {
    // The runtime lease remains held until process exit. Marking this normal
    // exit does not release file application admission or terminate the daemon.
    s->exiting = true; reply(call);
  } else if (g_str_equal(method, "cancelExit")) {
    s->exiting = false; reply(call);
  } else if (g_str_equal(method, "destroy")) {
    reply(call);
    app_indicator_set_status(s->indicator, APP_INDICATOR_STATUS_PASSIVE);
    g_application_quit(G_APPLICATION(gtk_window_get_application(s->window)));
  } else if (g_str_equal(method, "publishTray")) {
    std::string label = std::string(text(field(args, "statusTitle"))) + " · " + text(field(args, "statusDetail"));
    gtk_menu_item_set_label(GTK_MENU_ITEM(s->status), label.c_str());
    app_indicator_set_title(s->indicator, label.c_str());
    gtk_menu_item_set_label(GTK_MENU_ITEM(s->primary), text(field(args, "primaryLabel")));
    gtk_widget_set_sensitive(s->primary, boolean(field(args, "primaryEnabled")));
    gtk_menu_item_set_label(GTK_MENU_ITEM(s->secondary), text(field(args, "secondaryLabel")));
    gtk_widget_set_visible(s->secondary, *text(field(args, "secondaryLabel")) != '\0');
    gtk_widget_set_sensitive(s->secondary, boolean(field(args, "secondaryEnabled")));
    set_intent(&s->primary_intent, field(args, "primaryAction"));
    set_intent(&s->secondary_intent, field(args, "secondaryAction"));
    reply(call);
  } else {
    fl_method_call_respond_not_implemented(call, nullptr);
  }
}

static void window_call(FlMethodChannel*, FlMethodCall* call, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  const char* method = fl_method_call_get_name(call);
  if (g_str_equal(method, "configureFrame")) {
    auto* enabled = fl_method_call_get_args(call);
    if (!enabled || fl_value_get_type(enabled) != FL_VALUE_TYPE_BOOL) { fail(call, "窗口参数无效"); return; }
    s->custom_frame = fl_value_get_bool(enabled);
    gtk_window_set_decorated(s->window, !s->custom_frame);
#ifdef GDK_WINDOWING_WAYLAND
    auto* surface = gtk_widget_get_window(GTK_WIDGET(s->window));
    if (surface && GDK_IS_WAYLAND_WINDOW(surface)) {
      // GTK3's ordinary set_decorations backend is a no-op on Wayland.
      // Announce who renders decorations to KWin, not just the Gtk property.
      if (s->custom_frame) gdk_wayland_window_announce_csd(surface);
      else gdk_wayland_window_announce_ssd(surface);
    }
#endif
    s->frame_configured = true;
    g_autoptr(FlValue) state = frame_state(s);
    reply(call, state);
    if (s->frame_received && s->starting) desktop_shell_first_frame(s);
  } else if (g_str_equal(method, "setModalBlocked")) {
    auto* blocked = fl_method_call_get_args(call);
    if (!blocked || fl_value_get_type(blocked) != FL_VALUE_TYPE_BOOL) { fail(call, "模态参数无效"); return; }
    s->modal_blocked = fl_value_get_bool(blocked); reply(call);
  } else if (g_str_equal(method, "getState") || g_str_equal(method, "diagnostics")) {
    g_autoptr(FlValue) state = frame_state(s); reply(call, state);
  } else if (g_str_equal(method, "minimize")) {
    gtk_window_iconify(s->window); reply(call);
  } else if (g_str_equal(method, "close")) {
    gtk_window_close(s->window); reply(call);
  } else if (g_str_equal(method, "toggleMaximize")) {
    if (gtk_window_is_maximized(s->window)) gtk_window_unmaximize(s->window);
    else gtk_window_maximize(s->window);
    reply(call);
  } else if (g_str_equal(method, "openProjectPage")) {
    g_autoptr(GError) error = nullptr;
    if (!gtk_show_uri_on_window(s->window, "https://github.com/CaelestisAstesia/sidravia", GDK_CURRENT_TIME, &error))
      fail(call, "无法打开浏览器");
    else reply(call);
  } else if (g_str_equal(method, "setDarkMode")) {
    s->dark = boolean(fl_method_call_get_args(call));
    g_object_set(gtk_settings_get_default(), "gtk-application-prefer-dark-theme", s->dark, nullptr);
    announcement_window_set_dark(s->announcement,s->dark);

    reply(call);
  } else {
    fl_method_call_respond_not_implemented(call, nullptr);
  }
}

static void announcement_call(FlMethodChannel*, FlMethodCall* call, gpointer data) {
  auto* s = static_cast<DesktopShell*>(data);
  const auto* method = fl_method_call_get_name(call);
  if (g_str_equal(method,"close")) { announcement_window_close(s->announcement); reply(call); return; }
  if (!g_str_equal(method,"open")) { fl_method_call_respond_not_implemented(call,nullptr); return; }
  auto* args = fl_method_call_get_args(call);
  auto* width = field(args,"clientWidth"); auto* height = field(args,"clientHeight");
  if (!width || !height || fl_value_get_type(width) != FL_VALUE_TYPE_INT || fl_value_get_type(height) != FL_VALUE_TYPE_INT ||
      fl_value_get_int(width) < 1 || fl_value_get_int(width) > 4096 || fl_value_get_int(height) < 1 || fl_value_get_int(height) > 4096 ||
      !announcement_window_open(s->announcement,text(field(args,"url")),fl_value_get_int(width),fl_value_get_int(height))) {
    fail(call,"无效公告参数"); return;
  }
  g_autoptr(FlValue) value = fl_value_new_string("opened"); reply(call,value);
}

static FlMethodChannel* channel(FlView* view, const char* name, FlMethodChannelMethodCallHandler handler, DesktopShell* s) {
  g_autoptr(FlStandardMethodCodec) codec = fl_standard_method_codec_new();
  auto* result = fl_method_channel_new(fl_engine_get_binary_messenger(fl_view_get_engine(view)), name, FL_METHOD_CODEC(codec));
  fl_method_channel_set_method_call_handler(result, handler, s, nullptr);
  return result;
}
DesktopShell* desktop_shell_new(GtkWindow* window, FlView* view) {
  auto* s = new DesktopShell(); s->window = GTK_WINDOW(g_object_ref(window));
  s->view = FL_VIEW(g_object_ref(view));
  g_autofree gchar* executable = g_file_read_link("/proc/self/exe", nullptr);
  g_autofree gchar* directory = g_path_get_dirname(executable);
  s->icon = g_build_filename(directory, "data", "icons", "sidravia.png", nullptr);
  gtk_window_set_icon_from_file(window, s->icon, nullptr);
  s->window_icon = wayland_window_icon_new(window, s->icon);
  s->announcement = announcement_window_new(window,s->icon);
  s->desktop = channel(view, "sidravia/desktop", desktop_call, s);
  s->termination_source = g_unix_signal_add(SIGTERM, +[](gpointer data) -> gboolean {
    auto* shell = static_cast<DesktopShell*>(data);
    if (!shell->exiting) invoke(shell->desktop, "maintenanceExitRequested");
    return G_SOURCE_CONTINUE;
  }, s);
  s->window_channel = channel(view, "sidravia/window", window_call, s);
  s->announcements = channel(view, "sidravia/announcements", announcement_call, s);
  s->notifications = channel(view, "sidravia/system-notifications", notification_call, s);
  s->menu = gtk_menu_new(); g_object_ref_sink(s->menu);
  s->status = menu_item(s, "Sidravia", nullptr); gtk_widget_set_sensitive(s->status, FALSE);
  menu_item(s, "打开主界面", G_CALLBACK(tray_restore));
  s->primary = menu_item(s, "连接", G_CALLBACK(tray_primary));
  s->secondary = menu_item(s, "", G_CALLBACK(tray_secondary));
  menu_item(s, "退出", G_CALLBACK(tray_exit));
  gtk_widget_show_all(s->menu);
  s->indicator = APP_INDICATOR(g_object_new(APP_INDICATOR_TYPE, "id", g_application_get_application_id(G_APPLICATION(gtk_window_get_application(window))), "icon-name", s->icon, "category", "ApplicationStatus", nullptr));
  app_indicator_set_menu(s->indicator, GTK_MENU(s->menu));
  app_indicator_set_secondary_activate_target(s->indicator, s->menu);
  g_signal_connect(s->indicator, "connection-changed", G_CALLBACK(tray_connection), s);
  app_indicator_set_status(s->indicator, APP_INDICATOR_STATUS_ACTIVE);
  g_signal_connect(window, "delete-event", G_CALLBACK(delete_window), s);
  s->frame_gesture = gtk_gesture_multi_press_new(GTK_WIDGET(view));
  gtk_gesture_single_set_button(GTK_GESTURE_SINGLE(s->frame_gesture), 1);
  gtk_event_controller_set_propagation_phase(GTK_EVENT_CONTROLLER(s->frame_gesture), GTK_PHASE_CAPTURE);
  g_signal_connect(s->frame_gesture, "pressed", G_CALLBACK(frame_pressed), s);
  for (const char* signal : {"map-event", "unmap-event", "window-state-event"}) {
    g_signal_connect_after(window, signal, G_CALLBACK(+[](GtkWidget*, GdkEvent*, gpointer data) -> gboolean {
      auto* s = static_cast<DesktopShell*>(data);
      if (!s->closing) { visibility(s); frame_changed(s); }
      return FALSE;
    }), s);
  }
  g_signal_connect(window, "notify::is-maximized", G_CALLBACK(+[](GObject*, GParamSpec*, gpointer data) {
    frame_changed(static_cast<DesktopShell*>(data));
  }), s);
  g_signal_connect(window, "notify::is-active", G_CALLBACK(+[](GObject*, GParamSpec*, gpointer data) {
    visibility(static_cast<DesktopShell*>(data));
  }), s);
  s->bus = g_bus_get_sync(G_BUS_TYPE_SESSION, nullptr, nullptr);
  if (s->bus) s->notification_subscription = g_dbus_connection_signal_subscribe(s->bus, "org.freedesktop.Notifications", "org.freedesktop.Notifications", nullptr, "/org/freedesktop/Notifications", nullptr, G_DBUS_SIGNAL_FLAGS_NONE, notification_signal, s, nullptr);
  return s;
}
void desktop_shell_restore(DesktopShell* s) { restore(s); }
void desktop_shell_first_frame(DesktopShell* s) {
  s->frame_received = true;
  if (!s->starting || !s->configured || !s->frame_configured) return;
  if (!s->hidden) { restore(s); return; }
  if (tray_available(s)) { s->starting = false; return; }
  // Give the session tray a bounded opportunity to register before falling
  // back to a visible window. Never leave a hidden, unreachable application.
  s->startup_timeout = g_timeout_add(1000, +[](gpointer data) -> gboolean {
    auto* s = static_cast<DesktopShell*>(data);
    s->startup_timeout = 0;
    restore(s);
    invoke(s->desktop, "trayUnavailable");
    return G_SOURCE_REMOVE;
  }, s);
}
void desktop_shell_free(DesktopShell* s) {
  if (!s->closing) {
    s->closing = true;
    g_cancellable_cancel(s->cancel);
  }
  if (s->pending) return; // Cancelled async calls retain resources until joined.
  if (s->startup_timeout) g_source_remove(s->startup_timeout);
  if (s->termination_source) g_source_remove(s->termination_source);
  g_clear_object(&s->cancel);
  announcement_window_free(s->announcement);
  for (auto& entry : s->notices) dismiss_notice(s, entry.second);
  if (s->bus && s->notification_subscription) g_dbus_connection_signal_unsubscribe(s->bus, s->notification_subscription);
  g_clear_object(&s->bus);
  g_signal_handlers_disconnect_by_data(s->window, s);
  g_signal_handlers_disconnect_by_data(s->frame_gesture, s);
  g_clear_object(&s->frame_gesture);
  g_signal_handlers_disconnect_by_data(s->indicator, s);
  app_indicator_set_status(s->indicator, APP_INDICATOR_STATUS_PASSIVE);
  g_clear_object(&s->indicator);
  for (auto* c : {s->desktop, s->window_channel, s->announcements, s->notifications}) {
    fl_method_channel_set_method_call_handler(c, nullptr, nullptr, nullptr); g_object_unref(c);
  }
  set_intent(&s->primary_intent, nullptr); set_intent(&s->secondary_intent, nullptr);
  gtk_widget_destroy(s->menu); g_object_unref(s->menu);
  wayland_window_icon_free(s->window_icon);
  g_free(s->icon);
  g_object_unref(s->view);
  g_object_unref(s->window);
  delete s;
}
