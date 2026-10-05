#ifndef SIDRAVIA_FRAME_GEOMETRY_H_
#define SIDRAVIA_FRAME_GEOMETRY_H_
#include <algorithm>
#include <cmath>

// Pure physical hit geometry shared by top-level and Flutter child HWNDs.
// Values deliberately match the Flutter shell, never business-page constraints.
namespace sidravia_frame {
constexpr double kTop = 46;
constexpr double kButtonWidth = 36;
constexpr double kButtonHeight = 32;
constexpr double kButtonGap = 2;
constexpr double kButtonInset = 10;
enum class Hit { client, caption, maximize, left, right, top, bottom,
                 top_left, top_right, bottom_left, bottom_right };
inline Hit HitTest(double x, double y, double width, double height,
                   double dpi, bool maximized, bool enabled, bool modal = false) {
  if (!enabled || width <= 0 || height <= 0) return Hit::client;
  const double scale = dpi / 96.0;
  x /= scale; y /= scale; width /= scale; height /= scale;
  if (x < 0 || y < 0 || x >= width || y >= height) return Hit::client;
  if (!maximized) {
    constexpr double edge = 5;
    const bool left = x < edge, right = x >= width - edge;
    const bool top = y < edge, bottom = y >= height - edge;
    if (top && left) return Hit::top_left;
    if (top && right) return Hit::top_right;
    if (bottom && left) return Hit::bottom_left;
    if (bottom && right) return Hit::bottom_right;
    if (left) return Hit::left;
    if (right) return Hit::right;
    if (top) return Hit::top;
    if (bottom) return Hit::bottom;
  }
  if (modal) return Hit::client;
  const double right = width - kButtonInset;
  const double max_right = right - kButtonWidth - kButtonGap;
  const double min_left = max_right - kButtonWidth - kButtonGap - kButtonWidth;
  if (y >= kButtonInset && y < kButtonInset + kButtonHeight) {
    if (x >= max_right - kButtonWidth && x < max_right) return Hit::maximize;
    // Minimize and close remain client buttons for keyboard and Flutter input.
    if (x >= min_left && x < right) return Hit::client;
  }
  return y < kTop ? Hit::caption : Hit::client;
}
inline int PhysicalMinimum(int logical, double dpi) {
  return static_cast<int>(std::ceil(logical * dpi / 96.0));
}
}  // namespace sidravia_frame
#endif
