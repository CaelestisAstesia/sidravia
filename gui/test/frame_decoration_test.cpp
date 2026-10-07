#include "../windows/runner/frame_decoration.h"

#include <cassert>
#include <iostream>

int main() {
  using sidravia_frame::DecorationFor;
  for (bool dark : {false, true}) {
    for (bool active : {false, true}) {
      const auto normal = DecorationFor(true, false, dark, active);
      assert(normal.request_round);
      assert(normal.client_edge_pixels == 1);
      assert(normal.border_rgb == (dark ? (active ? 0x5D6878u : 0x465160u)
                                        : (active ? 0xA6B1BFu : 0xBEC6D0u)));
      const auto maximized = DecorationFor(true, true, dark, active);
      // The preference is stable: DWM, not our snap/edge heuristics, decides
      // whether to suppress rounding when the window is maximized or snapped.
      assert(maximized.request_round);
      assert(maximized.client_edge_pixels == 0);
      assert(maximized.border_rgb == normal.border_rgb);
      const auto restored = DecorationFor(true, false, dark, active);
      assert(restored.client_edge_pixels == normal.client_edge_pixels);
      for (bool zoomed : {false, true}) {
        const auto disabled = DecorationFor(false, zoomed, dark, active);
        assert(!disabled.request_round);
        assert(disabled.client_edge_pixels == 0);
      }
    }
  }
  std::cout << "Decoration policy: PASS (not a DWM visual test)\n";
}
