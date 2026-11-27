#include "../../linux/runner/installation_gate.h"

#include <glib.h>
#include <cassert>
#include <fcntl.h>
#include <unistd.h>

// CMake places this fixture under gui-runtime, exercising the packaged gate.
int main() {
  g_autofree gchar* executable = g_file_read_link("/proc/self/exe", nullptr);
  assert(executable);
  g_autofree gchar* runtime = g_path_get_dirname(executable);
  g_autofree gchar* directory = g_path_get_dirname(runtime);
  g_autofree gchar* seal = g_build_filename(directory, "sidravia.update", nullptr);

  assert(sidravia_can_start());
  const int fd = open(seal, O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC, 0600);
  assert(fd >= 0);
  assert(close(fd) == 0);
  const bool admitted_while_sealed = sidravia_can_start();
  assert(unlink(seal) == 0);
  assert(!admitted_while_sealed);
  assert(sidravia_can_start());
}
