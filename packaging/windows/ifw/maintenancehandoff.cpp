/* Copyright (C) 2026 The Qt Company Ltd.
 *
 * SPDX-License-Identifier: LicenseRef-Qt-Commercial OR GPL-3.0-only WITH Qt-GPL-exception-1.0
*/

#include "deployment.h"
#include "errors.h"

#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>
#include <QStringList>

#include <windows.h>
#include <sddl.h>
#include <restartmanager.h>

#include <array>
#include <utility>
#include <vector>

namespace {
struct InstallationGate
{
    HANDLE mutex = nullptr;

    ~InstallationGate()
    {
        if (mutex) {
            ReleaseMutex(mutex);
            CloseHandle(mutex);
        }
    }

    bool acquire()
    {
        HANDLE token = nullptr;
        if (!OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &token))
            return false;
        DWORD length = 0;
        GetTokenInformation(token, TokenUser, nullptr, 0, &length);
        std::vector<BYTE> user(length);
        const bool found = length && GetTokenInformation(token, TokenUser, user.data(), length, &length);
        CloseHandle(token);
        LPWSTR sid = nullptr;
        if (!found || !ConvertSidToStringSidW(reinterpret_cast<TOKEN_USER *>(user.data())->User.Sid, &sid))
            return false;
        const QString name = QStringLiteral("Global\\SidraviaInstall-") + QString::fromWCharArray(sid);
        LocalFree(sid);
        HANDLE candidate = CreateMutexW(nullptr, FALSE, reinterpret_cast<LPCWSTR>(name.utf16()));
        if (!candidate)
            return false;
        const DWORD acquired = WaitForSingleObject(candidate, 0);
        if (acquired != WAIT_OBJECT_0 && acquired != WAIT_ABANDONED) {
            CloseHandle(candidate);
            return false;
        }
        mutex = candidate;
        return true;
    }
};

struct HandoffHandles
{
    HANDLE ready = nullptr, proceed = nullptr;
    std::array<HANDLE, 2> previous{};
    int count = 0;

    ~HandoffHandles()
    {
        if (ready) CloseHandle(ready);
        if (proceed) CloseHandle(proceed);
        for (int i = 0; i < count; ++i)
            CloseHandle(previous[i]);
    }
};

bool awaitApplicationExit(const QByteArray &handoff)
{
    const auto parts = handoff.split(';');
    if (parts.size() < 2 || parts.size() > 3 || parts[0].size() != 32)
        return false;
    for (const char c : parts[0]) {
        if (!(c >= 'a' && c <= 'f') && !(c >= '0' && c <= '9'))
            return false;
    }
    const QString event = QStringLiteral("Local\\Sidravia-IFW-") + QString::fromLatin1(parts[0]);
    const QString ready = event + QStringLiteral("-ready");
    const QString proceed = event + QStringLiteral("-proceed");
    HandoffHandles handles;
    handles.ready = OpenEventW(EVENT_MODIFY_STATE, FALSE, reinterpret_cast<LPCWSTR>(ready.utf16()));
    handles.proceed = OpenEventW(SYNCHRONIZE, FALSE, reinterpret_cast<LPCWSTR>(proceed.utf16()));
    if (!handles.ready || !handles.proceed)
        return false;
    for (qsizetype i = 1; i < parts.size(); ++i) {
        bool ok = false;
        const DWORD pid = parts[i].toUInt(&ok);
        if (!ok || pid == 0 || pid == GetCurrentProcessId())
            return false;
        HANDLE process = OpenProcess(SYNCHRONIZE, FALSE, pid);
        if (!process)
            return false;
        handles.previous[handles.count++] = process;
    }
    return SetEvent(handles.ready)
        && WaitForSingleObject(handles.proceed, 30000) == WAIT_OBJECT_0
        && WaitForMultipleObjects(handles.count, handles.previous.data(), TRUE, 10000) == WAIT_OBJECT_0;
}

bool applicationFilesAvailable()
{
    const QDir directory(QCoreApplication::applicationDirPath());
    QStringList files;
    for (const auto &name : {QStringLiteral("Sidravia.exe"), QStringLiteral("sidraviad.exe"),
                             QStringLiteral("sidraviactl.exe"), QStringLiteral("sidraviaupdater.exe"),
                             QStringLiteral("sidraviamgr.exe")}) {
        const auto path = directory.filePath(name);
        if (QFileInfo::exists(path))
            files.append(QDir::toNativeSeparators(path));
    }
    if (files.isEmpty())
        return true;

    std::vector<LPCWSTR> names;
    for (const auto &file : std::as_const(files))
        names.push_back(reinterpret_cast<LPCWSTR>(file.utf16()));
    struct RestartManagerSession {
        DWORD id = 0;
        wchar_t key[CCH_RM_SESSION_KEY + 1]{};
        bool active = RmStartSession(&id, 0, key) == ERROR_SUCCESS;
        ~RestartManagerSession() { if (active) RmEndSession(id); }
    } session;
    if (!session.active || RmRegisterResources(session.id, UINT(names.size()), names.data(),
                                               0, nullptr, 0, nullptr) != ERROR_SUCCESS)
        return false;
    UINT needed = 0, count = 0;
    DWORD reasons = 0;
    return RmGetList(session.id, &needed, &count, nullptr, &reasons) == ERROR_SUCCESS
        && needed == 0 && reasons == 0;
}
}

void Sidravia::prepareMaintenance(bool offlineInstaller)
{
    const auto handoff = qgetenv("SIDRAVIA_IFW_HANDOFF");
    qunsetenv("SIDRAVIA_IFW_HANDOFF");
    // Keep the same startup exclusion for the application handoff and direct
    // IFW maintenance. Restart Manager provides an independent files-in-use
    // check, without asking it to stop a running application.
    static InstallationGate gate;
    if (!gate.acquire())
        throw QInstaller::Error(QObject::tr("Another Sidravia installation is already running."));
    if (!handoff.isEmpty() && !awaitApplicationExit(handoff))
        throw QInstaller::Error(QObject::tr("Sidravia could not finish preparing for the update."));
    if (!offlineInstaller && !applicationFilesAvailable())
        throw QInstaller::Error(QObject::tr("Close Sidravia and try again."));
}
