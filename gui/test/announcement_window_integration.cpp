#include "../linux/runner/announcement_window.h"
#include <webkit2/webkit2.h>
#include <chrono>
#include <cstdio>
#include <functional>
#include <fstream>
#include <stdexcept>
#include <string>

namespace {
void Until(const std::function<bool()>& ready,int seconds = 15) {
  auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(seconds);
  while (!ready()) {
    while (g_main_context_iteration(nullptr,FALSE)) {}
    if (std::chrono::steady_clock::now() > deadline) throw std::runtime_error("native announcement timeout");
    g_usleep(5000);
  }
}
GtkWidget* Find(GtkWidget* root,GType type) {
  if (G_TYPE_CHECK_INSTANCE_TYPE(root,type)) return root;
  if (!GTK_IS_CONTAINER(root)) return nullptr;
  GList* children = gtk_container_get_children(GTK_CONTAINER(root)); GtkWidget* found = nullptr;
  for (auto* item = children; item && !found; item=item->next) found=Find(GTK_WIDGET(item->data),type);
  g_list_free(children); return found;
}
GtkWidget* Top() {
  GList* windows=gtk_window_list_toplevels(); GtkWidget* found=nullptr;
  for (auto* item=windows;item;item=item->next) {
    const char* title=gtk_window_get_title(GTK_WINDOW(item->data));
    if (title && !g_strcmp0(title,"Sidravia · 校园公告")) found=GTK_WIDGET(item->data);
  }
  g_list_free(windows); return found;
}
GtkWidget* Visible() { auto* top=Top(); if(!top) return nullptr; auto* stack=Find(top,GTK_TYPE_STACK); return stack ? gtk_stack_get_visible_child(GTK_STACK(stack)) : nullptr; }
bool ShowingStatus(const char* message, bool can_open_in_browser) {
  auto* panel = Visible();
  if (!panel || !GTK_IS_BOX(panel)) return false;
  auto* label = Find(panel, GTK_TYPE_LABEL);
  auto* browser = Find(panel, GTK_TYPE_BUTTON);
  return label && browser && !g_strcmp0(gtk_label_get_text(GTK_LABEL(label)), message) &&
         (gtk_widget_get_sensitive(browser) != FALSE) == can_open_in_browser;
}
bool Failed() { return ShowingStatus("公告加载失败", true); }
WebKitWebView* Ready() { auto* child=Visible(); return child && WEBKIT_IS_WEB_VIEW(child) ? WEBKIT_WEB_VIEW(child) : nullptr; }
void Script(WebKitWebView* web,const char* code) {
  struct Result { bool done=false,failed=false; } result;
  webkit_web_view_evaluate_javascript(web,code,-1,nullptr,nullptr,nullptr,
    [](GObject* source,GAsyncResult* async,gpointer data) {
      auto* result=static_cast<Result*>(data); g_autoptr(GError) error=nullptr;
      g_autoptr(JSCValue) value=webkit_web_view_evaluate_javascript_finish(WEBKIT_WEB_VIEW(source),async,&error);
      result->failed=error != nullptr; result->done=true;
    },&result);
  Until([&]{return result.done;});
  if(result.failed) throw std::runtime_error("native JavaScript failed");
}
}
int main(int argc,char** argv) {
  setvbuf(stdout,nullptr,_IOLBF,0);
  if (argc != 4 || !gtk_init_check(&argc,&argv)) return 2;
  auto* parent=gtk_window_new(GTK_WINDOW_TOPLEVEL);
  auto* announcement=announcement_window_new(GTK_WINDOW(parent),argv[2]);
  const std::string base=argv[1];
  try {
    auto open=[&](const char* path){if(!announcement_window_open(announcement,(base+path).c_str(),600,460)) throw std::runtime_error("open failed");};
    if(announcement_window_open(announcement,"file:///tmp/forbidden",600,460)) throw std::runtime_error("accepted file URL");
    open("/redirect"); Until([]{return Ready();});
    if(!g_str_has_suffix(webkit_web_view_get_uri(Ready()),"/notice")) throw std::runtime_error("redirect not loaded");
    Script(Ready(),"location.hash='section'");
    Until([]{return Ready() && g_str_has_suffix(webkit_web_view_get_uri(Ready()),"#section");});
    open("/script-navigation");
    Until([]{return Ready() && g_str_has_suffix(webkit_web_view_get_uri(Ready()),"/script");});
    std::puts("HTTP redirect, same-document anchor and script navigation passed");
    Script(Ready(),"location.href='/external'");
    Until([&]{return g_file_test(argv[3],G_FILE_TEST_IS_REGULAR);});
    std::ifstream request(argv[3]); std::string line; std::getline(request,line);
    if(line.find("/external") == std::string::npos || !Ready() || !g_str_has_suffix(webkit_web_view_get_uri(Ready()),"/script")) throw std::runtime_error("external dispatch changed current document");
    std::puts("engine-marked gesture dispatched to isolated URI handler and preserved document");
    open("/delayed");
    if (!ShowingStatus("正在加载公告…", false)) throw std::runtime_error("loading status panel not shown");
    announcement_window_close(announcement); open("/notice");
    Until([]{return Ready();});
    auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(6);
    Until([&]{return std::chrono::steady_clock::now()>deadline;},8);
    if(!Ready() || !g_str_has_suffix(webkit_web_view_get_uri(Ready()),"/notice")) throw std::runtime_error("old load replaced new generation");
    std::puts("close/reopen isolates delayed old generation");
    open("/broken"); Until(Failed);
    open("/missing"); Until(Failed);
    open("/server-error"); Until(Failed);
    open("/download"); Until(Failed);
    open("/notice"); Until([]{return Ready();});
    webkit_web_view_terminate_web_process(Ready()); Until(Failed);
    std::puts("transport/HTTP failure, download rejection and process termination passed");
    open("/hang"); Until(Failed,35);
    std::puts("real 30-second load deadline passed");
    open("/notice"); Until([]{return Ready();});
    announcement_window_set_dark(announcement,true);
    announcement_window_close(announcement);
    if(Top()) throw std::runtime_error("announcement window leaked");
    announcement_window_free(announcement); gtk_widget_destroy(parent);
    std::puts("announcement native lifecycle PASS"); return 0;
  } catch(const std::exception& error) {
    std::fprintf(stderr,"%s\n",error.what());
    announcement_window_free(announcement); gtk_widget_destroy(parent); return 1;
  }
}
