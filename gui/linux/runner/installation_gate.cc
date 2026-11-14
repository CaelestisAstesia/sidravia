#include "installation_gate.h"

#include <glib.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include <cerrno>
#include <cstring>

bool sidravia_can_start() {
  g_autofree gchar* exe = g_file_read_link("/proc/self/exe", nullptr);
  if (!exe) return false;
  g_autofree gchar* directory = g_path_get_dirname(exe);
  if (strcmp(directory, "/usr/lib64/sidravia") != 0) return true;
  g_autofree gchar* marker = nullptr;
  g_autoptr(GError) error = nullptr;
  if (!g_file_get_contents("/usr/lib64/sidravia/sidravia.rpm", &marker, nullptr, &error)) {
    if (!g_error_matches(error, G_FILE_ERROR, G_FILE_ERROR_NOENT)) return false;
  } else if (strcmp(marker, "sidravia-rpm-v1\n") != 0) return false;
  int fd = open("/run/sidravia/rpm.lock", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return errno == ENOENT;
  struct stat owner = {}, parent = {}, info = {};
  bool valid = stat(directory, &owner) == 0 &&
      lstat("/run/sidravia", &parent) == 0 && fstat(fd, &info) == 0 &&
      S_ISDIR(parent.st_mode) && S_ISREG(info.st_mode) && info.st_size == 0 &&
      info.st_uid == owner.st_uid && parent.st_uid == owner.st_uid &&
      !(info.st_mode & 0022) && !(parent.st_mode & 0022) &&
      flock(fd, LOCK_SH | LOCK_NB) == 0;
  close(fd); // The live executable is now visible to maintenance's process scan.
  return valid;
}
