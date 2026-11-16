#include "../../linux/runner/installation_gate.h"

#include <glib.h>
#include <cassert>
#include <cstdlib>
#include <fcntl.h>
#include <sys/file.h>
#include <unistd.h>

// CMake places this fixture under gui-runtime, exercising the packaged gate.
int main() {
  g_unsetenv("SIDRAVIA_GUI_GATE_FD");
  assert(!sidravia_can_start());
  char path[] = "/tmp/sidravia-native-gate-XXXXXX";
  const int fd = mkstemp(path);
  assert(fd >= 3);
  assert(flock(fd, LOCK_SH | LOCK_NB) == 0);
  g_autofree gchar* number = g_strdup_printf("%d", fd);
  g_setenv("SIDRAVIA_GUI_GATE_FD", number, true);
  assert(sidravia_can_start());
  assert(!g_getenv("SIDRAVIA_GUI_GATE_FD"));
  assert(fcntl(fd, F_GETFD) & FD_CLOEXEC);
  const int exclusive = open(path, O_RDWR | O_CLOEXEC);
  assert(exclusive >= 3);
  assert(flock(exclusive, LOCK_EX | LOCK_NB) != 0);
  close(fd);
  assert(flock(exclusive, LOCK_EX | LOCK_NB) == 0);
  close(exclusive);
  assert(unlink(path) == 0);
  return 0;
}
