/* Copyright (C) 2026 The Qt Company Ltd.
 *
 * SPDX-License-Identifier: LicenseRef-Qt-Commercial OR GPL-3.0-only WITH Qt-GPL-exception-1.0
*/

#include "productkeycheck.h"
#include "packagemanagercore.h"
#include "errors.h"

#include <QtUiTools/QUiLoader>

#include "repository.h"
#include <QCoreApplication>
#include <QCryptographicHash>
#include <QDomDocument>
#include <QDirIterator>
#include <QFile>
#include <QSet>
#include <QJsonDocument>
#include <QJsonObject>
#include <QProcess>

#include "publisherkey.h"

#include <sodium.h>
#include <windows.h>
#include <sddl.h>
#include <restartmanager.h>

#include <memory>
#include <array>
#include <utility>
#include <vector>

class ProductKeyCheckPrivate
{
public:
    ProductKeyCheckPrivate() {
        m_uiLoader = new QUiLoader();
    }
    ~ProductKeyCheckPrivate() {
        delete m_uiLoader;
    }
    QUiLoader * m_uiLoader;
    QInstaller::PackageManagerCore *core = nullptr;
};

ProductKeyCheck::ProductKeyCheck()
    : d(new ProductKeyCheckPrivate())
{
}

ProductKeyCheck::~ProductKeyCheck()
{
    delete d;
}

ProductKeyCheck *ProductKeyCheck::instance()
{
    static ProductKeyCheck instance;
    return &instance;
}

QUiLoader *ProductKeyCheck::uiLoader()
{
    return d->m_uiLoader;
}

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

void ProductKeyCheck::init(QInstaller::PackageManagerCore *core)
{
    d->core = core;
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
    if (!core->isInstaller() && !applicationFilesAvailable())
        throw QInstaller::Error(QObject::tr("Close Sidravia and try again."));
}

namespace QInstaller {
// Full uninstallation only. An update keeps the user's PATH and startup
// settings. The installed manager owns their validation and removal rules.
void sidraviaRemoveUserIntegration(const QString &directory)
{
    const QDir installed(QCoreApplication::applicationDirPath());
    if (QDir(directory).canonicalPath().compare(installed.canonicalPath(), Qt::CaseInsensitive) != 0)
        throw Error(QObject::tr("Sidravia could not confirm the uninstall directory."));
    const QString manager = installed.filePath(QStringLiteral("sidraviamgr.exe"));
    const QFileInfo info(manager);
    if (!info.isFile() || info.isSymLink()) {
        qWarning("Sidravia user integration manager is unavailable; its registrations may need manual cleanup.");
        return;
    }
    for (const auto &feature : {QStringLiteral("autostart"), QStringLiteral("path")}) {
        QProcess process;
        process.setWorkingDirectory(directory);
        process.start(manager, {feature, QStringLiteral("disable")});
        if (!process.waitForStarted(5000)) {
            qWarning("Sidravia user integration manager could not start; its registrations may need manual cleanup.");
            return;
        }
        if (!process.waitForFinished(15000)) {
            process.kill();
            process.waitForFinished(3000);
            throw Error(QObject::tr("Sidravia could not finish removing its user integration. Please try again."));
        }
        if (process.exitStatus() == QProcess::CrashExit) {
            qWarning("Sidravia user integration manager stopped unexpectedly; its registrations may need manual cleanup.");
            return;
        }
        if (process.exitCode() == 0)
            continue;
        const auto result = QJsonDocument::fromJson(process.readAllStandardOutput()).object();
        if (feature == QStringLiteral("autostart")
            && result.value(QStringLiteral("code")).toString() == QStringLiteral("scheduled_task_conflict")) {
            // Another installation or a manually created task owns this name.
            continue;
        }
        throw Error(QObject::tr("Sidravia could not remove its user integration. Please try again."));
    }
}
} // namespace QInstaller

bool ProductKeyCheck::hasValidKey()
{
    return true;
}

bool ProductKeyCheck::applyKey(const QStringList &/*arguments*/)
{
    return true;
}

QString ProductKeyCheck::lastErrorString()
{
    return QString();
}

QString ProductKeyCheck::maintainanceToolDetailErrorNotice()
{
    return QString();
}

// to filter none valid licenses
bool ProductKeyCheck::isValidLicenseTextFile(const QString &/*fileName*/)
{
    return true;
}


namespace {
QByteArray readBounded(const QString &path, qint64 limit)
{
    QFile file(path);
    if (!file.open(QIODevice::ReadOnly) || file.size() <= 0 || file.size() > limit)
        return {};
    return file.readAll();
}

bool verifySignature(const QByteArray &manifest, const QByteArray &signature)
{
    static const bool sodiumReady = sodium_init() >= 0;
    const QByteArray publicKey = QByteArray::fromHex(
        SIDRAVIA_PUBLISHER_PUBLIC_KEY);
    return sodiumReady && publicKey.size() == crypto_sign_PUBLICKEYBYTES
        && signature.size() == crypto_sign_BYTES
        && crypto_sign_verify_detached(
            reinterpret_cast<const unsigned char *>(signature.constData()),
            reinterpret_cast<const unsigned char *>(manifest.constData()),
            manifest.size(),
            reinterpret_cast<const unsigned char *>(publicKey.constData())) == 0;
}
struct RepositoryLocks
{
    ~RepositoryLocks()
    {
        for (const auto handle : handles)
            CloseHandle(handle);
    }

    bool add(const QString &path, bool contents = true)
    {
        const auto normalized = QDir::toNativeSeparators(path);
        const DWORD access = contents ? GENERIC_READ : FILE_READ_ATTRIBUTES;
        const DWORD sharing = contents ? FILE_SHARE_READ : (FILE_SHARE_READ | FILE_SHARE_WRITE);
        HANDLE file = CreateFileW(reinterpret_cast<LPCWSTR>(normalized.utf16()), access,
            sharing, nullptr, OPEN_EXISTING,
            FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OPEN_REPARSE_POINT, nullptr);
        if (file == INVALID_HANDLE_VALUE)
            return false;
        FILE_ATTRIBUTE_TAG_INFO attributes;
        if (!GetFileInformationByHandleEx(file, FileAttributeTagInfo, &attributes, sizeof attributes)
            || (attributes.FileAttributes & FILE_ATTRIBUTE_REPARSE_POINT)) {
            CloseHandle(file);
            return false;
        }
        handles.push_back(file);
        return true;
    }
    std::vector<HANDLE> handles;
};

QString fileKey(const QString &path)
{
    return QDir::cleanPath(QDir::fromNativeSeparators(path)).toCaseFolded();
}

struct VerifiedRepository
{
    RepositoryLocks locks;
    QSet<QString> files;
};
std::vector<std::unique_ptr<VerifiedRepository>> verifiedRepositories;

bool matchesPackageIndex(const QByteArray &xml, const QString &version, bool gui)
{
    QDomDocument document;
    if (version.isEmpty() || !document.setContent(xml))
        return false;
    const auto root = document.documentElement();
    if (root.tagName() != QStringLiteral("Updates"))
        return false;
    QSet<QString> components;
    for (auto package = root.firstChildElement(QStringLiteral("PackageUpdate")); !package.isNull();
         package = package.nextSiblingElement(QStringLiteral("PackageUpdate"))) {
        const auto name = package.firstChildElement(QStringLiteral("Name")).text();
        if (package.firstChildElement(QStringLiteral("Version")).text() != version || components.contains(name))
            return false;
        components.insert(name);
    }
    return components.size() == (gui ? 2 : 1)
        && components.contains(QStringLiteral("org.sidravia.core"))
        && components.contains(QStringLiteral("org.sidravia.gui")) == gui;
}

bool verifyRepository(const QInstaller::Repository &repository, QInstaller::PackageManagerCore *core)
{
    if (!core || core->isInstaller())
        return false;
    if (!repository.url().isLocalFile() || repository.isCompressed())
        return false;

    const QDir root(repository.url().toLocalFile());
    // Lock directories from the volume down before opening children. Volume
    // root identity is outside the profile-owned staging directory.
    QDir ancestor(root.absolutePath());
    QStringList ancestors;
    while (ancestor.cdUp() && !ancestor.isRoot())
        ancestors.prepend(ancestor.absolutePath());
    auto snapshot = std::make_unique<VerifiedRepository>();
    for (const auto &directory : std::as_const(ancestors)) {
        if (!snapshot->locks.add(directory, false))
            return false;
    }
    if (!snapshot->locks.add(root.absolutePath()))
        return false;
    QDirIterator lockingPaths(root.absolutePath(), QDir::AllEntries | QDir::Hidden | QDir::System
                              | QDir::NoDotAndDotDot, QDirIterator::Subdirectories);
    QSet<QString> locked;
    while (lockingPaths.hasNext()) {
        const auto path = lockingPaths.next();
        if (!snapshot->locks.add(path))
            return false;
        locked.insert(fileKey(path));
    }
    const QFileInfo rootInfo(root.absolutePath());
    if (!rootInfo.isDir() || rootInfo.isSymLink())
        return false;
    const QString manifestName = QStringLiteral("sidravia-ifw-inventory.json");
    const QString signatureName = QStringLiteral("sidravia-ifw-inventory.sig");
    if (!locked.contains(fileKey(root.filePath(manifestName)))
        || !locked.contains(fileKey(root.filePath(signatureName))))
        return false;
    const QByteArray manifest = readBounded(root.filePath(manifestName), 256 * 1024);
    const QByteArray signature = readBounded(root.filePath(signatureName), 64);
    if (manifest.isEmpty() || !verifySignature(manifest, signature))
        return false;
    QJsonParseError error;
    const auto document = QJsonDocument::fromJson(manifest, &error);
    if (error.error != QJsonParseError::NoError || !document.isObject())
        return false;
    const auto index = document.object();
    if (index.value(QStringLiteral("schemaVersion")).toInt() != 1
        || index.value(QStringLiteral("product")).toString() != QStringLiteral("Sidravia")
        || index.value(QStringLiteral("target")).toString() != QStringLiteral(SIDRAVIA_IFW_TARGET))
        return false;
    QFile executable(QCoreApplication::applicationFilePath());
    QCryptographicHash executableHash(QCryptographicHash::Sha256);
    if (!executable.open(QIODevice::ReadOnly) || !executableHash.addData(&executable)
        || QString::fromLatin1(executableHash.result().toHex())
            != index.value(QStringLiteral("maintenanceSha256")).toString())
        return false;
    const auto installed = core->localInstalledPackages();
    const bool gui = installed.contains(QStringLiteral("org.sidravia.gui"));
    if (!installed.contains(QStringLiteral("org.sidravia.core"))
        || installed.size() != (gui ? 2 : 1)
        || index.value(QStringLiteral("profile")).toString() != (gui ? QStringLiteral("gui") : QStringLiteral("core")))
        return false;
    auto files = index.value(QStringLiteral("files")).toObject();
    if (!files.contains(QStringLiteral("Updates.xml")))
        return false;
    QDirIterator paths(root.absolutePath(), QDir::AllEntries | QDir::Hidden | QDir::System
                       | QDir::NoDotAndDotDot, QDirIterator::Subdirectories);
    while (paths.hasNext()) {
        paths.next();
        const auto info = paths.fileInfo();
        if (info.isSymLink())
            return false;
        if (info.isDir())
            continue;
        const QString relative = root.relativeFilePath(info.filePath());
        if (relative == manifestName || relative == signatureName)
            continue;
        if (!info.isFile() || !files.contains(relative))
            return false;
        if (!locked.contains(fileKey(info.absoluteFilePath())))
            return false;
        QFile file(info.filePath());
        QCryptographicHash hash(QCryptographicHash::Sha256);
        if (!file.open(QIODevice::ReadOnly) || !hash.addData(&file)
            || QString::fromLatin1(hash.result().toHex()) != files.take(relative).toString())
            return false;
        snapshot->files.insert(fileKey(info.absoluteFilePath()));
    }
    if (!files.isEmpty() || !matchesPackageIndex(
            readBounded(root.filePath(QStringLiteral("Updates.xml")), 256 * 1024),
            index.value(QStringLiteral("version")).toString(), gui))
        return false;
    verifiedRepositories.push_back(std::move(snapshot));
    return true;
}
}

// Called on the main thread by the archive job after its ordinary checksum
// check. The resource maps the exact source held under its verified handle.
bool sidraviaIsPinnedArchive(const QString &source)
{
    const auto key = fileKey(source);
    for (const auto &snapshot : verifiedRepositories) {
        if (snapshot->files.contains(key))
            return true;
    }
    return false;
}

bool ProductKeyCheck::isValidRepository(const QInstaller::Repository &repository) const
{
    return verifyRepository(repository, d->core);
}

void ProductKeyCheck::addPackagesFromXml(const QString &xmlPath)
{
    Q_UNUSED(xmlPath)
}

bool ProductKeyCheck::isValidPackage(const QString &packageName) const
{
    Q_UNUSED(packageName)
    return true;
}

QList<int> ProductKeyCheck::registeredPages() const
{
    return QList<int>();
}

bool ProductKeyCheck::hasValidLicense() const
{
    return true;
}

bool ProductKeyCheck::hasAcceptedAllLicenses() const
{
    return true;
}

QString ProductKeyCheck::licenseAcceptanceText() const
{
    return QString();
}
QString ProductKeyCheck::securityWarning() const
{
    return QString();
}

QString ProductKeyCheck::additionalMetaDownloadWarning() const
{
    return QString();
}

QString ProductKeyCheck::additionalAcceptanceForCategory(const QStandardItem *item) const
{
    Q_UNUSED(item)
    return QString();
}
