#ifndef RUNNER_ANNOUNCEMENT_LINK_POLICY_H_
#define RUNNER_ANNOUNCEMENT_LINK_POLICY_H_

#include <string>

enum class AnnouncementLinkAction { in_window, external_browser, block };

// Shared navigation/new-window contract; only validated user gestures can leave the app.
AnnouncementLinkAction DecideAnnouncementLink(
    const std::wstring& document, const std::wstring& target,
    bool user_initiated, bool redirected, bool new_window, bool document_ready);

#endif  // RUNNER_ANNOUNCEMENT_LINK_POLICY_H_
