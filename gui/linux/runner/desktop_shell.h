#ifndef SIDRAVIA_DESKTOP_SHELL_H_
#define SIDRAVIA_DESKTOP_SHELL_H_
#include <flutter_linux/flutter_linux.h>
#include <gtk/gtk.h>

// Native shell owns desktop resources only; all domain state stays in Dart/Go.
struct DesktopShell;
DesktopShell* desktop_shell_new(GtkWindow* window, FlView* view);
void desktop_shell_free(DesktopShell* shell);
void desktop_shell_restore(DesktopShell* shell);
void desktop_shell_first_frame(DesktopShell* shell);
#endif
