#ifndef SIDRAVIA_LINUX_ANNOUNCEMENT_WINDOW_H_
#define SIDRAVIA_LINUX_ANNOUNCEMENT_WINDOW_H_
#include <gtk/gtk.h>
struct AnnouncementWindow;
AnnouncementWindow* announcement_window_new(GtkWindow* parent, const char* icon);
bool announcement_window_open(AnnouncementWindow*, const char* uri, int width, int height);
void announcement_window_close(AnnouncementWindow*);
void announcement_window_set_dark(AnnouncementWindow*, bool dark);
void announcement_window_free(AnnouncementWindow*);
#endif
