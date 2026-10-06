#ifndef SIDRAVIA_FRAME_DECORATION_H_
#define SIDRAVIA_FRAME_DECORATION_H_

#include <cstdint>

namespace sidravia_frame {
struct Decoration {
  bool request_round;
  int client_edge_pixels;
  std::uint32_t border_rgb;
};

// Decoration only: no window/client geometry or hit-test dimensions are changed.
// DWM owns the actual corner radius and snap/maximize policy.
constexpr Decoration DecorationFor(bool custom, bool maximized, bool dark,
                                   bool active) {
  return {custom, custom && !maximized ? 1 : 0,
          dark ? (active ? 0x5D6878u : 0x465160u)
               : (active ? 0xA6B1BFu : 0xBEC6D0u)};
}
}  // namespace sidravia_frame

#endif  // SIDRAVIA_FRAME_DECORATION_H_
