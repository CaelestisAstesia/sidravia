cmake_minimum_required(VERSION 3.20)
project(sidravia_native_policy_tests LANGUAGES CXX)

if(NOT WIN32 OR NOT MSVC)
  message(FATAL_ERROR "These policy tests require the Windows MSVC toolchain")
endif()
enable_testing()

add_executable(frame_geometry_test ../frame_geometry_test.cpp)
add_executable(frame_decoration_test ../frame_decoration_test.cpp)
add_executable(announcement_link_policy_test
  ../../windows/runner/announcement_link_policy_test.cpp
  ../../windows/runner/announcement_link_policy.cpp)
target_link_libraries(announcement_link_policy_test PRIVATE urlmon ole32)

foreach(policy frame_geometry frame_decoration announcement_link_policy)
  target_compile_features(${policy}_test PRIVATE cxx_std_17)
  target_compile_options(${policy}_test PRIVATE /UNDEBUG)
  add_test(NAME ${policy} COMMAND ${policy}_test)
endforeach()
