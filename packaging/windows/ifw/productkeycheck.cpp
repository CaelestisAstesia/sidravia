/* Copyright (C) 2026 The Qt Company Ltd.
 *
 * SPDX-License-Identifier: LicenseRef-Qt-Commercial OR GPL-3.0-only WITH Qt-GPL-exception-1.0
*/

#include "productkeycheck.h"

#include "deployment.h"
#include "packagemanagercore.h"

#include <QtUiTools/QUiLoader>

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

void ProductKeyCheck::init(QInstaller::PackageManagerCore *core)
{
    d->core = core;
    Sidravia::prepareMaintenance(core->isInstaller());
}

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

bool ProductKeyCheck::isValidRepository(const QInstaller::Repository &repository) const
{
    return Sidravia::verifyRepository(repository, d->core);
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
