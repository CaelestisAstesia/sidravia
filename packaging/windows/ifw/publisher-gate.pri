!win32:error("The Sidravia publisher gate requires Windows")
CONFIG += no_batch
DEFINES += SODIUM_STATIC
SOURCES += $$PWD/productkeycheck.cpp $$IFW_SOURCE_TREE/src/libs/installer/commandlineparser_p.cpp
