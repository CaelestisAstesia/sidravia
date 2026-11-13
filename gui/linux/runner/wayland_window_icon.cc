#include "wayland_window_icon.h"
#include <gdk/gdkwayland.h>
#include <wayland-client.h>
#include <sys/mman.h>
#include <unistd.h>
#include <cstring>
#include <cstdint>
#include <string>
#include "xdg-shell-client-protocol.h"
#include "xdg-toplevel-icon-v1-client-protocol.h"

struct WaylandWindowIcon {
  GtkWindow* window;
  std::string path;
  GdkWindow* surface = nullptr;
  wl_registry* registry = nullptr;
  wl_callback* registry_done = nullptr;
  wl_shm* shm = nullptr;
  xdg_toplevel_icon_manager_v1* manager = nullptr;
  xdg_toplevel_icon_v1* icon = nullptr;
  wl_buffer* buffer = nullptr;
  void* pixels = MAP_FAILED;
  size_t length = 0;
  bool registry_ready = false, manager_ready = false;
  const char* status = "pending";
};

static bool create_pixels(WaylandWindowIcon* s) {
  g_autoptr(GdkPixbuf) image = gdk_pixbuf_new_from_file(s->path.c_str(), nullptr);
  if (!image) return false;
  const int size = gdk_pixbuf_get_width(image);
  if (size <= 0 || size > 1024 || gdk_pixbuf_get_height(image) != size ||
      gdk_pixbuf_get_bits_per_sample(image) != 8) return false;
  const int channels = gdk_pixbuf_get_n_channels(image);
  if (channels != 3 && channels != 4) return false;
  const size_t length = static_cast<size_t>(size) * size * 4;
  int fd = memfd_create("sidravia-window-icon", MFD_CLOEXEC);
  if (fd < 0) return false;
  if (ftruncate(fd, length) != 0) { close(fd); return false; }
  void* pixels = mmap(nullptr, length, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
  if (pixels == MAP_FAILED) { close(fd); return false; }
  const auto* source = gdk_pixbuf_read_pixels(image);
  const int stride = gdk_pixbuf_get_rowstride(image);
  auto* target = static_cast<uint32_t*>(pixels);
  for (int y = 0; y < size; ++y) {
    for (int x = 0; x < size; ++x) {
      const auto* p = source + y * stride + x * channels;
      const uint32_t a = channels == 4 ? p[3] : 255;
      target[y * size + x] = (a << 24) | ((p[0] * a / 255) << 16) |
          ((p[1] * a / 255) << 8) | (p[2] * a / 255);
    }
  }
  auto* pool = wl_shm_create_pool(s->shm, fd, length);
  close(fd); // libwayland owns the marshalled FD; the mapping remains immutable.
  if (!pool) { munmap(pixels, length); return false; }
  s->buffer = wl_shm_pool_create_buffer(pool, 0, size, size, size * 4, WL_SHM_FORMAT_ARGB8888);
  wl_shm_pool_destroy(pool);
  if (!s->buffer) { munmap(pixels, length); return false; }
  s->pixels = pixels; s->length = length;
  return true;
}

static void apply_icon(WaylandWindowIcon* s) {
  if (!s->surface || !s->registry_ready || !s->manager_ready || !s->manager || !s->shm ||
      std::strcmp(s->status, "failed") == 0) return;
  auto* toplevel = gdk_wayland_window_get_xdg_toplevel(s->surface);
  if (!toplevel) return;
  if (!s->icon) {
    if (!create_pixels(s)) { s->status = "failed"; return; }
    s->icon = xdg_toplevel_icon_manager_v1_create_icon(s->manager);
    if (!s->icon) { s->status = "failed"; return; }
    xdg_toplevel_icon_v1_add_buffer(s->icon, s->buffer, 1);
  }
  // Reusing this immutable icon on a newly mapped toplevel is permitted.
  xdg_toplevel_icon_manager_v1_set_icon(s->manager, toplevel, s->icon);
  s->status = "submitted"; // Protocol submission is not visual delivery proof.
  gtk_widget_queue_draw(GTK_WIDGET(s->window)); // GDK owns surface commits.
}
static const xdg_toplevel_icon_manager_v1_listener manager_listener = {
  [](void*, xdg_toplevel_icon_manager_v1*, int32_t) {}, // Sizes are advisory.
  [](void* data, xdg_toplevel_icon_manager_v1*) {
    auto* s = static_cast<WaylandWindowIcon*>(data);
    s->manager_ready = true; apply_icon(s);
  }
};
static const wl_shm_listener shm_listener = {
  [](void*, wl_shm*, uint32_t) {}
};
static const wl_registry_listener registry_listener = {
  [](void* data, wl_registry* registry, uint32_t name, const char* interface, uint32_t) {
    auto* s = static_cast<WaylandWindowIcon*>(data);
    if (std::strcmp(interface, xdg_toplevel_icon_manager_v1_interface.name) == 0 && !s->manager) {
      s->manager = static_cast<xdg_toplevel_icon_manager_v1*>(wl_registry_bind(registry, name, &xdg_toplevel_icon_manager_v1_interface, 1));
      xdg_toplevel_icon_manager_v1_add_listener(s->manager, &manager_listener, s);
    } else if (std::strcmp(interface, wl_shm_interface.name) == 0 && !s->shm) {
      s->shm = static_cast<wl_shm*>(wl_registry_bind(registry, name, &wl_shm_interface, 1));
      wl_shm_add_listener(s->shm, &shm_listener, s);
    }
  },
  [](void*, wl_registry*, uint32_t) {} // Bound objects survive global removal.
};
static const wl_callback_listener sync_listener = {
  [](void* data, wl_callback* callback, uint32_t) {
    auto* s = static_cast<WaylandWindowIcon*>(data);
    wl_callback_destroy(callback); s->registry_done = nullptr;
    s->registry_ready = true;
    if (!s->manager || !s->shm) s->status = "unsupported";
    else apply_icon(s);
  }
};

static void clear_protocol(WaylandWindowIcon* s) {
  if (s->surface) g_signal_handlers_disconnect_by_data(s->surface, s);
  // The protocol requires the immutable buffer to outlive its icon object.
  if (s->icon) xdg_toplevel_icon_v1_destroy(s->icon);
  if (s->buffer) wl_buffer_destroy(s->buffer);
  if (s->pixels != MAP_FAILED) munmap(s->pixels, s->length);
  if (s->manager) xdg_toplevel_icon_manager_v1_destroy(s->manager);
  if (s->shm) wl_shm_destroy(s->shm);
  if (s->registry_done) wl_callback_destroy(s->registry_done);
  if (s->registry) wl_registry_destroy(s->registry);
  g_clear_object(&s->surface);
  s->icon = nullptr; s->buffer = nullptr; s->manager = nullptr; s->shm = nullptr;
  s->registry = nullptr; s->registry_done = nullptr; s->pixels = MAP_FAILED; s->length = 0;
  s->registry_ready = false; s->manager_ready = false;
}
static void realize_icon(WaylandWindowIcon* s) {
  auto* surface = gtk_widget_get_window(GTK_WIDGET(s->window));
  if (!surface || !GDK_IS_WAYLAND_WINDOW(surface)) { s->status = "not-wayland"; return; }
  s->status = "pending";
  s->surface = GDK_WINDOW(g_object_ref(surface));
  g_signal_connect(surface, "xdg-toplevel-realized", G_CALLBACK(+[](GdkWindow*, gpointer data) {
    apply_icon(static_cast<WaylandWindowIcon*>(data));
  }), s);
  auto* display = gdk_wayland_display_get_wl_display(gtk_widget_get_display(GTK_WIDGET(s->window)));
  s->registry = wl_display_get_registry(display);
  wl_registry_add_listener(s->registry, &registry_listener, s);
  // Async barrier only; GTK dispatches this queue and retains control of input.
  s->registry_done = wl_display_sync(display);
  wl_callback_add_listener(s->registry_done, &sync_listener, s);
}
WaylandWindowIcon* wayland_window_icon_new(GtkWindow* window, const char* path) {
  auto* s = new WaylandWindowIcon();
  s->window = GTK_WINDOW(g_object_ref(window)); s->path = path;
  g_signal_connect(window, "realize", G_CALLBACK(+[](GtkWidget*, gpointer data) { realize_icon(static_cast<WaylandWindowIcon*>(data)); }), s);
  g_signal_connect(window, "unrealize", G_CALLBACK(+[](GtkWidget*, gpointer data) { clear_protocol(static_cast<WaylandWindowIcon*>(data)); }), s);
  if (gtk_widget_get_realized(GTK_WIDGET(window))) realize_icon(s);
  return s;
}
void wayland_window_icon_free(WaylandWindowIcon* s) {
  if (!s) return;
  g_signal_handlers_disconnect_by_data(s->window, s);
  clear_protocol(s);
  g_object_unref(s->window); delete s;
}
const char* wayland_window_icon_status(WaylandWindowIcon* s) {
  return s ? s->status : "unsupported";
}
