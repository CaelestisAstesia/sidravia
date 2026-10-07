#include "../windows/runner/frame_geometry.h"
#include <cassert>
#include <iostream>
int main() {
  using namespace sidravia_frame;
  for (double dpi : {96., 120., 144., 192.}) {
    double s = dpi / 96.;
    auto hit = [=](double x, double y, bool max = false, bool modal = false) {
      return HitTest(x*s, y*s, 400*s, 690*s, dpi, max, true, modal);
    };
    assert(hit(1,1)==Hit::top_left); assert(hit(399,1)==Hit::top_right);
    assert(hit(1,689)==Hit::bottom_left); assert(hit(399,689)==Hit::bottom_right);
    assert(hit(1,350)==Hit::left); assert(hit(399,350)==Hit::right);
    assert(hit(200,1)==Hit::top); assert(hit(200,689)==Hit::bottom);
    assert(hit(100,20)==Hit::caption); assert(hit(100,46)==Hit::client);
    assert(hit(334,26)==Hit::maximize); assert(hit(296,26)==Hit::client);
    assert(hit(372,26)==Hit::client); assert(hit(334,50)==Hit::client);
    assert(hit(1,350,true)==Hit::client); assert(hit(334,26,true)==Hit::maximize);
    assert(hit(100,20,false,true)==Hit::client);
    assert(hit(334,26,false,true)==Hit::client);
    assert(hit(1,350,false,true)==Hit::left);
    assert(hit(-1,20)==Hit::client);
    assert(PhysicalMinimum(360,dpi)==static_cast<int>(360*s));
    assert(PhysicalMinimum(640,dpi)==static_cast<int>(640*s));
    assert(HitTest(1,1,400,690,dpi,false,false)==Hit::client);
  }
  assert(PhysicalMinimum(360,110)==413);
  std::cout << "Frame hit tests and minimums: PASS (96/120/144/192 DPI)\n";
}
