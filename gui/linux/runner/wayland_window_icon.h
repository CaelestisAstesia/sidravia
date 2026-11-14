#ifndef SIDRAVIA_WAYLAND_WINDOW_ICON_H_
#define SIDRAVIA_WAYLAND_WINDOW_ICON_H_
#include <gtk/gtk.h>

struct WaylandWindowIcon;
// GTK still owns display dispatch and the surface. This adapter owns only its
// asynchronous icon-extension objects and immutable shared-memory icon pixels.
WaylandWindowIcon* wayland_window_icon_new(GtkWindow* window, const char* path);
void wayland_window_icon_free(WaylandWindowIcon* icon);
const char* wayland_window_icon_status(WaylandWindowIcon* icon);
#endif
