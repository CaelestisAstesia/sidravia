#ifndef SIDRAVIA_LINUX_ANNOUNCEMENT_LINK_POLICY_H_
#define SIDRAVIA_LINUX_ANNOUNCEMENT_LINK_POLICY_H_
#include <string>
#include "../../native/announcement_navigation.h"

bool AnnouncementHttpUri(const std::string& text);
AnnouncementLinkAction DecideAnnouncementLink(
    const std::string& document, const std::string& target, bool user_initiated,
    bool redirected, bool new_window, bool document_ready);
#endif
