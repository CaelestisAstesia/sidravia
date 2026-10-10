cmake_minimum_required(VERSION 3.14)
project(runner LANGUAGES CXX)

# Define the application target. To change its name, change BINARY_NAME in the
# top-level CMakeLists.txt, not the value here, or `flutter run` will no longer
# work.
#
# Any new source files that you add to the application should be added here.
add_executable(${BINARY_NAME} WIN32
  "flutter_window.cpp"
  "school_announcement_window.cpp"
  "main.cpp"
  "utils.cpp"
  "win32_window.cpp"
  "${FLUTTER_MANAGED_DIR}/generated_plugin_registrant.cc"
  "Runner.rc"
  "runner.exe.manifest"
)

# Apply the standard set of build settings. This can be removed for applications
# that need different build settings.
apply_standard_settings(${BINARY_NAME})

# Add preprocessor definitions for the build version.
target_compile_definitions(${BINARY_NAME} PRIVATE "FLUTTER_VERSION=\"${FLUTTER_VERSION}\"")
target_compile_definitions(${BINARY_NAME} PRIVATE "FLUTTER_VERSION_MAJOR=${FLUTTER_VERSION_MAJOR}")
target_compile_definitions(${BINARY_NAME} PRIVATE "FLUTTER_VERSION_MINOR=${FLUTTER_VERSION_MINOR}")
target_compile_definitions(${BINARY_NAME} PRIVATE "FLUTTER_VERSION_PATCH=${FLUTTER_VERSION_PATCH}")
target_compile_definitions(${BINARY_NAME} PRIVATE "FLUTTER_VERSION_BUILD=${FLUTTER_VERSION_BUILD}")

# Disable Windows macros that collide with C++ standard library functions.
target_compile_definitions(${BINARY_NAME} PRIVATE "NOMINMAX")

# Add dependency libraries and include directories. Add any application-specific
# dependencies here.
target_link_libraries(${BINARY_NAME} PRIVATE flutter flutter_wrapper_app)
target_link_libraries(${BINARY_NAME} PRIVATE "dwmapi.lib")
target_link_libraries(${BINARY_NAME} PRIVATE "shell32.lib" "comctl32.lib")
target_include_directories(${BINARY_NAME} PRIVATE "${CMAKE_SOURCE_DIR}")

# Official pinned SDK only; the installed WebView2 runtime remains external.
include(FetchContent)
FetchContent_Declare(sidravia_webview2
  URL "https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/1.0.4258.31/microsoft.web.webview2.1.0.4258.31.nupkg"
  URL_HASH SHA256=56f7f4b8bf9aee4b8efefbbdd4f67d5f74ebd1b100ed0806da71bf76af481aa9
  DOWNLOAD_NAME WebView2.zip
)
FetchContent_MakeAvailable(sidravia_webview2)
string(TOLOWER "${CMAKE_GENERATOR_PLATFORM}" WEBVIEW2_PLATFORM)
if(WEBVIEW2_PLATFORM STREQUAL "arm64")
  set(WEBVIEW2_ARCH arm64)
elseif(WEBVIEW2_PLATFORM STREQUAL "x64")
  set(WEBVIEW2_ARCH x64)
elseif(WEBVIEW2_PLATFORM STREQUAL "win32" OR WEBVIEW2_PLATFORM STREQUAL "x86")
  set(WEBVIEW2_ARCH x86)
elseif(WEBVIEW2_PLATFORM STREQUAL "")
  string(TOLOWER "${CMAKE_SYSTEM_PROCESSOR}" WEBVIEW2_PROCESSOR)
  if(CMAKE_SIZEOF_VOID_P EQUAL 8 AND WEBVIEW2_PROCESSOR MATCHES "^(amd64|x86_64)$")
    set(WEBVIEW2_ARCH x64)
  elseif(CMAKE_SIZEOF_VOID_P EQUAL 8 AND WEBVIEW2_PROCESSOR MATCHES "^(arm64|aarch64)$")
    set(WEBVIEW2_ARCH arm64)
  elseif(CMAKE_SIZEOF_VOID_P EQUAL 4 AND WEBVIEW2_PROCESSOR MATCHES "^(x86|i[3-6]86|amd64|x86_64)$")
    set(WEBVIEW2_ARCH x86)
  else()
    message(FATAL_ERROR "Unsupported WebView2 target architecture")
  endif()
else()
  message(FATAL_ERROR "Unsupported WebView2 generator platform")
endif()
target_include_directories(${BINARY_NAME} SYSTEM PRIVATE
  "${sidravia_webview2_SOURCE_DIR}/build/native/include")
target_link_libraries(${BINARY_NAME} PRIVATE
  "${sidravia_webview2_SOURCE_DIR}/build/native/${WEBVIEW2_ARCH}/WebView2LoaderStatic.lib"
  "version.lib" "urlmon.lib" "ole32.lib" "oleaut32.lib" "shlwapi.lib")
install(FILES "${sidravia_webview2_SOURCE_DIR}/LICENSE.txt"
  DESTINATION "$<TARGET_FILE_DIR:${BINARY_NAME}>/licenses/Microsoft.Web.WebView2" COMPONENT Runtime)

# Run the Flutter tool portions of the build. This must not be removed.
add_dependencies(${BINARY_NAME} flutter_assemble)
