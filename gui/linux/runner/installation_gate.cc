#include "installation_gate.h"

#include <glib.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include <cerrno>
#include <cstring>
#include <climits>

bool sidravia_can_start() {
  g_autofree gchar* exe = g_file_read_link("/proc/self/exe", nullptr);
  if (!exe) return false;
  g_autofree gchar* executable_directory = g_path_get_dirname(exe);
  const bool wrapped = g_str_has_suffix(executable_directory, "/gui-runtime");
  g_autofree gchar* product_directory = wrapped ? g_path_get_dirname(executable_directory) : nullptr;
  const gchar* directory = wrapped ? product_directory : executable_directory;
  if (strcmp(directory, "/usr/lib64/sidravia") != 0) {
    if (!wrapped) return true; // Unpackaged developer bundle only.
    const gchar* value = g_getenv("SIDRAVIA_GUI_GATE_FD");
    if (!value || !*value) return false;
    gchar* end = nullptr;
    errno = 0;
    const gint64 descriptor = g_ascii_strtoll(value, &end, 10);
    if (errno || !end || *end || descriptor < 3 || descriptor > INT_MAX) return false;
    const int fd = static_cast<int>(descriptor);
    struct stat lease = {};
    if (fstat(fd, &lease) != 0 || !S_ISREG(lease.st_mode) || lease.st_nlink != 1 ||
        lease.st_size != 0 || lease.st_uid != geteuid() || (lease.st_mode & 0077) ||
        flock(fd, LOCK_SH | LOCK_NB) != 0 || fcntl(fd, F_SETFD, FD_CLOEXEC) != 0) return false;
    // The bootstrap acquired this lease before the dynamic loader ran. Keep it
    // until exit, but prevent CTL/daemon children from inheriting GUI ownership.
    g_unsetenv("SIDRAVIA_GUI_GATE_FD");
    return true;
  }
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
