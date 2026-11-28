!win32:error("The Sidravia publisher gate requires Windows")
CONFIG += no_batch
DEFINES += SODIUM_STATIC
SOURCES += $$PWD/productkeycheck.cpp \
    $$PWD/maintenancehandoff.cpp \
    $$PWD/repositoryverification.cpp \
    $$PWD/uninstallintegration.cpp \
    $$IFW_SOURCE_TREE/src/libs/installer/commandlineparser_p.cpp
HEADERS += $$PWD/deployment.h
