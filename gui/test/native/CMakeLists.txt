cmake_minimum_required(VERSION 3.20)
project(sidravia_native_policy_tests LANGUAGES CXX)
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
  list(APPEND policies frame_decoration announcement_link_policy)
elseif(NOT CMAKE_SYSTEM_NAME STREQUAL "Linux")
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
