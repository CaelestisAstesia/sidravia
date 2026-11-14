#include "announcement_window.h"
#include "announcement_link_policy.h"
#include "wayland_window_icon.h"
#include <webkit2/webkit2.h>
#include <deque>
#include <string>

struct AnnouncementWindow {
  GtkWindow* parent;
  std::string icon, document;
  GtkWidget *window = nullptr, *stack = nullptr, *label = nullptr;
  WebKitWebView* web = nullptr;
  WaylandWindowIcon* window_icon = nullptr;
  enum class Phase { closed, loading, ready, failed } phase = Phase::closed;
  bool dark = false;
  guint timer = 0, external = 0, release = 0;
  std::deque<std::string> links;
};
namespace {
void CancelTimer(AnnouncementWindow* s) {
  if (s->timer) { g_source_remove(s->timer); s->timer = 0; }
}
void ReleaseWeb(AnnouncementWindow* s) {
  if (!s->web) return;
  auto* web = s->web; s->web = nullptr;
  g_signal_handlers_disconnect_by_data(web,s);
  webkit_web_view_stop_loading(web);
  gtk_widget_destroy(GTK_WIDGET(web));
}
void Fail(AnnouncementWindow* s) {
  if (s->phase == AnnouncementWindow::Phase::closed || s->phase == AnnouncementWindow::Phase::failed) return;
  s->phase = AnnouncementWindow::Phase::failed;
  CancelTimer(s);
  if (s->external) { g_source_remove(s->external); s->external = 0; }
  s->links.clear();
  gtk_label_set_text(GTK_LABEL(s->label), "公告加载失败");
  gtk_stack_set_visible_child(GTK_STACK(s->stack), s->label);
  // WebKit still owns the emission stack (notably process termination). Do
  // not destroy its view from that callback; join disposal on the next turn.
  s->release = g_idle_add([](gpointer data) -> gboolean {
    auto* s = static_cast<AnnouncementWindow*>(data); s->release = 0;
    ReleaseWeb(s); return G_SOURCE_REMOVE;
  },s);
}
void StartLoading(AnnouncementWindow* s) {
  if (s->phase == AnnouncementWindow::Phase::loading) return; // Redirects retain the original deadline.
  s->phase = AnnouncementWindow::Phase::loading;
  gtk_label_set_text(GTK_LABEL(s->label), "正在加载公告…");
  gtk_stack_set_visible_child(GTK_STACK(s->stack),s->label);
  CancelTimer(s);
  s->timer = g_timeout_add(30000, [](gpointer data) -> gboolean {
    auto* s = static_cast<AnnouncementWindow*>(data); s->timer = 0; Fail(s); return G_SOURCE_REMOVE;
  },s);
}
void QueueLink(AnnouncementWindow* s, const char* uri) {
  s->links.emplace_back(uri);
  if (s->external) return;
  // Dispatch outside WebKit callbacks. Closing/reopening cancels this source.
  s->external = g_idle_add([](gpointer data) -> gboolean {
    auto* s = static_cast<AnnouncementWindow*>(data); s->external = 0;
    while (!s->links.empty() && s->phase == AnnouncementWindow::Phase::ready) {
      const auto link = s->links.front(); s->links.pop_front();
      g_autoptr(GError) error = nullptr;
      if (!gtk_show_uri_on_window(GTK_WINDOW(s->window),link.c_str(),GDK_CURRENT_TIME,&error)) {
        auto* dialog = gtk_message_dialog_new(GTK_WINDOW(s->window),GTK_DIALOG_DESTROY_WITH_PARENT,
            GTK_MESSAGE_WARNING,GTK_BUTTONS_CLOSE,"%s","无法打开链接，请检查默认浏览器设置");
        g_signal_connect_swapped(dialog,"response",G_CALLBACK(gtk_widget_destroy),dialog);
        gtk_widget_show(dialog);
      }
    }
    return G_SOURCE_REMOVE;
  },s);
}
gboolean Policy(WebKitWebView*, WebKitPolicyDecision* decision, WebKitPolicyDecisionType type, gpointer data) {
  auto* s = static_cast<AnnouncementWindow*>(data);
  if (s->phase == AnnouncementWindow::Phase::closed || s->phase == AnnouncementWindow::Phase::failed) {
    webkit_policy_decision_ignore(decision); return TRUE;
  }
  if (type == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
    auto* response = WEBKIT_RESPONSE_POLICY_DECISION(decision);
    auto* http = webkit_response_policy_decision_get_response(response);
    if (webkit_response_policy_decision_is_main_frame_main_resource(response) &&
        (!webkit_response_policy_decision_is_mime_type_supported(response) ||
         webkit_uri_response_get_status_code(http) >= 400)) {
      webkit_policy_decision_ignore(decision); Fail(s); return TRUE;
    }
    return FALSE;
  }
  if (type != WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION && type != WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) return FALSE;
  auto* navigation = webkit_navigation_policy_decision_get_navigation_action(WEBKIT_NAVIGATION_POLICY_DECISION(decision));
  const auto* uri = webkit_uri_request_get_uri(webkit_navigation_action_get_request(navigation));
  const auto action = DecideAnnouncementLink(s->document,uri ? uri : "",
      webkit_navigation_action_is_user_gesture(navigation),webkit_navigation_action_is_redirect(navigation),
      type == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION,s->phase == AnnouncementWindow::Phase::ready);
  if (action == AnnouncementLinkAction::in_window) return FALSE;
  webkit_policy_decision_ignore(decision);
  if (action == AnnouncementLinkAction::external_browser) QueueLink(s,uri);
  return TRUE;
}
void LoadChanged(WebKitWebView* web, WebKitLoadEvent event, gpointer data) {
  auto* s = static_cast<AnnouncementWindow*>(data);
  if (web != s->web || s->phase == AnnouncementWindow::Phase::failed) return;
  if (event == WEBKIT_LOAD_STARTED) StartLoading(s);
  if (event == WEBKIT_LOAD_FINISHED && s->phase == AnnouncementWindow::Phase::loading) {
    const auto* uri = webkit_web_view_get_uri(web);
    if (!uri || !AnnouncementHttpUri(uri)) { Fail(s); return; }
    s->document = uri;
    s->phase = AnnouncementWindow::Phase::ready;
    CancelTimer(s);
    gtk_stack_set_visible_child(GTK_STACK(s->stack),GTK_WIDGET(web));
  }
}
}
AnnouncementWindow* announcement_window_new(GtkWindow* parent,const char* icon) {
  auto* s = new AnnouncementWindow;
  s->parent = parent; s->icon = icon ? icon : ""; return s;
}
void announcement_window_close(AnnouncementWindow* s) {
  s->phase = AnnouncementWindow::Phase::closed;
  if (s->release) { g_source_remove(s->release); s->release = 0; }
  CancelTimer(s);
  if (s->external) { g_source_remove(s->external); s->external = 0; }
  s->links.clear(); s->document.clear();
  ReleaseWeb(s);
  wayland_window_icon_free(s->window_icon); s->window_icon = nullptr;
  if (s->window) {
    auto* window = s->window; s->window = nullptr;
    g_signal_handlers_disconnect_by_data(window,s);
    gtk_widget_destroy(window);
  }
  s->stack = s->label = nullptr;
}
bool announcement_window_open(AnnouncementWindow* s,const char* uri,int width,int height) {
  if (!uri || !AnnouncementHttpUri(uri) || width < 1 || width > 4096 || height < 1 || height > 4096) return false;
  // A new native/WebKit generation prevents delayed old loads replacing it.
  announcement_window_close(s);
  s->window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_window_set_title(GTK_WINDOW(s->window),"Sidravia · 校园公告");
  gtk_window_set_transient_for(GTK_WINDOW(s->window),s->parent);
  gtk_window_set_default_size(GTK_WINDOW(s->window),width,height);
  if (!s->icon.empty()) gtk_window_set_icon_from_file(GTK_WINDOW(s->window),s->icon.c_str(),nullptr);
  s->window_icon = wayland_window_icon_new(GTK_WINDOW(s->window),s->icon.c_str());
  s->stack = gtk_stack_new(); s->label = gtk_label_new("正在加载公告…");
  gtk_container_add(GTK_CONTAINER(s->window),s->stack);
  gtk_stack_add_named(GTK_STACK(s->stack),s->label,"status");
  auto* context = webkit_web_context_new_ephemeral();
  g_signal_connect(context,"download-started",G_CALLBACK(+[](WebKitWebContext*,WebKitDownload* download,gpointer) { webkit_download_cancel(download); }),nullptr);
  s->web = WEBKIT_WEB_VIEW(webkit_web_view_new_with_context(context));
  g_object_unref(context);
  g_object_set(webkit_web_view_get_settings(s->web),"allow-file-access-from-file-urls",FALSE,
      "allow-universal-access-from-file-urls",FALSE,"enable-developer-extras",FALSE,nullptr);
  gtk_stack_add_named(GTK_STACK(s->stack),GTK_WIDGET(s->web),"web");
  g_signal_connect(s->web,"decide-policy",G_CALLBACK(Policy),s);
  g_signal_connect(s->web,"load-changed",G_CALLBACK(LoadChanged),s);
  g_signal_connect(s->web,"load-failed",G_CALLBACK(+[](WebKitWebView*,WebKitLoadEvent,const gchar*,GError* error,gpointer data) -> gboolean {
    if (!g_error_matches(error,WEBKIT_NETWORK_ERROR,WEBKIT_NETWORK_ERROR_CANCELLED) &&
        !g_error_matches(error,WEBKIT_POLICY_ERROR,WEBKIT_POLICY_ERROR_FRAME_LOAD_INTERRUPTED_BY_POLICY_CHANGE)) Fail(static_cast<AnnouncementWindow*>(data));
    return TRUE; // Never show a WebKit error page with raw diagnostic details.
  }),s);
  g_signal_connect(s->web,"web-process-terminated",G_CALLBACK(+[](WebKitWebView*,WebKitWebProcessTerminationReason,gpointer data) { Fail(static_cast<AnnouncementWindow*>(data)); }),s);
  g_signal_connect(s->web,"permission-request",G_CALLBACK(+[](WebKitWebView*,WebKitPermissionRequest* request,gpointer) -> gboolean { webkit_permission_request_deny(request); return TRUE; }),nullptr);
  g_signal_connect(s->web,"authenticate",G_CALLBACK(+[](WebKitWebView*,WebKitAuthenticationRequest* request,gpointer) -> gboolean { webkit_authentication_request_cancel(request); return TRUE; }),nullptr);
  g_signal_connect(s->window,"delete-event",G_CALLBACK(+[](GtkWidget*,GdkEvent*,gpointer data) -> gboolean { announcement_window_close(static_cast<AnnouncementWindow*>(data)); return TRUE; }),s);
  announcement_window_set_dark(s,s->dark);
  StartLoading(s);
  gtk_widget_show_all(s->window);
  gtk_stack_set_visible_child(GTK_STACK(s->stack),s->label);
  webkit_web_view_load_uri(s->web,uri);
  gtk_window_present(GTK_WINDOW(s->window));
  return true;
}
void announcement_window_set_dark(AnnouncementWindow* s,bool dark) {
  s->dark = dark;
  if (s->web) {
    GdkRGBA background; gdk_rgba_parse(&background,dark ? "#171a20" : "#ffffff");
    webkit_web_view_set_background_color(s->web,&background);
  }
}
void announcement_window_free(AnnouncementWindow* s) { announcement_window_close(s); delete s; }
