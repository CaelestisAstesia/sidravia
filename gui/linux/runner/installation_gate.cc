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
  g_autofree gchar* executable_directory = g_path_get_dirname(exe);
  const bool wrapped = g_str_has_suffix(executable_directory, "/gui-runtime");
  g_autofree gchar* product_directory = wrapped ? g_path_get_dirname(executable_directory) : nullptr;
  const gchar* directory = wrapped ? product_directory : executable_directory;
  const bool deb = strcmp(directory, "/usr/lib/sidravia") == 0;
  if (!deb && strcmp(directory, "/usr/lib64/sidravia") != 0) {
    struct stat seal = {};
    g_autofree gchar* seal_path = g_build_filename(directory, "sidravia.update", nullptr);
    return lstat(seal_path, &seal) < 0 && errno == ENOENT;
  }
  g_autofree gchar* marker = nullptr;
  g_autoptr(GError) error = nullptr;
  g_autofree gchar* marker_path = g_build_filename(directory, deb ? "sidravia.deb" : "sidravia.rpm", nullptr);
  if (!g_file_get_contents(marker_path, &marker, nullptr, &error)) {
    if (deb || !g_error_matches(error, G_FILE_ERROR, G_FILE_ERROR_NOENT)) return false;
  } else if (strcmp(marker, deb ? "sidravia-deb-v1\n" : "sidravia-rpm-v1\n") != 0) return false;
  const char* gate_directory = deb ? "/var/lib/sidravia/deb-maintenance" : "/run/sidravia";
  g_autofree gchar* gate = g_build_filename(gate_directory, deb ? "lock" : "rpm.lock", nullptr);
  int fd = open(gate, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return !deb && errno == ENOENT;
  struct stat owner = {}, parent = {}, info = {};
  bool valid = stat(directory, &owner) == 0 &&
      lstat(gate_directory, &parent) == 0 && fstat(fd, &info) == 0 &&
      S_ISDIR(parent.st_mode) && S_ISREG(info.st_mode) && info.st_size == 0 &&
      info.st_uid == owner.st_uid && parent.st_uid == owner.st_uid &&
      !(info.st_mode & 0022) && !(parent.st_mode & 0022) &&
      flock(fd, LOCK_SH | LOCK_NB) == 0;
  if (valid && deb) {
    struct stat block = {};
    valid = lstat("/var/lib/sidravia/deb-maintenance/block", &block) < 0 && errno == ENOENT;
  }
  close(fd); // The live executable is now visible to maintenance's process scan.
  return valid;
}
