cmake_minimum_required(VERSION 3.13)
project(runner LANGUAGES CXX)

# Define the application target. To change its name, change BINARY_NAME in the
# top-level CMakeLists.txt, not the value here, or `flutter run` will no longer
# work.
#
# Any new source files that you add to the application should be added here.
add_executable(${BINARY_NAME}
  "main.cc"
  "my_application.cc"
  "desktop_shell.cc"
  "${FLUTTER_MANAGED_DIR}/generated_plugin_registrant.cc"
)

# Apply the standard set of build settings. This can be removed for applications
# that need different build settings.
apply_standard_settings(${BINARY_NAME})

# Add preprocessor definitions for the application ID.
add_definitions(-DAPPLICATION_ID="${APPLICATION_ID}")

# Add dependency libraries. Add any application-specific dependencies here.
target_link_libraries(${BINARY_NAME} PRIVATE flutter)
target_link_libraries(${BINARY_NAME} PRIVATE PkgConfig::GTK)

target_include_directories(${BINARY_NAME} PRIVATE "${CMAKE_SOURCE_DIR}")

pkg_check_modules(INDICATOR REQUIRED IMPORTED_TARGET ayatana-appindicator3-0.1)
pkg_check_modules(WEBKIT REQUIRED IMPORTED_TARGET webkit2gtk-4.1)
target_link_libraries(${BINARY_NAME} PRIVATE PkgConfig::INDICATOR PkgConfig::WEBKIT)

# Generate standard protocol bindings from pinned, licensed XML inputs.
pkg_check_modules(WAYLAND REQUIRED IMPORTED_TARGET wayland-client)
find_program(WAYLAND_SCANNER wayland-scanner REQUIRED)
foreach(protocol xdg-shell xdg-toplevel-icon-v1)
  set(protocol_xml "${CMAKE_CURRENT_SOURCE_DIR}/../protocols/${protocol}.xml")
  set(protocol_header "${CMAKE_CURRENT_BINARY_DIR}/${protocol}-client-protocol.h")
  set(protocol_code "${CMAKE_CURRENT_BINARY_DIR}/${protocol}-protocol.c")
  add_custom_command(OUTPUT "${protocol_header}"
    COMMAND "${WAYLAND_SCANNER}" client-header "${protocol_xml}" "${protocol_header}"
    DEPENDS "${protocol_xml}")
  add_custom_command(OUTPUT "${protocol_code}"
    COMMAND "${WAYLAND_SCANNER}" private-code "${protocol_xml}" "${protocol_code}"
    DEPENDS "${protocol_xml}")
  target_sources(${BINARY_NAME} PRIVATE "${protocol_header}" "${protocol_code}")
endforeach()
target_sources(${BINARY_NAME} PRIVATE "wayland_window_icon.cc")
target_include_directories(${BINARY_NAME} PRIVATE "${CMAKE_CURRENT_BINARY_DIR}")
target_link_libraries(${BINARY_NAME} PRIVATE PkgConfig::WAYLAND)
