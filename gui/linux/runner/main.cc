#include "my_application.h"
#include "installation_gate.h"
#include <cstdio>

int main(int argc, char** argv) {
  if (!sidravia_can_start()) {
    std::fputs("Sidravia: installation maintenance is active or startup admission is unavailable.\n", stderr);
    return 1;
  }
  g_autoptr(MyApplication) app = my_application_new();
  return g_application_run(G_APPLICATION(app), argc, argv);
}
