cmake_minimum_required(VERSION 3.20)
project(sidravia_native_policy_tests LANGUAGES C CXX)
enable_testing()

# Geometry is platform-neutral and guards both Windows and GTK input boundaries.
add_executable(frame_geometry_test ../frame_geometry_test.cpp)
set(policies frame_geometry)

if(WIN32 AND MSVC)
  add_executable(frame_decoration_test ../frame_decoration_test.cpp)
  add_executable(announcement_link_policy_test
    ../../windows/runner/announcement_link_policy_test.cpp
    ../../windows/runner/announcement_link_policy.cpp)
  target_link_libraries(announcement_link_policy_test PRIVATE urlmon ole32)
  add_executable(installation_gate_test installation_gate_windows_test.cpp
    ../../windows/runner/installation_gate.cpp)
  target_link_libraries(installation_gate_test PRIVATE advapi32)
  list(APPEND policies frame_decoration announcement_link_policy installation_gate)
elseif(CMAKE_SYSTEM_NAME STREQUAL "Linux")
  find_package(PkgConfig REQUIRED)
  pkg_check_modules(GLIB REQUIRED IMPORTED_TARGET glib-2.0)
  pkg_check_modules(WEBKIT REQUIRED IMPORTED_TARGET webkit2gtk-4.1)
  add_executable(announcement_link_policy_test
    ../../linux/runner/announcement_link_policy_test.cpp
    ../../linux/runner/announcement_link_policy.cc)
  target_link_libraries(announcement_link_policy_test PRIVATE PkgConfig::GLIB)
  list(APPEND policies announcement_link_policy installation_gate)
  add_executable(installation_gate_test installation_gate_test.cpp
    ../../linux/runner/installation_gate.cc)
  set_target_properties(installation_gate_test PROPERTIES
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_CURRENT_BINARY_DIR}/gui-runtime")
  target_link_libraries(installation_gate_test PRIVATE PkgConfig::GLIB)
  add_executable(announcement_window_integration
    ../announcement_window_integration.cpp
    ../../linux/runner/announcement_window.cc
    ../../linux/runner/announcement_link_policy.cc)
  target_link_libraries(announcement_window_integration PRIVATE PkgConfig::WEBKIT)
  include(../../linux/runner/wayland_window_icon.cmake)
  sidravia_wayland_window_icon(announcement_window_integration)
else()
  message(FATAL_ERROR "Policy tests support Windows MSVC or native Linux geometry")
endif()

foreach(policy ${policies})
  target_compile_features(${policy}_test PRIVATE cxx_std_17)
  if(MSVC)
    target_compile_options(${policy}_test PRIVATE /UNDEBUG)
  else()
    target_compile_options(${policy}_test PRIVATE -UNDEBUG)
  endif()
  add_test(NAME ${policy} COMMAND ${policy}_test)
endforeach()
